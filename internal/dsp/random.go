package dsp

// VP8Random implements the pseudo-random number generator used by C libwebp
// for dithering during RGB->YUV conversion. It uses D. Knuth's
// difference-based random generator with a 55-element table.
//
// Reference: libwebp/src/utils/random_utils.h and random_utils.c.

const (
	// vp8RandomDitherFix is the fixed-point precision for the dithering amplitude.
	vp8RandomDitherFix = 8
	// vp8RandomTableSize is the number of entries in the random table.
	vp8RandomTableSize = 55
)

// VP8Random holds the state of the pseudo-random generator.
type VP8Random struct {
	index1, index2 int
	tab            [vp8RandomTableSize]uint32
	amp            int
}

// kRandomTable contains 31-bit random seed values, matching C libwebp exactly.
var kRandomTable = [vp8RandomTableSize]uint32{
	0x0de15230, 0x03b31886, 0x775faccb, 0x1c88626a, 0x68385c55, 0x14b3b828,
	0x4a85fef8, 0x49ddb84b, 0x64fcf397, 0x5c550289, 0x4a290000, 0x0d7ec1da,
	0x5940b7ab, 0x5492577d, 0x4e19ca72, 0x38d38c69, 0x0c01ee65, 0x32a1755f,
	0x5437f652, 0x5abb2c32, 0x0faa57b1, 0x73f533e7, 0x685feeda, 0x7563cce2,
	0x6e990e83, 0x4730a7ed, 0x4fc0d9c6, 0x496b153c, 0x4f1403fa, 0x541afb0c,
	0x73990b32, 0x26d7cb1c, 0x6fcc3706, 0x2cbb77d8, 0x75762f2a, 0x6425ccdd,
	0x24b35461, 0x0a7d8715, 0x220414a8, 0x141ebf67, 0x56b41583, 0x73e502e3,
	0x44cab16f, 0x28264d42, 0x73baaefb, 0x0a50ebed, 0x1d6ab6fb, 0x0d3ad40b,
	0x35db3b68, 0x2b081e83, 0x77ce6b95, 0x5181e5f0, 0x78853bbc, 0x009f9494,
	0x27e5ed3c,
}

// InitRandom initializes the random generator with an amplitude in [0..1].
// Matches C VP8InitRandom.
func InitRandom(rg *VP8Random, dithering float32) {
	rg.tab = kRandomTable
	rg.index1 = 0
	rg.index2 = 31
	if dithering < 0.0 {
		rg.amp = 0
	} else if dithering > 1.0 {
		rg.amp = 1 << vp8RandomDitherFix
	} else {
		rg.amp = int(float32(1<<vp8RandomDitherFix) * dithering)
	}
}

// RandomBits2 returns a centered pseudo-random number with numBits amplitude,
// scaled by the given amp. Matches C VP8RandomBits2.
func RandomBits2(rg *VP8Random, numBits, amp int) int {
	diff := int(rg.tab[rg.index1]) - int(rg.tab[rg.index2])
	if diff < 0 {
		diff += 1 << 31
	}
	rg.tab[rg.index1] = uint32(diff)
	rg.index1++
	if rg.index1 == vp8RandomTableSize {
		rg.index1 = 0
	}
	rg.index2++
	if rg.index2 == vp8RandomTableSize {
		rg.index2 = 0
	}
	// sign-extend, 0-center
	diff = int(int32(uint32(diff)<<1)) >> (32 - numBits)
	diff = (diff * amp) >> vp8RandomDitherFix // restrict range
	diff += 1 << (numBits - 1)               // shift back to 0.5-center
	return diff
}

// RandomBits returns a centered pseudo-random number with numBits amplitude,
// using the generator's own amp. Matches C VP8RandomBits.
func RandomBits(rg *VP8Random, numBits int) int {
	return RandomBits2(rg, numBits, rg.amp)
}
