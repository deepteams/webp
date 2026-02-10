package bitio

import "encoding/binary"

const (
	// vp8lMaxNumBitRead is the maximum number of bits that can be read
	// in a single ReadBits call.
	vp8lMaxNumBitRead = 24
	// vp8lLBits is the total number of prefetched bits (= bit-size of val).
	vp8lLBits = 64
	// vp8lWBits is the minimum number of ready bits after FillBitWindow.
	vp8lWBits = 32
)

// LosslessReader implements the VP8L bit reader with 64-bit prefetch.
//
// Unlike the boolean decoder used by lossy VP8, VP8L packs raw bit fields
// in little-endian byte order. The reader maintains a 64-bit sliding window
// (val) and advances through the source buffer 4 bytes at a time.
type LosslessReader struct {
	val    uint64 // pre-fetched bits
	buf    []byte // input byte buffer
	len_   int    // buffer length
	pos    int    // byte position in buf
	bitPos int    // current bit-reading position in val
	eos    bool   // end of stream flag
}

// NewLosslessReader creates a LosslessReader over the given byte slice,
// pre-loading the first 8 (or fewer) bytes into the val register.
func NewLosslessReader(data []byte) *LosslessReader {
	br := &LosslessReader{
		buf:  data,
		len_: len(data),
	}

	// Load initial bytes into val in little-endian order.
	n := len(data)
	if n > 8 {
		n = 8
	}
	var value uint64
	for i := 0; i < n; i++ {
		value |= uint64(data[i]) << uint(8*i)
	}
	br.val = value
	br.pos = n
	return br
}

// FillBitWindow ensures that at least vp8lWBits (32) bits are available
// in the val register. It loads 4 new bytes when possible, falling back
// to a byte-by-byte shift when near the end of the buffer.
func (br *LosslessReader) FillBitWindow() {
	if br.bitPos >= vp8lWBits {
		br.doFillBitWindow()
	}
}

func (br *LosslessReader) doFillBitWindow() {
	// Fast path: 4+ bytes remain.
	if br.pos+4 <= br.len_ {
		br.val >>= vp8lWBits
		br.bitPos -= vp8lWBits
		br.val |= uint64(binary.LittleEndian.Uint32(br.buf[br.pos:])) << (vp8lLBits - vp8lWBits)
		br.pos += 4
		return
	}
	// Slow path.
	br.shiftBytes()
}

// shiftBytes loads individual bytes into val until bitPos < 8 or the
// buffer is exhausted.
func (br *LosslessReader) shiftBytes() {
	for br.bitPos >= 8 && br.pos < br.len_ {
		br.val >>= 8
		br.val |= uint64(br.buf[br.pos]) << (vp8lLBits - 8)
		br.pos++
		br.bitPos -= 8
	}
	if br.IsEndOfStream() {
		br.setEndOfStream()
	}
}

func (br *LosslessReader) setEndOfStream() {
	br.eos = true
	br.bitPos = 0 // avoid undefined behaviour with shifts
}

// ReadBits reads nBits (0..24) from the bitstream and returns them as an
// unsigned 32-bit value. If nBits exceeds vp8lMaxNumBitRead or the stream
// is already at EOS, zero is returned and the EOS flag is set.
func (br *LosslessReader) ReadBits(nBits int) uint32 {
	if !br.eos && nBits >= 0 && nBits <= vp8lMaxNumBitRead {
		val := br.PrefetchBits() & kBitMask[nBits]
		br.bitPos += nBits
		br.shiftBytes()
		return val
	}
	br.setEndOfStream()
	return 0
}

// PrefetchBits returns the next bits from the val register without
// advancing the bit position. The caller must call FillBitWindow
// beforehand to guarantee enough bits are available.
func (br *LosslessReader) PrefetchBits() uint32 {
	return uint32(br.val >> uint(br.bitPos&(vp8lLBits-1)))
}

// SetBitPos overwrites the current bit position. This is used when the
// caller has inspected prefetched bits and wants to skip a known number
// of them.
func (br *LosslessReader) SetBitPos(val int) {
	br.bitPos = val
}

// BitPos returns the current bit position inside the val register.
func (br *LosslessReader) BitPos() int {
	return br.bitPos
}

// IsEndOfStream reports whether the reader has attempted to read past the
// end of the buffer.
func (br *LosslessReader) IsEndOfStream() bool {
	return br.eos || (br.pos == br.len_ && br.bitPos > vp8lLBits)
}

// kBitMask maps nBits (0..24) to the corresponding mask (2^n - 1).
var kBitMask = [vp8lMaxNumBitRead + 1]uint32{
	0x000000, // 0
	0x000001, // 1
	0x000003, // 2
	0x000007, // 3
	0x00000f, // 4
	0x00001f, // 5
	0x00003f, // 6
	0x00007f, // 7
	0x0000ff, // 8
	0x0001ff, // 9
	0x0003ff, // 10
	0x0007ff, // 11
	0x000fff, // 12
	0x001fff, // 13
	0x003fff, // 14
	0x007fff, // 15
	0x00ffff, // 16
	0x01ffff, // 17
	0x03ffff, // 18
	0x07ffff, // 19
	0x0fffff, // 20
	0x1fffff, // 21
	0x3fffff, // 22
	0x7fffff, // 23
	0xffffff, // 24
}
