package lossy

import "github.com/deepteams/webp/internal/dsp"

// Trellis quantization using delta-distortion approach.
//
// The score formula: score = rate * lambda + RD_DISTO_MULT * delta_distortion
// where delta_distortion = weight * (new_error² - original_error²).
// Delta_distortion is negative when quantization reduces error from the
// "not coding" baseline, and zero when level=0.

const rdDistoMult = 256

// kWeightTrellis provides per-frequency distortion weights (indexed by zigzag position).
var kWeightTrellis = [16]int{
	30, 27, 19, 11,
	27, 24, 17, 10,
	19, 17, 12, 8,
	11, 10, 8, 6,
}

// TrellisQuantizeBlock finds the optimal quantized levels for a 4x4 block.
func TrellisQuantizeBlock(
	in, out []int16,
	sq *SegmentQuant,
	firstCoeff int,
	ctxType int,
	initialCtx int,
	proba *Proba,
	lambda int,
) int {
	// BCE hints.
	_ = in[15]
	_ = out[15]

	// Quick pre-scan: if neutral-bias quantization gives all-zero levels for
	// every coefficient, the trellis result is trivially all-zero. This avoids
	// the expensive main loop for flat/near-zero blocks (~30-50% of blocks).
	{
		allZero := true
		for n := firstCoeff; n < 16; n++ {
			raw := int(in[KZigzag[n]])
			if raw < 0 {
				raw = -raw
			}
			coeff0 := raw + int(sq.Sharpen[KZigzag[n]])
			if coeff0 < 0 {
				coeff0 = 0
			}
			var iquant int
			if n == 0 {
				iquant = sq.DCIQuant
			} else {
				iquant = sq.IQuant
			}
			if coeff0*iquant>>17 > 0 {
				allZero = false
				break
			}
		}
		if allZero {
			for i := range out[:16] {
				out[i] = 0
			}
			return 0
		}
	}

	var inBuf [16]int16
	copy(inBuf[:], in[:16])

	for i := range out[:16] {
		out[i] = 0
	}

	if initialCtx > 2 {
		initialCtx = 2
	}

	type state struct {
		score   int64
		level   int16
		prevCtx int
		valid   bool
	}

	var prev [3]state
	var curr [3]state

	type pathEntry struct {
		level   int16
		prevCtx int
		valid   bool
	}
	var path [16][3]pathEntry

	for c := 0; c < 3; c++ {
		prev[c].valid = false
	}
	prev[initialCtx] = state{score: 0, valid: true}

	// Skip score: code EOB at the first position.
	firstBand := int(dsp.VP8EncBands[firstCoeff])
	skipRate := dsp.VP8BitCost(0, proba.Bands[ctxType][firstBand].Probas[initialCtx][0])
	bestTerminal := int64(skipRate) * int64(lambda)
	bestLastN := -1
	bestLastCtx := -1

	// Pre-extract AC quant/iquant (DC only used for n==0).
	acQuant := sq.Quant
	acIQuant := sq.IQuant
	lambdaI64 := int64(lambda)
	bands := &proba.Bands[ctxType]

	for n := firstCoeff; n < 16; n++ {
		zigIdx := int(KZigzag[n])
		band := int(dsp.VP8EncBands[n+1])

		raw := int(inBuf[zigIdx])
		sign := 1
		if raw < 0 {
			sign = -1
			raw = -raw
		}

		coeff0 := raw + int(sq.Sharpen[zigIdx])
		if coeff0 < 0 {
			coeff0 = 0
		}

		quant := acQuant
		iquant := acIQuant
		if n == 0 {
			quant = sq.DCQuant
			iquant = sq.DCIQuant
		}

		// Neutral bias quantization (level0).
		L0 := coeff0 * iquant >> 17
		if L0 > 2047 {
			L0 = 2047
		}
		// High bias quantization (thresh_level).
		threshLevel := int(uint32(coeff0)*uint32(iquant)+65536) >> 17
		if threshLevel > 2047 {
			threshLevel = 2047
		}

		weight := int64(kWeightTrellis[zigIdx])
		coeff0sq := int64(coeff0 * coeff0)

		// Extract band probabilities once per coefficient position.
		bandProbas := &bands[band].Probas

		const maxScore = int64(1) << 60
		curr[0].valid = false
		curr[0].score = maxScore
		curr[1].valid = false
		curr[1].score = maxScore
		curr[2].valid = false
		curr[2].score = maxScore

		// Pre-compute L0 and L1 distortion deltas (independent of context).
		hasL0 := L0 > 0 && L0 <= threshLevel
		hasL1 := L0+1 <= 2047 && L0+1 <= threshLevel
		var deltaD0, deltaD1 int64
		var nextCtx0, nextCtx1 int
		var signedL0, signedL1 int16
		if hasL0 {
			newErr := coeff0 - L0*quant
			deltaD0 = weight * (int64(newErr*newErr) - coeff0sq)
			nextCtx0 = L0
			if nextCtx0 > 2 {
				nextCtx0 = 2
			}
			signedL0 = int16(sign * L0)
		}
		if hasL1 {
			L1 := L0 + 1
			newErr := coeff0 - L1*quant
			deltaD1 = weight * (int64(newErr*newErr) - coeff0sq)
			nextCtx1 = L1
			if nextCtx1 > 2 {
				nextCtx1 = 2
			}
			signedL1 = int16(sign * L1)
		}

		for prevCtx := 0; prevCtx < 3; prevCtx++ {
			if !prev[prevCtx].valid {
				continue
			}
			prevScore := prev[prevCtx].score
			p := &bandProbas[prevCtx]

			// Inlined level=0 rate: not-EOB + zero.
			rate0 := dsp.VP8BitCost(1, p[0]) + dsp.VP8BitCost(0, p[1])
			totalScore := prevScore + int64(rate0)*lambdaI64
			if !curr[0].valid || totalScore < curr[0].score {
				curr[0] = state{score: totalScore, level: 0, prevCtx: prevCtx, valid: true}
			}

			if hasL0 {
				// Inlined level=L0 rate: not-EOB + non-zero + level tree.
				rateL0 := dsp.VP8BitCost(1, p[0]) + dsp.VP8BitCost(1, p[1]) + levelCost(L0, p)
				ts := prevScore + int64(rateL0)*lambdaI64 + int64(rdDistoMult)*deltaD0
				if !curr[nextCtx0].valid || ts < curr[nextCtx0].score {
					curr[nextCtx0] = state{score: ts, level: signedL0, prevCtx: prevCtx, valid: true}
				}
			}

			if hasL1 {
				L1 := L0 + 1
				rateL1 := dsp.VP8BitCost(1, p[0]) + dsp.VP8BitCost(1, p[1]) + levelCost(L1, p)
				ts := prevScore + int64(rateL1)*lambdaI64 + int64(rdDistoMult)*deltaD1
				if !curr[nextCtx1].valid || ts < curr[nextCtx1].score {
					curr[nextCtx1] = state{score: ts, level: signedL1, prevCtx: prevCtx, valid: true}
				}
			}
		}

		if curr[0].valid {
			path[n][0] = pathEntry{level: curr[0].level, prevCtx: curr[0].prevCtx, valid: true}
		}
		if curr[1].valid {
			path[n][1] = pathEntry{level: curr[1].level, prevCtx: curr[1].prevCtx, valid: true}
		}
		if curr[2].valid {
			path[n][2] = pathEntry{level: curr[2].level, prevCtx: curr[2].prevCtx, valid: true}
		}

		// Check terminal scores for non-zero contexts.
		for c := 1; c < 3; c++ {
			if !curr[c].valid {
				continue
			}
			eobScore := curr[c].score
			if n < 15 {
				eobRate := dsp.VP8BitCost(0, bands[band].Probas[c][0])
				eobScore += int64(eobRate) * lambdaI64
			}
			if eobScore < bestTerminal {
				bestTerminal = eobScore
				bestLastN = n
				bestLastCtx = c
			}
		}

		prev = curr
	}

	if bestLastN < 0 {
		return 0
	}

	// Backtrack.
	ctx := bestLastCtx
	last := 0
	for n := bestLastN; n >= firstCoeff; n-- {
		if path[n][ctx].valid {
			zigIdx := int(KZigzag[n])
			out[zigIdx] = path[n][ctx].level
			if out[zigIdx] != 0 && last == 0 {
				last = n + 1
			}
			ctx = path[n][ctx].prevCtx
		}
	}

	for n := 0; n < firstCoeff; n++ {
		out[KZigzag[n]] = 0
	}

	return last
}

// computeTrellisLevelRate computes the rate cost for a coefficient level.
func computeTrellisLevelRate(level, n, ctx, ctxType, band int, proba *Proba) int {
	p := &proba.Bands[ctxType][band].Probas[ctx]

	if level == 0 {
		return dsp.VP8BitCost(1, p[0]) + dsp.VP8BitCost(0, p[1])
	}

	// non-zero: not-EOB + non-zero + level tree (including sign bit)
	cost := dsp.VP8BitCost(1, p[0]) + dsp.VP8BitCost(1, p[1])
	cost += levelCost(level, p)
	return cost
}
