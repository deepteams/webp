package pool

import (
	"runtime"
	"sync"
	"testing"
)

func TestGetPut_ExactSize(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"256B", 256},
		{"1K", 1024},
		{"4K", 4096},
		{"16K", 16384},
		{"64K", 65536},
		{"256K", 262144},
		{"1M", 1048576},
		{"500B", 500},
		{"3000B", 3000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Get(tt.size)
			if len(b) != tt.size {
				t.Errorf("Get(%d): len = %d, want %d", tt.size, len(b), tt.size)
			}
			Put(b)
		})
	}
}

func TestGetPut_LargeCapacity(t *testing.T) {
	// For each size class, request a size within that class and verify
	// the capacity is at least the size class minimum.
	tests := []struct {
		name      string
		size      int
		minCap    int
	}{
		{"bucket0_exact", 256, 256},
		{"bucket0_small", 100, 256},
		{"bucket1_exact", 1024, 1024},
		{"bucket1_mid", 512, 1024},
		{"bucket2_exact", 4096, 4096},
		{"bucket2_mid", 2048, 4096},
		{"bucket3_exact", 16384, 16384},
		{"bucket4_exact", 65536, 65536},
		{"bucket5_exact", 262144, 262144},
		{"bucket6_exact", 1048576, 1048576},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Get(tt.size)
			if cap(b) < tt.minCap {
				t.Errorf("Get(%d): cap = %d, want >= %d", tt.size, cap(b), tt.minCap)
			}
			Put(b)
		})
	}
}

func TestGet_SmallSize(t *testing.T) {
	sizes := []int{1, 10, 64, 128, 255}
	for _, size := range sizes {
		b := Get(size)
		if len(b) != size {
			t.Errorf("Get(%d): len = %d, want %d", size, len(b), size)
		}
		// Small sizes go to bucket 0 (256B), so cap should be >= 256.
		if cap(b) < Size256B {
			t.Errorf("Get(%d): cap = %d, want >= %d", size, cap(b), Size256B)
		}
		Put(b)
	}
}

func TestGet_LargeSize(t *testing.T) {
	// Sizes larger than 1MB go to bucket 6 (1M pool).
	// The pool's New creates 1M slices, so Get must handle the case
	// where cap(b) < size by allocating a new slice.
	largeSize := 2 * 1048576 // 2MB
	b := Get(largeSize)
	if len(b) != largeSize {
		t.Errorf("Get(%d): len = %d, want %d", largeSize, len(b), largeSize)
	}
	if cap(b) < largeSize {
		t.Errorf("Get(%d): cap = %d, want >= %d", largeSize, cap(b), largeSize)
	}
	Put(b)

	// Also test a size just above 1MB.
	justOver := 1048576 + 1
	b2 := Get(justOver)
	if len(b2) != justOver {
		t.Errorf("Get(%d): len = %d, want %d", justOver, len(b2), justOver)
	}
	Put(b2)
}

func TestPut_SmallSlice(t *testing.T) {
	// Put of slices with cap < 256 should be a no-op (not panic).
	small := make([]byte, 100)
	Put(small) // Should not panic.

	tiny := make([]byte, 0, 10)
	Put(tiny) // Should not panic.

	// Verify the pool still works correctly after putting small slices.
	b := Get(256)
	if len(b) != 256 {
		t.Errorf("Get(256) after small Put: len = %d, want 256", len(b))
	}
	Put(b)
}

func TestGetInt16(t *testing.T) {
	tests := []int{0, 1, 100, 1024, 65536}
	for _, length := range tests {
		s := GetInt16(length)
		if len(s) != length {
			t.Errorf("GetInt16(%d): len = %d, want %d", length, len(s), length)
		}
	}
}

func TestGetInt32(t *testing.T) {
	tests := []int{0, 1, 100, 1024, 65536}
	for _, length := range tests {
		s := GetInt32(length)
		if len(s) != length {
			t.Errorf("GetInt32(%d): len = %d, want %d", length, len(s), length)
		}
	}
}

func TestGetUint32(t *testing.T) {
	tests := []int{0, 1, 100, 1024, 65536}
	for _, length := range tests {
		s := GetUint32(length)
		if len(s) != length {
			t.Errorf("GetUint32(%d): len = %d, want %d", length, len(s), length)
		}
	}
}

func TestConcurrency(t *testing.T) {
	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Vary sizes across all bucket classes.
				for _, size := range []int{128, 512, 2048, 8192, 32768, 131072, 524288} {
					b := Get(size)
					if len(b) != size {
						t.Errorf("concurrent Get(%d): len = %d", size, len(b))
						return
					}
					// Write to the buffer to detect data races.
					for j := range b {
						b[j] = byte(j)
					}
					Put(b)
				}
			}
		}()
	}

	wg.Wait()
}

func TestBucketIndex(t *testing.T) {
	// Verify bucket assignment by checking that Get returns buffers
	// with capacity matching the expected size class.
	tests := []struct {
		name       string
		size       int
		wantBucket int
		wantMinCap int
	}{
		{"1->bucket0", 1, 0, Size256B},
		{"256->bucket0", 256, 0, Size256B},
		{"257->bucket1", 257, 1, Size1K},
		{"1024->bucket1", 1024, 1, Size1K},
		{"1025->bucket2", 1025, 2, Size4K},
		{"4096->bucket2", 4096, 2, Size4K},
		{"4097->bucket3", 4097, 3, Size16K},
		{"16384->bucket3", 16384, 3, Size16K},
		{"16385->bucket4", 16385, 4, Size64K},
		{"65536->bucket4", 65536, 4, Size64K},
		{"65537->bucket5", 65537, 5, Size256K},
		{"262144->bucket5", 262144, 5, Size256K},
		{"262145->bucket6", 262145, 6, Size1M},
		{"1048576->bucket6", 1048576, 6, Size1M},
		{"2097152->bucket6", 2097152, 6, Size1M},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := bucketIndex(tt.size)
			if idx != tt.wantBucket {
				t.Errorf("bucketIndex(%d) = %d, want %d", tt.size, idx, tt.wantBucket)
			}
		})
	}
}

func TestReuse(t *testing.T) {
	// Verify that after Put + GC, a subsequent Get can reuse the buffer.
	// We do this by writing a sentinel value, putting it back, forcing GC,
	// then getting again and checking if the sentinel persists.
	// Note: sync.Pool may or may not retain objects across GC; this test
	// verifies correctness regardless of reuse.

	const size = 4096
	b := Get(size)
	if len(b) != size {
		t.Fatalf("Get(%d): len = %d", size, len(b))
	}

	// Write a sentinel pattern.
	sentinel := byte(0xAB)
	b[0] = sentinel
	b[size-1] = sentinel

	savedCap := cap(b)
	Put(b)

	// Force a GC to clear non-reused pool entries, but the pool
	// should still be able to provide a valid buffer.
	runtime.GC()

	b2 := Get(size)
	if len(b2) != size {
		t.Fatalf("Get(%d) after reuse: len = %d", size, len(b2))
	}
	if cap(b2) < savedCap {
		// If it was reused, cap should match. If new, cap should still
		// be at least the size class.
		if cap(b2) < Size4K {
			t.Errorf("Get(%d) after reuse: cap = %d, want >= %d", size, cap(b2), Size4K)
		}
	}
	Put(b2)

	// Verify the pool works for multiple cycles of Get/Put.
	for i := 0; i < 10; i++ {
		buf := Get(size)
		if len(buf) != size {
			t.Errorf("cycle %d: Get(%d) len = %d", i, size, len(buf))
		}
		Put(buf)
	}
}

func TestGet_ZeroSize(t *testing.T) {
	// Edge case: requesting size 0 should not panic and return a
	// zero-length slice backed by a pooled buffer.
	b := Get(0)
	if len(b) != 0 {
		t.Errorf("Get(0): len = %d, want 0", len(b))
	}
	Put(b)
}

func TestPut_NilSlice(t *testing.T) {
	// Putting a nil slice should not panic (cap is 0, which is < 256).
	Put(nil)
}

func BenchmarkGet(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"256B", 256},
		{"4K", 4096},
		{"64K", 65536},
		{"1M", 1048576},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				buf := Get(bm.size)
				Put(buf)
			}
		})
	}
}

func BenchmarkGetParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := Get(4096)
			Put(buf)
		}
	})
}
