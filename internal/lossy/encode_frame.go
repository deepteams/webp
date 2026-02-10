package lossy

import (
	"github.com/deepteams/webp/internal/dsp"
)

// minRefreshCount is the minimum number of MBs between probability refreshes.
// Matches C libwebp's MIN_COUNT = 96.
const minRefreshCount = 96

// encodeFrame performs the main encoding loop over all macroblocks.
// For each MB: choose prediction mode, compute residuals, quantize, record tokens.
// Probabilities are refreshed periodically (~8 times per frame) to allow
// mid-stream adaptation, matching C libwebp's VP8EncTokenLoop behavior.
func (enc *VP8Encoder) encodeFrame() {
	enc.InitIterator()
	it := &enc.mbIterator

	// Reset NZ context tracking.
	for i := range enc.topNz {
		enc.topNz[i] = 0
	}
	for i := range enc.topNzDC {
		enc.topNzDC[i] = 0
	}
	enc.leftNz = 0
	enc.leftNzDC = 0

	enc.numSkip = 0

	// Mid-stream probability refresh: roughly every 1/8 of the frame,
	// collect stats and update probability tables so that subsequent
	// mode selection uses more accurate RD cost estimates.
	// Matches C libwebp's VP8EncTokenLoop refresh logic (frame_enc.c:793).
	totalMB := enc.mbW * enc.mbH
	maxCount := totalMB >> 3
	if maxCount < minRefreshCount {
		maxCount = minRefreshCount
	}
	refreshCnt := maxCount

	for !it.IsDone() {
		// Reset left NZ at start of each row.
		if it.X == 0 {
			enc.leftNz = 0
			enc.leftNzDC = 0
		}
		mbIdx := it.MBIdx
		info := &enc.mbInfo[mbIdx]
		seg := &enc.dqm[info.Segment]

		// Mid-stream probability refresh.
		refreshCnt--
		if refreshCnt < 0 {
			enc.refreshProbas()
			refreshCnt = maxCount
		}

		// 1. Import source data for this MB.
		it.Import(enc)

		// 2. Fill prediction context borders.
		it.FillPredictionContext(enc)

		// 3. Choose prediction mode.
		enc.pickBestMode(it, info, seg)

		// 4. Compute residuals, transform, quantize.
		enc.encodeResiduals(it, info, seg)

		// 5. Detect skip: all coefficients are zero.
		info.Skip = (info.NonZeroY == 0 && info.NonZeroUV == 0)
		if info.Skip {
			enc.numSkip++
		}

		// 6. Record tokens for the coefficient data (skip if no coefficients).
		if info.Skip {
			// Mirror decoder's skip handling: clear NZ context.
			enc.topNz[it.X] = 0
			enc.leftNz = 0
			if info.MBType == 0 {
				enc.topNzDC[it.X] = 0
				enc.leftNzDC = 0
			}
		} else if enc.skipTokens {
			// StatLoop mode: update NZ context without recording tokens.
			enc.updateNZContext(it, info)
		} else {
			enc.recordMBTokens(it, info)
		}

		// 7. Reconstruct (IDCT + prediction) for future reference.
		enc.reconstructMB(it, info, seg)

		// 8. Export reconstructed data and update context.
		it.Export(enc)

		// 9. Advance.
		it.Next()
	}

	// Compute skip probability: P(skip=0) = (total - nb) * 255 / total.
	// Matches C libwebp CalcSkipProba in frame_enc.c.
	if enc.numSkip > 0 {
		enc.skipProba = uint8((totalMB - enc.numSkip) * 255 / totalMB)
	}
}

// refreshProbas collects coefficient statistics from all encoded MBs so far
// and updates the probability tables. This matches C libwebp's mid-stream
// FinalizeTokenProbas + VP8CalculateLevelCosts refresh in VP8EncTokenLoop.
func (enc *VP8Encoder) refreshProbas() {
	var stats ProbaStats
	enc.collectAllStats(&stats)
	optimizeProba(&stats, &enc.proba)
}

// pickBestMode selects the best intra prediction mode for the macroblock.
// Note: FillPredictionContext must be called before this (done in encodeFrame).
func (enc *VP8Encoder) pickBestMode(it *MBIterator, info *MBEncInfo, seg *SegmentInfo) {
	if enc.config.Method >= 3 {
		// RD-aware mode selection.
		// Per libwebp: each type uses its own lambda for internal mode selection,
		// but the final I4-vs-I16 decision uses LambdaMode for both.
		bestMode16, _, rate16, disto16 := enc.PickBestI16ModeRD(it, seg)
		// Re-score I16 with LambdaMode for fair comparison.
		bestScore16 := RDScore(disto16, rate16, seg.LambdaMode)

		var bestScore4 uint64 = ^uint64(0)
		var modes4 [16]uint8
		bestScore4 = enc.tryI4ModesRD(it, info, seg, &modes4, bestScore16)

		if bestScore4 < bestScore16 {
			info.MBType = 1
			info.Modes = modes4
			info.Score = bestScore4
			info.PredCached = false
			// When Method >= 4, tryI4ModesRD used trellis quantization.
			// The cached coefficients in info.Coeffs/NzY are final.
			// Copy the reconstructed Y plane from yuvOut2 to yuvOut.
			if enc.config.Method >= 4 {
				info.I4Cached = true
				for j := 0; j < 16; j++ {
					off := YOff + j*dsp.BPS
					copy(enc.yuvOut[off:off+16], enc.yuvOut2[off:off+16])
				}
			}
		} else {
			info.MBType = 0
			info.I16Mode = bestMode16
			info.Score = bestScore16
			info.I4Cached = false
			// Generate winning I16 prediction once in yuvOut.
			actualI16 := checkMode(it.X, it.Y, int(bestMode16))
			dsp.PredLuma16Direct(actualI16, enc.yuvOut, YOff)
			info.PredCached = true
		}

		bestUV, _, _, _ := enc.PickBestUVModeRD(it, seg)
		info.UVMode = bestUV

		// Generate winning UV prediction once in yuvOut.
		actualUV := checkMode(it.X, it.Y, int(bestUV))
		dsp.PredChroma8Direct(actualUV, enc.yuvOut, UOff)
		dsp.PredChroma8Direct(actualUV, enc.yuvOut, VOff)
	} else {
		// Original fast mode selection.
		bestMode16, bestScore16 := PickBestI16Mode(enc.yuvIn, YOff, enc.yuvOut, YOff, seg, it.X, it.Y)

		var bestScore4 uint64 = ^uint64(0)
		var modes4 [16]uint8
		if enc.config.Method >= 2 {
			bestScore4 = enc.tryI4Modes(it, info, seg, &modes4)
		}

		if bestScore4 < bestScore16 {
			info.MBType = 1
			info.Modes = modes4
			info.Score = bestScore4
		} else {
			info.MBType = 0
			info.I16Mode = bestMode16
			info.Score = bestScore16
		}

		bestUV, _ := PickBestUVMode(enc.yuvIn, UOff, enc.yuvOut, UOff, seg, it.X, it.Y)
		info.UVMode = bestUV
	}
}

// tryI4Modes evaluates I4x4 prediction for all 16 sub-blocks.
func (enc *VP8Encoder) tryI4Modes(it *MBIterator, info *MBEncInfo, seg *SegmentInfo, modes *[16]uint8) uint64 {
	totalScore := uint64(0)
	topModes := it.GetTopModes()

	for by := 0; by < 4; by++ {
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx

			// Get context modes.
			var topMode, leftMode uint8
			if by == 0 {
				topMode = topModes[bx]
			} else {
				topMode = modes[blockIdx-4]
			}
			if bx == 0 {
				leftMode = it.leftModes[by]
			} else {
				leftMode = modes[blockIdx-1]
			}

			// Sub-block source and pred in BPS-strided layout.
			// yuvP has 1 row of top context + 1 byte of left context,
			// so the block origin is at BPS+1.
			srcOff := YOff + by*4*dsp.BPS + bx*4
			predOff := dsp.BPS + 1 + by*4*dsp.BPS + bx*4

			hasTop := (it.Y > 0 || by > 0)
			hasLeft := (it.X > 0 || bx > 0)
			bestMode, bestScore := PickBestI4Mode(enc.yuvIn, srcOff, enc.yuvP, predOff, seg, topMode, leftMode, hasTop, hasLeft)
			modes[blockIdx] = bestMode
			totalScore += bestScore
		}
	}

	// Save bottom-row modes for top context.
	it.SaveTopModes([4]uint8{modes[12], modes[13], modes[14], modes[15]})
	// Save right-column modes for left context.
	it.leftModes = [4]uint8{modes[3], modes[7], modes[11], modes[15]}

	// Add I4 selector bit cost: VP8BitCost(0, 145) = 211.
	totalScore += uint64(seg.LambdaMode) * 211

	return totalScore
}

// tryI4ModesRD evaluates I4x4 prediction for all 16 sub-blocks using RD scoring.
// Returns the total score computed with LambdaMode (for fair comparison with I16).
// i16Score is the I16 baseline score; if the running I4 score exceeds it, we early-exit.
func (enc *VP8Encoder) tryI4ModesRD(it *MBIterator, info *MBEncInfo, seg *SegmentInfo, modes *[16]uint8, i16Score uint64) uint64 {
	totalRate := 0
	totalDisto := 0
	totalHeaderBits := 0
	topModes := it.GetTopModes()

	// Use yuvOut2 for I4 RD trials - copy context.
	copy(enc.yuvOut2, enc.yuvOut)

	// NZ context tracking for I4 token cost (mirrors recordMBTokens).
	tnz := enc.topNz[it.X] & 0x0f
	lnz := enc.leftNz & 0x0f

	// Maximum I4 header bits budget (matching libwebp: 15000).
	maxI4HeaderBits := 15000

	earlyExit := false

	for by := 0; by < 4 && !earlyExit; by++ {
		l := lnz & 1
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx

			var topMode, leftMode uint8
			if by == 0 {
				topMode = topModes[bx]
			} else {
				topMode = modes[blockIdx-4]
			}
			if bx == 0 {
				leftMode = it.leftModes[by]
			} else {
				leftMode = modes[blockIdx-1]
			}

			srcOff := YOff + by*4*dsp.BPS + bx*4
			hasTop := (it.Y > 0 || by > 0)
			hasLeft := (it.X > 0 || bx > 0)

			nzCtx := int(l) + int(tnz&1)
			if nzCtx > 2 {
				nzCtx = 2
			}

			var bestMode uint8
			var rate, disto int
			if enc.config.Method >= 4 {
				bestMode, _, rate, disto = enc.PickBestI4ModeRDTrellis(enc.yuvIn, srcOff, enc.yuvOut2, srcOff, seg, topMode, leftMode, hasTop, hasLeft, nzCtx)
			} else {
				bestMode, _, rate, disto = enc.PickBestI4ModeRD(enc.yuvIn, srcOff, enc.yuvOut2, srcOff, seg, topMode, leftMode, hasTop, hasLeft, nzCtx)
			}
			modes[blockIdx] = bestMode
			totalRate += rate
			totalDisto += disto
			totalHeaderBits += int(VP8FixedCostsI4[topMode][leftMode][bestMode])

			// Cache quantized coefficients and NZ data for encodeI4Residuals.
			// This eliminates redundant FTransform+Quantize in the encode phase.
			coeffOff := blockIdx * 16
			copy(info.Coeffs[coeffOff:coeffOff+16], enc.tmpBestQ[:])
			nz := enc.tmpBestNz
			info.NzY[blockIdx] = uint8(nz)

			// Early exit: if running I4 score exceeds I16 score, bail.
			runningScore := RDScore(totalDisto, totalRate+211, seg.LambdaMode)
			if runningScore >= i16Score {
				earlyExit = true
				break
			}
			// Header bits budget check.
			if totalHeaderBits > maxI4HeaderBits {
				earlyExit = true
				break
			}

			// Reconstruct best mode in yuvOut2 for future prediction context.
			// Use saved dequantized coefficients from PickBestI4ModeRD to avoid
			// redundant FTransform + Quantize + Dequant (3 expensive ops per block).
			dsp.PredLuma4Direct(int(bestMode), enc.yuvOut2, srcOff)
			dsp.ITransformDirect(enc.yuvOut2[srcOff:], enc.tmpBestDQ[:], enc.yuvOut2[srcOff:], false)

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

	if earlyExit {
		return ^uint64(0) // I4 is worse than I16
	}

	// Save bottom-row modes for top context.
	it.SaveTopModes([4]uint8{modes[12], modes[13], modes[14], modes[15]})
	it.leftModes = [4]uint8{modes[3], modes[7], modes[11], modes[15]}

	// Add I4 selector bit cost: VP8BitCost(0, 145) = 211 (matches libwebp).
	totalRate += 211

	// Re-score with LambdaMode for fair comparison with I16.
	return RDScore(totalDisto, totalRate, seg.LambdaMode)
}

// encodeResiduals computes DCT residuals and quantizes them.
func (enc *VP8Encoder) encodeResiduals(it *MBIterator, info *MBEncInfo, seg *SegmentInfo) {
	if info.MBType == 0 {
		enc.encodeI16Residuals(it, info, seg)
	} else {
		enc.encodeI4Residuals(it, info, seg)
	}
	enc.encodeUVResiduals(it, info, seg)
}

// encodeI16Residuals handles the I16x16 prediction mode residuals.
func (enc *VP8Encoder) encodeI16Residuals(it *MBIterator, info *MBEncInfo, seg *SegmentInfo) {
	srcY := enc.yuvIn[YOff:]
	predY := enc.yuvOut[YOff:] // prediction output

	// Generate 16x16 prediction (skip if already cached by pickBestMode).
	if !info.PredCached {
		actualI16Mode := checkMode(it.X, it.Y, int(info.I16Mode))
		dsp.PredLuma16Direct(actualI16Mode, enc.yuvOut, YOff)
	}

	// Forward DCT for each 4x4 sub-block.
	dcCoeffs := &enc.tmpDCCoeffs
	nzY := uint32(0)

	// Track NZ context for trellis (mirrors recordMBTokens layout).
	tnz := enc.topNz[it.X] & 0x0f
	lnz := enc.leftNz & 0x0f

	for by := 0; by < 4; by++ {
		l := lnz & 1
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx
			srcOff := by*4*dsp.BPS + bx*4
			coeffOff := blockIdx * 16

			dsp.FTransformDirect(srcY[srcOff:], predY[srcOff:], info.Coeffs[coeffOff:])

			// Save DC coefficient for WHT.
			dcCoeffs[blockIdx] = info.Coeffs[coeffOff]
			info.Coeffs[coeffOff] = 0 // clear DC from AC block

			// Quantize AC coefficients (skip DC at index 0).
			var nz int
			if enc.config.Method >= 4 {
				// Trellis quantization for AC blocks (type 0 = I16-AC).
				// Uses Y1 quantizer but TLambdaI16 for lambda, matching C libwebp's
				// lambda_trellis_i16 = (q_i16^2) >> 2.
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz = TrellisQuantizeBlock(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16],
					&seg.Y1, 1, 0, ctx, &enc.proba, seg.TLambdaI16)
				// TrellisQuantizeBlock returns zigzag nzCount directly.
				info.NzY[blockIdx] = uint8(nz)
			} else {
				nz = QuantizeCoeffs(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16], &seg.Y1, 1)
				if nz == 0 {
					info.NzY[blockIdx] = 0
				} else {
					info.NzY[blockIdx] = uint8(nzCount(info.Coeffs[coeffOff : coeffOff+16]))
				}
			}
			if nz > 0 {
				nzY |= 1 << uint(blockIdx)
				l = 1
			} else {
				l = 0
			}
			tnz = (tnz >> 1) | (l << 7)
		}
		tnz >>= 4
		lnz = (lnz >> 1) | (l << 7)
	}

	// Apply WHT to the 16 DC coefficients (flat 4x4 array).
	whtOut := &enc.tmpCoeffs
	dsp.FTransformWHT(dcCoeffs[:], whtOut[:])

	// Quantize WHT output (stored at offset 256 = 16*16 in Coeffs).
	// Note: libwebp does NOT trellis-quantize the WHT block, just normal quantize.
	nzDC := QuantizeCoeffs(whtOut[:], info.Coeffs[384:400], &seg.Y2, 0)
	if nzDC > 0 {
		nzY |= 1 << 24 // flag for DC block
		info.NzDC = uint8(nzCount(info.Coeffs[384:400]))
	} else {
		info.NzDC = 0
	}

	info.NonZeroY = nzY
}

// encodeI4Residuals handles the I4x4 prediction mode residuals.
func (enc *VP8Encoder) encodeI4Residuals(it *MBIterator, info *MBEncInfo, seg *SegmentInfo) {
	// Fast path: when I4Cached is set (Method >= 4 and I4 won), coefficients
	// and NzY are already final from PickBestI4ModeRDTrellis. The Y plane in
	// yuvOut was copied from yuvOut2 in pickBestMode. We only need to compute
	// the NonZeroY bitmask from cached NzY values.
	if info.I4Cached {
		nzY := uint32(0)
		for blockIdx := 0; blockIdx < 16; blockIdx++ {
			if info.NzY[blockIdx] > 0 {
				nzY |= 1 << uint(blockIdx)
			}
		}
		info.NonZeroY = nzY
		return
	}

	srcY := enc.yuvIn[YOff:]
	predY := enc.yuvOut[YOff:]
	nzY := uint32(0)

	// Track NZ context for trellis (mirrors recordMBTokens layout).
	tnz := enc.topNz[it.X] & 0x0f
	lnz := enc.leftNz & 0x0f

	for by := 0; by < 4; by++ {
		l := lnz & 1
		for bx := 0; bx < 4; bx++ {
			blockIdx := by*4 + bx
			srcOff := by*4*dsp.BPS + bx*4
			coeffOff := blockIdx * 16

			// Generate 4x4 prediction.
			mode := info.Modes[blockIdx]
			dsp.PredLuma4Direct(int(mode), enc.yuvOut, YOff+srcOff)

			// Forward DCT.
			dsp.FTransformDirect(srcY[srcOff:], predY[srcOff:], info.Coeffs[coeffOff:])

			// Quantize all 16 coefficients (including DC at index 0).
			nz := QuantizeCoeffs(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16], &seg.Y1, 0)
			if nz == 0 {
				info.NzY[blockIdx] = 0
			} else {
				info.NzY[blockIdx] = uint8(nzCount(info.Coeffs[coeffOff : coeffOff+16]))
			}
			if nz > 0 {
				nzY |= 1 << uint(blockIdx)
				l = 1
			} else {
				l = 0
			}
			tnz = (tnz >> 1) | (l << 7)

			// Reconstruct immediately (for use as prediction context).
			DequantCoeffs(info.Coeffs[coeffOff:coeffOff+16], enc.tmpDQCoeffs[:], &seg.Y1)
			dsp.ITransformDirect(predY[srcOff:], enc.tmpDQCoeffs[:], predY[srcOff:], false)
		}
		tnz >>= 4
		lnz = (lnz >> 1) | (l << 7)
	}

	info.NonZeroY = nzY
}

// DC error diffusion constants matching libwebp quant_enc.c.
const (
	derrC1     = 7 // fraction of error sent to the 4x4 block below
	derrC2     = 8 // fraction of error sent to the 4x4 block on the right
	derrDShift = 4
	derrDScale = 1 // storage descaling, needed to make the error fit int8
)

// quantizeSingle quantizes a single DC coefficient and returns the quantization error.
// Matches libwebp's QuantizeSingle in quant_enc.c.
func quantizeSingle(v *int16, sq *SegmentQuant) int {
	V := int(*v)
	sign := 1
	if V < 0 {
		sign = -1
		V = -V
	}
	if V > sq.DCZthresh {
		qV := (int(uint32(V)*uint32(sq.DCIQuant)+uint32(sq.DCBias)) >> 17) * sq.DCQuant
		err := V - qV
		*v = int16(sign * qV)
		return (sign * err) >> derrDScale
	}
	*v = 0
	return (sign * V) >> derrDScale
}

// correctDCValues applies DC error diffusion to the UV coefficient blocks.
// Matches libwebp's CorrectDCValues in quant_enc.c.
func (enc *VP8Encoder) correctDCValues(it *MBIterator, info *MBEncInfo, seg *SegmentInfo) {
	for ch := 0; ch < 2; ch++ {
		top := enc.topDerr[it.X][ch]
		left := enc.leftDerr[ch]

		uvBase := 16 + ch*4
		c0dc := &info.Coeffs[uvBase*16]
		c1dc := &info.Coeffs[(uvBase+1)*16]
		c2dc := &info.Coeffs[(uvBase+2)*16]
		c3dc := &info.Coeffs[(uvBase+3)*16]

		*c0dc += int16((derrC1*int(top[0]) + derrC2*int(left[0])) >> (derrDShift - derrDScale))
		err0 := quantizeSingle(c0dc, &seg.UV)
		*c1dc += int16((derrC1*int(top[1]) + derrC2*int(err0)) >> (derrDShift - derrDScale))
		err1 := quantizeSingle(c1dc, &seg.UV)
		*c2dc += int16((derrC1*int(err0) + derrC2*int(left[1])) >> (derrDShift - derrDScale))
		err2 := quantizeSingle(c2dc, &seg.UV)
		*c3dc += int16((derrC1*int(err1) + derrC2*int(err2)) >> (derrDShift - derrDScale))
		err3 := quantizeSingle(c3dc, &seg.UV)

		info.Derr[ch][0] = int8(err1)
		info.Derr[ch][1] = int8(err2)
		info.Derr[ch][2] = int8(err3)
	}
}

// storeDiffusionErrors stores DC error diffusion results for the next block.
// Matches libwebp's StoreDiffusionErrors in quant_enc.c.
func (enc *VP8Encoder) storeDiffusionErrors(it *MBIterator, info *MBEncInfo) {
	for ch := 0; ch < 2; ch++ {
		top := &enc.topDerr[it.X][ch]
		left := &enc.leftDerr[ch]
		left[0] = info.Derr[ch][0]
		left[1] = int8(3 * int(info.Derr[ch][2]) >> 2)
		top[0] = info.Derr[ch][1]
		top[1] = info.Derr[ch][2] - left[1]
	}
}

// encodeUVResiduals handles the chroma residuals.
func (enc *VP8Encoder) encodeUVResiduals(it *MBIterator, info *MBEncInfo, seg *SegmentInfo) {
	srcU := enc.yuvIn[UOff:]
	srcV := enc.yuvIn[VOff:]
	predU := enc.yuvOut[UOff:]
	predV := enc.yuvOut[VOff:]

	// Generate chroma prediction (skip if already cached by pickBestMode).
	if !info.PredCached {
		actualUVMode := checkMode(it.X, it.Y, int(info.UVMode))
		dsp.PredChroma8Direct(actualUVMode, enc.yuvOut, UOff)
		dsp.PredChroma8Direct(actualUVMode, enc.yuvOut, VOff)
	}

	nzUV := uint32(0)

	// Forward DCT all UV blocks first (needed before DC error diffusion).
	srcs := [2][]byte{srcU, srcV}
	preds := [2][]byte{predU, predV}

	for ch := 0; ch < 2; ch++ {
		for by := 0; by < 2; by++ {
			for bx := 0; bx < 2; bx++ {
				blockIdx := by*2 + bx
				srcOff := by*4*dsp.BPS + bx*4
				uvBase := 16 + ch*4
				coeffOff := (uvBase + blockIdx) * 16
				dsp.FTransformDirect(srcs[ch][srcOff:], preds[ch][srcOff:], info.Coeffs[coeffOff:])
			}
		}
	}

	// Apply DC error diffusion if enabled (before quantization).
	if enc.useDerr {
		enc.correctDCValues(it, info, seg)
	}

	// Quantize all UV blocks with NZ context tracking.
	for ch := uint(0); ch < 4; ch += 2 {
		tnz := (enc.topNz[it.X] >> (4 + ch)) & 0x0f
		lnz := (enc.leftNz >> (4 + ch)) & 0x0f

		for by := 0; by < 2; by++ {
			l := lnz & 1
			for bx := 0; bx < 2; bx++ {
				blockIdx := by*2 + bx
				uvBase := 16 + int(ch/2)*4
				coeffOff := (uvBase + blockIdx) * 16

				// Note: UV trellis is disabled, matching C libwebp's DO_TRELLIS_UV=0
				// ("Risky. Not worth.") to avoid chroma color bleeding.
				nz := QuantizeCoeffs(info.Coeffs[coeffOff:coeffOff+16], info.Coeffs[coeffOff:coeffOff+16], &seg.UV, 0)
				// Store nzCount for collectAllStats reuse.
				uvIdx := int(ch/2)*4 + blockIdx
				if nz == 0 {
					info.NzUV[uvIdx] = 0
				} else {
					info.NzUV[uvIdx] = uint8(nzCount(info.Coeffs[coeffOff : coeffOff+16]))
				}
				if nz > 0 {
					nzUV |= 1 << uint(ch/2*4+uint(blockIdx))
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

	// Store DC error diffusion results for the next block.
	if enc.useDerr {
		enc.storeDiffusionErrors(it, info)
	}

	info.NonZeroUV = nzUV
}

// recordMBTokens records the coefficient tokens for the current macroblock,
// tracking NZ context to match the decoder's parseResiduals exactly.
func (enc *VP8Encoder) recordMBTokens(it *MBIterator, info *MBEncInfo) {
	enc.tokens.MarkMBStart(it.MBIdx)

	topNz := enc.topNz[it.X]
	leftNz := enc.leftNz

	var outTNz, outLNz uint32

	if info.MBType == 0 {
		// I16: DC block — use pre-computed nzCount.
		dcCtx := int(enc.topNzDC[it.X]) + int(enc.leftNzDC)
		if dcCtx > 2 {
			dcCtx = 2
		}
		nzDC := int(info.NzDC)
		enc.tokens.RecordCoeffs(info.Coeffs[384:400], nzDC, 1, &enc.proba, 0, dcCtx)
		if nzDC > 0 {
			enc.topNzDC[it.X] = 1
			enc.leftNzDC = 1
		} else {
			enc.topNzDC[it.X] = 0
			enc.leftNzDC = 0
		}

		// 16 luma AC blocks — use pre-computed nzCounts.
		first := 1
		tnz := topNz & 0x0f
		lnz := leftNz & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				off := blockIdx * 16
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz := int(info.NzY[blockIdx])
				enc.tokens.RecordCoeffs(info.Coeffs[off:off+16], nz, 0, &enc.proba, first, ctx)
				if nz > first {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}
		outTNz = tnz
		outLNz = lnz >> 4
	} else {
		// I4x4: 16 full blocks — use pre-computed nzCounts.
		tnz := topNz & 0x0f
		lnz := leftNz & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				off := blockIdx * 16
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz := int(info.NzY[blockIdx])
				enc.tokens.RecordCoeffs(info.Coeffs[off:off+16], nz, 3, &enc.proba, 0, ctx)
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
		outTNz = tnz
		outLNz = lnz >> 4
	}

	// UV blocks — use pre-computed nzCounts.
	for ch := uint(0); ch < 4; ch += 2 {
		tnz := (topNz >> (4 + ch)) & 0x0f
		lnz := (leftNz >> (4 + ch)) & 0x0f
		for y := 0; y < 2; y++ {
			l := lnz & 1
			for x := 0; x < 2; x++ {
				blockIdx := y*2 + x
				uvIdx := int(ch/2)*4 + blockIdx
				off := (16 + uvIdx) * 16
				ctx := int(l) + int(tnz&1)
				if ctx > 2 {
					ctx = 2
				}
				nz := int(info.NzUV[uvIdx])
				enc.tokens.RecordCoeffs(info.Coeffs[off:off+16], nz, 2, &enc.proba, 0, ctx)
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
		outTNz |= (tnz << 4) << ch
		outLNz |= (lnz & 0xf0) << ch
	}

	enc.topNz[it.X] = outTNz
	enc.leftNz = outLNz
}

// updateNZContext computes the NZ context bits for the current macroblock
// WITHOUT recording tokens. Used during statLoop to maintain correct NZ
// tracking while avoiding expensive token page allocations.
func (enc *VP8Encoder) updateNZContext(it *MBIterator, info *MBEncInfo) {
	topNz := enc.topNz[it.X]
	leftNz := enc.leftNz

	var outTNz, outLNz uint32

	if info.MBType == 0 {
		// I16: DC block NZ.
		nzDC := int(info.NzDC)
		if nzDC > 0 {
			enc.topNzDC[it.X] = 1
			enc.leftNzDC = 1
		} else {
			enc.topNzDC[it.X] = 0
			enc.leftNzDC = 0
		}

		// 16 luma AC blocks.
		first := 1
		tnz := topNz & 0x0f
		lnz := leftNz & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				nz := int(info.NzY[blockIdx])
				if nz > first {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 7)
			}
			tnz >>= 4
			lnz = (lnz >> 1) | (l << 7)
		}
		outTNz = tnz
		outLNz = lnz >> 4
	} else {
		// I4x4: 16 full blocks.
		tnz := topNz & 0x0f
		lnz := leftNz & 0x0f
		for y := 0; y < 4; y++ {
			l := lnz & 1
			for x := 0; x < 4; x++ {
				blockIdx := y*4 + x
				nz := int(info.NzY[blockIdx])
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
		outTNz = tnz
		outLNz = lnz >> 4
	}

	// UV blocks.
	for ch := uint(0); ch < 4; ch += 2 {
		tnz := (topNz >> (4 + ch)) & 0x0f
		lnz := (leftNz >> (4 + ch)) & 0x0f
		for y := 0; y < 2; y++ {
			l := lnz & 1
			for x := 0; x < 2; x++ {
				blockIdx := y*2 + x
				uvIdx := int(ch/2)*4 + blockIdx
				nz := int(info.NzUV[uvIdx])
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
		outTNz |= (tnz << 4) << ch
		outLNz |= (lnz & 0xf0) << ch
	}

	enc.topNz[it.X] = outTNz
	enc.leftNz = outLNz
}

// nzCount returns the number of non-zero coefficients in a block.
func nzCount(coeffs []int16) int {
	last := 0
	for n := 15; n >= 0; n-- {
		if coeffs[KZigzag[n]] != 0 {
			last = n + 1
			break
		}
	}
	return last
}

// reconstructMB applies inverse transforms to reconstruct the macroblock
// in yuvOut for use as context for future macroblocks.
func (enc *VP8Encoder) reconstructMB(it *MBIterator, info *MBEncInfo, seg *SegmentInfo) {
	predY := enc.yuvOut[YOff:]

	if info.MBType == 0 {
		// I16: generate prediction, then add dequantized residuals.
		// Skip prediction if already cached by pickBestMode (Method >= 3).
		if !info.PredCached {
			reconI16Mode := checkMode(it.X, it.Y, int(info.I16Mode))
			dsp.PredLuma16Direct(reconI16Mode, enc.yuvOut, YOff)
		}

		// Dequantize WHT coefficients with Y2.
		DequantCoeffs(info.Coeffs[384:400], enc.tmpWHTDQ[:], &seg.Y2)

		// Apply inverse WHT to get per-block DC values.
		// TransformWHT writes output with stride 16 (out[i*16+j]), but we need
		// a flat 16-element array. Use a buffer of 16*16 and extract.
		dsp.TransformWHT(enc.tmpWHTDQ[:], enc.tmpWHTBuf[:])
		// Extract the 16 DC values (at indices 0, 16, 32, ..., 240).
		for i := 0; i < 16; i++ {
			enc.tmpDCCoeffs[i] = enc.tmpWHTBuf[i*16]
		}

		// Add DC to each sub-block and apply IDCT.
		for by := 0; by < 4; by++ {
			for bx := 0; bx < 4; bx++ {
				blockIdx := by*4 + bx
				off := blockIdx * 16
				dstOff := by*4*dsp.BPS + bx*4

				// Dequantize AC coefficients (DC at index 0 is already zeroed
				// during encodeI16Residuals).
				DequantCoeffs(info.Coeffs[off:off+16], enc.tmpDQCoeffs[:], &seg.Y1)

				// Set DC from inverse WHT result.
				enc.tmpDQCoeffs[0] = enc.tmpDCCoeffs[blockIdx]

				dsp.ITransformDirect(predY[dstOff:], enc.tmpDQCoeffs[:], predY[dstOff:], false)
			}
		}
	}
	// I4x4 reconstruction is done during encodeI4Residuals.

	// UV reconstruction (skip prediction if already cached by pickBestMode).
	predU := enc.yuvOut[UOff:]
	predV := enc.yuvOut[VOff:]
	if !info.PredCached {
		reconUVMode := checkMode(it.X, it.Y, int(info.UVMode))
		dsp.PredChroma8Direct(reconUVMode, enc.yuvOut, UOff)
		dsp.PredChroma8Direct(reconUVMode, enc.yuvOut, VOff)
	}

	for by := 0; by < 2; by++ {
		for bx := 0; bx < 2; bx++ {
			blockIdx := by*2 + bx

			// U
			coeffOffU := (16 + blockIdx) * 16
			DequantCoeffs(info.Coeffs[coeffOffU:coeffOffU+16], enc.tmpDQCoeffs[:], &seg.UV)
			dstU := by*4*dsp.BPS + bx*4
			dsp.ITransformDirect(predU[dstU:], enc.tmpDQCoeffs[:], predU[dstU:], false)

			// V
			coeffOffV := (20 + blockIdx) * 16
			DequantCoeffs(info.Coeffs[coeffOffV:coeffOffV+16], enc.tmpDQCoeffs[:], &seg.UV)
			dstV := by*4*dsp.BPS + bx*4
			dsp.ITransformDirect(predV[dstV:], enc.tmpDQCoeffs[:], predV[dstV:], false)
		}
	}
}
