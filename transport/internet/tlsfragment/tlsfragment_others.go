//go:build !(linux || darwin || windows)

package tlsfragment

import (
	"context"
	"net"
	"time"
)

func writeAndWaitAck(ctx context.Context, conn *net.TCPConn, payload []byte, fallbackDelay time.Duration) error {
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(fallbackDelay):
	}
	return nil
}
