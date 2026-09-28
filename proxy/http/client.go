package http

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yosida95/uritemplate/v3"
	"golang.org/x/net/http2"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/bytespool"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/protocol"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/common/signal"
	"github.com/exclavenetwork/exclave-core/v5/common/task"
	"github.com/exclavenetwork/exclave-core/v5/features/policy"
	"github.com/exclavenetwork/exclave-core/v5/proxy"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/security"
)

var (
	_ proxy.Outbound                    = (*Client)(nil)
	_ proxy.ClosableOutbound            = (*Client)(nil)
	_ proxy.OutboundWithInterfaceUpdate = (*Client)(nil)
)

type Client struct {
	serverPicker       protocol.ServerPicker
	policyManager      policy.Manager
	h1SkipWaitForReply bool
	transport          *http2.Transport
	cachedH2Mutex      sync.Mutex
	cachedH2Conns      map[net.Destination]*list.List
	connectUDP         bool
	uriTemplate        *uritemplate.Template
}

func (c *Client) InterfaceUpdate() {
	_ = c.Close()
}

func (c *Client) Close() error {
	c.cachedH2Mutex.Lock()
	for _, cachedH2Conn := range c.cachedH2Conns {
		for elem := cachedH2Conn.Front(); elem != nil; elem = elem.Next() {
			_ = elem.Value.(*http2.ClientConn).Close()
			cachedH2Conn.Remove(elem)
		}
	}
	clear(c.cachedH2Conns)
	c.cachedH2Mutex.Unlock()
	return nil
}

// NewClient create a new http client based on the given config.
func NewClient(ctx context.Context, config *ClientConfig) (*Client, error) {
	serverList := protocol.NewServerList()
	for _, rec := range config.Server {
		s, err := protocol.NewServerSpecFromPB(rec)
		if err != nil {
			return nil, newError("failed to get server spec").Base(err)
		}
		serverList.AddServer(s)
	}
	if serverList.Size() == 0 {
		return nil, newError("0 target server")
	}
	v := core.MustFromContext(ctx)
	client := &Client{
		serverPicker:       protocol.NewRoundRobinServerPicker(serverList),
		policyManager:      v.GetFeature(policy.ManagerType()).(policy.Manager),
		h1SkipWaitForReply: config.H1SkipWaitForReply,
		transport: &http2.Transport{
			ReadIdleTimeout: time.Second * 15,
		},
		cachedH2Conns: make(map[net.Destination]*list.List),
		connectUDP:    config.ConnectUdp,
	}
	if config.ConnectUdp && len(config.UriTemplate) > 0 {
		var err error
		client.uriTemplate, err = uritemplate.New(config.UriTemplate)
		if err != nil {
			return nil, err
		}
	}
	return client, nil
}

// Process implements proxy.Outbound.Process. We first create a socket tunnel via HTTP CONNECT method, then redirect all inbound traffic to that tunnel.
func (c *Client) Process(ctx context.Context, link *transport.Link, dialer internet.Dialer) error {
	outbound := session.OutboundFromContext(ctx)
	if outbound == nil || !outbound.Target.IsValid() {
		return newError("target not specified.")
	}
	target := outbound.Target

	if target.Network == net.Network_UDP && !c.connectUDP {
		return newError("UDP is not supported by HTTP outbound")
	}

	server := c.serverPicker.PickServer()
	dest := server.Destination()

	var firstPayload []byte
	if target.Network == net.Network_TCP {
		if reader, ok := link.Reader.(buf.TimeoutReader); ok {
			// 0-RTT optimization for HTTP/2: If the payload comes very soon, it can be
			// transmitted together. Note we should not get stuck here, as the payload may
			// not exist (considering to access MySQL database via a HTTP proxy, where the
			// server sends hello to the client first).
			waitTime := proxy.FirstPayloadTimeout
			if c.h1SkipWaitForReply {
				// Some server require first write to be present in client hello.
				// Increase timeout to if the client have explicitly requested to skip waiting for reply.
				waitTime = time.Second
			}
			if mbuf, _ := reader.ReadMultiBufferTimeout(waitTime); mbuf != nil {
				mlen := mbuf.Len()
				firstPayload = bytespool.Alloc(mlen)
				mbuf, _ = buf.SplitBytes(mbuf, firstPayload)
				firstPayload = firstPayload[:mlen]
				buf.ReleaseMulti(mbuf)
				defer bytespool.Free(firstPayload)
			}
		}
	}

	user := server.PickUser()
	conn, firstResp, err := c.setupHTTPTunnel(ctx, dest, target, user, dialer, firstPayload, c.h1SkipWaitForReply)
	if err != nil {
		return newError("failed to find an available destination").Base(err)
	}
	defer conn.Close()
	if target.Network == net.Network_TCP {
		if _, ok := conn.(*http2Conn); !ok && !c.h1SkipWaitForReply {
			if _, err := conn.Write(firstPayload); err != nil {
				return err
			}
		}
	}
	if firstResp != nil {
		if err := link.Writer.WriteMultiBuffer(firstResp); err != nil {
			return err
		}
	}

	newError("tunneling request to ", target, " via ", server.Destination().NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	p := c.policyManager.ForLevel(0)
	if user != nil {
		p = c.policyManager.ForLevel(user.Level)
	}

	ctx, cancel := context.WithCancel(ctx)
	timer := signal.CancelAfterInactivity(ctx, cancel, p.Timeouts.ConnectionIdle)

	requestFunc := func() error {
		defer timer.SetTimeout(p.Timeouts.DownlinkOnly)
		if target.Network == net.Network_UDP {
			return buf.Copy(link.Reader, newUoTWriter(conn, target), buf.UpdateActivity(timer))
		}
		return buf.Copy(link.Reader, buf.NewWriter(conn), buf.UpdateActivity(timer))
	}
	responseFunc := func() error {
		defer timer.SetTimeout(p.Timeouts.UplinkOnly)
		if target.Network == net.Network_UDP {
			return buf.Copy(newUoTReader(conn, target), link.Writer, buf.UpdateActivity(timer))
		}
		return buf.Copy(buf.NewReader(conn), link.Writer, buf.UpdateActivity(timer))
	}

	responseDonePost := task.OnSuccess(responseFunc, task.Close(link.Writer))
	if err := task.Run(ctx, requestFunc, responseDonePost); err != nil {
		return newError("connection ends").Base(err)
	}

	return nil
}

// setupHTTPTunnel will create a socket tunnel via HTTP CONNECT method
func (c *Client) setupHTTPTunnel(ctx context.Context, dest net.Destination, target net.Destination, user *protocol.MemoryUser, dialer internet.Dialer, firstPayload []byte, writeFirstPayloadInH1 bool,
) (net.Conn, buf.MultiBuffer, error) {
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: target.NetAddr()},
		Header: make(http.Header),
		Host:   target.NetAddr(),
	}

	if user != nil && user.Account != nil {
		account := user.Account.(*Account)
		username, password, headers := account.GetUsername(), account.GetPassword(), account.GetHeaders()
		auth := username + ":" + password
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	connectHTTP1 := func(rawConn net.Conn) (net.Conn, buf.MultiBuffer, error) {
		if target.Network == net.Network_TCP {
			req.Header.Set("Proxy-Connection", "Keep-Alive")
		} else {
			req.Method = http.MethodGet
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "connect-udp")
			req.Header.Set("Capsule-Protocol", "?1")
			var targetHost string
			if target.Address.Family().IsDomain() {
				targetHost = target.Address.Domain()
			} else {
				targetHost = target.Address.IP().String()
			}
			uriTemplate := c.uriTemplate
			if uriTemplate == nil {
				var err error
				uriTemplate, err = uritemplate.New((&url.URL{
					Scheme: "https",
					Host:   dest.NetAddr(),
				}).String() + "/.well-known/masque/udp/{target_host}/{target_port}/")
				if err != nil {
					return nil, nil, err
				}
			}
			rawURL, err := uriTemplate.Expand(uritemplate.Values{
				"target_host": uritemplate.String(targetHost),
				"target_port": uritemplate.String(target.Port.String()),
			})
			if err != nil {
				return nil, nil, err
			}
			u, err := url.Parse(rawURL)
			if err != nil {
				return nil, nil, err
			}
			req.URL = u
			req.Host = u.Host
		}

		if target.Network != net.Network_TCP || !writeFirstPayloadInH1 {
			err := req.Write(rawConn)
			if err != nil {
				return nil, nil, err
			}
		} else {
			buffer := bytes.NewBuffer(nil)
			err := req.Write(buffer)
			if err != nil {
				return nil, nil, err
			}
			_, err = io.Copy(buffer, bytes.NewReader(firstPayload))
			if err != nil {
				return nil, nil, err
			}
			_, err = rawConn.Write(buffer.Bytes())
			if err != nil {
				return nil, nil, err
			}
		}
		bufferedReader := bufio.NewReader(rawConn)
		resp, err := http.ReadResponse(bufferedReader, req)
		if err != nil {
			return nil, nil, err
		}

		if target.Network == net.Network_TCP {
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return nil, nil, newError("Proxy responded with non 200 code: " + resp.Status)
			}
		} else {
			if resp.StatusCode != http.StatusSwitchingProtocols {
				resp.Body.Close()
				return nil, nil, newError("Proxy responded with non 200 code: " + resp.Status)
			}
			if resp.Header.Get("Connection") != "Upgrade" {
				resp.Body.Close()
				return nil, nil, newError("invalid response \"Connection\" header")
			}
			if resp.Header.Get("Upgrade") != "connect-udp" {
				resp.Body.Close()
				return nil, nil, newError("invalid response \"Upgrade\" header")
			}
			if resp.Header.Get("Capsule-Protocol") != "?1" {
				resp.Body.Close()
				return nil, nil, newError("invalid response \"Capsule-Protocol\" header")
			}
		}

		if bufferedReader.Buffered() > 0 {
			payload, err := buf.ReadFrom(io.LimitReader(bufferedReader, int64(bufferedReader.Buffered())))
			if err != nil {
				resp.Body.Close()
				return nil, nil, newError("unable to drain buffer: ").Base(err)
			}
			resp.Body.Close()
			return rawConn, payload, nil
		}
		return rawConn, nil, nil
	}

	connectHTTP2 := func(h2clientConn *http2.ClientConn, elem *list.Element) (net.Conn, error) {
		if target.Network != net.Network_TCP {
			var targetHost string
			if target.Address.Family().IsDomain() {
				targetHost = target.Address.Domain()
			} else {
				targetHost = target.Address.IP().String()
			}
			uriTemplate := c.uriTemplate
			if uriTemplate == nil {
				var err error
				uriTemplate, err = uritemplate.New((&url.URL{
					Host: dest.NetAddr(),
				}).String() + "/.well-known/masque/udp/{target_host}/{target_port}/")
				if err != nil {
					return nil, err
				}
			}
			rawURL, err := uriTemplate.Expand(uritemplate.Values{
				"target_host": uritemplate.String(targetHost),
				"target_port": uritemplate.String(target.Port.String()),
			})
			if err != nil {
				return nil, err
			}
			u, err := url.Parse(rawURL)
			if err != nil {
				return nil, err
			}
			req.URL = u
			req.URL.Host = u.Host
			req.Header.Set(":protocol", "connect-udp")
			req.Header.Set("capsule-protocol", "?1")
		}

		pr, pw := io.Pipe()
		req.Body = pr

		var pErr error
		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			if target.Network == net.Network_TCP {
				_, pErr = pw.Write(firstPayload)
			}
			wg.Done()
		}()

		resp, err := h2clientConn.RoundTrip(req) // nolint: bodyclose
		if err != nil {
			if strings.Contains(err.Error(), "extended connect not supported") {
				return nil, newError("extended connect not supported")
			}
			h2clientConn.Close()
			if elem != nil {
				c.cachedH2Mutex.Lock()
				if cachedH2Conn, found := c.cachedH2Conns[dest]; found {
					cachedH2Conn.Remove(elem)
				}
				c.cachedH2Mutex.Unlock()
			}
			return nil, err
		}

		wg.Wait()
		if pErr != nil {
			resp.Body.Close()
			h2clientConn.Close()
			if elem != nil {
				c.cachedH2Mutex.Lock()
				if cachedH2Conn, found := c.cachedH2Conns[dest]; found {
					cachedH2Conn.Remove(elem)
				}
				c.cachedH2Mutex.Unlock()
			}
			return nil, pErr
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, newError("Proxy responded with non 200 code: " + resp.Status)
		}
		if target.Network != net.Network_TCP && resp.Header.Get("capsule-protocol") != "?1" {
			resp.Body.Close()
			return nil, newError("invalid response \"capsule-protocol\" header")
		}
		return newHTTP2Conn(pw, resp.Body), nil
	}

	c.cachedH2Mutex.Lock()
	var elem *list.Element
	if cachedH2Conn, found := c.cachedH2Conns[dest]; found {
		elem = cachedH2Conn.Front()
	}
	c.cachedH2Mutex.Unlock()

	if elem != nil {
		if h2ClientConn := elem.Value.(*http2.ClientConn); h2ClientConn.CanTakeNewRequest() {
			proxyConn, err := connectHTTP2(h2ClientConn, elem)
			if err != nil {
				return nil, nil, err
			}
			return proxyConn, nil, nil
		} else {
			h2ClientConn.Close()
			c.cachedH2Mutex.Lock()
			if _, found := c.cachedH2Conns[dest]; found {
				c.cachedH2Conns[dest].Remove(elem)
			}
			c.cachedH2Mutex.Unlock()
		}
	}

	rawConn, err := dialer.Dial(ctx, dest)
	if err != nil {
		return nil, nil, err
	}

	iConn := rawConn
	if statConn, ok := iConn.(*internet.StatCouterConnection); ok {
		iConn = statConn.Connection
	}

	nextProto := ""
	if connALPNGetter, ok := iConn.(security.ConnectionApplicationProtocol); ok {
		nextProto, err = connALPNGetter.GetConnectionApplicationProtocol()
		if err != nil {
			rawConn.Close()
			return nil, nil, err
		}
	}

	switch nextProto {
	case "", "http/1.1":
		conn, mb, err := connectHTTP1(rawConn)
		if err != nil {
			rawConn.Close()
			return nil, nil, err
		}
		return conn, mb, nil
	case "h2":
		h2clientConn, err := c.transport.NewClientConn(rawConn)
		if err != nil {
			rawConn.Close()
			return nil, nil, err
		}

		proxyConn, err := connectHTTP2(h2clientConn, nil)
		if err != nil {
			return nil, nil, err
		}

		c.cachedH2Mutex.Lock()
		if _, found := c.cachedH2Conns[dest]; !found {
			c.cachedH2Conns[dest] = &list.List{}
		}
		c.cachedH2Conns[dest].PushFront(h2clientConn)
		c.cachedH2Mutex.Unlock()

		return proxyConn, nil, err
	default:
		rawConn.Close()
		return nil, nil, newError("negotiated unsupported application layer protocol: " + nextProto)
	}
}

func newHTTP2Conn(pipedReqBody *io.PipeWriter, respBody io.ReadCloser) net.Conn {
	return &http2Conn{in: pipedReqBody, out: respBody}
}

type http2Conn struct {
	in  *io.PipeWriter
	out io.ReadCloser
}

func (h *http2Conn) Read(p []byte) (n int, err error) {
	return h.out.Read(p)
}

func (h *http2Conn) Write(p []byte) (n int, err error) {
	return h.in.Write(p)
}

func (c *http2Conn) RemoteAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *http2Conn) LocalAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *http2Conn) SetDeadline(t time.Time) error {
	return nil
}

func (c *http2Conn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *http2Conn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (h *http2Conn) Close() error {
	h.in.Close()
	return h.out.Close()
}

func init() {
	common.Must(common.RegisterConfig((*ClientConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewClient(ctx, config.(*ClientConfig))
	}))
}
