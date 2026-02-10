package dsp

// Zigzag is the zig-zag scan order for a 4x4 block of DCT coefficients.
// Maps linear index [0..15] to the 2D position in row-major order.
var Zigzag = [16]int{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}

// Quantize quantizes the coefficients in by the given quantization multiplier.
// out[i] = in[i] / quant, with rounding.
func Quantize(in []int16, out []int16, quant int) {
	if quant == 0 {
		quant = 1
	}
	for i := 0; i < 16 && i < len(in) && i < len(out); i++ {
		v := int(in[i])
		sign := 1
		if v < 0 {
			sign = -1
			v = -v
		}
		out[i] = int16(sign * ((v + quant/2) / quant))
	}
}

// Dequantize multiplies each coefficient by the quantization factor.
// out[i] = in[i] * dequant.
func Dequantize(in []int16, out []int16, dequant int) {
	for i := 0; i < 16 && i < len(in) && i < len(out); i++ {
		out[i] = int16(int(in[i]) * dequant)
	}
}

// QuantizeBlock quantizes a 4x4 block in zig-zag order. Returns the number
// of trailing zero coefficients (the "last" non-zero index + 1).
func QuantizeBlock(in, out []int16, quant int) int {
	if quant == 0 {
		quant = 1
	}
	last := -1
	for n := 0; n < 16; n++ {
		j := Zigzag[n]
		v := int(in[j])
		sign := 1
		if v < 0 {
			sign = -1
			v = -v
		}
		coeff := int16(sign * ((v + quant/2) / quant))
		out[n] = coeff
		if coeff != 0 {
			last = n
		}
	}
	return last + 1
}

// DequantizeBlock dequantizes a 4x4 block from zig-zag order back to raster
// order. This is the inverse of QuantizeBlock: out[Zigzag[n]] = in[n] * dequant.
func DequantizeBlock(in, out []int16, dequant int) {
	for n := 0; n < 16; n++ {
		out[Zigzag[n]] = int16(int(in[n]) * dequant)
	}
}
