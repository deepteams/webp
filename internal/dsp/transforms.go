package dsp

// IDCT/FDCT transforms for VP8 lossy codec.
// Constants and algorithm match libwebp dec.c TransformOne_C and enc.c FTransform_C.

const (
	c1 = 20091 // cos(pi/8) * 2^16
	c2 = 35468 // sin(pi/8) * 2^16
)

// b2i returns 1 if cond is true, 0 otherwise.
func b2i(cond bool) int {
	if cond {
		return 1
	}
	return 0
}

// mul1 computes (a * C1 >> 16) + a, matching the MUL1 macro.
func mul1(a int) int {
	return ((a * c1) >> 16) + a
}

// mul2 computes a * C2 >> 16, matching the MUL2 macro.
func mul2(a int) int {
	return (a * c2) >> 16
}

// store clips (dst[off] + (x >> 3)) to [0,255] and writes it back.
func store(dst []byte, off, x int) {
	dst[off] = Clip8b(int(dst[off]) + (x >> 3))
}

// transformOne performs a single 4x4 inverse DCT (TransformOne_C from dec.c).
// in contains 16 coefficients, dst is the output with stride BPS.
// Loops manually unrolled for performance (same pattern as iTransformOne).
func transformOne(in []int16, dst []byte) {
	_ = in[15]
	_ = dst[3+3*BPS]

	var tmp [4 * 4]int

	// Vertical pass — column 0.
	{
		a := int(in[0]) + int(in[8])
		b := int(in[0]) - int(in[8])
		cc := mul2(int(in[4])) - mul1(int(in[12]))
		d := mul1(int(in[4])) + mul2(int(in[12]))
		tmp[0] = a + d
		tmp[4] = b + cc
		tmp[8] = b - cc
		tmp[12] = a - d
	}
	// Column 1.
	{
		a := int(in[1]) + int(in[9])
		b := int(in[1]) - int(in[9])
		cc := mul2(int(in[5])) - mul1(int(in[13]))
		d := mul1(int(in[5])) + mul2(int(in[13]))
		tmp[1] = a + d
		tmp[5] = b + cc
		tmp[9] = b - cc
		tmp[13] = a - d
	}
	// Column 2.
	{
		a := int(in[2]) + int(in[10])
		b := int(in[2]) - int(in[10])
		cc := mul2(int(in[6])) - mul1(int(in[14]))
		d := mul1(int(in[6])) + mul2(int(in[14]))
		tmp[2] = a + d
		tmp[6] = b + cc
		tmp[10] = b - cc
		tmp[14] = a - d
	}
	// Column 3.
	{
		a := int(in[3]) + int(in[11])
		b := int(in[3]) - int(in[11])
		cc := mul2(int(in[7])) - mul1(int(in[15]))
		d := mul1(int(in[7])) + mul2(int(in[15]))
		tmp[3] = a + d
		tmp[7] = b + cc
		tmp[11] = b - cc
		tmp[15] = a - d
	}

	// Horizontal pass — row 0.
	{
		dc := tmp[0] + 4
		a := dc + tmp[2]
		b := dc - tmp[2]
		cc := mul2(tmp[1]) - mul1(tmp[3])
		d := mul1(tmp[1]) + mul2(tmp[3])
		store(dst, 0, a+d)
		store(dst, 1, b+cc)
		store(dst, 2, b-cc)
		store(dst, 3, a-d)
	}
	// Row 1.
	{
		dc := tmp[4] + 4
		a := dc + tmp[6]
		b := dc - tmp[6]
		cc := mul2(tmp[5]) - mul1(tmp[7])
		d := mul1(tmp[5]) + mul2(tmp[7])
		store(dst, 0+BPS, a+d)
		store(dst, 1+BPS, b+cc)
		store(dst, 2+BPS, b-cc)
		store(dst, 3+BPS, a-d)
	}
	// Row 2.
	{
		dc := tmp[8] + 4
		a := dc + tmp[10]
		b := dc - tmp[10]
		cc := mul2(tmp[9]) - mul1(tmp[11])
		d := mul1(tmp[9]) + mul2(tmp[11])
		store(dst, 0+2*BPS, a+d)
		store(dst, 1+2*BPS, b+cc)
		store(dst, 2+2*BPS, b-cc)
		store(dst, 3+2*BPS, a-d)
	}
	// Row 3.
	{
		dc := tmp[12] + 4
		a := dc + tmp[14]
		b := dc - tmp[14]
		cc := mul2(tmp[13]) - mul1(tmp[15])
		d := mul1(tmp[13]) + mul2(tmp[15])
		store(dst, 0+3*BPS, a+d)
		store(dst, 1+3*BPS, b+cc)
		store(dst, 2+3*BPS, b-cc)
		store(dst, 3+3*BPS, a-d)
	}
}

// transformTwo applies one or two 4x4 IDCTs side by side (TransformTwo_C).
func transformTwo(in []int16, dst []byte, doTwo bool) {
	transformOne(in, dst)
	if doTwo {
		transformOne(in[16:], dst[4:])
	}
}

// transformDC applies a DC-only inverse transform (all AC coefficients zero).
// Manually unrolled for performance.
func transformDC(in []int16, dst []byte) {
	dc := int(in[0]) + 4
	store(dst, 0, dc)
	store(dst, 1, dc)
	store(dst, 2, dc)
	store(dst, 3, dc)
	store(dst, 0+BPS, dc)
	store(dst, 1+BPS, dc)
	store(dst, 2+BPS, dc)
	store(dst, 3+BPS, dc)
	store(dst, 0+2*BPS, dc)
	store(dst, 1+2*BPS, dc)
	store(dst, 2+2*BPS, dc)
	store(dst, 3+2*BPS, dc)
	store(dst, 0+3*BPS, dc)
	store(dst, 1+3*BPS, dc)
	store(dst, 2+3*BPS, dc)
	store(dst, 3+3*BPS, dc)
}

// transformAC3 applies the inverse transform when only the first 3 coefficients
// are non-zero (indices 0, 1, 4 in scan order).
func transformAC3(in []int16, dst []byte) {
	a := int(in[0]) + 4
	c4 := mul2(int(in[4]))
	d4 := mul1(int(in[4]))
	c1v := mul2(int(in[1]))
	d1v := mul1(int(in[1]))

	store(dst, 0+0*BPS, a+d4+d1v)
	store(dst, 1+0*BPS, a+d4+c1v)
	store(dst, 2+0*BPS, a+d4-c1v)
	store(dst, 3+0*BPS, a+d4-d1v)
	store(dst, 0+1*BPS, a+c4+d1v)
	store(dst, 1+1*BPS, a+c4+c1v)
	store(dst, 2+1*BPS, a+c4-c1v)
	store(dst, 3+1*BPS, a+c4-d1v)
	store(dst, 0+2*BPS, a-c4+d1v)
	store(dst, 1+2*BPS, a-c4+c1v)
	store(dst, 2+2*BPS, a-c4-c1v)
	store(dst, 3+2*BPS, a-c4-d1v)
	store(dst, 0+3*BPS, a-d4+d1v)
	store(dst, 1+3*BPS, a-d4+c1v)
	store(dst, 2+3*BPS, a-d4-c1v)
	store(dst, 3+3*BPS, a-d4-d1v)
}

// transformUV applies two DC IDCTs for the U and V 4x4 blocks that sit at
// offsets 0 and 4*BPS in the destination.
func transformUV(in []int16, dst []byte) {
	transformTwo(in[0:], dst[0:], true)
	transformTwo(in[32:], dst[4*BPS:], true)
}

// transformDCUV applies DC-only IDCT for both chroma blocks.
func transformDCUV(in []int16, dst []byte) {
	if in[0] != 0 {
		transformDC(in[0:], dst[0:])
	}
	if in[16] != 0 {
		transformDC(in[16:], dst[4:])
	}
	if in[32] != 0 {
		transformDC(in[32:], dst[4*BPS:])
	}
	if in[48] != 0 {
		transformDC(in[48:], dst[4*BPS+4:])
	}
}

// transformWHT performs the inverse Walsh-Hadamard Transform (TransformWHT_C).
// in has 16 int16 coefficients, out receives 16 DC values stored at positions
// {0, 16, 32, 48, 64, 80, ...} (i.e. stride 16 between consecutive DC values
// matching the coefficient buffer layout where each 4x4 block occupies 16 int16s).
// out must have at least 16*16 = 256 elements.
func transformWHT(in []int16, out []int16) {
	var tmp [16]int

	// Vertical pass.
	for i := 0; i < 4; i++ {
		a0 := int(in[0+i]) + int(in[12+i])
		a1 := int(in[4+i]) + int(in[8+i])
		a2 := int(in[4+i]) - int(in[8+i])
		a3 := int(in[0+i]) - int(in[12+i])
		tmp[0+i] = a0 + a1
		tmp[8+i] = a0 - a1
		tmp[4+i] = a3 + a2
		tmp[12+i] = a3 - a2
	}

	// Horizontal pass: each row i writes to 4 block DCs.
	// C code uses out += 64 per row (4 blocks * 16 coeffs each).
	for i := 0; i < 4; i++ {
		dc := tmp[i*4+0] + 3 // rounding
		a0 := dc + tmp[i*4+3]
		a1 := tmp[i*4+1] + tmp[i*4+2]
		a2 := tmp[i*4+1] - tmp[i*4+2]
		a3 := dc - tmp[i*4+3]
		base := i * 4 * 16 // row of 4 blocks, each block = 16 int16
		out[base+0*16] = int16((a0 + a1) >> 3)
		out[base+1*16] = int16((a3 + a2) >> 3)
		out[base+2*16] = int16((a0 - a1) >> 3)
		out[base+3*16] = int16((a3 - a2) >> 3)
	}
}

// iTransform computes the inverse DCT for the encoder (ITransform_C from enc.c).
// ref is the reference block, in contains the coefficients, dst is the output.
func iTransform(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransformOne(ref, in, dst)
	if doTwo {
		iTransformOne(ref[4:], in[16:], dst[4:])
	}
}

// iTransformOne performs a single 4x4 IDCT for the encoder path.
// Loops are manually unrolled for performance.
func iTransformOne(ref []byte, in []int16, dst []byte) {
	// BCE hints: prove to compiler that all accesses are in-bounds.
	_ = in[15]
	_ = ref[3+3*BPS]
	_ = dst[3+3*BPS]

	var tmp [4 * 4]int

	// Vertical pass — column 0.
	{
		a := int(in[0]) + int(in[8])
		b := int(in[0]) - int(in[8])
		cc := mul2(int(in[4])) - mul1(int(in[12]))
		d := mul1(int(in[4])) + mul2(int(in[12]))
		tmp[0] = a + d
		tmp[4] = b + cc
		tmp[8] = b - cc
		tmp[12] = a - d
	}
	// Column 1.
	{
		a := int(in[1]) + int(in[9])
		b := int(in[1]) - int(in[9])
		cc := mul2(int(in[5])) - mul1(int(in[13]))
		d := mul1(int(in[5])) + mul2(int(in[13]))
		tmp[1] = a + d
		tmp[5] = b + cc
		tmp[9] = b - cc
		tmp[13] = a - d
	}
	// Column 2.
	{
		a := int(in[2]) + int(in[10])
		b := int(in[2]) - int(in[10])
		cc := mul2(int(in[6])) - mul1(int(in[14]))
		d := mul1(int(in[6])) + mul2(int(in[14]))
		tmp[2] = a + d
		tmp[6] = b + cc
		tmp[10] = b - cc
		tmp[14] = a - d
	}
	// Column 3.
	{
		a := int(in[3]) + int(in[11])
		b := int(in[3]) - int(in[11])
		cc := mul2(int(in[7])) - mul1(int(in[15]))
		d := mul1(int(in[7])) + mul2(int(in[15]))
		tmp[3] = a + d
		tmp[7] = b + cc
		tmp[11] = b - cc
		tmp[15] = a - d
	}

	// Horizontal pass — row 0.
	{
		dc := tmp[0] + 4
		a := dc + tmp[2]
		b := dc - tmp[2]
		cc := mul2(tmp[1]) - mul1(tmp[3])
		d := mul1(tmp[1]) + mul2(tmp[3])
		dst[0] = Clip8b(int(ref[0]) + ((a + d) >> 3))
		dst[1] = Clip8b(int(ref[1]) + ((b + cc) >> 3))
		dst[2] = Clip8b(int(ref[2]) + ((b - cc) >> 3))
		dst[3] = Clip8b(int(ref[3]) + ((a - d) >> 3))
	}
	// Row 1.
	{
		dc := tmp[4] + 4
		a := dc + tmp[6]
		b := dc - tmp[6]
		cc := mul2(tmp[5]) - mul1(tmp[7])
		d := mul1(tmp[5]) + mul2(tmp[7])
		dst[0+BPS] = Clip8b(int(ref[0+BPS]) + ((a + d) >> 3))
		dst[1+BPS] = Clip8b(int(ref[1+BPS]) + ((b + cc) >> 3))
		dst[2+BPS] = Clip8b(int(ref[2+BPS]) + ((b - cc) >> 3))
		dst[3+BPS] = Clip8b(int(ref[3+BPS]) + ((a - d) >> 3))
	}
	// Row 2.
	{
		dc := tmp[8] + 4
		a := dc + tmp[10]
		b := dc - tmp[10]
		cc := mul2(tmp[9]) - mul1(tmp[11])
		d := mul1(tmp[9]) + mul2(tmp[11])
		dst[0+2*BPS] = Clip8b(int(ref[0+2*BPS]) + ((a + d) >> 3))
		dst[1+2*BPS] = Clip8b(int(ref[1+2*BPS]) + ((b + cc) >> 3))
		dst[2+2*BPS] = Clip8b(int(ref[2+2*BPS]) + ((b - cc) >> 3))
		dst[3+2*BPS] = Clip8b(int(ref[3+2*BPS]) + ((a - d) >> 3))
	}
	// Row 3.
	{
		dc := tmp[12] + 4
		a := dc + tmp[14]
		b := dc - tmp[14]
		cc := mul2(tmp[13]) - mul1(tmp[15])
		d := mul1(tmp[13]) + mul2(tmp[15])
		dst[0+3*BPS] = Clip8b(int(ref[0+3*BPS]) + ((a + d) >> 3))
		dst[1+3*BPS] = Clip8b(int(ref[1+3*BPS]) + ((b + cc) >> 3))
		dst[2+3*BPS] = Clip8b(int(ref[2+3*BPS]) + ((b - cc) >> 3))
		dst[3+3*BPS] = Clip8b(int(ref[3+3*BPS]) + ((a - d) >> 3))
	}
}

// fTransform computes the forward DCT (FTransform_C from enc.c).
// src and ref are 4x4 blocks with stride BPS; out receives 16 coefficients.
// Loops are manually unrolled for performance.
func fTransform(src, ref []byte, out []int16) {
	// BCE hints: prove to compiler that all accesses are in-bounds.
	_ = src[3+3*BPS]
	_ = ref[3+3*BPS]
	_ = out[15]

	var tmp [16]int

	// Horizontal pass — row 0.
	{
		d0 := int(src[0]) - int(ref[0])
		d1 := int(src[1]) - int(ref[1])
		d2 := int(src[2]) - int(ref[2])
		d3 := int(src[3]) - int(ref[3])
		a0 := d0 + d3
		a1 := d1 + d2
		a2 := d1 - d2
		a3 := d0 - d3
		tmp[0] = (a0 + a1) * 8
		tmp[1] = (a2*2217 + a3*5352 + 1812) >> 9
		tmp[2] = (a0 - a1) * 8
		tmp[3] = (a3*2217 - a2*5352 + 937) >> 9
	}
	// Row 1.
	{
		d0 := int(src[0+BPS]) - int(ref[0+BPS])
		d1 := int(src[1+BPS]) - int(ref[1+BPS])
		d2 := int(src[2+BPS]) - int(ref[2+BPS])
		d3 := int(src[3+BPS]) - int(ref[3+BPS])
		a0 := d0 + d3
		a1 := d1 + d2
		a2 := d1 - d2
		a3 := d0 - d3
		tmp[4] = (a0 + a1) * 8
		tmp[5] = (a2*2217 + a3*5352 + 1812) >> 9
		tmp[6] = (a0 - a1) * 8
		tmp[7] = (a3*2217 - a2*5352 + 937) >> 9
	}
	// Row 2.
	{
		d0 := int(src[0+2*BPS]) - int(ref[0+2*BPS])
		d1 := int(src[1+2*BPS]) - int(ref[1+2*BPS])
		d2 := int(src[2+2*BPS]) - int(ref[2+2*BPS])
		d3 := int(src[3+2*BPS]) - int(ref[3+2*BPS])
		a0 := d0 + d3
		a1 := d1 + d2
		a2 := d1 - d2
		a3 := d0 - d3
		tmp[8] = (a0 + a1) * 8
		tmp[9] = (a2*2217 + a3*5352 + 1812) >> 9
		tmp[10] = (a0 - a1) * 8
		tmp[11] = (a3*2217 - a2*5352 + 937) >> 9
	}
	// Row 3.
	{
		d0 := int(src[0+3*BPS]) - int(ref[0+3*BPS])
		d1 := int(src[1+3*BPS]) - int(ref[1+3*BPS])
		d2 := int(src[2+3*BPS]) - int(ref[2+3*BPS])
		d3 := int(src[3+3*BPS]) - int(ref[3+3*BPS])
		a0 := d0 + d3
		a1 := d1 + d2
		a2 := d1 - d2
		a3 := d0 - d3
		tmp[12] = (a0 + a1) * 8
		tmp[13] = (a2*2217 + a3*5352 + 1812) >> 9
		tmp[14] = (a0 - a1) * 8
		tmp[15] = (a3*2217 - a2*5352 + 937) >> 9
	}

	// Vertical pass — column 0.
	{
		a0 := tmp[0] + tmp[12]
		a1 := tmp[4] + tmp[8]
		a2 := tmp[4] - tmp[8]
		a3 := tmp[0] - tmp[12]
		out[0] = int16((a0 + a1 + 7) >> 4)
		out[4] = int16((a2*2217+a3*5352+12000)>>16 + b2i(a3 != 0))
		out[8] = int16((a0 - a1 + 7) >> 4)
		out[12] = int16((a3*2217 - a2*5352 + 51000) >> 16)
	}
	// Column 1.
	{
		a0 := tmp[1] + tmp[13]
		a1 := tmp[5] + tmp[9]
		a2 := tmp[5] - tmp[9]
		a3 := tmp[1] - tmp[13]
		out[1] = int16((a0 + a1 + 7) >> 4)
		out[5] = int16((a2*2217+a3*5352+12000)>>16 + b2i(a3 != 0))
		out[9] = int16((a0 - a1 + 7) >> 4)
		out[13] = int16((a3*2217 - a2*5352 + 51000) >> 16)
	}
	// Column 2.
	{
		a0 := tmp[2] + tmp[14]
		a1 := tmp[6] + tmp[10]
		a2 := tmp[6] - tmp[10]
		a3 := tmp[2] - tmp[14]
		out[2] = int16((a0 + a1 + 7) >> 4)
		out[6] = int16((a2*2217+a3*5352+12000)>>16 + b2i(a3 != 0))
		out[10] = int16((a0 - a1 + 7) >> 4)
		out[14] = int16((a3*2217 - a2*5352 + 51000) >> 16)
	}
	// Column 3.
	{
		a0 := tmp[3] + tmp[15]
		a1 := tmp[7] + tmp[11]
		a2 := tmp[7] - tmp[11]
		a3 := tmp[3] - tmp[15]
		out[3] = int16((a0 + a1 + 7) >> 4)
		out[7] = int16((a2*2217+a3*5352+12000)>>16 + b2i(a3 != 0))
		out[11] = int16((a0 - a1 + 7) >> 4)
		out[15] = int16((a3*2217 - a2*5352 + 51000) >> 16)
	}
}

// fTransform2 applies fTransform to two side-by-side 4x4 blocks.
func fTransform2(src, ref []byte, out []int16) {
	fTransform(src, ref, out)
	fTransform(src[4:], ref[4:], out[16:])
}

// fTransformWHT computes the forward Walsh-Hadamard Transform on a flat 4x4
// array of DC coefficients (stride 4). This corresponds to libwebp's
// FTransformWHT_C adapted for a pre-extracted flat DC array rather than the
// full coefficient buffer with stride 64.
//
// The flat input maps row r, col c -> in[r*4+c]. The C code iterates over
// rows in the first pass (in += 64 per row, reading columns via in[c*16]).
// We replicate the same operations using flat indexing: in[i*4+c].
func fTransformWHT(in []int16, out []int16) {
	var tmp [16]int

	// First pass (row-wise, matching C first pass).
	// C iterates over rows i=0..3, for each row reads columns 0..3.
	// C: in[i*64 + c*16] -> flat: in[i*4 + c]
	for i := 0; i < 4; i++ {
		a0 := int(in[i*4+0]) + int(in[i*4+2])
		a1 := int(in[i*4+1]) + int(in[i*4+3])
		a2 := int(in[i*4+1]) - int(in[i*4+3])
		a3 := int(in[i*4+0]) - int(in[i*4+2])
		tmp[0+i*4] = a0 + a1
		tmp[1+i*4] = a3 + a2
		tmp[2+i*4] = a3 - a2
		tmp[3+i*4] = a0 - a1
	}

	// Second pass (column-wise, matching C second pass).
	for i := 0; i < 4; i++ {
		a0 := tmp[0+i] + tmp[8+i]
		a1 := tmp[4+i] + tmp[12+i]
		a2 := tmp[4+i] - tmp[12+i]
		a3 := tmp[0+i] - tmp[8+i]
		b0 := a0 + a1
		b1 := a3 + a2
		b2 := a3 - a2
		b3 := a0 - a1
		out[0+i] = int16(b0 >> 1)
		out[4+i] = int16(b1 >> 1)
		out[8+i] = int16(b2 >> 1)
		out[12+i] = int16(b3 >> 1)
	}
}
