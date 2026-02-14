package bitio

// BoolWriter implements the VP8 boolean (arithmetic) encoder.
//
// It is the encoding counterpart of BoolReader. Symbols are encoded by
// narrowing a probability-weighted interval and emitting bytes as the
// interval shrinks below the normalisation threshold.
//
// The implementation is a direct translation of VP8BitWriter from
// libwebp/bit_writer_utils.c.
type BoolWriter struct {
	range_ int32 // current range (kept around 128..255 via renormalisation)
	value  int32 // current fractional value
	run    int   // count of pending 0xff bytes (carry propagation)
	nbBits int   // number of pending bits; when > 0, Flush emits a byte
	buf    []byte
	pos    int
	err    error
}

// NewBoolWriter creates a BoolWriter with an initial buffer sized for
// expectedSize bytes. Pass 0 for a minimal default allocation.
func NewBoolWriter(expectedSize int) *BoolWriter {
	if expectedSize < 1024 {
		expectedSize = 1024
	}
	return &BoolWriter{
		range_: 255 - 1,
		value:  0,
		run:    0,
		nbBits: -8,
		buf:    make([]byte, 0, expectedSize),
	}
}

// Reset resets the BoolWriter state for reuse, keeping the existing buffer
// if it has sufficient capacity. This avoids re-allocation when encoding
// multiple frames or partitions of similar size.
func (bw *BoolWriter) Reset(expectedSize int) {
	if expectedSize < 1024 {
		expectedSize = 1024
	}
	if cap(bw.buf) >= expectedSize {
		bw.buf = bw.buf[:0]
	} else {
		bw.buf = make([]byte, 0, expectedSize)
	}
	bw.range_ = 255 - 1
	bw.value = 0
	bw.run = 0
	bw.nbBits = -8
	bw.pos = 0
	bw.err = nil
}

// PutBit encodes a single boolean symbol with the given probability
// (0..255, where 255 ~ certain 0). Returns the input bit unchanged.
func (bw *BoolWriter) PutBit(bit int, prob int) int {
	split := (int32(bw.range_) * int32(prob)) >> 8
	if bit != 0 {
		bw.value += split + 1
		bw.range_ -= split + 1
	} else {
		bw.range_ = split
	}
	if bw.range_ < 127 {
		shift := kNorm[bw.range_]
		bw.range_ = int32(kNewRange[bw.range_])
		bw.value <<= uint(shift)
		bw.nbBits += int(shift)
		if bw.nbBits > 0 {
			bw.flush()
		}
	}
	return bit
}

// PutBitUniform encodes a single boolean symbol with uniform probability
// (prob = 128, i.e. 50/50).
func (bw *BoolWriter) PutBitUniform(bit int) int {
	split := bw.range_ >> 1
	if bit != 0 {
		bw.value += split + 1
		bw.range_ -= split + 1
	} else {
		bw.range_ = split
	}
	if bw.range_ < 127 {
		bw.range_ = int32(kNewRange[bw.range_])
		bw.value <<= 1
		bw.nbBits++
		if bw.nbBits > 0 {
			bw.flush()
		}
	}
	return bit
}

// PutBitBatchPacked encodes count boolean symbols from packed bit/prob pairs.
// Each pair occupies 2 consecutive bytes: [bit, prob, bit, prob, ...].
// The data slice must have at least count*2 bytes.
//
// This is optimized for large batches (e.g., token emission): the encoder's
// hot state (range_, value, nbBits) is kept in local variables (registers)
// instead of reading/writing through the struct pointer on each iteration.
func (bw *BoolWriter) PutBitBatchPacked(data []byte, count int) {
	if count <= 0 {
		return
	}
	_ = data[count*2-1] // BCE hint
	r := bw.range_
	v := bw.value
	nb := bw.nbBits
	for i := 0; i < count; i++ {
		bit := data[i*2]
		prob := int32(data[i*2+1])
		split := (r * prob) >> 8
		if bit != 0 {
			v += split + 1
			r -= split + 1
		} else {
			r = split
		}
		if r < 127 {
			shift := kNorm[r]
			r = int32(kNewRange[r])
			v <<= uint(shift)
			nb += int(shift)
			if nb > 0 {
				// Write back before flush (flush reads from bw fields).
				bw.range_ = r
				bw.value = v
				bw.nbBits = nb
				bw.flush()
				// Read back after flush (flush modifies value and nbBits).
				v = bw.value
				nb = bw.nbBits
			}
		}
	}
	bw.range_ = r
	bw.value = v
	bw.nbBits = nb
}

// PutBits encodes nbBits bits from value, MSB first, each with uniform
// probability.
func (bw *BoolWriter) PutBits(value uint32, nbBits int) {
	for mask := uint32(1) << uint(nbBits-1); mask != 0; mask >>= 1 {
		bit := 0
		if value&mask != 0 {
			bit = 1
		}
		bw.PutBitUniform(bit)
	}
}

// PutSignedBits encodes a signed value. First a flag bit indicates whether
// value is non-zero. If non-zero, the absolute value (nbBits) and a sign
// bit are encoded.
func (bw *BoolWriter) PutSignedBits(value int, nbBits int) {
	if bw.PutBitUniform(boolToInt(value != 0)) == 0 {
		return
	}
	if value < 0 {
		bw.PutBits(uint32(-value)<<1|1, nbBits+1)
	} else {
		bw.PutBits(uint32(value)<<1, nbBits+1)
	}
}

// flush emits one byte from the value register, handling carry propagation
// through any pending 0xff bytes.
func (bw *BoolWriter) flush() {
	s := 8 + bw.nbBits
	bits := bw.value >> uint(s)
	bw.value -= bits << uint(s)
	bw.nbBits -= 8
	if bits&0xff != 0xff {
		if bits&0x100 != 0 {
			// Carry: increment the last written byte.
			if bw.pos > 0 {
				bw.buf[bw.pos-1]++
			}
		}
		if bw.run > 0 {
			val := byte(0xff)
			if bits&0x100 != 0 {
				val = 0x00
			}
			for ; bw.run > 0; bw.run-- {
				bw.buf = append(bw.buf, val)
				bw.pos++
			}
		}
		bw.buf = append(bw.buf, byte(bits&0xff))
		bw.pos++
	} else {
		bw.run++ // delay writing 0xff bytes, pending eventual carry
	}
}

// Finish finalises the bitstream by flushing all remaining bits and
// returns the encoded byte slice.
func (bw *BoolWriter) Finish() []byte {
	bw.PutBits(0, 9-bw.nbBits)
	bw.nbBits = 0
	bw.flush()
	return bw.buf[:bw.pos]
}

// Bytes returns the encoded bytes written so far (without finalising).
func (bw *BoolWriter) Bytes() []byte {
	return bw.buf[:bw.pos]
}

// Err returns the first error encountered during writing, if any.
func (bw *BoolWriter) Err() error {
	return bw.err
}

// Pos returns the approximate write position in bits.
func (bw *BoolWriter) Pos() uint64 {
	nb := uint64(8 + bw.nbBits) // nbBits is <= 0
	return uint64(bw.pos+bw.run)*8 + nb
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// kNorm maps range values [0..127] to the shift count needed for
// renormalisation: 8 - floor(log2(range+1)).
var kNorm = [128]uint8{
	7, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0,
}

// kNewRange maps range values [0..127] to the normalised range after
// shifting: ((range + 1) << kNorm[range]) - 1.
var kNewRange = [128]uint8{
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
