//go:build go1.27 && !http2legacy

package http

import (
	"net/http"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/net/http2"
)

var (
	initOffsetOnce      sync.Once
	ccOffset            uintptr
	muOffset            uintptr
	closingOffset       uintptr
	closedOffset        uintptr
	roundTripsOffset    uintptr
	reservedOffset      uintptr
	startingOffset      uintptr
	pendingOffset       uintptr
	maxConcurrentOffset uintptr
	lastIdleOffset      uintptr
	shutdowncOffset     uintptr
	gcType              reflect.Type
	gcOffset            uintptr
)

type http2ClientConn struct {
	*http2.ClientConn
	initOffsetOnce sync.Once
	cc             *http.ClientConn
	mu             *sync.Mutex
	closing        *bool
	closed         *bool
	roundTrips     *int
	reserved       *int
	starting       *int
	pending        *int
	maxConcurrent  *int
	lastIdle       *time.Time
	shutdownc      *chan struct{}
	roundTripper   http.RoundTripper
}

func newH2ClientConn(clientConn *http2.ClientConn) *http2ClientConn {
	return &http2ClientConn{
		ClientConn: clientConn,
	}
}

func (c *http2ClientConn) initOffset() {
	initOffsetOnce.Do(func() {
		t := reflect.TypeFor[http2.ClientConn]()
		field, _ := t.FieldByName("cc")
		ccOffset = field.Offset
		field, _ = t.FieldByName("mu")
		muOffset = field.Offset
		field, _ = t.FieldByName("closing")
		closingOffset = field.Offset
		field, _ = t.FieldByName("closed")
		closedOffset = field.Offset
		field, _ = t.FieldByName("roundTrips")
		roundTripsOffset = field.Offset
		field, _ = t.FieldByName("reserved")
		reservedOffset = field.Offset
		field, _ = t.FieldByName("starting")
		startingOffset = field.Offset
		field, _ = t.FieldByName("pending")
		pendingOffset = field.Offset
		field, _ = t.FieldByName("maxConcurrent")
		maxConcurrentOffset = field.Offset
		field, _ = t.FieldByName("lastIdle")
		lastIdleOffset = field.Offset
		field, _ = t.FieldByName("shutdownc")
		shutdowncOffset = field.Offset
		field, _ = reflect.TypeFor[http.ClientConn]().FieldByName("cc")
		gcType = field.Type
		gcOffset = field.Offset
	})
	c.initOffsetOnce.Do(func() {
		clientConnPtr := unsafe.Pointer(c.ClientConn)
		c.cc = *(**http.ClientConn)(unsafe.Add(clientConnPtr, ccOffset))
		c.mu = (*sync.Mutex)(unsafe.Add(clientConnPtr, muOffset))
		c.closing = (*bool)(unsafe.Add(clientConnPtr, closingOffset))
		c.closed = (*bool)(unsafe.Add(clientConnPtr, closedOffset))
		c.roundTrips = (*int)(unsafe.Add(clientConnPtr, roundTripsOffset))
		c.reserved = (*int)(unsafe.Add(clientConnPtr, reservedOffset))
		c.starting = (*int)(unsafe.Add(clientConnPtr, startingOffset))
		c.pending = (*int)(unsafe.Add(clientConnPtr, pendingOffset))
		c.maxConcurrent = (*int)(unsafe.Add(clientConnPtr, maxConcurrentOffset))
		c.lastIdle = (*time.Time)(unsafe.Add(clientConnPtr, lastIdleOffset))
		c.shutdownc = (*chan struct{})(unsafe.Add(clientConnPtr, shutdowncOffset))
		c.roundTripper = reflect.NewAt(gcType, unsafe.Add(unsafe.Pointer(c.cc), gcOffset)).Elem().Interface().(http.RoundTripper)
	})
}

func (c *http2ClientConn) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get(":protocol") != "connect-udp" {
		return c.ClientConn.RoundTrip(req)
	}
	c.initOffset()
	haveReservation := false
	c.mu.Lock()
	*c.starting++
	*c.roundTrips++
	if *c.reserved != 0 {
		*c.reserved--
		haveReservation = true
	}
	c.mu.Unlock()
	if !haveReservation && c.cc.Reserve() != nil {
		c.mu.Lock()
		*c.pending++
		c.mu.Unlock()
	}
	// https://github.com/golang/go/blob/8dfc83de2c7b611e7525f09fd51616f180bf4d9b/src/net/http/clientconn.go#L273-L276
	// Pseudo header ":protocol"  is not allowed in net/http, and the header validation needs to be bypassed.
	resp, err := c.roundTripper.RoundTrip(req)
	c.mu.Lock()
	*c.starting--
	if *c.pending > 0 {
		*c.pending--
	}
	if c.cc.Err() != nil && !*c.closed {
		*c.closing = true
		*c.closed = true
	}
	if c.cc.InFlight() == 0 && *c.roundTrips > 0 && *c.starting == 0 {
		*c.lastIdle = time.Now()
	}
	if !*c.closed {
		*c.maxConcurrent = c.cc.Available() + c.cc.InFlight()
	}
	if *c.shutdownc != nil && c.cc.InFlight()+*c.starting == 0 {
		close(*c.shutdownc)
		*c.shutdownc = nil
	}
	c.mu.Unlock()
	return resp, err
}
