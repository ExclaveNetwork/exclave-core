package singbridge

import (
	"syscall"

	"golang.org/x/net/ipv4"

	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/features/stats"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet"
)

var (
	_ net.Conn       = (*noSyscallConn)(nil)
	_ net.PacketConn = (*quicCapableConn)(nil)
	_ net.Conn       = (*quicCapableConn)(nil)
	_ syscall.Conn   = (*quicCapableConn)(nil)
	_ batchReader    = (*quicCapableConn)(nil)
)

// FIXME: add method for internet.Dialer to create a connect() socket instead of converting a bind() socket to a connect() socket
func NewQUICDialerWrapper(dialer internet.Dialer) *dialerWrapper {
	return &dialerWrapper{
		dialer: dialer,
		quic:   true,
	}
}

func newQUICConnectPacketConn(conn net.Conn) net.Conn {
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
		if _, ok := iConn.Conn.(syscall.Conn); !ok {
			return conn
		}
		packetConn = iConn.Conn
	case net.PacketConn:
		if _, ok := iConn.(syscall.Conn); !ok {
			return conn
		}
		packetConn = iConn
	default:
		if !supportOOB {
			return conn
		}
		if _, ok := conn.(syscall.Conn); !ok {
			return conn
		}
		return &noSyscallConn{Conn: conn}
	}
	if supportOOB {
		// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L496
		// connect() socket required
		addr, ok := conn.RemoteAddr().(*net.UDPAddr)
		if !ok {
			return &noSyscallConn{Conn: conn}
		}
		syscallConn, err := packetConn.(syscall.Conn).SyscallConn()
		if err != nil {
			return &noSyscallConn{Conn: conn}
		}
		if err := connect(syscallConn, addr); err != nil {
			return &noSyscallConn{Conn: conn}
		}
	}
	quicCapableConn := &quicCapableConn{
		statCounterPacketConn: &statCounterPacketConn{
			PacketConn:   packetConn,
			readCounter:  readCounter,
			writeCounter: writeCounter,
		},
		syscallConn: packetConn.(syscall.Conn).SyscallConn,
		read:        conn.Read,
		write:       conn.Write,
		remoteAddr:  conn.RemoteAddr,
	}
	if batchReader, ok := packetConn.(batchReader); ok {
		quicCapableConn.readBatch = batchReader.ReadBatch
	} else {
		quicCapableConn.readBatch = ipv4.NewPacketConn(quicCapableConn).ReadBatch
	}
	return quicCapableConn
}

// noSyscallConn must NOT implement syscall.Conn
type noSyscallConn struct {
	net.Conn
}

type quicCapableConn struct {
	*statCounterPacketConn
	syscallConn func() (syscall.RawConn, error)
	readBatch   func(ms []ipv4.Message, flags int) (int, error)
	read        func(b []byte) (int, error)
	write       func(b []byte) (int, error)
	remoteAddr  func() net.Addr
}

// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L249
// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L315
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *quicCapableConn) Read(b []byte) (int, error) {
	n, err := c.read(b)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, err
}

// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L249
// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L315
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *quicCapableConn) Write(b []byte) (int, error) {
	n, err := c.write(b)
	if c.writeCounter != nil {
		c.writeCounter.Add(int64(n))
	}
	return n, err
}

// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L249
// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L315
// https://github.com/golang/net/blob/f6c404bf8371cea2a96e5bf2075b6f5a3b06657c/ipv4/endpoint.go#L103
func (c *quicCapableConn) RemoteAddr() net.Addr {
	return c.remoteAddr()
}

// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn.go#L54
// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn.go#L90
func (c *quicCapableConn) SyscallConn() (syscall.RawConn, error) {
	return c.syscallConn()
}

// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L243-L250
// https://github.com/SagerNet/quic-go/blob/acafdc7599d1238495def470f7077b3212903b1f/sys_conn_oob.go#L309-L316
func (c *quicCapableConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	n, err := c.readBatch(ms, flags)
	if c.readCounter != nil {
		c.readCounter.Add(int64(n))
	}
	return n, err
}

type batchReader interface {
	ReadBatch(ms []ipv4.Message, flags int) (int, error)
}
