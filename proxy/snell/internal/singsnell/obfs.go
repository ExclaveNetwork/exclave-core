package snell

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
)

type ObfsMode int

const (
	ObfsModeNone ObfsMode = iota
	ObfsModeHTTP
	ObfsModeTLS
)

const (
	// Surge 6.7.0 (11520): SGObfsHelper::initWithPolicy: uses these legacy obfs default hosts.
	DefaultObfsHost    = "bing.com"
	DefaultTLSObfsHost = "cloudfront.net"
)

const (
	// Surge 6.7.0 (11520): SGObfsHelperTLS::encodeObfsData: embeds at most 0x400 bytes in the ClientHello and chunks later records at 0x4000.
	tlsObfsFirstClientPayloadLen = 0x400
	tlsObfsRecordPayloadLen      = 0x4000
)

var (
	httpObfsClientFingerprintOnce sync.Once
	httpObfsClientFingerprintErr  error
	httpObfsClientKey             string
	httpObfsClientUserAgent       string

	httpObfsServerResponseOnce sync.Once
	httpObfsServerResponseErr  error
	httpObfsServerResponse     []byte
)

type ObfsConfig struct {
	Mode ObfsMode
	Host string
}

func ParseObfsMode(name string) (ObfsMode, error) {
	switch strings.ToLower(name) {
	case "", "none":
		return ObfsModeNone, nil
	case "http":
		return ObfsModeHTTP, nil
	case "tls":
		return ObfsModeTLS, nil
	default:
		return 0, E.New("snell: unknown obfs mode: ", name)
	}
}

func (m ObfsMode) String() string {
	switch m {
	case ObfsModeNone:
		return "none"
	case ObfsModeHTTP:
		return "http"
	case ObfsModeTLS:
		return "tls"
	default:
		panic("snell: invalid obfs mode")
	}
}

func (c ObfsConfig) ClientConn(conn net.Conn) net.Conn {
	switch c.Mode {
	case ObfsModeNone:
		return conn
	case ObfsModeHTTP:
		return &httpObfsClientConn{Conn: conn, config: c}
	case ObfsModeTLS:
		return &tlsObfsClientConn{Conn: conn, config: c, firstResponse: true}
	default:
		panic("snell: invalid obfs mode")
	}
}

func (c ObfsConfig) ServerConn(conn net.Conn) net.Conn {
	switch c.Mode {
	case ObfsModeNone:
		return conn
	case ObfsModeHTTP:
		return &httpObfsServerConn{Conn: conn, config: c}
	case ObfsModeTLS:
		return &tlsObfsServerConn{Conn: conn, config: c, firstRequest: true, firstResponse: true}
	default:
		panic("snell: invalid obfs mode")
	}
}

func (c ObfsConfig) WriteClientRequest(w io.Writer) error {
	return c.WriteClientRequestWithPayload(w, nil)
}

func (c ObfsConfig) WriteClientRequestWithPayload(w io.Writer, payload []byte) error {
	switch c.Mode {
	case ObfsModeHTTP:
		httpObfsClientFingerprintOnce.Do(func() {
			var keyBytes [16]byte
			_, readErr := io.ReadFull(rand.Reader, keyBytes[:])
			if readErr != nil {
				httpObfsClientFingerprintErr = E.Cause(readErr, "generate http obfs websocket key")
				return
			}
			osMinorDelta, randomErr := rand.Int(rand.Reader, big.NewInt(6))
			if randomErr != nil {
				httpObfsClientFingerprintErr = E.Cause(randomErr, "generate http obfs user agent")
				return
			}
			firefoxVersionDelta, randomErr := rand.Int(rand.Reader, big.NewInt(43))
			if randomErr != nil {
				httpObfsClientFingerprintErr = E.Cause(randomErr, "generate http obfs user agent")
				return
			}
			httpObfsClientKey = base64.StdEncoding.EncodeToString(keyBytes[:])
			// Surge 6.7.0 (11520): SNObfsHelperHTTP init creates one Firefox-style user agent per helper class lifetime.
			httpObfsClientUserAgent = fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10.%d; rv:64.0) Gecko/20100101 Firefox/%d.0", int(osMinorDelta.Int64())+9, int(firefoxVersionDelta.Int64())+22)
		})
		if httpObfsClientFingerprintErr != nil {
			return httpObfsClientFingerprintErr
		}
		host := c.Host
		if host == "" {
			host = DefaultObfsHost
		}
		var request []byte
		if len(payload) == 0 {
			request = fmt.Appendf(request, "GET / HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\n\r\n", host, httpObfsClientUserAgent, httpObfsClientKey)
		} else {
			request = fmt.Appendf(request, "GET / HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nContent-Length: %d\r\nSec-WebSocket-Key: %s\r\n\r\n", host, httpObfsClientUserAgent, len(payload), httpObfsClientKey)
			request = append(request, payload...)
		}
		_, err := w.Write(request)
		if err != nil {
			return E.Cause(err, "write http obfs request")
		}
		return nil
	case ObfsModeTLS:
		host := c.Host
		if host == "" {
			host = DefaultTLSObfsHost
		}
		firstPayload := payload
		if len(firstPayload) > tlsObfsFirstClientPayloadLen {
			firstPayload = payload[:tlsObfsFirstClientPayloadLen]
		}
		random := make([]byte, 28)
		_, err := io.ReadFull(rand.Reader, random)
		if err != nil {
			return err
		}
		sessionID := make([]byte, 32)
		_, err = io.ReadFull(rand.Reader, sessionID)
		if err != nil {
			return err
		}
		// Surge 6.7.0 (11520): SGObfsHelperTLS::encodeObfsData: sends the first Snell record in the ClientHello
		// session_ticket extension and caps that embedded payload at 0x400 bytes.
		out := make([]byte, 0, 0xd9+len(firstPayload)+len(host))
		out = append(out, 0x16, 0x03, 0x01)
		out = binary.BigEndian.AppendUint16(out, uint16(212+len(firstPayload)+len(host)))
		out = append(out, 0x01, 0x00)
		out = binary.BigEndian.AppendUint16(out, uint16(208+len(firstPayload)+len(host)))
		out = append(out, 0x03, 0x03)
		out = binary.BigEndian.AppendUint32(out, uint32(time.Now().Unix()))
		out = append(out, random...)
		out = append(out, 0x20)
		out = append(out, sessionID...)
		out = binary.BigEndian.AppendUint16(out, 0x0038)
		out = append(out,
			0xc0, 0x2c, 0xc0, 0x30, 0x00, 0x9f, 0xcc, 0xa9, 0xcc, 0xa8, 0xcc, 0xaa, 0xc0, 0x2b, 0xc0, 0x2f,
			0x00, 0x9e, 0xc0, 0x24, 0xc0, 0x28, 0x00, 0x6b, 0xc0, 0x23, 0xc0, 0x27, 0x00, 0x67, 0xc0, 0x0a,
			0xc0, 0x14, 0x00, 0x39, 0xc0, 0x09, 0xc0, 0x13, 0x00, 0x33, 0x00, 0x9d, 0x00, 0x9c, 0x00, 0x3d,
			0x00, 0x3c, 0x00, 0x35, 0x00, 0x2f, 0x00, 0xff,
		)
		out = append(out, 0x01, 0x00)
		out = binary.BigEndian.AppendUint16(out, uint16(79+len(firstPayload)+len(host)))
		out = binary.BigEndian.AppendUint16(out, 0x0023)
		out = binary.BigEndian.AppendUint16(out, uint16(len(firstPayload)))
		out = append(out, firstPayload...)
		out = binary.BigEndian.AppendUint16(out, 0x0000)
		out = binary.BigEndian.AppendUint16(out, uint16(len(host)+5))
		out = binary.BigEndian.AppendUint16(out, uint16(len(host)+3))
		out = append(out, 0x00)
		out = binary.BigEndian.AppendUint16(out, uint16(len(host)))
		out = append(out, host...)
		out = append(out,
			0x00, 0x0b, 0x00, 0x04, 0x03, 0x01, 0x00, 0x02,
			0x00, 0x0a, 0x00, 0x0a, 0x00, 0x08, 0x00, 0x1d, 0x00, 0x17, 0x00, 0x19, 0x00, 0x18,
			0x00, 0x0d, 0x00, 0x20, 0x00, 0x1e, 0x06, 0x01, 0x06, 0x02, 0x06, 0x03, 0x05,
			0x01, 0x05, 0x02, 0x05, 0x03, 0x04, 0x01, 0x04, 0x02, 0x04, 0x03, 0x03, 0x01,
			0x03, 0x02, 0x03, 0x03, 0x02, 0x01, 0x02, 0x02, 0x02, 0x03,
			0x00, 0x16, 0x00, 0x00,
			0x00, 0x17, 0x00, 0x00,
		)
		_, err = w.Write(out)
		if err != nil {
			return E.Cause(err, "write tls obfs request")
		}
		for payload = payload[len(firstPayload):]; len(payload) > 0; {
			payloadLen := min(len(payload), tlsObfsRecordPayloadLen)
			record := make([]byte, 0, 5+payloadLen)
			record = append(record, 0x17, 0x03, 0x03)
			record = binary.BigEndian.AppendUint16(record, uint16(payloadLen))
			record = append(record, payload[:payloadLen]...)
			_, err = w.Write(record)
			if err != nil {
				return E.Cause(err, "write tls obfs request payload")
			}
			payload = payload[payloadLen:]
		}
		return nil
	case ObfsModeNone:
		if len(payload) == 0 {
			return nil
		}
		_, err := w.Write(payload)
		if err != nil {
			return E.Cause(err, "write obfs payload")
		}
		return nil
	default:
		panic("snell: invalid obfs mode")
	}
}

func (c ObfsConfig) ReadClientResponse(r io.Reader) (io.Reader, error) {
	switch c.Mode {
	case ObfsModeNone, ObfsModeTLS:
		return r, nil
	case ObfsModeHTTP:
		reader := bufio.NewReader(r)
		terminator := [4]byte{'\r', '\n', '\r', '\n'}
		matched := 0
		for {
			value, err := reader.ReadByte()
			if err != nil {
				return nil, E.Cause(err, "read http obfs response")
			}
			if value == terminator[matched] {
				matched++
				if matched == len(terminator) {
					return reader, nil
				}
				continue
			}
			if value == terminator[0] {
				matched = 1
			} else {
				matched = 0
			}
		}
	default:
		panic("snell: invalid obfs mode")
	}
}

func (c ObfsConfig) ReadServerRequest(r io.Reader) (io.Reader, error) {
	switch c.Mode {
	case ObfsModeNone, ObfsModeTLS:
		return r, nil
	case ObfsModeHTTP:
		reader := bufio.NewReader(r)
		terminator := [4]byte{'\r', '\n', '\r', '\n'}
		matched := 0
		for {
			value, err := reader.ReadByte()
			if err != nil {
				return nil, E.Cause(err, "read http obfs request")
			}
			if value == terminator[matched] {
				matched++
				if matched == len(terminator) {
					return reader, nil
				}
				continue
			}
			if value == terminator[0] {
				matched = 1
			} else {
				matched = 0
			}
		}
	default:
		panic("snell: invalid obfs mode")
	}
}

func (c ObfsConfig) WriteServerResponse(w io.Writer) error {
	return c.WriteServerResponseWithPayload(w, nil)
}

func (c ObfsConfig) WriteServerResponseWithPayload(w io.Writer, payload []byte) error {
	switch c.Mode {
	case ObfsModeHTTP:
		httpObfsServerResponseOnce.Do(func() {
			var acceptBytes [16]byte
			_, readErr := io.ReadFull(rand.Reader, acceptBytes[:])
			if readErr != nil {
				httpObfsServerResponseErr = E.Cause(readErr, "generate http obfs accept")
				return
			}
			versionMinorDelta, randomErr := rand.Int(rand.Reader, big.NewInt(14))
			if randomErr != nil {
				httpObfsServerResponseErr = E.Cause(randomErr, "generate http obfs server version")
				return
			}
			versionPatchDelta, randomErr := rand.Int(rand.Reader, big.NewInt(12))
			if randomErr != nil {
				httpObfsServerResponseErr = E.Cause(randomErr, "generate http obfs server version")
				return
			}
			accept := base64.StdEncoding.EncodeToString(acceptBytes[:])
			// Surge 6.7.0 (11520): SNObfsHelperServer initializes and reuses one fake 101 response.
			httpObfsServerResponse = fmt.Appendf(nil, "HTTP/1.1 101 Switching Protocols\r\nServer: nginx/1.%d.%d\r\nDate: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", int(versionMinorDelta.Int64()), int(versionPatchDelta.Int64()), time.Now().Format("Mon, 02 Jan 2006 15:04:05 GMT"), accept)
		})
		if httpObfsServerResponseErr != nil {
			return httpObfsServerResponseErr
		}
		response := append([]byte(nil), httpObfsServerResponse...)
		if len(payload) > 0 {
			response = append(response, payload...)
		}
		_, err := w.Write(response)
		if err != nil {
			return E.Cause(err, "write http obfs response")
		}
		return nil
	case ObfsModeTLS:
		random := make([]byte, 28)
		_, err := io.ReadFull(rand.Reader, random)
		if err != nil {
			return err
		}
		sessionID := make([]byte, 32)
		_, err = io.ReadFull(rand.Reader, sessionID)
		if err != nil {
			return err
		}
		firstPayload := payload
		if len(firstPayload) > tlsObfsRecordPayloadLen {
			firstPayload = payload[:tlsObfsRecordPayloadLen]
		}
		// Surge 6.7.0 (11520): SGObfsHelperTLS::decodeObfsData: expects this fake ServerHello, a CCS record,
		// then the first Snell payload in a TLS handshake record.
		out := make([]byte, 0, 107+len(firstPayload))
		out = append(out, 0x16, 0x03, 0x01)
		out = binary.BigEndian.AppendUint16(out, 91)
		out = append(out, 0x02, 0x00, 0x00, 0x57, 0x03, 0x03)
		out = binary.BigEndian.AppendUint32(out, uint32(time.Now().Unix()))
		out = append(out, random...)
		out = append(out, 0x20)
		out = append(out, sessionID...)
		out = append(out,
			0xcc, 0xa8, 0x00,
			0x00, 0x00,
			0xff, 0x01, 0x00, 0x01, 0x00,
			0x00, 0x17, 0x00, 0x00,
			0x00, 0x0b, 0x00, 0x02, 0x01, 0x00,
			0x14, 0x03, 0x03, 0x00, 0x01, 0x01,
			0x16, 0x03, 0x03,
		)
		out = binary.BigEndian.AppendUint16(out, uint16(len(firstPayload)))
		out = append(out, firstPayload...)
		_, err = w.Write(out)
		if err != nil {
			return E.Cause(err, "write tls obfs response")
		}
		for payload = payload[len(firstPayload):]; len(payload) > 0; {
			payloadLen := min(len(payload), tlsObfsRecordPayloadLen)
			record := make([]byte, 0, 5+payloadLen)
			record = append(record, 0x17, 0x03, 0x03)
			record = binary.BigEndian.AppendUint16(record, uint16(payloadLen))
			record = append(record, payload[:payloadLen]...)
			_, err = w.Write(record)
			if err != nil {
				return E.Cause(err, "write tls obfs response payload")
			}
			payload = payload[payloadLen:]
		}
		return nil
	case ObfsModeNone:
		if len(payload) == 0 {
			return nil
		}
		_, err := w.Write(payload)
		if err != nil {
			return E.Cause(err, "write obfs payload")
		}
		return nil
	default:
		panic("snell: invalid obfs mode")
	}
}

type httpObfsClientConn struct {
	net.Conn
	config ObfsConfig

	readAccess  sync.Mutex
	writeAccess sync.Mutex
	reader      io.Reader
	wrote       bool
}

func (c *httpObfsClientConn) Read(p []byte) (int, error) {
	c.readAccess.Lock()
	if c.reader == nil {
		reader, err := c.config.ReadClientResponse(c.Conn)
		if err != nil {
			c.readAccess.Unlock()
			return 0, err
		}
		c.reader = reader
	}
	reader := c.reader
	c.readAccess.Unlock()
	return reader.Read(p)
}

func (c *httpObfsClientConn) Write(p []byte) (int, error) {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	if !c.wrote {
		c.wrote = true
		err := c.config.WriteClientRequestWithPayload(c.Conn, p)
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return c.Conn.Write(p)
}

type httpObfsServerConn struct {
	net.Conn
	config ObfsConfig

	readAccess  sync.Mutex
	writeAccess sync.Mutex
	reader      io.Reader
	wrote       bool
}

func (c *httpObfsServerConn) Read(p []byte) (int, error) {
	c.readAccess.Lock()
	if c.reader == nil {
		reader, err := c.config.ReadServerRequest(c.Conn)
		if err != nil {
			c.readAccess.Unlock()
			return 0, err
		}
		c.reader = reader
	}
	reader := c.reader
	c.readAccess.Unlock()
	return reader.Read(p)
}

func (c *httpObfsServerConn) Write(p []byte) (int, error) {
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	if !c.wrote {
		c.wrote = true
		err := c.config.WriteServerResponseWithPayload(c.Conn, p)
		if err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return c.Conn.Write(p)
}

type tlsObfsClientConn struct {
	net.Conn
	config ObfsConfig

	readAccess    sync.Mutex
	writeAccess   sync.Mutex
	readRemaining int
	firstResponse bool
	wrote         bool
}

func (c *tlsObfsClientConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	if c.readRemaining > 0 {
		payloadLen := min(c.readRemaining, len(p))
		n, err := io.ReadFull(c.Conn, p[:payloadLen])
		c.readRemaining -= n
		return n, err
	}
	if c.firstResponse {
		c.firstResponse = false
		return c.readRecordPayload(p, 105)
	}
	return c.readRecordPayload(p, 3)
}

func (c *tlsObfsClientConn) readRecordPayload(p []byte, discardLen int) (int, error) {
	_, err := io.CopyN(io.Discard, c.Conn, int64(discardLen))
	if err != nil {
		return 0, err
	}
	var lengthBytes [2]byte
	_, err = io.ReadFull(c.Conn, lengthBytes[:])
	if err != nil {
		return 0, err
	}
	payloadLen := int(binary.BigEndian.Uint16(lengthBytes[:]))
	if payloadLen == 0 {
		return 0, nil
	}
	if payloadLen > len(p) {
		n, err := io.ReadFull(c.Conn, p)
		c.readRemaining = payloadLen - n
		return n, err
	}
	return io.ReadFull(c.Conn, p[:payloadLen])
}

func (c *tlsObfsClientConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	written := 0
	if !c.wrote {
		c.wrote = true
		firstPayload := p
		if len(firstPayload) > tlsObfsFirstClientPayloadLen {
			firstPayload = p[:tlsObfsFirstClientPayloadLen]
		}
		err := c.config.WriteClientRequestWithPayload(c.Conn, firstPayload)
		if err != nil {
			return 0, err
		}
		written += len(firstPayload)
		p = p[len(firstPayload):]
	}
	for len(p) > 0 {
		payloadLen := min(len(p), tlsObfsRecordPayloadLen)
		record := make([]byte, 0, 5+payloadLen)
		record = append(record, 0x17, 0x03, 0x03)
		record = binary.BigEndian.AppendUint16(record, uint16(payloadLen))
		record = append(record, p[:payloadLen]...)
		_, err := c.Conn.Write(record)
		if err != nil {
			return written, E.Cause(err, "write tls obfs payload")
		}
		written += payloadLen
		p = p[payloadLen:]
	}
	return written, nil
}

type tlsObfsServerConn struct {
	net.Conn
	config ObfsConfig

	readAccess        sync.Mutex
	writeAccess       sync.Mutex
	readRemaining     int
	firstRequest      bool
	sessionTicketDone bool
	firstResponse     bool
}

func (c *tlsObfsServerConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.readAccess.Lock()
	defer c.readAccess.Unlock()
	if c.readRemaining > 0 {
		payloadLen := min(c.readRemaining, len(p))
		n, err := io.ReadFull(c.Conn, p[:payloadLen])
		c.readRemaining -= n
		return n, err
	}
	if c.firstRequest {
		c.firstRequest = false
		return c.readRecordPayload(p, 9*16-4)
	}
	if !c.sessionTicketDone {
		c.sessionTicketDone = true
		_, err := io.CopyN(io.Discard, c.Conn, 7)
		if err != nil {
			return 0, err
		}
		var lengthBytes [2]byte
		_, err = io.ReadFull(c.Conn, lengthBytes[:])
		if err != nil {
			return 0, err
		}
		_, err = io.CopyN(io.Discard, c.Conn, int64(binary.BigEndian.Uint16(lengthBytes[:])))
		if err != nil {
			return 0, err
		}
		_, err = io.CopyN(io.Discard, c.Conn, 4*16+2)
		if err != nil {
			return 0, err
		}
	}
	return c.readRecordPayload(p, 3)
}

func (c *tlsObfsServerConn) readRecordPayload(p []byte, discardLen int) (int, error) {
	_, err := io.CopyN(io.Discard, c.Conn, int64(discardLen))
	if err != nil {
		return 0, err
	}
	var lengthBytes [2]byte
	_, err = io.ReadFull(c.Conn, lengthBytes[:])
	if err != nil {
		return 0, err
	}
	payloadLen := int(binary.BigEndian.Uint16(lengthBytes[:]))
	if payloadLen == 0 {
		return 0, nil
	}
	if payloadLen > len(p) {
		n, err := io.ReadFull(c.Conn, p)
		c.readRemaining = payloadLen - n
		return n, err
	}
	return io.ReadFull(c.Conn, p[:payloadLen])
}

func (c *tlsObfsServerConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	c.writeAccess.Lock()
	defer c.writeAccess.Unlock()
	written := 0
	if c.firstResponse {
		c.firstResponse = false
		firstPayload := p
		if len(firstPayload) > tlsObfsRecordPayloadLen {
			firstPayload = p[:tlsObfsRecordPayloadLen]
		}
		err := c.config.WriteServerResponseWithPayload(c.Conn, firstPayload)
		if err != nil {
			return 0, err
		}
		written += len(firstPayload)
		p = p[len(firstPayload):]
	}
	for len(p) > 0 {
		payloadLen := min(len(p), tlsObfsRecordPayloadLen)
		record := make([]byte, 0, 5+payloadLen)
		record = append(record, 0x17, 0x03, 0x03)
		record = binary.BigEndian.AppendUint16(record, uint16(payloadLen))
		record = append(record, p[:payloadLen]...)
		_, err := c.Conn.Write(record)
		if err != nil {
			return written, E.Cause(err, "write tls obfs payload")
		}
		written += payloadLen
		p = p[payloadLen:]
	}
	return written, nil
}
