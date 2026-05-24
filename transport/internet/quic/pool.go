package quic

import (
	"sync"

	"github.com/exclavenetwork/exclave-core/v5/common/buf"
	"github.com/exclavenetwork/exclave-core/v5/common/bytespool"
)

var pool *sync.Pool

func init() {
	pool = bytespool.GetPool(buf.Size)
}

func getBuffer() []byte {
	return pool.Get().([]byte)
}

func putBuffer(p []byte) {
	pool.Put(p) // nolint: staticcheck
}
