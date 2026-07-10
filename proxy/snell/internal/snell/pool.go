/*
 * This file is part of opensnell.
 * SPDX-License-Identifier: GPL-3.0-or-later
 */

package snell

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// Pool is a tiny TCP-connection pool used by the client to reuse upstream
// Snell connections when the server negotiated reuse mode (V2 connect).
//
// Empirically the official Surge snell-server v5.0.1 closes a reuse-mode
// TCP connection after the second session on it (1 fresh CONNECT + 1
// reuse). Putting back a conn that has already been reused once would
// hand the next caller a soon-to-be-dead socket, so we cap each pooled
// conn at MaxUsesPerConn lifetime sessions and discard beyond that.
type Pool struct {
	factory        func(ctx context.Context) (*Snell, error)
	maxSize        int
	maxAge         time.Duration
	maxUsesPerConn int
	mu             sync.Mutex
	items          []pooledConn
}

type pooledConn struct {
	conn      *Snell
	expiresAt time.Time
	uses      int // number of CONNECT sessions served by this TCP so far
}

func NewPool(factory func(ctx context.Context) (*Snell, error)) *Pool {
	return &Pool{
		factory:        factory,
		maxSize:        10,
		maxAge:         15 * time.Second,
		maxUsesPerConn: 2,
	}
}

// GetContext returns an idle pooled connection wrapped so that Close
// returns it to the pool, or dials a fresh one.
func (p *Pool) GetContext(ctx context.Context) (net.Conn, error) {
	if item := p.takeIdle(); item != nil {
		pc := &PoolConn{Snell: item.conn, pool: p, uses: item.uses + 1}
		return pc, nil
	}
	conn, err := p.factory(ctx)
	if err != nil {
		return nil, err
	}
	return &PoolConn{Snell: conn, pool: p, uses: 1}, nil
}

func (p *Pool) takeIdle() *pooledConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for len(p.items) > 0 {
		item := p.items[len(p.items)-1]
		p.items = p.items[:len(p.items)-1]
		if now.Before(item.expiresAt) {
			return &item
		}
		_ = item.conn.Close()
	}
	return nil
}

func (p *Pool) put(conn *Snell, uses int) {
	if p.maxUsesPerConn > 0 && uses >= p.maxUsesPerConn {
		_ = conn.Close()
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.items) >= p.maxSize {
		_ = conn.Close()
		return
	}
	p.items = append(p.items, pooledConn{
		conn:      conn,
		expiresAt: time.Now().Add(p.maxAge),
		uses:      uses,
	})
}

// PoolConn is a checked-out Snell connection that, on Close, gets returned
// to the pool after sending a zero chunk to half-close the upstream
// session. Read maps the snell zero-chunk EOF to io.EOF for the caller's
// convenience. `uses` tracks how many CONNECT sessions this TCP has served
// so far (1 for the first session).
type PoolConn struct {
	*Snell
	pool           *Pool
	uses           int
	closeWriteOnce sync.Once
	closeWriteErr  error
	closeOnce      sync.Once
	closeErr       error
}

// Read converts the snell zero-chunk half-close signal into io.EOF so
// io.Copy-style relays terminate cleanly instead of spinning on (0, nil).
// Mirrors mihomo's PoolConn behavior.
func (pc *PoolConn) Read(b []byte) (int, error) {
	n, err := pc.Snell.Read(b)
	if errors.Is(err, ErrZeroChunk) {
		return n, io.EOF
	}
	return n, err
}

func (pc *PoolConn) Write(b []byte) (int, error) {
	return pc.Snell.Write(b)
}

func (pc *PoolConn) CloseWrite() error {
	pc.closeWriteOnce.Do(func() {
		pc.closeWriteErr = writeZeroChunk(pc.Snell)
	})
	return pc.closeWriteErr
}

func (pc *PoolConn) Close() error {
	pc.closeOnce.Do(func() {
		if err := pc.CloseWrite(); err != nil {
			pc.closeErr = err
			_ = pc.Snell.Close()
			return
		}

		// If the relay terminated because the SOCKS5 client closed first
		// (typical for short HTTP/1 responses), the server may still have
		// pending data — most importantly its half-close zero chunk —
		// queued in the TCP receive buffer. Reusing this conn without
		// draining would surface that stale zero chunk on the next read
		// and break the next session. Drain until we observe the
		// server's zero chunk or hit a short timeout; only then put back.
		if !pc.drainPendingForReuse() {
			_ = pc.Snell.Close()
			return
		}
		_ = pc.Snell.Conn.SetReadDeadline(time.Time{})
		pc.Snell.reply = false
		pc.pool.put(pc.Snell, pc.uses)
	})
	return pc.closeErr
}

// drainPendingForReuse reads remaining frames from the snell stream until
// the server's zero-chunk half-close is observed, with a short deadline.
// Returns true if the conn is in a clean state suitable for pool reuse.
func (pc *PoolConn) drainPendingForReuse() bool {
	if err := pc.Snell.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		return false
	}
	scratch := make([]byte, 4096)
	for {
		_, err := pc.Snell.Read(scratch)
		if errors.Is(err, ErrZeroChunk) {
			return true
		}
		if err != nil {
			return false
		}
	}
}
