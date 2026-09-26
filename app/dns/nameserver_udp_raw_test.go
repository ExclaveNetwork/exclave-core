package dns

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	udp_proto "github.com/exclavenetwork/exclave-core/v5/common/protocol/udp"
	dns_feature "github.com/exclavenetwork/exclave-core/v5/features/dns"
)

type rawTestDispatcher struct {
	dispatch func(context.Context, *buf.Buffer)
}

func (d *rawTestDispatcher) Dispatch(ctx context.Context, _ net.Destination, payload *buf.Buffer) {
	defer payload.Release()
	d.dispatch(ctx, payload)
}

func (*rawTestDispatcher) Close() error { return nil }

func waitRawTest(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("UDP DNS operation blocked")
	}
}

func TestUDPQueryRawEarlyDuplicateResponse(t *testing.T) {
	s := &ClassicNameServer{channel: make(map[uint16]chan []byte)}
	// Deliver replies before Dispatch returns, so QueryRaw cannot yet receive.
	// A duplicate must neither block nor replace the first response.
	want := []byte{0x12, 0x34, 0x80, 0, 1}
	s.udpServer = &rawTestDispatcher{dispatch: func(ctx context.Context, _ *buf.Buffer) {
		s.HandleResponse(ctx, &udp_proto.Packet{Payload: buf.FromBytes(bytes.Clone(want))})
		s.HandleResponse(ctx, &udp_proto.Packet{Payload: buf.FromBytes([]byte{0x12, 0x34, 0x80, 0, 2})})
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := s.QueryRaw(context.Background(), []byte{0x12, 0x34, 0, 0})
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("QueryRaw = %v, %v; want %v, nil", got, err, want)
		}
	}()
	waitRawTest(t, done)
	if len(s.channel) != 0 {
		t.Fatal("completed query left a pending response channel")
	}
}

func TestUDPQueryRawCancellation(t *testing.T) {
	s := &ClassicNameServer{channel: make(map[uint16]chan []byte)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.udpServer = &rawTestDispatcher{dispatch: func(context.Context, *buf.Buffer) { cancel() }}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := s.QueryRaw(ctx, []byte{0x12, 0x34})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("QueryRaw error = %v; want context.Canceled", err)
		}
	}()
	waitRawTest(t, done)
	if len(s.channel) != 0 {
		t.Fatal("canceled query left a pending response channel")
	}
}

func TestUDPHandleResponseAbandonedQuery(t *testing.T) {
	// This is the state after QueryRaw selects cancellation but before it
	// acquires the lock to remove its channel. There is no receiver anymore.
	ch := make(chan []byte, 1)
	s := &ClassicNameServer{channel: map[uint16]chan []byte{0x1234: ch}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			s.HandleResponse(context.Background(), &udp_proto.Packet{
				Payload: buf.FromBytes([]byte{0x12, 0x34, 0x80, 0}),
			})
		}
		// Both timeout cleanup and unrelated IP cache lookups must still run.
		s.Lock()
		delete(s.channel, 0x1234)
		s.Unlock()
		_, _, err := s.findIPsForDomain("example.com.", dns_feature.IPOption{IPv4Enable: true})
		if err != errRecordNotFound {
			t.Errorf("cache lookup error = %v; want errRecordNotFound", err)
		}
	}()
	waitRawTest(t, done)
}
