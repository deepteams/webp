package lossless

import "testing"

// TestSelectPredictorEncoderMatchesDecoder is a regression test for issue #7.
//
// The Select predictor (mode 11) must make the SAME top-vs-left choice in the
// encoder (selectPred, used to compute forward residuals) and the decoder
// (selectPredictor, used to reconstruct). The encoder previously summed the
// negation of the spec formula, inverting the choice for many inputs and
// corrupting any tile the per-tile search assigned to mode 11.
//
// Sweeping a coarse grid over all four ARGB channels of left/top/topLeft gives
// broad coverage of the sign-of-sum decision boundary; the two functions must
// agree on every sample.
func TestSelectPredictorEncoderMatchesDecoder(t *testing.T) {
	vals := []uint32{0, 1, 7, 64, 127, 128, 200, 254, 255}
	mismatches := 0
	for _, lr := range vals {
		for _, lg := range vals {
			for _, tr := range vals {
				for _, tg := range vals {
					for _, cr := range vals {
						for _, cg := range vals {
							left := 0xff000000 | lr<<16 | lg<<8 | lr
							top := 0xff000000 | tr<<16 | tg<<8 | tg
							topLeft := 0xff000000 | cr<<16 | cg<<8 | cr
							if selectPred(left, top, topLeft) != selectPredictor(left, top, topLeft) {
								if mismatches < 5 {
									t.Errorf("mismatch: left=%08x top=%08x topLeft=%08x enc=%08x dec=%08x",
										left, top, topLeft,
										selectPred(left, top, topLeft),
										selectPredictor(left, top, topLeft))
								}
								mismatches++
							}
						}
					}
				}
			}
		}
	}
	if mismatches != 0 {
		t.Fatalf("selectPred disagrees with selectPredictor on %d input combinations", mismatches)
	}
}
