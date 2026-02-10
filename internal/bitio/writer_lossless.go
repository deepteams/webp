package bitio

import "encoding/binary"

const (
	// writerBits is the number of bits flushed at a time (32 on 64-bit).
	writerBits = 32
	// writerBytes is the number of bytes written per flush (4 on 64-bit).
	writerBytes = 4
)

// LosslessWriter implements the VP8L accumulator-based bit writer.
//
// Bits are accumulated in a 64-bit register and flushed 32 bits (4 bytes)
// at a time in little-endian byte order. This matches the format expected
// by LosslessReader.
type LosslessWriter struct {
	bits uint64 // bit accumulator
	used int    // number of bits used in accumulator
	buf  []byte // output buffer
	cur  int    // current write position in buf
	err  error
}

// NewLosslessWriter creates a LosslessWriter with an initial buffer
// pre-allocated for expectedSize bytes.
func NewLosslessWriter(expectedSize int) *LosslessWriter {
	if expectedSize < 1024 {
		expectedSize = 1024
	}
	// Round up to the next 1k boundary.
	expectedSize = ((expectedSize >> 10) + 1) << 10
	return &LosslessWriter{
		buf: make([]byte, expectedSize),
	}
}

// WriteBits writes nBits (0..64) from the lower bits of v into the
// bitstream in little-endian order.
func (bw *LosslessWriter) WriteBits(v uint32, nBits int) {
	if nBits == 0 {
		return
	}
	if bw.used >= writerBits {
		bw.flushBits()
	}
	bw.bits |= uint64(v) << uint(bw.used)
	bw.used += nBits
}

// flushBits writes the lower 32 bits of the accumulator to the output
// buffer as 4 little-endian bytes and shifts the accumulator right by 32.
func (bw *LosslessWriter) flushBits() {
	bw.grow(writerBytes)
	binary.LittleEndian.PutUint32(bw.buf[bw.cur:], uint32(bw.bits))
	bw.cur += writerBytes
	bw.bits >>= writerBits
	bw.used -= writerBits
}

// grow ensures at least n bytes of capacity remain at bw.cur.
func (bw *LosslessWriter) grow(n int) {
	if bw.cur+n <= len(bw.buf) {
		return
	}
	newSize := len(bw.buf) * 3 / 2
	need := bw.cur + n
	if newSize < need {
		newSize = need
	}
	// Round up to next 1k boundary.
	newSize = ((newSize >> 10) + 1) << 10
	tmp := make([]byte, newSize)
	copy(tmp, bw.buf[:bw.cur])
	bw.buf = tmp
}

// Finish flushes all remaining bits to the output buffer and returns
// the complete encoded byte slice.
func (bw *LosslessWriter) Finish() []byte {
	// Flush full 32-bit words while possible.
	for bw.used >= writerBits {
		bw.flushBits()
	}
	// Flush remaining bytes one at a time.
	bw.grow((bw.used + 7) >> 3)
	for bw.used > 0 {
		bw.buf[bw.cur] = byte(bw.bits)
		bw.cur++
		bw.bits >>= 8
		bw.used -= 8
	}
	bw.used = 0
	return bw.buf[:bw.cur]
}

// NumBytes returns the number of encoded bytes, including any partial
// byte in the accumulator.
func (bw *LosslessWriter) NumBytes() int {
	return bw.cur + (bw.used+7)/8
}

// Err returns the first error encountered during writing, if any.
func (bw *LosslessWriter) Err() error {
	return bw.err
}
