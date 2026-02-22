package dsp

import (
	"math/rand"
	"testing"
)

// SIMD conformance tests: verify dispatched (potentially assembly) implementations
// produce identical results to the pure Go reference implementations.

// makeRandBuf creates a random buffer with the given size seeded by rng.
func makeRandBuf(rng *rand.Rand, size int) []byte {
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(rng.Intn(256))
	}
	return buf
}

// copyBuf returns a copy of the buffer.
func copyBuf(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// ---------- SSE metric conformance ----------

func TestSSE4x4Conformance(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 1000; iter++ {
		pix := makeRandBuf(rng, 4*BPS)
		ref := makeRandBuf(rng, 4*BPS)
		goResult := sse4x4(pix, ref)
		dispResult := SSE4x4(pix, ref)
		if goResult != dispResult {
			t.Fatalf("iter %d: Go=%d dispatch=%d", iter, goResult, dispResult)
		}
	}
}

func TestSSE16x16Conformance(t *testing.T) {
	rng := rand.New(rand.NewSource(43))
	for iter := 0; iter < 200; iter++ {
		pix := makeRandBuf(rng, 16*BPS)
		ref := makeRandBuf(rng, 16*BPS)
		goResult := sse16x16(pix, ref)
		dispResult := SSE16x16(pix, ref)
		if goResult != dispResult {
			t.Fatalf("iter %d: Go=%d dispatch=%d", iter, goResult, dispResult)
		}
	}
}

// ---------- WHT conformance ----------

func TestFTransformWHTConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(44))
	for iter := 0; iter < 1000; iter++ {
		in := make([]int16, 16)
		for i := range in {
			in[i] = int16(rng.Intn(512) - 256)
		}
		goOut := make([]int16, 16)
		dispOut := make([]int16, 16)
		fTransformWHT(in, goOut)
		FTransformWHT(in, dispOut)
		for i := range goOut {
			if goOut[i] != dispOut[i] {
				t.Fatalf("iter %d, index %d: Go=%d dispatch=%d", iter, i, goOut[i], dispOut[i])
			}
		}
	}
}

func TestTransformWHTConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(45))
	for iter := 0; iter < 1000; iter++ {
		in := make([]int16, 16)
		for i := range in {
			in[i] = int16(rng.Intn(512) - 256)
		}
		// Output is 16 values at stride-16 positions (offsets 0,32,64,...,480 bytes = 0,16,32,...,240 int16)
		goOut := make([]int16, 256)
		dispOut := make([]int16, 256)
		transformWHT(in, goOut)
		TransformWHT(in, dispOut)
		// Check at stride-16 positions
		for i := 0; i < 16; i++ {
			if goOut[i*16] != dispOut[i*16] {
				t.Fatalf("iter %d, out[%d*16]: Go=%d dispatch=%d", iter, i, goOut[i*16], dispOut[i*16])
			}
		}
	}
}

// ---------- 16x16 Prediction conformance ----------

// makePredBuf16 creates a buffer for 16x16 prediction tests with random
// reference pixels (top row, left column, top-left corner).
func makePredBuf16(rng *rand.Rand) ([]byte, int) {
	// Need space for top row at off-BPS and left column at off-1+j*BPS.
	// off = BPS + 1 gives room for top-left at off-BPS-1 = 0.
	size := (17) * BPS + 1
	buf := make([]byte, size)
	off := BPS + 1

	// Fill top-left corner
	buf[off-BPS-1] = byte(rng.Intn(256))
	// Fill top row
	for i := 0; i < 16; i++ {
		buf[off-BPS+i] = byte(rng.Intn(256))
	}
	// Fill left column
	for j := 0; j < 16; j++ {
		buf[off-1+j*BPS] = byte(rng.Intn(256))
	}
	return buf, off
}

func testPred16Conformance(t *testing.T, name string, goFn PredFunc, dispIdx int) {
	t.Helper()
	rng := rand.New(rand.NewSource(50 + int64(dispIdx)))
	for iter := 0; iter < 500; iter++ {
		buf1, off := makePredBuf16(rng)
		buf2 := copyBuf(buf1)
		goFn(buf1, off)
		PredLuma16[dispIdx](buf2, off)
		for j := 0; j < 16; j++ {
			for i := 0; i < 16; i++ {
				idx := off + j*BPS + i
				if buf1[idx] != buf2[idx] {
					t.Fatalf("%s iter %d: pixel[%d,%d] Go=%d dispatch=%d", name, iter, j, i, buf1[idx], buf2[idx])
				}
			}
		}
	}
}

func TestDC16Conformance(t *testing.T)  { testPred16Conformance(t, "dc16", dc16, 0) }
func TestTM16Conformance(t *testing.T)  { testPred16Conformance(t, "tm16", tm16, 1) }
func TestVE16Conformance(t *testing.T)  { testPred16Conformance(t, "ve16", ve16, 2) }
func TestHE16Conformance(t *testing.T)  { testPred16Conformance(t, "he16", he16, 3) }

// ---------- 8x8 Chroma prediction conformance ----------

// makePredBuf8 creates a buffer for 8x8 prediction tests.
func makePredBuf8(rng *rand.Rand) ([]byte, int) {
	size := (9) * BPS + 1
	buf := make([]byte, size)
	off := BPS + 1

	buf[off-BPS-1] = byte(rng.Intn(256))
	for i := 0; i < 8; i++ {
		buf[off-BPS+i] = byte(rng.Intn(256))
	}
	for j := 0; j < 8; j++ {
		buf[off-1+j*BPS] = byte(rng.Intn(256))
	}
	return buf, off
}

func testPred8Conformance(t *testing.T, name string, goFn PredFunc, dispIdx int) {
	t.Helper()
	rng := rand.New(rand.NewSource(60 + int64(dispIdx)))
	for iter := 0; iter < 500; iter++ {
		buf1, off := makePredBuf8(rng)
		buf2 := copyBuf(buf1)
		goFn(buf1, off)
		PredChroma8[dispIdx](buf2, off)
		for j := 0; j < 8; j++ {
			for i := 0; i < 8; i++ {
				idx := off + j*BPS + i
				if buf1[idx] != buf2[idx] {
					t.Fatalf("%s iter %d: pixel[%d,%d] Go=%d dispatch=%d", name, iter, j, i, buf1[idx], buf2[idx])
				}
			}
		}
	}
}

func TestDC8uvConformance(t *testing.T)  { testPred8Conformance(t, "dc8uv", dc8uv, 0) }
func TestTM8uvConformance(t *testing.T)  { testPred8Conformance(t, "tm8uv", tm8uv, 1) }
func TestVE8uvConformance(t *testing.T)  { testPred8Conformance(t, "ve8uv", ve8uv, 2) }
func TestHE8uvConformance(t *testing.T)  { testPred8Conformance(t, "he8uv", he8uv, 3) }

// ---------- Lossless color transforms conformance ----------

func TestAddGreenConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(70))
	for iter := 0; iter < 500; iter++ {
		n := rng.Intn(64) + 1
		pixels := make([]uint32, n)
		for i := range pixels {
			pixels[i] = rng.Uint32()
		}
		ref := make([]uint32, n)
		copy(ref, pixels)

		addGreenToBlueAndRedGo(ref, n)
		AddGreenToBlueAndRed(pixels, n)

		for i := range pixels {
			if pixels[i] != ref[i] {
				t.Fatalf("iter %d, pixel %d: Go=0x%08x dispatch=0x%08x", iter, i, ref[i], pixels[i])
			}
		}
	}
}

func TestSubtractGreenConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(71))
	for iter := 0; iter < 500; iter++ {
		n := rng.Intn(64) + 1
		pixels := make([]uint32, n)
		for i := range pixels {
			pixels[i] = rng.Uint32()
		}
		ref := make([]uint32, n)
		copy(ref, pixels)

		subtractGreenGo(ref, n)
		SubtractGreen(pixels, n)

		for i := range pixels {
			if pixels[i] != ref[i] {
				t.Fatalf("iter %d, pixel %d: Go=0x%08x dispatch=0x%08x", iter, i, ref[i], pixels[i])
			}
		}
	}
}

// ---------- Lossless round-trip test ----------

func TestGreenRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(72))
	for iter := 0; iter < 200; iter++ {
		n := rng.Intn(64) + 1
		original := make([]uint32, n)
		for i := range original {
			original[i] = rng.Uint32()
		}
		pixels := make([]uint32, n)
		copy(pixels, original)

		SubtractGreen(pixels, n)
		AddGreenToBlueAndRed(pixels, n)

		for i := range pixels {
			if pixels[i] != original[i] {
				t.Fatalf("iter %d, pixel %d: original=0x%08x roundtrip=0x%08x", iter, i, original[i], pixels[i])
			}
		}
	}
}

// ---------- SSE Direct function conformance ----------

func TestSSE4x4DirectConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(80))
	for iter := 0; iter < 500; iter++ {
		pix := makeRandBuf(rng, 4*BPS)
		ref := makeRandBuf(rng, 4*BPS)
		goResult := sse4x4(pix, ref)
		directResult := SSE4x4Direct(pix, ref)
		if goResult != directResult {
			t.Fatalf("iter %d: Go=%d Direct=%d", iter, goResult, directResult)
		}
	}
}

func TestSSE16x16DirectConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(81))
	for iter := 0; iter < 200; iter++ {
		pix := makeRandBuf(rng, 16*BPS)
		ref := makeRandBuf(rng, 16*BPS)
		goResult := sse16x16(pix, ref)
		directResult := SSE16x16Direct(pix, ref)
		if goResult != directResult {
			t.Fatalf("iter %d: Go=%d Direct=%d", iter, goResult, directResult)
		}
	}
}

// ---------- Benchmarks ----------

func BenchmarkSSE4x4Go(b *testing.B) {
	pix := makeRandBuf(rand.New(rand.NewSource(90)), 4*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(91)), 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sse4x4(pix, ref)
	}
}

func BenchmarkSSE4x4Dispatch(b *testing.B) {
	pix := makeRandBuf(rand.New(rand.NewSource(90)), 4*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(91)), 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SSE4x4(pix, ref)
	}
}

func BenchmarkSSE16x16Go(b *testing.B) {
	pix := makeRandBuf(rand.New(rand.NewSource(92)), 16*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(93)), 16*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sse16x16(pix, ref)
	}
}

func BenchmarkSSE16x16Dispatch(b *testing.B) {
	pix := makeRandBuf(rand.New(rand.NewSource(92)), 16*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(93)), 16*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SSE16x16(pix, ref)
	}
}

func BenchmarkFTransformWHTGo(b *testing.B) {
	in := make([]int16, 16)
	out := make([]int16, 16)
	for i := range in {
		in[i] = int16(i*17 - 100)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fTransformWHT(in, out)
	}
}

func BenchmarkFTransformWHTDispatch(b *testing.B) {
	in := make([]int16, 16)
	out := make([]int16, 16)
	for i := range in {
		in[i] = int16(i*17 - 100)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FTransformWHT(in, out)
	}
}

func BenchmarkAddGreenGo(b *testing.B) {
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addGreenToBlueAndRedGo(pixels, len(pixels))
	}
}

func BenchmarkAddGreenDispatch(b *testing.B) {
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AddGreenToBlueAndRed(pixels, len(pixels))
	}
}

func BenchmarkDC16Go(b *testing.B) {
	rng := rand.New(rand.NewSource(100))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dc16(buf, off)
	}
}

func BenchmarkDC16Dispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(100))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredLuma16[0](buf, off)
	}
}

func BenchmarkVE16Go(b *testing.B) {
	rng := rand.New(rand.NewSource(101))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ve16(buf, off)
	}
}

func BenchmarkVE16Dispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(101))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredLuma16[2](buf, off)
	}
}

func BenchmarkTM16Go(b *testing.B) {
	rng := rand.New(rand.NewSource(102))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm16(buf, off)
	}
}

func BenchmarkTM16Dispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(102))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredLuma16[1](buf, off)
	}
}

func BenchmarkTM8uvGo(b *testing.B) {
	rng := rand.New(rand.NewSource(103))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tm8uv(buf, off)
	}
}

func BenchmarkTM8uvDispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(103))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredChroma8[1](buf, off)
	}
}

// ---------- FTransform / ITransform conformance ----------

func TestFTransformConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(200))
	for iter := 0; iter < 100; iter++ {
		src := makeRandBuf(rng, 4*BPS)
		ref := makeRandBuf(rng, 4*BPS)
		goOut := make([]int16, 16)
		dispOut := make([]int16, 16)
		FTransformDirect(src, ref, goOut)
		FTransform(src, ref, dispOut)
		for i := 0; i < 16; i++ {
			if goOut[i] != dispOut[i] {
				t.Fatalf("iter %d: FTransform mismatch at [%d]: go=%d dispatch=%d", iter, i, goOut[i], dispOut[i])
			}
		}
	}
}

func TestITransformConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(201))
	for iter := 0; iter < 100; iter++ {
		ref := makeRandBuf(rng, 4*BPS)
		in := make([]int16, 16)
		for i := range in {
			in[i] = int16(rng.Intn(2001) - 1000)
		}
		goDst := makeRandBuf(rng, 4*BPS)
		dispDst := copyBuf(goDst)
		dispRef := copyBuf(ref)
		inCopy := make([]int16, 16)
		copy(inCopy, in)
		ITransformDirect(ref, in, goDst, false)
		ITransform(dispRef, inCopy, dispDst, false)
		for r := 0; r < 4; r++ {
			for c := 0; c < 4; c++ {
				off := r*BPS + c
				if goDst[off] != dispDst[off] {
					t.Fatalf("iter %d: ITransform mismatch at (%d,%d): go=%d dispatch=%d", iter, r, c, goDst[off], dispDst[off])
				}
			}
		}
	}
}

func TestITransformDoTwoConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(202))
	for iter := 0; iter < 50; iter++ {
		ref := makeRandBuf(rng, 4*BPS)
		in := make([]int16, 32)
		for i := range in {
			in[i] = int16(rng.Intn(2001) - 1000)
		}
		goDst := makeRandBuf(rng, 4*BPS)
		dispDst := copyBuf(goDst)
		dispRef := copyBuf(ref)
		inCopy := make([]int16, 32)
		copy(inCopy, in)
		ITransformDirect(ref, in, goDst, true)
		ITransform(dispRef, inCopy, dispDst, true)
		for r := 0; r < 4; r++ {
			for c := 0; c < 8; c++ {
				off := r*BPS + c
				if goDst[off] != dispDst[off] {
					t.Fatalf("iter %d: ITransform(doTwo) mismatch at (%d,%d): go=%d dispatch=%d", iter, r, c, goDst[off], dispDst[off])
				}
			}
		}
	}
}

func BenchmarkFTransformPureGo(b *testing.B) {
	rng := rand.New(rand.NewSource(210))
	src := makeRandBuf(rng, 4*BPS)
	ref := makeRandBuf(rng, 4*BPS)
	out := make([]int16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fTransform(src, ref, out)
	}
}

func BenchmarkFTransformGo(b *testing.B) {
	rng := rand.New(rand.NewSource(210))
	src := makeRandBuf(rng, 4*BPS)
	ref := makeRandBuf(rng, 4*BPS)
	out := make([]int16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FTransformDirect(src, ref, out)
	}
}

func BenchmarkFTransformDispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(210))
	src := makeRandBuf(rng, 4*BPS)
	ref := makeRandBuf(rng, 4*BPS)
	out := make([]int16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FTransform(src, ref, out)
	}
}

func BenchmarkITransformPureGo(b *testing.B) {
	rng := rand.New(rand.NewSource(211))
	ref := makeRandBuf(rng, 4*BPS)
	in := make([]int16, 16)
	for i := range in {
		in[i] = int16(rng.Intn(2001) - 1000)
	}
	dst := make([]byte, 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iTransformOne(ref, in, dst)
	}
}

func BenchmarkITransformGo(b *testing.B) {
	rng := rand.New(rand.NewSource(211))
	ref := makeRandBuf(rng, 4*BPS)
	in := make([]int16, 16)
	for i := range in {
		in[i] = int16(rng.Intn(2001) - 1000)
	}
	dst := make([]byte, 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ITransformDirect(ref, in, dst, false)
	}
}

func BenchmarkITransformDispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(211))
	ref := makeRandBuf(rng, 4*BPS)
	in := make([]int16, 16)
	for i := range in {
		in[i] = int16(rng.Intn(2001) - 1000)
	}
	dst := make([]byte, 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(in, in) // prevent dead-code elimination
		ITransform(ref, in, dst, false)
	}
}

func BenchmarkTransformWHTGo(b *testing.B) {
	in := make([]int16, 16)
	out := make([]int16, 256)
	for i := range in {
		in[i] = int16(i*17 - 100)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transformWHT(in, out)
	}
}

func BenchmarkTransformWHTDispatch(b *testing.B) {
	in := make([]int16, 16)
	out := make([]int16, 256)
	for i := range in {
		in[i] = int16(i*17 - 100)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		TransformWHT(in, out)
	}
}

func BenchmarkHE16Go(b *testing.B) {
	rng := rand.New(rand.NewSource(104))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		he16(buf, off)
	}
}

func BenchmarkHE16Dispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(104))
	buf, off := makePredBuf16(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredLuma16[3](buf, off)
	}
}

func BenchmarkDC8uvGo(b *testing.B) {
	rng := rand.New(rand.NewSource(105))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dc8uv(buf, off)
	}
}

func BenchmarkDC8uvDispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(105))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredChroma8[0](buf, off)
	}
}

func BenchmarkVE8uvGo(b *testing.B) {
	rng := rand.New(rand.NewSource(106))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ve8uv(buf, off)
	}
}

func BenchmarkVE8uvDispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(106))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredChroma8[2](buf, off)
	}
}

func BenchmarkHE8uvGo(b *testing.B) {
	rng := rand.New(rand.NewSource(107))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		he8uv(buf, off)
	}
}

func BenchmarkHE8uvDispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(107))
	buf, off := makePredBuf8(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredChroma8[3](buf, off)
	}
}

func BenchmarkSubtractGreenGo(b *testing.B) {
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		subtractGreenGo(pixels, len(pixels))
	}
}

func BenchmarkSubtractGreenDispatch(b *testing.B) {
	pixels := make([]uint32, 256)
	for i := range pixels {
		pixels[i] = uint32(i * 0x01010101)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SubtractGreen(pixels, len(pixels))
	}
}

// ---------- TDisto4x4 conformance ----------

func TestTDisto4x4Conformance(t *testing.T) {
	rng := rand.New(rand.NewSource(400))
	for iter := 0; iter < 1000; iter++ {
		a := makeRandBuf(rng, 4*BPS)
		b := makeRandBuf(rng, 4*BPS)
		goResult := tDisto4x4Go(a, b)
		dispResult := TDisto4x4(a, b)
		if goResult != dispResult {
			t.Fatalf("iter %d: Go=%d dispatch=%d", iter, goResult, dispResult)
		}
	}
}

func TestTDisto16x16Conformance(t *testing.T) {
	rng := rand.New(rand.NewSource(401))
	for iter := 0; iter < 200; iter++ {
		a := makeRandBuf(rng, 16*BPS)
		b := makeRandBuf(rng, 16*BPS)
		goResult := tDisto16x16Go(a, b)
		dispResult := TDisto16x16(a, b)
		if goResult != dispResult {
			t.Fatalf("iter %d: Go=%d dispatch=%d", iter, goResult, dispResult)
		}
	}
}

func BenchmarkTDisto4x4Go(b *testing.B) {
	a := makeRandBuf(rand.New(rand.NewSource(410)), 4*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(411)), 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tDisto4x4Go(a, ref)
	}
}

func BenchmarkTDisto4x4Dispatch(b *testing.B) {
	a := makeRandBuf(rand.New(rand.NewSource(410)), 4*BPS)
	ref := makeRandBuf(rand.New(rand.NewSource(411)), 4*BPS)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		TDisto4x4(a, ref)
	}
}

// ---------- SimpleVFilter16 conformance ----------

// makeFilterBuf creates a random buffer for SimpleVFilter16 tests.
// Returns the buffer, base offset, and stride. The buffer has enough room
// for p1 (base-2*stride) through q1 (base+stride) rows of 16 bytes each.
func makeFilterBuf(rng *rand.Rand, stride int) ([]byte, int) {
	// Need at least 2*stride bytes before base for p1 row, and stride+16 after for q1.
	base := 2 * stride
	size := base + stride + 16
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(rng.Intn(256))
	}
	return buf, base
}

func TestSimpleVFilter16Conformance(t *testing.T) {
	rng := rand.New(rand.NewSource(300))
	for iter := 0; iter < 500; iter++ {
		stride := 16 + rng.Intn(64) // stride 16..79
		thresh := rng.Intn(128)     // thresh 0..127
		buf1, base := makeFilterBuf(rng, stride)
		buf2 := copyBuf(buf1)

		simpleVFilter16Go(buf1, base, stride, thresh)
		SimpleVFilter16(buf2, base, stride, thresh)

		for i := range buf1 {
			if buf1[i] != buf2[i] {
				t.Fatalf("iter %d (stride=%d, thresh=%d): byte[%d] Go=%d dispatch=%d",
					iter, stride, thresh, i, buf1[i], buf2[i])
			}
		}
	}
}

func TestSimpleVFilter16ConformanceHighThresh(t *testing.T) {
	// Test with high thresholds where thresh2 > 255 (exercises int16 path).
	rng := rand.New(rand.NewSource(301))
	for iter := 0; iter < 200; iter++ {
		stride := 16 + rng.Intn(64)
		thresh := 100 + rng.Intn(156) // thresh 100..255, thresh2 up to 511
		buf1, base := makeFilterBuf(rng, stride)
		buf2 := copyBuf(buf1)

		simpleVFilter16Go(buf1, base, stride, thresh)
		SimpleVFilter16(buf2, base, stride, thresh)

		for i := range buf1 {
			if buf1[i] != buf2[i] {
				t.Fatalf("iter %d (stride=%d, thresh=%d, thresh2=%d): byte[%d] Go=%d dispatch=%d",
					iter, stride, thresh, 2*thresh+1, i, buf1[i], buf2[i])
			}
		}
	}
}

func TestSimpleVFilter16ConformanceNoFilter(t *testing.T) {
	// With thresh=0, thresh2=1, almost nothing should be filtered.
	rng := rand.New(rand.NewSource(302))
	for iter := 0; iter < 100; iter++ {
		stride := 16 + rng.Intn(64)
		buf1, base := makeFilterBuf(rng, stride)
		buf2 := copyBuf(buf1)

		simpleVFilter16Go(buf1, base, stride, 0)
		SimpleVFilter16(buf2, base, stride, 0)

		for i := range buf1 {
			if buf1[i] != buf2[i] {
				t.Fatalf("iter %d: byte[%d] Go=%d dispatch=%d", iter, i, buf1[i], buf2[i])
			}
		}
	}
}

func BenchmarkSimpleVFilter16Go(b *testing.B) {
	rng := rand.New(rand.NewSource(310))
	stride := 32
	thresh := 40
	buf, base := makeFilterBuf(rng, stride)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		simpleVFilter16Go(buf, base, stride, thresh)
	}
}

func BenchmarkSimpleVFilter16Dispatch(b *testing.B) {
	rng := rand.New(rand.NewSource(310))
	stride := 32
	thresh := 40
	buf, base := makeFilterBuf(rng, stride)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SimpleVFilter16(buf, base, stride, thresh)
	}
}
