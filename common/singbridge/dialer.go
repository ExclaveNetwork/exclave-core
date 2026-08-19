package singbridge

import (
	"context"

	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/net/cnc"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/features/stats"
	"github.com/exclavenetwork/exclave-core/v5/proxy"
	"github.com/exclavenetwork/exclave-core/v5/transport"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
	"github.com/exclavenetwork/exclave-core/v5/transport/pipe"
)

var (
	_ network.Dialer = (*dialerWrapper)(nil)
	_ network.Dialer = (*outboundDialerWrapper)(nil)
	_ net.PacketConn = (*statCounterPacketConn)(nil)
)

type dialerWrapper struct {
	dialer internet.Dialer
	quic   bool
}

func NewDialerWrapper(dialer internet.Dialer) *dialerWrapper {
	return &dialerWrapper{
		dialer: dialer,
	}
}

func (d *dialerWrapper) DialContext(ctx context.Context, network string, destination metadata.Socksaddr) (net.Conn, error) {
	dest := ToDestination(destination, ToNetwork(network))
	conn, err := d.dialer.Dial(ctx, dest)
	if err != nil {
		return nil, err
	}
	if dest.Network == net.Network_TCP {
		return conn, nil
	}
	if !d.quic {
		return conn, nil
	}
	return newQUICConnectPacketConn(conn), nil
}

func (d *dialerWrapper) ListenPacket(ctx context.Context, destination metadata.Socksaddr) (net.PacketConn, error) {
	conn, err := d.dialer.Dial(ctx, ToDestination(destination, net.Network_UDP))
	if err != nil {
		return nil, err
	}
	return newBindPacketConn(conn), nil
}

func NewOutboundDialerWrapper(outbound proxy.Outbound, dialer internet.Dialer) *outboundDialerWrapper {
	return &outboundDialerWrapper{outbound, dialer}
}

type outboundDialerWrapper struct {
	outbound proxy.Outbound
	dialer   internet.Dialer
}

func (d *outboundDialerWrapper) DialContext(ctx context.Context, network string, destination metadata.Socksaddr) (net.Conn, error) {
	ctx = session.ContextWithOutbound(ctx, &session.Outbound{
		Target: ToDestination(destination, ToNetwork(network)),
	})
	opts := []pipe.Option{pipe.WithSizeLimit(64 * 1024)}
	uplinkReader, uplinkWriter := pipe.New(opts...)
	downlinkReader, downlinkWriter := pipe.New(opts...)
	conn := cnc.NewConnection(cnc.ConnectionInputMulti(downlinkWriter), cnc.ConnectionOutputMulti(uplinkReader))
	go d.outbound.Process(core.ToBackgroundDetachedContext(ctx), &transport.Link{Reader: downlinkReader, Writer: uplinkWriter}, d.dialer)
	return conn, nil
}

func (d *outboundDialerWrapper) ListenPacket(ctx context.Context, destination metadata.Socksaddr) (net.PacketConn, error) {
	conn, err := d.DialContext(ctx, network.NetworkUDP, destination)
	if err != nil {
		return nil, err
	}
	return internet.NewConnWrapper(conn), nil
}

func newBindPacketConn(conn net.Conn) net.PacketConn {
	var readCounter, writeCounter stats.Counter
	iConn := conn
	if statConn, ok := iConn.(*internet.StatCouterConnection); ok {
		iConn = statConn.Connection
		readCounter = statConn.ReadCounter
		writeCounter = statConn.WriteCounter
	}
	switch iConn := iConn.(type) {
	case *internet.PacketConnWrapper:
		if readCounter == nil && writeCounter == nil {
			return iConn.Conn
		}
		return &statCounterPacketConn{
			PacketConn:   iConn.Conn,
			readCounter:  readCounter,
			writeCounter: writeCounter,
		}
	case net.PacketConn:
		if readCounter == nil && writeCounter == nil {
			return iConn
		}
		return &statCounterPacketConn{
			PacketConn:   iConn,
			readCounter:  readCounter,
			writeCounter: writeCounter,
		}
	default:
		return internet.NewConnWrapper(conn)
	}
}

// statCounterPacketConn must NOT implement syscall.Conn
type statCounterPacketConn struct {
	net.PacketConn
	readCounter  stats.Counter
	writeCounter stats.Counter
}

func (c *statCounterPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, addr, err
}

func (c *statCounterPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, addr)
	if c.writeCounter != nil {
		c.writeCounter.Add(int64(n))
	}
	return n, err
}
