/*
Copyright 2024 Marten Seemann

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

// https://github.com/quic-go/connect-ip-go

package connectip

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/quicvarint"
)

type CloseError struct {
	Remote bool
}

func (e *CloseError) Error() string {
	return net.ErrClosed.Error()
}

func (e *CloseError) Is(target error) bool {
	return target == net.ErrClosed
}

type httpStream interface {
	io.ReadWriteCloser
	CancelRead(quic.StreamErrorCode)
	CancelWrite(quic.StreamErrorCode)
	SetWriteDeadline(time.Time) error
}

type http3Stream interface {
	httpStream
	ReceiveDatagram(context.Context) ([]byte, error)
	SendDatagram([]byte) error
}

var (
	_ http3Stream = &http3.Stream{}
	_ http3Stream = &http3.RequestStream{}
)

const minMTU = 1280

const (
	maxQueuedCapsules    = 128
	maxQueuedDatagrams   = 128
	maxCapsulePacketSize = 1<<16 - 1
)

var errCapsuleLimit = errors.New("connect-ip: capsule limit exceeded")

type streamWrite struct {
	Data []byte
	Fin  bool
}

type Conn struct {
	str             httpStream
	enableDatagrams bool
	datagrams       chan []byte
	writeMu         sync.Mutex
	writeNotify     chan struct{}
	writeDone       chan error

	assignedAddressUpdates chan []AssignedAddress
	addressRequests        chan *addressRequestCapsule
	availableRouteUpdates  chan []IPRoute

	mu                   sync.Mutex
	queuedWrites         []streamWrite
	peerAddresses        []netip.Prefix
	localRoutes          []IPRoute
	assignedAddresses    []netip.Prefix
	peerRoutes           []IPRoute
	lastAddressRequestID AddressRequestID

	ctx       context.Context
	ctxCancel context.CancelCauseFunc

	closeOnce   sync.Once
	closeResult error
}

func newProxiedConn(str httpStream, enableDatagrams bool) *Conn {
	ctx, ctxCancel := context.WithCancelCause(context.Background())
	c := &Conn{
		str:                    str,
		enableDatagrams:        enableDatagrams,
		writeNotify:            make(chan struct{}, 1),
		writeDone:              make(chan error, 1),
		assignedAddressUpdates: make(chan []AssignedAddress, maxQueuedCapsules),
		addressRequests:        make(chan *addressRequestCapsule, maxQueuedCapsules),
		availableRouteUpdates:  make(chan []IPRoute, 1),
		ctx:                    ctx,
		ctxCancel:              ctxCancel,
	}
	if !enableDatagrams {
		c.datagrams = make(chan []byte, maxQueuedDatagrams)
	}
	go func() {
		err := c.readFromStream()
		c.mu.Lock()
		if c.ctx.Err() == nil {
			c.ctxCancel(&CloseError{Remote: true})
			if err != nil {
				c.str.CancelRead(quic.StreamErrorCode(http3.ErrCodeExcessiveLoad))
				c.str.CancelWrite(quic.StreamErrorCode(http3.ErrCodeExcessiveLoad))
				close(c.writeNotify)
			} else {
				_ = c.queueWrite(streamWrite{Fin: true})
			}
		}
		c.mu.Unlock()
	}()
	go func() {
		err := c.writeToStream()
		if err != nil {
			c.mu.Lock()
			if c.ctx.Err() == nil {
				c.ctxCancel(&CloseError{Remote: true})
				c.str.CancelRead(quic.StreamErrorCode(http3.ErrCodeExcessiveLoad))
				c.str.CancelWrite(quic.StreamErrorCode(http3.ErrCodeExcessiveLoad))
			} else {
				c.str.CancelWrite(quic.StreamErrorCode(http3.ErrCodeNoError))
			}
			c.mu.Unlock()
		}
		c.writeDone <- err
		close(c.writeDone)
	}()
	return c
}

func (c *Conn) AdvertiseRoute(routes []IPRoute) error {
	if err := validateRouteAdvertisement(routes); err != nil {
		return err
	}

	c.mu.Lock()
	if err := context.Cause(c.ctx); err != nil {
		c.mu.Unlock()
		return err
	}
	routes = slices.Clone(routes)
	err := c.queueWrite(streamWrite{Data: (&routeAdvertisementCapsule{IPAddressRanges: routes}).append(nil)})
	if err == nil {
		c.localRoutes = routes
	}
	c.mu.Unlock()
	if err != nil {
		_ = c.Close()
		return err
	}
	return nil
}

func (c *Conn) RequestAddresses(prefixes []netip.Prefix) ([]AddressRequestID, error) {
	if len(prefixes) == 0 {
		return nil, errors.New("connect-ip: address request must contain at least one prefix")
	}
	for i, p := range prefixes {
		if !p.IsValid() || p != p.Masked() {
			return nil, fmt.Errorf("connect-ip: invalid requested prefix %d: %s", i, p)
		}
	}

	c.mu.Lock()
	if err := context.Cause(c.ctx); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	ids := make([]AddressRequestID, len(prefixes))
	for i := range ids {
		ids[i] = c.lastAddressRequestID + AddressRequestID(i) + 1
	}
	capsule := &addressRequestCapsule{RequestIDs: ids, Prefixes: prefixes}
	err := c.queueWrite(streamWrite{Data: capsule.append(nil)})
	if err == nil {
		c.lastAddressRequestID = ids[len(ids)-1]
	}
	c.mu.Unlock()
	if err != nil {
		c.Close()
		return nil, err
	}
	return ids, nil
}

func (c *Conn) ReceiveAddressAssignment(ctx context.Context) ([]AssignedAddress, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case assignment := <-c.assignedAddressUpdates:
		return assignment, nil
	case <-c.ctx.Done():
		select {
		case assignment := <-c.assignedAddressUpdates:
			return assignment, nil
		default:
			return nil, context.Cause(c.ctx)
		}
	}
}

func (c *Conn) ReceiveAddressRequest(ctx context.Context) (*AddressRequest, error) {
	var requested *addressRequestCapsule
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case requested = <-c.addressRequests:
	case <-c.ctx.Done():
		select {
		case requested = <-c.addressRequests:
		default:
			return nil, context.Cause(c.ctx)
		}
	}
	return newAddressRequest(c, requested), nil
}

func (c *Conn) sendAddressAssignment(capsule *addressAssignCapsule) error {
	for i, addr := range capsule.AssignedAddresses {
		p := addr.IPPrefix
		if !p.IsValid() || p != p.Masked() {
			return fmt.Errorf("connect-ip: invalid assigned prefix %d: %s", i, p)
		}
	}

	c.mu.Lock()
	if err := context.Cause(c.ctx); err != nil {
		c.mu.Unlock()
		return err
	}
	if err := c.queueWrite(streamWrite{Data: capsule.append(nil)}); err != nil {
		c.mu.Unlock()
		_ = c.Close()
		return err
	}

	prefixes := make([]netip.Prefix, 0, len(capsule.AssignedAddresses))
	for _, assigned := range capsule.AssignedAddresses {
		if !assigned.Rejected() {
			prefixes = append(prefixes, assigned.IPPrefix)
		}
	}
	c.peerAddresses = prefixes
	c.mu.Unlock()
	return nil
}

func (c *Conn) queueWrite(w streamWrite) error {
	if w.Fin {
		_ = c.str.SetWriteDeadline(time.Now())
	} else if len(c.queuedWrites) >= maxQueuedCapsules {
		c.ctxCancel(&CloseError{Remote: false})
		c.str.CancelRead(quic.StreamErrorCode(http3.ErrCodeExcessiveLoad))
		c.str.CancelWrite(quic.StreamErrorCode(http3.ErrCodeExcessiveLoad))
		close(c.writeNotify)
		return errors.New("connect-ip: capsule queue full")
	}

	c.queuedWrites = append(c.queuedWrites, w)

	select {
	case c.writeNotify <- struct{}{}:
	default:
	}
	return nil
}

func queueLatest[T any](ch chan T, value T) {
	for {
		select {
		case ch <- value:
			return
		case <-ch:
		}
	}
}

func (c *Conn) Routes(ctx context.Context) ([]IPRoute, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, context.Cause(c.ctx)
	case routes := <-c.availableRouteUpdates:
		return routes, nil
	}
}

func (c *Conn) readFromStream() error {
	p := http3.NewCapsuleParser(c.str)
	for {
		t, cr, err := p.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch t {
		case capsuleTypeAddressAssign:
			capsule, err := parseAddressAssignCapsule(cr)
			if err != nil {
				return err
			}
			prefixes := make([]netip.Prefix, 0, len(capsule.AssignedAddresses))
			for _, assigned := range capsule.AssignedAddresses {
				if !assigned.Rejected() {
					prefixes = append(prefixes, assigned.IPPrefix)
				}
			}
			c.mu.Lock()
			c.assignedAddresses = prefixes
			c.mu.Unlock()
			select {
			case c.assignedAddressUpdates <- capsule.AssignedAddresses:
			default:
				return fmt.Errorf("%w: address assignment queue full", errCapsuleLimit)
			}
		case capsuleTypeAddressRequest:
			capsule, err := parseAddressRequestCapsule(cr)
			if err != nil {
				return err
			}
			select {
			case c.addressRequests <- capsule:
			default:
				return fmt.Errorf("%w: address request queue full", errCapsuleLimit)
			}
		case capsuleTypeRouteAdvertisement:
			capsule, err := parseRouteAdvertisementCapsule(cr)
			if err != nil {
				return err
			}
			c.mu.Lock()
			c.peerRoutes = slices.Clone(capsule.IPAddressRanges)
			c.mu.Unlock()
			queueLatest(c.availableRouteUpdates, capsule.IPAddressRanges)
		case capsuleTypeDatagram:
			if !c.enableDatagrams {
				if err := c.queueDatagram(cr); err != nil {
					return err
				}
				continue
			} else {
				if err := cr.Discard(); err != nil {
					return err
				}
			}
		default:
			if err := cr.Discard(); err != nil {
				return err
			}
		}
	}
}

func (c *Conn) writeToStream() error {
	for range c.writeNotify {
		for {
			c.mu.Lock()
			if len(c.queuedWrites) == 0 {
				c.mu.Unlock()
				break
			}
			w := c.queuedWrites[0]
			c.queuedWrites[0] = streamWrite{}
			c.queuedWrites = c.queuedWrites[1:]
			c.mu.Unlock()

			if w.Fin {
				return c.str.Close()
			}
			if err := c.write(w.Data); err != nil {
				return err
			}
		}
	}
	return context.Cause(c.ctx)
}

func (c *Conn) write(b []byte) error {
	c.writeMu.Lock()
	_, err := c.str.Write(b)
	c.writeMu.Unlock()
	return err
}

func (c *Conn) queueDatagram(cr http3.CapsuleReader) error {
	if cr.Remaining() > int64(len(contextIDZero)+maxCapsulePacketSize) {
		return cr.Discard()
	}
	data := make([]byte, cr.Remaining())
	if _, err := io.ReadFull(cr, data); err != nil {
		return err
	}
	select {
	case c.datagrams <- data:
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	}
	return nil
}

func (c *Conn) receiveDatagram(ctx context.Context) ([]byte, error) {
	if c.enableDatagrams {
		return c.str.(http3Stream).ReceiveDatagram(ctx)
	}
	select {
	case data := <-c.datagrams:
		return data, nil
	case <-c.ctx.Done():
		return nil, context.Cause(c.ctx)
	}
}

func (c *Conn) ReadPacket(b []byte) (int, error) {
start:
	data, err := c.receiveDatagram(c.ctx)
	if err != nil {
		return 0, err
	}
	contextID, n, err := quicvarint.Parse(data)
	if err != nil {
		return 0, fmt.Errorf("connect-ip: malformed datagram: %w", err)
	}
	if contextID != 0 {
		goto start
	}
	if err := c.handleIncomingProxiedPacket(data[n:]); err != nil {
		goto start
	}
	return copy(b, data[n:]), nil
}

func (c *Conn) handleIncomingProxiedPacket(data []byte) error {
	src, dst, ipProto, err := parseIPHeader(data)
	if err != nil {
		return err
	}

	c.mu.Lock()
	assignedAddresses := c.assignedAddresses
	localRoutes := c.localRoutes
	peerAddresses := c.peerAddresses
	peerRoutes := c.peerRoutes
	c.mu.Unlock()

	if !isAllowedSource(src, peerAddresses, nil) {
		invoking, ok := icmpInvokingPacket(data, ipProto)
		if !ok {
			return fmt.Errorf("connect-ip: datagram source address not allowed: %s", src)
		}
		invokingSrc, invokingDst, invokingProto, err := parseTruncatedIPHeader(invoking)
		if err != nil {
			return errors.New("connect-ip: malformed ICMP error message")
		}
		if invokingSrc != dst {
			return fmt.Errorf("connect-ip: ICMP error sent to %s for a packet from %s", dst, invokingSrc)
		}
		if !isAllowedSource(invokingSrc, assignedAddresses, localRoutes) ||
			!isAllowedDestination(invokingDst, invokingProto, peerAddresses, peerRoutes) {
			return fmt.Errorf(
				"connect-ip: ICMP error for a packet that couldn't have been sent through the tunnel: %s -> %s (protocol: %d)",
				invokingSrc, invokingDst, invokingProto,
			)
		}
	}

	if !isAllowedDestination(dst, ipProto, assignedAddresses, localRoutes) {
		return fmt.Errorf("connect-ip: datagram destination address / protocol not allowed: %s (protocol: %d)", dst, ipProto)
	}
	return nil
}

func (c *Conn) WritePacket(b []byte) (icmp []byte, err error) {
	select {
	case <-c.ctx.Done():
		return nil, context.Cause(c.ctx)
	default:
	}
	data, err := c.composeDatagram(b)
	if err != nil {
		return nil, nil
	}
	if !c.enableDatagrams {
		if err := c.write(data); err != nil {
			select {
			case <-c.ctx.Done():
				return nil, context.Cause(c.ctx)
			default:
				return nil, err
			}
		}
		return nil, nil
	}
	if err := c.str.(http3Stream).SendDatagram(data); err != nil {
		if _, ok := errors.AsType[*quic.DatagramTooLargeError](err); ok {
			icmpPacket, _ := composeICMPTooLargePacket(b, minMTU)
			return icmpPacket, nil
		}
		select {
		case <-c.ctx.Done():
			return nil, context.Cause(c.ctx)
		default:
			return nil, err
		}
	}
	return nil, nil
}

func (c *Conn) composeDatagram(b []byte) ([]byte, error) {
	src, dst, ipProto, err := parseIPHeader(b)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	assignedAddresses := c.assignedAddresses
	localRoutes := c.localRoutes
	peerAddresses := c.peerAddresses
	peerRoutes := c.peerRoutes
	c.mu.Unlock()

	if invoking, ok := icmpInvokingPacket(b, ipProto); ok {
		invokingSrc, invokingDst, invokingProto, err := parseTruncatedIPHeader(invoking)
		if err != nil {
			return nil, errors.New("connect-ip: malformed ICMP error message")
		}
		if invokingSrc != dst {
			return nil, fmt.Errorf("connect-ip: ICMP error sent to %s for a packet from %s", dst, invokingSrc)
		}
		if !isAllowedSource(invokingSrc, peerAddresses, nil) ||
			!isAllowedDestination(invokingDst, invokingProto, assignedAddresses, localRoutes) {
			return nil, fmt.Errorf(
				"connect-ip: ICMP error for a packet that couldn't have been received through the tunnel: %s -> %s (protocol: %d)",
				invokingSrc, invokingDst, invokingProto,
			)
		}
	} else if !isAllowedSource(src, assignedAddresses, localRoutes) {
		return nil, fmt.Errorf("connect-ip: source address not allowed: %s", src)
	}
	if !isAllowedDestination(dst, ipProto, peerAddresses, peerRoutes) {
		return nil, fmt.Errorf("connect-ip: destination address / protocol not allowed: %s (protocol: %d)", dst, ipProto)
	}

	switch ipVersion(b) {
	case 4:
		ttl := b[8]
		if ttl <= 1 {
			return nil, fmt.Errorf("connect-ip: datagram TTL too small: %d", ttl)
		}
		b[8]--
		hdrLen := int(b[0]&0x0f) * 4
		binary.BigEndian.PutUint16(b[10:12], calculateIPv4Checksum(b[:hdrLen]))
	case 6:
		hopLimit := b[7]
		if hopLimit <= 1 {
			return nil, fmt.Errorf("connect-ip: datagram Hop Limit too small: %d", hopLimit)
		}
		b[7]--
	}
	size := len(contextIDZero) + len(b)
	var data []byte
	if !c.enableDatagrams {
		data = make([]byte, 0, quicvarint.Len(uint64(capsuleTypeDatagram))+quicvarint.Len(uint64(size))+size)
		data = quicvarint.Append(data, uint64(capsuleTypeDatagram))
		data = quicvarint.Append(data, uint64(size))
	} else {
		data = make([]byte, 0, size)
	}
	data = append(data, contextIDZero...)
	data = append(data, b...)
	return data, nil
}

func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		if c.ctx.Err() == nil {
			c.ctxCancel(&CloseError{Remote: false})
			_ = c.queueWrite(streamWrite{Fin: true})
		}
		c.mu.Unlock()
		c.closeResult = <-c.writeDone
		c.str.CancelRead(quic.StreamErrorCode(http3.ErrCodeNoError))
	})
	return c.closeResult
}
