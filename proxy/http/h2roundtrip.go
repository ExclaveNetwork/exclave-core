//go:build go1.28 || http2legacy

package http

import (
	"net"
	"net/http"

	"golang.org/x/net/http2"
)

var _ http.RoundTripper = (*http2ClientConn)(nil)

type http2ClientConn struct {
	h2ClientConn *http2.ClientConn
	rawConn      net.Conn
}

func newH2ClientConn(clientConn *http2.ClientConn, rawConn net.Conn) *http2ClientConn {
	return &http2ClientConn{
		h2ClientConn: clientConn,
		rawConn:      rawConn,
	}
}

func (c *http2ClientConn) canTakeNewRequest() bool {
	return c.h2ClientConn.CanTakeNewRequest()
}

func (c *http2ClientConn) close() {
	c.h2ClientConn.Close()
	c.rawConn.Close()
}

func (c *http2ClientConn) RoundTrip(req *http.Request) (*http.Response, error) {
	return c.h2ClientConn.RoundTrip(req)
}
