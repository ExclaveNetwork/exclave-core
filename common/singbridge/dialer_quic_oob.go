//go:build darwin || linux || freebsd

package singbridge

import (
	"errors"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

const supportOOB = true

func connect(rawConn syscall.RawConn, addr *net.UDPAddr) error {
	var sockaddr unix.Sockaddr
	if addr.AddrPort().Addr().Is4() || addr.AddrPort().Addr().Is4In6() {
		sockaddr = &unix.SockaddrInet4{
			Port: addr.Port,
			Addr: addr.AddrPort().Addr().As4(),
		}
	} else {
		sockaddr = &unix.SockaddrInet6{
			Port: addr.Port,
			Addr: addr.AddrPort().Addr().As16(),
		}
	}
	var innerErr error
	outerErr := rawConn.Control(func(fd uintptr) {
		if _, e := unix.Getpeername(int(fd)); errors.Is(e, unix.ENOTCONN) {
			innerErr = unix.Connect(int(fd), sockaddr)
		}
	})
	if outerErr != nil {
		return outerErr
	}
	if innerErr != nil {
		return innerErr
	}
	return nil
}
