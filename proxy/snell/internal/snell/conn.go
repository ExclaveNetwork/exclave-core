/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

package snell

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

// Snell wraps an AEAD-encrypted stream with the request/response framing
// used between a snell client and server. On the client side, the first
// Read consumes the response status byte (and any error payload) before
// returning relay data. On the server side, ServerStreamConn pre-sets the
// reply state so the next Read returns the client's request bytes directly.
type Snell struct {
	net.Conn
	buffer [1]byte
	reply  bool
}

// Read returns relayed data, parsing the snell response header on the
// first call.
func (s *Snell) Read(b []byte) (int, error) {
	if err := s.ReadReply(); err != nil {
		return 0, err
	}
	return s.Conn.Read(b)
}

// ReadReply consumes the server's response status byte if it hasn't been
// consumed yet. Subsequent calls are no-ops until the connection is
// half-closed via HalfClose.
func (s *Snell) ReadReply() error {
	if s.reply {
		return nil
	}

	if _, err := io.ReadFull(s.Conn, s.buffer[:]); err != nil {
		return err
	}
	s.reply = true

	switch s.buffer[0] {
	case ResponseTunnel, ResponsePong:
		return nil
	case ResponseError:
	default:
		return fmt.Errorf("snell: unknown response code 0x%x", s.buffer[0])
	}

	// CommandError: 1 byte code, 1 byte msg length, message bytes.
	if _, err := io.ReadFull(s.Conn, s.buffer[:]); err != nil {
		return err
	}
	errcode := s.buffer[0]

	if _, err := io.ReadFull(s.Conn, s.buffer[:]); err != nil {
		return err
	}
	length := int(s.buffer[0])
	msg := make([]byte, length)
	if _, err := io.ReadFull(s.Conn, msg); err != nil {
		return err
	}
	return NewAppError(errcode, fmt.Sprintf("server reported code: %d, message: %s", errcode, string(msg)))
}

// WriteHeader emits a TCP CONNECT request header. When reuse is true the
// V2 connect command is used so the server keeps the underlying connection
// alive after the destination closes.
func WriteHeader(conn net.Conn, host string, port uint16, reuse bool) error {
	if len(host) > 255 {
		return errors.New("snell: host name too long")
	}
	buf := make([]byte, 0, 5+len(host)+2)
	buf = append(buf, HeaderVersion)
	if reuse {
		buf = append(buf, CommandConnectV2)
	} else {
		buf = append(buf, CommandConnect)
	}
	buf = append(buf, 0) // empty client ID
	buf = append(buf, byte(len(host)))
	buf = append(buf, host...)
	buf = binary.BigEndian.AppendUint16(buf, port)
	_, err := conn.Write(buf)
	return err
}

// WriteUDPHeader emits a UDP-ASSOCIATE request header. v4/v5 only.
func WriteUDPHeader(conn net.Conn) error {
	_, err := conn.Write([]byte{HeaderVersion, CommandUDP, 0x00})
	return err
}

// writeZeroChunk sends an empty payload frame, signaling half-close to the
// peer.
func writeZeroChunk(conn net.Conn) error {
	_, err := conn.Write(nil)
	return err
}

// HalfClose sends a zero chunk and resets the reply state so the same Snell
// can be reused for a subsequent request (only valid after a successful
// reuse-mode CONNECT).
func HalfClose(conn net.Conn) error {
	if err := writeZeroChunk(conn); err != nil {
		return err
	}
	if s, ok := conn.(*Snell); ok {
		s.reply = false
	}
	return nil
}

// StreamConn turns a raw transport (TCP, obfs-wrapped TCP, ...) into a
// Snell client connection. The returned value is ready for WriteHeader.
func StreamConn(conn net.Conn, psk []byte) *Snell {
	return &Snell{Conn: newV4Conn(conn, psk)}
}

// ServerStreamConn is StreamConn for the server side: the reply state is
// pre-set so a subsequent Read returns the client's first request bytes.
func ServerStreamConn(conn net.Conn, psk []byte) *Snell {
	s := StreamConn(conn, psk)
	s.reply = true
	return s
}

// packetFrameWriter is implemented by the v4 transport conn so that UDP
// datagrams can be sent as discrete frames with snell-specific padding.
type packetFrameWriter interface {
	WritePacketFrame(b []byte) (int, error)
}

// WritePacketFrame writes b as a single snell frame on the underlying
// transport, falling back to a plain Write if the transport does not
// support framed writes.
func (s *Snell) WritePacketFrame(b []byte) (int, error) {
	if fw, ok := s.Conn.(packetFrameWriter); ok {
		return fw.WritePacketFrame(b)
	}
	return s.Conn.Write(b)
}
