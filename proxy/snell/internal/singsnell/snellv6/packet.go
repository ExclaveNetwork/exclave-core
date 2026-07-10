package snellv6

import (
	"io"
	"net"
	"os"
	"sync"

	snell "github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/singsnell"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/singsnell/internal/reuse"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	maxUDPRequestHeaderLen  = 1 + 1 + 255 + 2
	maxUDPResponseHeaderLen = 1 + 16 + 2
)

func (c *clientPacketConn) udpRequestAddrLen(destination M.Socksaddr) int {
	if destination.IsIP() {
		if destination.Unwrap().Addr.Is4() {
			return 1 + 1 + 4 + 2
		}
		return 1 + 1 + 16 + 2
	}
	return 1 + len(destination.Fqdn) + 2
}

type clientPacketConn struct {
	net.Conn
	client *Client

	writeAccess sync.Mutex
	writer      reuse.RecordWriter

	readAccess sync.Mutex
	reader     reuse.RecordReader

	readWaitOptions N.ReadWaitOptions
}

func (c *clientPacketConn) writeRequest() error {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	if c.writer != nil {
		return nil
	}
	requestPayload := snell.Request{Command: snell.CommandUDP, ClientID: c.client.userKey}
	request := buf.NewSize(requestPayload.Len())
	err := requestPayload.Write(request)
	if err != nil {
		request.Release()
		return err
	}
	writer, err := writeFirstRecord(c.Conn, c.client.mode, c.client.psk, c.client.profile, request.Bytes())
	request.Release()
	if err != nil {
		return E.Cause(err, "write udp request")
	}
	c.writer = writer
	return nil
}

func (c *clientPacketConn) readReply() (reuse.RecordReader, error) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	if c.reader != nil {
		return c.reader, nil
	}
	reader, record, err := readFirstRecord(c.Conn, c.client.mode, c.client.psk, c.client.profile, c.readWaitOptions)
	if err != nil {
		return nil, E.Cause(err, "read udp reply")
	}
	cached, err := reuse.ParseReply(record)
	if err != nil {
		return nil, err
	}
	reader.SetCache(cached)
	c.reader = reader
	return reader, nil
}

func (c *clientPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	err := c.writeRequest()
	if err != nil {
		buffer.Release()
		return err
	}
	_, err = c.readReply()
	if err != nil {
		buffer.Release()
		return err
	}
	header := buf.With(buffer.ExtendHeader(1 + c.udpRequestAddrLen(destination)))
	common.Must(header.WriteByte(snell.UDPCommandForward))
	err = snell.WriteUDPRequestAddress(header, destination)
	if err != nil {
		buffer.Release()
		return err
	}
	if buffer.Len() > maxPayload {
		buffer.Release()
		return snell.ErrPayloadTooLarge
	}
	return c.writer.WritePacketBuffer(buffer)
}

func (c *clientPacketConn) CreatePacketBatchWriter() (PacketBatchWriter, bool) {
	return nil, false
}

type clientPacketBatchWriter struct {
	conn     *clientPacketConn
	upstream N.VectorisedWriter

	access sync.Mutex
	writer N.VectorisedWriter
}

func (w *clientPacketBatchWriter) WritePacketBatch(buffers []*buf.Buffer, destinations []M.Socksaddr) error {
	return nil
}

func (c *clientPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	reader, err := c.readReply()
	if err != nil {
		return M.Socksaddr{}, err
	}
	record, err := reader.NextRecord()
	if err != nil {
		return M.Socksaddr{}, err
	}
	destination, err := snell.ReadUDPResponseAddress(record)
	if err != nil {
		record.Release()
		return M.Socksaddr{}, err
	}
	if record.Len() > buffer.FreeLen() {
		record.Release()
		return M.Socksaddr{}, io.ErrShortBuffer
	}
	_, err = buffer.Write(record.Bytes())
	record.Release()
	if err != nil {
		return M.Socksaddr{}, err
	}
	return destination, nil
}

func (c *clientPacketConn) FrontHeadroom() int {
	switch c.client.mode {
	case ModeUnsafeRaw:
		return snell.HeaderPlainLen + maxUDPRequestHeaderLen
	case ModeUnshaped:
		return snell.HeaderCipherLen + maxUDPRequestHeaderLen
	default:
		return c.client.profile.recordPrefixMax + snell.HeaderCipherLen + c.client.profile.padMaxHeadroom + maxUDPRequestHeaderLen
	}
}

func (c *clientPacketConn) RearHeadroom() int {
	if c.client.mode == ModeUnsafeRaw {
		return 0
	}
	return snell.AEADTagLen
}

func (c *clientPacketConn) WriterMTU() int {
	switch c.client.mode {
	case ModeUnsafeRaw, ModeUnshaped:
		return maxPayload - maxUDPRequestHeaderLen
	default:
		payloadLimit := c.client.profile.chunkInitial
		switch c.client.profile.chunkPolicy {
		case 1:
			payloadLimit = c.client.profile.chunkBuckets[0]
			for _, chunkBucket := range c.client.profile.chunkBuckets[1:] {
				payloadLimit = min(payloadLimit, chunkBucket)
			}
		case 2:
			payloadLimit -= c.client.profile.chunkJitter
		}
		payloadLimit = max(0x40, min(payloadLimit, c.client.profile.chunkMax))
		return max(1, payloadLimit-maxUDPRequestHeaderLen)
	}
}

func (c *clientPacketConn) Upstream() any {
	return c.Conn
}

func (c *clientPacketConn) InitializeReadWaiter(options N.ReadWaitOptions) (needCopy bool) {
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	c.readWaitOptions = options
	if c.reader != nil {
		c.reader.InitializeReadWaiter(options)
	}
	return false
}

func (c *clientPacketConn) WaitReadPacket() (*buf.Buffer, M.Socksaddr, error) {
	reader, err := c.readReply()
	if err != nil {
		return nil, M.Socksaddr{}, err
	}
	record, err := reader.WaitReadBuffer()
	if err != nil {
		return nil, M.Socksaddr{}, err
	}
	destination, err := snell.ReadUDPResponseAddress(record)
	if err != nil {
		record.Release()
		return nil, M.Socksaddr{}, err
	}
	return record, destination, nil
}















var (
	_ N.PacketConn              = (*clientPacketConn)(nil)
	_ N.PacketReadWaiter        = (*clientPacketConn)(nil)
	_ N.WriterWithMTU           = (*clientPacketConn)(nil)
	_ N.PacketConn              = (*serverPacketConn)(nil)
	_ N.PacketReadWaiter        = (*serverPacketConn)(nil)
	_ N.WriterWithMTU           = (*serverPacketConn)(nil)
)
