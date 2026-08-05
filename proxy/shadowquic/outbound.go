package shadowquic

import (
	"context"
	"sync"

	shadowquic "github.com/exclavenetwork/sing-shadowquic"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/network"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/app/proxyman/outbound"
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/bytespool"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
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

var (
	_ proxy.Outbound                    = (*Outbound)(nil)
	_ proxy.ClosableOutbound            = (*Outbound)(nil)
	_ proxy.OutboundWithInterfaceUpdate = (*Outbound)(nil)
)

type Outbound struct {
	serverAddr   net.Destination
	options      shadowquic.ClientOptions
	client       *shadowquic.Client
	clientAccess sync.Mutex
}

func NewClient(ctx context.Context, config *ClientConfig) (*Outbound, error) {
	o := &Outbound{
		serverAddr: net.Destination{
			Address: config.Address.AsAddress(),
			Port:    net.Port(config.Port),
			Network: net.Network_UDP,
		},
	}
	switch config.CongestionControl {
	case "", "bbr", "new_reno", "cubic":
	default:
		return nil, newError("invalid congestion control: ", config.CongestionControl)
	}
	o.options = shadowquic.ClientOptions{
		Context:           ctx,
		ServerAddress:     singbridge.ToSocksAddr(o.serverAddr),
		Username:          config.Username,
		Password:          config.Password,
		CongestionControl: config.CongestionControl,
		UDPOverStream:     config.UdpOverStream,
		ZeroRTTHandshake:  config.ZeroRttHandshake,
		ServerName:        config.ServerName,
	}
	if len(config.ServerName) == 0 {
		switch o.serverAddr.Address.Family() {
		case net.AddressFamilyDomain:
			o.options.ServerName = o.serverAddr.Address.Domain()
		default:
			o.options.ServerName = o.serverAddr.Address.IP().String()
		}
	} else {
		o.options.ServerName = config.ServerName
	}
	if len(config.Alpn) > 0 {
		o.options.NextProtos = config.Alpn
	}
	return o, nil
}

func (o *Outbound) getClient(dialer internet.Dialer) (*shadowquic.Client, error) {
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
	options := o.options
	options.Dialer = singbridge.NewDialerWrapper(dialer)
	client, err := shadowquic.NewClient(options)
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

	outbound := session.OutboundFromContext(ctx)
	if outbound == nil || !outbound.Target.IsValid() {
		return newError("target not specified")
	}
	destination := outbound.Target

	newError("tunneling request to ", destination, " via ", o.serverAddr.NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	detachedCtx := core.ToBackgroundDetachedContext(ctx)
	if destination.Network == net.Network_TCP {
		serverConn, err := client.DialConn(detachedCtx, singbridge.ToSocksAddr(destination))
		if err != nil {
			return err
		}
		// for server-speaks-first protocols
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
		_, err = serverConn.Write(firstPayload)
		if firstPayload != nil {
			bytespool.Free(firstPayload)
		}
		if err != nil {
			return singbridge.ReturnError(err)
		}
		return singbridge.ReturnError(bufio.CopyConn(detachedCtx, singbridge.NewPipeConnWrapper(link), serverConn))
	} else {
		serverConn, err := client.ListenPacket(detachedCtx)
		if err != nil {
			return err
		}
		return singbridge.ReturnError(bufio.CopyPacketConn(detachedCtx, singbridge.NewPacketConnWrapper(link, destination), serverConn.(network.PacketConn)))
	}
}

func (o *Outbound) InterfaceUpdate() {
	_ = o.Close()
}

func (o *Outbound) Close() error {
	o.clientAccess.Lock()
	if o.client != nil {
		o.client.Close()
	}
	o.clientAccess.Unlock()
	return nil
}
