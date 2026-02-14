package lossy

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
)

// smpteBarImage creates a SMPTE color bar test pattern.
// 8 vertical bars: white, yellow, cyan, green, magenta, red, blue, black.
func smpteBarImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	bars := []color.NRGBA{
		{R: 255, G: 255, B: 255, A: 255}, // white
		{R: 255, G: 255, B: 0, A: 255},   // yellow
		{R: 0, G: 255, B: 255, A: 255},   // cyan
		{R: 0, G: 255, B: 0, A: 255},     // green
		{R: 255, G: 0, B: 255, A: 255},   // magenta
		{R: 255, G: 0, B: 0, A: 255},     // red
		{R: 0, G: 0, B: 255, A: 255},     // blue
		{R: 0, G: 0, B: 0, A: 255},       // black
	}
	barWidth := w / len(bars)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			idx := x / barWidth
			if idx >= len(bars) {
				idx = len(bars) - 1
			}
			img.SetNRGBA(x, y, bars[idx])
		}
	}
	return img
}

// solidBlockImage creates a test image with 6 solid color blocks
// arranged in a 3x2 grid: R, G, B (top), C, M, Y (bottom).
func solidBlockImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	blocks := []color.NRGBA{
		{R: 255, G: 0, B: 0, A: 255},     // red
		{R: 0, G: 255, B: 0, A: 255},     // green
		{R: 0, G: 0, B: 255, A: 255},     // blue
		{R: 0, G: 255, B: 255, A: 255},   // cyan
		{R: 255, G: 0, B: 255, A: 255},   // magenta
		{R: 255, G: 255, B: 0, A: 255},   // yellow
	}
	halfW, halfH := w/3, h/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			row := 0
			if y >= halfH {
				row = 1
			}
			col := x / halfW
			if col >= 3 {
				col = 2
			}
			idx := row*3 + col
			img.SetNRGBA(x, y, blocks[idx])
		}
	}
	return img
}

// computeYUVPSNR encodes an image and returns per-channel PSNR (Y, U, V).
func computeYUVPSNR(t *testing.T, img *image.NRGBA, quality int) (yPSNR, uPSNR, vPSNR float64) {
	t.Helper()
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	// Compute source YUV.
	srcY := make([]byte, w*h)
	srcU := make([]byte, (w/2)*(h/2))
	srcV := make([]byte, (w/2)*(h/2))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			srcY[y*w+x] = dsp.RGBToY(int(c.R), int(c.G), int(c.B))
		}
	}
	for y := 0; y < h/2; y++ {
		for x := 0; x < w/2; x++ {
			var rSum, gSum, bSum int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					c := img.NRGBAAt(x*2+dx, y*2+dy)
					rSum += int(c.R)
					gSum += int(c.G)
					bSum += int(c.B)
				}
			}
			srcU[y*(w/2)+x] = dsp.RGBToU(rSum, gSum, bSum, 1<<17)
			srcV[y*(w/2)+x] = dsp.RGBToV(rSum, gSum, bSum, 1<<17)
		}
	}

	// Encode.
	cfg := DefaultConfig(quality)
	cfg.Segments = 1
	enc := NewEncoder(img, cfg)
	vp8Data, err := enc.EncodeFrame()
	if err != nil {
		t.Fatalf("EncodeFrame q%d: %v", quality, err)
	}

	// Decode.
	_, decW, decH, decY, decYStride, decU, decV, decUVStride, err := DecodeFrame(vp8Data)
	if err != nil {
		t.Fatalf("DecodeFrame q%d: %v", quality, err)
	}
	if decW != w || decH != h {
		t.Fatalf("decoded dimensions %dx%d, want %dx%d", decW, decH, w, h)
	}

	// Extract decoded planes.
	dY := make([]byte, w*h)
	for row := 0; row < h; row++ {
		copy(dY[row*w:], decY[row*decYStride:row*decYStride+w])
	}
	uvW, uvH := w/2, h/2
	dU := make([]byte, uvW*uvH)
	dV := make([]byte, uvW*uvH)
	for row := 0; row < uvH; row++ {
		copy(dU[row*uvW:], decU[row*decUVStride:row*decUVStride+uvW])
		copy(dV[row*uvW:], decV[row*decUVStride:row*decUVStride+uvW])
	}

	yPSNR = computePSNR(srcY, dY)
	uPSNR = computePSNR(srcU, dU)
	vPSNR = computePSNR(srcV, dV)
	return
}

// TestColorFidelity encodes various test patterns at different sizes and
// qualities, measuring per-channel PSNR to detect color degradation.
func TestColorFidelity(t *testing.T) {
	type sizeSpec struct {
		w, h int
	}
	type qualitySpec struct {
		q         int
		minYPSNR  float64
		minUVPSNR float64
	}

	sizes := []sizeSpec{
		{64, 64},
		{256, 256},
		{768, 576},
	}
	qualities := []qualitySpec{
		{50, 38, 38},
		{75, 42, 42},
	}

	patterns := []struct {
		name string
		gen  func(int, int) *image.NRGBA
	}{
		{"smpte_bars", smpteBarImage},
		{"solid_blocks", solidBlockImage},
		{"color_gradient", colorPatternImage},
	}

	for _, pat := range patterns {
		for _, sz := range sizes {
			for _, q := range qualities {
				name := fmt.Sprintf("%s/%dx%d/q%d", pat.name, sz.w, sz.h, q.q)
				t.Run(name, func(t *testing.T) {
					img := pat.gen(sz.w, sz.h)
					yPSNR, uPSNR, vPSNR := computeYUVPSNR(t, img, q.q)

					t.Logf("Y=%.2f dB  U=%.2f dB  V=%.2f dB", yPSNR, uPSNR, vPSNR)

					if yPSNR < q.minYPSNR {
						t.Errorf("Y PSNR %.2f dB < minimum %.2f dB", yPSNR, q.minYPSNR)
					}
					if uPSNR < q.minUVPSNR {
						t.Errorf("U PSNR %.2f dB < minimum %.2f dB", uPSNR, q.minUVPSNR)
					}
					if vPSNR < q.minUVPSNR {
						t.Errorf("V PSNR %.2f dB < minimum %.2f dB", vPSNR, q.minUVPSNR)
					}
				})
			}
		}
	}
}

// TestPerColorPSNR measures PSNR separately for each color region in a
// SMPTE bar pattern to detect per-channel color degradation.
func TestPerColorPSNR(t *testing.T) {
	w, h := 256, 256
	img := smpteBarImage(w, h)

	barNames := []string{"white", "yellow", "cyan", "green", "magenta", "red", "blue", "black"}
	barWidth := w / len(barNames)

	for _, q := range []int{75, 50} {
		t.Run(fmt.Sprintf("q%d", q), func(t *testing.T) {
			cfg := DefaultConfig(q)
			cfg.Segments = 1
			enc := NewEncoder(img, cfg)
			vp8Data, err := enc.EncodeFrame()
			if err != nil {
				t.Fatalf("EncodeFrame: %v", err)
			}

			_, _, _, decY, decYStride, decU, decV, decUVStride, err := DecodeFrame(vp8Data)
			if err != nil {
				t.Fatalf("DecodeFrame: %v", err)
			}

			for bi, barName := range barNames {
				x0 := bi * barWidth
				x1 := x0 + barWidth

				// Compute source and decoded Y for this bar region.
				regionSize := barWidth * h
				srcYRegion := make([]byte, 0, regionSize)
				decYRegion := make([]byte, 0, regionSize)
				for row := 0; row < h; row++ {
					for col := x0; col < x1; col++ {
						c := img.NRGBAAt(col, row)
						srcYRegion = append(srcYRegion, dsp.RGBToY(int(c.R), int(c.G), int(c.B)))
						decYRegion = append(decYRegion, decY[row*decYStride+col])
					}
				}

				// Compute source and decoded U/V for this bar region (chroma).
				uvX0, uvX1 := x0/2, x1/2
				uvRegionSize := (uvX1 - uvX0) * (h / 2)
				srcURegion := make([]byte, 0, uvRegionSize)
				srcVRegion := make([]byte, 0, uvRegionSize)
				decURegion := make([]byte, 0, uvRegionSize)
				decVRegion := make([]byte, 0, uvRegionSize)
				for row := 0; row < h/2; row++ {
					for col := uvX0; col < uvX1; col++ {
						var rSum, gSum, bSum int
						for dy := 0; dy < 2; dy++ {
							for dx := 0; dx < 2; dx++ {
								c := img.NRGBAAt(col*2+dx, row*2+dy)
								rSum += int(c.R)
								gSum += int(c.G)
								bSum += int(c.B)
							}
						}
						srcURegion = append(srcURegion, dsp.RGBToU(rSum, gSum, bSum, 1<<17))
						srcVRegion = append(srcVRegion, dsp.RGBToV(rSum, gSum, bSum, 1<<17))
						decURegion = append(decURegion, decU[row*decUVStride+col])
						decVRegion = append(decVRegion, decV[row*decUVStride+col])
					}
				}

				yPSNR := computePSNR(srcYRegion, decYRegion)
				uPSNR := computePSNR(srcURegion, decURegion)
				vPSNR := computePSNR(srcVRegion, decVRegion)

				t.Logf("  %-8s  Y=%.1f dB  U=%.1f dB  V=%.1f dB", barName, yPSNR, uPSNR, vPSNR)

				// For Q75, per-color Y should be at least 24 dB.
				// UV can be lower for saturated colors due to chroma subsampling.
				minY := 22.0
				if q >= 75 {
					minY = 26.0
				}
				if yPSNR < minY && !math.IsInf(yPSNR, 1) {
					t.Errorf("%s: Y PSNR %.1f dB < minimum %.1f dB", barName, yPSNR, minY)
				}
			}
		})
	}
}
