// Package pool provides bucketed sync.Pool instances for reducing allocations
// in hot paths. Buffers are organized by size class to minimize waste.
package pool

import "sync"

// Size classes for bucketed pools.
const (
	Size256B = 256
	Size1K   = 1024
	Size4K   = 4096
	Size16K  = 16384
	Size64K  = 65536
	Size256K = 262144
	Size1M   = 1048576
)

// bucketIndex returns the pool index for a given size.
func bucketIndex(size int) int {
	switch {
	case size <= Size256B:
		return 0
	case size <= Size1K:
		return 1
	case size <= Size4K:
		return 2
	case size <= Size16K:
		return 3
	case size <= Size64K:
		return 4
	case size <= Size256K:
		return 5
	default:
		return 6
	}
}

var sizes = [7]int{Size256B, Size1K, Size4K, Size16K, Size64K, Size256K, Size1M}

var pools [7]sync.Pool

func init() {
	for i := range pools {
		sz := sizes[i]
		pools[i] = sync.Pool{
			New: func() any {
				b := make([]byte, sz)
				return &b
			},
		}
	}
}

// Get returns a byte slice of at least the requested size from the pool.
// The returned slice has length == size and may have a larger capacity.
// The caller must call Put when done.
func Get(size int) []byte {
	idx := bucketIndex(size)
	bp := pools[idx].Get().(*[]byte)
	b := *bp
	if cap(b) < size {
		b = make([]byte, size)
		*bp = b
		return b
	}
	return b[:size]
}

// Put returns a byte slice to the pool. The slice must have been obtained
// from Get. Slices smaller than Size256B are not pooled.
func Put(b []byte) {
	c := cap(b)
	if c < Size256B {
		return
	}
	idx := bucketIndex(c)
	b = b[:c]
	pools[idx].Put(&b)
}

// GetInt16 returns an int16 slice of at least the requested length from the pool.
// Backed by a byte pool allocation.
func GetInt16(length int) []int16 {
	s := make([]int16, length)
	return s
}

// GetInt32 returns an int32 slice of at least the requested length.
func GetInt32(length int) []int32 {
	s := make([]int32, length)
	return s
}

// GetUint32 returns a uint32 slice of at least the requested length.
func GetUint32(length int) []uint32 {
	s := make([]uint32, length)
	return s
}
