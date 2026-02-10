//go:build testc

package sharpyuv

import (
	"image"
	"math/rand"
	"testing"

	"github.com/deepteams/webp/sharpyuv"
)

func TestComputeConversionMatrix(t *testing.T) {
	tests := []struct {
		name string
		cs   sharpyuv.ColorSpace
	}{
		{"BT601_Limited", sharpyuv.BT601},
		{"BT601_Full", sharpyuv.BT601Full},
		{"BT709_Limited", sharpyuv.BT709},
		{"BT709_Full", sharpyuv.BT709Full},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			goMatrix := sharpyuv.ComputeConversionMatrix(&tc.cs)

			cY, cU, cV := CComputeConversionMatrix(tc.cs.Kr, tc.cs.Kb, tc.cs.BitDepth, tc.cs.Range)

			// Compare RGBToY
			for i := 0; i < 4; i++ {
				if goMatrix.RGBToY[i] != cY[i] {
					t.Errorf("RGBToY[%d]: Go=%d C=%d", i, goMatrix.RGBToY[i], cY[i])
				}
			}
			// Compare RGBToU
			for i := 0; i < 4; i++ {
				if goMatrix.RGBToU[i] != cU[i] {
					t.Errorf("RGBToU[%d]: Go=%d C=%d", i, goMatrix.RGBToU[i], cU[i])
				}
			}
			// Compare RGBToV
			for i := 0; i < 4; i++ {
				if goMatrix.RGBToV[i] != cV[i] {
					t.Errorf("RGBToV[%d]: Go=%d C=%d", i, goMatrix.RGBToV[i], cV[i])
				}
			}

			t.Logf("Go Y=%v U=%v V=%v", goMatrix.RGBToY, goMatrix.RGBToU, goMatrix.RGBToV)
			t.Logf(" C Y=%v U=%v V=%v", cY, cU, cV)
		})
	}
}

func TestSharpYuvConvert(t *testing.T) {
	type testImage struct {
		name   string
		width  int
		height int
		gen    func(w, h int) []byte
	}

	images := []testImage{
		{
			name: "solid_16x16", width: 16, height: 16,
			gen: func(w, h int) []byte {
				rgb := make([]byte, w*h*3)
				for i := 0; i < w*h; i++ {
					rgb[i*3+0] = 128
					rgb[i*3+1] = 64
					rgb[i*3+2] = 200
				}
				return rgb
			},
		},
		{
			name: "gradient_16x16", width: 16, height: 16,
			gen: func(w, h int) []byte {
				rgb := make([]byte, w*h*3)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						idx := (y*w + x) * 3
						rgb[idx+0] = byte(x * 255 / (w - 1))
						rgb[idx+1] = 100
						rgb[idx+2] = 150
					}
				}
				return rgb
			},
		},
		{
			name: "random_16x16", width: 16, height: 16,
			gen: func(w, h int) []byte {
				rng := rand.New(rand.NewSource(42))
				rgb := make([]byte, w*h*3)
				for i := range rgb {
					rgb[i] = byte(rng.Intn(256))
				}
				return rgb
			},
		},
		{
			name: "solid_64x64", width: 64, height: 64,
			gen: func(w, h int) []byte {
				rgb := make([]byte, w*h*3)
				for i := 0; i < w*h; i++ {
					rgb[i*3+0] = 128
					rgb[i*3+1] = 64
					rgb[i*3+2] = 200
				}
				return rgb
			},
		},
		{
			name: "gradient_64x64", width: 64, height: 64,
			gen: func(w, h int) []byte {
				rgb := make([]byte, w*h*3)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						idx := (y*w + x) * 3
						rgb[idx+0] = byte(x * 255 / (w - 1))
						rgb[idx+1] = 100
						rgb[idx+2] = 150
					}
				}
				return rgb
			},
		},
		{
			name: "random_64x64", width: 64, height: 64,
			gen: func(w, h int) []byte {
				rng := rand.New(rand.NewSource(123))
				rgb := make([]byte, w*h*3)
				for i := range rgb {
					rgb[i] = byte(rng.Intn(256))
				}
				return rgb
			},
		},
	}

	// Use BT.601 limited matrix from ComputeConversionMatrix
	// to ensure Go and C use the exact same matrix.
	cs := sharpyuv.BT601
	matrix := sharpyuv.ComputeConversionMatrix(&cs)

	for _, img := range images {
		t.Run(img.name, func(t *testing.T) {
			w, h := img.width, img.height
			rgb := img.gen(w, h)
			rgbStride := w * 3

			uvW := (w + 1) / 2
			uvH := (h + 1) / 2

			// Go conversion
			goYCbCr := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
			opts := &sharpyuv.Options{
				Matrix:       matrix,
				TransferType: sharpyuv.TransferSRGB,
				SharpEnabled: true,
			}
			if err := sharpyuv.Convert(rgb, w, h, rgbStride, goYCbCr, opts); err != nil {
				t.Fatalf("Go Convert: %v", err)
			}

			// C conversion
			cY, cU, cV, _, _, _ := CSharpYuvConvert(rgb, w, h, rgbStride, matrix)

			// Compare Y plane
			const tolerance = 1
			var yMismatch, uMismatch, vMismatch int
			for j := 0; j < h; j++ {
				for i := 0; i < w; i++ {
					goVal := goYCbCr.Y[j*goYCbCr.YStride+i]
					cVal := cY[j*w+i]
					diff := int(goVal) - int(cVal)
					if diff < -tolerance || diff > tolerance {
						yMismatch++
						if yMismatch <= 5 {
							t.Errorf("Y[%d,%d]: Go=%d C=%d diff=%d", i, j, goVal, cVal, diff)
						}
					}
				}
			}

			// Compare U plane
			for j := 0; j < uvH; j++ {
				for i := 0; i < uvW; i++ {
					goVal := goYCbCr.Cb[j*goYCbCr.CStride+i]
					cVal := cU[j*uvW+i]
					diff := int(goVal) - int(cVal)
					if diff < -tolerance || diff > tolerance {
						uMismatch++
						if uMismatch <= 5 {
							t.Errorf("U[%d,%d]: Go=%d C=%d diff=%d", i, j, goVal, cVal, diff)
						}
					}
				}
			}

			// Compare V plane
			for j := 0; j < uvH; j++ {
				for i := 0; i < uvW; i++ {
					goVal := goYCbCr.Cr[j*goYCbCr.CStride+i]
					cVal := cV[j*uvW+i]
					diff := int(goVal) - int(cVal)
					if diff < -tolerance || diff > tolerance {
						vMismatch++
						if vMismatch <= 5 {
							t.Errorf("V[%d,%d]: Go=%d C=%d diff=%d", i, j, goVal, cVal, diff)
						}
					}
				}
			}

			totalY := w * h
			totalUV := uvW * uvH
			t.Logf("Y: %d/%d mismatches, U: %d/%d mismatches, V: %d/%d mismatches",
				yMismatch, totalY, uMismatch, totalUV, vMismatch, totalUV)

			if yMismatch > 0 || uMismatch > 0 || vMismatch > 0 {
				t.Fatalf("total mismatches: Y=%d U=%d V=%d (tolerance=%d)",
					yMismatch, uMismatch, vMismatch, tolerance)
			}
		})
	}
}
