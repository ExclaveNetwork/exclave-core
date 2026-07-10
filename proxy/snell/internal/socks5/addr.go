// Address helpers for Snell UDP framing (subset of SOCKS5 address encoding).
package socks5

import (
	"encoding/binary"
	"net"
	"strconv"
)

const (
	AtypIPv4       = 1
	AtypDomainName = 3
	AtypIPv6       = 4
	MaxAddrLen     = 1 + 1 + 255 + 2
)

type Addr []byte

func (a Addr) UDPAddr() *net.UDPAddr {
	if len(a) == 0 {
		return nil
	}
	switch a[0] {
	case AtypIPv4:
		return &net.UDPAddr{
			IP:   net.IP(a[1 : 1+net.IPv4len]),
			Port: int(binary.BigEndian.Uint16(a[1+net.IPv4len:])),
		}
	case AtypIPv6:
		return &net.UDPAddr{
			IP:   net.IP(a[1 : 1+net.IPv6len]),
			Port: int(binary.BigEndian.Uint16(a[1+net.IPv6len:])),
		}
	}
	return nil
}

func SplitAddr(b []byte) Addr {
	addrLen := 1
	if len(b) < addrLen {
		return nil
	}
	switch b[0] {
	case AtypDomainName:
		if len(b) < 2 {
			return nil
		}
		addrLen = 1 + 1 + int(b[1]) + 2
	case AtypIPv4:
		addrLen = 1 + net.IPv4len + 2
	case AtypIPv6:
		addrLen = 1 + net.IPv6len + 2
	default:
		return nil
	}
	if len(b) < addrLen {
		return nil
	}
	return b[:addrLen]
}

func ParseAddr(s string) Addr {
	var addr Addr
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			addr = make([]byte, 1+net.IPv4len+2)
			addr[0] = AtypIPv4
			copy(addr[1:], ip4)
		} else {
			addr = make([]byte, 1+net.IPv6len+2)
			addr[0] = AtypIPv6
			copy(addr[1:], ip)
		}
	} else {
		if len(host) > 255 {
			return nil
		}
		addr = make([]byte, 1+1+len(host)+2)
		addr[0] = AtypDomainName
		addr[1] = byte(len(host))
		copy(addr[2:], host)
	}
	portnum, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil
	}
	binary.BigEndian.PutUint16(addr[len(addr)-2:], uint16(portnum))
	return addr
}

func ParseAddrToSocksAddr(addr net.Addr) Addr {
	var hostip net.IP
	var port int
	switch a := addr.(type) {
	case *net.UDPAddr:
		hostip = a.IP
		port = a.Port
	case *net.TCPAddr:
		hostip = a.IP
		port = a.Port
	default:
		return ParseAddr(addr.String())
	}
	if ip4 := hostip.To4(); ip4 != nil {
		parsed := make([]byte, 1+net.IPv4len+2)
		parsed[0] = AtypIPv4
		copy(parsed[1:], ip4)
		binary.BigEndian.PutUint16(parsed[1+net.IPv4len:], uint16(port))
		return parsed
	}
	if ip6 := hostip.To16(); ip6 != nil {
		parsed := make([]byte, 1+net.IPv6len+2)
		parsed[0] = AtypIPv6
		copy(parsed[1:], ip6)
		binary.BigEndian.PutUint16(parsed[1+net.IPv6len:], uint16(port))
		return parsed
	}
	return nil
}
