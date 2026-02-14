package lossy

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
)

// colorPatternImage creates a test image with distinct color regions to stress
// chroma encoding: red, green, blue, and gradient quadrants.
func colorPatternImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	halfW, halfH := w/2, h/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c color.NRGBA
			switch {
			case x < halfW && y < halfH: // top-left: red gradient
				c = color.NRGBA{R: uint8(x * 255 / halfW), G: 30, B: 30, A: 255}
			case x >= halfW && y < halfH: // top-right: green gradient
				c = color.NRGBA{R: 30, G: uint8((x - halfW) * 255 / halfW), B: 30, A: 255}
			case x < halfW && y >= halfH: // bottom-left: blue gradient
				c = color.NRGBA{R: 30, G: 30, B: uint8((y - halfH) * 255 / halfH), A: 255}
			default: // bottom-right: grayscale gradient
				v := uint8((x - halfW + y - halfH) * 255 / (halfW + halfH))
				c = color.NRGBA{R: v, G: v, B: v, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// computePSNR computes PSNR between two byte slices of equal length.
// Returns infinity if the slices are identical.
func computePSNR(a, b []byte) float64 {
	if len(a) != len(b) {
		return 0
	}
	var mse float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		mse += d * d
	}
	mse /= float64(len(a))
	if mse == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/mse)
}

// maxAbsError returns the maximum absolute error between two byte slices.
func maxAbsError(a, b []byte) int {
	maxErr := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > maxErr {
			maxErr = d
		}
	}
	return maxErr
}

// TestEncodeDiag encodes a test image, decodes it, and reports per-channel PSNR.
func TestEncodeDiag(t *testing.T) {
	img := colorPatternImage(128, 128)
	w, h := 128, 128
	halfH := h / 2

	// Compute source YUV for comparison.
	srcY := make([]byte, w*h)
	srcU := make([]byte, (w/2)*(h/2))
	srcV := make([]byte, (w/2)*(h/2))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			srcY[y*w+x] = dsp.RGBToY(int(r>>8), int(g>>8), int(b>>8))
		}
	}
	for y := 0; y < halfH; y++ {
		for x := 0; x < w/2; x++ {
			// Average 2x2 block for chroma.
			var rSum, gSum, bSum int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					r, g, b, _ := img.At(x*2+dx, y*2+dy).RGBA()
					rSum += int(r >> 8)
					gSum += int(g >> 8)
					bSum += int(b >> 8)
				}
			}
			srcU[y*(w/2)+x] = dsp.RGBToU(rSum, gSum, bSum, 1<<17)
			srcV[y*(w/2)+x] = dsp.RGBToV(rSum, gSum, bSum, 1<<17)
		}
	}

	qualities := []int{100, 75, 50}
	for _, q := range qualities {
		t.Run(qualityName(q), func(t *testing.T) {
			cfg := DefaultConfig(q)
			cfg.Segments = 1
			enc := NewEncoder(img, cfg)

			vp8Data, err := enc.EncodeFrame()
			if err != nil {
				t.Fatalf("EncodeFrame failed: %v", err)
			}

			// Sanity check: filter level should not be excessive.
			// With the non-linear q mapping, quantizer indices are lower,
			// so filter levels are also lower.
			filterLevel := enc.filterHdr.Level
			t.Logf("  Filter level: %d", filterLevel)
			maxLevel := 10
			if q <= 50 {
				maxLevel = 15
			}
			if q == 100 {
				maxLevel = 5
			}
			if filterLevel > maxLevel {
				t.Errorf("filter level %d exceeds maximum %d for Q%d", filterLevel, maxLevel, q)
			}

			_, decW, decH, decY, decYStride, decU, decV, decUVStride, err := DecodeFrame(vp8Data)
			if err != nil {
				t.Fatalf("DecodeFrame failed: %v", err)
			}
			if decW != w || decH != h {
				t.Fatalf("decoded dimensions %dx%d, want %dx%d", decW, decH, w, h)
			}

			// Extract decoded planes into contiguous slices for comparison.
			dY := make([]byte, w*h)
			for y := 0; y < h; y++ {
				copy(dY[y*w:], decY[y*decYStride:y*decYStride+w])
			}
			uvW, uvH := w/2, halfH
			dU := make([]byte, uvW*uvH)
			dV := make([]byte, uvW*uvH)
			for y := 0; y < uvH; y++ {
				copy(dU[y*uvW:], decU[y*decUVStride:y*decUVStride+uvW])
				copy(dV[y*uvW:], decV[y*decUVStride:y*decUVStride+uvW])
			}

			yPSNR := computePSNR(srcY, dY)
			uPSNR := computePSNR(srcU, dU)
			vPSNR := computePSNR(srcV, dV)
			yMax := maxAbsError(srcY, dY)
			uMax := maxAbsError(srcU, dU)
			vMax := maxAbsError(srcV, dV)

			t.Logf("Quality %d:", q)
			t.Logf("  Y: PSNR=%.2f dB, maxErr=%d", yPSNR, yMax)
			t.Logf("  U: PSNR=%.2f dB, maxErr=%d", uPSNR, uMax)
			t.Logf("  V: PSNR=%.2f dB, maxErr=%d", vPSNR, vMax)

			// PSNR quality thresholds (tightened after FTransform coeff[12]
			// fix and per-segment filter strength improvements).
			minYPSNR := 38.0
			minUVPSNR := 38.0
			if q >= 75 {
				minYPSNR = 42.0
				minUVPSNR = 42.0
			}
			if q == 100 {
				minYPSNR = 50.0
				minUVPSNR = 50.0
			}
			if yPSNR < minYPSNR {
				t.Errorf("Y PSNR %.2f dB < minimum expected %.2f dB", yPSNR, minYPSNR)
			}
			if uPSNR < minUVPSNR {
				t.Errorf("U PSNR %.2f dB < minimum expected %.2f dB", uPSNR, minUVPSNR)
			}
			if vPSNR < minUVPSNR {
				t.Errorf("V PSNR %.2f dB < minimum expected %.2f dB", vPSNR, minUVPSNR)
			}
		})
	}
}

func qualityName(q int) string {
	switch q {
	case 100:
		return "q100"
	case 75:
		return "q75"
	case 50:
		return "q50"
	default:
		return "q?"
	}
}
