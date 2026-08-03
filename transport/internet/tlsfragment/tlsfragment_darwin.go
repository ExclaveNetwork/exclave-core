package tlsfragment

import (
	"context"
	"errors"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

func writeAndWaitAck(ctx context.Context, conn *net.TCPConn, payload []byte, fallbackDelay time.Duration) error {
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var innerErr error
	err = rawConn.Control(func(fd uintptr) {
		start := time.Now()
		for {
			unacked, err := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_NWRITE)
			if err != nil {
				innerErr = err
				return
			}
			if unacked == 0 {
				if time.Since(start) <= 20*time.Millisecond {
					// under transparent proxy
					select {
					case <-ctx.Done():
						innerErr = ctx.Err()
						return
					case <-time.After(fallbackDelay):
					}
				}
				return
			}
			select {
			case <-ctx.Done():
				innerErr = ctx.Err()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	})
	if innerErr != nil || err != nil {
		if err == nil {
			return innerErr
		}
		return errors.Join(innerErr, err)
	}
	return nil
}
