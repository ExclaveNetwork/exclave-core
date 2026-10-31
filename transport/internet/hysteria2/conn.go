package hysteria2

import (
	"time"

	"github.com/apernet/quic-go/quicvarint"
	hyClient "github.com/exclavenetwork/hysteria/core/v2/client"
	"github.com/exclavenetwork/hysteria/core/v2/international/protocol"
	"github.com/exclavenetwork/hysteria/core/v2/international/utils"
	hyServer "github.com/exclavenetwork/hysteria/core/v2/server"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
)

const (
	CanNotUseUDPExtension = "Only hysteria2 proxy protocol can use udpExtension."
	Hy2MustNeedTLS        = "Hysteria2 based on QUIC that requires TLS."
)

type HyConn struct {
	IsUDPExtension   bool
	IsServer         bool
	ClientUDPSession hyClient.HyUDPConn
	ServerUDPSession *hyServer.UdpSessionEntry

	stream        *utils.QStream
	headerWritten bool
}

func (c *HyConn) Read(b []byte) (int, error) {
	if c.IsUDPExtension {
		return 0, newError(CanNotUseUDPExtension)
	}
	return c.stream.Read(b)
}

func (c *HyConn) Write(b []byte) (int, error) {
	if c.IsUDPExtension {
		return 0, newError(CanNotUseUDPExtension)
	}
	if !c.IsServer && !c.headerWritten {
		c.headerWritten = true
		// write TCP frame type
		frameSize := quicvarint.Len(protocol.FrameTypeTCPRequest)
		buffer := buf.NewWithSize(int32(frameSize + len(b)))
		protocol.VarintPut(buffer.Extend(int32(frameSize)), protocol.FrameTypeTCPRequest)
		common.Must2(buffer.Write(b))
		_, err := c.stream.Write(buffer.Bytes())
		buffer.Release()
		if err != nil {
			return 0, err
		}
		return len(b), nil
	}
	return c.stream.Write(b)
}

func (c *HyConn) WritePacket(b []byte, dest net.Destination) error {
	if !c.IsUDPExtension {
		return newError(CanNotUseUDPExtension)
	}

	if c.IsServer {
		msg := &protocol.UDPMessage{
			SessionID: c.ServerUDPSession.ID,
			PacketID:  0,
			FragID:    0,
			FragCount: 1,
			Addr:      dest.NetAddr(),
			Data:      b,
		}
		c.ServerUDPSession.SendCh <- msg
		return nil
	}
	return c.ClientUDPSession.Send(b, dest.NetAddr())
}

func (c *HyConn) ReadPacket() ([]byte, net.Destination, error) {
	if !c.IsUDPExtension {
		return nil, net.Destination{}, newError(CanNotUseUDPExtension)
	}

	if c.IsServer {
		msg, ok := <-c.ServerUDPSession.ReceiveCh
		if !ok {
			return nil, net.Destination{}, newError("UDP session receive channel closed")
		}
		dest, err := net.ParseDestination("udp:" + msg.Addr)
		if err != nil {
			return nil, net.Destination{}, err
		}
		return msg.Data, dest, err
	}
	data, address, err := c.ClientUDPSession.Receive()
	if err != nil {
		return nil, net.Destination{}, err
	}
	dest, err := net.ParseDestination("udp:" + address)
	if err != nil {
		return nil, net.Destination{}, err
	}
	return data, dest, nil
}

func (c *HyConn) Close() error {
	if c.IsUDPExtension {
		if !c.IsServer && c.ClientUDPSession == nil || (c.IsServer && c.ServerUDPSession == nil) {
			return newError(CanNotUseUDPExtension)
		}
		if c.IsServer {
			c.ServerUDPSession.CloseWithErr(nil)
			return nil
		}
		return c.ClientUDPSession.Close()
	}
	return c.stream.Close()
}

func (c *HyConn) LocalAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *HyConn) RemoteAddr() net.Addr {
	return &net.UDPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (c *HyConn) SetDeadline(t time.Time) error {
	if c.IsUDPExtension {
		return nil
	}
	return c.stream.SetDeadline(t)
}

func (c *HyConn) SetReadDeadline(t time.Time) error {
	if c.IsUDPExtension {
		return nil
	}
	return c.stream.SetReadDeadline(t)
}

func (c *HyConn) SetWriteDeadline(t time.Time) error {
	if c.IsUDPExtension {
		return nil
	}
	return c.stream.SetWriteDeadline(t)
}
