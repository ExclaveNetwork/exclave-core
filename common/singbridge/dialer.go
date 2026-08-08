package singbridge

import (
	"context"
	"syscall"

	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
	"golang.org/x/net/ipv4"

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

var _ network.Dialer = (*dialerWrapper)(nil)

type dialerWrapper struct {
	dialer internet.Dialer
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
	return newConnectPacketConn(conn), nil
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

func newConnectPacketConn(conn net.Conn) net.Conn {
	var readCounter, writeCounter stats.Counter
	iConn := conn
	if statConn, ok := iConn.(*internet.StatCouterConnection); ok {
		iConn = statConn.Connection
		readCounter = statConn.ReadCounter
		writeCounter = statConn.WriteCounter
	}
	var packetConn net.PacketConn
	switch iConn := iConn.(type) {
	case *internet.PacketConnWrapper:
		packetConn = iConn.Conn
	case net.PacketConn:
		packetConn = iConn
	default:
		return conn
	}
	statCounterPacketConn := &statCounterPacketConn{
		PacketConn:   packetConn,
		readCounter:  readCounter,
		writeCounter: writeCounter,
		read:         iConn.Read,
		write:        iConn.Write,
		remoteAddr:   iConn.RemoteAddr,
	}
	setBufferFn, canSetBuffer := packetConn.(interface {
		SetWriteBuffer(bytes int) error
		SetReadBuffer(bytes int) error
	})
	if !canSetBuffer {
		return statCounterPacketConn
	}
	setBufferConn := &setBufferConn{
		statCounterPacketConn: statCounterPacketConn,
		setWriteBuffer:        setBufferFn.SetWriteBuffer,
		setReadBuffer:         setBufferFn.SetReadBuffer,
	}
	syscallConnFn, isSyscallConn := packetConn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !isSyscallConn {
		return setBufferConn
	}
	syscallConn := &syscallConn{
		setBufferConn: setBufferConn,
		syscallConn:   syscallConnFn.SyscallConn,
	}
	oobFn, oobCapable := packetConn.(interface {
		ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error)
		WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error)
	})
	if !oobCapable {
		return syscallConn
	}
	oobConn := &oobConn{
		syscallConn: syscallConn,
		readMsgUDP:  oobFn.ReadMsgUDP,
		writeMsgUDP: oobFn.WriteMsgUDP,
	}
	readBatchFn, canReadBatch := packetConn.(interface {
		ReadBatch(ms []ipv4.Message, flags int) (int, error)
	})
	if canReadBatch {
		oobConn.readBatch = readBatchFn.ReadBatch
	} else {
		oobConn.readBatch = ipv4.NewPacketConn(oobConn).ReadBatch
	}
	return oobConn
}

func newBindPacketConn(conn net.Conn) net.PacketConn {
	var readCounter, writeCounter stats.Counter
	iConn := conn
	if statConn, ok := iConn.(*internet.StatCouterConnection); ok {
		iConn = statConn.Connection
		readCounter = statConn.ReadCounter
		writeCounter = statConn.WriteCounter
	}
	var packetConn net.PacketConn
	switch iConn := iConn.(type) {
	case *internet.PacketConnWrapper:
		packetConn = iConn.Conn
	case net.PacketConn:
		packetConn = iConn
	default:
		return internet.NewConnWrapper(conn)
	}
	statCounterPacketConn := &statCounterPacketConn{
		PacketConn:   packetConn,
		readCounter:  readCounter,
		writeCounter: writeCounter,
		read:         iConn.Read,
		write:        iConn.Write,
		remoteAddr:   iConn.RemoteAddr,
	}
	setBufferFn, canSetBuffer := packetConn.(interface {
		SetWriteBuffer(bytes int) error
		SetReadBuffer(bytes int) error
	})
	if !canSetBuffer {
		return statCounterPacketConn
	}
	setBufferConn := &setBufferConn{
		statCounterPacketConn: statCounterPacketConn,
		setWriteBuffer:        setBufferFn.SetWriteBuffer,
		setReadBuffer:         setBufferFn.SetReadBuffer,
	}
	syscallConnFn, isSyscallConn := packetConn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !isSyscallConn {
		return setBufferConn
	}
	syscallConn := &syscallConn{
		setBufferConn: setBufferConn,
		syscallConn:   syscallConnFn.SyscallConn,
	}
	oobFn, oobCapable := packetConn.(interface {
		ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error)
		WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error)
	})
	if !oobCapable {
		return syscallConn
	}
	oobConn := &oobConn{
		syscallConn: syscallConn,
		readMsgUDP:  oobFn.ReadMsgUDP,
		writeMsgUDP: oobFn.WriteMsgUDP,
	}
	readBatchFn, canReadBatch := packetConn.(interface {
		ReadBatch(ms []ipv4.Message, flags int) (int, error)
	})
	if canReadBatch {
		oobConn.readBatch = readBatchFn.ReadBatch
	} else {
		oobConn.readBatch = ipv4.NewPacketConn(oobConn).ReadBatch
	}
	return oobConn
}

type statCounterPacketConn struct {
	net.PacketConn
	readCounter  stats.Counter
	writeCounter stats.Counter
	read         func(b []byte) (int, error)
	write        func(b []byte) (int, error)
	remoteAddr   func() net.Addr
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

func (c *statCounterPacketConn) Read(b []byte) (int, error) {
	n, err := c.read(b)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, err
}

func (c *statCounterPacketConn) Write(b []byte) (int, error) {
	n, err := c.write(b)
	if c.writeCounter != nil {
		c.writeCounter.Add(int64(n))
	}
	return n, err
}

func (c *statCounterPacketConn) RemoteAddr() net.Addr {
	return c.remoteAddr()
}

type setBufferConn struct {
	*statCounterPacketConn
	setWriteBuffer func(bytes int) error
	setReadBuffer  func(bytes int) error
}

type syscallConn struct {
	*setBufferConn
	syscallConn func() (syscall.RawConn, error)
}

type oobConn struct {
	*syscallConn
	readMsgUDP  func(b, oob []byte) (int, int, int, *net.UDPAddr, error)
	writeMsgUDP func(b, oob []byte, addr *net.UDPAddr) (int, int, error)
	readBatch   func(ms []ipv4.Message, flags int) (int, error)
}

func (c *setBufferConn) SetReadBuffer(bytes int) error {
	return c.setReadBuffer(bytes)
}

func (c *setBufferConn) SetWriteBuffer(bytes int) error {
	return c.setWriteBuffer(bytes)
}

func (c *syscallConn) SyscallConn() (syscall.RawConn, error) {
	return c.syscallConn()
}

func (c *oobConn) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	n, oobn, flags, addr, err := c.readMsgUDP(b, oob)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, oobn, flags, addr, err
}

func (c *oobConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	n, oobn, err := c.writeMsgUDP(b, oob, addr)
	if c.writeCounter != nil {
		c.writeCounter.Add(int64(n))
	}
	return n, oobn, err
}

func (c *oobConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	n, err := c.readBatch(ms, flags)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, err
}
