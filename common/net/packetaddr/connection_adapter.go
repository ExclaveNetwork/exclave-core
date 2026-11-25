package packetaddr

import (
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
)

func ToPacketAddrConnWrapper(conn net.PacketConn) FusedConnection {
	return &packetConnWrapper{conn}
}

type packetConnWrapper struct {
	net.PacketConn
}

func (pc *packetConnWrapper) RemoteAddr() net.Addr {
	return nil
}

type FusedConnection interface {
	net.PacketConn
	net.Conn
}

func (pc *packetConnWrapper) Read(p []byte) (n int, err error) {
	recbuf := buf.StackNew()
	recbuf.Extend(buf.Size)
	n, addr, err := pc.ReadFrom(recbuf.Bytes())
	if err != nil {
		recbuf.Release()
		return 0, err
	}
	recbuf.Resize(0, int32(n))
	result, err := AttachAddressToPacket(&recbuf, addr)
	if err != nil {
		recbuf.Release()
		return 0, err
	}
	n = copy(p, result.Bytes())
	result.Release()
	return n, nil
}

func (pc *packetConnWrapper) Write(p []byte) (n int, err error) {
	data, addr, err := ExtractAddressFromPacket(buf.FromBytes(p))
	if err != nil {
		return 0, err
	}
	_, err = pc.WriteTo(data.Bytes(), addr)
	data.Release()
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (pc *packetConnWrapper) Close() error {
	return pc.PacketConn.Close()
}

func GetDestinationSubsetOf(dest net.Destination) bool {
	return dest.Address.Family().IsDomain() && dest.Address.Domain() == SeqPacketMagicAddress
}
