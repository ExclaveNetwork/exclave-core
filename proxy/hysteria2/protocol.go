package hysteria2

import (
	"io"
	"math/rand"

	"github.com/apernet/quic-go/quicvarint"
	hyProtocol "github.com/exclavenetwork/hysteria/core/v2/international/protocol"

	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/features/stats"
	hyTransport "github.com/exclavenetwork/exclave-core/v5/transport/internet/hysteria2"
)

const (
	paddingChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// ConnWriter is TCP Connection Writer Wrapper
type ConnWriter struct {
	io.Writer
	Target        net.Destination
	TCPHeaderSent bool
}

// WriteMultiBuffer implements buf.Writer
func (c *ConnWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	defer buf.ReleaseMulti(mb)

	if !c.TCPHeaderSent {
		if err := c.writeTCPHeader(); err != nil {
			return newError("failed to write request header").Base(err)
		}
	}

	for _, b := range mb {
		if !b.IsEmpty() {
			if _, err := c.Write(b.Bytes()); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *ConnWriter) WriteTCPHeader() error {
	if !c.TCPHeaderSent {
		if err := c.writeTCPHeader(); err != nil {
			return err
		}
	}
	return nil
}

func QuicLen(s int) int {
	return quicvarint.Len(uint64(s))
}

func (c *ConnWriter) writeTCPHeader() error {
	c.TCPHeaderSent = true

	paddingLen := 64 + rand.Intn(512-64)
	padding := make([]byte, paddingLen)
	for i := range padding {
		padding[i] = paddingChars[rand.Intn(len(paddingChars))]
	}
	addressAndPort := c.Target.NetAddr()
	addressLen := len(addressAndPort)
	if addressLen > hyProtocol.MaxAddressLength {
		return newError("address length too large: ", addressLen)
	}
	size := QuicLen(addressLen) + addressLen + QuicLen(paddingLen) + paddingLen

	buf := make([]byte, size)
	i := hyProtocol.VarintPut(buf, uint64(addressLen))
	i += copy(buf[i:], addressAndPort)
	i += hyProtocol.VarintPut(buf[i:], uint64(paddingLen))
	copy(buf[i:], padding)

	_, err := c.Writer.Write(buf)
	return err
}

// PacketWriter UDP Connection Writer Wrapper
type PacketWriter struct {
	HyConn  *hyTransport.HyConn
	Target  net.Destination
	counter stats.Counter
}

// WriteMultiBuffer implements buf.Writer
func (w *PacketWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	defer buf.ReleaseMulti(mb)
	for _, b := range mb {
		if b.IsEmpty() {
			continue
		}
		target := w.Target
		if b.Endpoint != nil {
			target = *b.Endpoint
		}
		if err := w.writePacket(b.Bytes(), target); err != nil {
			return err
		}
	}

	return nil
}

// WriteMultiBufferWithMetadata writes udp packet with destination specified
func (w *PacketWriter) WriteMultiBufferWithMetadata(mb buf.MultiBuffer, dest net.Destination) error {
	defer buf.ReleaseMulti(mb)
	for _, b := range mb {
		if b.IsEmpty() {
			continue
		}
		if err := w.writePacket(b.Bytes(), dest); err != nil {
			return err
		}
	}

	return nil
}

func (w *PacketWriter) writePacket(payload []byte, dest net.Destination) error {
	if err := w.HyConn.WritePacket(payload, dest); err != nil {
		return err
	}
	if w.counter != nil {
		w.counter.Add(int64(len(payload)))
	}
	return nil
}

// PacketReader is UDP Connection Reader Wrapper
type PacketReader struct {
	HyConn  *hyTransport.HyConn
	counter stats.Counter
}

// ReadMultiBuffer implements buf.Reader
func (r *PacketReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	p, _, err := r.ReadMultiBufferWithMetadata()
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ReadMultiBufferWithMetadata reads udp packet with destination
func (r *PacketReader) ReadMultiBufferWithMetadata() (buf.MultiBuffer, net.Destination, error) {
	data, dest, err := r.HyConn.ReadPacket()
	if err != nil {
		return nil, net.Destination{}, err
	}
	if r.counter != nil {
		r.counter.Add(int64(len(data)))
	}
	b := buf.FromBytes(data)
	b.Endpoint = &net.Destination{
		Address: dest.Address,
		Port:    dest.Port,
		Network: net.Network_UDP,
	}
	return buf.MultiBuffer{b}, dest, nil
}
