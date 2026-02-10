package dsp

// Fancy upsampling for YUV -> RGB conversion.
//
// Implements the diamond-shaped 4-tap interpolation kernel used by libwebp's
// FANCY_UPSAMPLING to upsample chroma from 4:2:0 to the luma grid resolution.
//
// Given a 2x2 chroma block [tl t / l cur], the four interpolated sub-pixels are:
//   top-left  = (9*tl + 3*t + 3*l +   cur + 8) / 16
//   top-right = (3*tl + 9*t +   l + 3*cur + 8) / 16
//   bot-left  = (3*tl +   t + 9*l + 3*cur + 8) / 16
//   bot-right = (  tl + 3*t + 3*l + 9*cur + 8) / 16
//
// The implementation processes U and V in parallel using the same packed-uint32
// trick as the C reference: LOAD_UV packs u into the low 16 bits and v into
// the high 16 bits, so a single addition/shift operates on both channels.

// UpsampleFunc is the signature for an upsampler that converts two rows of
// YUV data into two rows of RGB data with chroma interpolation.
type UpsampleFunc func(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	width int,
)

// Upsamplers holds the registered upsampling functions, one per output format.
// Index 0 is RGB, 1 is BGR, etc. The caller sets these up.
var Upsamplers [16]UpsampleFunc

// loadUV packs u and v into a single uint32: u in the low 16 bits, v in the
// high 16 bits. This mirrors the C macro LOAD_UV(u, v).
func loadUV(u, v byte) uint32 {
	return uint32(u) | (uint32(v) << 16)
}

// UpsampleLinePair upsamples a pair of chroma rows (topU/V, botU/V) and
// combines with two luma rows (topY, botY) into packed RGB output (topDst,
// botDst). This is the diamond 4-tap kernel matching the C reference
// UpsampleRgbLinePair_C with XSTEP=3.
//
// botY may be nil when processing the last row of an odd-height image; in
// that case botDst is ignored.
func UpsampleLinePair(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	width int,
) {
	if width <= 0 {
		return
	}
	const xStep = 3

	lastPixelPair := (width - 1) >> 1

	// tl_uv and l_uv track the left-column chroma as we sweep right.
	tlUV := loadUV(topU[0], topV[0])
	lUV := loadUV(botU[0], botV[0])

	// First pixel: only vertical interpolation (no left neighbor).
	{
		uv0 := (3*tlUV + lUV + 0x00020002) >> 2
		YUVToRGB(int(topY[0]), int(uv0&0xff), int((uv0>>16)&0xff), topDst[0:])
	}
	if botY != nil {
		uv0 := (3*lUV + tlUV + 0x00020002) >> 2
		YUVToRGB(int(botY[0]), int(uv0&0xff), int((uv0>>16)&0xff), botDst[0:])
	}

	// Interior pixel pairs: full diamond 4-tap kernel.
	for x := 1; x <= lastPixelPair; x++ {
		tUV := loadUV(topU[x], topV[x])
		uv := loadUV(botU[x], botV[x])

		// The diamond kernel computes two diagonal averages:
		//   avg    = tl + t + l + cur + 8
		//   diag12 = (avg + 2*(t + l)) >> 3   -- biased towards t and l
		//   diag03 = (avg + 2*(tl + cur)) >> 3 -- biased towards tl and cur
		avg := tlUV + tUV + lUV + uv + 0x00080008
		diag12 := (avg + 2*(tUV+lUV)) >> 3
		diag03 := (avg + 2*(tlUV+uv)) >> 3

		// Top row: two output pixels.
		{
			uv0 := (diag12 + tlUV) >> 1
			uv1 := (diag03 + tUV) >> 1
			YUVToRGB(int(topY[2*x-1]), int(uv0&0xff), int((uv0>>16)&0xff),
				topDst[(2*x-1)*xStep:])
			YUVToRGB(int(topY[2*x]), int(uv1&0xff), int((uv1>>16)&0xff),
				topDst[(2*x)*xStep:])
		}
		// Bottom row: two output pixels.
		if botY != nil {
			uv0 := (diag03 + lUV) >> 1
			uv1 := (diag12 + uv) >> 1
			YUVToRGB(int(botY[2*x-1]), int(uv0&0xff), int((uv0>>16)&0xff),
				botDst[(2*x-1)*xStep:])
			YUVToRGB(int(botY[2*x]), int(uv1&0xff), int((uv1>>16)&0xff),
				botDst[(2*x)*xStep:])
		}

		tlUV = tUV
		lUV = uv
	}

	// Last pixel for even widths (no right neighbor).
	if width&1 == 0 {
		{
			uv0 := (3*tlUV + lUV + 0x00020002) >> 2
			YUVToRGB(int(topY[width-1]), int(uv0&0xff), int((uv0>>16)&0xff),
				topDst[(width-1)*xStep:])
		}
		if botY != nil {
			uv0 := (3*lUV + tlUV + 0x00020002) >> 2
			YUVToRGB(int(botY[width-1]), int(uv0&0xff), int((uv0>>16)&0xff),
				botDst[(width-1)*xStep:])
		}
	}
}

// UpsampleLinePairNRGBA is the same diamond 4-tap kernel as UpsampleLinePair
// but writes into NRGBA pixel buffers (4 bytes per pixel: R, G, B, A=255).
// alphaTop/alphaBot may be nil, in which case alpha is set to 255.
func UpsampleLinePairNRGBA(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	alphaTop, alphaBot []byte,
	width int,
) {
	if width <= 0 {
		return
	}
	const xStep = 4

	lastPixelPair := (width - 1) >> 1

	tlUV := loadUV(topU[0], topV[0])
	lUV := loadUV(botU[0], botV[0])

	// First pixel.
	{
		uv0 := (3*tlUV + lUV + 0x00020002) >> 2
		YUVToRGB(int(topY[0]), int(uv0&0xff), int((uv0>>16)&0xff), topDst[0:])
		if alphaTop != nil {
			topDst[3] = alphaTop[0]
		} else {
			topDst[3] = 255
		}
	}
	if botY != nil {
		uv0 := (3*lUV + tlUV + 0x00020002) >> 2
		YUVToRGB(int(botY[0]), int(uv0&0xff), int((uv0>>16)&0xff), botDst[0:])
		if alphaBot != nil {
			botDst[3] = alphaBot[0]
		} else {
			botDst[3] = 255
		}
	}

	// Interior pixel pairs.
	for x := 1; x <= lastPixelPair; x++ {
		tUV := loadUV(topU[x], topV[x])
		uv := loadUV(botU[x], botV[x])

		avg := tlUV + tUV + lUV + uv + 0x00080008
		diag12 := (avg + 2*(tUV+lUV)) >> 3
		diag03 := (avg + 2*(tlUV+uv)) >> 3

		// Top row.
		{
			uv0 := (diag12 + tlUV) >> 1
			uv1 := (diag03 + tUV) >> 1
			off0 := (2*x - 1) * xStep
			off1 := (2 * x) * xStep
			YUVToRGB(int(topY[2*x-1]), int(uv0&0xff), int((uv0>>16)&0xff), topDst[off0:])
			YUVToRGB(int(topY[2*x]), int(uv1&0xff), int((uv1>>16)&0xff), topDst[off1:])
			if alphaTop != nil {
				topDst[off0+3] = alphaTop[2*x-1]
				topDst[off1+3] = alphaTop[2*x]
			} else {
				topDst[off0+3] = 255
				topDst[off1+3] = 255
			}
		}
		// Bottom row.
		if botY != nil {
			uv0 := (diag03 + lUV) >> 1
			uv1 := (diag12 + uv) >> 1
			off0 := (2*x - 1) * xStep
			off1 := (2 * x) * xStep
			YUVToRGB(int(botY[2*x-1]), int(uv0&0xff), int((uv0>>16)&0xff), botDst[off0:])
			YUVToRGB(int(botY[2*x]), int(uv1&0xff), int((uv1>>16)&0xff), botDst[off1:])
			if alphaBot != nil {
				botDst[off0+3] = alphaBot[2*x-1]
				botDst[off1+3] = alphaBot[2*x]
			} else {
				botDst[off0+3] = 255
				botDst[off1+3] = 255
			}
		}

		tlUV = tUV
		lUV = uv
	}

	// Last pixel for even widths.
	if width&1 == 0 {
		off := (width - 1) * xStep
		{
			uv0 := (3*tlUV + lUV + 0x00020002) >> 2
			YUVToRGB(int(topY[width-1]), int(uv0&0xff), int((uv0>>16)&0xff), topDst[off:])
			if alphaTop != nil {
				topDst[off+3] = alphaTop[width-1]
			} else {
				topDst[off+3] = 255
			}
		}
		if botY != nil {
			uv0 := (3*lUV + tlUV + 0x00020002) >> 2
			YUVToRGB(int(botY[width-1]), int(uv0&0xff), int((uv0>>16)&0xff), botDst[off:])
			if alphaBot != nil {
				botDst[off+3] = alphaBot[width-1]
			} else {
				botDst[off+3] = 255
			}
		}
	}
}

// PointSampleRow performs nearest-neighbor (point) upsampling of a single
// row of YUV 4:2:0 data to RGB. Each chroma sample covers two luma pixels.
func PointSampleRow(y, u, v []byte, dst []byte, width int) {
	for x := 0; x < width; x++ {
		cx := x >> 1
		YUVToRGB(int(y[x]), int(u[cx]), int(v[cx]), dst[x*3:])
	}
}
