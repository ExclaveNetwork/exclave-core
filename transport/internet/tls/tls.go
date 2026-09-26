package tls

import (
	"context"
	"crypto/tls"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
)

//go:generate go run github.com/exclavenetwork/exclave-core/v5/common/errors/errorgen

type Conn struct {
	*tls.Conn
}

func (c *Conn) GetConnectionApplicationProtocol() (string, error) {
	if err := c.Handshake(); err != nil {
		return "", err
	}
	return c.ConnectionState().NegotiatedProtocol, nil
}

// Client initiates a TLS client handshake on the given connection.
func Client(c net.Conn, config *tls.Config) *Conn {
	tlsConn := tls.Client(c, config)
	return &Conn{Conn: tlsConn}
}

// Server initiates a TLS server handshake on the given connection.
func Server(c net.Conn, config *tls.Config) net.Conn {
	tlsConn := tls.Server(c, config)
	return &Conn{Conn: tlsConn}
}

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return NewTLSSecurityEngineFromConfig(ctx, config.(*Config))
	}))
}
