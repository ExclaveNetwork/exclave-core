package snell

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/snellv4"
	"github.com/sagernet/sing-snell/snellv6"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/app/proxyman/outbound"
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/bytespool"
	v2net "github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/common/singbridge"
	"github.com/exclavenetwork/exclave-core/v5/proxy"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

func init() {
	common.Must(common.RegisterConfig((*ClientConfig)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewClient(ctx, config.(*ClientConfig))
	}))
}

// snellClient is the subset used by the outbound for v4 and v6 clients.
type snellClient interface {
	DialContext(ctx context.Context, destination M.Socksaddr) (net.Conn, error)
	ListenPacket(ctx context.Context) (N.NetPacketConn, error)
	Close() error
}

// Outbound implements Snell v3–v6 client outbound (via SagerNet sing-snell).
type Outbound struct {
	serverAddr   v2net.Destination
	psk          string
	obfs         string
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
	if config.Psk == "" {
		return nil, newError("missing psk")
	}
	version := int(config.Version)
	if version == 0 {
		version = 4
	}
	if version < 3 || version > 6 {
		return nil, newError("unsupported snell version ", version, " (want 3-6)")
	}
	obfs := strings.ToLower(config.Obfs)
	switch obfs {
	case "", "off", "none":
		obfs = "none"
	case "http", "tls":
	default:
		return nil, newError("invalid snell obfs ", config.Obfs)
	}
	return &Outbound{
		serverAddr: v2net.Destination{
			Address: config.Address.AsAddress(),
			Port:    v2net.Port(config.Port),
			Network: v2net.Network_TCP,
		},
		psk:      config.Psk,
		obfs:     obfs,
		obfsHost: config.ObfsHost,
		version:  version,
		reuse:    config.Reuse,
		mode:     config.Mode,
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
		return nil, newError("tls/security enabled on streamSettings; snell uses built-in crypto (use obfs=tls for fake TLS)")
	}

	obfsMode, err := snell.ParseObfsMode(o.obfs)
	if err != nil {
		return nil, err
	}
	obfsHost := o.obfsHost
	if obfsHost == "" {
		switch obfsMode {
		case snell.ObfsModeTLS:
			obfsHost = snell.DefaultTLSObfsHost
		default:
			obfsHost = snell.DefaultObfsHost
		}
	}
	obfsCfg := snell.ObfsConfig{Mode: obfsMode, Host: obfsHost}
	sdialer := singbridge.NewDialerWrapper(dialer)
	server := M.ParseSocksaddr(o.serverAddr.NetAddr())
	psk := []byte(o.psk)

	var client snellClient
	if o.version == 6 {
		mode, err := snellv6.ParseMode(o.mode)
		if err != nil {
			return nil, err
		}
		client, err = snellv6.NewClient(snellv6.ClientOptions{
			Dialer: sdialer,
			Server: server,
			PSK:    psk,
			Obfs:   obfsCfg,
			Mode:   mode,
			Reuse:  o.reuse,
		})
		if err != nil {
			return nil, err
		}
	} else {
		client, err = snellv4.NewClient(snellv4.ClientOptions{
			Dialer:  sdialer,
			Server:  server,
			PSK:     psk,
			Version: o.version,
			Obfs:    obfsCfg,
			Reuse:   o.reuse,
		})
		if err != nil {
			return nil, err
		}
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
	target := singbridge.ToSocksaddr(destination)

	if destination.Network == v2net.Network_TCP {
		serverConn, err := client.DialContext(detachedCtx, target)
		if err != nil {
			return err
		}

		var firstPayload []byte
		if reader, ok := link.Reader.(buf.TimeoutReader); ok {
			if mb, _ := reader.ReadMultiBufferTimeout(proxy.FirstPayloadTimeout); mb != nil {
				length := mb.Len()
				firstPayload = bytespool.Alloc(length)
				mb, _ = buf.SplitBytes(mb, firstPayload)
				firstPayload = firstPayload[:length]
				buf.ReleaseMulti(mb)
			}
		}
		if len(firstPayload) > 0 {
			_, err = serverConn.Write(firstPayload)
			bytespool.Free(firstPayload)
			if err != nil {
				_ = serverConn.Close()
				return singbridge.ReturnError(err)
			}
		}

		return singbridge.ReturnError(bufio.CopyConn(detachedCtx, singbridge.NewPipeConnWrapper(link), serverConn))
	}

	pc, err := client.ListenPacket(detachedCtx)
	if err != nil {
		return err
	}
	return singbridge.ReturnError(bufio.CopyPacketConn(
		detachedCtx,
		singbridge.NewPacketConnWrapper(link, destination),
		bufio.NewPacketConn(pc),
	))
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
