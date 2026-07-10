package pool

// Borrowed from https://github.com/Dreamacro/clash/common/pool.

import (
	"errors"
	"math/bits"
	"sync"
)

var defaultAllocator *Allocator

func init() {
	defaultAllocator = NewAllocator()
}

// Allocator hands out []byte from sync.Pool buckets, keyed by power-of-two
// capacity. Memory fragmentation is bounded at 50% by always returning a
// buffer from the next-larger bucket if the requested size is not exactly
// a power of two.
type Allocator struct {
	buffers []sync.Pool
}

func NewAllocator() *Allocator {
	alloc := new(Allocator)
	alloc.buffers = make([]sync.Pool, 17) // 1B -> 64K
	for k := range alloc.buffers {
		i := k
		alloc.buffers[k].New = func() any {
			return make([]byte, 1<<uint32(i))
		}
	}
	return alloc
}

func (alloc *Allocator) Get(size int) []byte {
	if size <= 0 || size > 65536 {
		return nil
	}
	b := msb(size)
	if size == 1<<b {
		return alloc.buffers[b].Get().([]byte)[:size]
	}
	return alloc.buffers[b+1].Get().([]byte)[:size]
}

func (alloc *Allocator) Put(buf []byte) error {
	b := msb(cap(buf))
	if cap(buf) == 0 || cap(buf) > 65536 || cap(buf) != 1<<b {
		return errors.New("allocator Put() incorrect buffer size")
	}
	alloc.buffers[b].Put(buf) //nolint:staticcheck
	return nil
}

func msb(size int) uint16 {
	return uint16(bits.Len32(uint32(size)) - 1)
}
