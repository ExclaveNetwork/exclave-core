package snell

import (
	"context"
	"net"
	"sync"

	snellclient "github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/snell"
	"github.com/sagernet/sing/common/bufio"

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

// Outbound implements a Snell v4/v5 client outbound.
type Outbound struct {
	serverAddr   v2net.Destination
	psk          string
	obfs         string
	version      int
	reuse        bool
	client       *snellclient.Client
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
	if version != 4 && version != 5 {
		return nil, newError("unsupported snell version ", version)
	}
	obfs := config.Obfs
	if obfs == "" {
		obfs = "off"
	}
	return &Outbound{
		serverAddr: v2net.Destination{
			Address: config.Address.AsAddress(),
			Port:    v2net.Port(config.Port),
			Network: v2net.Network_TCP,
		},
		psk:     config.Psk,
		obfs:    obfs,
		version: version,
		reuse:   config.Reuse,
	}, nil
}

func (o *Outbound) getClient(dialer internet.Dialer) (*snellclient.Client, error) {
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
		// Snell carries its own AEAD; stream TLS is not used (obfs=tls is a fake handshake).
		return nil, newError("tls/security enabled on streamSettings; snell uses built-in AEAD (use obfs=tls for fake TLS)")
	}

	client, err := snellclient.NewClient(snellclient.ClientOptions{
		Server:  o.serverAddr.NetAddr(),
		PSK:     o.psk,
		Obfs:    o.obfs,
		Version: o.version,
		Reuse:   o.reuse,
		Dialer: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(ctx, o.serverAddr)
		},
	})
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

	newError("tunneling request to ", destination, " via snell://", o.serverAddr.NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	detachedCtx := core.ToBackgroundDetachedContext(ctx)

	if destination.Network == v2net.Network_TCP {
		host := destinationAddressHost(destination)
		port := uint16(destination.Port)
		serverConn, err := client.DialTCP(detachedCtx, host, port)
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

	// UDP-over-TCP: wrap std net.PacketConn as sing network.PacketConn
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

func destinationAddressHost(d v2net.Destination) string {
	if d.Address.Family().IsDomain() {
		return d.Address.Domain()
	}
	return d.Address.IP().String()
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
