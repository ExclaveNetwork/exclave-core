//go:build !go1.27 || http2legacy

package http

import (
	"net/http"

	"golang.org/x/net/http2"
)

func h2RoundTrip(clientConn *http2.ClientConn, req *http.Request) (*http.Response, error) {
	return clientConn.RoundTrip(req)
}
