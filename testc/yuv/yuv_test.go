//go:build testc

package yuv

import (
	"testing"

	"github.com/deepteams/webp/internal/dsp"
)

func init() {
	dsp.Init()
}

// TestYUVToRGB compares Go YUVToR/G/B against C VP8YUVToR/G/B.
// Grid: Y,U,V in [0,255] step 8 => 32^3 = 32768 combinations.
// The Go implementation uses slightly different bias constants than C
// (e.g., Go kRBias=14266 vs C 14234), so off-by-one differences are expected.
// We allow a tolerance of 1 and only fail on larger deviations.
func TestYUVToRGB(t *testing.T) {
	const step = 8
	const tolerance = 1
	var mismatchR, mismatchG, mismatchB int
	var exactR, exactG, exactB int
	var offByOneR, offByOneG, offByOneB int

	for y := 0; y < 256; y += step {
		for u := 0; u < 256; u += step {
			for v := 0; v < 256; v += step {
				goR := dsp.YUVToR(y, v)
				cR := CYUVToR(y, v)
				d := abs(int(goR) - int(cR))
				if d == 0 {
					exactR++
				} else if d <= tolerance {
					offByOneR++
				} else {
					mismatchR++
					if mismatchR <= 3 {
						t.Errorf("YUVToR(y=%d, v=%d): Go=%d C=%d diff=%d", y, v, goR, cR, d)
					}
				}

				goG := dsp.YUVToG(y, u, v)
				cG := CYUVToG(y, u, v)
				d = abs(int(goG) - int(cG))
				if d == 0 {
					exactG++
				} else if d <= tolerance {
					offByOneG++
				} else {
					mismatchG++
					if mismatchG <= 3 {
						t.Errorf("YUVToG(y=%d, u=%d, v=%d): Go=%d C=%d diff=%d", y, u, v, goG, cG, d)
					}
				}

				goB := dsp.YUVToB(y, u)
				cB := CYUVToB(y, u)
				d = abs(int(goB) - int(cB))
				if d == 0 {
					exactB++
				} else if d <= tolerance {
					offByOneB++
				} else {
					mismatchB++
					if mismatchB <= 3 {
						t.Errorf("YUVToB(y=%d, u=%d): Go=%d C=%d diff=%d", y, u, goB, cB, d)
					}
				}
			}
		}
	}

	total := 32 * 32 * 32
	t.Logf("YUV->RGB: tested %d combinations", total)
	t.Logf("  R: exact=%d off-by-one=%d mismatch=%d", exactR, offByOneR, mismatchR)
	t.Logf("  G: exact=%d off-by-one=%d mismatch=%d", exactG, offByOneG, mismatchG)
	t.Logf("  B: exact=%d off-by-one=%d mismatch=%d", exactB, offByOneB, mismatchB)
	if mismatchR > 0 || mismatchG > 0 || mismatchB > 0 {
		t.Fatalf("mismatches beyond tolerance=%d: R=%d G=%d B=%d", tolerance, mismatchR, mismatchG, mismatchB)
	}
}

// TestRGBToYUV compares Go RGBToY/U/V against C VP8RGBToY/U/V using the
// SAME rounding parameter. Now that Go VP8ClipUV uses >> 18 matching C,
// all Y/U/V values should be bit-exact.
func TestRGBToYUV(t *testing.T) {
	const step = 16
	const yuvHalf = 1 << 15

	var mismatchY, mismatchU, mismatchV int
	var exactY, exactU, exactV int

	total := 0
	for r := 0; r < 256; r += step {
		for g := 0; g < 256; g += step {
			for b := 0; b < 256; b += step {
				total++

				goY := dsp.RGBToY(r, g, b)
				cY := CRGBToY(r, g, b, yuvHalf)
				if goY != cY {
					mismatchY++
					if mismatchY <= 3 {
						t.Errorf("RGBToY(r=%d, g=%d, b=%d): Go=%d C=%d", r, g, b, goY, cY)
					}
				} else {
					exactY++
				}

				goU := dsp.RGBToU(r, g, b, yuvHalf)
				cU := CRGBToU(r, g, b, yuvHalf)
				if goU != cU {
					mismatchU++
					if mismatchU <= 3 {
						t.Errorf("RGBToU(r=%d, g=%d, b=%d): Go=%d C=%d", r, g, b, goU, cU)
					}
				} else {
					exactU++
				}

				goV := dsp.RGBToV(r, g, b, yuvHalf)
				cV := CRGBToV(r, g, b, yuvHalf)
				if goV != cV {
					mismatchV++
					if mismatchV <= 3 {
						t.Errorf("RGBToV(r=%d, g=%d, b=%d): Go=%d C=%d", r, g, b, goV, cV)
					}
				} else {
					exactV++
				}
			}
		}
	}

	t.Logf("RGB->YUV: tested %d combinations", total)
	t.Logf("  Y: exact=%d mismatch=%d", exactY, mismatchY)
	t.Logf("  U: exact=%d mismatch=%d", exactU, mismatchU)
	t.Logf("  V: exact=%d mismatch=%d", exactV, mismatchV)
	if mismatchY > 0 || mismatchU > 0 || mismatchV > 0 {
		t.Fatalf("mismatches: Y=%d U=%d V=%d out of %d", mismatchY, mismatchU, mismatchV, total)
	}
}

// TestRGBToYUV_SumConvention compares Go RGBToU/V against C VP8RGBToU/V
// using the same sum-of-4-pixels convention (the standard encoder usage):
//   - Both: sum-of-4 pixels (r, g, b) with rounding = 1<<17 (YUV_HALF << 2)
//
// Now that Go VP8ClipUV uses >> 18 matching C, both should be bit-exact.
func TestRGBToYUV_SumConvention(t *testing.T) {
	const step = 16

	var exactU, exactV int
	var mismatchU, mismatchV int

	total := 0
	for r1 := 0; r1 < 256; r1 += step * 2 {
		for g1 := 0; g1 < 256; g1 += step * 2 {
			for b1 := 0; b1 < 256; b1 += step * 2 {
				for r2 := 0; r2 < 256; r2 += step * 4 {
					for g2 := 0; g2 < 256; g2 += step * 4 {
						for b2 := 0; b2 < 256; b2 += step * 4 {
							total++

							// Sum of 4 pixels: 2x p1 + 2x p2.
							sumR := 2*r1 + 2*r2
							sumG := 2*g1 + 2*g2
							sumB := 2*b1 + 2*b2

							// Both use sum-of-4 convention with rounding=1<<17.
							goU := dsp.RGBToU(sumR, sumG, sumB, 1<<17)
							goV := dsp.RGBToV(sumR, sumG, sumB, 1<<17)

							cU := CRGBToU(sumR, sumG, sumB, 1<<17)
							cV := CRGBToV(sumR, sumG, sumB, 1<<17)

							if goU != cU {
								mismatchU++
								if mismatchU <= 5 {
									t.Errorf("U mismatch: sum=(%d,%d,%d) Go=%d C=%d",
										sumR, sumG, sumB, goU, cU)
								}
							} else {
								exactU++
							}

							if goV != cV {
								mismatchV++
								if mismatchV <= 5 {
									t.Errorf("V mismatch: sum=(%d,%d,%d) Go=%d C=%d",
										sumR, sumG, sumB, goV, cV)
								}
							} else {
								exactV++
							}
						}
					}
				}
			}
		}
	}

	t.Logf("Sum convention RGB->UV: tested %d combinations", total)
	t.Logf("  U: exact=%d mismatch=%d", exactU, mismatchU)
	t.Logf("  V: exact=%d mismatch=%d", exactV, mismatchV)
	if mismatchU > 0 || mismatchV > 0 {
		t.Fatalf("mismatches: U=%d V=%d out of %d", mismatchU, mismatchV, total)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
