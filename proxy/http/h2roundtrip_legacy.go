//go:build !go1.27 || http2legacy

package http

import (
	"golang.org/x/net/http2"
)

type http2ClientConn struct {
	*http2.ClientConn
}

func newH2ClientConn(clientConn *http2.ClientConn, _ bool) *http2ClientConn {
	return &http2ClientConn{
		ClientConn: clientConn,
	}
}
