package lossy

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
)

// TestEncodeCompare encodes the same image with both the Go encoder and cwebp,
// decodes both with dwebp, and compares pixel-level quality.
func TestEncodeCompare(t *testing.T) {
	// Check for cwebp and dwebp.
	if _, err := exec.LookPath("cwebp"); err != nil {
		t.Skip("cwebp not found, skipping comparison test")
	}
	if _, err := exec.LookPath("dwebp"); err != nil {
		t.Skip("dwebp not found, skipping comparison test")
	}

	tmpDir := t.TempDir()

	sizes := []struct{ w, h int }{{64, 64}, {256, 256}, {768, 576}}
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			// Create test image.
			img := colorPatternImage(sz.w, sz.h)
			pngPath := filepath.Join(tmpDir, fmt.Sprintf("test_%dx%d.png", sz.w, sz.h))
			f, err := os.Create(pngPath)
			if err != nil {
				t.Fatalf("create PNG: %v", err)
			}
			if err := png.Encode(f, img); err != nil {
				f.Close()
				t.Fatalf("encode PNG: %v", err)
			}
			f.Close()

			w, h := sz.w, sz.h

			// Compute source YUV.
			srcY := make([]byte, w*h)
			srcU := make([]byte, (w/2)*(h/2))
			srcV := make([]byte, (w/2)*(h/2))
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					srcY[y*w+x] = dsp.RGBToY(int(r>>8), int(g>>8), int(b>>8))
				}
			}
			for y := 0; y < h/2; y++ {
				for x := 0; x < w/2; x++ {
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

			for _, q := range []int{75, 50} {
				t.Run(fmt.Sprintf("q%d", q), func(t *testing.T) {
					// Encode with Go encoder.
					cfg := DefaultConfig(q)
					cfg.Segments = 1
					enc := NewEncoder(img, cfg)
					vp8Data, err := enc.EncodeFrame()
					if err != nil {
						t.Fatalf("Go EncodeFrame: %v", err)
					}

					// Decode Go output with Go decoder.
					_, _, goDecY, goYStride, goDecU, goDecV, goUVStride, err := DecodeFrame(vp8Data)
					if err != nil {
						t.Fatalf("Go DecodeFrame: %v", err)
					}

					// Encode with cwebp.
					cwebpPath := filepath.Join(tmpDir, fmt.Sprintf("cwebp_%dx%d_q%d.webp", w, h, q))
					cmd := exec.Command("cwebp", "-q", fmt.Sprintf("%d", q), "-segments", "1", "-m", "4", pngPath, "-o", cwebpPath)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("cwebp failed: %v\n%s", err, out)
					}

					// Decode cwebp output to get YUV for comparison.
					yuvPath := filepath.Join(tmpDir, fmt.Sprintf("cwebp_%dx%d_q%d.yuv", w, h, q))
					cmd = exec.Command("dwebp", cwebpPath, "-yuv", "-o", yuvPath)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("dwebp failed: %v\n%s", err, out)
					}

					// Read raw YUV data: Y(w*h) + U(w/2 * h/2) + V(w/2 * h/2).
					yuvData, err := os.ReadFile(yuvPath)
					if err != nil {
						t.Fatalf("read YUV: %v", err)
					}
					ySize := w * h
					uvSize := (w / 2) * (h / 2)
					if len(yuvData) < ySize+2*uvSize {
						t.Fatalf("YUV data too short: %d bytes", len(yuvData))
					}
					cY := yuvData[:ySize]
					cU := yuvData[ySize : ySize+uvSize]
					cV := yuvData[ySize+uvSize : ySize+2*uvSize]

					// Extract Go decoded planes.
					gY := make([]byte, w*h)
					gU := make([]byte, (w/2)*(h/2))
					gV := make([]byte, (w/2)*(h/2))
					for y := 0; y < h; y++ {
						copy(gY[y*w:], goDecY[y*goYStride:y*goYStride+w])
					}
					for y := 0; y < h/2; y++ {
						copy(gU[y*(w/2):], goDecU[y*goUVStride:y*goUVStride+w/2])
						copy(gV[y*(w/2):], goDecV[y*goUVStride:y*goUVStride+w/2])
					}

					// Compute PSNR for both vs source.
					goYPSNR := computePSNR(srcY, gY)
					goUPSNR := computePSNR(srcU, gU)
					goVPSNR := computePSNR(srcV, gV)

					cYPSNR := computePSNR(srcY, cY)
					cUPSNR := computePSNR(srcU, cU)
					cVPSNR := computePSNR(srcV, cV)

					t.Logf("Go encoder:    Y=%.2f dB  U=%.2f dB  V=%.2f dB", goYPSNR, goUPSNR, goVPSNR)
					t.Logf("cwebp:         Y=%.2f dB  U=%.2f dB  V=%.2f dB", cYPSNR, cUPSNR, cVPSNR)

					// Report PSNR delta.
					yDelta := goYPSNR - cYPSNR
					uDelta := goUPSNR - cUPSNR
					vDelta := goVPSNR - cVPSNR
					t.Logf("Delta (Go-C):  Y=%+.2f dB  U=%+.2f dB  V=%+.2f dB", yDelta, uDelta, vDelta)

					// Fail if Go is significantly worse than cwebp.
					// After FTransform coeff[12] fix and per-segment filter
					// strength, the gap should be small.
					// Y threshold: 2.5 dB (ref_lf_delta=0 matching C adds ~0.2 dB gap).
					// UV threshold: 4 dB at Q75+, 6 dB at Q50.
					yThreshold := -2.5
					uvThreshold := -4.0
					if q <= 50 {
						uvThreshold = -6.0
					}
					if yDelta < yThreshold {
						t.Errorf("Go Y PSNR is %.2f dB worse than cwebp (threshold: %.0f dB)", -yDelta, -yThreshold)
					}
					if uDelta < uvThreshold {
						t.Errorf("Go U PSNR is %.2f dB worse than cwebp (threshold: %.0f dB)", -uDelta, -uvThreshold)
					}
					if vDelta < uvThreshold {
						t.Errorf("Go V PSNR is %.2f dB worse than cwebp (threshold: %.0f dB)", -vDelta, -uvThreshold)
					}
				})
			}
		})
	}
}

// TestEncodeCompareRGB compares the final RGB output of both encoders
// by decoding to RGBA with dwebp and comparing against the source.
func TestEncodeCompareRGB(t *testing.T) {
	if _, err := exec.LookPath("cwebp"); err != nil {
		t.Skip("cwebp not found, skipping comparison test")
	}
	if _, err := exec.LookPath("dwebp"); err != nil {
		t.Skip("dwebp not found, skipping comparison test")
	}

	tmpDir := t.TempDir()

	sizes := []struct{ w, h int }{{64, 64}, {256, 256}, {768, 576}}
	for _, sz := range sizes {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			img := colorPatternImage(sz.w, sz.h)
			w, h := sz.w, sz.h

			// Save as PNG for cwebp.
			pngPath := filepath.Join(tmpDir, fmt.Sprintf("test_%dx%d.png", w, h))
			f, err := os.Create(pngPath)
			if err != nil {
				t.Fatalf("create PNG: %v", err)
			}
			if err := png.Encode(f, img); err != nil {
				f.Close()
				t.Fatalf("encode PNG: %v", err)
			}
			f.Close()

			// Source RGB.
			srcRGB := make([]byte, w*h*3)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					c := img.NRGBAAt(x, y)
					off := (y*w + x) * 3
					srcRGB[off+0] = c.R
					srcRGB[off+1] = c.G
					srcRGB[off+2] = c.B
				}
			}

			for _, q := range []int{75, 50} {
				t.Run(fmt.Sprintf("q%d", q), func(t *testing.T) {
					// Go encoder -> Go decoder -> RGB.
					cfg := DefaultConfig(q)
					cfg.Segments = 1
					enc := NewEncoder(img, cfg)
					vp8Data, err := enc.EncodeFrame()
					if err != nil {
						t.Fatalf("Go EncodeFrame: %v", err)
					}
					_, _, decY, yStride, decU, decV, uvStride, err := DecodeFrame(vp8Data)
					if err != nil {
						t.Fatalf("Go DecodeFrame: %v", err)
					}
					goRGB := yuvToRGB(decY, yStride, decU, decV, uvStride, w, h)

					// cwebp -> dwebp -> PPM -> RGB.
					cwebpPath := filepath.Join(tmpDir, fmt.Sprintf("cwebp_%dx%d_q%d.webp", w, h, q))
					cmd := exec.Command("cwebp", "-q", fmt.Sprintf("%d", q), "-segments", "1", "-m", "4", pngPath, "-o", cwebpPath)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("cwebp: %v\n%s", err, out)
					}
					ppmPath := filepath.Join(tmpDir, fmt.Sprintf("cwebp_%dx%d_q%d.ppm", w, h, q))
					cmd = exec.Command("dwebp", cwebpPath, "-ppm", "-o", ppmPath)
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("dwebp: %v\n%s", err, out)
					}
					cRGB, err := readPPM(ppmPath, w, h)
					if err != nil {
						t.Fatalf("read PPM: %v", err)
					}

					// Compare.
					goMSE := mse(srcRGB, goRGB)
					cMSE := mse(srcRGB, cRGB)
					goPSNR := 10 * math.Log10(255*255/goMSE)
					cPSNR := 10 * math.Log10(255*255/cMSE)

					t.Logf("RGB PSNR:  Go=%.2f dB  cwebp=%.2f dB  delta=%+.2f dB", goPSNR, cPSNR, goPSNR-cPSNR)

					// Go RGB PSNR should not be more than 3 dB worse than cwebp.
					rgbDelta := goPSNR - cPSNR
					rgbThreshold := -3.0
					if rgbDelta < rgbThreshold {
						t.Errorf("Go RGB PSNR is %.2f dB worse than cwebp (threshold: %.0f dB)", -rgbDelta, -rgbThreshold)
					}
				})
			}
		})
	}
}

// yuvToRGB converts YUV420 planes to packed RGB.
func yuvToRGB(y []byte, yStride int, u, v []byte, uvStride int, w, h int) []byte {
	rgb := make([]byte, w*h*3)
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			yVal := int(y[row*yStride+col])
			uVal := int(u[(row/2)*uvStride+col/2])
			vVal := int(v[(row/2)*uvStride+col/2])
			off := (row*w + col) * 3
			var buf [3]byte
			dsp.YUVToRGB(yVal, uVal, vVal, buf[:])
			rgb[off+0] = buf[0]
			rgb[off+1] = buf[1]
			rgb[off+2] = buf[2]
		}
	}
	return rgb
}

// readPPM reads a binary PPM (P6) file and returns packed RGB data.
func readPPM(path string, expectedW, expectedH int) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Parse PPM P6 header by scanning lines.
	// Format: "P6\n<W> <H>\n<MAXVAL>\n<binary data>"
	pos := 0
	readLine := func() string {
		start := pos
		for pos < len(data) && data[pos] != '\n' {
			pos++
		}
		line := string(data[start:pos])
		if pos < len(data) {
			pos++ // skip '\n'
		}
		return line
	}

	magic := readLine()
	if magic != "P6" {
		return nil, fmt.Errorf("not a P6 PPM: %q", magic)
	}

	var w, h int
	if _, err := fmt.Sscanf(readLine(), "%d %d", &w, &h); err != nil {
		return nil, fmt.Errorf("PPM dimensions: %v", err)
	}
	readLine() // maxval line

	if w != expectedW || h != expectedH {
		return nil, fmt.Errorf("PPM size %dx%d, expected %dx%d", w, h, expectedW, expectedH)
	}

	pixelData := data[pos:]
	expected := w * h * 3
	if len(pixelData) < expected {
		return nil, fmt.Errorf("PPM pixel data too short: %d/%d", len(pixelData), expected)
	}
	return pixelData[:expected], nil
}

func mse(a, b []byte) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	return sum / float64(len(a))
}

