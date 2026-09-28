package http3

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	uritemplate "github.com/yosida95/uritemplate/v3"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/app/proxyman/outbound"
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/bytespool"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/common/signal"
	"github.com/exclavenetwork/exclave-core/v5/common/task"
	"github.com/exclavenetwork/exclave-core/v5/features/policy"
	"github.com/exclavenetwork/exclave-core/v5/proxy"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
	v2tls "github.com/exclavenetwork/exclave-core/v5/transport/internet/tls"
)

var (
	_ proxy.Outbound                    = (*Client)(nil)
	_ proxy.ClosableOutbound            = (*Client)(nil)
	_ proxy.OutboundWithInterfaceUpdate = (*Client)(nil)
)

type Client struct {
	serverAddress   net.Destination
	config          *ClientConfig
	policyManager   policy.Manager
	quicConfig      *quic.Config
	transport       *http3.Transport
	cachedConnMutex sync.Mutex
	cachedConn      *http3.ClientConn
	createLock      sync.Mutex
	connectUDP      bool
	uriTemplate     *uritemplate.Template
}

func (c *Client) InterfaceUpdate() {
	_ = c.Close()
}

func (c *Client) Close() error {
	c.cachedConnMutex.Lock()
	if c.cachedConn != nil {
		c.cachedConn.CloseWithError(0, "")
		c.cachedConn = nil
	}
	c.cachedConnMutex.Unlock()
	return nil
}

func NewClient(ctx context.Context, config *ClientConfig) (*Client, error) {
	serverAddress := net.Destination{
		Address: config.Address.AsAddress(),
		Port:    net.Port(config.Port),
		Network: net.Network_UDP,
	}
	v := core.MustFromContext(ctx)
	quicConfig := &quic.Config{
		KeepAlivePeriod:      time.Second * 15,
		HandshakeIdleTimeout: time.Second * 8,
		EnableDatagrams:      config.ConnectUdp,
	}
	client := &Client{
		serverAddress: serverAddress,
		config:        config,
		policyManager: v.GetFeature(policy.ManagerType()).(policy.Manager),
		quicConfig:    quicConfig,
		transport: &http3.Transport{
			EnableDatagrams: config.ConnectUdp,
			QUICConfig:      quicConfig,
		},
		connectUDP: config.ConnectUdp,
	}
	if config.ConnectUdp {
		var err error
		if len(config.UriTemplate) == 0 {
			client.uriTemplate, err = uritemplate.New((&url.URL{
				Scheme: "https",
				Host:   serverAddress.NetAddr(),
			}).String() + "/.well-known/masque/udp/{target_host}/{target_port}/")
		} else {
			client.uriTemplate, err = uritemplate.New(config.UriTemplate)
		}
		if err != nil {
			return nil, err
		}
	}
	return client, nil
}

func (c *Client) Process(ctx context.Context, link *transport.Link, dialer internet.Dialer) error {
	outbound := session.OutboundFromContext(ctx)
	if outbound == nil || !outbound.Target.IsValid() {
		return newError("target not specified.")
	}
	target := outbound.Target

	if target.Network == net.Network_UDP && !c.connectUDP {
		return newError("UDP is not supported by HTTP/3 outbound")
	}

	newError("tunneling request to ", target, " via ", c.serverAddress.NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	var firstPayload []byte
	if target.Network == net.Network_TCP {
		if reader, ok := link.Reader.(buf.TimeoutReader); ok {
			if mbuf, _ := reader.ReadMultiBufferTimeout(proxy.FirstPayloadTimeout); mbuf != nil {
				mlen := mbuf.Len()
				firstPayload = bytespool.Alloc(mlen)
				mbuf, _ = buf.SplitBytes(mbuf, firstPayload)
				firstPayload = firstPayload[:mlen]
				buf.ReleaseMulti(mbuf)
				defer bytespool.Free(firstPayload)
			}
		}
	}

	conn, err := c.setupHTTPTunnel(ctx, target, dialer, firstPayload)
	if err != nil {
		return newError("failed to find an available destination").Base(err)
	}
	defer conn.Close()

	newError("tunneling request to ", target, " via ", c.serverAddress.NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	p := c.policyManager.ForLevel(c.config.Level)

	ctx, cancel := context.WithCancel(ctx)
	timer := signal.CancelAfterInactivity(ctx, cancel, p.Timeouts.ConnectionIdle)

	requestFunc := func() error {
		defer timer.SetTimeout(p.Timeouts.DownlinkOnly)
		var writer buf.Writer
		if target.Network == net.Network_TCP {
			writer = buf.NewWriter(conn)
		} else if _, ok := conn.(*http3PacketConn); ok {
			writer = newDatagramWriter(conn, target)
		} else {
			writer = newUoTWriter(conn, target)
		}
		return buf.Copy(link.Reader, writer, buf.UpdateActivity(timer))
	}
	responseFunc := func() error {
		defer timer.SetTimeout(p.Timeouts.UplinkOnly)
		var reader buf.Reader
		if target.Network == net.Network_TCP {
			reader = buf.NewReader(conn)
		} else if _, ok := conn.(*http3PacketConn); ok {
			reader = newDatagramReader(conn, target)
		} else {
			reader = newUoTReader(conn, target)
		}
		return buf.Copy(reader, link.Writer, buf.UpdateActivity(timer))
	}

	responseDonePost := task.OnSuccess(responseFunc, task.Close(link.Writer))
	if err := task.Run(ctx, requestFunc, responseDonePost); err != nil {
		return newError("connection ends").Base(err)
	}

	return nil
}

// setupHTTPTunnel will create a socket tunnel via HTTP CONNECT method
func (c *Client) setupHTTPTunnel(ctx context.Context, target net.Destination, dialer internet.Dialer, firstPayload []byte) (net.Conn, error) {
	handler, ok := dialer.(*outbound.Handler)
	if !ok {
		panic("dialer is not *outbound.Handler")
	}
	if handler.MuxEnabled() {
		return nil, newError("mux enabled")
	}
	if handler.TransportLayerEnabled() {
		return nil, newError("transport layer enabled")
	}
	streamSettings := handler.StreamSettings()
	if streamSettings == nil || streamSettings.SecurityType != "exclave.core.transport.internet.tls.Config" {
		return nil, newError("tls not enabled")
	}
	tlsSettings, ok := streamSettings.SecuritySettings.(*v2tls.Config)
	if !ok {
		return nil, newError("tls not enabled")
	}

	detachedContext := core.ToBackgroundDetachedContext(ctx)

	c.createLock.Lock()
	c.cachedConnMutex.Lock()
	clientConn := c.cachedConn
	c.cachedConnMutex.Unlock()
	if clientConn != nil {
		select {
		case <-clientConn.Context().Done():
			_ = clientConn.CloseWithError(0, "")
			c.cachedConnMutex.Lock()
			c.cachedConn = nil
			c.cachedConnMutex.Unlock()
		default:
		}
	}
	if clientConn == nil {
		tlsCfg, err := tlsSettings.GetTLSConfigWithContext(detachedContext, v2tls.WithNextProto("h3"), v2tls.WithDestination(c.serverAddress))
		if err != nil {
			return nil, err
		}
		rawConn, err := dialer.Dial(detachedContext, c.serverAddress)
		if err != nil {
			return nil, err
		}
		quicConn, err := quic.Dial(detachedContext, newQUICPacketConn(rawConn), rawConn.RemoteAddr(), tlsCfg, c.quicConfig)
		if err != nil {
			rawConn.Close()
			return nil, err
		}
		clientConn = c.transport.NewClientConn(quicConn)
		c.cachedConnMutex.Lock()
		c.cachedConn = clientConn
		c.cachedConnMutex.Unlock()
	}
	c.createLock.Unlock()

	select {
	case <-clientConn.ReceivedSettings():
	case <-clientConn.Context().Done():
		clientConn.CloseWithError(0, "")
		return nil, clientConn.Context().Err()
	}

	if target.Network != net.Network_TCP && !clientConn.Settings().EnableExtendedConnect {
		return nil, newError("extended connect not supported")
	}

	stream, err := clientConn.OpenRequestStream(detachedContext)
	if err != nil {
		clientConn.CloseWithError(0, "")
		return nil, err
	}

	var req *http.Request
	if target.Network == net.Network_TCP {
		req = &http.Request{
			Method: http.MethodConnect,
			URL: &url.URL{
				Scheme: "https",
				Host:   target.NetAddr(),
			},
			Header: make(http.Header),
			Host:   target.NetAddr(),
		}
	} else {
		var targetHost string
		if target.Address.Family().IsDomain() {
			targetHost = target.Address.Domain()
		} else {
			targetHost = target.Address.IP().String()
		}
		rawURL, err := c.uriTemplate.Expand(uritemplate.Values{
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
		req = &http.Request{
			Method: http.MethodConnect,
			Proto:  "connect-udp",
			URL:    u,
			Header: make(http.Header),
			Host:   u.Host,
		}
		req.Header.Set("capsule-protocol", "?1")
	}

	if c.config.Username != nil || c.config.Password != nil {
		auth := c.config.GetUsername() + ":" + c.config.GetPassword()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	}
	headers := c.config.GetHeaders()
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	if target.Network == net.Network_TCP {
		if err := stream.SendRequestHeader(req); err != nil {
			stream.CancelRead(0)
			stream.Close()
			clientConn.CloseWithError(0, "")
			return nil, err
		}
		var wg sync.WaitGroup
		var pErr error
		wg.Go(func() {
			_, pErr = stream.Write(firstPayload)
		})
		resp, err := stream.ReadResponse() // nolint: bodyclose
		if err != nil {
			stream.CancelRead(0)
			stream.Close()
			clientConn.CloseWithError(0, "")
			return nil, err
		}
		wg.Wait()
		if pErr != nil {
			resp.Body.Close()
			stream.CancelRead(0)
			stream.Close()
			clientConn.CloseWithError(0, "")
			return nil, pErr
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			stream.CancelRead(0)
			stream.Close()
			return nil, newError("Proxy responded with non 200 code: " + resp.Status)
		}
		return &http3Conn{
			stream: stream,
			body:   resp.Body,
		}, nil
	} else {
		err := stream.SendRequestHeader(req)
		if err != nil {
			stream.CancelRead(0)
			stream.Close()
			return nil, err
		}
		resp, err := stream.ReadResponse() // nolint: bodyclose
		if err != nil {
			stream.CancelRead(0)
			stream.Close()
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			stream.CancelRead(0)
			stream.Close()
			return nil, newError("Proxy responded with non 200 code: " + resp.Status)
		}
		if resp.Header.Get("capsule-protocol") != "?1" {
			resp.Body.Close()
			stream.CancelRead(0)
			stream.Close()
			return nil, newError("invalid response \"capsule-protocol\" header")
		}
		if clientConn.Settings().EnableDatagrams {
			return &http3PacketConn{
				ctx:    ctx,
				stream: stream,
				body:   resp.Body,
			}, nil
		} else {
			return &http3Conn{
				stream: stream,
				body:   resp.Body,
			}, nil
		}
	}
}

type http3PacketConn struct {
	ctx    context.Context
	stream *http3.RequestStream
	body   io.ReadCloser
}

func (c *http3PacketConn) Read(p []byte) (int, error) {
	payload, err := c.stream.ReceiveDatagram(c.ctx)
	if err != nil {
		return 0, err
	}
	if len(p) < len(payload) {
		return 0, io.ErrShortBuffer
	}
	return copy(p, payload), nil
}

func (c *http3PacketConn) Write(p []byte) (int, error) {
	err := c.stream.SendDatagram(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *http3PacketConn) RemoteAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *http3PacketConn) LocalAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *http3PacketConn) SetDeadline(t time.Time) error {
	return c.stream.SetDeadline(t)
}

func (c *http3PacketConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}

func (c *http3PacketConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

func (c *http3PacketConn) Close() error {
	c.body.Close()
	c.stream.CancelRead(0)
	return c.stream.Close()
}

type http3Conn struct {
	stream *http3.RequestStream
	body   io.ReadCloser
}

func (c *http3Conn) Read(p []byte) (n int, err error) {
	return c.stream.Read(p)
}

func (c *http3Conn) Write(p []byte) (n int, err error) {
	return c.stream.Write(p)
}

func (c *http3Conn) RemoteAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *http3Conn) LocalAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *http3Conn) SetDeadline(t time.Time) error {
	return c.stream.SetDeadline(t)
}

func (c *http3Conn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}

func (c *http3Conn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

func (c *http3Conn) Close() error {
	c.body.Close()
	c.stream.CancelRead(0)
	return c.stream.Close()
}

func init() {
	common.Must(common.RegisterConfig((*ClientConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewClient(ctx, config.(*ClientConfig))
	}))
}
