/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

package snell

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"

	"github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/socks5"
	"github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/pool"
)

// UDPRequest is one parsed UDP-over-TCP request frame coming from the client.
type UDPRequest struct {
	Host    string
	IP      netip.Addr
	Port    uint16
	Payload []byte
}

// ParseUDPRequest decodes one snell-format UDP request frame. The frame
// payload starts with the UDP forward command byte (0x01) followed by
// either a domain name or an IPv4/IPv6 address.
func ParseUDPRequest(packet []byte) (UDPRequest, error) {
	if len(packet) < 2 || packet[0] != CommandUDPForward {
		return UDPRequest{}, errors.New("snell: invalid UDP request")
	}
	if hostLen := int(packet[1]); hostLen != 0 {
		if len(packet) <= 2+hostLen+2 {
			return UDPRequest{}, errors.New("snell: invalid UDP domain request")
		}
		offset := 2 + hostLen
		return UDPRequest{
			Host:    string(packet[2:offset]),
			Port:    binary.BigEndian.Uint16(packet[offset : offset+2]),
			Payload: packet[offset+2:],
		}, nil
	}
	if len(packet) < 3 {
		return UDPRequest{}, errors.New("snell: invalid UDP IP request")
	}
	switch packet[2] {
	case 0x04:
		if len(packet) < 3+net.IPv4len+2 {
			return UDPRequest{}, errors.New("snell: invalid UDP IPv4 request")
		}
		offset := 3 + net.IPv4len
		ip, _ := netip.AddrFromSlice(packet[3:offset])
		return UDPRequest{
			IP:      ip.Unmap(),
			Port:    binary.BigEndian.Uint16(packet[offset : offset+2]),
			Payload: packet[offset+2:],
		}, nil
	case 0x06:
		if len(packet) < 3+net.IPv6len+2 {
			return UDPRequest{}, errors.New("snell: invalid UDP IPv6 request")
		}
		offset := 3 + net.IPv6len
		ip, _ := netip.AddrFromSlice(packet[3:offset])
		return UDPRequest{
			IP:      ip.Unmap(),
			Port:    binary.BigEndian.Uint16(packet[offset : offset+2]),
			Payload: packet[offset+2:],
		}, nil
	default:
		return UDPRequest{}, errors.New("snell: invalid UDP address type")
	}
}

// udpHeaderLength returns the encoded UDP request header length for a
// given SOCKS5 address slice.
func udpHeaderLength(socksAddr []byte) int {
	if len(socksAddr) == 0 {
		return MaxPayloadLength + 1
	}
	switch socksAddr[0] {
	case socks5.AtypDomainName:
		if len(socksAddr) < 2 {
			return MaxPayloadLength + 1
		}
		return 1 + 1 + int(socksAddr[1]) + 2
	case socks5.AtypIPv4:
		return 1 + 2 + net.IPv4len + 2
	case socks5.AtypIPv6:
		return 1 + 2 + net.IPv6len + 2
	default:
		return MaxPayloadLength + 1
	}
}

func writeUDPRequest(w io.Writer, socksAddr, payload []byte) (int, error) {
	buf := make([]byte, 0, 1+len(socksAddr)+len(payload))
	buf = append(buf, CommandUDPForward)
	switch socksAddr[0] {
	case socks5.AtypDomainName:
		hostLen := int(socksAddr[1])
		if len(socksAddr) < 1+1+hostLen+2 {
			return 0, errors.New("snell UDP address invalid")
		}
		buf = append(buf, socksAddr[1:1+1+hostLen+2]...)
	case socks5.AtypIPv4:
		if len(socksAddr) < 1+net.IPv4len+2 {
			return 0, errors.New("snell UDP address invalid")
		}
		buf = append(buf, 0x00, 0x04)
		buf = append(buf, socksAddr[1:1+net.IPv4len+2]...)
	case socks5.AtypIPv6:
		if len(socksAddr) < 1+net.IPv6len+2 {
			return 0, errors.New("snell UDP address invalid")
		}
		buf = append(buf, 0x00, 0x06)
		buf = append(buf, socksAddr[1:1+net.IPv6len+2]...)
	default:
		return 0, errors.New("snell UDP address invalid")
	}
	buf = append(buf, payload...)

	if fw, ok := w.(packetFrameWriter); ok {
		if _, err := fw.WritePacketFrame(buf); err != nil {
			return 0, err
		}
		return len(payload), nil
	}
	if _, err := w.Write(buf); err != nil {
		return 0, err
	}
	return len(payload), nil
}

// WritePacket writes one UDP datagram in snell-request format. The
// destination address is encoded inline with the payload.
func WritePacket(w io.Writer, socksAddr, payload []byte) (int, error) {
	maxPayload := MaxPayloadLength - udpHeaderLength(socksAddr)
	if maxPayload <= 0 {
		return 0, errors.New("snell UDP address too large")
	}
	if len(payload) > maxPayload {
		return 0, errors.New("snell UDP payload too large")
	}
	return writeUDPRequest(w, socksAddr, payload)
}

// WritePacketResponse writes one UDP datagram in snell-response format
// (server -> client). The response header uses a slightly different
// address encoding than the request header.
func WritePacketResponse(w io.Writer, addr net.Addr, payload []byte) (int, error) {
	socksAddr := socks5.ParseAddrToSocksAddr(addr)
	if len(socksAddr) == 0 {
		return 0, errors.New("snell UDP response address invalid")
	}
	buf := make([]byte, 0, 1+len(socksAddr)+len(payload))
	switch socksAddr[0] {
	case socks5.AtypIPv4:
		if len(socksAddr) < 1+net.IPv4len+2 {
			return 0, errors.New("snell UDP response address invalid")
		}
		buf = append(buf, 0x04)
		buf = append(buf, socksAddr[1:1+net.IPv4len+2]...)
	case socks5.AtypIPv6:
		if len(socksAddr) < 1+net.IPv6len+2 {
			return 0, errors.New("snell UDP response address invalid")
		}
		buf = append(buf, 0x06)
		buf = append(buf, socksAddr[1:1+net.IPv6len+2]...)
	default:
		return 0, errors.New("snell UDP response address invalid")
	}
	buf = append(buf, payload...)

	if fw, ok := w.(packetFrameWriter); ok {
		if _, err := fw.WritePacketFrame(buf); err != nil {
			return 0, err
		}
		return len(payload), nil
	}
	if _, err := w.Write(buf); err != nil {
		return 0, err
	}
	return len(payload), nil
}

// ReadPacketResponse reads one server->client UDP response frame from r,
// returning the source address embedded in the frame and the payload bytes
// copied into out.
func ReadPacketResponse(r io.Reader, out []byte) (net.Addr, int, error) {
	buf := pool.Get(MaxPayloadLength + 1)
	defer func() { _ = pool.Put(buf) }()

	n, err := r.Read(buf)
	if err != nil {
		return nil, 0, err
	}
	if n < 1 {
		return nil, 0, errors.New("insufficient UDP length")
	}

	headLen := 1
	switch buf[0] {
	case 0x04:
		headLen += net.IPv4len + 2
		if n < headLen {
			return nil, 0, errors.New("insufficient UDP length")
		}
		buf[0] = socks5.AtypIPv4
	case 0x06:
		headLen += net.IPv6len + 2
		if n < headLen {
			return nil, 0, errors.New("insufficient UDP length")
		}
		buf[0] = socks5.AtypIPv6
	default:
		return nil, 0, errors.New("ip version invalid")
	}

	addr := socks5.SplitAddr(buf[:headLen])
	if addr == nil {
		return nil, 0, errors.New("remote address invalid")
	}
	uaddr := addr.UDPAddr()
	if uaddr == nil {
		return nil, 0, errors.New("parse addr error")
	}

	length := len(out)
	if n-headLen < length {
		length = n - headLen
	}
	copy(out, buf[headLen:headLen+length])
	return uaddr, length, nil
}

// PacketConn adapts a Snell stream into a net.PacketConn so that the
// client can shuttle UDP datagrams across.
func PacketConn(conn net.Conn) net.PacketConn {
	return &packetConn{Conn: conn}
}

type packetConn struct {
	net.Conn
	rMux sync.Mutex
	wMux sync.Mutex
}

func (pc *packetConn) WritePacketFrame(b []byte) (int, error) {
	if s, ok := pc.Conn.(*Snell); ok {
		if fw, ok := s.Conn.(packetFrameWriter); ok {
			return fw.WritePacketFrame(b)
		}
	}
	return pc.Conn.Write(b)
}

func (pc *packetConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	pc.wMux.Lock()
	defer pc.wMux.Unlock()
	return WritePacket(pc, socks5.ParseAddrToSocksAddr(addr), b)
}

func (pc *packetConn) ReadFrom(b []byte) (int, net.Addr, error) {
	pc.rMux.Lock()
	defer pc.rMux.Unlock()
	addr, n, err := ReadPacketResponse(pc.Conn, b)
	if err != nil {
		return 0, nil, err
	}
	return n, addr, nil
}
