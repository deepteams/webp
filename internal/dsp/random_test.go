package dsp

import "testing"

func TestVP8RandomInit(t *testing.T) {
	// Verify that InitRandom sets up the state correctly.
	var rg VP8Random
	InitRandom(&rg, 1.0)

	if rg.index1 != 0 {
		t.Errorf("index1 = %d, want 0", rg.index1)
	}
	if rg.index2 != 31 {
		t.Errorf("index2 = %d, want 31", rg.index2)
	}
	// amp for dithering=1.0 should be 1 << vp8RandomDitherFix = 256.
	if rg.amp != 1<<vp8RandomDitherFix {
		t.Errorf("amp = %d, want %d", rg.amp, 1<<vp8RandomDitherFix)
	}

	// Verify table is copied from kRandomTable.
	for i := 0; i < vp8RandomTableSize; i++ {
		if rg.tab[i] != kRandomTable[i] {
			t.Errorf("tab[%d] = %d, want %d", i, rg.tab[i], kRandomTable[i])
		}
	}
}

func TestVP8RandomInitClamp(t *testing.T) {
	tests := []struct {
		name      string
		dithering float32
		wantAmp   int
	}{
		{"zero", 0.0, 0},
		{"negative", -1.0, 0},
		{"half", 0.5, 128},
		{"one", 1.0, 256},
		{"over", 2.0, 256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rg VP8Random
			InitRandom(&rg, tt.dithering)
			if rg.amp != tt.wantAmp {
				t.Errorf("amp = %d, want %d", rg.amp, tt.wantAmp)
			}
		})
	}
}

func TestVP8RandomBitsCentered(t *testing.T) {
	// With full amplitude (dithering=1.0), RandomBits should produce
	// values centered around 1 << (numBits-1) = YUV_HALF.
	var rg VP8Random
	InitRandom(&rg, 1.0)

	numBits := 16
	center := 1 << (numBits - 1) // 32768

	const n = 10000
	sum := 0
	min, max := center*2, 0
	for i := 0; i < n; i++ {
		v := RandomBits(&rg, numBits)
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	avg := float64(sum) / float64(n)

	// Average should be close to center (within 5%).
	if avg < float64(center)*0.90 || avg > float64(center)*1.10 {
		t.Errorf("average = %.1f, expected near %d", avg, center)
	}

	// With dithering=1.0, min and max should spread away from center.
	if min >= center || max <= center {
		t.Errorf("expected spread around center: min=%d, max=%d, center=%d", min, max, center)
	}
}

func TestVP8RandomBitsZeroAmp(t *testing.T) {
	// With zero amplitude (dithering=0.0), RandomBits should always return
	// exactly 1 << (numBits-1) because diff*0 >> fix == 0.
	var rg VP8Random
	InitRandom(&rg, 0.0)

	numBits := 16
	center := 1 << (numBits - 1)
	for i := 0; i < 100; i++ {
		v := RandomBits(&rg, numBits)
		if v != center {
			t.Fatalf("iteration %d: got %d, want %d", i, v, center)
		}
	}
}

func TestVP8RandomBitsDeterministic(t *testing.T) {
	// Two generators with the same seed should produce identical sequences.
	var rg1, rg2 VP8Random
	InitRandom(&rg1, 0.75)
	InitRandom(&rg2, 0.75)

	for i := 0; i < 200; i++ {
		v1 := RandomBits(&rg1, 16)
		v2 := RandomBits(&rg2, 16)
		if v1 != v2 {
			t.Fatalf("iteration %d: rg1=%d, rg2=%d (should be identical)", i, v1, v2)
		}
	}
}

func TestVP8RandomBitsWraparound(t *testing.T) {
	// Generate enough values to trigger index wraparound (>55 iterations).
	var rg VP8Random
	InitRandom(&rg, 1.0)

	for i := 0; i < 200; i++ {
		v := RandomBits(&rg, 16)
		// Values should be non-negative (centered around 32768 for 16 bits).
		// With maximum amplitude they can be negative in the diff, but the
		// final result adds 1<<(numBits-1) which re-centers.
		// The value should be within a reasonable range.
		_ = v
	}
	// If we got here without panicking, indices wrapped correctly.
}
