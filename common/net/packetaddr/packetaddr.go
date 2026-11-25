package packetaddr

import (
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/protocol"
)

//go:generate go run github.com/exclavenetwork/exclave-core/v5/common/errors/errorgen

var addrParser = protocol.NewAddressParser(
	protocol.AddressFamilyByte(0x01, net.AddressFamilyIPv4),
	protocol.AddressFamilyByte(0x02, net.AddressFamilyIPv6),
)

// AttachAddressToPacket
// relinquish ownership of data
// gain ownership of the returning value
func AttachAddressToPacket(data *buf.Buffer, address net.Addr) (*buf.Buffer, error) {
	udpAddr := address.(*net.UDPAddr)
	ip, port := net.IPAddress(udpAddr.IP), net.Port(udpAddr.Port)
	var bufSize int32
	if ip.Family().IsIPv4() {
		bufSize = 1 + 4 + 2 + data.Len()
	} else {
		bufSize = 1 + 16 + 2 + data.Len()
	}
	packetBuf := buf.NewWithSize(bufSize)
	common.Must(addrParser.WriteAddressPort(packetBuf, ip, port))
	common.Must2(packetBuf.Write(data.Bytes()))
	data.Release()
	return packetBuf, nil
}

// ExtractAddressFromPacket
// relinquish ownership of data
// gain ownership of the returning value
func ExtractAddressFromPacket(data *buf.Buffer) (*buf.Buffer, net.Addr, error) {
	address, port, err := addrParser.ReadAddressPort(nil, data)
	if err != nil {
		return nil, nil, err
	}
	return data, &net.UDPAddr{
		IP:   address.IP(),
		Port: int(port),
	}, nil
}
