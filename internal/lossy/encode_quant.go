package lossy

import "github.com/deepteams/webp/internal/dsp"

// kReverseZigzag maps raster-order coefficient position to zigzag scan position.
// Used by QuantizeCoeffs to compute zigzag nzCount in a single pass.
var kReverseZigzag = [16]int{0, 1, 5, 6, 2, 4, 7, 12, 3, 8, 11, 13, 9, 10, 14, 15}

// quantizeCoeffsGo is the pure-Go reference implementation of QuantizeCoeffs.
// Quantizes a 4x4 coefficient block and returns the zigzag-order
// nzCount: the position after the last non-zero coefficient in zigzag scan order
// (0 if all zero). This eliminates the need for a separate nzCount backward scan.
// firstCoeff is the starting coefficient index (0 for all, 1 to skip DC).
// Coefficient 0 (DC) uses DCIQuant/DCBias, coefficients 1-15 (AC) use IQuant/Bias.
// Uses QFIX=17 QUANTDIV: level = (coeff * iQ + bias) >> 17.
func quantizeCoeffsGo(in, out []int16, sq *SegmentQuant, firstCoeff int) int {
	// BCE hints: prove to compiler that all accesses are in-bounds.
	_ = in[15]
	_ = out[15]

	maxZZ := -1

	// Handle DC coefficient separately to avoid branch in hot AC loop.
	if firstCoeff == 0 {
		v := int(in[0])
		sign := 1
		if v < 0 {
			sign = -1
			v = -v
		}
		v += int(sq.Sharpen[0])
		if v < 0 {
			v = 0
		}
		coeff := int(uint32(v)*uint32(sq.DCIQuant)+uint32(sq.DCBias)) >> 17
		if coeff > 2047 {
			coeff = 2047
		}
		out[0] = int16(sign * coeff)
		if coeff != 0 {
			maxZZ = 0 // DC is zigzag position 0
		}
	} else {
		out[0] = 0
	}

	// AC coefficients (1-15) — loop with hoisted constants.
	// Track max zigzag position of non-zero coefficients inline to avoid
	// a separate backward zigzag scan (saves ~16 iterations per block).
	iq := uint32(sq.IQuant)
	bias := uint32(sq.Bias)
	for n := 1; n < 16; n++ {
		v := int(in[n])
		sign := 1
		if v < 0 {
			sign = -1
			v = -v
		}
		v += int(sq.Sharpen[n])
		if v < 0 {
			v = 0
		}
		coeff := int(uint32(v)*iq+bias) >> 17
		if coeff > 2047 {
			coeff = 2047
		}
		out[n] = int16(sign * coeff)
		if coeff != 0 {
			if zz := kReverseZigzag[n]; zz > maxZZ {
				maxZZ = zz
			}
		}
	}
	return maxZZ + 1
}

// dequantCoeffsGo is the pure-Go reference implementation of DequantCoeffs.
// Dequantizes a coefficient block using the segment quantizer.
// Coefficient 0 (DC) uses DCQuant, coefficients 1-15 (AC) use Quant.
// Fully unrolled for performance.
func dequantCoeffsGo(in, out []int16, sq *SegmentQuant) {
	_ = in[15]
	_ = out[15]
	q := sq.Quant
	out[0] = int16(int(in[0]) * sq.DCQuant)
	out[1] = int16(int(in[1]) * q)
	out[2] = int16(int(in[2]) * q)
	out[3] = int16(int(in[3]) * q)
	out[4] = int16(int(in[4]) * q)
	out[5] = int16(int(in[5]) * q)
	out[6] = int16(int(in[6]) * q)
	out[7] = int16(int(in[7]) * q)
	out[8] = int16(int(in[8]) * q)
	out[9] = int16(int(in[9]) * q)
	out[10] = int16(int(in[10]) * q)
	out[11] = int16(int(in[11]) * q)
	out[12] = int16(int(in[12]) * q)
	out[13] = int16(int(in[13]) * q)
	out[14] = int16(int(in[14]) * q)
	out[15] = int16(int(in[15]) * q)
}

// RDScore computes the rate-distortion score matching libwebp:
//
//	score = rate * lambda + RD_DISTO_MULT * distortion
//
// where RD_DISTO_MULT = 256. Rate is in fixed-point bits (8-bit precision),
// distortion is pixel-domain SSE.
func RDScore(distortion, rate, lambda int) uint64 {
	return uint64(rate)*uint64(lambda) + 256*uint64(distortion)
}

// ComputeResiduals computes the residual coefficients for a macroblock.
// src is the source block in BPS-strided layout, pred is the prediction,
// and out receives the 16 DCT coefficients.
func ComputeResiduals(src, pred []byte, coeffs []int16) {
	dsp.FTransformDirect(src, pred, coeffs)
}

// ComputeResiduals2 computes two side-by-side residual blocks.
func ComputeResiduals2(src, pred []byte, coeffs []int16) {
	dsp.FTransform2(src, pred, coeffs)
}

// ComputeResidualWHT computes the forward Walsh-Hadamard transform for
// the 16 DC coefficients of a 16x16 luma prediction.
func ComputeResidualWHT(dcCoeffs, out []int16) {
	dsp.FTransformWHT(dcCoeffs, out)
}

// --- Cost tables for coefficient coding ---

// LevelCost is a per-segment cost table for coefficient levels.
type LevelCost [NumCTX][NumProbas]uint16

// CostArray holds cached costs for all coefficient types.
type CostArray [NumTypes][NumBands][NumCTX]LevelCost

// ComputeLevelCosts fills in the cost tables from the current probabilities.
func ComputeLevelCosts(proba *Proba, costs *CostArray) {
	for t := 0; t < NumTypes; t++ {
		for b := 0; b < NumBands; b++ {
			for c := 0; c < NumCTX; c++ {
				p := &proba.Bands[t][b].Probas[c]
				computeOneLevelCost(p, &costs[t][b][c])
			}
		}
	}
}

// computeOneLevelCost fills costs for a single context.
func computeOneLevelCost(probas *[NumProbas]uint8, costs *LevelCost) {
	for c := 0; c < NumCTX; c++ {
		for p := 0; p < NumProbas; p++ {
			costs[c][p] = uint16(probas[p])
		}
	}
}

// --- RD token cost ---

// TokenCostForCoeffs computes the approximate rate for a quantized coefficient block.
// coeffs has up to 16 entries in raster order (matching our QuantizeCoeffs output).
// nzCount is the zigzag-order position after the last non-zero coefficient
// (as returned by QuantizeCoeffs or TrellisQuantizeBlock). 0 means all zeros.
// Coefficients are read in zigzag order to match the actual encoding order.
// ctx0 is the initial NZ context (0, 1, or 2) from neighboring blocks.
// first is the first coefficient position to scan (0 for full blocks, 1 for I16-AC
// where the DC coefficient is handled separately via WHT).
func TokenCostForCoeffs(coeffs []int16, nzCount int, ctxType int, proba *Proba, ctx0 int, first int) int {
	_ = coeffs[15] // BCE hint
	ecost := &dsp.VP8EntropyCost
	if nzCount <= first {
		p := proba.BandsPtr[ctxType][first]
		return int(ecost[p.Probas[ctx0][0]])
	}

	// Use pre-computed nzCount to determine last non-zero zigzag position,
	// avoiding the backward scan that previously iterated up to 16 positions.
	last := nzCount - 1

	cost := 0
	ctx := ctx0
	for n := first; n < 16; n++ {
		band := dsp.VP8EncBands[n]
		pp := &proba.Bands[ctxType][band].Probas[ctx]

		v := int(coeffs[KZigzag[n]])
		if v < 0 {
			v = -v
		}

		if n > last {
			cost += int(ecost[pp[0]]) // EOB
			break
		}

		// Not EOB.
		cost += int(ecost[255-pp[0]])

		if v == 0 {
			cost += int(ecost[pp[1]]) // zero
			ctx = 0
		} else {
			cost += int(ecost[255-pp[1]]) // non-zero
			if v == 1 {
				cost += int(dsp.VP8LevelFixedCosts[1]) + int(ecost[pp[2]])
				ctx = 1
			} else if v == 2 {
				cost += int(dsp.VP8LevelFixedCosts[2]) + int(ecost[255-pp[2]]) + int(ecost[pp[3]]) + int(ecost[pp[4]])
				ctx = 2
			} else {
				cost += int(dsp.VP8LevelFixedCosts[v]) + variableLevelCost(v, pp)
				ctx = 2
			}
		}
	}

	return cost
}

// VP8LevelCodes encodes levels 1..67 in the VP8 coefficient coding tree.
// Each entry is {pattern, bits} where pattern marks which probas are used
// and bits contains the actual bit values.
// From libwebp cost_enc.c MAX_VARIABLE_LEVEL = 67.
var vp8LevelCodes = [67][2]uint16{
	{0x001, 0x000}, {0x007, 0x001}, {0x00f, 0x005}, {0x00f, 0x00d},
	{0x033, 0x003}, {0x033, 0x003}, {0x033, 0x023}, {0x033, 0x023},
	{0x033, 0x023}, {0x033, 0x023}, {0x0d3, 0x013}, {0x0d3, 0x013},
	{0x0d3, 0x013}, {0x0d3, 0x013}, {0x0d3, 0x013}, {0x0d3, 0x013},
	{0x0d3, 0x013}, {0x0d3, 0x013}, {0x0d3, 0x093}, {0x0d3, 0x093},
	{0x0d3, 0x093}, {0x0d3, 0x093}, {0x0d3, 0x093}, {0x0d3, 0x093},
	{0x0d3, 0x093}, {0x0d3, 0x093}, {0x0d3, 0x093}, {0x0d3, 0x093},
	{0x0d3, 0x093}, {0x0d3, 0x093}, {0x0d3, 0x093}, {0x0d3, 0x093},
	{0x0d3, 0x093}, {0x0d3, 0x093}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053}, {0x153, 0x053},
	{0x153, 0x053}, {0x153, 0x053}, {0x153, 0x153},
}

// variableLevelCost computes the probability-dependent part of the level cost.
// Matches libwebp's VariableLevelCost exactly.
func variableLevelCost(level int, probas *[NumProbas]uint8) int {
	idx := level - 1
	if idx >= len(vp8LevelCodes) {
		idx = len(vp8LevelCodes) - 1
	}
	pattern := int(vp8LevelCodes[idx][0])
	bits := int(vp8LevelCodes[idx][1])
	cost := 0
	for i := 2; pattern != 0; i++ {
		if pattern&1 != 0 {
			cost += dsp.VP8BitCost(bits&1, probas[i])
		}
		bits >>= 1
		pattern >>= 1
	}
	return cost
}

// levelCost computes the bit cost of encoding a coefficient level using
// the probability context. This codes the level tree AFTER the zero/non-zero
// decision has already been made: p[2] = one-or-more decision.
// Includes sign bit (256) and all variable-level tree costs.
func levelCost(level int, probas *[NumProbas]uint8) int {
	if level <= 0 {
		return 0
	}
	// The full level cost = sign bit + VariableLevelCost (probability-dependent tree)
	// VP8LevelFixedCosts contains the fixed (non-probability) part.
	// The total matches: VP8LevelFixedCosts[level] + variableLevelCost + sign(256 already in FixedCosts)
	// Actually VP8LevelFixedCosts already includes sign bit, and VariableLevelCost is
	// the prob-dependent tree. Together they form the full level tree cost.
	return int(dsp.VP8LevelFixedCosts[level]) + variableLevelCost(level, probas)
}

// --- SSE (distortion) helpers ---

// ComputeSSE4x4 computes the sum of squared errors for a 4x4 block.
func ComputeSSE4x4(src, ref []byte) int {
	return dsp.SSE4x4Direct(src, ref)
}

// ComputeSSE16x16 computes the sum of squared errors for a 16x16 block.
func ComputeSSE16x16(src, ref []byte) int {
	return dsp.SSE16x16Direct(src, ref)
}
