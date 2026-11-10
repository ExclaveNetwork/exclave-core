package snell

import (
	"sync/atomic"

	singbuf "github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/singbridge"
)

var _ network.PacketConn = (*packetConnWrapper)(nil)

func newPacketConnWrapper(packetConn network.PacketConn, writer buf.Writer, domainDest net.Destination) *packetConnWrapper {
	return &packetConnWrapper{
		PacketConn: packetConn,
		writer:     writer,
		domainDest: singbridge.ToSocksAddr(domainDest),
	}
}

type packetConnWrapper struct {
	network.PacketConn
	writer        buf.Writer
	domainDest    metadata.Socksaddr
	ipDest        metadata.Socksaddr
	notFirstWrite atomic.Bool
}

func (w *packetConnWrapper) ReadPacket(buffer *singbuf.Buffer) (metadata.Socksaddr, error) {
	return w.PacketConn.ReadPacket(buffer)
}

func (w *packetConnWrapper) WritePacket(buffer *singbuf.Buffer, ipDest metadata.Socksaddr) error {
	if w.notFirstWrite.CompareAndSwap(false, true) {
		w.ipDest = ipDest
	}
	b := buf.NewWithSize(int32(buffer.Len()))
	common.Must2(b.Write(buffer.Bytes()))
	var endpoint net.Destination
	if ipDest == w.ipDest {
		endpoint = singbridge.ToDestination(w.domainDest, net.Network_UDP)
	} else {
		endpoint = singbridge.ToDestination(ipDest, net.Network_UDP)
	}
	b.Endpoint = &endpoint
	return w.writer.WriteMultiBuffer(buf.MultiBuffer{b})
}
