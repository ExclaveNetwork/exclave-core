package pool

const (
	// RelayBufferSize is the default per-direction buffer size used by Relay.
	RelayBufferSize = 32 * 1024
)

func Get(size int) []byte {
	return defaultAllocator.Get(size)
}

func Put(buf []byte) error {
	return defaultAllocator.Put(buf)
}
