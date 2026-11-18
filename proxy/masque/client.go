package masque

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	uritemplate "github.com/yosida95/uritemplate/v3"
	"golang.org/x/net/http2"
	"golang.zx2c4.com/wireguard/tun"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/app/proxyman/outbound"
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/errors"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/common/signal"
	"github.com/exclavenetwork/exclave-core/v5/common/task"
	"github.com/exclavenetwork/exclave-core/v5/features/dns"
	"github.com/exclavenetwork/exclave-core/v5/features/policy"
	"github.com/exclavenetwork/exclave-core/v5/proxy"
	"github.com/exclavenetwork/exclave-core/v5/proxy/masque/internal/connectip"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/security"
	v2tls "github.com/exclavenetwork/exclave-core/v5/transport/internet/tls"
)

var (
	_ proxy.Outbound                    = (*Client)(nil)
	_ proxy.OutboundWithInterfaceUpdate = (*Client)(nil)
	_ proxy.ClosableOutbound            = (*Client)(nil)
)

type Client struct {
	config        *ClientConfig
	serverAddr    net.Destination
	policyManager policy.Manager
	dns           dns.Client
	h2Transport   *http2.Transport
	h3Transport   *http3.Transport
	mtu           int32
	reqURL        *url.URL
	ctxLock       sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	createLock    sync.Mutex
	tunnel        atomic.Pointer[tunnel]
	h2ClientConn  atomic.Pointer[http2ClientConn]
	h3ClientConn  atomic.Pointer[http3ClientConn]
}

func (c *Client) Close() error {
	c.ctxLock.Lock()
	c.cancel()
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.ctxLock.Unlock()
	tunnel := c.tunnel.Swap(nil)
	h2ClientConn := c.h2ClientConn.Swap(nil)
	h3ClientConn := c.h3ClientConn.Swap(nil)
	if tunnel != nil {
		tunnel.close()
	}
	if h2ClientConn != nil {
		h2ClientConn.close()
	}
	if h3ClientConn != nil {
		h3ClientConn.close()
	}
	return nil
}

func (c *Client) InterfaceUpdate() {
	_ = c.Close()
}

func NewClient(ctx context.Context, config *ClientConfig) (*Client, error) {
	serverAddr := net.Destination{
		Address: config.Address.AsAddress(),
		Port:    net.Port(config.Port),
	}
	if config.Http3 {
		serverAddr.Network = net.Network_UDP
	} else {
		serverAddr.Network = net.Network_TCP
	}
	v := core.MustFromContext(ctx)
	c := &Client{
		config:        config,
		serverAddr:    serverAddr,
		policyManager: v.GetFeature(policy.ManagerType()).(policy.Manager),
		dns:           v.GetFeature(dns.ClientType()).(dns.Client),
		mtu:           config.Mtu,
	}
	if c.mtu == 0 {
		c.mtu = 1350
	}
	if config.Http3 {
		c.h3Transport = &http3.Transport{EnableDatagrams: true}
	} else {
		c.h2Transport = &http2.Transport{ReadIdleTimeout: time.Second * 15}
	}
	if len(config.UriTemplate) == 0 {
		c.reqURL = &url.URL{
			Scheme: "https",
			Host:   serverAddr.NetAddr(),
			Path:   "/.well-known/masque/ip/*/*/",
		}
	} else {
		template, err := uritemplate.New(config.UriTemplate)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(template.Varnames(), func(varname string) bool {
			return varname != "target" && varname != "ipproto"
		}) {
			return nil, newError("connect-ip: IP flow forwarding not supported")
		}
		rawURL, err := template.Expand(uritemplate.Values{
			"target":  uritemplate.String("*"),
			"ipproto": uritemplate.String("*"),
		})
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			return nil, err
		}
		c.reqURL = u
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c, nil
}

func (c *Client) Process(ctx context.Context, link *transport.Link, dialer internet.Dialer) error {
	ob := session.OutboundFromContext(ctx)
	if !ob.Target.IsValid() {
		return errors.New("target not specified")
	}
	dest := ob.Target

	tunnel, err := c.getTunnel(ctx, dialer)
	if err != nil {
		return err
	}

	newError("tunneling request to ", dest, " via ", c.serverAddr.NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	sessionPolicy := c.policyManager.ForLevel(c.config.Level)
	ctx, cancel := context.WithCancel(ctx)
	timer := signal.CancelAfterInactivity(ctx, cancel, sessionPolicy.Timeouts.ConnectionIdle)

	originalDest := dest
	if dest.Address.Family().IsDomain() {
		ips, err := dns.LookupIPWithOption(c.dns, dest.Address.Domain(), dns.IPOption{
			IPv4Enable: tunnel.tun.hasV4,
			IPv6Enable: tunnel.tun.hasV6,
		})
		if err != nil {
			return newError("failed to lookup DNS").Base(err)
		} else if len(ips) == 0 {
			return dns.ErrEmptyResponse
		}
		dest.Address = net.IPAddress(ips[0])
	}

	var requestFunc func() error
	var responseFunc func() error

	ip, _ := netip.AddrFromSlice(dest.Address.IP())
	addrPort := netip.AddrPortFrom(ip, dest.Port.Value())
	if ob.Target.Network == net.Network_TCP {
		conn, err := tunnel.tun.DialContextTCPAddrPort(ctx, addrPort)
		if err != nil {
			return err
		}
		defer conn.Close()
		requestFunc = func() error {
			defer timer.SetTimeout(sessionPolicy.Timeouts.DownlinkOnly)
			return buf.Copy(link.Reader, buf.NewWriter(conn), buf.UpdateActivity(timer))
		}
		responseFunc = func() error {
			defer timer.SetTimeout(sessionPolicy.Timeouts.UplinkOnly)
			return buf.Copy(buf.NewReader(conn), link.Writer, buf.UpdateActivity(timer))
		}
	} else {
		conn, err := tunnel.tun.DialUDPAddrPort(netip.AddrPort{}, addrPort)
		if err != nil {
			return err
		}
		defer conn.Close()
		packetReaderWriter := newPacketReaderWriter(conn, originalDest, dest, c.dns, c.config.DomainStrategy, tunnel.tun.hasV4, tunnel.tun.hasV6)
		requestFunc = func() error {
			defer timer.SetTimeout(sessionPolicy.Timeouts.DownlinkOnly)
			return buf.Copy(link.Reader, packetReaderWriter, buf.UpdateActivity(timer))
		}
		responseFunc = func() error {
			defer timer.SetTimeout(sessionPolicy.Timeouts.UplinkOnly)
			return buf.Copy(packetReaderWriter, link.Writer, buf.UpdateActivity(timer))
		}
	}

	responseDonePost := task.OnSuccess(responseFunc, task.Close(link.Writer))
	if err := task.Run(ctx, requestFunc, responseDonePost); err != nil {
		return newError("connection ends").Base(err)
	}

	return nil
}

func (c *Client) getTunnel(ctx context.Context, dialer internet.Dialer) (*tunnel, error) {
	c.createLock.Lock()
	defer c.createLock.Unlock()
	if tunnel := c.tunnel.Load(); tunnel != nil {
		select {
		case <-tunnel.closed:
		default:
			return tunnel, nil
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	c.ctxLock.Lock()
	clientCtx := c.ctx
	c.ctxLock.Unlock()
	stop := context.AfterFunc(clientCtx, cancel)
	defer stop()
	var (
		conn *connectip.Conn
		resp *http.Response
		err  error
	)
	if c.config.Http3 {
		conn, resp, err = c.dialH3(ctx, dialer) // nolint: bodyclose
	} else {
		conn, resp, err = c.dialH1H2(ctx, dialer) // nolint: bodyclose
	}
	if err != nil {
		return nil, err
	}
	connectipConn := &connectipConn{
		Conn: conn,
		resp: resp,
	}
	_, err = connectipConn.RequestAddresses([]netip.Prefix{
		netip.PrefixFrom(netip.IPv4Unspecified(), 32),
		netip.PrefixFrom(netip.IPv6Unspecified(), 128),
	})
	if err != nil {
		connectipConn.Close()
		return nil, err
	}
	assignedAddresses, err := connectipConn.ReceiveAddressAssignment(ctx)
	if err != nil {
		connectipConn.Close()
		return nil, newError("no address assigned").Base(err)
	}
	var localAddresses []netip.Addr
	for _, addresses := range assignedAddresses {
		if addresses.Rejected() {
			continue
		}
		addr := addresses.IPPrefix.Addr()
		if addresses.IPPrefix.Bits() != addr.BitLen() {
			addr = addresses.IPPrefix.Masked().Addr().Next()
		}
		localAddresses = append(localAddresses, addr)
	}
	slices.SortStableFunc(localAddresses, func(addr1, addr2 netip.Addr) int {
		return addr1.Compare(addr2)
	})
	localAddresses = slices.Compact(localAddresses)
	if len(localAddresses) == 0 {
		connectipConn.Close()
		return nil, newError("no address assigned")
	}
	newError("assigned addresses: ", localAddresses).AtDebug().WriteToLog(session.ExportIDToError(ctx))
	tunnel, err := createTunnel(connectipConn, localAddresses, c.mtu)
	if err != nil {
		conn.Close()
		return nil, err
	}
	c.tunnel.Store(tunnel)
	go tunnel.serveAddressRequests(clientCtx, connectipConn)
	go c.serveAddressAssignments(clientCtx, connectipConn, tunnel, localAddresses)
	return tunnel, nil
}

func (c *Client) dialH1H2(ctx context.Context, dialer internet.Dialer) (*connectip.Conn, *http.Response, error) {
	handler, ok := dialer.(*outbound.Handler)
	if !ok {
		panic("dialer is not *outbound.Handler")
	}
	if handler.MuxEnabled() {
		return nil, nil, newError("mux enabled")
	}
	if handler.TransportLayerEnabled() {
		return nil, nil, newError("transport layer enabled")
	}
	var (
		h2ClientConn *http2ClientConn
		rawConn      net.Conn
		err          error
		isTLS        bool
	)
	cacheH2ClientConn := c.h2ClientConn.Load()
	if cacheH2ClientConn != nil && cacheH2ClientConn.canTakeNewRequest() {
		h2ClientConn = cacheH2ClientConn
	} else {
		if cacheH2ClientConn != nil && !cacheH2ClientConn.canTakeNewRequest() {
			cacheH2ClientConn.close()
		}
		rawConn, err = dialer.Dial(ctx, c.serverAddr)
		if err != nil {
			return nil, nil, err
		}
		iConn := rawConn
		if statConn, ok := iConn.(*internet.StatCouterConnection); ok {
			iConn = statConn.Connection
		}
		nextProto := ""
		if connALPNGetter, ok := iConn.(security.ConnectionApplicationProtocol); ok {
			isTLS = true
			nextProto, err = connALPNGetter.GetConnectionApplicationProtocol(ctx)
			if err != nil {
				rawConn.Close()
				return nil, nil, err
			}
		}
		switch nextProto {
		case "h2":
			cc, err := c.h2Transport.NewClientConn(rawConn)
			if err != nil {
				rawConn.Close()
				return nil, nil, err
			}
			h2ClientConn = newH2ClientConn(cc, rawConn)
			c.h2ClientConn.Store(h2ClientConn)
		case "http/1.1", "":
		default:
			rawConn.Close()
			return nil, nil, newError("invalid alpn")
		}
	}
	var req *connectip.Request
	if h2ClientConn != nil {
		req, err = connectip.NewHTTP2Request(ctx, c.reqURL)
	} else {
		var reqURL *url.URL
		if !isTLS && len(c.config.UriTemplate) == 0 {
			reqURL = new(url.URL)
			*reqURL = *c.reqURL
			reqURL.Scheme = "http"
		} else {
			reqURL = c.reqURL
		}
		req, err = connectip.NewHTTP1Request(ctx, reqURL)
	}
	if err != nil {
		return nil, nil, err
	}
	if c.config.Username != nil || c.config.Password != nil {
		req.Header().Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.config.GetUsername()+":"+c.config.GetPassword())))
	}
	for key, value := range c.config.GetHeaders() {
		req.Header().Set(key, value)
	}
	if h2ClientConn != nil {
		conn, resp, err := connectip.NewHTTP2ClientConn(h2ClientConn).Dial(req) // nolint: bodyclose
		if err != nil {
			h2ClientConn.close()
			return nil, nil, err
		}
		return conn, resp, nil
	} else {
		conn, resp, err := connectip.NewHTTP1ClientConn(rawConn).Dial(req) // nolint: bodyclose
		if err != nil {
			rawConn.Close()
			return nil, nil, err
		}
		return conn, resp, nil
	}
}

func (c *Client) dialH3(ctx context.Context, dialer internet.Dialer) (*connectip.Conn, *http.Response, error) {
	var (
		h3ClientConn *http3ClientConn
		rawConn      net.Conn
		err          error
	)
	cacheH3ClientConn := c.h3ClientConn.Load()
	canTakeNewRequest := true
	if cacheH3ClientConn != nil {
		select {
		case <-cacheH3ClientConn.Context().Done():
			canTakeNewRequest = false
		default:
		}
	}
	if cacheH3ClientConn != nil && canTakeNewRequest {
		h3ClientConn = cacheH3ClientConn
	} else {
		if cacheH3ClientConn != nil && !canTakeNewRequest {
			cacheH3ClientConn.close()
		}
		handler, ok := dialer.(*outbound.Handler)
		if !ok {
			panic("dialer is not *outbound.Handler")
		}
		if handler.MuxEnabled() {
			return nil, nil, newError("mux enabled")
		}
		if handler.TransportLayerEnabled() {
			return nil, nil, newError("transport layer enabled")
		}
		streamSettings := handler.StreamSettings()
		if streamSettings == nil || streamSettings.SecurityType != "exclave.core.transport.internet.tls.Config" {
			return nil, nil, newError("tls not enabled")
		}
		tlsSettings, ok := streamSettings.SecuritySettings.(*v2tls.Config)
		if !ok {
			return nil, nil, newError("tls not enabled")
		}
		tlsCfg, err := tlsSettings.GetTLSConfigWithContext(ctx, v2tls.WithNextProto("h3"), v2tls.WithDestination(c.serverAddr))
		if err != nil {
			return nil, nil, err
		}
		rawConn, err = dialer.Dial(ctx, c.serverAddr)
		if err != nil {
			return nil, nil, err
		}
		quicConn, err := quic.Dial(ctx, newQUICPacketConn(rawConn), rawConn.RemoteAddr(), tlsCfg, &quic.Config{
			EnableDatagrams: true,
			KeepAlivePeriod: time.Second * 15,
		})
		if err != nil {
			rawConn.Close()
			return nil, nil, err
		}
		h3ClientConn = &http3ClientConn{
			ClientConn: c.h3Transport.NewClientConn(quicConn),
			rawConn:    rawConn,
		}
		c.h3ClientConn.Store(h3ClientConn)
	}
	req, err := connectip.NewRequest(ctx, c.reqURL)
	if err != nil {
		return nil, nil, err
	}
	if c.config.Username != nil || c.config.Password != nil {
		req.Header().Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.config.GetUsername()+":"+c.config.GetPassword())))
	}
	for key, value := range c.config.GetHeaders() {
		req.Header().Set(key, value)
	}
	conn, resp, err := connectip.NewClientConn(h3ClientConn.ClientConn).Dial(req) // nolint: bodyclose
	if err != nil {
		h3ClientConn.close()
		return nil, nil, err
	}
	return conn, resp, nil
}

type tunnel struct {
	conn      *connectipConn
	mtu       int32
	dev       tun.Device
	tun       *netTun
	closeOnce sync.Once
	closed    chan struct{}
}

func createTunnel(conn *connectipConn, localAddresses []netip.Addr, mtu int32) (*tunnel, error) {
	dev, err := createNetTUN(localAddresses, int(mtu))
	if err != nil {
		return nil, err
	}
	tunnel := &tunnel{
		conn:   conn,
		mtu:    mtu,
		dev:    dev,
		tun:    dev.(*netTun),
		closed: make(chan struct{}),
	}
	go tunnel.readLoop()
	go tunnel.writeLoop()
	return tunnel, nil
}

func (t *tunnel) readLoop() {
	bufferSize := max(t.mtu, buf.Size)
	b := buf.NewWithSize(bufferSize)
	for {
		b.Clear()
		b.Resize(0, bufferSize)
		n, err := t.conn.ReadPacket(b.Bytes())
		if err != nil {
			break
		}
		b.Resize(0, int32(n))
		_, err = t.dev.Write([][]byte{b.Bytes()}, 0)
		if err != nil {
			break
		}
	}
	b.Release()
	t.close()
}

func (t *tunnel) writeLoop() {
	bufferSize := max(t.mtu, buf.Size)
	b := buf.NewWithSize(bufferSize)
	b.Resize(0, bufferSize)
	bufs := [][]byte{b.Bytes()}
	sizes := []int{0}
	for {
		b.Clear()
		b.Resize(0, bufferSize)
		_, err := t.dev.Read(bufs, sizes, 0)
		if err != nil {
			break
		}
		b.Resize(0, int32(sizes[0]))
		icmp, err := t.conn.WritePacket(b.Bytes())
		if err != nil {
			break
		}
		if len(icmp) > 0 {
			go t.dev.Write([][]byte{icmp}, 0)
		}
	}
	b.Release()
	t.close()
}

func (c *Client) serveAddressAssignments(ctx context.Context, conn *connectipConn, tunnel *tunnel, localAddresses []netip.Addr) {
	for {
		assignedAddresses, err := conn.ReceiveAddressAssignment(ctx)
		if err != nil {
			break
		}
		var newLocalAddresses []netip.Addr
		for _, addresses := range assignedAddresses {
			if addresses.Rejected() {
				continue
			}
			addr := addresses.IPPrefix.Addr()
			if addresses.IPPrefix.Bits() != addr.BitLen() {
				addr = addresses.IPPrefix.Masked().Addr().Next()
			}
			newLocalAddresses = append(newLocalAddresses, addr)
		}
		slices.SortStableFunc(newLocalAddresses, func(addr1, addr2 netip.Addr) int {
			return addr1.Compare(addr2)
		})
		newLocalAddresses = slices.Compact(newLocalAddresses)
		if len(newLocalAddresses) == 0 {
			newError("no address assigned").AtError().WriteToLog(session.ExportIDToError(ctx))
			break
		}
		if !slices.Equal(localAddresses, newLocalAddresses) {
			newError("assigned addresses: ", newLocalAddresses).AtDebug().WriteToLog(session.ExportIDToError(ctx))
			newTunnel, err := createTunnel(conn, newLocalAddresses, c.mtu)
			if err != nil {
				break
			}
			c.tunnel.Store(newTunnel)
			break
		}
	}
	tunnel.close()
}

func (t *tunnel) serveAddressRequests(ctx context.Context, conn *connectipConn) {
	for {
		addressRequest, err := conn.ReceiveAddressRequest(ctx)
		if err != nil {
			break
		}
		err = addressRequest.Respond(make([]netip.Prefix, len(addressRequest.Prefixes)), nil)
		if err != nil {
			break
		}
	}
	t.close()
}

func (t *tunnel) close() {
	t.closeOnce.Do(func() {
		close(t.closed)
		t.conn.Close()
		go t.dev.Close()
	})
}

func init() {
	common.Must(common.RegisterConfig((*ClientConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewClient(ctx, config.(*ClientConfig))
	}))
}
