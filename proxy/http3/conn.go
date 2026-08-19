package http3

import (
	"syscall"

	"github.com/quic-go/quic-go"
	"golang.org/x/net/ipv4"

	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/features/stats"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

var (
	_ net.PacketConn            = (*statCounterConn)(nil)
	_ net.Conn                  = (*statCounterConn)(nil)
	_ setBuffer                 = (*setBufferConn)(nil)
	_ net.PacketConn            = (*setBufferConn)(nil)
	_ net.Conn                  = (*setBufferConn)(nil)
	_ syscall.Conn              = (*syscallConn)(nil)
	_ setBuffer                 = (*syscallConn)(nil)
	_ net.PacketConn            = (*syscallConn)(nil)
	_ net.Conn                  = (*syscallConn)(nil)
	_ quic.OOBCapablePacketConn = (*oobConn)(nil)
	_ syscall.Conn              = (*oobConn)(nil)
	_ setBuffer                 = (*oobConn)(nil)
	_ net.PacketConn            = (*oobConn)(nil)
	_ net.Conn                  = (*oobConn)(nil)
	_ readBatch                 = (*oobConn)(nil)
)

func newQUICPacketConn(conn net.Conn) net.PacketConn {
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
		if readCounter == nil && writeCounter == nil {
			return packetConn
		}
	case net.PacketConn:
		packetConn = iConn
		if readCounter == nil && writeCounter == nil {
			return packetConn
		}
	default:
		return internet.NewConnWrapper(conn)
	}
	statCounterConn := &statCounterConn{
		PacketConn:   packetConn,
		readCounter:  readCounter,
		writeCounter: writeCounter,
		read:         conn.Read,
		write:        conn.Write,
		remoteAddr:   conn.RemoteAddr,
	}
	setBufferFn, canSetBuffer := packetConn.(setBuffer)
	if !canSetBuffer {
		return statCounterConn
	}
	setBufferConn := &setBufferConn{
		statCounterConn: statCounterConn,
		setWriteBuffer:  setBufferFn.SetWriteBuffer,
		setReadBuffer:   setBufferFn.SetReadBuffer,
	}
	syscallConnFn, isSyscallConn := packetConn.(syscall.Conn)
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
	readBatchFn, canReadBatch := packetConn.(readBatch)
	if canReadBatch {
		oobConn.readBatch = readBatchFn.ReadBatch
	} else {
		oobConn.readBatch = ipv4.NewPacketConn(oobConn).ReadBatch
	}
	return oobConn
}

type statCounterConn struct {
	net.PacketConn
	readCounter  stats.Counter
	writeCounter stats.Counter
	read         func(b []byte) (int, error)
	write        func(b []byte) (int, error)
	remoteAddr   func() net.Addr
}

func (c *statCounterConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, addr, err
}

func (c *statCounterConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, addr)
	if c.writeCounter != nil {
		c.writeCounter.Add(int64(n))
	}
	return n, err
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L113
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *statCounterConn) Read(b []byte) (int, error) {
	n, err := c.read(b)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, err
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L113
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *statCounterConn) Write(b []byte) (int, error) {
	n, err := c.write(b)
	if c.writeCounter != nil {
		c.writeCounter.Add(int64(n))
	}
	return n, err
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L113
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *statCounterConn) RemoteAddr() net.Addr {
	return c.remoteAddr()
}

type setBufferConn struct {
	*statCounterConn
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

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_buffers.go#L14
func (c *setBufferConn) SetReadBuffer(bytes int) error {
	return c.setReadBuffer(bytes)
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_buffers_write.go#L16
func (c *setBufferConn) SetWriteBuffer(bytes int) error {
	return c.setWriteBuffer(bytes)
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_buffers.go#L21
// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_buffers_write.go#L23
// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn.go#L79
func (c *syscallConn) SyscallConn() (syscall.RawConn, error) {
	return c.syscallConn()
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn.go#L97
func (c *oobConn) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	n, oobn, flags, addr, err := c.readMsgUDP(b, oob)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, oobn, flags, addr, err
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn.go#L97
func (c *oobConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	n, oobn, err := c.writeMsgUDP(b, oob, addr)
	if c.writeCounter != nil {
		c.writeCounter.Add(int64(n))
	}
	return n, oobn, err
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L109-L114
func (c *oobConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	n, err := c.readBatch(ms, flags)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, err
}

type setBuffer interface {
	SetWriteBuffer(bytes int) error
	SetReadBuffer(bytes int) error
}

type readBatch interface {
	ReadBatch(ms []ipv4.Message, flags int) (int, error)
}
