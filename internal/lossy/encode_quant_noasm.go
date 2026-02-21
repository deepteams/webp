//go:build !amd64

package lossy

// DequantCoeffs dequantizes a coefficient block using the segment quantizer.
// Coefficient 0 (DC) uses DCQuant, coefficients 1-15 (AC) use Quant.
func DequantCoeffs(in, out []int16, sq *SegmentQuant) {
	dequantCoeffsGo(in, out, sq)
}

// QuantizeCoeffs quantizes a 4x4 coefficient block and returns the zigzag-order
// nzCount. Uses the pure Go reference implementation.
func QuantizeCoeffs(in, out []int16, sq *SegmentQuant, firstCoeff int) int {
	return quantizeCoeffsGo(in, out, sq, firstCoeff)
}
