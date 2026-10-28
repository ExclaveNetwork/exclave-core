//go:build !go1.27 || http2legacy

package masque

import (
	"net"

	"golang.org/x/net/http2"
)

type http2ClientConn struct {
	*http2.ClientConn
	rawConn net.Conn
}

func newH2ClientConn(clientConn *http2.ClientConn, rawConn net.Conn) *http2ClientConn {
	return &http2ClientConn{
		ClientConn: clientConn,
		rawConn:    rawConn,
	}
}

func (c *http2ClientConn) close() {
	c.Close()
	c.rawConn.Close()
}
