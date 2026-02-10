package dsp

// VP8L spatial predictors for lossless WebP decoding.
// Implements all 14 predictors (0-13) from lossless.c, plus sentinels 14-15.
// Predictors operate on ARGB uint32 values.
//
// Convention: the caller passes the top row slice already offset so that:
//   - top[0] = top-left pixel (TL)
//   - top[1] = top pixel (T, directly above current)
//   - top[2] = top-right pixel (TR)
//
// The left pixel is passed by pointer.

// LosslessPredFunc is the signature for VP8L spatial predictors.
type LosslessPredFunc func(left *uint32, top []uint32) uint32

// LosslessPredictors holds all 16 predictor functions (indices 0-15).
var LosslessPredictors [16]LosslessPredFunc

// Multipliers holds the VP8L color-space transform multipliers.
type Multipliers struct {
	GreenToRed  uint8
	GreenToBlue uint8
	RedToBlue   uint8
}

// lAverage2 computes the average of two ARGB pixels per component without
// overflow: ((a ^ b) & 0xfefefefe) >> 1 + (a & b).
func lAverage2(a, b uint32) uint32 {
	return (((a ^ b) & 0xfefefefe) >> 1) + (a & b)
}

// lAverage3 computes average3(a, b, c) = average2(average2(a, c), b).
func lAverage3(a, b, c uint32) uint32 {
	return lAverage2(lAverage2(a, c), b)
}

// lAverage4 computes average4(a, b, c, d) = average2(average2(a, b), average2(c, d)).
func lAverage4(a, b, c, d uint32) uint32 {
	return lAverage2(lAverage2(a, b), lAverage2(c, d))
}

// lAbs returns |v| as int for int32.
func lAbs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// lSelect implements the Select predictor: if gradient favors T, return T; else L.
func lSelect(a, b, c uint32) uint32 {
	// Compare per-component distance |a-c| vs |b-c| to decide between a (top) and b (left).
	paMinusPb := int32(0)
	for shift := uint(0); shift < 32; shift += 8 {
		ac := int32((a>>shift)&0xff) - int32((c>>shift)&0xff)
		bc := int32((b>>shift)&0xff) - int32((c>>shift)&0xff)
		paMinusPb += lAbs(bc) - lAbs(ac)
	}
	if paMinusPb <= 0 {
		return a
	}
	return b
}

// lClamp clamps a per-component value to [0, 255].
func lClamp(a int32) uint8 {
	if a < 0 {
		return 0
	}
	if a > 255 {
		return 255
	}
	return uint8(a)
}

// lClampedAddSubtractFull computes L + T - TL per component, clamped.
func lClampedAddSubtractFull(a, b, c uint32) uint32 {
	var result uint32
	for shift := uint(0); shift < 32; shift += 8 {
		va := int32((a >> shift) & 0xff)
		vb := int32((b >> shift) & 0xff)
		vc := int32((c >> shift) & 0xff)
		result |= uint32(lClamp(va+vb-vc)) << shift
	}
	return result
}

// lClampedAddSubtractHalf computes average2(L, T) + (average2(L, T) - TL) / 2 per component, clamped.
func lClampedAddSubtractHalf(a, b, c uint32) uint32 {
	avg := lAverage2(a, b)
	var result uint32
	for shift := uint(0); shift < 32; shift += 8 {
		va := int32((avg >> shift) & 0xff)
		vc := int32((c >> shift) & 0xff)
		result |= uint32(lClamp(va+(va-vc)/2)) << shift
	}
	return result
}

// Predictor implementations.
// top[0] = TL, top[1] = T, top[2] = TR

// pred0 returns ARGB_BLACK (opaque black).
func pred0(_ *uint32, _ []uint32) uint32 {
	return 0xff000000
}

// pred1 returns L (left pixel).
func pred1(left *uint32, _ []uint32) uint32 {
	return *left
}

// pred2 returns T (top pixel).
func pred2(_ *uint32, top []uint32) uint32 {
	return top[1]
}

// pred3 returns TR (top-right pixel).
func pred3(_ *uint32, top []uint32) uint32 {
	return top[2]
}

// pred4 returns TL (top-left pixel).
func pred4(_ *uint32, top []uint32) uint32 {
	return top[0]
}

// pred5 returns Average3(L, T, TR).
func pred5(left *uint32, top []uint32) uint32 {
	return lAverage3(*left, top[1], top[2])
}

// pred6 returns Average2(L, TL).
func pred6(left *uint32, top []uint32) uint32 {
	return lAverage2(*left, top[0])
}

// pred7 returns Average2(L, T).
func pred7(left *uint32, top []uint32) uint32 {
	return lAverage2(*left, top[1])
}

// pred8 returns Average2(TL, T).
func pred8(_ *uint32, top []uint32) uint32 {
	return lAverage2(top[0], top[1])
}

// pred9 returns Average2(T, TR).
func pred9(_ *uint32, top []uint32) uint32 {
	return lAverage2(top[1], top[2])
}

// pred10 returns Average4(L, TL, T, TR).
func pred10(left *uint32, top []uint32) uint32 {
	return lAverage4(*left, top[0], top[1], top[2])
}

// pred11 returns Select(T, L, TL).
func pred11(left *uint32, top []uint32) uint32 {
	return lSelect(top[1], *left, top[0])
}

// pred12 returns ClampedAddSubtractFull(L, T, TL).
func pred12(left *uint32, top []uint32) uint32 {
	return lClampedAddSubtractFull(*left, top[1], top[0])
}

// pred13 returns ClampedAddSubtractHalf(L, T, TL) using avg(L,T) vs TL.
func pred13(left *uint32, top []uint32) uint32 {
	return lClampedAddSubtractHalf(*left, top[1], top[0])
}

// initLosslessPredictors registers all predictor functions.
func initLosslessPredictors() {
	LosslessPredictors[0] = pred0
	LosslessPredictors[1] = pred1
	LosslessPredictors[2] = pred2
	LosslessPredictors[3] = pred3
	LosslessPredictors[4] = pred4
	LosslessPredictors[5] = pred5
	LosslessPredictors[6] = pred6
	LosslessPredictors[7] = pred7
	LosslessPredictors[8] = pred8
	LosslessPredictors[9] = pred9
	LosslessPredictors[10] = pred10
	LosslessPredictors[11] = pred11
	LosslessPredictors[12] = pred12
	LosslessPredictors[13] = pred13
	LosslessPredictors[14] = pred0 // sentinel
	LosslessPredictors[15] = pred0 // sentinel
}
