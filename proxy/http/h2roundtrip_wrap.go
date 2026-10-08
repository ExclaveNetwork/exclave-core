//go:build go1.27 && !http2legacy

package http

import (
	"net/http"
	"reflect"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

func h2RoundTrip(clientConn *http2.ClientConn, req *http.Request) (*http.Response, error) {
	// https://github.com/golang/net/blob/8456a207cc33a21300bb8964ed756e5b681aeeeb/http2/transport_wrap.go#L395-L461
	elem := reflect.ValueOf(clientConn).Elem()
	cc := *(**http.ClientConn)(elem.FieldByName("cc").Addr().UnsafePointer())
	mu := (*sync.Mutex)(elem.FieldByName("mu").Addr().UnsafePointer())
	closing := (*bool)(elem.FieldByName("closing").Addr().UnsafePointer())
	closed := (*bool)(elem.FieldByName("closed").Addr().UnsafePointer())
	roundTrips := (*int)(elem.FieldByName("roundTrips").Addr().UnsafePointer())
	reserved := (*int)(elem.FieldByName("reserved").Addr().UnsafePointer())
	starting := (*int)(elem.FieldByName("starting").Addr().UnsafePointer())
	pending := (*int)(elem.FieldByName("pending").Addr().UnsafePointer())
	maxConcurrent := (*int)(elem.FieldByName("maxConcurrent").Addr().UnsafePointer())
	lastIdle := (*time.Time)(elem.FieldByName("lastIdle").Addr().UnsafePointer())
	shutdownc := (*chan struct{})(elem.FieldByName("shutdownc").Addr().UnsafePointer())

	field := reflect.ValueOf(cc).Elem().FieldByName("cc")
	roundTripper := reflect.NewAt(field.Type(), field.Addr().UnsafePointer()).Elem().Interface().(http.RoundTripper)

	hasReservation := false
	mu.Lock()
	*starting++
	*roundTrips++
	if *reserved != 0 {
		*reserved--
		hasReservation = true
	}
	mu.Unlock()
	if !hasReservation && cc.Reserve() != nil {
		mu.Lock()
		*pending++
		mu.Unlock()
	}

	// https://github.com/golang/go/blob/8dfc83de2c7b611e7525f09fd51616f180bf4d9b/src/net/http/clientconn.go#L273-L276
	// Pseudo header ":protocol"  is not allowed in net/http, and the header validation needs to be bypassed.
	resp, err := roundTripper.RoundTrip(req)

	mu.Lock()
	*starting--
	if *pending > 0 {
		*pending--
	}
	if cc.Err() != nil && !*closed {
		*closing = true
		*closed = true
	}
	if cc.InFlight() == 0 && *roundTrips > 0 && *starting == 0 {
		*lastIdle = time.Now()
	}
	if !*closed {
		*maxConcurrent = cc.Available() + cc.InFlight()
	}
	if *shutdownc != nil && cc.InFlight()+*starting == 0 {
		close(*shutdownc)
		*shutdownc = nil
	}
	mu.Unlock()
	return resp, err
}
