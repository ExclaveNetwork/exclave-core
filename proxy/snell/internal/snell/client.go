/*
 * Based on opensnell (GPL-3.0-or-later).
 * Client dialer for Snell v4/v5 used by exclave-core outbound.
 */
package snell

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/obfs"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/socks5"
)

// ClientOptions configures a Snell client.
type ClientOptions struct {
	// Server is host:port of the snell-server.
	Server string
	// PSK is the pre-shared key (raw UTF-8 string, not base64).
	PSK string
	// Obfs is "off", "http", or "tls". Empty defaults to off.
	Obfs string
	// Version is 4 or 5. Zero defaults to 4. v5 is wire-compatible with v4
	// for TCP/UDP-over-TCP (dynamic record sizing is negotiated in-band).
	Version int
	// Reuse enables CommandConnectV2 connection reuse when supported.
	Reuse bool
	// Dialer dials the underlying TCP connection. If nil, net.Dialer is used.
	Dialer func(ctx context.Context, network, address string) (net.Conn, error)
}

// Client is a Snell v4/v5 client with optional connection pooling for reuse.
type Client struct {
	opts ClientOptions
	psk  []byte
	pool *Pool
}

// NewClient creates a Snell client. Call DialTCP / ListenPacket for traffic.
func NewClient(opts ClientOptions) (*Client, error) {
	if opts.Server == "" {
		return nil, fmt.Errorf("snell: empty server")
	}
	if opts.PSK == "" {
		return nil, fmt.Errorf("snell: empty psk")
	}
	if opts.Version == 0 {
		opts.Version = 4
	}
	if opts.Version != 4 && opts.Version != 5 {
		return nil, fmt.Errorf("snell: unsupported version %d (want 4 or 5)", opts.Version)
	}
	if opts.Dialer == nil {
		var d net.Dialer
		opts.Dialer = d.DialContext
	}
	c := &Client{opts: opts, psk: []byte(opts.PSK)}
	if opts.Reuse {
		c.pool = NewPool(c.dialSnell)
	}
	return c, nil
}

func (c *Client) dialRaw(ctx context.Context) (net.Conn, error) {
	conn, err := c.opts.Dialer(ctx, "tcp", c.opts.Server)
	if err != nil {
		return nil, err
	}
	host, portStr, err := net.SplitHostPort(c.opts.Server)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	// Normalize port for http obfs Host header.
	if _, err := strconv.Atoi(portStr); err != nil {
		_ = conn.Close()
		return nil, err
	}
	obfsConn, err := obfs.NewClient(conn, host, portStr, c.opts.Obfs)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return obfsConn, nil
}

func (c *Client) dialSnell(ctx context.Context) (*Snell, error) {
	raw, err := c.dialRaw(ctx)
	if err != nil {
		return nil, err
	}
	return StreamConn(raw, c.psk), nil
}

// DialTCP opens a TCP CONNECT tunnel to host:port.
func (c *Client) DialTCP(ctx context.Context, host string, port uint16) (net.Conn, error) {
	var conn net.Conn
	var err error
	if c.pool != nil {
		conn, err = c.pool.GetContext(ctx)
	} else {
		var s *Snell
		s, err = c.dialSnell(ctx)
		if err == nil {
			conn = s
		}
	}
	if err != nil {
		return nil, err
	}
	if err := WriteHeader(conn, host, port, c.opts.Reuse); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// ListenPacket opens a UDP-over-TCP association.
func (c *Client) ListenPacket(ctx context.Context) (net.PacketConn, error) {
	s, err := c.dialSnell(ctx)
	if err != nil {
		return nil, err
	}
	if err := WriteUDPHeader(s); err != nil {
		_ = s.Close()
		return nil, err
	}
	// Consume the server reply before treating the stream as packet-framed.
	if err := s.ReadReply(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return PacketConn(s), nil
}

// DialPacketConn is like ListenPacket but stamps a fixed target so Write can
// omit the destination (WriteTo still works with arbitrary addrs).
func (c *Client) DialPacketConn(ctx context.Context, target net.Addr) (net.PacketConn, error) {
	pc, err := c.ListenPacket(ctx)
	if err != nil {
		return nil, err
	}
	return &fixedTargetPacketConn{PacketConn: pc, target: target}, nil
}

type fixedTargetPacketConn struct {
	net.PacketConn
	target net.Addr
}

func (c *fixedTargetPacketConn) Write(b []byte) (int, error) {
	return c.PacketConn.WriteTo(b, c.target)
}

func (c *fixedTargetPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if addr == nil {
		addr = c.target
	}
	return c.PacketConn.WriteTo(b, addr)
}

// EncodeSocksAddr builds a SOCKS5 address for WritePacket callers.
func EncodeSocksAddr(host string, port uint16) []byte {
	return socks5.ParseAddr(net.JoinHostPort(host, strconv.Itoa(int(port))))
}

// Close releases pooled connections.
func (c *Client) Close() error {
	// Pool has no explicit close; idle conns expire by maxAge.
	return nil
}
