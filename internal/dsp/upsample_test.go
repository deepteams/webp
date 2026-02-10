package dsp

import (
	"testing"
)

// TestLoadUV verifies the UV packing matches the C macro LOAD_UV(u, v).
func TestLoadUV(t *testing.T) {
	tests := []struct {
		u, v byte
		want uint32
	}{
		{0, 0, 0x00000000},
		{128, 128, 0x00800080},
		{255, 0, 0x000000ff},
		{0, 255, 0x00ff0000},
		{100, 200, 0x00c80064},
	}
	for _, tt := range tests {
		got := loadUV(tt.u, tt.v)
		if got != tt.want {
			t.Errorf("loadUV(%d, %d) = 0x%08x, want 0x%08x", tt.u, tt.v, got, tt.want)
		}
	}
}

// TestDiamondKernelValues verifies the diamond 4-tap interpolation produces
// the correct values for a known 2x2 chroma block.
//
// Given chroma:
//   [a b]   =  [100 200]
//   [c d]      [150 250]
//
// The four interpolated sub-pixels should be:
//   top-left  = (9*a + 3*b + 3*c +   d + 8) / 16 = (900+600+450+250+8)/16 = 137
//   top-right = (3*a + 9*b +   c + 3*d + 8) / 16 = (300+1800+150+750+8)/16 = 188
//   bot-left  = (3*a +   b + 9*c + 3*d + 8) / 16 = (300+200+1350+750+8)/16 = 163
//   bot-right = (  a + 3*b + 3*c + 9*d + 8) / 16 = (100+600+450+2250+8)/16 = 213
func TestDiamondKernelValues(t *testing.T) {
	// We use a 2-pixel wide image (width=2) so the upsampler produces exactly
	// 2 luma columns from 1 pair of chroma samples.
	//
	// With width=2:
	//   lastPixelPair = (2-1)>>1 = 0
	//   So the interior loop runs 0 times.
	//   First pixel uses (3*tl + l + 2) >> 2 formula.
	//   Last pixel (even width) uses (3*tl + l + 2) >> 2 formula.
	//
	// For width=2, the upsampler actually uses only the edge formula, not the
	// diamond kernel. We need width >= 4 (at least 2 chroma samples) to exercise
	// the interior diamond kernel.

	// Set up a 4-pixel wide, 2-row image:
	// Chroma: topU = [100, 200], botU = [150, 250]
	//         topV = [100, 200], botV = [150, 250]
	// Luma: all 128 (arbitrary, we only verify chroma interpolation).

	width := 4
	topY := []byte{128, 128, 128, 128}
	botY := []byte{128, 128, 128, 128}
	topU := []byte{100, 200}
	topV := []byte{100, 200}
	botU := []byte{150, 250}
	botV := []byte{150, 250}

	topDst := make([]byte, width*3)
	botDst := make([]byte, width*3)

	UpsampleLinePair(topY, botY, topU, topV, botU, botV, topDst, botDst, width)

	// For the interior pair (x=1), the diamond kernel computes:
	//   tl_uv = LOAD_UV(100, 100), t_uv = LOAD_UV(200, 200)
	//   l_uv  = LOAD_UV(150, 150), uv   = LOAD_UV(250, 250)
	//
	// avg    = 100 + 200 + 150 + 250 + 8 = 708
	// diag12 = (708 + 2*(200+150)) / 8 = (708 + 700) / 8 = 1408/8 = 176
	// diag03 = (708 + 2*(100+250)) / 8 = (708 + 700) / 8 = 1408/8 = 176
	//
	// Wait -- for this particular symmetric case diag12 == diag03.
	// Let me use asymmetric values instead.

	// Use asymmetric chroma values to distinguish diag12 from diag03.
	topU2 := []byte{80, 160}
	topV2 := []byte{80, 160}
	botU2 := []byte{120, 240}
	botV2 := []byte{120, 240}

	topDst2 := make([]byte, width*3)
	botDst2 := make([]byte, width*3)

	UpsampleLinePair(topY, botY, topU2, topV2, botU2, botV2, topDst2, botDst2, width)

	// Interior pair x=1, the 4 chroma samples (U channel):
	//   tl=80, t=160, l=120, cur=240
	//
	// avg    = 80 + 160 + 120 + 240 + 8 = 608
	// diag12 = (608 + 2*(160 + 120)) >> 3 = (608 + 560) >> 3 = 1168 >> 3 = 146
	// diag03 = (608 + 2*(80 + 240)) >> 3  = (608 + 640) >> 3 = 1248 >> 3 = 156
	//
	// top-left (pixel 1):  uv0 = (diag12 + tl) >> 1 = (146 + 80) >> 1 = 113
	// top-right (pixel 2): uv1 = (diag03 + t) >> 1  = (156 + 160) >> 1 = 158
	// bot-left (pixel 1):  uv0 = (diag03 + l) >> 1  = (156 + 120) >> 1 = 138
	// bot-right (pixel 2): uv1 = (diag12 + cur) >> 1 = (146 + 240) >> 1 = 193

	// Expected diamond-interpolated U values at interior pixels 1 and 2.
	// (V is the same since topV == topU in this test.)
	expectedTopU1 := 113
	expectedTopU2 := 158
	expectedBotU1 := 138
	expectedBotU2 := 193

	// We cannot read U directly from the RGB output, but we can verify the
	// formulas are correct by checking the intermediate U values manually.
	// Instead, verify full equivalence by computing expected RGB from the
	// interpolated UV and comparing.

	// Verify top row pixel 1 (offset 1*3 = 3).
	verifyPixelUV(t, "top[1]", topDst2[3:6], 128, expectedTopU1, expectedTopU1)
	// Verify top row pixel 2 (offset 2*3 = 6).
	verifyPixelUV(t, "top[2]", topDst2[6:9], 128, expectedTopU2, expectedTopU2)
	// Verify bottom row pixel 1.
	verifyPixelUV(t, "bot[1]", botDst2[3:6], 128, expectedBotU1, expectedBotU1)
	// Verify bottom row pixel 2.
	verifyPixelUV(t, "bot[2]", botDst2[6:9], 128, expectedBotU2, expectedBotU2)
}

// verifyPixelUV checks that the RGB triple at dst matches the expected
// YUV-to-RGB conversion for the given Y, U, V values.
func verifyPixelUV(t *testing.T, label string, dst []byte, y, u, v int) {
	t.Helper()
	var expected [3]byte
	YUVToRGB(y, u, v, expected[:])
	if dst[0] != expected[0] || dst[1] != expected[1] || dst[2] != expected[2] {
		t.Errorf("%s: got RGB(%d,%d,%d), want RGB(%d,%d,%d) for Y=%d U=%d V=%d",
			label, dst[0], dst[1], dst[2], expected[0], expected[1], expected[2], y, u, v)
	}
}

// TestUpsampleLinePairSinglePixel verifies the edge case of width=1.
func TestUpsampleLinePairSinglePixel(t *testing.T) {
	topY := []byte{128}
	topU := []byte{128}
	topV := []byte{128}
	botU := []byte{128}
	botV := []byte{128}
	topDst := make([]byte, 3)

	UpsampleLinePair(topY, nil, topU, topV, botU, botV, topDst, nil, 1)

	// First pixel formula: (3*128 + 128 + 2) >> 2 = (512 + 2) >> 2 = 128
	var expected [3]byte
	YUVToRGB(128, 128, 128, expected[:])
	if topDst[0] != expected[0] || topDst[1] != expected[1] || topDst[2] != expected[2] {
		t.Errorf("width=1: got RGB(%d,%d,%d), want RGB(%d,%d,%d)",
			topDst[0], topDst[1], topDst[2], expected[0], expected[1], expected[2])
	}
}

// TestUpsampleLinePairNRGBA verifies the NRGBA variant produces the same
// chroma values as the RGB variant, with correct alpha.
func TestUpsampleLinePairNRGBA(t *testing.T) {
	width := 4
	topY := []byte{100, 120, 140, 160}
	botY := []byte{110, 130, 150, 170}
	topU := []byte{80, 160}
	topV := []byte{90, 170}
	botU := []byte{120, 200}
	botV := []byte{130, 210}

	// RGB output.
	topRGB := make([]byte, width*3)
	botRGB := make([]byte, width*3)
	UpsampleLinePair(topY, botY, topU, topV, botU, botV, topRGB, botRGB, width)

	// NRGBA output without alpha.
	topNRGBA := make([]byte, width*4)
	botNRGBA := make([]byte, width*4)
	UpsampleLinePairNRGBA(topY, botY, topU, topV, botU, botV, topNRGBA, botNRGBA, nil, nil, width)

	// Verify RGB channels match and alpha is 255.
	for x := 0; x < width; x++ {
		rgbOff := x * 3
		nrgbaOff := x * 4
		if topNRGBA[nrgbaOff] != topRGB[rgbOff] ||
			topNRGBA[nrgbaOff+1] != topRGB[rgbOff+1] ||
			topNRGBA[nrgbaOff+2] != topRGB[rgbOff+2] {
			t.Errorf("top[%d]: RGB mismatch: NRGBA=(%d,%d,%d) vs RGB=(%d,%d,%d)",
				x, topNRGBA[nrgbaOff], topNRGBA[nrgbaOff+1], topNRGBA[nrgbaOff+2],
				topRGB[rgbOff], topRGB[rgbOff+1], topRGB[rgbOff+2])
		}
		if topNRGBA[nrgbaOff+3] != 255 {
			t.Errorf("top[%d]: alpha=%d, want 255", x, topNRGBA[nrgbaOff+3])
		}

		if botNRGBA[nrgbaOff] != botRGB[rgbOff] ||
			botNRGBA[nrgbaOff+1] != botRGB[rgbOff+1] ||
			botNRGBA[nrgbaOff+2] != botRGB[rgbOff+2] {
			t.Errorf("bot[%d]: RGB mismatch: NRGBA=(%d,%d,%d) vs RGB=(%d,%d,%d)",
				x, botNRGBA[nrgbaOff], botNRGBA[nrgbaOff+1], botNRGBA[nrgbaOff+2],
				botRGB[rgbOff], botRGB[rgbOff+1], botRGB[rgbOff+2])
		}
		if botNRGBA[nrgbaOff+3] != 255 {
			t.Errorf("bot[%d]: alpha=%d, want 255", x, botNRGBA[nrgbaOff+3])
		}
	}

	// Test with alpha plane.
	alphaTop := []byte{200, 201, 202, 203}
	alphaBot := []byte{210, 211, 212, 213}
	topNRGBA2 := make([]byte, width*4)
	botNRGBA2 := make([]byte, width*4)
	UpsampleLinePairNRGBA(topY, botY, topU, topV, botU, botV, topNRGBA2, botNRGBA2, alphaTop, alphaBot, width)

	for x := 0; x < width; x++ {
		off := x * 4
		if topNRGBA2[off+3] != alphaTop[x] {
			t.Errorf("top[%d]: alpha=%d, want %d", x, topNRGBA2[off+3], alphaTop[x])
		}
		if botNRGBA2[off+3] != alphaBot[x] {
			t.Errorf("bot[%d]: alpha=%d, want %d", x, botNRGBA2[off+3], alphaBot[x])
		}
	}
}

// TestUpsampleLinePairEvenWidth tests even-width images where the last pixel
// uses the edge formula.
func TestUpsampleLinePairEvenWidth(t *testing.T) {
	// Width=6: 3 chroma samples, lastPixelPair=2.
	// Interior loop processes x=1,2 (two pairs = pixels 1-4).
	// Last pixel (5) uses edge formula.
	width := 6
	topY := make([]byte, width)
	botY := make([]byte, width)
	for i := range topY {
		topY[i] = 128
		botY[i] = 128
	}
	topU := []byte{100, 150, 200}
	topV := []byte{100, 150, 200}
	botU := []byte{100, 150, 200}
	botV := []byte{100, 150, 200}

	topDst := make([]byte, width*3)
	botDst := make([]byte, width*3)

	UpsampleLinePair(topY, botY, topU, topV, botU, botV, topDst, botDst, width)

	// Last pixel (index 5) should use edge formula: (3*tl + l + 2) >> 2.
	// After processing x=2: tlUV = loadUV(150,150), lUV = loadUV(150,150).
	// Wait -- at x=2, t_uv = loadUV(200,200), uv = loadUV(200,200).
	// After x=2: tlUV = loadUV(200,200), lUV = loadUV(200,200).
	// Last pixel: (3*200 + 200 + 2) >> 2 = (800+2) >> 2 = 200.
	// Since topU == botU, the edge formula gives 200 for both rows.

	var expected [3]byte
	YUVToRGB(128, 200, 200, expected[:])
	lastOff := (width - 1) * 3
	if topDst[lastOff] != expected[0] || topDst[lastOff+1] != expected[1] || topDst[lastOff+2] != expected[2] {
		t.Errorf("last pixel top: got RGB(%d,%d,%d), want RGB(%d,%d,%d)",
			topDst[lastOff], topDst[lastOff+1], topDst[lastOff+2],
			expected[0], expected[1], expected[2])
	}
}

// TestUpsampleLinePairOddWidth tests that odd-width images do NOT trigger
// the even-width trailing pixel code.
func TestUpsampleLinePairOddWidth(t *testing.T) {
	// Width=5: 3 chroma samples, lastPixelPair=2.
	// Interior loop processes x=1,2 (pixels 1-4).
	// No trailing pixel since 5 is odd.
	width := 5
	topY := make([]byte, width)
	for i := range topY {
		topY[i] = 128
	}
	topU := []byte{100, 150, 200}
	topV := []byte{100, 150, 200}
	botU := []byte{100, 150, 200}
	botV := []byte{100, 150, 200}

	topDst := make([]byte, width*3)

	// Should not panic.
	UpsampleLinePair(topY, nil, topU, topV, botU, botV, topDst, nil, width)

	// Pixel 4 (last, index 4) should be written by the interior loop at x=2.
	// x=2: top-right pixel = pixel 2*2 = 4.
	// This is the (diag03 + t_uv) >> 1 value.
	// All is well if we don't panic.
	t.Logf("odd width=%d: last pixel RGB=(%d,%d,%d)", width,
		topDst[(width-1)*3], topDst[(width-1)*3+1], topDst[(width-1)*3+2])
}
