// Package dsp provides low-level DSP routines for the WebP codec.
// This file contains pre-computed clip and absolute-value lookup tables used
// by the VP8 loop filter and inverse transforms. Negative-index access is
// emulated through fixed offsets into oversized arrays.
package dsp

// Table sizes accommodate the full range of intermediate values produced by
// the VP8 filter arithmetic and transform butterfly stages.
var (
	sclip1 [893 + 892 + 1]int8 // clips [-893, 892] to [-128, 127]
	sclip2 [112 + 112 + 1]int8    // clips [-112, 112] to [-16, 15]
	clip1  [255 + 511 + 1]uint8   // clips [-255, 511] to [0, 255]
	abs0   [255 + 255 + 1]uint8   // abs(x) for x in [-255, 255]
)

// Offsets for indexing with negative values.
const (
	sclip1Offset = 893
	sclip2Offset = 112
	clip1Offset  = 255
	abs0Offset   = 255
)

// Ksclip1 returns the value of v clipped to [-128, 127].
func Ksclip1(v int) int8 { return sclip1[sclip1Offset+v] }

// Ksclip2 returns the value of v clipped to [-16, 15].
func Ksclip2(v int) int8 { return sclip2[sclip2Offset+v] }

// Kclip1 returns the value of v clipped to [0, 255].
func Kclip1(v int) uint8 { return clip1[clip1Offset+v] }

// Kabs0 returns |v| for v in [-255, 255].
func Kabs0(v int) uint8 { return abs0[abs0Offset+v] }

// Clip8b clips v to the range [0, 255].
// Uses unsigned comparison for single-branch hot path when v is in [0, 255].
func Clip8b(v int) uint8 {
	if uint(v) <= 255 {
		return uint8(v)
	}
	// Out of range: clamp to 0 or 255.
	// Arithmetic right shift: v>>63 is 0 for positive, -1 for negative.
	return uint8(^(v >> 63) & 255)
}

// initClipTables fills all lookup tables at package initialisation.
func initClipTables() {
	// sclip1: clips to [-128, 127], range [-893, 892] matching C
	for i := -893; i <= 892; i++ {
		v := i
		if v < -128 {
			v = -128
		} else if v > 127 {
			v = 127
		}
		sclip1[sclip1Offset+i] = int8(v)
	}

	// sclip2: clips to [-16, 15]
	for i := -112; i <= 112; i++ {
		v := i
		if v < -16 {
			v = -16
		} else if v > 15 {
			v = 15
		}
		sclip2[sclip2Offset+i] = int8(v)
	}

	// clip1: clips to [0, 255]
	for i := -255; i <= 511; i++ {
		v := i
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		clip1[clip1Offset+i] = uint8(v)
	}

	// abs0: absolute value
	for i := -255; i <= 255; i++ {
		v := i
		if v < 0 {
			v = -v
		}
		abs0[abs0Offset+i] = uint8(v)
	}
}
