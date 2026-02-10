package dsp

// Image rescaler matching libwebp's rescaler.c / rescaler_utils.c.
// Provides a box-filter based downscaler/upscaler that operates row by row.

// rescalerRFix is the fixed-point precision for rescaler multiplies.
// Matches WEBP_RESCALER_RFIX = 32 in libwebp.
const rescalerRFix = 32

// rescalerOne is (1 << rescalerRFix) as uint64.
const rescalerOne = uint64(1) << rescalerRFix

// Rescaler holds the state for incremental image rescaling.
type Rescaler struct {
	SrcWidth, SrcHeight int // source dimensions
	DstWidth, DstHeight int // destination dimensions

	XExpand bool // true if dst is wider than src (upscaling x)
	YExpand bool // true if dst is taller than src (upscaling y)

	// Accumulators.
	FRow []int32 // horizontal accumulator, length = DstWidth
	IRow []int32 // vertical accumulator, length = DstWidth

	// Counters for vertical stepping.
	YAccum int // current vertical accumulator
	YAdd   int // amount to add per source row (= src_height for shrink)
	YSub   int // amount to subtract per destination row (= dst_height for shrink)

	// Horizontal stepping.
	XAdd int // = src_width
	XSub int // = dst_width

	// Fixed-point scale factors for final output normalisation.
	FXScale  uint32 // horizontal scale (used in shrink import)
	FYScale  uint32 // vertical scale (used in expand export)
	FXYScale uint32 // combined scale (used in shrink export)

	SrcY int // current source row index
	DstY int // current destination row index
}

// multFix computes ((uint64(x) * uint64(y)) + rounder) >> rescalerRFix.
func multFix(x, y uint32) uint32 {
	rounder := uint64(1) << (rescalerRFix - 1)
	return uint32((uint64(x)*uint64(y) + rounder) >> rescalerRFix)
}

// multFixFloor computes (uint64(x) * uint64(y)) >> rescalerRFix (floor).
func multFixFloor(x, y uint32) uint32 {
	return uint32((uint64(x) * uint64(y)) >> rescalerRFix)
}

// rescalerFrac computes ((uint64(x) << rescalerRFix) / y) as uint32.
func rescalerFrac(x, y int) uint32 {
	if y == 0 {
		return 0
	}
	return uint32((uint64(x) << rescalerRFix) / uint64(y))
}

// RescalerInit initialises a Rescaler for the given source and destination sizes.
func RescalerInit(r *Rescaler, srcWidth, srcHeight, dstWidth, dstHeight int) {
	r.SrcWidth = srcWidth
	r.SrcHeight = srcHeight
	r.DstWidth = dstWidth
	r.DstHeight = dstHeight

	r.XExpand = dstWidth > srcWidth
	r.YExpand = dstHeight > srcHeight

	r.FRow = make([]int32, dstWidth)
	r.IRow = make([]int32, dstWidth)

	r.XAdd = srcWidth
	r.XSub = dstWidth
	r.YAdd = srcHeight
	r.YSub = dstHeight

	if r.YExpand {
		r.YAccum = r.YSub
	} else {
		r.YAccum = r.YAdd
	}

	// Fixed-point scale factors.
	if !r.XExpand && r.XSub > 0 {
		r.FXScale = rescalerFrac(1, r.XSub)
	}
	if r.YExpand && r.YSub > 0 {
		r.FYScale = rescalerFrac(1, r.YSub)
	}
	if !r.YExpand && r.XAdd > 0 && r.YAdd > 0 {
		ratio := (uint64(dstHeight) << rescalerRFix) / uint64(r.XAdd*r.YAdd)
		if ratio != uint64(uint32(ratio)) {
			r.FXYScale = 0 // overflow: special-cased in export
		} else {
			r.FXYScale = uint32(ratio)
		}
	}

	r.SrcY = 0
	r.DstY = 0
}

// RescalerImportRow imports one source row, performs horizontal resampling
// into FRow, then accumulates vertically into IRow (for shrink mode).
// src contains srcWidth bytes (one channel).
func RescalerImportRow(r *Rescaler, src []byte) {
	if r.XExpand {
		rescalerImportRowExpand(r, src)
	} else {
		rescalerImportRowShrink(r, src)
	}
	// Vertical accumulation for shrink mode.
	if !r.YExpand {
		for x := 0; x < r.DstWidth; x++ {
			r.IRow[x] += r.FRow[x]
		}
	}
	r.SrcY++
	r.YAccum -= r.YSub
}

// rescalerImportRowExpand handles the case where destination is wider (upscale).
// Matches WebPRescalerImportRowExpand_C from libwebp.
func rescalerImportRowExpand(r *Rescaler, src []byte) {
	xIn := 0
	xOut := 0
	accum := r.XAdd
	left := int32(src[0])
	right := left
	if r.SrcWidth > 1 {
		right = int32(src[1])
	}
	xIn = 1
	for {
		r.FRow[xOut] = right*int32(r.XAdd) + (left-right)*int32(accum)
		xOut++
		if xOut >= r.DstWidth {
			break
		}
		accum -= r.XSub
		if accum < 0 {
			left = right
			xIn++
			if xIn < r.SrcWidth {
				right = int32(src[xIn])
			}
			accum += r.XAdd
		}
	}
}

// rescalerImportRowShrink handles the case where destination is narrower (downscale).
// Matches WebPRescalerImportRowShrink_C from libwebp.
func rescalerImportRowShrink(r *Rescaler, src []byte) {
	xIn := 0
	xOut := 0
	var sum uint32
	accum := 0

	for xOut < r.DstWidth {
		var base uint32
		accum += r.XAdd
		for accum > 0 {
			accum -= r.XSub
			if xIn < r.SrcWidth {
				base = uint32(src[xIn])
			}
			sum += base
			xIn++
		}
		// Emit next horizontal pixel.
		frac := base * uint32(-accum)
		r.FRow[xOut] = int32(sum*uint32(r.XSub) - frac)
		// Fresh fractional start for next pixel.
		sum = multFix(frac, r.FXScale)
		xOut++
	}
}

// RescalerExportRow exports one destination row from the accumulators.
// dst receives dstWidth bytes. Returns true if a row was exported.
func RescalerExportRow(r *Rescaler, dst []byte) bool {
	if r.YAccum > 0 {
		return false
	}

	if r.YExpand {
		rescalerExportRowExpand(r, dst)
	} else {
		rescalerExportRowShrink(r, dst)
	}

	r.YAccum += r.YAdd
	r.DstY++
	return true
}

// rescalerExportRowExpand handles export for upscaling.
// Matches WebPRescalerExportRowExpand_C from libwebp.
func rescalerExportRowExpand(r *Rescaler, dst []byte) {
	if r.YAccum == 0 {
		// First row or exact boundary: output FRow directly.
		for x := 0; x < r.DstWidth; x++ {
			j := uint32(r.FRow[x])
			v := int(multFix(j, r.FYScale))
			if v > 255 {
				v = 255
			}
			dst[x] = uint8(v)
		}
	} else {
		// Interpolate between FRow (current) and IRow (previous).
		b := rescalerFrac(-r.YAccum, r.YSub)
		a := uint32(rescalerOne - uint64(b))
		for x := 0; x < r.DstWidth; x++ {
			i := uint64(a)*uint64(uint32(r.FRow[x])) + uint64(b)*uint64(uint32(r.IRow[x]))
			rounder := uint64(1) << (rescalerRFix - 1)
			j := uint32((i + rounder) >> rescalerRFix)
			v := int(multFix(j, r.FYScale))
			if v > 255 {
				v = 255
			}
			dst[x] = uint8(v)
		}
	}
	// Save current FRow into IRow for next interpolation.
	copy(r.IRow, r.FRow)
}

// rescalerExportRowShrink handles export for downscaling.
// Matches WebPRescalerExportRowShrink_C from libwebp.
func rescalerExportRowShrink(r *Rescaler, dst []byte) {
	yscale := r.FYScale * uint32(-r.YAccum)
	if yscale != 0 {
		for x := 0; x < r.DstWidth; x++ {
			frac := multFixFloor(uint32(r.FRow[x]), yscale)
			v := int(multFix(uint32(r.IRow[x])-frac, r.FXYScale))
			if v > 255 {
				v = 255
			}
			dst[x] = uint8(v)
			r.IRow[x] = int32(frac) // new fractional start
		}
	} else {
		for x := 0; x < r.DstWidth; x++ {
			v := int(multFix(uint32(r.IRow[x]), r.FXYScale))
			if v > 255 {
				v = 255
			}
			dst[x] = uint8(v)
			r.IRow[x] = 0
		}
	}
}

// RescalerHasDstRow returns true if a destination row is ready for export.
func RescalerHasDstRow(r *Rescaler) bool {
	return r.YAccum <= 0
}

// RescalerNeedsSrcRow returns true if another source row should be imported.
func RescalerNeedsSrcRow(r *Rescaler) bool {
	return r.YAccum > 0
}
