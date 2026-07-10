/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

package http

import (
	"bufio"
	"bytes"
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
)

type HTTPObfsClient struct {
	net.Conn
	host          string
	port          string
	bio           *bufio.Reader
	buf           []byte
	offset        int
	firstRequest  bool
	firstResponse bool
}

func (ho *HTTPObfsClient) Read(b []byte) (int, error) {
	if ho.buf != nil {
		n := copy(b, ho.buf[ho.offset:])
		ho.offset += n
		if ho.offset == len(ho.buf) {
			ho.offset = 0
			ho.buf = nil
		}
		return n, nil
	}

	if ho.firstResponse {
		bio := bufio.NewReader(ho.Conn)
		resp, err := http.ReadResponse(bio, nil)
		if err != nil {
			return 0, err
		}
		buf, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, err
		}
		n := copy(b, buf)
		if n < len(buf) {
			ho.buf = buf
			ho.offset = n
		}
		_ = resp.Body.Close()
		ho.bio = bio
		ho.firstResponse = false
		return n, nil
	}
	return ho.bio.Read(b)
}

func (ho *HTTPObfsClient) Write(b []byte) (int, error) {
	if ho.firstRequest {
		randBytes := make([]byte, 16)
		_, _ = crand.Read(randBytes)
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://%s/", ho.host), bytes.NewBuffer(b))
		req.Header.Set("User-Agent", fmt.Sprintf("curl/7.%d.%d", rand.IntN(54), rand.IntN(2)))
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		req.Host = fmt.Sprintf("%s:%s", ho.host, ho.port)
		req.Header.Set("Sec-WebSocket-Key", base64.URLEncoding.EncodeToString(randBytes))
		req.ContentLength = int64(len(b))
		err := req.Write(ho.Conn)
		ho.firstRequest = false
		return len(b), err
	}
	return ho.Conn.Write(b)
}

func NewHTTPObfsClient(conn net.Conn, host string, port string) net.Conn {
	return &HTTPObfsClient{
		Conn:          conn,
		firstRequest:  true,
		firstResponse: true,
		host:          host,
		port:          port,
	}
}
