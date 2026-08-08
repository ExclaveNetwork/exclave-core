package singbridge

import (
	"io"
	"time"

	singbuf "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/transport"
)

var _ network.PacketConn = (*packetConnWrapper)(nil)

func NewPacketConnWrapper(link *transport.Link, dest net.Destination) *packetConnWrapper {
	return &packetConnWrapper{
		reader: link.Reader,
		writer: link.Writer,
		dest:   dest,
	}
}

type packetConnWrapper struct {
	reader buf.Reader
	writer buf.Writer
	dest   net.Destination
	cached buf.MultiBuffer
}

func (w *packetConnWrapper) ReadPacket(buffer *singbuf.Buffer) (metadata.Socksaddr, error) {
	if w.cached != nil {
		mb, b := buf.SplitFirst(w.cached)
		if b == nil {
			w.cached = nil
		} else {
			w.cached = mb
			_, err := buffer.Write(b.Bytes())
			if err != nil {
				b.Release()
				return metadata.Socksaddr{}, err
			}
			var destination net.Destination
			if b.Endpoint != nil {
				destination = *b.Endpoint
			} else {
				destination = w.dest
			}
			b.Release()
			return ToSocksAddr(destination), nil
		}
	}
	mb, err := w.reader.ReadMultiBuffer()
	if err != nil {
		return metadata.Socksaddr{}, err
	}
	mb2, b := buf.SplitFirst(mb)
	if b == nil {
		return metadata.Socksaddr{}, io.EOF
	}
	w.cached = mb2
	_, err = buffer.Write(b.Bytes())
	if err != nil {
		b.Release()
		return metadata.Socksaddr{}, err
	}
	var destination net.Destination
	if b.Endpoint != nil {
		destination = *b.Endpoint
	} else {
		destination = w.dest
	}
	b.Release()
	return ToSocksAddr(destination), nil
}

func (w *packetConnWrapper) WritePacket(buffer *singbuf.Buffer, destination metadata.Socksaddr) error {
	b := buf.NewWithSize(int32(buffer.Len()))
	common.Must2(b.Write(buffer.Bytes()))
	endpoint := ToDestination(destination, net.Network_UDP)
	b.Endpoint = &endpoint
	return w.writer.WriteMultiBuffer(buf.MultiBuffer{b})
}

func (w *packetConnWrapper) Close() error {
	buf.ReleaseMulti(w.cached)
	return nil
}

func (w *packetConnWrapper) SetDeadline(_ time.Time) error {
	return nil
}

func (w *packetConnWrapper) SetReadDeadline(_ time.Time) error {
	return nil
}

func (w *packetConnWrapper) SetWriteDeadline(_ time.Time) error {
	return nil
}

func (w *packetConnWrapper) LocalAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}
