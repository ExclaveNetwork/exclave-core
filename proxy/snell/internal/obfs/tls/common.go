/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

package tls

import (
	"bytes"
	"io"
	"net"
	"sync"

	p "github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/pool"
)

const chunkSize = 16 * 1024

var bufferPool = sync.Pool{New: func() any { return &bytes.Buffer{} }}

// readBlock reads `skipSize` bytes followed by a 2-byte big-endian length
// and that many payload bytes. When the payload exceeds len(b), the unread
// remainder is returned so the caller can drain it on the next call.
func readBlock(c net.Conn, b []byte, skipSize int) (remain, n int, err error) {
	if skipSize > 0 {
		buf := p.Get(skipSize)
		_, err = io.ReadFull(c, buf)
		_ = p.Put(buf)
		if err != nil {
			return
		}
	}

	sizeBuf := make([]byte, 2)
	if _, err = io.ReadFull(c, sizeBuf); err != nil {
		return
	}

	length := (int(sizeBuf[0]) << 8) | int(sizeBuf[1])
	if length > len(b) {
		n, err = c.Read(b)
		remain = length - n
		return
	}
	n, err = io.ReadFull(c, b[:length])
	return
}
