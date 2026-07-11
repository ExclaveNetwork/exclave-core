package snell

import (
	"context"
	"net"
	"sync"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/snellv4"
	"github.com/sagernet/sing-snell/snellv6"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"

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

var (
	_ snellClient = (*snellv4.Client)(nil)
	_ snellClient = (*snellv6.Client)(nil)
)

type snellClient interface {
	DialContext(ctx context.Context, destination metadata.Socksaddr) (net.Conn, error)
	DialPacketConn(conn net.Conn) (network.NetPacketConn, error)
	Close() error
}

type Outbound struct {
	serverAddr   v2net.Destination
	psk          []byte
	userKey      []byte
	obfsMode     snell.ObfsMode
	obfsHost     string
	version      uint32
	reuse        bool
	mode         snellv6.Mode
	client       snellClient
	clientAccess sync.Mutex
}

func NewClient(ctx context.Context, config *ClientConfig) (*Outbound, error) {
	if config.Address == nil {
		return nil, newError("missing server address")
	}
	outbound := &Outbound{
		serverAddr: v2net.Destination{
			Address: config.Address.AsAddress(),
			Port:    v2net.Port(config.Port),
			Network: v2net.Network_TCP,
		},
		psk:     []byte(config.Psk),
		userKey: []byte(config.UserKey),
		version: config.Version,
		reuse:   config.Reuse,
	}

	if outbound.version != 4 && outbound.version != 6 {
		return nil, newError("unsupported snell version: ", outbound.version)
	}

	if outbound.version == 4 {
		switch config.ObfsMode {
		case "", "none":
			outbound.obfsMode = snell.ObfsModeNone
			if config.ObfsHost != "" {
				return nil, newError(`invalid obfsHost for obfsMode "none"`)
			}
		case "http":
			outbound.obfsMode = snell.ObfsModeHTTP
			outbound.obfsHost = config.ObfsHost
		case "tls":
			outbound.obfsMode = snell.ObfsModeTLS
			outbound.obfsHost = config.ObfsHost
		default:
			return nil, newError("invalid snell obfsMode: ", config.ObfsMode)
		}
		if config.Mode != "" {
			return nil, newError("mode is only valid for version 6")
		}
	}

	if outbound.version == 6 {
		switch config.Mode {
		case "", "default":
			outbound.mode = snellv6.ModeDefault
		case "unshaped":
			outbound.mode = snellv6.ModeUnshaped
		case "unsafe-raw":
			outbound.mode = snellv6.ModeUnsafeRaw
		default:
			return nil, newError("invalid snell mode: ", config.Mode)
		}
		if config.ObfsHost != "" {
			return nil, newError("obfsHost is only valid for version 4")
		}
		if config.ObfsMode != "" {
			return nil, newError("obfsMode is only valid for version 4")
		}
	}

	return outbound, nil
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
		return nil, newError("tls enabled")
	}

	var client snellClient
	var err error
	if o.version == 6 {
		client, err = snellv6.NewClient(snellv6.ClientOptions{
			PSK:     o.psk,
			UserKey: o.userKey,
			Mode:    o.mode,
			Reuse:   o.reuse,
			Dialer:  singbridge.NewDialerWrapper(dialer),
			Server:  metadata.ParseSocksaddr(o.serverAddr.NetAddr()),
		})
	} else {
		client, err = snellv4.NewClient(snellv4.ClientOptions{
			PSK:      o.psk,
			UserKey:  o.userKey,
			Reuse:    o.reuse,
			ObfsMode: o.obfsMode,
			ObfsHost: o.obfsHost,
			Dialer:   singbridge.NewDialerWrapper(dialer),
			Server:   metadata.ParseSocksaddr(o.serverAddr.NetAddr()),
		})
	}
	if err != nil {
		return nil, err
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

	newError("tunneling request to ", destination, " via ", o.serverAddr.NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	detachedCtx := core.ToBackgroundDetachedContext(ctx)

	if destination.Network == v2net.Network_TCP {
		serverConn, err := client.DialContext(detachedCtx, singbridge.ToSocksAddr(destination))
		if err != nil {
			return err
		}
		return singbridge.ReturnError(bufio.CopyConn(detachedCtx, singbridge.NewPipeConnWrapper(link), serverConn))
	} else {
		rawConn, err := dialer.Dial(detachedCtx, o.serverAddr)
		if err != nil {
			return err
		}
		serverConn, err := client.DialPacketConn(rawConn)
		if err != nil {
			rawConn.Close()
			return err
		}
		return singbridge.ReturnError(bufio.CopyPacketConn(detachedCtx, singbridge.NewPacketConnWrapper(link, destination), serverConn))
	}
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
