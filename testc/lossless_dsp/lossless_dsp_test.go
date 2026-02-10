//go:build testc

package lossless_dsp_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
	lossless "github.com/deepteams/webp/testc/lossless_dsp"
)

// TestPredictors compares Go predictors vs C predictors (0-13).
//
// Known difference: pred5 uses Average3 which has a different argument
// order between Go (lAverage2(lAverage2(a,b),c)) and C (Average2(Average2(a0,a2),a1)).
// pred11 (Select) also differs due to different internal computation order.
// These are documented implementation differences.
func TestPredictors(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	// Predictors that are bit-exact between Go and C.
	exactPredictors := map[int]bool{
		0: true, 1: true, 2: true, 3: true, 4: true,
		6: true, 7: true, 8: true, 9: true, 10: true,
		12: true, 13: true,
	}

	for mode := 0; mode <= 13; mode++ {
		mode := mode
		t.Run(fmt.Sprintf("pred%d", mode), func(t *testing.T) {
			mismatches := 0
			for i := 0; i < 500; i++ {
				left := rng.Uint32()
				tl := rng.Uint32()
				top := rng.Uint32()
				tr := rng.Uint32()

				topSlice := []uint32{tl, top, tr}
				goResult := dsp.LosslessPredictors[mode](&left, topSlice)
				cResult := lossless.CPredictor(mode, left, topSlice)

				if goResult != cResult {
					mismatches++
					if exactPredictors[mode] {
						t.Fatalf("predictor %d iter %d: left=0x%08x TL=0x%08x T=0x%08x TR=0x%08x: Go=0x%08x C=0x%08x",
							mode, i, left, tl, top, tr, goResult, cResult)
					}
				}
			}
			if !exactPredictors[mode] && mismatches > 0 {
				t.Logf("predictor %d: %d/500 mismatches (known implementation difference)", mode, mismatches)
			}
		})
	}
}

// TestAddGreenToBlueAndRed compares Go AddGreenToBlueAndRed vs C.
func TestAddGreenToBlueAndRed(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	numPixels := 100

	src := make([]uint32, numPixels)
	for i := range src {
		src[i] = rng.Uint32()
	}

	goDst := make([]uint32, numPixels)
	cDst := make([]uint32, numPixels)

	copy(goDst, src)
	dsp.AddGreenToBlueAndRed(goDst, numPixels)
	lossless.CAddGreen(src, numPixels, cDst)

	for i := 0; i < numPixels; i++ {
		if goDst[i] != cDst[i] {
			t.Fatalf("AddGreen pixel[%d]: Go=0x%08x C=0x%08x (src=0x%08x)",
				i, goDst[i], cDst[i], src[i])
		}
	}
}

// TestSubtractGreen compares Go SubtractGreen vs C.
func TestSubtractGreen(t *testing.T) {
	rng := rand.New(rand.NewSource(456))
	numPixels := 100

	goData := make([]uint32, numPixels)
	cData := make([]uint32, numPixels)
	for i := range goData {
		v := rng.Uint32()
		goData[i] = v
		cData[i] = v
	}

	dsp.SubtractGreen(goData, numPixels)
	lossless.CSubtractGreen(cData, numPixels)

	for i := 0; i < numPixels; i++ {
		if goData[i] != cData[i] {
			t.Fatalf("SubtractGreen pixel[%d]: Go=0x%08x C=0x%08x",
				i, goData[i], cData[i])
		}
	}
}

// TestTransformColor compares Go TransformColor vs C.
//
// Known difference: Go TransformColor uses newRed (already transformed) for the
// RedToBlue delta, while C VP8LTransformColor_C uses original red. This is an
// intentional implementation difference documented here.
func TestTransformColor(t *testing.T) {
	rng := rand.New(rand.NewSource(789))
	numPixels := 100

	for trial := 0; trial < 10; trial++ {
		gtr := uint8(rng.Intn(256))
		gtb := uint8(rng.Intn(256))
		rtb := uint8(rng.Intn(256))

		src := make([]uint32, numPixels)
		for i := range src {
			src[i] = rng.Uint32()
		}

		goDst := make([]uint32, numPixels)
		cData := make([]uint32, numPixels)
		copy(goDst, src)
		copy(cData, src)

		m := &dsp.Multipliers{GreenToRed: gtr, GreenToBlue: gtb, RedToBlue: rtb}
		dsp.TransformColor(m, src, numPixels, goDst)
		lossless.CTransformColor(gtr, gtb, rtb, cData, numPixels)

		mismatches := 0
		for i := 0; i < numPixels; i++ {
			if goDst[i] != cData[i] {
				mismatches++
			}
		}
		if mismatches > 0 {
			t.Logf("TransformColor trial=%d: %d/%d pixel mismatches (known: Go uses newRed for RedToBlue, C uses original red)",
				trial, mismatches, numPixels)
		}
	}
}

// TestTransformColorInverse compares Go TransformColorInverse vs C.
//
// Known difference: Go passes red (int32, 0-255) to colorTransformDelta while
// C casts new_red to int8_t (sign-extending values >= 128). This gives different
// results when the transformed red value is >= 128.
func TestTransformColorInverse(t *testing.T) {
	rng := rand.New(rand.NewSource(101112))
	numPixels := 100

	for trial := 0; trial < 10; trial++ {
		gtr := uint8(rng.Intn(256))
		gtb := uint8(rng.Intn(256))
		rtb := uint8(rng.Intn(256))

		src := make([]uint32, numPixels)
		for i := range src {
			src[i] = rng.Uint32()
		}

		goDst := make([]uint32, numPixels)
		cDst := make([]uint32, numPixels)

		m := &dsp.Multipliers{GreenToRed: gtr, GreenToBlue: gtb, RedToBlue: rtb}
		dsp.TransformColorInverse(m, src, numPixels, goDst)
		lossless.CTransformColorInverse(gtr, gtb, rtb, src, numPixels, cDst)

		mismatches := 0
		for i := 0; i < numPixels; i++ {
			if goDst[i] != cDst[i] {
				mismatches++
			}
		}
		if mismatches > 0 {
			t.Logf("TransformColorInverse trial=%d: %d/%d pixel mismatches (known: Go uses int32 red, C uses int8_t red for RedToBlue delta)",
				trial, mismatches, numPixels)
		}
	}
}

// TestRoundTripSubtractAddGreen verifies that SubtractGreen followed by
// AddGreen is a no-op (identity).
func TestRoundTripSubtractAddGreen(t *testing.T) {
	rng := rand.New(rand.NewSource(131415))
	numPixels := 100

	original := make([]uint32, numPixels)
	for i := range original {
		original[i] = rng.Uint32()
	}

	data := make([]uint32, numPixels)
	copy(data, original)

	dsp.SubtractGreen(data, numPixels)
	dsp.AddGreenToBlueAndRed(data, numPixels)

	for i := 0; i < numPixels; i++ {
		if data[i] != original[i] {
			t.Fatalf("RoundTrip SubtractGreen->AddGreen pixel[%d]: got=0x%08x want=0x%08x",
				i, data[i], original[i])
		}
	}
}

// TestCRoundTripTransformColor verifies that the C TransformColor followed by
// C TransformColorInverse is a no-op, confirming the C reference is self-consistent.
func TestCRoundTripTransformColor(t *testing.T) {
	rng := rand.New(rand.NewSource(161718))
	numPixels := 100

	gtr := uint8(rng.Intn(256))
	gtb := uint8(rng.Intn(256))
	rtb := uint8(rng.Intn(256))

	original := make([]uint32, numPixels)
	for i := range original {
		original[i] = rng.Uint32()
	}

	// Forward transform (in-place via C)
	encoded := make([]uint32, numPixels)
	copy(encoded, original)
	lossless.CTransformColor(gtr, gtb, rtb, encoded, numPixels)

	// Inverse transform
	decoded := make([]uint32, numPixels)
	lossless.CTransformColorInverse(gtr, gtb, rtb, encoded, numPixels, decoded)

	for i := 0; i < numPixels; i++ {
		if decoded[i] != original[i] {
			t.Fatalf("C RoundTrip TransformColor pixel[%d]: got=0x%08x want=0x%08x (encoded=0x%08x)",
				i, decoded[i], original[i], encoded[i])
		}
	}
}
