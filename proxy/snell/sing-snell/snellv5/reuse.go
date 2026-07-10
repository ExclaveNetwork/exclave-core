package snellv5

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing-snell/internal/reuse"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	remoteEOFCode    = reuse.RemoteEOFCode
	remoteEOFMessage = reuse.RemoteEOFMessage
)

type serverReuseSession[U comparable] struct {
	net.Conn
	service      *Service
	multiService *MultiService[U]
	reader       reuse.RecordReader
	writer       reuse.RecordWriter
}

func (s *serverReuseSession[U]) Serve(ctx context.Context, source M.Socksaddr, onClose N.CloseHandlerFunc, record *buf.Buffer, request snell.Request) (err error) {
	callOnClose := true
	if onClose != nil {
		defer func() {
			if callOnClose {
				onClose(err)
			}
		}()
	}
	for {
		requestCtx := ctx
		switch request.Command {
		case snell.CommandConnect, snell.CommandConnectV2:
			if s.multiService != nil {
				requestCtx, err = s.multiService.authenticate(ctx, request)
				if err != nil {
					record.Release()
					s.Conn.Close()
					return err
				}
			}
		case snell.CommandUDP:
			if s.multiService != nil {
				requestCtx, err = s.multiService.authenticate(ctx, request)
				if err != nil {
					record.Release()
					s.Conn.Close()
					return err
				}
			}
			record.Release()
			packetConn := &serverPacketConn{Conn: s.Conn, service: s.service, reader: s.reader, writer: s.writer}
			err = packetConn.writeTunnelReply()
			if err != nil {
				s.Conn.Close()
				return err
			}
			s.writer = packetConn.writer
			callOnClose = false
			s.service.handler.NewPacketConnectionEx(requestCtx, packetConn, source, M.Socksaddr{}, onClose)
			return nil
		case snell.CommandPing:
			record.Release()
			pong := [1]byte{snell.ReplyPong}
			if s.writer == nil {
				err = s.service.writePong(s.Conn)
			} else {
				_, err = s.writer.Write(pong[:])
			}
			if err != nil {
				s.Conn.Close()
				return err
			}
			s.Conn.Close()
			return nil
		default:
			record.Release()
			s.Conn.Close()
			return E.Extend(snell.ErrUnsupportedCommand, request.Command)
		}
		serverConn := &serverReuseConn[U]{Conn: s.Conn, session: s, completion: make(chan struct{})}
		if record.IsEmpty() {
			record.Release()
		} else {
			s.reader.SetCache(record)
		}
		s.service.handler.NewConnectionEx(requestCtx, serverConn, source, request.Destination, serverConn.completeLogicalConnection)
		select {
		case <-serverConn.completion:
		case <-requestCtx.Done():
			s.Conn.Close()
			return requestCtx.Err()
		}
		err = serverConn.CloseWrite()
		if err != nil {
			s.Conn.Close()
			return err
		}
		if serverConn.aborted.Load() {
			return nil
		}
		if !serverConn.readClosed.Load() {
			// snell-server v5.0.1: after writing the EOF packet, FUN_0013f630 keeps
			// consuming client records (forwarding them into the still writable
			// outgoing socket) until the zero chunk reaches FUN_0013f500 ->
			// FUN_0013ddb0 -> FUN_0013dca0, which resets the tunnel stage so the
			// next record is parsed as a request. The handler has already released
			// the upstream, so discard instead, capped like the client waiting state.
			var discarded int
			for {
				var drainRecord *buf.Buffer
				drainRecord, err = s.reader.NextRecord()
				if err != nil {
					break
				}
				discarded += drainRecord.Len()
				drainRecord.Release()
				if discarded >= reuse.WaitingDiscardLimit {
					s.Conn.Close()
					return E.New("snell: read too much data after request end")
				}
			}
			if !errors.Is(err, io.EOF) {
				s.Conn.Close()
				return E.Cause(err, "drain request")
			}
		}
		record, err = s.reader.ReadRecord()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			s.Conn.Close()
			return E.Cause(err, "read request")
		}
		request, err = s.service.readRequest(record)
		if err != nil {
			record.Release()
			s.Conn.Close()
			return E.Cause(err, "decode request")
		}
	}
}

type serverReuseConn[U comparable] struct {
	net.Conn
	session *serverReuseSession[U]

	access         sync.Mutex
	closeWriteOnce sync.Once
	closeWriteErr  error
	closeOnce      sync.Once
	closeErr       error
	readClosed     atomic.Bool
	aborted        atomic.Bool
	replyWritten   bool
	completeOnce   sync.Once
	completion     chan struct{}
}

func (c *serverReuseConn[U]) writeFirstResponse(payload []byte) error {
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		return err
	}
	key := snell.DeriveKey(c.session.service.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, snell.NonceLen)
	reply := buf.NewSize(1 + len(payload))
	common.Must(reply.WriteByte(snell.ReplyTunnel))
	common.Must1(reply.Write(payload))
	writer, err := newWriter(c.session.Conn, aead, nonce)
	if err != nil {
		reply.Release()
		return err
	}
	err = writer.WriteFirst(salt, reply.Bytes())
	reply.Release()
	if err != nil {
		return E.Cause(err, "write reply")
	}
	c.session.writer = writer
	c.replyWritten = true
	return nil
}

func (c *serverReuseConn[U]) writeResponse(payload []byte) error {
	if c.session.writer == nil {
		return c.writeFirstResponse(payload)
	}
	reply := buf.NewSize(1 + len(payload))
	common.Must(reply.WriteByte(snell.ReplyTunnel))
	common.Must1(reply.Write(payload))
	defer reply.Release()
	_, err := c.session.writer.Write(reply.Bytes())
	if err != nil {
		return E.Cause(err, "write reply")
	}
	c.replyWritten = true
	return nil
}

func (c *serverReuseConn[U]) writeResponseBuffer(buffer *buf.Buffer) error {
	if c.session.writer != nil {
		buffer.ExtendHeader(1)[0] = snell.ReplyTunnel
		err := c.session.writer.WriteBuffer(buffer)
		if err != nil {
			return E.Cause(err, "write reply")
		}
		c.replyWritten = true
		return nil
	}
	buffer.ExtendHeader(1)[0] = snell.ReplyTunnel
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		buffer.Release()
		return err
	}
	key := snell.DeriveKey(c.session.service.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		buffer.Release()
		return err
	}
	nonce := make([]byte, snell.NonceLen)
	writer, err := newWriter(c.session.Conn, aead, nonce)
	if err != nil {
		buffer.Release()
		return err
	}
	err = writer.WriteFirst(salt, buffer.Bytes())
	buffer.Release()
	if err != nil {
		return E.Cause(err, "write reply")
	}
	c.session.writer = writer
	c.replyWritten = true
	return nil
}

func (c *serverReuseConn[U]) writeErrorResponse(code byte, messageText string) error {
	message := []byte(messageText)
	reply := buf.NewSize(3 + len(message))
	common.Must(reply.WriteByte(snell.ReplyError))
	common.Must(reply.WriteByte(code))
	common.Must(reply.WriteByte(byte(len(message))))
	common.Must1(reply.Write(message))
	defer reply.Release()
	if c.session.writer == nil {
		salt := make([]byte, snell.SaltLen)
		_, err := io.ReadFull(rand.Reader, salt)
		if err != nil {
			return err
		}
		key := snell.DeriveKey(c.session.service.psk, salt)
		aead, err := snell.NewAEAD(key)
		if err != nil {
			return err
		}
		nonce := make([]byte, snell.NonceLen)
		writer, err := newWriter(c.session.Conn, aead, nonce)
		if err != nil {
			return err
		}
		err = writer.WriteFirst(salt, reply.Bytes())
		if err != nil {
			return E.Cause(err, "write error reply")
		}
		c.session.writer = writer
		return nil
	}
	_, err := c.session.writer.Write(reply.Bytes())
	if err != nil {
		return E.Cause(err, "write error reply")
	}
	return nil
}

func (c *serverReuseConn[U]) Read(p []byte) (int, error) {
	n, err := c.session.reader.Read(p)
	if errors.Is(err, io.EOF) {
		c.readClosed.Store(true)
	}
	return n, err
}

func (c *serverReuseConn[U]) ReadBuffer(buffer *buf.Buffer) error {
	err := c.session.reader.ReadBuffer(buffer)
	if errors.Is(err, io.EOF) {
		c.readClosed.Store(true)
	}
	return err
}

func (c *serverReuseConn[U]) Write(p []byte) (int, error) {
	c.access.Lock()
	defer c.access.Unlock()
	if c.replyWritten {
		return c.session.writer.Write(p)
	}
	err := c.writeResponse(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *serverReuseConn[U]) WriteBuffer(buffer *buf.Buffer) error {
	c.access.Lock()
	defer c.access.Unlock()
	if c.replyWritten {
		return c.session.writer.WriteBuffer(buffer)
	}
	return c.writeResponseBuffer(buffer)
}

func (c *serverReuseConn[U]) CreateVectorisedWriter() (N.VectorisedWriter, bool) {
	upstreamWriter, created := bufio.CreateVectorisedWriter(c.session.Conn)
	if !created {
		return nil, false
	}
	return &serverReuseVectorisedWriter[U]{conn: c, upstream: upstreamWriter}, true
}

type serverReuseVectorisedWriter[U comparable] struct {
	conn     *serverReuseConn[U]
	upstream N.VectorisedWriter
}

func (w *serverReuseVectorisedWriter[U]) WriteVectorised(buffers []*buf.Buffer) error {
	conn := w.conn
	conn.access.Lock()
	if conn.replyWritten {
		recordWriter := conn.session.writer
		conn.access.Unlock()
		return recordWriter.CreateVectorisedWriterFor(w.upstream).WriteVectorised(buffers)
	}
	for index, buffer := range buffers {
		if buffer.IsEmpty() {
			buffer.Release()
			continue
		}
		buffer.ExtendHeader(1)[0] = snell.ReplyTunnel
		if conn.session.writer != nil {
			recordWriter := conn.session.writer
			err := recordWriter.CreateVectorisedWriterFor(w.upstream).WriteVectorised(buffers[index:])
			if err != nil {
				conn.access.Unlock()
				return err
			}
			conn.replyWritten = true
			conn.access.Unlock()
			return nil
		}
		salt := make([]byte, snell.SaltLen)
		_, err := io.ReadFull(rand.Reader, salt)
		if err != nil {
			conn.access.Unlock()
			buf.ReleaseMulti(buffers[index:])
			return err
		}
		key := snell.DeriveKey(conn.session.service.psk, salt)
		aead, err := snell.NewAEAD(key)
		if err != nil {
			conn.access.Unlock()
			buf.ReleaseMulti(buffers[index:])
			return err
		}
		nonce := make([]byte, snell.NonceLen)
		recordWriter, err := newWriter(conn.session.Conn, aead, nonce)
		if err != nil {
			conn.access.Unlock()
			buf.ReleaseMulti(buffers[index:])
			return err
		}
		err = recordWriter.WriteFirstVectorised(salt, buffers[index:], w.upstream)
		if err != nil {
			conn.access.Unlock()
			return E.Cause(err, "write reply")
		}
		conn.session.writer = recordWriter
		conn.replyWritten = true
		conn.access.Unlock()
		return nil
	}
	conn.access.Unlock()
	return nil
}

func (c *serverReuseConn[U]) CloseWrite() error {
	c.closeWriteOnce.Do(func() {
		c.access.Lock()
		defer c.access.Unlock()
		// snell-server v5.0.1: FUN_0013f020 reports upstream EOF before any
		// payload as ReplyError 0x65 "Remote EOF", then aborts the reusable tunnel.
		if !c.replyWritten {
			c.closeWriteErr = c.writeErrorResponse(remoteEOFCode, remoteEOFMessage)
			if c.closeWriteErr == nil {
				c.aborted.Store(true)
				c.closeWriteErr = c.session.Conn.Close()
			}
			return
		}
		c.closeWriteErr = c.session.writer.WriteZeroChunk()
	})
	return c.closeWriteErr
}

func (c *serverReuseConn[U]) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.CloseWrite()
		c.completeLogicalConnection(c.closeErr)
	})
	return c.closeErr
}

func (c *serverReuseConn[U]) completeLogicalConnection(error) {
	c.completeOnce.Do(func() {
		close(c.completion)
	})
}

func (c *serverReuseConn[U]) InitializeReadWaiter(options N.ReadWaitOptions) (needCopy bool) {
	return c.session.reader.InitializeReadWaiter(options)
}

func (c *serverReuseConn[U]) WaitReadBuffer() (*buf.Buffer, error) {
	buffer, err := c.session.reader.WaitReadBuffer()
	if errors.Is(err, io.EOF) {
		c.readClosed.Store(true)
	}
	return buffer, err
}

func (c *serverReuseConn[U]) FrontHeadroom() int {
	if c.replyWritten {
		return snell.HeaderCipherLen
	}
	if c.session.writer != nil {
		return 1 + snell.HeaderCipherLen
	}
	return 1 + snell.SaltLen + snell.HeaderCipherLen + maxInitialPaddingLen
}

func (c *serverReuseConn[U]) RearHeadroom() int {
	return snell.AEADTagLen
}

func (c *serverReuseConn[U]) WriterMTU() int {
	return maxPayload
}

func (c *serverReuseConn[U]) Upstream() any {
	return c.session.Conn
}

var (
	_ N.ExtendedConn           = (*serverReuseConn[struct{}])(nil)
	_ N.ReadWaiter             = (*serverReuseConn[struct{}])(nil)
	_ N.VectorisedWriteCreator = (*serverReuseConn[struct{}])(nil)
	_ N.VectorisedWriter       = (*serverReuseVectorisedWriter[struct{}])(nil)
	_ N.WriteCloser            = (*serverReuseConn[struct{}])(nil)
)
