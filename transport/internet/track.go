package internet

import (
	"container/list"
	"net"
	"syscall"

	"golang.org/x/net/ipv4"

	"github.com/exclavenetwork/exclave-core/v5/common/track"
)

var (
	_ net.Conn       = (*trackedConn)(nil)
	_ net.Conn       = (*trackedPacketConn)(nil)
	_ net.PacketConn = (*trackedPacketConn)(nil)
)

func newTrackedConn(conn net.Conn, pool *track.ConnectionPool) net.Conn {
	if _, ok := conn.(*trackedConn); ok {
		panic("already a trackedConn")
	}
	if _, ok := conn.(*trackedPacketConn); ok {
		panic("already a trackedPacketConn")
	}

	var packetConn net.PacketConn
	switch conn := conn.(type) {
	case *PacketConnWrapper:
		packetConn = conn.Conn
	case net.PacketConn:
		packetConn = conn
	default:
		return &trackedConn{
			Conn: conn,
			pool: pool,
			elem: pool.PushBack(conn),
		}
	}
	trackedPacketConn := &trackedPacketConn{
		PacketConn: packetConn,
		pool:       pool,
		elem:       pool.PushBack(conn),
		read:       conn.Read,
		write:      conn.Write,
		remoteAddr: conn.RemoteAddr,
	}
	setBufferFn, canSetBuffer := packetConn.(interface {
		SetWriteBuffer(bytes int) error
		SetReadBuffer(bytes int) error
	})
	if !canSetBuffer {
		return trackedPacketConn
	}
	setBufferConn := &setBufferConn{
		trackedPacketConn: trackedPacketConn,
		setWriteBuffer:    setBufferFn.SetWriteBuffer,
		setReadBuffer:     setBufferFn.SetReadBuffer,
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

type trackedConn struct {
	net.Conn
	pool *track.ConnectionPool
	elem *list.Element
}

func (c *trackedConn) Close() error {
	c.pool.Remove(c.elem)
	return c.Conn.Close()
}

type trackedPacketConn struct {
	net.PacketConn
	pool       *track.ConnectionPool
	elem       *list.Element
	read       func(b []byte) (int, error)
	write      func(b []byte) (int, error)
	remoteAddr func() net.Addr
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L113
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *trackedPacketConn) Read(b []byte) (int, error) {
	return c.read(b)
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L113
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *trackedPacketConn) Write(b []byte) (int, error) {
	return c.write(b)
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L113
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *trackedPacketConn) RemoteAddr() net.Addr {
	return c.remoteAddr()
}

func (c *trackedPacketConn) Close() error {
	c.pool.Remove(c.elem)
	return c.PacketConn.Close()
}

type setBufferConn struct {
	*trackedPacketConn
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
	return c.readMsgUDP(b, oob)
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn.go#L97
func (c *oobConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	return c.writeMsgUDP(b, oob, addr)
}

// https://github.com/quic-go/quic-go/blob/cea2e60cea0e3ce5248d1ec2003c0a2b73051547/sys_conn_oob.go#L109-L114
func (c *oobConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	return c.readBatch(ms, flags)
}
