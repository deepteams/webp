//go:build testc

package transforms

import (
	"math/rand"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
)

func init() {
	dsp.Init()
}

// randomCoeffs generates 16 random int16 coefficients in [-2048, 2047].
func randomCoeffs(rng *rand.Rand) [16]int16 {
	var c [16]int16
	for i := range c {
		c[i] = int16(rng.Intn(4096) - 2048)
	}
	return c
}

// randomPixels generates a pixel buffer of size bytes with values in [0, 255].
func randomPixels(rng *rand.Rand, size int) []byte {
	buf := make([]byte, size)
	for i := range buf {
		buf[i] = byte(rng.Intn(256))
	}
	return buf
}

// TestTransformOne compares Go transformOne against C TransformOne_C.
func TestTransformOne(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000

	for trial := 0; trial < trials; trial++ {
		coeffs := randomCoeffs(rng)

		// Create identical dst buffers (4 rows x BPS stride, need 4*BPS bytes).
		baseDst := randomPixels(rng, 4*BPS)
		goDst := make([]byte, len(baseDst))
		cDst := make([]byte, len(baseDst))
		copy(goDst, baseDst)
		copy(cDst, baseDst)

		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		copy(goIn, coeffs[:])
		copy(cIn, coeffs[:])

		dsp.Transform(goIn, goDst, false)
		CTransformOne(cIn, cDst)

		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				off := j + i*BPS
				if goDst[off] != cDst[off] {
					t.Fatalf("trial %d: TransformOne mismatch at (%d,%d): Go=%d C=%d",
						trial, j, i, goDst[off], cDst[off])
				}
			}
		}
	}
	t.Logf("TransformOne: %d trials passed", trials)
}

// TestTransformDC compares Go transformDC against C TransformDC_C.
func TestTransformDC(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000

	for trial := 0; trial < trials; trial++ {
		dc := int16(rng.Intn(4096) - 2048)

		baseDst := randomPixels(rng, 4*BPS)
		goDst := make([]byte, len(baseDst))
		cDst := make([]byte, len(baseDst))
		copy(goDst, baseDst)
		copy(cDst, baseDst)

		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		goIn[0] = dc
		cIn[0] = dc

		dsp.TransformDC(goIn, goDst)
		CTransformDC(cIn, cDst)

		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				off := j + i*BPS
				if goDst[off] != cDst[off] {
					t.Fatalf("trial %d: TransformDC mismatch at (%d,%d): Go=%d C=%d",
						trial, j, i, goDst[off], cDst[off])
				}
			}
		}
	}
	t.Logf("TransformDC: %d trials passed", trials)
}

// TestTransformAC3 compares Go transformAC3 against C TransformAC3_C.
func TestTransformAC3(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000

	for trial := 0; trial < trials; trial++ {
		baseDst := randomPixels(rng, 4*BPS)
		goDst := make([]byte, len(baseDst))
		cDst := make([]byte, len(baseDst))
		copy(goDst, baseDst)
		copy(cDst, baseDst)

		// AC3: only in[0], in[1], in[4] are non-zero
		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		goIn[0] = int16(rng.Intn(4096) - 2048)
		goIn[1] = int16(rng.Intn(4096) - 2048)
		goIn[4] = int16(rng.Intn(4096) - 2048)
		copy(cIn, goIn)

		dsp.TransformAC3(goIn, goDst)
		CTransformAC3(cIn, cDst)

		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				off := j + i*BPS
				if goDst[off] != cDst[off] {
					t.Fatalf("trial %d: TransformAC3 mismatch at (%d,%d): Go=%d C=%d",
						trial, j, i, goDst[off], cDst[off])
				}
			}
		}
	}
	t.Logf("TransformAC3: %d trials passed", trials)
}

// TestTransformWHT compares Go transformWHT against C TransformWHT_C.
func TestTransformWHT(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000

	for trial := 0; trial < trials; trial++ {
		coeffs := randomCoeffs(rng)

		// Go output: 256 int16s (16 blocks * 16 coeffs, writes at positions 0, 16, 32, 48, ...)
		goOut := make([]int16, 256)
		// C output: uses out += 64 per row, writes at out[0], out[16], out[32], out[48]
		// Total needed: 4 rows * 64 = 256 int16s
		cOut := make([]int16, 256)

		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		copy(goIn, coeffs[:])
		copy(cIn, coeffs[:])

		dsp.TransformWHT(goIn, goOut)
		CTransformWHT(cIn, cOut)

		// Compare the 16 DC values at their respective positions.
		// C writes: row 0 -> out[0], out[16], out[32], out[48], then out += 64
		//           row 1 -> out[64], out[80], out[96], out[112], etc.
		// Go writes: row 0 -> out[0], out[16], out[32], out[48]
		//            row 1 -> out[64], out[80], out[96], out[112], etc.
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				off := i*4*16 + j*16
				if goOut[off] != cOut[off] {
					t.Fatalf("trial %d: TransformWHT mismatch at (%d,%d) offset=%d: Go=%d C=%d",
						trial, j, i, off, goOut[off], cOut[off])
				}
			}
		}
	}
	t.Logf("TransformWHT: %d trials passed", trials)
}

// TestFTransform compares Go fTransform against C FTransform_C.
// Known difference: Go out[12] has extra "- b2i(a2 != 0)" term not in C.
// This causes off-by-one differences in coefficient [12..15].
// Coefficients [0..11] should match exactly.
func TestFTransform(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000
	const tolerance = 1

	var exact, offByOne, mismatch int

	for trial := 0; trial < trials; trial++ {
		src := randomPixels(rng, 4*BPS)
		ref := randomPixels(rng, 4*BPS)

		goOut := make([]int16, 16)
		cOut := make([]int16, 16)

		dsp.FTransform(src, ref, goOut)
		CFTransform(src, ref, cOut)

		for i := 0; i < 16; i++ {
			d := abs16(goOut[i] - cOut[i])
			if d == 0 {
				exact++
			} else if d <= tolerance {
				offByOne++
			} else {
				mismatch++
				if mismatch <= 3 {
					t.Errorf("trial %d: FTransform mismatch at [%d]: Go=%d C=%d diff=%d",
						trial, i, goOut[i], cOut[i], d)
				}
			}
		}
	}
	total := trials * 16
	t.Logf("FTransform: %d coefficients tested, exact=%d off-by-one=%d mismatch=%d",
		total, exact, offByOne, mismatch)
	if mismatch > 0 {
		t.Fatalf("FTransform: %d mismatches beyond tolerance=%d", mismatch, tolerance)
	}
}

// TestFTransformWHT compares Go fTransformWHT against C FTransformWHT_C.
// Note: The Go version reads from a flat 4x4 array (stride 4) while the C
// version reads from a coefficient buffer with stride 64 (in += 64 per row,
// reading at offsets 0*16, 1*16, 2*16, 3*16 within each row).
// Known difference: Go adds +1 rounding before >>1 in the vertical pass,
// while C does not, causing off-by-one differences.
func TestFTransformWHT(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000
	const tolerance = 1

	var exact, offByOne, mismatch int

	for trial := 0; trial < trials; trial++ {
		// Go input: flat 4x4 = 16 int16s
		goIn := make([]int16, 16)
		for i := range goIn {
			goIn[i] = int16(rng.Intn(4096) - 2048)
		}

		// C input: coefficient buffer with stride 64 per row,
		// reading at offsets 0*16, 1*16, 2*16, 3*16 within each row.
		// Need 4 rows * 64 = 256 int16s.
		cIn := make([]int16, 256)
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				cIn[i*64+j*16] = goIn[i*4+j]
			}
		}

		goOut := make([]int16, 16)
		cOut := make([]int16, 16)

		dsp.FTransformWHT(goIn, goOut)
		CFTransformWHT(cIn, cOut)

		for i := 0; i < 16; i++ {
			d := abs16(goOut[i] - cOut[i])
			if d == 0 {
				exact++
			} else if d <= tolerance {
				offByOne++
			} else {
				mismatch++
				if mismatch <= 3 {
					t.Errorf("trial %d: FTransformWHT mismatch at [%d]: Go=%d C=%d diff=%d",
						trial, i, goOut[i], cOut[i], d)
				}
			}
		}
	}
	total := trials * 16
	t.Logf("FTransformWHT: %d coefficients tested, exact=%d off-by-one=%d mismatch=%d",
		total, exact, offByOne, mismatch)
	if mismatch > 0 {
		t.Fatalf("FTransformWHT: %d mismatches beyond tolerance=%d", mismatch, tolerance)
	}
}

// TestITransform compares Go iTransform against C ITransform_C.
func TestITransform(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000

	for trial := 0; trial < trials; trial++ {
		ref := randomPixels(rng, 4*BPS)
		coeffs := randomCoeffs(rng)

		goDst := make([]byte, 4*BPS)
		cDst := make([]byte, 4*BPS)

		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		copy(goIn, coeffs[:])
		copy(cIn, coeffs[:])

		dsp.ITransform(ref, goIn, goDst, false)
		CITransform(ref, cIn, cDst, false)

		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				off := j + i*BPS
				if goDst[off] != cDst[off] {
					t.Fatalf("trial %d: ITransform mismatch at (%d,%d): Go=%d C=%d",
						trial, j, i, goDst[off], cDst[off])
				}
			}
		}
	}
	t.Logf("ITransform: %d trials passed", trials)
}

// TestITransformTwo compares Go iTransform (doTwo=true) against C ITransform_C.
func TestITransformTwo(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const trials = 1000

	for trial := 0; trial < trials; trial++ {
		ref := randomPixels(rng, 4*BPS)

		goIn := make([]int16, 32) // two blocks of 16
		cIn := make([]int16, 32)
		for i := range goIn {
			goIn[i] = int16(rng.Intn(4096) - 2048)
		}
		copy(cIn, goIn)

		goDst := make([]byte, 4*BPS)
		cDst := make([]byte, 4*BPS)

		dsp.ITransform(ref, goIn, goDst, true)
		CITransform(ref, cIn, cDst, true)

		// Check both 4x4 blocks (side by side, so columns 0-3 and 4-7)
		for i := 0; i < 4; i++ {
			for j := 0; j < 8; j++ {
				off := j + i*BPS
				if goDst[off] != cDst[off] {
					t.Fatalf("trial %d: ITransformTwo mismatch at (%d,%d): Go=%d C=%d",
						trial, j, i, goDst[off], cDst[off])
				}
			}
		}
	}
	t.Logf("ITransformTwo: %d trials passed", trials)
}

// TestEdgeCases tests with specific edge case inputs.
func TestEdgeCases(t *testing.T) {
	// All zeros
	t.Run("ZeroCoeffs", func(t *testing.T) {
		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		baseDst := make([]byte, 4*BPS)
		for i := range baseDst {
			baseDst[i] = 128
		}
		goDst := make([]byte, len(baseDst))
		cDst := make([]byte, len(baseDst))
		copy(goDst, baseDst)
		copy(cDst, baseDst)

		dsp.Transform(goIn, goDst, false)
		CTransformOne(cIn, cDst)

		for i := 0; i < 4*BPS; i++ {
			if goDst[i] != cDst[i] {
				t.Fatalf("ZeroCoeffs mismatch at byte %d: Go=%d C=%d", i, goDst[i], cDst[i])
			}
		}
	})

	// Max positive coefficients
	t.Run("MaxCoeffs", func(t *testing.T) {
		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		for i := range goIn {
			goIn[i] = 2047
			cIn[i] = 2047
		}
		goDst := make([]byte, 4*BPS)
		cDst := make([]byte, 4*BPS)

		dsp.Transform(goIn, goDst, false)
		CTransformOne(cIn, cDst)

		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				off := j + i*BPS
				if goDst[off] != cDst[off] {
					t.Fatalf("MaxCoeffs mismatch at (%d,%d): Go=%d C=%d", j, i, goDst[off], cDst[off])
				}
			}
		}
	})

	// Max negative coefficients
	t.Run("MinCoeffs", func(t *testing.T) {
		goIn := make([]int16, 16)
		cIn := make([]int16, 16)
		for i := range goIn {
			goIn[i] = -2048
			cIn[i] = -2048
		}
		baseDst := make([]byte, 4*BPS)
		for i := range baseDst {
			baseDst[i] = 255
		}
		goDst := make([]byte, len(baseDst))
		cDst := make([]byte, len(baseDst))
		copy(goDst, baseDst)
		copy(cDst, baseDst)

		dsp.Transform(goIn, goDst, false)
		CTransformOne(cIn, cDst)

		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				off := j + i*BPS
				if goDst[off] != cDst[off] {
					t.Fatalf("MinCoeffs mismatch at (%d,%d): Go=%d C=%d", j, i, goDst[off], cDst[off])
				}
			}
		}
	})

	// DC-only with various DC values
	t.Run("DCOnlyValues", func(t *testing.T) {
		for _, dc := range []int16{-2048, -1024, -1, 0, 1, 1024, 2047} {
			goIn := make([]int16, 16)
			cIn := make([]int16, 16)
			goIn[0] = dc
			cIn[0] = dc

			baseDst := make([]byte, 4*BPS)
			for i := range baseDst {
				baseDst[i] = 128
			}
			goDst := make([]byte, len(baseDst))
			cDst := make([]byte, len(baseDst))
			copy(goDst, baseDst)
			copy(cDst, baseDst)

			dsp.TransformDC(goIn, goDst)
			CTransformDC(cIn, cDst)

			for i := 0; i < 4; i++ {
				for j := 0; j < 4; j++ {
					off := j + i*BPS
					if goDst[off] != cDst[off] {
						t.Fatalf("DCOnly dc=%d mismatch at (%d,%d): Go=%d C=%d", dc, j, i, goDst[off], cDst[off])
					}
				}
			}
		}
	})
}

func abs16(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}
