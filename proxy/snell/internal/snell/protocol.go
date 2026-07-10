/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

// Package snell implements the Snell v4/v5 wire protocol used by Surge.
//
// Only v4 and v5 are supported; v5 currently behaves identically to v4 on
// the wire (the version byte in the request header is 0x01 regardless of
// the AEAD frame "version field" being 4). Older Snell v1/v2/v3 are not
// supported and live in the sibling opensnellv1-2 project.
package snell

import "errors"

// Version constants. The byte that appears as the first byte of every
// Snell request header is HeaderVersion, regardless of v4 vs v5. The "4"
// inside each AEAD frame header indicates the AEAD frame format.
const (
	Version4 = 4
	Version5 = 5

	// HeaderVersion is the byte sent as the first byte of every Snell
	// request from the client. It has always been 0x01 since v1.
	HeaderVersion byte = 1
)

// Commands sent by the client.
const (
	CommandPing      byte = 0
	CommandConnect   byte = 1
	CommandConnectV2 byte = 5 // reuse-capable TCP connect
	CommandUDP       byte = 6
)

// CommandUDPForward is the inner UDP command byte that appears as the
// first byte of each UDP-over-TCP request frame.
const CommandUDPForward byte = 1

// Server response codes.
const (
	ResponseTunnel byte = 0
	ResponsePong   byte = 1
	ResponseError  byte = 2
)

// MaxPayloadLength is the largest snell payload that may appear in a
// single frame (matches mihomo's `maxLength`).
const MaxPayloadLength = 0x3FFF

// ErrZeroChunk is returned by Read when the peer signaled half-close by
// emitting an empty payload frame. The caller should treat this as EOF.
var ErrZeroChunk = errors.New("snell: zero chunk")

// AppError is an application-layer error carrying the error code returned
// by the snell peer.
type AppError struct {
	Code    byte
	Message string
}

func (e *AppError) Error() string {
	return e.Message
}

// NewAppError constructs an AppError. Empty messages get a placeholder so
// the AppError can still be propagated through `errors.As`.
func NewAppError(code byte, msg string) error {
	if msg == "" {
		msg = "snell app error"
	}
	return &AppError{Code: code, Message: msg}
}
