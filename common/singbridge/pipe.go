package singbridge

import (
	"io"
	"time"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/transport"
)

var _ net.Conn = (*pipeConnWrapper)(nil)

type pipeConnWrapper struct {
	reader io.Reader
	writer buf.Writer
}

func NewPipeConnWrapper(link *transport.Link) *pipeConnWrapper {
	conn := &pipeConnWrapper{
		writer: link.Writer,
	}
	if ir, ok := link.Reader.(io.Reader); ok {
		conn.reader = ir
	} else {
		conn.reader = &buf.BufferedReader{Reader: link.Reader}
	}
	return conn
}

func (w *pipeConnWrapper) Close() error {
	return nil
}

func (w *pipeConnWrapper) Read(b []byte) (int, error) {
	return w.reader.Read(b)
}

func (w *pipeConnWrapper) Write(p []byte) (int, error) {
	b := buf.NewWithSize(int32(len(p)))
	common.Must2(b.Write(p))
	mb := buf.MultiBuffer{b}
	err := w.writer.WriteMultiBuffer(mb)
	if err != nil {
		buf.ReleaseMulti(mb)
		return 0, err
	}
	return len(p), nil
}

func (w *pipeConnWrapper) SetDeadline(_ time.Time) error {
	return nil
}

func (w *pipeConnWrapper) SetReadDeadline(_ time.Time) error {
	return nil
}

func (w *pipeConnWrapper) SetWriteDeadline(_ time.Time) error {
	return nil
}

func (w *pipeConnWrapper) LocalAddr() net.Addr {
	return &net.TCPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}

func (w *pipeConnWrapper) RemoteAddr() net.Addr {
	return &net.TCPAddr{
		IP:   []byte{0, 0, 0, 0},
		Port: 0,
	}
}
