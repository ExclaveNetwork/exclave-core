package internet

import (
	"encoding/binary"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	v2net "github.com/exclavenetwork/exclave-core/v5/common/net"
)

const (
	IP_UNICAST_IF   = 31 // nolint: revive,stylecheck
	IPV6_UNICAST_IF = 31 // nolint: revive,stylecheck
)

func applyOutboundSocketOptions(network string, address string, fd uintptr, config *SocketConfig) error {
	if isTCPSocket(network) {
		if config.TcpKeepAliveIdle > 0 {
			if err := syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1); err != nil {
				return newError("failed to set SO_KEEPALIVE", err)
			}
		}
	}

	if config.BindToDevice != "" {
		iface, err := net.InterfaceByName(config.BindToDevice)
		if err != nil {
			return newError("failed to get interface ", config.BindToDevice).Base(err)
		}
		dest, err := v2net.ParseDestination(address)
		multiCast := err == nil && dest.Address.Family().IsIP() && dest.Address.IP().IsMulticast()
		// bind is always used in the default dialer
		switch network {
		case "tcp4", "udp4":
			var bytes [4]byte
			binary.BigEndian.PutUint32(bytes[:], uint32(iface.Index))
			index := *(*uint32)(unsafe.Pointer(&bytes[0]))
			if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, IP_UNICAST_IF, int(index)); err != nil {
				return newError("failed to set IP_UNICAST_IF", err)
			}
			if network == "udp4" || multiCast {
				if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_MULTICAST_IF, int(index)); err != nil {
					return newError("failed to set IP_MULTICAST_IF", err)
				}
			}
		case "tcp6", "udp6":
			if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, IPV6_UNICAST_IF, iface.Index); err != nil {
				return newError("failed to set IPV6_UNICAST_IF", err)
			}
			if network == "udp6" || multiCast {
				if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, windows.IPV6_MULTICAST_IF, iface.Index); err != nil {
					return newError("failed to set IPV6_MULTICAST_IF", err)
				}
			}
		}
	}

	if config.TxBufSize != 0 {
		if err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_SNDBUF, int(config.TxBufSize)); err != nil {
			return newError("failed to set SO_SNDBUF").Base(err)
		}
	}

	if config.RxBufSize != 0 {
		if err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_RCVBUF, int(config.TxBufSize)); err != nil {
			return newError("failed to set SO_RCVBUF").Base(err)
		}
	}

	return nil
}

func applyInboundSocketOptions(network string, address string, fd uintptr, config *SocketConfig) error {
	if isTCPSocket(network) {
		if config.TcpKeepAliveIdle > 0 {
			if err := syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1); err != nil {
				return newError("failed to set SO_KEEPALIVE", err)
			}
		}
	}

	if config.BindToDevice != "" {
		iface, err := net.InterfaceByName(config.BindToDevice)
		if err != nil {
			return newError("failed to get interface ", config.BindToDevice).Base(err)
		}
		dest, err := v2net.ParseDestination(address)
		multiCast := err == nil && dest.Address.Family().IsIP() && dest.Address.IP().IsMulticast()
		// bind is always used in the default dialer
		switch network {
		case "tcp4", "udp4":
			var bytes [4]byte
			binary.BigEndian.PutUint32(bytes[:], uint32(iface.Index))
			index := *(*uint32)(unsafe.Pointer(&bytes[0]))
			if network == "udp4" || multiCast {
				if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_MULTICAST_IF, int(index)); err != nil {
					return newError("failed to set IP_MULTICAST_IF", err)
				}
			}
			if network == "udp4" || !multiCast {
				if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, IP_UNICAST_IF, int(index)); err != nil {
					return newError("failed to set IP_UNICAST_IF", err)
				}
			}
		case "tcp6", "udp6":
			if network == "udp6" || multiCast {
				if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, windows.IPV6_MULTICAST_IF, iface.Index); err != nil {
					return newError("failed to set IPV6_MULTICAST_IF", err)
				}
			}
			if network == "udp6" || !multiCast {
				if err := windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, IPV6_UNICAST_IF, iface.Index); err != nil {
					return newError("failed to set IPV6_UNICAST_IF", err)
				}
			}
		}
	}

	if config.TxBufSize != 0 {
		if err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_SNDBUF, int(config.TxBufSize)); err != nil {
			return newError("failed to set SO_SNDBUF").Base(err)
		}
	}

	if config.RxBufSize != 0 {
		if err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_RCVBUF, int(config.TxBufSize)); err != nil {
			return newError("failed to set SO_RCVBUF").Base(err)
		}
	}

	return nil
}

func setReusePort(_ uintptr) error {
	return nil
}
