package lossless

// Near-lossless image preprocessing for VP8L encoding.
//
// Adjusts pixel values to help compressibility with a guarantee of maximum
// deviation between original and resulting pixel values.
//
// This implementation matches the C reference in
// libwebp/src/enc/near_lossless_enc.c: it quantizes raw pixel values (not
// residuals) via FindClosestDiscretized using banker's rounding, and applies
// multi-pass processing for each quantization level.

const (
	// minDimForNearLossless is the minimum dimension for near-lossless.
	minDimForNearLossless = 64
	// maxLimitBits is the maximum quantization level.
	maxLimitBits = 5
)

// NearLosslessBits returns the quantization level from quality.
// Maps quality ranges to bits:
//
//	100     -> 0
//	80..99  -> 1
//	60..79  -> 2
//	40..59  -> 3
//	20..39  -> 4
//	 0..19  -> 5
//
// Matches C VP8LNearLosslessBits in lossless_common.h.
func NearLosslessBits(nearLosslessQuality int) int {
	return maxLimitBits - nearLosslessQuality/20
}

// findClosestDiscretized quantizes the value up or down to a multiple of
// 1<<bits (or to 255), choosing the closer one, resolving ties using
// banker's rounding.
//
// Matches C FindClosestDiscretized in near_lossless_enc.c:34-40.
func findClosestDiscretized(a uint32, bits uint) uint32 {
	mask := (uint32(1) << bits) - 1
	biased := a + (mask >> 1) + ((a >> bits) & 1)
	if biased > 0xff {
		return 0xff
	}
	return biased & ^mask
}

// closestDiscretizedArgb applies findClosestDiscretized to all four channels
// of an ARGB pixel independently.
//
// Matches C ClosestDiscretizedArgb in near_lossless_enc.c:43-48.
func closestDiscretizedArgb(a uint32, bits uint) uint32 {
	return (findClosestDiscretized(a>>24, bits) << 24) |
		(findClosestDiscretized((a>>16)&0xff, bits) << 16) |
		(findClosestDiscretized((a>>8)&0xff, bits) << 8) |
		findClosestDiscretized(a&0xff, bits)
}

// isNear checks if the distance between corresponding channel values of
// pixels a and b is within the given limit (strictly less than limit).
//
// Matches C IsNear in near_lossless_enc.c:52-62.
func isNear(a, b uint32, limit int) bool {
	for k := 0; k < 4; k++ {
		delta := int((a>>(uint(k)*8))&0xff) - int((b>>(uint(k)*8))&0xff)
		if delta >= limit || delta <= -limit {
			return false
		}
	}
	return true
}

// isSmooth checks that all pixels in the 4-connected neighborhood are
// within the given limit.
//
// Matches C IsSmooth in near_lossless_enc.c:64-72.
func isSmooth(prevRow, currRow, nextRow []uint32, ix, limit int) bool {
	return isNear(currRow[ix], currRow[ix-1], limit) &&
		isNear(currRow[ix], currRow[ix+1], limit) &&
		isNear(currRow[ix], prevRow[ix], limit) &&
		isNear(currRow[ix], nextRow[ix], limit)
}

// nearLosslessPass applies one pass of near-lossless quantization with the
// given limitBits. It reads from src (with the given stride) and writes to
// dst (packed, stride = xsize).
//
// Matches C NearLossless in near_lossless_enc.c:75-109.
func nearLosslessPass(xsize, ysize int, src []uint32, stride int, limitBits uint, dst []uint32) {
	limit := int(1) << limitBits

	// Allocate 3 row buffers for a sliding window: prev, curr, next.
	prevRow := make([]uint32, xsize)
	currRow := make([]uint32, xsize)
	nextRow := make([]uint32, xsize)

	// Initialize curr_row and next_row from the source.
	copy(currRow, src[:xsize])
	if ysize > 1 {
		copy(nextRow, src[stride:stride+xsize])
	}

	srcOff := 0
	dstOff := 0

	for y := 0; y < ysize; y++ {
		if y == 0 || y == ysize-1 {
			// First and last rows: copy exactly from source.
			copy(dst[dstOff:dstOff+xsize], src[srcOff:srcOff+xsize])
		} else {
			// Load next row from source.
			copy(nextRow, src[srcOff+stride:srcOff+stride+xsize])

			// First and last columns: copy exactly.
			dst[dstOff] = src[srcOff]
			dst[dstOff+xsize-1] = src[srcOff+xsize-1]

			// Interior pixels.
			for x := 1; x < xsize-1; x++ {
				if isSmooth(prevRow, currRow, nextRow, x, limit) {
					dst[dstOff+x] = currRow[x]
				} else {
					dst[dstOff+x] = closestDiscretizedArgb(currRow[x], limitBits)
				}
			}
		}

		// Three-way rotation of row buffers.
		prevRow, currRow, nextRow = currRow, nextRow, prevRow

		srcOff += stride
		dstOff += xsize
	}
}

// ApplyNearLossless applies near-lossless preprocessing to the ARGB image.
// width and height are the image dimensions. The bits parameter is accepted
// for API compatibility but is unused (the C reference near-lossless does not
// use tile-based predictor selection). quality controls the quantization level
// (0 = most quantization, 100 = no quantization).
//
// The algorithm quantizes raw pixel values (not residuals) to the nearest
// multiple of 1<<limitBits using banker's rounding, preserving boundary pixels
// and smooth areas. It then applies multi-pass refinement from limitBits-1
// down to 1.
//
// Matches C VP8ApplyNearLossless in near_lossless_enc.c:111-146.
//
// The image is modified in place.
func ApplyNearLossless(argb []uint32, width, height, bits, quality int) {
	limitBits := NearLosslessBits(quality)
	if limitBits <= 0 {
		return
	}
	if limitBits > maxLimitBits {
		limitBits = maxLimitBits
	}

	// For small icon images, don't attempt near-lossless compression.
	if (width < minDimForNearLossless && height < minDimForNearLossless) || height < 3 {
		return
	}

	// The source image has stride = width (argb is a flat row-major buffer).
	stride := width

	// Allocate the destination buffer (packed, stride = width).
	dst := make([]uint32, width*height)

	// First pass with the full limitBits.
	nearLosslessPass(width, height, argb, stride, uint(limitBits), dst)

	// Subsequent passes with decreasing limitBits, operating on dst in-place.
	// Each pass reads from dst (stride = width) and writes back to dst.
	for i := limitBits - 1; i >= 1; i-- {
		nearLosslessPass(width, height, dst, width, uint(i), dst)
	}

	// Copy the result back to the original buffer.
	copy(argb, dst)
}
