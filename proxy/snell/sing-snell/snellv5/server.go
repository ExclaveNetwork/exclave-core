package snellv5

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"math"
	"net"
	"sync"

	snell "github.com/sagernet/sing-snell"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	// snell-server v5.0.1: FUN_0014c020 initializes two 500000-entry Bloom generations
	// with false-positive probability 1e-10 for first-record salt replay detection.
	saltReplayCacheGenerationSize        = 500000
	saltReplayCacheFalsePositiveRate     = 1e-10
	saltReplayCacheMurmurHashDefaultSeed = 0x9747b28c
)

var errDuplicateSalt = E.New("snell: duplicated salt")

const (
	nonReusableEOFCode    = 0xff
	nonReusableEOFMessage = "end of file"
)

type saltReplayCache struct {
	access  sync.Mutex
	current int
	counts  [2]int
	filters [2]saltReplayBloom
}

type saltReplayBloom struct {
	bits      []byte
	bitCount  uint32
	hashCount int
}

func (b *saltReplayBloom) Initialize(expectedEntries int, falsePositiveRate float64) {
	bitsPerEntry := -math.Log(falsePositiveRate) / (math.Ln2 * math.Ln2)
	b.bitCount = uint32(float64(expectedEntries) * bitsPerEntry)
	b.hashCount = int(math.Ceil(bitsPerEntry * math.Ln2))
	b.bits = make([]byte, (b.bitCount+7)/8)
}

func (b *saltReplayBloom) Contains(data []byte) bool {
	firstHash := b.MurmurHash(data, saltReplayCacheMurmurHashDefaultSeed)
	hashStep := b.MurmurHash(data, firstHash)
	hashValue := firstHash
	for index := 0; index < b.hashCount; index++ {
		bitIndex := hashValue % b.bitCount
		if b.bits[bitIndex>>3]&(1<<(bitIndex&7)) == 0 {
			return false
		}
		hashValue += hashStep
	}
	return true
}

func (b *saltReplayBloom) Add(data []byte) {
	firstHash := b.MurmurHash(data, saltReplayCacheMurmurHashDefaultSeed)
	hashStep := b.MurmurHash(data, firstHash)
	hashValue := firstHash
	for index := 0; index < b.hashCount; index++ {
		bitIndex := hashValue % b.bitCount
		b.bits[bitIndex>>3] |= 1 << (bitIndex & 7)
		hashValue += hashStep
	}
}

func (b *saltReplayBloom) Clear() {
	clear(b.bits)
}

func (b *saltReplayBloom) MurmurHash(data []byte, seed uint32) uint32 {
	hashValue := seed ^ uint32(len(data))
	for len(data) >= 4 {
		chunk := binary.LittleEndian.Uint32(data)
		chunk *= 0x5bd1e995
		chunk ^= chunk >> 24
		chunk *= 0x5bd1e995
		hashValue *= 0x5bd1e995
		hashValue ^= chunk
		data = data[4:]
	}
	switch len(data) {
	case 3:
		hashValue ^= uint32(data[2]) << 16
		fallthrough
	case 2:
		hashValue ^= uint32(data[1]) << 8
		fallthrough
	case 1:
		hashValue ^= uint32(data[0])
		hashValue *= 0x5bd1e995
	}
	hashValue ^= hashValue >> 13
	hashValue *= 0x5bd1e995
	hashValue ^= hashValue >> 15
	return hashValue
}

func (c *saltReplayCache) CheckAndAdd(salt []byte) bool {
	c.access.Lock()
	defer c.access.Unlock()
	if c.filters[0].bits == nil {
		c.filters[0].Initialize(saltReplayCacheGenerationSize, saltReplayCacheFalsePositiveRate)
		c.filters[1].Initialize(saltReplayCacheGenerationSize, saltReplayCacheFalsePositiveRate)
	}
	if c.filters[0].Contains(salt) || c.filters[1].Contains(salt) {
		return true
	}
	c.filters[c.current].Add(salt)
	c.counts[c.current]++
	if c.counts[c.current] >= saltReplayCacheGenerationSize {
		c.counts[c.current] = 0
		c.current ^= 1
		c.filters[c.current].Clear()
	}
	return false
}

type Service struct {
	psk       []byte
	obfs      snell.ObfsConfig
	handler   snell.Handler
	saltCache saltReplayCache
}

type ServiceOptions struct {
	PSK      []byte
	ObfsMode snell.ObfsMode
	Handler  snell.Handler
}

func NewService(options ServiceOptions) (*Service, error) {
	if len(options.PSK) == 0 {
		return nil, snell.ErrMissingPSK
	}
	switch options.ObfsMode {
	case snell.ObfsModeNone, snell.ObfsModeHTTP:
	case snell.ObfsModeTLS:
	default:
		return nil, E.New("snell: unknown obfs mode: ", int(options.ObfsMode))
	}
	return &Service{
		psk:     options.PSK,
		obfs:    snell.ObfsConfig{Mode: options.ObfsMode},
		handler: options.Handler,
	}, nil
}

func (s *Service) NewConnection(ctx context.Context, conn net.Conn, source M.Socksaddr, onClose N.CloseHandlerFunc) error {
	err := s.newConnection(ctx, conn, source, onClose)
	if err != nil {
		return &snell.ServerError{Conn: conn, Source: source, Cause: err}
	}
	return nil
}

type MultiService[U comparable] struct {
	*Service
	users map[string]U
}

func NewMultiService[U comparable](options ServiceOptions) (*MultiService[U], error) {
	service, err := NewService(options)
	if err != nil {
		return nil, err
	}
	return &MultiService[U]{Service: service, users: make(map[string]U)}, nil
}

func (s *MultiService[U]) UpdateUsers(users []U, userKeys [][]byte) error {
	if len(users) != len(userKeys) {
		return E.New("snell: user/key count mismatch")
	}
	if len(users) == 0 {
		return snell.ErrNoUsers
	}
	userMap := make(map[string]U, len(users))
	for index, user := range users {
		key := userKeys[index]
		if len(key) == 0 {
			return snell.ErrBadUserKey
		}
		if len(key) > 255 {
			return E.New("snell: user key too long")
		}
		keyString := string(key)
		if _, loaded := userMap[keyString]; loaded {
			return snell.ErrDuplicateUserKey
		}
		userMap[keyString] = user
	}
	s.users = userMap
	return nil
}

func (s *MultiService[U]) NewConnection(ctx context.Context, conn net.Conn, source M.Socksaddr, onClose N.CloseHandlerFunc) error {
	err := s.newConnection(ctx, conn, source, onClose)
	if err != nil {
		return &snell.ServerError{Conn: conn, Source: source, Cause: err}
	}
	return nil
}

func (s *MultiService[U]) authenticate(ctx context.Context, request snell.Request) (context.Context, error) {
	user, loaded := s.users[string(request.ClientID)]
	if !loaded {
		return nil, snell.ErrBadUserKey
	}
	return auth.ContextWithUser(ctx, user), nil
}

func (s *Service) readRequest(record *buf.Buffer) (snell.Request, error) {
	prefix := make([]byte, 3)
	_, err := io.ReadFull(record, prefix)
	if err != nil {
		return snell.Request{}, err
	}
	if prefix[0] != snell.RequestVersion {
		return snell.Request{}, E.Extend(snell.ErrBadVersion, "request version ", prefix[0])
	}
	request := snell.Request{Command: prefix[1]}
	// snell-server v5.0.1: FUN_0013f630 handles SN_COMMAND_PING after the 3-byte
	// request prefix is read and aborts with PONG data, without consuming prefix[2]
	// client-id bytes.
	if request.Command == snell.CommandPing {
		return request, nil
	}
	clientIDLen := int(prefix[2])
	if clientIDLen > 0 {
		request.ClientID = make([]byte, clientIDLen)
		_, err = io.ReadFull(record, request.ClientID)
		if err != nil {
			return snell.Request{}, err
		}
	}
	switch request.Command {
	case snell.CommandConnect, snell.CommandConnectV2:
		request.Destination, err = snell.ReadConnectAddress(record)
		if err != nil {
			return snell.Request{}, err
		}
	case snell.CommandUDP:
	default:
		return snell.Request{}, E.Extend(snell.ErrUnsupportedCommand, request.Command)
	}
	return request, nil
}

func (s *Service) newConnection(ctx context.Context, conn net.Conn, source M.Socksaddr, onClose N.CloseHandlerFunc) error {
	conn = s.obfs.ServerConn(conn)
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(conn, salt)
	if err != nil {
		return err
	}
	key := snell.DeriveKey(s.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		return err
	}
	r := newReader(conn, aead, make([]byte, snell.NonceLen))
	record, err := r.ReadRecord()
	if err != nil {
		return E.Cause(err, "read request")
	}
	// snell-server v5.0.1: initial request path: checks the first record salt after decryption
	// and aborts with "Duplicated salt detected" when it was recently seen.
	if s.saltCache.CheckAndAdd(salt) {
		record.Release()
		return errDuplicateSalt
	}
	request, err := s.readRequest(record)
	if err != nil {
		record.Release()
		return E.Cause(err, "decode request")
	}

	switch request.Command {
	case snell.CommandConnect:
		serverConn := &serverConn{Conn: conn, service: s, reader: r}
		if record.IsEmpty() {
			record.Release()
		} else {
			r.SetCache(record)
		}
		s.handler.NewConnectionEx(ctx, serverConn, source, request.Destination, onClose)
		return nil
	case snell.CommandConnectV2:
		reuseSession := &serverReuseSession[struct{}]{Conn: conn, service: s, reader: r}
		return reuseSession.Serve(ctx, source, onClose, record, request)
	case snell.CommandUDP:
		record.Release()
		packetConn := &serverPacketConn{Conn: conn, service: s, reader: r}
		err = packetConn.writeTunnelReply()
		if err != nil {
			return err
		}
		s.handler.NewPacketConnectionEx(ctx, packetConn, source, M.Socksaddr{}, onClose)
		return nil
	case snell.CommandPing:
		record.Release()
		err = s.writePong(conn)
		closeErr := conn.Close()
		if err != nil {
			return err
		}
		return closeErr
	default:
		record.Release()
		return E.Extend(snell.ErrUnsupportedCommand, request.Command)
	}
}

func (s *MultiService[U]) newConnection(ctx context.Context, conn net.Conn, source M.Socksaddr, onClose N.CloseHandlerFunc) error {
	conn = s.obfs.ServerConn(conn)
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(conn, salt)
	if err != nil {
		return err
	}
	key := snell.DeriveKey(s.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		return err
	}
	r := newReader(conn, aead, make([]byte, snell.NonceLen))
	record, err := r.ReadRecord()
	if err != nil {
		return E.Cause(err, "read request")
	}
	// snell-server v5.0.1: initial request path: checks the first record salt after decryption
	// and aborts with "Duplicated salt detected" when it was recently seen.
	if s.saltCache.CheckAndAdd(salt) {
		record.Release()
		return errDuplicateSalt
	}
	request, err := s.readRequest(record)
	if err != nil {
		record.Release()
		return E.Cause(err, "decode request")
	}

	switch request.Command {
	case snell.CommandConnect:
		requestCtx, err := s.authenticate(ctx, request)
		if err != nil {
			record.Release()
			return err
		}
		serverConn := &serverConn{Conn: conn, service: s.Service, reader: r}
		if record.IsEmpty() {
			record.Release()
		} else {
			r.SetCache(record)
		}
		s.handler.NewConnectionEx(requestCtx, serverConn, source, request.Destination, onClose)
		return nil
	case snell.CommandConnectV2:
		reuseSession := &serverReuseSession[U]{Conn: conn, service: s.Service, multiService: s, reader: r}
		return reuseSession.Serve(ctx, source, onClose, record, request)
	case snell.CommandUDP:
		requestCtx, err := s.authenticate(ctx, request)
		if err != nil {
			record.Release()
			return err
		}
		record.Release()
		packetConn := &serverPacketConn{Conn: conn, service: s.Service, reader: r}
		err = packetConn.writeTunnelReply()
		if err != nil {
			return err
		}
		s.handler.NewPacketConnectionEx(requestCtx, packetConn, source, M.Socksaddr{}, onClose)
		return nil
	case snell.CommandPing:
		record.Release()
		err = s.writePong(conn)
		closeErr := conn.Close()
		if err != nil {
			return err
		}
		return closeErr
	default:
		record.Release()
		return E.Extend(snell.ErrUnsupportedCommand, request.Command)
	}
}

var (
	_ snell.Service = (*Service)(nil)
	_ snell.Service = (*MultiService[int])(nil)
)

type serverConn struct {
	net.Conn
	service *Service
	reader  *reader

	access sync.Mutex
	writer *writer

	closeWriteOnce sync.Once
	closeWriteErr  error
	closeOnce      sync.Once
	closeErr       error
}

func (c *serverConn) writeResponse(payload []byte) error {
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		return err
	}
	key := snell.DeriveKey(c.service.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, snell.NonceLen)
	w, err := newWriter(c.Conn, aead, nonce)
	if err != nil {
		return err
	}
	reply := buf.NewSize(1 + len(payload))
	common.Must(reply.WriteByte(snell.ReplyTunnel))
	common.Must1(reply.Write(payload))
	err = w.WriteFirst(salt, reply.Bytes())
	reply.Release()
	if err != nil {
		return E.Cause(err, "write reply")
	}
	c.writer = w
	return nil
}

func (c *serverConn) writeErrorResponse(code byte, messageText string) error {
	message := []byte(messageText)
	reply := buf.NewSize(3 + len(message))
	common.Must(reply.WriteByte(snell.ReplyError))
	common.Must(reply.WriteByte(code))
	common.Must(reply.WriteByte(byte(len(message))))
	common.Must1(reply.Write(message))
	defer reply.Release()
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		return err
	}
	key := snell.DeriveKey(c.service.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, snell.NonceLen)
	w, err := newWriter(c.Conn, aead, nonce)
	if err != nil {
		return err
	}
	err = w.WriteFirst(salt, reply.Bytes())
	if err != nil {
		return E.Cause(err, "write error reply")
	}
	c.writer = w
	return nil
}

func (c *serverConn) writeResponseBuffer(buffer *buf.Buffer) error {
	buffer.ExtendHeader(1)[0] = snell.ReplyTunnel
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		buffer.Release()
		return err
	}
	key := snell.DeriveKey(c.service.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		buffer.Release()
		return err
	}
	nonce := make([]byte, snell.NonceLen)
	writer, err := newWriter(c.Conn, aead, nonce)
	if err != nil {
		buffer.Release()
		return err
	}
	err = writer.WriteFirst(salt, buffer.Bytes())
	buffer.Release()
	if err != nil {
		return E.Cause(err, "write reply")
	}
	c.writer = writer
	return nil
}

func (c *serverConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *serverConn) ReadBuffer(buffer *buf.Buffer) error {
	return c.reader.ReadBuffer(buffer)
}

func (c *serverConn) Write(p []byte) (int, error) {
	if c.writer != nil {
		return c.writer.Write(p)
	}
	c.access.Lock()
	if c.writer != nil {
		c.access.Unlock()
		return c.writer.Write(p)
	}
	defer c.access.Unlock()
	err := c.writeResponse(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *serverConn) WriteBuffer(buffer *buf.Buffer) error {
	if c.writer != nil {
		return c.writer.WriteBuffer(buffer)
	}
	c.access.Lock()
	if c.writer != nil {
		c.access.Unlock()
		return c.writer.WriteBuffer(buffer)
	}
	defer c.access.Unlock()
	return c.writeResponseBuffer(buffer)
}

func (c *serverConn) CreateVectorisedWriter() (N.VectorisedWriter, bool) {
	upstreamWriter, created := bufio.CreateVectorisedWriter(c.Conn)
	if !created {
		return nil, false
	}
	return &serverVectorisedWriter{conn: c, upstream: upstreamWriter}, true
}

type serverVectorisedWriter struct {
	conn     *serverConn
	upstream N.VectorisedWriter
}

func (w *serverVectorisedWriter) WriteVectorised(buffers []*buf.Buffer) error {
	conn := w.conn
	if conn.writer != nil {
		return conn.writer.CreateVectorisedWriterFor(w.upstream).WriteVectorised(buffers)
	}
	conn.access.Lock()
	if conn.writer != nil {
		recordWriter := conn.writer
		conn.access.Unlock()
		return recordWriter.CreateVectorisedWriterFor(w.upstream).WriteVectorised(buffers)
	}
	for index, buffer := range buffers {
		if buffer.IsEmpty() {
			buffer.Release()
			continue
		}
		buffer.ExtendHeader(1)[0] = snell.ReplyTunnel
		salt := make([]byte, snell.SaltLen)
		_, err := io.ReadFull(rand.Reader, salt)
		if err != nil {
			conn.access.Unlock()
			buf.ReleaseMulti(buffers[index:])
			return err
		}
		key := snell.DeriveKey(conn.service.psk, salt)
		aead, err := snell.NewAEAD(key)
		if err != nil {
			conn.access.Unlock()
			buf.ReleaseMulti(buffers[index:])
			return err
		}
		nonce := make([]byte, snell.NonceLen)
		recordWriter, err := newWriter(conn.Conn, aead, nonce)
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
		conn.writer = recordWriter
		conn.access.Unlock()
		return nil
	}
	conn.access.Unlock()
	return nil
}

func (c *serverConn) CloseWrite() error {
	c.closeWriteOnce.Do(func() {
		c.access.Lock()
		defer c.access.Unlock()
		if c.writer == nil {
			// snell-server v5.0.1: FUN_0013f020 uses the generic network-error
			// path for command-1 EOF before the first response; UV_EOF maps to
			// ReplyError 0xff "end of file".
			c.closeWriteErr = c.writeErrorResponse(nonReusableEOFCode, nonReusableEOFMessage)
			closeErr := c.Close()
			if c.closeWriteErr == nil {
				c.closeWriteErr = closeErr
			}
			return
		}
		// snell-server v5.0.1: FUN_0013f020 closes non-reusable command-1 tunnels
		// on upstream EOF instead of writing a Snell zero chunk.
		c.closeWriteErr = c.Close()
	})
	return c.closeWriteErr
}

func (c *serverConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

func (c *serverConn) InitializeReadWaiter(options N.ReadWaitOptions) (needCopy bool) {
	return c.reader.InitializeReadWaiter(options)
}

func (c *serverConn) WaitReadBuffer() (*buf.Buffer, error) {
	return c.reader.WaitReadBuffer()
}

func (c *serverConn) FrontHeadroom() int {
	if c.writer == nil {
		return 1 + snell.SaltLen + snell.HeaderCipherLen + maxInitialPaddingLen
	}
	return snell.HeaderCipherLen
}

func (c *serverConn) RearHeadroom() int {
	return snell.AEADTagLen
}

func (c *serverConn) WriterMTU() int {
	return maxPayload
}

func (c *serverConn) Upstream() any {
	return c.Conn
}

func (s *Service) writePong(conn net.Conn) error {
	salt := make([]byte, snell.SaltLen)
	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		return err
	}
	key := snell.DeriveKey(s.psk, salt)
	aead, err := snell.NewAEAD(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, snell.NonceLen)
	w, err := newWriter(conn, aead, nonce)
	if err != nil {
		return err
	}
	pong := [1]byte{snell.ReplyPong}
	return w.WriteFirst(salt, pong[:])
}

var (
	_ N.ExtendedConn           = (*serverConn)(nil)
	_ N.ReadWaiter             = (*serverConn)(nil)
	_ N.VectorisedWriteCreator = (*serverConn)(nil)
	_ N.VectorisedWriter       = (*serverVectorisedWriter)(nil)
	_ N.WriteCloser            = (*serverConn)(nil)
)
