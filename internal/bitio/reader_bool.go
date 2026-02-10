// Package bitio provides bit-level I/O primitives for the WebP codec.
//
// It implements both the VP8 boolean (arithmetic) coding used by lossy WebP
// and the VP8L bit-packing used by lossless WebP. The implementations are
// faithful Go translations of the corresponding C routines in libwebp.
package bitio

import (
	"encoding/binary"
	"math/bits"
)

// boolBITS is the number of cached look-ahead bits kept in the value register.
// On 64-bit Go this is always 56 (7 bytes at a time).
const boolBITS = 56

// BoolReader implements the VP8 boolean (arithmetic) decoder.
//
// The algorithm maintains a probability-weighted interval [0, range] and
// narrows it on every decoded symbol. A 64-bit value register caches up
// to 56 look-ahead bits so that bulk byte loads are amortised over many
// decoded symbols.
type BoolReader struct {
	value  uint64 // current value register (BITS+8 bits active)
	range_ uint32 // current range minus 1, kept in [127, 254]
	bits   int    // number of valid bits remaining in value
	buf    []byte // input byte buffer
	pos    int    // current read position in buf
	eof    bool   // true once input is exhausted
}

// NewBoolReader creates a BoolReader over the given byte slice and loads
// the initial bits into the value register.
func NewBoolReader(data []byte) *BoolReader {
	br := &BoolReader{
		range_: 255 - 1,
		value:  0,
		bits:   -8, // forces an immediate load of the first bytes
		buf:    data,
	}
	br.loadNewBytes()
	return br
}

// loadNewBytes reads up to 7 bytes (56 bits) from the input buffer into the
// value register. When fewer than 7 bytes remain the slow path
// loadFinalBytes is used instead.
func (br *BoolReader) loadNewBytes() {
	// Fast path: at least 8 bytes available (we read 8, use 7).
	if br.pos+8 <= len(br.buf) {
		// Read 8 bytes as little-endian uint64, byte-swap to big-endian,
		// then discard the lowest byte (shift right by 8) to get 56 bits
		// in the correct MSB-first order.
		in := binary.LittleEndian.Uint64(br.buf[br.pos:])
		in = bits.ReverseBytes64(in)
		in >>= 64 - boolBITS
		br.value = in | (br.value << boolBITS)
		br.pos += boolBITS >> 3
		br.bits += boolBITS
	} else {
		br.loadFinalBytes()
	}
}

// loadFinalBytes reads one byte at a time when the remaining buffer is too
// short for a bulk load.
func (br *BoolReader) loadFinalBytes() {
	if br.pos < len(br.buf) {
		br.bits += 8
		br.value = uint64(br.buf[br.pos]) | (br.value << 8)
		br.pos++
	} else if !br.eof {
		br.value <<= 8
		br.bits += 8
		br.eof = true
	} else {
		br.bits = 0 // avoid undefined behaviour with shifts
	}
}

// GetBit decodes a single boolean symbol using the given probability (0..255).
//
// This is the speed-critical inner loop of the VP8 decoder. The algorithm is
// a direct translation of VP8GetBit from libwebp/bit_reader_inl_utils.h.
func (br *BoolReader) GetBit(prob uint8) int {
	// Cache range before potential byte load -- this ordering matters for
	// performance even in Go.
	range_ := br.range_
	if br.bits < 0 {
		br.loadNewBytes()
	}

	pos := br.bits
	split := (range_ * uint32(prob)) >> 8
	value := uint32(br.value >> uint(pos))

	var bit int
	if value > split {
		bit = 1
		range_ -= split
		br.value -= uint64(split+1) << uint(pos)
	} else {
		range_ = split + 1
	}

	// Normalise: shift range up so that the MSB is in bit 7.
	// bits.Len32 returns floor(log2(x))+1, so subtract 1 to match BitsLog2Floor.
	shift := 7 ^ (bits.Len32(range_) - 1)
	range_ <<= uint(shift)
	br.bits -= shift

	br.range_ = range_ - 1
	return bit
}

// GetBitAlt decodes a single boolean symbol using lookup tables instead of
// bit-counting. This matches VP8GetBitAlt in libwebp and may be faster on
// some platforms.
func (br *BoolReader) GetBitAlt(prob uint8) int {
	range_ := br.range_
	if br.bits < 0 {
		br.loadNewBytes()
	}

	pos := br.bits
	split := (range_ * uint32(prob)) >> 8
	value := uint32(br.value >> uint(pos))

	var bit int
	if value > split {
		range_ -= split + 1
		br.value -= uint64(split+1) << uint(pos)
		bit = 1
	} else {
		range_ = split
		bit = 0
	}

	if range_ <= 0x7e {
		shift := int(kVP8Log2Range[range_])
		range_ = uint32(kVP8NewRange[range_])
		br.bits -= shift
	}

	br.range_ = range_
	return bit
}

// GetSigned is a simplified version of GetBit for prob = 0x80.
// It returns +v or -v depending on the decoded sign bit.
func (br *BoolReader) GetSigned(v int) int {
	if br.bits < 0 {
		br.loadNewBytes()
	}

	pos := br.bits
	split := br.range_ >> 1
	value := uint32(br.value >> uint(pos))

	// mask is -1 when value >= split+1, 0 otherwise
	mask := int32(split-value) >> 31

	br.bits--
	br.range_ += uint32(mask)
	br.range_ |= 1
	br.value -= uint64((split+1)&uint32(mask)) << uint(pos)

	return (v ^ int(mask)) - int(mask)
}

// GetValue reads numBits bits MSB-first, each decoded with uniform
// probability (prob = 0x80).
func (br *BoolReader) GetValue(numBits int) uint32 {
	var v uint32
	for i := numBits - 1; i >= 0; i-- {
		v |= uint32(br.GetBit(0x80)) << uint(i)
	}
	return v
}

// GetSignedValue reads a numBits unsigned value followed by a sign bit.
// If the sign bit is set, the value is negated.
func (br *BoolReader) GetSignedValue(numBits int) int32 {
	value := int32(br.GetValue(numBits))
	if br.GetBit(0x80) != 0 {
		return -value
	}
	return value
}

// EOF reports whether the reader has reached the end of the input buffer.
func (br *BoolReader) EOF() bool {
	return br.eof
}

// kVP8Log2Range maps range values [0..127] to the number of left-shifts
// needed for normalisation: 7 - floor(log2(range)).
var kVP8Log2Range = [128]uint8{
	7, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0,
}

// kVP8NewRange maps range values [0..127] to the normalised range after
// shifting: ((range - 1) << kVP8Log2Range[range]) + 1.
var kVP8NewRange = [128]uint8{
	127, 127, 191, 127, 159, 191, 223, 127, 143, 159, 175, 191, 207, 223, 239,
	127, 135, 143, 151, 159, 167, 175, 183, 191, 199, 207, 215, 223, 231, 239,
	247, 127, 131, 135, 139, 143, 147, 151, 155, 159, 163, 167, 171, 175, 179,
	183, 187, 191, 195, 199, 203, 207, 211, 215, 219, 223, 227, 231, 235, 239,
	243, 247, 251, 127, 129, 131, 133, 135, 137, 139, 141, 143, 145, 147, 149,
	151, 153, 155, 157, 159, 161, 163, 165, 167, 169, 171, 173, 175, 177, 179,
	181, 183, 185, 187, 189, 191, 193, 195, 197, 199, 201, 203, 205, 207, 209,
	211, 213, 215, 217, 219, 221, 223, 225, 227, 229, 231, 233, 235, 237, 239,
	241, 243, 245, 247, 249, 251, 253, 127,
}
