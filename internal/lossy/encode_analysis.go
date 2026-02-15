package lossy

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/deepteams/webp/internal/dsp"
)

// analysisWorker holds per-worker buffers for parallel analysis.
type analysisWorker struct {
	tmpCoeffs [16]int16
	src       [512]byte // 16*BPS luma source
	pred      [512]byte // 16*BPS luma prediction
	srcU      [256]byte // 8*BPS chroma U source
	srcV      [256]byte // 8*BPS chroma V source
	predU     [256]byte // 8*BPS chroma U prediction
	predV     [256]byte // 8*BPS chroma V prediction
}

// analysis performs the pre-encoding analysis pass:
// - Computes per-macroblock complexity/alpha
// - Assigns segments via k-means clustering
// - Sets up segment quantizers and filter strengths.
//
// Matches C libwebp's VP8EncAnalyze -> VP8SetSegmentParams flow.
func (enc *VP8Encoder) analysis() {
	numSegs := enc.config.Segments
	if numSegs < 1 {
		numSegs = 1
	}
	if numSegs > NumMBSegments {
		numSegs = NumMBSegments
	}

	// Compute alpha (complexity) for each macroblock.
	// Use pre-allocated buffer from enc.analysisAlphas.
	alphas := enc.analysisAlphas[:len(enc.mbInfo)]
	for i := range alphas {
		alphas[i] = 0
	}
	globalUVAlpha := computeAlphas(enc, alphas)

	// Store global alpha and UV alpha (matching C enc->alpha, enc->uv_alpha).
	enc.globalAlpha = 0
	enc.globalUVAlpha = globalUVAlpha

	if numSegs <= 1 {
		// Single segment: every MB gets segment 0.
		// Matching C libwebp's ResetAllMBInfo path.
		for i := range enc.mbInfo {
			enc.mbInfo[i].Segment = 0
		}
		enc.dqm[0].Alpha = 0
		enc.dqm[0].Beta = 0
	} else {
		// K-means clustering of alphas into segments.
		assignSegments(enc, alphas, numSegs)
	}

	// Unified segment parameter setup (matching C VP8SetSegmentParams).
	// This handles: per-segment Q modulation via SNS alpha, UV deltas,
	// filter strength, segment simplification, and matrix setup.
	enc.setSegmentParams(numSegs)

	// Rebuild segment headers for the bitstream (using effective numSegments
	// which may have been reduced by simplifySegments).
	enc.buildSegmentHeader(enc.numSegments)
}

// smoothSegmentMap applies a 3x3 majority-vote filter to the segment map,
// reducing noise in segment assignment. This matches libwebp's SmoothSegmentMap
// from analysis_enc.c.
func smoothSegmentMap(enc *VP8Encoder) {
	w, h := enc.mbW, enc.mbH
	if w < 3 || h < 3 {
		return
	}
	tmp := enc.segMapTmp[:w*h]
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			tmp[y*w+x] = enc.mbInfo[y*w+x].Segment
		}
	}
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			var cnt [NumMBSegments]int
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					cnt[enc.mbInfo[(y+dy)*w+(x+dx)].Segment]++
				}
			}
			best := tmp[y*w+x]
			for s := 0; s < NumMBSegments; s++ {
				if cnt[s] >= 5 {
					best = uint8(s)
				}
			}
			tmp[y*w+x] = best
		}
	}
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			enc.mbInfo[y*w+x].Segment = tmp[y*w+x]
		}
	}
}

// setSegmentParams computes per-segment quantizers and filter strengths,
// matching C libwebp's VP8SetSegmentParams exactly.
//
// Flow:
// 1. Compute c_base from quality (QualityToCompression)
// 2. For each segment: modulate Q via power-law using segment alpha and SNS amp
// 3. Set base_quant from segment 0, fill unused segments
// 4. Compute UV AC/DC deltas from global UV alpha and SNS strength
// 5. Set up filter strengths
// 6. Simplify equivalent segments (multi-segment only)
// 7. Set up quantization matrices (setupSegment)
func (enc *VP8Encoder) setSegmentParams(numSegs int) {
	snsStr := enc.config.SNSStrength
	if snsStr < 0 {
		snsStr = 0
	}

	// SNS_TO_DQ = 0.9, amp = SNS_TO_DQ * sns_strength / 100.0 / 128.0
	amp := 0.9 * float64(snsStr) / 100.0 / 128.0

	// c_base = QualityToCompression(Q) — matching C libwebp.
	cBase := qualityToCompression(enc.config.Quality)

	// Compute per-segment quantizer via power-law modulation.
	for i := 0; i < numSegs; i++ {
		// expn = 1.0 - amp * alpha
		// When amp=0 (no SNS), expn=1 for all segments, so c=c_base, all get same Q.
		expn := 1.0 - amp*float64(enc.dqm[i].Alpha)
		c := math.Pow(cBase, expn)
		q := int(127.0 * (1.0 - c))
		enc.dqm[i].Quant = clampInt(q, 0, 127)
	}

	// Purely indicative in the bitstream (except for the 1-segment case).
	enc.baseQuant = enc.dqm[0].Quant

	// Fill unused segments with base_quant (required by the syntax).
	for i := numSegs; i < NumMBSegments; i++ {
		enc.dqm[i].Quant = enc.baseQuant
	}

	// UV delta: uv_alpha is normally spread around ~60.
	// Map to the safe maximal range of MAX_DQ_UV / MIN_DQ_UV.
	// C: dq_uv_ac = (enc->uv_alpha - MID_ALPHA) * (MAX_DQ_UV - MIN_DQ_UV) / (MAX_ALPHA - MIN_ALPHA)
	const (
		midAlpha = 64  // neutral value for susceptibility
		minAlpha = 30  // lowest usable value
		maxAlpha2 = 100 // higher meaningful value (renamed to avoid conflict with maxAlpha const)
		maxDQUV  = 6
		minDQUV  = -4
	)
	dqUVAC := (enc.globalUVAlpha - midAlpha) * (maxDQUV - minDQUV) / (maxAlpha2 - minAlpha)
	// Rescale by user-defined strength of adaptation.
	dqUVAC = dqUVAC * snsStr / 100
	enc.dqUVAC = clampInt(dqUVAC, minDQUV, maxDQUV)

	// Boost UV DC quality based on SNS strength.
	// C: dq_uv_dc = -4 * enc->config->sns_strength / 100
	dqUVDC := -4 * snsStr / 100
	enc.dqUVDC = clampInt(dqUVDC, -15, 15)

	// Y deltas (always 0 in C libwebp, included for structural parity).
	enc.dqY1DC = 0
	enc.dqY2DC = 0
	enc.dqY2AC = 0

	// Set up filter strengths (matching C SetupFilterStrength).
	enc.setupFilterStrength()

	// Simplify segments: merge equivalent segments (matching C SimplifySegments).
	if numSegs > 1 {
		numSegs = enc.simplifySegments(numSegs)
	}

	// Store the effective number of segments for buildSegmentHeader and other uses.
	enc.numSegments = numSegs

	// Set up quantization matrices for all segments (matching C SetupMatrices).
	for i := 0; i < NumMBSegments; i++ {
		setupSegment(enc, i, enc.dqm[i].Quant)
	}
}

// simplifySegments merges segments that have identical quantizer and filter
// strength, matching C libwebp's SimplifySegments in quant_enc.c.
// Returns the new (possibly reduced) number of segments.
func (enc *VP8Encoder) simplifySegments(numSegs int) int {
	if numSegs > NumMBSegments {
		numSegs = NumMBSegments
	}

	segMap := [NumMBSegments]int{0, 1, 2, 3}
	numFinal := 1

	for s1 := 1; s1 < numSegs; s1++ {
		found := false
		for s2 := 0; s2 < numFinal; s2++ {
			if enc.dqm[s1].Quant == enc.dqm[s2].Quant &&
				enc.dqm[s1].FStrength == enc.dqm[s2].FStrength {
				segMap[s1] = s2
				found = true
				break
			}
		}
		if !found {
			segMap[s1] = numFinal
			if numFinal != s1 {
				enc.dqm[numFinal] = enc.dqm[s1]
			}
			numFinal++
		}
	}

	if numFinal < numSegs {
		// Remap macroblock segment assignments.
		for i := range enc.mbInfo {
			enc.mbInfo[i].Segment = uint8(segMap[enc.mbInfo[i].Segment])
		}

		// Replicate trailing segment infos (cosmetics).
		for i := numFinal; i < numSegs; i++ {
			enc.dqm[i] = enc.dqm[numFinal-1]
		}
	}

	return numFinal
}

// computeAlphas estimates the complexity of each macroblock using DCT-based
// histogram analysis matching libwebp's MBAnalyzeBestIntra16Mode.
// Higher alpha = more complex (more high-frequency energy in the residual).
// Returns the global UV alpha average (0-255) for adaptive UV quantization.
// For segment assignment, luma and UV alphas are mixed per libwebp:
//   mixed = (3*lumaAlpha + uvAlpha + 2) >> 2
func computeAlphas(enc *VP8Encoder, alphas []int) int {
	total := enc.mbH * enc.mbW
	if total == 0 {
		return 0
	}

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > total {
		numWorkers = total
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Single-threaded fast path.
	if numWorkers == 1 {
		return computeAlphasSerial(enc, alphas)
	}

	// Parallel: each worker processes a range of MB rows.
	var uvAlphaSum int64
	var wg sync.WaitGroup

	// Distribute rows across workers.
	rowsPerWorker := (enc.mbH + numWorkers - 1) / numWorkers

	for wi := 0; wi < numWorkers; wi++ {
		startY := wi * rowsPerWorker
		endY := startY + rowsPerWorker
		if endY > enc.mbH {
			endY = enc.mbH
		}
		if startY >= endY {
			break
		}
		wg.Add(1)
		go func(startY, endY int) {
			defer wg.Done()
			var w analysisWorker
			localUVSum := 0
			for mbY := startY; mbY < endY; mbY++ {
				for mbX := 0; mbX < enc.mbW; mbX++ {
					idx := mbY*enc.mbW + mbX
					lumaAlpha := computeMBAlphaDCTWorker(enc, &w, mbX, mbY)
					uvAlpha := computeMBUVAlphaDCTWorker(enc, &w, mbX, mbY)
					mixed := (3*lumaAlpha + uvAlpha + 2) >> 2
					mixed = maxAlpha - mixed
					if mixed < 0 {
						mixed = 0
					}
					if mixed > maxAlpha {
						mixed = maxAlpha
					}
					alphas[idx] = mixed
					enc.mbInfo[idx].Alpha = mixed
					localUVSum += uvAlpha
				}
			}
			atomic.AddInt64(&uvAlphaSum, int64(localUVSum))
		}(startY, endY)
	}
	wg.Wait()

	return int(uvAlphaSum) / total
}

// computeAlphasSerial is the single-threaded path for small images.
func computeAlphasSerial(enc *VP8Encoder, alphas []int) int {
	src := enc.tmpAnSrc[:]
	pred := enc.tmpAnPred[:]

	uvAlphaSum := 0
	total := enc.mbH * enc.mbW

	for mbY := 0; mbY < enc.mbH; mbY++ {
		for mbX := 0; mbX < enc.mbW; mbX++ {
			idx := mbY*enc.mbW + mbX
			lumaAlpha := computeMBAlphaDCT(enc, mbX, mbY, src, pred)
			uvAlpha := computeMBUVAlphaDCT(enc, mbX, mbY)
			mixed := (3*lumaAlpha + uvAlpha + 2) >> 2
			mixed = maxAlpha - mixed
			if mixed < 0 {
				mixed = 0
			}
			if mixed > maxAlpha {
				mixed = maxAlpha
			}
			alphas[idx] = mixed
			enc.mbInfo[idx].Alpha = mixed
			uvAlphaSum += uvAlpha
		}
	}

	if total > 0 {
		return uvAlphaSum / total
	}
	return 0
}

const (
	maxCoeffThresh = 31
	alphaScale     = 2 * 255 // ALPHA_SCALE = 2 * MAX_ALPHA
	maxAlpha       = 255

	// Flatness detection thresholds, matching C libwebp quant_enc.c:41-44.
	flatnessLimitI16 = 0   // I16 mode (special case: any non-zero AC means not flat)
	flatnessLimitI4  = 3   // I4 mode
	flatnessLimitUV  = 2   // UV mode
	flatnessPenalty  = 140  // roughly ~1 bit penalty per block
)

// isFlatSource16 checks if a 16x16 block (BPS-strided) has all identical
// pixel values, matching C libwebp's IsFlatSource16 (quant.h:78-89).
func isFlatSource16(src []byte, off int) bool {
	v := src[off]
	for j := 0; j < 16; j++ {
		row := off + j*dsp.BPS
		for i := 0; i < 16; i++ {
			if src[row+i] != v {
				return false
			}
		}
	}
	return true
}

// isFlat counts non-zero AC coefficients across numBlocks blocks of 16
// coefficients each (skipping DC at index 0). Returns true if count <= thresh.
// Matches C libwebp's IsFlat (quant.h:61-73).
func isFlat(levels []int16, numBlocks, thresh int) bool {
	_ = levels[numBlocks*16-1] // BCE hint
	score := 0
	for b := 0; b < numBlocks; b++ {
		base := b * 16
		for i := 1; i < 16; i++ { // skip DC (i=0)
			if levels[base+i] != 0 {
				score++
				if score > thresh {
					return false
				}
			}
		}
	}
	return true
}

// maxIntra16Mode is the number of I16 modes tested during analysis.
// Matching C libwebp's MAX_INTRA16_MODE = 2 (DC + TM).
const maxIntra16Mode = 2

// computeMBAlphaDCTWorker is the parallel version using per-worker buffers.
func computeMBAlphaDCTWorker(enc *VP8Encoder, w *analysisWorker, mbX, mbY int) int {
	return computeMBAlphaDCTWith(enc, mbX, mbY, w.src[:], w.pred[:], &w.tmpCoeffs)
}

// computeMBAlphaDCT computes macroblock complexity using DCT-based histogram
// analysis, matching libwebp's MBAnalyzeBestIntra16Mode (analysis_enc.c:236-258).
// Tests DC and TM predictions, picks the mode with the lowest alpha (simplest).
func computeMBAlphaDCT(enc *VP8Encoder, mbX, mbY int, src, pred []byte) int {
	return computeMBAlphaDCTWith(enc, mbX, mbY, src, pred, &enc.tmpCoeffs)
}

func computeMBAlphaDCTWith(enc *VP8Encoder, mbX, mbY int, src, pred []byte, tmpCoeffs *[16]int16) int {
	x0 := mbX * 16
	y0 := mbY * 16

	// Copy 16x16 source block into BPS-strided buffer with edge replication.
	for j := 0; j < 16; j++ {
		sy := y0 + j
		if sy >= enc.height {
			sy = enc.height - 1
		}
		for i := 0; i < 16; i++ {
			sx := x0 + i
			if sx >= enc.width {
				sx = enc.width - 1
			}
			src[j*dsp.BPS+i] = enc.yPlane[sy*enc.yStride+sx]
		}
	}

	bestAlpha := maxAlpha + 1 // DEFAULT_ALPHA: worse than any valid alpha

	// Test DC and TM modes (MAX_INTRA16_MODE = 2).
	for mode := 0; mode < maxIntra16Mode; mode++ {
		// Skip TM if we don't have both top and left context.
		if mode == int(TMPred) && (mbX == 0 || mbY == 0) {
			continue
		}

		// Generate prediction for this mode.
		generateI16Prediction(enc, mbX, mbY, mode, src, pred)

		// Collect DCT histogram from all 16 4x4 sub-blocks.
		alpha := collectHistogramAlphaWith(src, pred, tmpCoeffs)

		// IS_BETTER_ALPHA: lower alpha = simpler = better prediction.
		if alpha < bestAlpha {
			bestAlpha = alpha
		}
	}

	if bestAlpha > maxAlpha {
		bestAlpha = maxAlpha
	}
	return bestAlpha
}

// generateI16Prediction generates a 16x16 intra prediction into pred.
// Mode 0=DC, 1=TM. For analysis only (no top/left context from reconstruction).
func generateI16Prediction(enc *VP8Encoder, mbX, mbY, mode int, src, pred []byte) {
	x0 := mbX * 16
	y0 := mbY * 16

	switch mode {
	case int(DCPred):
		// DC prediction: average of top and left pixels.
		dcVal := 128
		sum, count := 0, 0
		if mbY > 0 {
			topY := y0 - 1
			for i := 0; i < 16; i++ {
				sx := x0 + i
				if sx >= enc.width {
					sx = enc.width - 1
				}
				sum += int(enc.yPlane[topY*enc.yStride+sx])
				count++
			}
		}
		if mbX > 0 {
			leftX := x0 - 1
			for j := 0; j < 16; j++ {
				sy := y0 + j
				if sy >= enc.height {
					sy = enc.height - 1
				}
				sum += int(enc.yPlane[sy*enc.yStride+leftX])
				count++
			}
		}
		if count > 0 {
			dcVal = (sum + count/2) / count
		}
		for j := 0; j < 16; j++ {
			for i := 0; i < 16; i++ {
				pred[j*dsp.BPS+i] = uint8(dcVal)
			}
		}

	case int(TMPred):
		// TM prediction: top[i] + left[j] - topLeft.
		var top [16]int
		topLeft := 128
		var left [16]int

		if mbY > 0 {
			topY := y0 - 1
			for i := 0; i < 16; i++ {
				sx := x0 + i
				if sx >= enc.width {
					sx = enc.width - 1
				}
				top[i] = int(enc.yPlane[topY*enc.yStride+sx])
			}
			if mbX > 0 {
				topLeft = int(enc.yPlane[topY*enc.yStride+x0-1])
			} else {
				topLeft = top[0]
			}
		} else {
			for i := 0; i < 16; i++ {
				top[i] = 128
			}
		}

		if mbX > 0 {
			leftX := x0 - 1
			for j := 0; j < 16; j++ {
				sy := y0 + j
				if sy >= enc.height {
					sy = enc.height - 1
				}
				left[j] = int(enc.yPlane[sy*enc.yStride+leftX])
			}
		} else {
			for j := 0; j < 16; j++ {
				left[j] = 128
			}
		}

		for j := 0; j < 16; j++ {
			for i := 0; i < 16; i++ {
				v := top[i] + left[j] - topLeft
				if v < 0 {
					v = 0
				} else if v > 255 {
					v = 255
				}
				pred[j*dsp.BPS+i] = uint8(v)
			}
		}
	}
}

// collectHistogramAlpha computes alpha from a DCT histogram of the residual
// between src and pred (both BPS-strided 16x16 blocks).
// Matching C libwebp's VP8CollectHistogram + GetAlpha.
// Uses enc.tmpCoeffs to avoid heap escapes in the inner loop.
func (enc *VP8Encoder) collectHistogramAlpha(src, pred []byte) int {
	return collectHistogramAlphaWith(src, pred, &enc.tmpCoeffs)
}

// collectHistogramAlphaWith uses an external coeffs buffer (thread-safe).
func collectHistogramAlphaWith(src, pred []byte, tmpCoeffs *[16]int16) int {
	var distribution [maxCoeffThresh + 1]int
	for by := 0; by < 4; by++ {
		for bx := 0; bx < 4; bx++ {
			off := by*4*dsp.BPS + bx*4
			dsp.FTransformDirect(src[off:], pred[off:], tmpCoeffs[:])

			for k := 0; k < 16; k++ {
				v := int(tmpCoeffs[k])
				if v < 0 {
					v = -v
				}
				v >>= 3
				if v > maxCoeffThresh {
					v = maxCoeffThresh
				}
				distribution[v]++
			}
		}
	}

	maxValue := 0
	lastNonZero := 1
	for k := 0; k <= maxCoeffThresh; k++ {
		if distribution[k] > 0 {
			if distribution[k] > maxValue {
				maxValue = distribution[k]
			}
			lastNonZero = k
		}
	}

	alpha := 0
	if maxValue > 1 {
		alpha = alphaScale * lastNonZero / maxValue
	}
	if alpha > maxAlpha {
		alpha = maxAlpha
	}
	return alpha
}

// computeMBUVAlphaDCTWorker is the parallel version using per-worker buffers.
func computeMBUVAlphaDCTWorker(enc *VP8Encoder, w *analysisWorker, mbX, mbY int) int {
	return computeMBUVAlphaDCTWith(enc, mbX, mbY, w.srcU[:], w.srcV[:], w.predU[:], w.predV[:], &w.tmpCoeffs)
}

// computeMBUVAlphaDCT computes UV complexity for a macroblock using DCT-based
// histogram analysis on chroma planes, matching libwebp's VP8CollectHistogram
// for UV. Operates at half resolution (8x8 per MB), analyzing both U and V planes.
func computeMBUVAlphaDCT(enc *VP8Encoder, mbX, mbY int) int {
	return computeMBUVAlphaDCTWith(enc, mbX, mbY, enc.tmpAnSrcU[:], enc.tmpAnSrcV[:], enc.tmpAnPredU[:], enc.tmpAnPredV[:], &enc.tmpCoeffs)
}

func computeMBUVAlphaDCTWith(enc *VP8Encoder, mbX, mbY int, srcU, srcV, predU, predV []byte, tmpCoeffs *[16]int16) int {
	ux0 := mbX * 8
	uy0 := mbY * 8
	// Copy 8x8 source U and V blocks into BPS-strided buffers.
	for j := 0; j < 8; j++ {
		sy := uy0 + j
		if sy >= enc.mbH*8 {
			sy = enc.mbH*8 - 1
		}
		for i := 0; i < 8; i++ {
			sx := ux0 + i
			if sx >= enc.mbW*8 {
				sx = enc.mbW*8 - 1
			}
			srcU[j*dsp.BPS+i] = enc.uPlane[sy*enc.uvStride+sx]
			srcV[j*dsp.BPS+i] = enc.vPlane[sy*enc.uvStride+sx]
		}
	}

	// DC prediction: average of top/left pixels for U and V.
	dcU, dcV := 128, 128
	sumU, sumV := 0, 0
	count := 0

	if mbY > 0 {
		topY := uy0 - 1
		for i := 0; i < 8; i++ {
			sx := ux0 + i
			if sx < enc.mbW*8 {
				sumU += int(enc.uPlane[topY*enc.uvStride+sx])
				sumV += int(enc.vPlane[topY*enc.uvStride+sx])
				count++
			}
		}
	}
	if mbX > 0 {
		leftX := ux0 - 1
		for j := 0; j < 8; j++ {
			sy := uy0 + j
			if sy < enc.mbH*8 {
				sumU += int(enc.uPlane[sy*enc.uvStride+leftX])
				sumV += int(enc.vPlane[sy*enc.uvStride+leftX])
				count++
			}
		}
	}
	if count > 0 {
		dcU = (sumU + count/2) / count
		dcV = (sumV + count/2) / count
	}

	// Fill prediction buffers with DC value.
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			predU[j*dsp.BPS+i] = uint8(dcU)
			predV[j*dsp.BPS+i] = uint8(dcV)
		}
	}

	// Collect DCT histogram from 4 sub-blocks (2x2 of 4x4) for U and V.
	var distribution [maxCoeffThresh + 1]int
	for by := 0; by < 2; by++ {
		for bx := 0; bx < 2; bx++ {
			off := by*4*dsp.BPS + bx*4

			// U block
			dsp.FTransformDirect(srcU[off:], predU[off:], tmpCoeffs[:])
			for k := 0; k < 16; k++ {
				v := int(tmpCoeffs[k])
				if v < 0 {
					v = -v
				}
				v >>= 3
				if v > maxCoeffThresh {
					v = maxCoeffThresh
				}
				distribution[v]++
			}

			// V block
			dsp.FTransformDirect(srcV[off:], predV[off:], tmpCoeffs[:])
			for k := 0; k < 16; k++ {
				v := int(tmpCoeffs[k])
				if v < 0 {
					v = -v
				}
				v >>= 3
				if v > maxCoeffThresh {
					v = maxCoeffThresh
				}
				distribution[v]++
			}
		}
	}

	// Compute alpha from histogram (same GetAlpha logic as luma).
	maxValue := 0
	lastNonZero := 1
	for k := 0; k <= maxCoeffThresh; k++ {
		if distribution[k] > 0 {
			if distribution[k] > maxValue {
				maxValue = distribution[k]
			}
			lastNonZero = k
		}
	}

	alpha := 0
	if maxValue > 1 {
		alpha = alphaScale * lastNonZero / maxValue
	}
	if alpha > maxAlpha {
		alpha = maxAlpha
	}
	return alpha
}

// maxItersKMeans is the maximum number of k-means iterations.
const maxItersKMeans = 6

// assignSegments clusters the macroblock alphas into numSegs segments using
// histogram-based k-means, matching C libwebp's AssignSegments (analysis_enc.c:135-222).
// The key difference from per-MB k-means is iterating on histogram bins rather
// than individual MBs, which is both faster and matches the C behavior exactly.
func assignSegments(enc *VP8Encoder, alphas []int, numSegs int) {
	total := len(alphas)
	if total == 0 {
		return
	}

	// Build histogram: histo[a] = count of MBs with alpha value a.
	var histo [maxAlpha + 1]int
	for _, a := range alphas {
		histo[a]++
	}

	// Bracket the input: find min and max alpha values with non-zero counts.
	minA := 0
	for minA <= maxAlpha && histo[minA] == 0 {
		minA++
	}
	maxA := maxAlpha
	for maxA > minA && histo[maxA] == 0 {
		maxA--
	}
	rangeA := maxA - minA

	// Spread initial centers evenly in [minA, maxA].
	// Use fixed-size arrays to avoid heap allocations (numSegs <= NumMBSegments = 4).
	var centers [NumMBSegments]int
	for k := 0; k < numSegs; k++ {
		centers[k] = minA + ((2*k + 1) * rangeA) / (2 * numSegs)
	}

	// K-means iterations on histogram (matching C: MAX_ITERS_K_MEANS = 6).
	var alphaMap [maxAlpha + 1]int // maps alpha value -> segment index
	weightedAvg := 0
	var accum, distAccum [NumMBSegments]int
	for iter := 0; iter < maxItersKMeans; iter++ {
		// Accumulate contributions per segment.
		accum = [NumMBSegments]int{}
		distAccum = [NumMBSegments]int{}

		// Assign nearest center for each alpha value.
		n := 0 // track nearest center
		for a := minA; a <= maxA; a++ {
			if histo[a] == 0 {
				continue
			}
			// Advance to nearest center (C uses while loop scanning forward).
			for n+1 < numSegs && abs(a-centers[n+1]) < abs(a-centers[n]) {
				n++
			}
			alphaMap[a] = n
			distAccum[n] += a * histo[a]
			accum[n] += histo[a]
		}

		// Move centroids to center of their respective cloud.
		displaced := 0
		weightedAvg = 0
		totalWeight := 0
		for s := 0; s < numSegs; s++ {
			if accum[s] > 0 {
				newCenter := (distAccum[s] + accum[s]/2) / accum[s]
				displaced += abs(centers[s] - newCenter)
				centers[s] = newCenter
				weightedAvg += newCenter * accum[s]
				totalWeight += accum[s]
			}
		}
		if totalWeight > 0 {
			weightedAvg = (weightedAvg + totalWeight/2) / totalWeight
		}
		if displaced < 5 {
			break
		}
	}

	// Map each MB to its segment using the alpha -> segment map.
	for i := range enc.mbInfo {
		a := enc.mbInfo[i].Alpha
		enc.mbInfo[i].Segment = uint8(alphaMap[a])
		// Update alpha to center value (matching C: mb->alpha = centers[map[alpha]]).
		enc.mbInfo[i].Alpha = centers[alphaMap[a]]
	}

	// Apply smooth segment map if preprocessing bit 0 is set.
	if enc.config.Segments > 1 && enc.config.Preprocessing&1 != 0 {
		smoothSegmentMap(enc)
	}

	// Set segment alphas (matching C SetSegmentAlphas).
	minC, maxC := centers[0], centers[0]
	for s := 1; s < numSegs; s++ {
		if centers[s] < minC {
			minC = centers[s]
		}
		if centers[s] > maxC {
			maxC = centers[s]
		}
	}
	rangeC := maxC - minC
	if rangeC == 0 {
		rangeC = 1
	}

	for s := 0; s < numSegs; s++ {
		alpha := 255 * (centers[s] - weightedAvg) / rangeC
		alpha = clampInt(alpha, -127, 127)
		enc.dqm[s].Alpha = alpha

		beta := 255 * (centers[s] - minC) / rangeC
		beta = clampInt(beta, 0, 255)
		enc.dqm[s].Beta = beta
	}
}

// buildSegmentHeader sets up the SegmentHeader from the current segment config.
func (enc *VP8Encoder) buildSegmentHeader(numSegs int) {
	hdr := &enc.segmentHdr
	hdr.UseSegment = numSegs > 1
	hdr.UpdateMap = hdr.UseSegment
	hdr.AbsoluteDelta = true

	if hdr.UseSegment {
		for i := 0; i < numSegs; i++ {
			hdr.Quantizer[i] = int8(clampInt(enc.dqm[i].Quant, -127, 127))
			// Per-segment filter strength delta (use qstep, not raw q).
			qstep0 := int(KAcTable[clampInt(enc.dqm[0].Quant, 0, 127)]) >> 2
			qstepI := int(KAcTable[clampInt(enc.dqm[i].Quant, 0, 127)]) >> 2
			fDelta := (qstepI - qstep0) * enc.config.FilterStrength / 100
			hdr.FilterStrength[i] = int8(clampInt(fDelta, -63, 63))
		}
	}
}

// setSegmentProbas computes the segment tree probabilities from the actual
// macroblock distribution. This mirrors libwebp's SetSegmentProbas (frame_enc.c).
// Without this, proba.Segments stays at the ResetProba default of 255, causing
// the boolean coder to desynchronize when encoding/decoding non-zero segment IDs.
func (enc *VP8Encoder) setSegmentProbas() {
	var counts [NumMBSegments]int
	for _, info := range enc.mbInfo {
		counts[info.Segment]++
	}

	getProba := func(a, b int) uint8 {
		total := a + b
		if total == 0 {
			return 255
		}
		return uint8((255*a + total/2) / total)
	}

	enc.proba.Segments[0] = getProba(counts[0]+counts[1], counts[2]+counts[3])
	enc.proba.Segments[1] = getProba(counts[0], counts[1])
	enc.proba.Segments[2] = getProba(counts[2], counts[3])

	// If all probabilities are 255, all MBs are in one partition of the tree.
	// Disable the segment map to avoid unnecessary overhead.
	if enc.proba.Segments[0] == 255 &&
		enc.proba.Segments[1] == 255 &&
		enc.proba.Segments[2] == 255 {
		enc.segmentHdr.UpdateMap = false
		// Reset all segments to 0 since the map won't be written.
		for i := range enc.mbInfo {
			enc.mbInfo[i].Segment = 0
		}
	}
}

// --- Macroblock prediction mode selection ---

// PickBestI16Mode evaluates all 16x16 intra prediction modes and returns
// the one with the best rate-distortion score.
// srcBuf/srcOff and predBuf/predOff point to the Y data in BPS-strided buffers.
// mbX, mbY are the macroblock coordinates (for boundary mode selection).
func PickBestI16Mode(srcBuf []byte, srcOff int, predBuf []byte, predOff int, seg *SegmentInfo, mbX, mbY int) (bestMode uint8, bestScore uint64) {
	bestScore = ^uint64(0)
	bestMode = DCPred

	for mode := 0; mode < NumPredModes; mode++ {
		// Apply boundary check for DC mode to avoid accessing
		// out-of-bounds top/left context.
		actualMode := checkMode(mbX, mbY, mode)

		// Skip modes that require unavailable context.
		if mode == int(VPred) && mbY == 0 {
			continue // vertical prediction needs top row
		}
		if mode == int(HPred) && mbX == 0 {
			continue // horizontal prediction needs left column
		}
		if mode == int(TMPred) && (mbX == 0 || mbY == 0) {
			continue // TM needs both top and left
		}

		// Generate prediction.
		dsp.PredLuma16Direct(actualMode, predBuf, predOff)

		// Compute distortion.
		disto := dsp.SSE16x16Direct(srcBuf[srcOff:], predBuf[predOff:])

		// Compute approximate rate (simplified: fixed cost per mode).
		rate := modeFixedCost16[mode]

		score := RDScore(disto, rate, seg.LambdaI16)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
		}
	}

	// Checkerboard avoidance at borders: for flat source blocks at the image
	// edge, force DC (x==0) or VPred (y==0) to prevent checkerboard resonance.
	// Matching C libwebp quant_enc.c:1260-1265.
	if mbX == 0 || mbY == 0 {
		if isFlatSource16(srcBuf, srcOff) {
			if mbX == 0 {
				bestMode = DCPred // mode 0
			} else {
				bestMode = VPred // mode 2
			}
		}
	}

	return
}

// PickBestI4Mode evaluates all 4x4 intra prediction modes for a single
// sub-block and returns the best one.
// srcBuf/srcOff and predBuf/predOff point to the sub-block in BPS-strided buffers.
// hasTop/hasLeft indicate whether context is available for this sub-block.
func PickBestI4Mode(srcBuf []byte, srcOff int, predBuf []byte, predOff int, seg *SegmentInfo, topMode, leftMode uint8, hasTop, hasLeft bool) (bestMode uint8, bestScore uint64) {
	bestScore = ^uint64(0)
	bestMode = BDCPred

	for mode := 0; mode < NumBModes; mode++ {
		// Skip modes requiring unavailable context.
		if !hasTop && needsTop4(mode) {
			continue
		}
		if !hasLeft && needsLeft4(mode) {
			continue
		}

		dsp.PredLuma4Direct(mode, predBuf, predOff)

		disto := dsp.SSE4x4Direct(srcBuf[srcOff:], predBuf[predOff:])
		rate := int(VP8FixedCostsI4[topMode][leftMode][mode])

		score := RDScore(disto, rate, seg.LambdaI4)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
		}
	}
	return
}

// needsTop4 returns true if the 4x4 prediction mode requires top context.
func needsTop4(mode int) bool {
	switch mode {
	case BVEPred, BVRPred, BLDPred, BVLPred, BHDPred, BRDPred, BTMPred:
		return true
	}
	return false
}

// needsLeft4 returns true if the 4x4 prediction mode requires left context.
func needsLeft4(mode int) bool {
	switch mode {
	case BHEPred, BHUPred, BHDPred, BRDPred, BTMPred:
		return true
	}
	return false
}

// PickBestUVMode evaluates chroma prediction modes and returns the best one.
// srcBuf/srcOff and predBuf/predOff point to the U data in BPS-strided buffers.
// V data is at offset VOff-UOff = 16 bytes from U within the same buffer.
func PickBestUVMode(srcBuf []byte, srcOff int, predBuf []byte, predOff int, seg *SegmentInfo, mbX, mbY int) (bestMode uint8, bestScore uint64) {
	bestScore = ^uint64(0)
	bestMode = DCPred

	vDelta := VOff - UOff // = 16, offset from U to V within the buffer

	for mode := 0; mode < NumPredModes; mode++ {
		actualMode := checkMode(mbX, mbY, mode)

		if mode == int(VPred) && mbY == 0 {
			continue
		}
		if mode == int(HPred) && mbX == 0 {
			continue
		}
		if mode == int(TMPred) && (mbX == 0 || mbY == 0) {
			continue
		}

		// Generate prediction for both U and V channels.
		dsp.PredChroma8Direct(actualMode, predBuf, predOff)
		dsp.PredChroma8Direct(actualMode, predBuf, predOff+vDelta)

		// Compute full 8x8 SSE for U (4 blocks of 4x4).
		distoU := 0
		for by := 0; by < 2; by++ {
			for bx := 0; bx < 2; bx++ {
				off := by*4*dsp.BPS + bx*4
				distoU += dsp.SSE4x4Direct(srcBuf[srcOff+off:], predBuf[predOff+off:])
			}
		}

		// Compute full 8x8 SSE for V (4 blocks of 4x4).
		distoV := 0
		for by := 0; by < 2; by++ {
			for bx := 0; bx < 2; bx++ {
				off := by*4*dsp.BPS + bx*4
				distoV += dsp.SSE4x4Direct(srcBuf[srcOff+vDelta+off:], predBuf[predOff+vDelta+off:])
			}
		}

		disto := distoU + distoV
		rate := modeFixedCostUV[mode]

		score := RDScore(disto, rate, seg.LambdaUV)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
		}
	}
	return
}

// --- RD-aware mode selection (Method >= 3) ---

// PickBestI16ModeRD evaluates all 16x16 intra prediction modes using full
// rate-distortion scoring: forward transform, quantize, dequantize, reconstruct,
// then measure distortion and token cost.
// Returns the best mode, a type-specific score (using LambdaI16), and the
// separate rate/distortion for re-scoring with LambdaMode.
func (enc *VP8Encoder) PickBestI16ModeRD(it *MBIterator, seg *SegmentInfo) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = DCPred

	src := enc.yuvIn
	pred := enc.yuvOut2

	// Check if the source block is flat (all same pixel value).
	// Matching C libwebp's PickBestIntra16 (quant_enc.c:993).
	srcFlat := isFlatSource16(src, YOff)

	// Copy prediction context once before the mode loop. The reconstruction
	// loop (ITransform) only modifies the 16x16 block area [YOff..YOff+16*BPS],
	// while PredLuma16 reads only from border pixels (top row, left column)
	// which are never modified. So we don't need to re-copy per mode.
	copy(pred[:UOff], enc.yuvOut[:UOff])

	for mode := 0; mode < NumPredModes; mode++ {
		actualMode := checkMode(it.X, it.Y, mode)

		if mode == int(VPred) && it.Y == 0 {
			continue
		}
		if mode == int(HPred) && it.X == 0 {
			continue
		}
		if mode == int(TMPred) && (it.X == 0 || it.Y == 0) {
			continue
		}

		// Generate prediction (context border pixels are intact from previous modes).
		dsp.PredLuma16Direct(actualMode, pred, YOff)

		// Forward transform, quantize each 4x4 sub-block.
		// Use pre-allocated encoder buffers to avoid heap escapes.
		enc.tmpDCCoeffs = [16]int16{}
		totalRate := modeFixedCost16[mode]

		// NZ context tracking (mirrors recordMBTokens).
		tnz := enc.topNz[it.X] & 0x0f
		lnz := enc.leftNz & 0x0f

		// DC context for WHT block.
		dcCtx := int(enc.topNzDC[it.X]) + int(enc.leftNzDC)
		if dcCtx > 2 {
			dcCtx = 2
		}

		for by := 0; by < 4; by++ {
			l := lnz & 1
			for bx := 0; bx < 4; bx++ {
				blockIdx := by*4 + bx
				srcOff := YOff + by*4*dsp.BPS + bx*4

				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}

				dsp.FTransformDirect(src[srcOff:], pred[srcOff:], enc.tmpCoeffs[:])

				enc.tmpDCCoeffs[blockIdx] = enc.tmpCoeffs[0]
				enc.tmpCoeffs[0] = 0

				nz := QuantizeCoeffs(enc.tmpCoeffs[:], enc.tmpQCoeffs[:], &seg.Y1, 1)
				enc.tmpAllQ[blockIdx] = enc.tmpQCoeffs

				totalRate += TokenCostForCoeffs(enc.tmpQCoeffs[:], nz, 0, &enc.proba, ctx, 1)

				if nz > 0 {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}

		// WHT transform on DC coefficients (reuse tmpCoeffs/tmpQCoeffs).
		dsp.FTransformWHT(enc.tmpDCCoeffs[:], enc.tmpCoeffs[:])
		nzDC := QuantizeCoeffs(enc.tmpCoeffs[:], enc.tmpQCoeffs[:], &seg.Y2, 0)
		totalRate += TokenCostForCoeffs(enc.tmpQCoeffs[:], nzDC, 1, &enc.proba, dcCtx, 0)

		// Reconstruct with proper DC values for accurate distortion.
		// Note: pred still contains the correct prediction from the first
		// PredLuma16 call above — the FTransform loop only reads from pred.

		// Inverse WHT to get DC values.
		DequantCoeffs(enc.tmpQCoeffs[:], enc.tmpWHTDQ[:], &seg.Y2)
		dsp.TransformWHT(enc.tmpWHTDQ[:], enc.tmpWHTBuf[:])

		for by := 0; by < 4; by++ {
			for bx := 0; bx < 4; bx++ {
				blockIdx := by*4 + bx
				srcOff := YOff + by*4*dsp.BPS + bx*4

				DequantCoeffs(enc.tmpAllQ[blockIdx][:], enc.tmpDQCoeffs[:], &seg.Y1)
				enc.tmpDQCoeffs[0] = enc.tmpWHTBuf[blockIdx*16]

				dsp.ITransformDirect(pred[srcOff:], enc.tmpDQCoeffs[:], pred[srcOff:], false)
			}
		}

		// Compute distortion: SSE + perceptual texture distortion (SD).
		disto := dsp.SSE16x16Direct(src[YOff:], pred[YOff:])
		if seg.TLambdaSD > 0 {
			td := dsp.TDisto16x16(src[YOff:], pred[YOff:])
			disto += (seg.TLambdaSD*td + 128) >> 8
		}

		// Flatness detection: if source is flat in pixel space, refine by
		// checking quantized AC coefficients. If still flat, double distortion
		// to emphasize low-distortion modes for uniform blocks.
		// Matching C libwebp PickBestIntra16 (quant_enc.c:1009-1016).
		if srcFlat {
			for b := 0; b < 16; b++ {
				copy(enc.tmpACLevels[b*16:b*16+16], enc.tmpAllQ[b][:])
			}
			if isFlat(enc.tmpACLevels[:], 16, flatnessLimitI16) {
				disto *= 2
			}
		}

		// Use LambdaI16 for choosing which I16 sub-mode is best.
		score := RDScore(disto, totalRate, seg.LambdaI16)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = totalRate
			bestDisto = disto
		}
	}

	return
}

// PickBestI4ModeRD evaluates all 4x4 intra prediction modes for a single
// sub-block using full rate-distortion scoring.
// Returns best mode, type-specific score (LambdaI4), rate, distortion, and quantized coeffs.
func (enc *VP8Encoder) PickBestI4ModeRD(srcBuf []byte, srcOff int, predBuf []byte, predOff int,
	seg *SegmentInfo, topMode, leftMode uint8, hasTop, hasLeft bool, nzCtx int) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = BDCPred

	for mode := 0; mode < NumBModes; mode++ {
		if !hasTop && needsTop4(mode) {
			continue
		}
		if !hasLeft && needsLeft4(mode) {
			continue
		}

		// Generate prediction.
		dsp.PredLuma4Direct(mode, predBuf, predOff)

		// Forward transform + quantize using pre-allocated buffers.
		dsp.FTransformDirect(srcBuf[srcOff:], predBuf[predOff:], enc.tmpCoeffs[:])
		nz := QuantizeCoeffs(enc.tmpCoeffs[:], enc.tmpQCoeffs[:], &seg.Y1, 0)

		// Dequantize and reconstruct for distortion (compute BEFORE rate
		// to enable early termination for losing modes).
		// Use predBuf[predOff:] directly as ref — avoids copying into tmpRecon.
		DequantCoeffs(enc.tmpQCoeffs[:], enc.tmpDQCoeffs[:], &seg.Y1)
		dsp.ITransformDirect(predBuf[predOff:], enc.tmpDQCoeffs[:], enc.tmpRecon[:], false)

		disto := dsp.SSE4x4Direct(srcBuf[srcOff:], enc.tmpRecon[:])
		if seg.TLambdaSD > 0 {
			td := dsp.TDisto4x4(srcBuf[srcOff:], enc.tmpRecon[:])
			disto += (seg.TLambdaSD*td + 128) >> 8
		}

		// Early termination: if distortion alone already exceeds best score,
		// this mode can't win regardless of rate. Skip expensive rate computation.
		if 256*uint64(disto) >= bestScore {
			continue
		}

		// Flatness penalty: if the block is flat (few non-zero AC coeffs)
		// and using a non-DC mode, add a penalty to discourage complex
		// predictions for flat areas. Matching C libwebp (quant_enc.c:1097-1103):
		// IsFlat counts non-zero coefficients (not position of last non-zero).
		rate := 0
		if mode > 0 && isFlat(enc.tmpQCoeffs[:], 1, flatnessLimitI4) {
			rate = flatnessPenalty * 1 // kNumBlocks=1 for I4
		}

		// Rate: token cost + mode signaling cost.
		rate += TokenCostForCoeffs(enc.tmpQCoeffs[:], nz, 3, &enc.proba, nzCtx, 0)
		rate += int(VP8FixedCostsI4[topMode][leftMode][mode])

		score := RDScore(disto, rate, seg.LambdaI4)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = rate
			bestDisto = disto
			// Save quantized and dequantized coefficients for the winning mode.
			// tryI4ModesRD uses these to reconstruct without re-doing FT+Q+DQ,
			// and caches quantized coeffs to skip encodeI4Residuals later.
			enc.tmpBestDQ = enc.tmpDQCoeffs
			enc.tmpBestQ = enc.tmpQCoeffs
			enc.tmpBestNz = nz
		}
	}

	return
}

// PickBestI4ModeRDTrellis is like PickBestI4ModeRD but uses trellis quantization
// instead of regular quantization. The cached coefficients are the final
// trellis-quantized values, eliminating the need for a second quantization pass
// in encodeI4Residuals.
func (enc *VP8Encoder) PickBestI4ModeRDTrellis(srcBuf []byte, srcOff int, predBuf []byte, predOff int,
	seg *SegmentInfo, topMode, leftMode uint8, hasTop, hasLeft bool, nzCtx int) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = BDCPred

	// Pre-screen: compute prediction SSE for all eligible modes.
	type modeCandidate struct {
		mode int
		sse  int
	}
	var candidates [NumBModes]modeCandidate
	nCandidates := 0

	for mode := 0; mode < NumBModes; mode++ {
		if !hasTop && needsTop4(mode) {
			continue
		}
		if !hasLeft && needsLeft4(mode) {
			continue
		}
		dsp.PredLuma4Direct(mode, predBuf, predOff)
		sse := dsp.SSE4x4Direct(srcBuf[srcOff:], predBuf[predOff:])
		candidates[nCandidates] = modeCandidate{mode, sse}
		nCandidates++
	}

	// Select top K modes by prediction SSE (partial selection sort).
	K := maxI4RDModes
	if nCandidates <= K {
		K = nCandidates
	}
	for i := 0; i < K; i++ {
		minIdx := i
		for j := i + 1; j < nCandidates; j++ {
			if candidates[j].sse < candidates[minIdx].sse {
				minIdx = j
			}
		}
		if minIdx != i {
			candidates[i], candidates[minIdx] = candidates[minIdx], candidates[i]
		}
	}

	// Full RD evaluation for top K modes only.
	for i := 0; i < K; i++ {
		mode := candidates[i].mode

		dsp.PredLuma4Direct(mode, predBuf, predOff)
		dsp.FTransformDirect(srcBuf[srcOff:], predBuf[predOff:], enc.tmpCoeffs[:])
		nz := TrellisQuantizeBlock(enc.tmpCoeffs[:], enc.tmpQCoeffs[:],
			&seg.Y1, 0, 3, nzCtx, &enc.proba, seg.TLambdaI4)

		DequantCoeffs(enc.tmpQCoeffs[:], enc.tmpDQCoeffs[:], &seg.Y1)
		dsp.ITransformDirect(predBuf[predOff:], enc.tmpDQCoeffs[:], enc.tmpRecon[:], false)

		disto := dsp.SSE4x4Direct(srcBuf[srcOff:], enc.tmpRecon[:])
		if seg.TLambdaSD > 0 {
			td := dsp.TDisto4x4(srcBuf[srcOff:], enc.tmpRecon[:])
			disto += (seg.TLambdaSD*td + 128) >> 8
		}

		if 256*uint64(disto) >= bestScore {
			continue
		}

		rate := 0
		if mode > 0 && isFlat(enc.tmpQCoeffs[:], 1, flatnessLimitI4) {
			rate = flatnessPenalty * 1
		}

		rate += TokenCostForCoeffs(enc.tmpQCoeffs[:], nz, 3, &enc.proba, nzCtx, 0)
		rate += int(VP8FixedCostsI4[topMode][leftMode][mode])

		score := RDScore(disto, rate, seg.LambdaI4)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = rate
			bestDisto = disto
			enc.tmpBestDQ = enc.tmpDQCoeffs
			enc.tmpBestQ = enc.tmpQCoeffs
			enc.tmpBestNz = nz
		}
	}

	return
}

// PickBestUVModeRD evaluates chroma prediction modes using full
// rate-distortion scoring.
func (enc *VP8Encoder) PickBestUVModeRD(it *MBIterator, seg *SegmentInfo) (bestMode uint8, bestScore uint64, bestRate int, bestDisto int) {
	bestScore = ^uint64(0)
	bestMode = DCPred

	src := enc.yuvIn
	pred := enc.yuvOut2

	// Copy UV context once before the mode loop. The reconstruction
	// (ITransform) only modifies the 8x8 block area, while PredChroma8
	// reads only from border pixels which are never modified.
	copy(pred[UOff:], enc.yuvOut[UOff:])

	for mode := 0; mode < NumPredModes; mode++ {
		actualMode := checkMode(it.X, it.Y, mode)

		if mode == int(VPred) && it.Y == 0 {
			continue
		}
		if mode == int(HPred) && it.X == 0 {
			continue
		}
		if mode == int(TMPred) && (it.X == 0 || it.Y == 0) {
			continue
		}

		// Generate chroma prediction (context border pixels are intact).
		dsp.PredChroma8Direct(actualMode, pred, UOff)
		dsp.PredChroma8Direct(actualMode, pred, VOff)

		totalRate := modeFixedCostUV[mode]

		// Collect UV quantized levels for flatness check (8 blocks total).
		uvBlockIdx := 0

		// Process U and V blocks with NZ context tracking.
		chPlanes := [2]int{UOff, VOff}
		for ch := uint(0); ch < 4; ch += 2 {
			tnz := (enc.topNz[it.X] >> (4 + ch)) & 0x0f
			lnz := (enc.leftNz >> (4 + ch)) & 0x0f
			planeOff := chPlanes[ch/2]

			for by := 0; by < 2; by++ {
				l := lnz & 1
				for bx := 0; bx < 2; bx++ {
					off := by*4*dsp.BPS + bx*4
					ctx := int(l) + int(tnz&1)
					if ctx > 2 {
						ctx = 2
					}
					dsp.FTransformDirect(src[planeOff+off:], pred[planeOff+off:], enc.tmpCoeffs[:])
					nz := QuantizeCoeffs(enc.tmpCoeffs[:], enc.tmpQCoeffs[:], &seg.UV, 0)
					totalRate += TokenCostForCoeffs(enc.tmpQCoeffs[:], nz, 2, &enc.proba, ctx, 0)
					copy(enc.tmpUVLevels[uvBlockIdx*16:uvBlockIdx*16+16], enc.tmpQCoeffs[:])
					uvBlockIdx++
					DequantCoeffs(enc.tmpQCoeffs[:], enc.tmpDQCoeffs[:], &seg.UV)
					dsp.ITransformDirect(pred[planeOff+off:], enc.tmpDQCoeffs[:], pred[planeOff+off:], false)

					if nz > 0 {
						l = 1
					} else {
						l = 0
					}
					tnz = (tnz >> 1) | (l << 3)
				}
				tnz >>= 2
				lnz = (lnz >> 1) | (l << 5)
			}
		}

		// Add flatness penalty for non-DC UV modes with flat coefficients.
		// Matching C libwebp PickBestUV (quant_enc.c:1173-1175).
		if mode > 0 && isFlat(enc.tmpUVLevels[:], 8, flatnessLimitUV) {
			totalRate += flatnessPenalty * 8 // kNumBlocks=8 for UV
		}

		// Compute distortion for both U and V.
		distoU := 0
		distoV := 0
		for by := 0; by < 2; by++ {
			for bx := 0; bx < 2; bx++ {
				off := by*4*dsp.BPS + bx*4
				distoU += dsp.SSE4x4Direct(src[UOff+off:], pred[UOff+off:])
				distoV += dsp.SSE4x4Direct(src[VOff+off:], pred[VOff+off:])
			}
		}

		disto := distoU + distoV
		score := RDScore(disto, totalRate, seg.LambdaUV)
		if score < bestScore {
			bestScore = score
			bestMode = uint8(mode)
			bestRate = totalRate
			bestDisto = disto
		}
	}

	return
}

// modeFixedCost16 is the fixed bit cost for each 16x16 mode.
// These include the VP8BitCost(1, 145) cost for signaling "not I4".
// Matches libwebp VP8FixedCostsI16. Indexed by DCPred=0, TMPred=1, VPred=2, HPred=3.
var modeFixedCost16 = [NumPredModes]int{663, 919, 872, 919}

// modeFixedCostUV is the fixed bit cost for each UV mode.
// From libwebp VP8FixedCostsUV. Indexed by DCPred=0, TMPred=1, VPred=2, HPred=3.
var modeFixedCostUV = [NumPredModes]int{302, 984, 439, 642}

// VP8FixedCostsI4 holds precomputed I4 mode signaling costs.
// Indexed as [topMode][leftMode][targetMode]. Computed by walking the
// KYModesIntra4 tree with KBModesProba probabilities via VP8BitCost.
// Matches libwebp VP8FixedCostsI4.
var VP8FixedCostsI4 [NumBModes][NumBModes][NumBModes]uint16

func init() {
	computeFixedCostsI4()
}

// computeFixedCostsI4 precomputes I4 mode signaling costs for all context/mode combos.
func computeFixedCostsI4() {
	for top := 0; top < NumBModes; top++ {
		for left := 0; left < NumBModes; left++ {
			prob := &KBModesProba[top][left]
			for mode := 0; mode < NumBModes; mode++ {
				VP8FixedCostsI4[top][left][mode] = uint16(i4ModeCost(mode, prob))
			}
		}
	}
}

// i4ModeCost computes the bit cost of encoding a specific I4 mode through
// the KYModesIntra4 tree using the given context probabilities.
func i4ModeCost(mode int, prob *[NumBModes - 1]uint8) int {
	cost := 0
	// First step: choose between tree[0] (bit=0) and tree[1] (bit=1).
	left := int(KYModesIntra4[0])
	bit := 0
	if !i4SubtreeContains(left, mode) {
		bit = 1
	}
	cost += dsp.VP8BitCost(bit, prob[0])
	i := int(KYModesIntra4[bit])

	// Subsequent steps.
	for i > 0 {
		left = int(KYModesIntra4[2*i])
		bit = 0
		if !i4SubtreeContains(left, mode) {
			bit = 1
		}
		cost += dsp.VP8BitCost(bit, prob[i])
		i = int(KYModesIntra4[2*i+bit])
	}
	return cost
}

