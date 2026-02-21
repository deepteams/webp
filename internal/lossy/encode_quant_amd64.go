//go:build amd64

package lossy

import "github.com/deepteams/webp/internal/dsp"

//go:noescape
func dequantCoeffsSSE2(in, out []int16, q, dcq int)

// DequantCoeffs dequantizes a coefficient block using SSE2 assembly.
// Coefficient 0 (DC) uses DCQuant, coefficients 1-15 (AC) use Quant.
func DequantCoeffs(in, out []int16, sq *SegmentQuant) {
	dequantCoeffsSSE2(in, out, sq.Quant, sq.DCQuant)
}

//go:noescape
func quantizeACSSE2(in, out, sharpen []int16, iQuant, bias int)

//go:noescape
func nzCountACSSE2(out []int16) int

// QuantizeCoeffs quantizes a 4x4 coefficient block using SSE2 for AC
// coefficients and scalar code for DC. Returns the zigzag-order nzCount.
func QuantizeCoeffs(in, out []int16, sq *SegmentQuant, firstCoeff int) int {
	// BCE hints: prove to compiler that all accesses are in-bounds.
	_ = in[15]
	_ = out[15]

	// Save DC value before SSE2 may overwrite it (when in == out).
	dc := in[0]

	// SIMD: quantize all 16 positions using AC params.
	// Position 0 gets wrong result (AC instead of DC params) but is fixed below.
	if dsp.HasAVX2() {
		quantizeACAVX2(in, out, sq.Sharpen[:], sq.IQuant, sq.Bias)
	} else {
		quantizeACSSE2(in, out, sq.Sharpen[:], sq.IQuant, sq.Bias)
	}

	maxZZ := -1

	// Handle DC coefficient (position 0) with scalar code using DC params.
	if firstCoeff == 0 {
		v := int(dc)
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

	// SSE2 scan of AC coefficients for zigzag nzCount.
	// Returns max zigzag position of non-zero ACs, or -1 if all zero.
	acMaxZZ := nzCountACSSE2(out)
	if acMaxZZ > maxZZ {
		maxZZ = acMaxZZ
	}
	return maxZZ + 1
}
