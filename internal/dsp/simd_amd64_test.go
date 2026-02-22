//go:build amd64

package dsp

import (
	"math/rand"
	"testing"
)

// ---------- AVX2 conformance tests ----------
// These verify AVX2 implementations produce bit-exact results vs SSE2.
// On non-AVX2 machines, the tests are skipped.

func TestSSE16x16AVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(500))
	for iter := 0; iter < 200; iter++ {
		pix := makeRandBuf(rng, 16*BPS)
		ref := makeRandBuf(rng, 16*BPS)
		sse2Result := sse16x16SSE2(pix, ref)
		avx2Result := sse16x16AVX2(pix, ref)
		if sse2Result != avx2Result {
			t.Fatalf("iter %d: SSE2=%d AVX2=%d", iter, sse2Result, avx2Result)
		}
	}
}

func TestAddGreenAVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(501))
	for iter := 0; iter < 500; iter++ {
		n := rng.Intn(64) + 1
		sse2Pix := make([]uint32, n)
		for i := range sse2Pix {
			sse2Pix[i] = rng.Uint32()
		}
		avx2Pix := make([]uint32, n)
		copy(avx2Pix, sse2Pix)

		addGreenToBlueAndRedSSE2(sse2Pix, n)
		addGreenToBlueAndRedAVX2(avx2Pix, n)

		for i := range sse2Pix {
			if sse2Pix[i] != avx2Pix[i] {
				t.Fatalf("iter %d, pixel %d: SSE2=0x%08x AVX2=0x%08x", iter, i, sse2Pix[i], avx2Pix[i])
			}
		}
	}
}

func TestSubtractGreenAVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(502))
	for iter := 0; iter < 500; iter++ {
		n := rng.Intn(64) + 1
		sse2Pix := make([]uint32, n)
		for i := range sse2Pix {
			sse2Pix[i] = rng.Uint32()
		}
		avx2Pix := make([]uint32, n)
		copy(avx2Pix, sse2Pix)

		subtractGreenSSE2(sse2Pix, n)
		subtractGreenAVX2(avx2Pix, n)

		for i := range sse2Pix {
			if sse2Pix[i] != avx2Pix[i] {
				t.Fatalf("iter %d, pixel %d: SSE2=0x%08x AVX2=0x%08x", iter, i, sse2Pix[i], avx2Pix[i])
			}
		}
	}
}

func TestSimpleVFilter16AVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(503))
	for iter := 0; iter < 500; iter++ {
		stride := 16 + rng.Intn(64)
		thresh := rng.Intn(256)
		buf1, base := makeFilterBuf(rng, stride)
		buf2 := copyBuf(buf1)

		simpleVFilter16SSE2(buf1, base, stride, thresh)
		simpleVFilter16AVX2(buf2, base, stride, thresh)

		for i := range buf1 {
			if buf1[i] != buf2[i] {
				t.Fatalf("iter %d (stride=%d, thresh=%d): byte[%d] SSE2=%d AVX2=%d",
					iter, stride, thresh, i, buf1[i], buf2[i])
			}
		}
	}
}

func TestYUVPackedToNRGBABatchAVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(504))
	for iter := 0; iter < 200; iter++ {
		// Width must be multiple of 8 for AVX2.
		width := (rng.Intn(32) + 1) * 8

		y := makeRandBuf(rng, width)
		packedUV := make([]uint32, width)
		for i := range packedUV {
			// UV values packed as (V << 16) | U, both 0-255.
			u := uint32(rng.Intn(256))
			v := uint32(rng.Intn(256))
			packedUV[i] = (v << 16) | u
		}

		sse2Dst := make([]byte, width*4)
		avx2Dst := make([]byte, width*4)

		yuvPackedToNRGBABatchSSE2(y, packedUV, sse2Dst, width)
		yuvPackedToNRGBABatchAVX2(y, packedUV, avx2Dst, width)

		for i := range sse2Dst {
			if sse2Dst[i] != avx2Dst[i] {
				px := i / 4
				ch := i % 4
				t.Fatalf("iter %d, pixel %d channel %d: SSE2=%d AVX2=%d",
					iter, px, ch, sse2Dst[i], avx2Dst[i])
			}
		}
	}
}

// ---------- AVX2 benchmarks ----------

func BenchmarkSSE16x16AVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	pix := makeRandBuf(rand.New(rand.NewSource(92)), 16*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(93)), 16*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sse16x16AVX2(pix, ref)
	}
}

func BenchmarkSSE16x16SSE2(b *testing.B) {
	pix := makeRandBuf(rand.New(rand.NewSource(92)), 16*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(93)), 16*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sse16x16SSE2(pix, ref)
	}
}

func BenchmarkAddGreenAVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addGreenToBlueAndRedAVX2(pixels, len(pixels))
	}
}

func BenchmarkAddGreenSSE2(b *testing.B) {
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addGreenToBlueAndRedSSE2(pixels, len(pixels))
	}
}

func BenchmarkSubtractGreenAVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		subtractGreenAVX2(pixels, len(pixels))
	}
}

func BenchmarkSubtractGreenSSE2(b *testing.B) {
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		subtractGreenSSE2(pixels, len(pixels))
	}
}

func BenchmarkSimpleVFilter16AVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(310))
	stride := 32
	thresh := 40
	buf, base := makeFilterBuf(rng, stride)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		simpleVFilter16AVX2(buf, base, stride, thresh)
	}
}

func BenchmarkSimpleVFilter16SSE2(b *testing.B) {
	rng := rand.New(rand.NewSource(310))
	stride := 32
	thresh := 40
	buf, base := makeFilterBuf(rng, stride)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		simpleVFilter16SSE2(buf, base, stride, thresh)
	}
}

func BenchmarkYUVBatchAVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(520))
	width := 256
	y := makeRandBuf(rng, width)
	uv := make([]uint32, width)
	for i := range uv {
		uv[i] = (uint32(rng.Intn(256)) << 16) | uint32(rng.Intn(256))
	}
	dst := make([]byte, width*4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		yuvPackedToNRGBABatchAVX2(y, uv, dst, width)
	}
}

func BenchmarkYUVBatchSSE2(b *testing.B) {
	rng := rand.New(rand.NewSource(520))
	width := 256
	y := makeRandBuf(rng, width)
	uv := make([]uint32, width)
	for i := range uv {
		uv[i] = (uint32(rng.Intn(256)) << 16) | uint32(rng.Intn(256))
	}
	dst := make([]byte, width*4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		yuvPackedToNRGBABatchSSE2(y, uv, dst, width)
	}
}

// ---------- AVX2 transform conformance ----------

func TestFTransformAVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(600))
	for iter := 0; iter < 1000; iter++ {
		src := makeRandBuf(rng, 4*BPS)
		ref := makeRandBuf(rng, 4*BPS)
		sse2Out := make([]int16, 16)
		avx2Out := make([]int16, 16)
		fTransformSSE2(src, ref, sse2Out)
		fTransformAVX2(src, ref, avx2Out)
		for i := 0; i < 16; i++ {
			if sse2Out[i] != avx2Out[i] {
				t.Fatalf("iter %d: FTransform mismatch at [%d]: SSE2=%d AVX2=%d", iter, i, sse2Out[i], avx2Out[i])
			}
		}
	}
}

func TestITransformAVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(601))
	for iter := 0; iter < 1000; iter++ {
		ref := makeRandBuf(rng, 4*BPS)
		in := make([]int16, 16)
		for i := range in {
			in[i] = int16(rng.Intn(2001) - 1000)
		}
		sse2Dst := makeRandBuf(rng, 4*BPS)
		avx2Dst := copyBuf(sse2Dst)
		sse2Ref := copyBuf(ref)
		avx2Ref := copyBuf(ref)
		sse2In := make([]int16, 16)
		avx2In := make([]int16, 16)
		copy(sse2In, in)
		copy(avx2In, in)
		iTransformOneSSE2(sse2Ref, sse2In, sse2Dst)
		iTransformOneAVX2(avx2Ref, avx2In, avx2Dst)
		for r := 0; r < 4; r++ {
			for c := 0; c < 4; c++ {
				off := r*BPS + c
				if sse2Dst[off] != avx2Dst[off] {
					t.Fatalf("iter %d: ITransform mismatch at (%d,%d): SSE2=%d AVX2=%d", iter, r, c, sse2Dst[off], avx2Dst[off])
				}
			}
		}
	}
}

func TestTDisto4x4AVX2Conformance(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(602))
	for iter := 0; iter < 1000; iter++ {
		a := makeRandBuf(rng, 4*BPS)
		b := makeRandBuf(rng, 4*BPS)
		sse2Result := tDisto4x4SSE2(a, b)
		avx2Result := tDisto4x4AVX2(a, b)
		if sse2Result != avx2Result {
			t.Fatalf("iter %d: TDisto4x4 SSE2=%d AVX2=%d", iter, sse2Result, avx2Result)
		}
	}
}

// ---------- AVX2 transform benchmarks ----------

func BenchmarkFTransformAVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(210))
	src := makeRandBuf(rng, 4*BPS)
	ref := makeRandBuf(rng, 4*BPS)
	out := make([]int16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fTransformAVX2(src, ref, out)
	}
}

func BenchmarkFTransformSSE2(b *testing.B) {
	rng := rand.New(rand.NewSource(210))
	src := makeRandBuf(rng, 4*BPS)
	ref := makeRandBuf(rng, 4*BPS)
	out := make([]int16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fTransformSSE2(src, ref, out)
	}
}

func BenchmarkITransformAVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	rng := rand.New(rand.NewSource(211))
	ref := makeRandBuf(rng, 4*BPS)
	in := make([]int16, 16)
	for i := range in {
		in[i] = int16(rng.Intn(2001) - 1000)
	}
	dst := make([]byte, 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iTransformOneAVX2(ref, in, dst)
	}
}

func BenchmarkITransformSSE2(b *testing.B) {
	rng := rand.New(rand.NewSource(211))
	ref := makeRandBuf(rng, 4*BPS)
	in := make([]int16, 16)
	for i := range in {
		in[i] = int16(rng.Intn(2001) - 1000)
	}
	dst := make([]byte, 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iTransformOneSSE2(ref, in, dst)
	}
}

func BenchmarkTDisto4x4AVX2(b *testing.B) {
	if !HasAVX2() {
		b.Skip("AVX2 not available")
	}
	a := makeRandBuf(rand.New(rand.NewSource(410)), 4*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(411)), 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tDisto4x4AVX2(a, ref)
	}
}

func BenchmarkTDisto4x4SSE2(b *testing.B) {
	a := makeRandBuf(rand.New(rand.NewSource(410)), 4*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(411)), 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tDisto4x4SSE2(a, ref)
	}
}
