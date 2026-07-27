package snell

import (
	"io"

	singcommon "github.com/sagernet/sing/common"
	singbuf "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/singbridge"
	"github.com/exclavenetwork/exclave-core/v5/transport"
)

var _ network.PacketConn = (*packetConnWrapper)(nil)

func newPacketConnWrapper(link *transport.Link, dest net.Destination, destIP net.Address, resolver func(domain string) (net.Address, error)) *packetConnWrapper {
	conn := &packetConnWrapper{
		reader:   link.Reader,
		writer:   link.Writer,
		dest:     dest,
		destIP:   destIP,
		resolver: resolver,
	}
	return conn
}

type packetConnWrapper struct {
	reader buf.Reader
	writer buf.Writer
	dest   net.Destination
	destIP net.Address
	cached buf.MultiBuffer
	net.Conn
	resolver func(domain string) (net.Address, error)
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
			if destination.Address.Family().IsDomain() {
				if destination.Address.Domain() == w.dest.Address.Domain() {
					destination.Address = w.destIP
				} else {
					addr, err := w.resolver(destination.Address.Domain())
					if err != nil {
						return metadata.Socksaddr{}, err
					}
					destination.Address = addr
				}
			}
			return singbridge.ToSocksAddr(destination), nil
		}
	}
	mb, err := w.reader.ReadMultiBuffer()
	if err != nil {
		return metadata.Socksaddr{}, err
	}
	mb, b := buf.SplitFirst(mb)
	if b == nil {
		return metadata.Socksaddr{}, io.EOF
	}
	w.cached = mb
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
	if destination.Address.Family().IsDomain() {
		if destination.Address.Domain() == w.dest.Address.Domain() {
			destination.Address = w.destIP
		} else {
			addr, err := w.resolver(destination.Address.Domain())
			if err != nil {
				return metadata.Socksaddr{}, err
			}
			destination.Address = addr
		}
	}
	return singbridge.ToSocksAddr(destination), nil
}

func (w *packetConnWrapper) WritePacket(buffer *singbuf.Buffer, destination metadata.Socksaddr) error {
	b := buf.NewWithSize(int32(buffer.Len()))
	common.Must2(b.Write(buffer.Bytes()))
	endpoint := singbridge.ToDestination(destination, net.Network_UDP)
	b.Endpoint = &endpoint
	return w.writer.WriteMultiBuffer(buf.MultiBuffer{b})
}

func (w *packetConnWrapper) Close() error {
	buf.ReleaseMulti(w.cached)
	return nil
}

func newServerConnWrapper(serverConn network.NetPacketConn) network.NetPacketConn {
	frontHeadroom, _ := singcommon.Cast[network.FrontHeadroom](serverConn)
	rearHeadroom, _ := singcommon.Cast[network.RearHeadroom](serverConn)
	return &serverConnWrapper{
		NetPacketConn: serverConn,
		frontHeadroom: frontHeadroom,
		rearHeadroom:  rearHeadroom,
	}
}

var (
	_ network.FrontHeadroom = (*serverConnWrapper)(nil)
	_ network.RearHeadroom  = (*serverConnWrapper)(nil)
	_ network.WriterWithMTU = (*serverConnWrapper)(nil)
)

type serverConnWrapper struct {
	network.NetPacketConn
	frontHeadroom network.FrontHeadroom
	rearHeadroom  network.RearHeadroom
}

func (w *serverConnWrapper) FrontHeadroom() int {
	return w.frontHeadroom.FrontHeadroom()
}

func (w *serverConnWrapper) RearHeadroom() int {
	return w.rearHeadroom.RearHeadroom()
}

func (w *serverConnWrapper) WriterMTU() int {
	// https://github.com/SagerNet/sing-snell/blob/c43fbee0e8399abb81e9954944fb63c076331aec/snellv4/packet.go#L219
	// workaround UDP MTU issue
	return 0xffff - 259
}
