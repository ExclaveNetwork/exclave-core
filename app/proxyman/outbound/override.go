package outbound

import (
	"sync"
	"time"

	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/features/dns"
)

type EndpointOverrideReader struct {
	buf.Reader
	Dest         net.Address
	OriginalDest net.Address
	resolver     func(domain string) net.Address
	ipToDomain   *sync.Map
	fakedns      dns.FakeDNSEngine
	usedFakeIPs  *sync.Map
}

func (r *EndpointOverrideReader) Interrupt() {
	common.Interrupt(r.Reader)
}

func (r *EndpointOverrideReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.Reader.ReadMultiBuffer()
	if err != nil {
		return nil, err
	}
	for _, b := range mb {
		if b.Endpoint == nil {
			continue
		}
		if b.Endpoint.Address == r.OriginalDest {
			b.Endpoint.Address = r.Dest
			continue
		}
		if r.fakedns != nil && r.usedFakeIPs != nil && b.Endpoint.Address.Family().IsIP() {
			if domain := r.fakedns.GetDomainFromFakeDNS(b.Endpoint.Address); len(domain) > 0 {
				r.usedFakeIPs.LoadOrStore(b.Endpoint.Address, true)
				b.Endpoint.Address = net.DomainAddress(domain)
			}
		}
		if r.resolver != nil && r.ipToDomain != nil && b.Endpoint.Address.Family().IsDomain() {
			if ip := r.resolver(b.Endpoint.Address.Domain()); ip != nil {
				r.ipToDomain.LoadOrStore(ip, b.Endpoint.Address)
				b.Endpoint.Address = ip
			} else {
				return nil, newError("failed to resolve domain ", b.Endpoint.Address.Domain())
			}
		}
	}
	return mb, nil
}

type EndpointOverrideWriter struct {
	buf.Writer
	Dest         net.Address
	OriginalDest net.Address
	fakedns      dns.FakeDNSEngine
	usedFakeIPs  *sync.Map
	ipToDomain   *sync.Map
}

func (r *EndpointOverrideWriter) Interrupt() {
	common.Interrupt(r.Writer)
}

func (w *EndpointOverrideWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	for _, b := range mb {
		if b.Endpoint == nil {
			continue
		}
		if b.Endpoint.Address == w.Dest && w.OriginalDest != nil {
			b.Endpoint.Address = w.OriginalDest
			continue
		}
		if w.ipToDomain != nil && b.Endpoint.Address.Family().IsIP() {
			if domain, ok := w.ipToDomain.Load(b.Endpoint.Address); ok {
				b.Endpoint.Address = domain.(net.Address)
			}
		}
		if w.fakedns != nil && w.usedFakeIPs != nil && b.Endpoint.Address.Family().IsDomain() {
			if ips := w.fakedns.GetFakeIPForDomain(b.Endpoint.Address.Domain()); len(ips) > 0 {
				for _, ip := range ips {
					if _, ok := w.usedFakeIPs.Load(ip); ok {
						b.Endpoint.Address = ip
						break
					}
				}
			}
		}
	}
	return w.Writer.WriteMultiBuffer(mb)
}

type EndpointOverrideReaderWithTimeout struct {
	*EndpointOverrideReader
}

func (r *EndpointOverrideReaderWithTimeout) ReadMultiBufferTimeout(timeout time.Duration) (buf.MultiBuffer, error) {
	mb, err := r.Reader.(buf.TimeoutReader).ReadMultiBufferTimeout(timeout)
	if err != nil {
		return nil, err
	}
	for _, b := range mb {
		if b.Endpoint == nil {
			continue
		}
		if b.Endpoint.Address == r.OriginalDest {
			b.Endpoint.Address = r.Dest
			continue
		}
		if r.fakedns != nil && r.usedFakeIPs != nil && b.Endpoint.Address.Family().IsIP() {
			if domain := r.fakedns.GetDomainFromFakeDNS(b.Endpoint.Address); len(domain) > 0 {
				r.usedFakeIPs.LoadOrStore(b.Endpoint.Address, true)
				b.Endpoint.Address = net.DomainAddress(domain)
			}
		}
		if r.resolver != nil && r.ipToDomain != nil && b.Endpoint.Address.Family().IsDomain() {
			if ip := r.resolver(b.Endpoint.Address.Domain()); ip != nil {
				r.ipToDomain.LoadOrStore(ip, b.Endpoint.Address)
				b.Endpoint.Address = ip
			} else {
				return nil, newError("failed to resolve domain ", b.Endpoint.Address.Domain())
			}
		}
	}
	return mb, nil
}
