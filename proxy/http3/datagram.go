package http3

import (
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
)

var (
	_ buf.Writer = (*datagramWriter)(nil)
	_ buf.Reader = (*datagramReader)(nil)
)

type datagramReader struct {
	conn net.Conn
	dest net.Destination
}

func newDatagramReader(conn net.Conn, dest net.Destination) *datagramReader {
	return &datagramReader{
		conn: conn,
		dest: dest,
	}
}

func (r *datagramReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	b := buf.New()
	b.Resize(0, buf.Size)
	n, err := r.conn.Read(b.Bytes())
	if err != nil {
		b.Release()
		return nil, err
	}
	b.Resize(0, int32(n))
	if n < 1 {
		b.Release()
		return nil, newError("invalid payload length")
	}
	first, err := b.ReadByte()
	if err != nil {
		b.Release()
		return nil, err
	}
	if first != 0x00 {
		b.Release()
		return nil, newError("invalid context id")
	}
	b.Endpoint = &r.dest
	return buf.MultiBuffer{b}, nil
}

type datagramWriter struct {
	conn net.Conn
	dest net.Destination
}

func newDatagramWriter(conn net.Conn, dest net.Destination) *datagramWriter {
	return &datagramWriter{
		conn: conn,
		dest: dest,
	}
}

func (w *datagramWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	defer buf.ReleaseMulti(mb)
	for _, b := range mb {
		if *b.Endpoint != w.dest {
			newError("CONNECT-UDP can not have different destination addresses").AtDebug().WriteToLog()
			continue
		}
		payload := buf.NewWithSize(1 + b.Len())
		payload.WriteByte(0x00)  // Context ID
		payload.Write(b.Bytes()) // UDP Proxying Payload
		_, err := w.conn.Write(payload.Bytes())
		payload.Release()
		if err != nil {
			return err
		}
	}
	return nil
}
