//go:build !go1.28 && !http2legacy

package masque

import (
	"net"
	"net/http"
	"reflect"
	"sync"
	"unsafe"

	"golang.org/x/net/http2"
)

var _ http.RoundTripper = (*http2ClientConn)(nil)

var (
	initOnce                sync.Once
	httpClientConnOffset    uintptr
	genericClientConnType   reflect.Type
	genericClientConnOffset uintptr
)

type http2ClientConn struct {
	h2ClientConn *http2.ClientConn
	rawConn      net.Conn
	initOnce     sync.Once
	roundTripper http.RoundTripper
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
	if req.Header.Get(":protocol") != "connect-ip" {
		return c.h2ClientConn.RoundTrip(req)
	}
	initOnce.Do(func() {
		field, _ := reflect.TypeFor[http2.ClientConn]().FieldByName("cc")
		httpClientConnOffset = field.Offset
		field, _ = reflect.TypeFor[http.ClientConn]().FieldByName("cc")
		genericClientConnType = field.Type
		genericClientConnOffset = field.Offset
	})
	c.initOnce.Do(func() {
		httpClientConn := *(**http.ClientConn)(unsafe.Add(unsafe.Pointer(c.h2ClientConn), httpClientConnOffset))
		c.roundTripper = reflect.NewAt(genericClientConnType, unsafe.Add(unsafe.Pointer(httpClientConn), genericClientConnOffset)).Elem().Interface().(http.RoundTripper)
	})
	// https://github.com/golang/go/blob/8dfc83de2c7b611e7525f09fd51616f180bf4d9b/src/net/http/clientconn.go#L273-L276
	// Pseudo header ":protocol" is not allowed in net/http, and the header validation needs to be bypassed.
	return c.roundTripper.RoundTrip(req)
}
