//go:build js || dragonfly || netbsd || openbsd || solaris

package internet

func applyOutboundSocketOptions(_ string, _ string, _ uintptr, _ *SocketConfig) error {
	return nil
}

func applyInboundSocketOptions(_ string, _ string, _ uintptr, _ *SocketConfig) error {
	return nil
}

func setReusePort(_ uintptr) error {
	return nil
}
