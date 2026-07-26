package snell

import (
	"github.com/sagernet/sing/common"
	singbuf "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"

	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/singbridge"
	"github.com/exclavenetwork/exclave-core/v5/transport"
)

var _ network.PacketConn = (*packetConnWrapper)(nil)

func newPacketConnWrapper(link *transport.Link, dest net.Destination, destIP net.Address, resolver func(domain string) (net.Address, error)) *packetConnWrapper {
	conn := &packetConnWrapper{
		Reader:   link.Reader,
		Writer:   link.Writer,
		dest:     dest,
		destIP:   destIP,
		resolver: resolver,
	}
	return conn
}

type packetConnWrapper struct {
	buf.Reader
	buf.Writer
	dest   net.Destination
	destIP net.Address
	cached buf.MultiBuffer
	net.Conn
	resolver func(domain string) (net.Address, error)
}

func (w *packetConnWrapper) ReadPacket(buffer *singbuf.Buffer) (metadata.Socksaddr, error) {
	if w.cached != nil {
		mb, bb := buf.SplitFirst(w.cached)
		if bb == nil {
			w.cached = nil
		} else {
			buffer.Write(bb.Bytes())
			w.cached = mb
			var destination net.Destination
			if bb.Endpoint != nil {
				destination = *bb.Endpoint
			} else {
				destination = w.dest
			}
			bb.Release()
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
	mb, err := w.ReadMultiBuffer()
	if err != nil {
		return metadata.Socksaddr{}, err
	}
	nb, bb := buf.SplitFirst(mb)
	if bb == nil {
		return metadata.Socksaddr{}, nil
	} else {
		buffer.Write(bb.Bytes())
		w.cached = nb
		var destination net.Destination
		if bb.Endpoint != nil {
			destination = *bb.Endpoint
		} else {
			destination = w.dest
		}
		bb.Release()
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

func (w *packetConnWrapper) WritePacket(buffer *singbuf.Buffer, destination metadata.Socksaddr) error {
	vBuf := buf.New()
	vBuf.Write(buffer.Bytes())
	endpoint := singbridge.ToDestination(destination, net.Network_UDP)
	if endpoint.Address.Family().IsIP() && endpoint.Address == w.destIP {
		endpoint.Address = w.dest.Address
	}
	vBuf.Endpoint = &endpoint
	return w.WriteMultiBuffer(buf.MultiBuffer{vBuf})
}

func (w *packetConnWrapper) Close() error {
	buf.ReleaseMulti(w.cached)
	return nil
}

func newServerConnWrapper(serverConn network.NetPacketConn) network.NetPacketConn {
	frontHeadroom, ok1 := common.Cast[network.FrontHeadroom](serverConn)
	rearHeadroom, ok2 := common.Cast[network.RearHeadroom](serverConn)
	_, ok3 := common.Cast[network.WriterWithMTU](serverConn)
	if ok1 && ok2 && ok3 {
		return &serverConnWrapper{
			NetPacketConn: serverConn,
			frontHeadroom: frontHeadroom,
			rearHeadroom:  rearHeadroom,
		}
	}
	return serverConn
}

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
	// https://github.com/SagerNet/sing-snell/blob/c43fbee0e8399abb81e9954944fb63c076331aec/snellv6/packet.go#L224
	// workaround UDP MTU issue
	return 0xffff
}
