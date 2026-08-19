//go:build !darwin && !linux && !freebsd

package singbridge

import (
	"net"
	"syscall"
)

const supportOOB = false

func connect(_ syscall.RawConn, _ *net.UDPAddr) error {
	panic("unsupported")
}
