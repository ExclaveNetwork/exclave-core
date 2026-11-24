package mieru

import (
	"context"
	"sync"

	mieruclient "github.com/enfein/mieru/v3/apis/client"
	mierucommon "github.com/enfein/mieru/v3/apis/common"
	mierumodel "github.com/enfein/mieru/v3/apis/model"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/app/proxyman/outbound"
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/common/signal"
	"github.com/exclavenetwork/exclave-core/v5/common/task"
	"github.com/exclavenetwork/exclave-core/v5/features/dns/localdns"
	"github.com/exclavenetwork/exclave-core/v5/features/policy"
	"github.com/exclavenetwork/exclave-core/v5/proxy"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/udp"
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
	serverAddr    net.Destination
	config        *ClientConfig
	policyManager policy.Manager
	client        mieruclient.Client
	clientAccess  sync.Mutex
}

func NewClient(ctx context.Context, config *ClientConfig) (*Outbound, error) {
	serverAddr := net.Destination{
		Address: config.Address.AsAddress(),
		Port:    net.Port(config.Port),
	}
	if len(config.PortRange) > 0 {
		serverAddr.Port = 0
	}
	switch config.Protocol {
	case "", "tcp":
		serverAddr.Network = net.Network_TCP
	case "udp":
		serverAddr.Network = net.Network_UDP
	default:
		return nil, newError("unknown protocol")
	}
	v := core.MustFromContext(ctx)
	return &Outbound{
		serverAddr:    serverAddr,
		config:        config,
		policyManager: v.GetFeature(policy.ManagerType()).(policy.Manager),
	}, nil
}

func (o *Outbound) getClient(dialer internet.Dialer, resolver func(ctx context.Context, domain string) net.Address) (mieruclient.Client, error) {
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
	mieruDialer := &dialerWrapper{
		dialer: dialer,
	}
	var mieruResolver mierucommon.DNSResolver
	if resolver != nil {
		mieruResolver = &resolverWrapper{
			resolver: resolver,
		}
	} else {
		mieruResolver = &localResolverWrapper{
			resolver: localdns.New(),
		}
	}
	config, err := buildMieruClientConfig(o.config, mieruDialer, mieruResolver)
	if err != nil {
		return nil, err
	}
	client := mieruclient.NewClient()
	if err = client.Store(config); err != nil {
		return nil, err
	}
	if err = client.Start(); err != nil {
		return nil, err
	}
	o.client = client
	return client, nil
}

func (o *Outbound) Process(ctx context.Context, link *transport.Link, dialer internet.Dialer) error {
	ob := session.OutboundFromContext(ctx)
	if ob == nil || !ob.Target.IsValid() {
		return newError("target not specified")
	}

	client, err := o.getClient(dialer, ob.Resolver)
	if err != nil {
		return err
	}

	destination := ob.Target

	newError("tunneling request to ", destination, " via ", o.serverAddr.NetAddr()).WriteToLog(session.ExportIDToError(ctx))

	addr := mierumodel.NetAddrSpec{}
	if destination.Network == net.Network_TCP {
		addr.Net = "tcp"
	} else {
		addr.Net = "udp"
	}
	switch destination.Address.Family() {
	case net.AddressFamilyDomain:
		addr.AddrSpec = mierumodel.AddrSpec{
			FQDN: destination.Address.Domain(),
			Port: int(destination.Port),
		}
	case net.AddressFamilyIPv4, net.AddressFamilyIPv6:
		addr.AddrSpec = mierumodel.AddrSpec{
			IP:   destination.Address.IP(),
			Port: int(destination.Port),
		}
	}

	sessionPolicy := o.policyManager.ForLevel(o.config.Level)
	ctx, cancel := context.WithCancel(ctx)
	timer := signal.CancelAfterInactivity(ctx, cancel, sessionPolicy.Timeouts.ConnectionIdle)

	conn, err := client.DialContext(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	var reader buf.Reader
	var writer buf.Writer
	if destination.Network == net.Network_TCP {
		reader = buf.NewReader(conn)
		writer = buf.NewWriter(conn)
	} else {
		packetConn := udp.NewMonoDestUDPConn(newUDPAssociateWrapper(conn), udp.NewMonoDestUDPAddr(destination.Address, destination.Port))
		reader = packetConn
		writer = packetConn
	}

	if err := task.Run(ctx, func() error {
		defer timer.SetTimeout(sessionPolicy.Timeouts.DownlinkOnly)
		return buf.Copy(link.Reader, writer, buf.UpdateActivity(timer))
	}, func() error {
		defer timer.SetTimeout(sessionPolicy.Timeouts.UplinkOnly)
		return buf.Copy(reader, link.Writer, buf.UpdateActivity(timer))
	}); err != nil {
		return newError("connection ends").Base(err)
	}
	return nil
}

func (o *Outbound) InterfaceUpdate() {
	_ = o.Close()
}

func (o *Outbound) Close() error {
	o.clientAccess.Lock()
	client := o.client
	o.client = nil
	o.clientAccess.Unlock()
	if client != nil {
		go client.Stop() // blocking call
	}
	return nil
}
