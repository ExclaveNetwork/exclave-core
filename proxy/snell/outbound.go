package snell

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/snellv4"
	"github.com/sagernet/sing-snell/snellv6"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/app/proxyman/outbound"
	"github.com/exclavenetwork/exclave-core/v5/common"
	v2net "github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/common/singbridge"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

func init() {
	common.Must(common.RegisterConfig((*ClientConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewClient(ctx, config.(*ClientConfig))
	}))
}

// snellClient is the subset of SagerNet sing-snell clients used here.
type snellClient interface {
	DialContext(ctx context.Context, destination M.Socksaddr) (net.Conn, error)
	DialPacketConn(conn net.Conn) (N.NetPacketConn, error)
	Close() error
}

// Outbound is a Snell v4 / v6 client outbound.
type Outbound struct {
	serverAddr   v2net.Destination
	psk          string
	userPSK      string
	obfsMode     string
	obfsHost     string
	version      int
	reuse        bool
	mode         string
	client       snellClient
	clientAccess sync.Mutex
}

func NewClient(ctx context.Context, config *ClientConfig) (*Outbound, error) {
	if config.Address == nil {
		return nil, newError("missing server address")
	}
	version := int(config.Version)
	if version == 0 {
		version = 4
	}
	if version != 4 && version != 6 {
		return nil, newError("unsupported snell version ", version, " (want 4 or 6)")
	}

	obfsMode := config.ObfsMode
	if obfsMode == "" {
		obfsMode = "none"
	}
	switch obfsMode {
	case "none", "http", "tls":
	default:
		return nil, newError(`invalid snell obfsMode `, config.ObfsMode, ` (want "none", "http" or "tls")`)
	}

	mode := config.Mode
	if version == 6 {
		if mode == "" {
			mode = "default"
		}
		switch mode {
		case "default", "unshaped", "unsafe-raw":
		default:
			return nil, newError(`invalid snell mode `, config.Mode)
		}
		if config.ObfsHost != "" {
			return nil, newError("obfsHost is only valid for version 4")
		}
		if config.ObfsMode != "" && config.ObfsMode != "none" {
			return nil, newError("obfsMode is only valid for version 4")
		}
		obfsMode = "none"
	} else if config.Mode != "" {
		return nil, newError("mode is only valid for version 6")
	}

	// Empty PSK is allowed: Snell derives keys via Argon2id and servers may accept empty PSK.

	return &Outbound{
		serverAddr: v2net.Destination{
			Address: config.Address.AsAddress(),
			Port:    v2net.Port(config.Port),
			Network: v2net.Network_TCP,
		},
		psk:      config.Psk,
		userPSK:  config.UserPsk,
		obfsMode: obfsMode,
		obfsHost: config.ObfsHost,
		version:  version,
		reuse:    config.Reuse,
		mode:     mode,
	}, nil
}

func (o *Outbound) getClient(dialer internet.Dialer) (snellClient, error) {
	o.clientAccess.Lock()
	defer o.clientAccess.Unlock()
	if o.client != nil {
		return o.client, nil
	}
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
	if streamSettings := handler.StreamSettings(); streamSettings != nil && streamSettings.SecurityType != "" {
		return nil, newError("tls/security enabled on streamSettings; snell uses built-in crypto (use obfsMode=tls for fake TLS on v4)")
	}

	sdialer := singbridge.NewDialerWrapper(dialer)
	server := M.ParseSocksaddr(o.serverAddr.NetAddr())
	psk := []byte(o.psk)
	userKey := []byte(o.userPSK)

	var client snellClient
	if o.version == 6 {
		mode, err := snellv6.ParseMode(o.mode)
		if err != nil {
			return nil, err
		}
		c, err := snellv6.NewClient(snellv6.ClientOptions{
			PSK:     psk,
			UserKey: userKey,
			Mode:    mode,
			Reuse:   o.reuse,
			Dialer:  sdialer,
			Server:  server,
		})
		if err != nil {
			return nil, err
		}
		client = c
	} else {
		obfs, err := snell.ParseObfsMode(o.obfsMode)
		if err != nil {
			return nil, err
		}
		c, err := snellv4.NewClient(snellv4.ClientOptions{
			PSK:      psk,
			UserKey:  userKey,
			Reuse:    o.reuse,
			ObfsMode: obfs,
			ObfsHost: o.obfsHost,
			Dialer:   sdialer,
			Server:   server,
		})
		if err != nil {
			return nil, err
		}
		client = c
	}
	o.client = client
	return client, nil
}

func (o *Outbound) Process(ctx context.Context, link *transport.Link, dialer internet.Dialer) error {
	client, err := o.getClient(dialer)
	if err != nil {
		return err
	}

	ob := session.OutboundFromContext(ctx)
	if ob == nil || !ob.Target.IsValid() {
		return newError("target not specified")
	}
	destination := ob.Target

	newError("tunneling request to ", destination, " via snell://", o.serverAddr.NetAddr(),
		" (v", o.version, ")").WriteToLog(session.ExportIDToError(ctx))

	detachedCtx := core.ToBackgroundDetachedContext(ctx)

	if destination.Network == v2net.Network_TCP {
		// Snell is client-speaks-first (CONNECT header before any application payload).
		// No server-speaks-first first-payload handling is required.
		serverConn, err := client.DialContext(detachedCtx, singbridge.ToSocksAddr(destination))
		if err != nil {
			return err
		}
		return singbridge.ReturnError(bufio.CopyConn(detachedCtx, singbridge.NewPipeConnWrapper(link), serverConn))
	}

	// UDP-over-TCP.
	// Snell UDP framing only carries IPv4/IPv6 (no domain ATYP). Resolve domains before write.
	raw, err := dialer.Dial(detachedCtx, o.serverAddr)
	if err != nil {
		return err
	}
	pc, err := client.DialPacketConn(raw)
	if err != nil {
		_ = raw.Close()
		return err
	}

	var resolve domainResolveFunc
	if ob.Resolver != nil {
		resolve = func(ctx context.Context, domain string) (net.IP, error) {
			addr := ob.Resolver(ctx, domain)
			if !addr.Family().IsIP() {
				return nil, newError("resolver returned non-IP for ", domain)
			}
			return addr.IP(), nil
		}
	} else {
		resolve = func(ctx context.Context, domain string) (net.IP, error) {
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", domain)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, newError("no IP for domain ", domain)
			}
			return ips[0], nil
		}
	}

	return singbridge.ReturnError(bufio.CopyPacketConn(
		detachedCtx,
		singbridge.NewPacketConnWrapper(link, destination),
		&udpIPOnlyPacketConn{NetPacketConn: pc, ctx: detachedCtx, resolve: resolve},
	))
}

type domainResolveFunc func(ctx context.Context, domain string) (net.IP, error)

// udpIPOnlyPacketConn resolves domain destinations to IP for Snell UDP writes.
// Read path is unchanged: servers reply with IP addresses only.
type udpIPOnlyPacketConn struct {
	N.NetPacketConn
	ctx     context.Context
	resolve domainResolveFunc
}

func (c *udpIPOnlyPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	// Snell UDP address encoding only supports IPv4/IPv6, not domain names.
	if destination.IsDomain() {
		ip, err := c.resolve(c.ctx, destination.Fqdn)
		if err != nil {
			buffer.Release()
			return err
		}
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			buffer.Release()
			return newError("invalid IP for domain ", destination.Fqdn)
		}
		destination = M.SocksaddrFrom(addr, destination.Port)
	}
	return c.NetPacketConn.WritePacket(buffer, destination)
}

func (o *Outbound) InterfaceUpdate() {
	_ = o.Close()
}

func (o *Outbound) Close() error {
	o.clientAccess.Lock()
	if o.client != nil {
		_ = o.client.Close()
		o.client = nil
	}
	o.clientAccess.Unlock()
	return nil
}
