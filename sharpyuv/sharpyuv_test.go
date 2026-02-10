package sharpyuv

import (
	"image"
	"math"
	"testing"
)

func TestComputeConversionMatrix(t *testing.T) {
	// Test that ComputeConversionMatrix for BT.601 Limited produces values
	// close to the predefined matrix.
	cs := &ColorSpace{Kr: 0.2990, Kb: 0.1140, BitDepth: 8, Range: RangeLimited}
	computed := ComputeConversionMatrix(cs)
	predefined := GetConversionMatrix(MatrixRec601Limited)

	for i := 0; i < 4; i++ {
		if abs32(computed.RGBToY[i]-predefined.RGBToY[i]) > 1 {
			t.Errorf("RGBToY[%d]: computed %d, predefined %d", i, computed.RGBToY[i], predefined.RGBToY[i])
		}
		if abs32(computed.RGBToU[i]-predefined.RGBToU[i]) > 1 {
			t.Errorf("RGBToU[%d]: computed %d, predefined %d", i, computed.RGBToU[i], predefined.RGBToU[i])
		}
		if abs32(computed.RGBToV[i]-predefined.RGBToV[i]) > 1 {
			t.Errorf("RGBToV[%d]: computed %d, predefined %d", i, computed.RGBToV[i], predefined.RGBToV[i])
		}
	}
}

func TestComputeConversionMatrixBT709Full(t *testing.T) {
	cs := &ColorSpace{Kr: 0.2126, Kb: 0.0722, BitDepth: 8, Range: RangeFull}
	computed := ComputeConversionMatrix(cs)
	predefined := GetConversionMatrix(MatrixRec709Full)

	for i := 0; i < 4; i++ {
		if abs32(computed.RGBToY[i]-predefined.RGBToY[i]) > 1 {
			t.Errorf("RGBToY[%d]: computed %d, predefined %d", i, computed.RGBToY[i], predefined.RGBToY[i])
		}
	}
}

func TestGetConversionMatrixInvalid(t *testing.T) {
	m := GetConversionMatrix(MatrixType(99))
	if m != nil {
		t.Error("expected nil for invalid matrix type")
	}
}

func TestGammaRoundTrip_SRGB(t *testing.T) {
	// Test that GammaToLinear -> LinearToGamma is approximately identity for sRGB.
	for v := 0; v <= 255; v++ {
		linear := GammaToLinear(uint16(v), 8, TransferSRGB)
		back := LinearToGamma(linear, 8, TransferSRGB)
		diff := int(back) - v
		if diff < -1 || diff > 1 {
			t.Errorf("sRGB round-trip v=%d -> linear=%d -> back=%d (diff=%d)", v, linear, back, diff)
		}
	}
}

func TestGammaRoundTrip_BT709(t *testing.T) {
	for v := 0; v <= 255; v++ {
		linear := GammaToLinear(uint16(v), 8, TransferBT709)
		back := LinearToGamma(linear, 8, TransferBT709)
		diff := int(back) - v
		if diff < -1 || diff > 1 {
			t.Errorf("BT709 round-trip v=%d -> linear=%d -> back=%d (diff=%d)", v, linear, back, diff)
		}
	}
}

func TestGammaLinearTransfer(t *testing.T) {
	// Linear transfer should be identity.
	v := uint16(128)
	lin := GammaToLinear(v, 8, TransferLinear)
	if lin != uint32(v) {
		t.Errorf("Linear GammaToLinear(%d) = %d, expected %d", v, lin, v)
	}
	back := LinearToGamma(uint32(v), 8, TransferLinear)
	if back != v {
		t.Errorf("Linear LinearToGamma(%d) = %d, expected %d", v, back, v)
	}
}

func TestGammaToLinearPQ(t *testing.T) {
	// PQ: black should be 0.
	lin := GammaToLinear(0, 8, TransferPQ)
	if lin != 0 {
		t.Errorf("PQ GammaToLinear(0) = %d, expected 0", lin)
	}
}

func TestGammaToLinearHLG(t *testing.T) {
	lin := GammaToLinear(0, 8, TransferHLG)
	if lin != 0 {
		t.Errorf("HLG GammaToLinear(0) = %d, expected 0", lin)
	}
}

func TestConvertStandard_SolidRed(t *testing.T) {
	width, height := 4, 4
	rgb := make([]byte, width*height*3)
	for i := 0; i < len(rgb); i += 3 {
		rgb[i] = 255   // R
		rgb[i+1] = 0   // G
		rgb[i+2] = 0   // B
	}

	yuv := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	opts := &Options{
		Matrix:       GetConversionMatrix(MatrixWebP),
		TransferType: TransferSRGB,
		SharpEnabled: false,
	}

	err := Convert(rgb, width, height, width*3, yuv, opts)
	if err != nil {
		t.Fatal(err)
	}

	// For pure red with WebP matrix:
	// Y = (16839 * 255 + 0 + 0 + 16<<16 + 1<<15) >> 16
	// All Y values should be the same.
	y0 := yuv.Y[0]
	for j := 0; j < height; j++ {
		for i := 0; i < width; i++ {
			if yuv.Y[j*yuv.YStride+i] != y0 {
				t.Errorf("Y[%d,%d] = %d, expected %d", i, j, yuv.Y[j*yuv.YStride+i], y0)
			}
		}
	}
	// Y should be in a reasonable range for red (around 81 for BT.601-like)
	if y0 < 50 || y0 > 120 {
		t.Errorf("Y for pure red = %d, expected ~81", y0)
	}

	// UV should be uniform
	u0 := yuv.Cb[0]
	v0 := yuv.Cr[0]
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for j := 0; j < uvH; j++ {
		for i := 0; i < uvW; i++ {
			if yuv.Cb[j*yuv.CStride+i] != u0 {
				t.Errorf("Cb[%d,%d] = %d, expected %d", i, j, yuv.Cb[j*yuv.CStride+i], u0)
			}
			if yuv.Cr[j*yuv.CStride+i] != v0 {
				t.Errorf("Cr[%d,%d] = %d, expected %d", i, j, yuv.Cr[j*yuv.CStride+i], v0)
			}
		}
	}
}

func TestConvertStandard_SolidGray(t *testing.T) {
	width, height := 2, 2
	rgb := make([]byte, width*height*3)
	for i := 0; i < len(rgb); i += 3 {
		rgb[i] = 128
		rgb[i+1] = 128
		rgb[i+2] = 128
	}

	yuv := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	opts := &Options{
		Matrix:       GetConversionMatrix(MatrixRec601Full),
		TransferType: TransferSRGB,
		SharpEnabled: false,
	}

	err := Convert(rgb, width, height, width*3, yuv, opts)
	if err != nil {
		t.Fatal(err)
	}

	// For gray with full-range Rec601, Y should be ~128, U/V should be ~128.
	y0 := yuv.Y[0]
	if abs32(int32(y0)-128) > 2 {
		t.Errorf("Y for gray128 = %d, expected ~128", y0)
	}
	u0 := yuv.Cb[0]
	if abs32(int32(u0)-128) > 2 {
		t.Errorf("Cb for gray128 = %d, expected ~128", u0)
	}
}

func TestConvertSharp_SolidColor(t *testing.T) {
	width, height := 4, 4
	rgb := make([]byte, width*height*3)
	for i := 0; i < len(rgb); i += 3 {
		rgb[i] = 100
		rgb[i+1] = 150
		rgb[i+2] = 200
	}

	yuv := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	opts := &Options{
		Matrix:       GetConversionMatrix(MatrixWebP),
		TransferType: TransferSRGB,
		SharpEnabled: true,
	}

	err := Convert(rgb, width, height, width*3, yuv, opts)
	if err != nil {
		t.Fatal(err)
	}

	// For a solid color, sharp and standard should produce similar results.
	// All Y values should be identical.
	y0 := yuv.Y[0]
	for j := 0; j < height; j++ {
		for i := 0; i < width; i++ {
			diff := int(yuv.Y[j*yuv.YStride+i]) - int(y0)
			if diff < -2 || diff > 2 {
				t.Errorf("Sharp Y[%d,%d] = %d, expected ~%d", i, j, yuv.Y[j*yuv.YStride+i], y0)
			}
		}
	}
}

func TestConvertSharpVsStandard_Gradient(t *testing.T) {
	// A gradient image should produce valid output from both paths.
	width, height := 8, 8
	rgb := make([]byte, width*height*3)
	for j := 0; j < height; j++ {
		for i := 0; i < width; i++ {
			off := j*width*3 + i*3
			rgb[off+0] = uint8(i * 255 / (width - 1))
			rgb[off+1] = uint8(j * 255 / (height - 1))
			rgb[off+2] = 128
		}
	}

	yuvStd := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	yuvSharp := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)

	matrix := GetConversionMatrix(MatrixWebP)
	optsStd := &Options{Matrix: matrix, TransferType: TransferSRGB, SharpEnabled: false}
	optsSharp := &Options{Matrix: matrix, TransferType: TransferSRGB, SharpEnabled: true}

	if err := Convert(rgb, width, height, width*3, yuvStd, optsStd); err != nil {
		t.Fatal(err)
	}
	if err := Convert(rgb, width, height, width*3, yuvSharp, optsSharp); err != nil {
		t.Fatal(err)
	}

	// Both should produce valid Y values in [0, 255].
	for j := 0; j < height; j++ {
		for i := 0; i < width; i++ {
			ys := yuvStd.Y[j*yuvStd.YStride+i]
			ysh := yuvSharp.Y[j*yuvSharp.YStride+i]
			_ = ys
			_ = ysh
			// Just verify no crash and values are in range (always true for uint8).
		}
	}
}

func TestConvertSharp_OddDimensions(t *testing.T) {
	// Odd dimensions should be handled correctly.
	width, height := 3, 5
	rgb := make([]byte, width*height*3)
	for i := range rgb {
		rgb[i] = 128
	}

	yuv := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	opts := DefaultOptions()

	err := Convert(rgb, width, height, width*3, yuv, opts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConvertSharp_MinDimensions(t *testing.T) {
	// 1x1 image.
	rgb := []byte{255, 0, 0}
	yuv := image.NewYCbCr(image.Rect(0, 0, 1, 1), image.YCbCrSubsampleRatio420)
	opts := DefaultOptions()

	err := Convert(rgb, 1, 1, 3, yuv, opts)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConvertErrors(t *testing.T) {
	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "zero width",
			fn: func() error {
				return Convert([]byte{0, 0, 0}, 0, 1, 3, image.NewYCbCr(image.Rect(0, 0, 1, 1), image.YCbCrSubsampleRatio420), nil)
			},
		},
		{
			name: "nil output",
			fn: func() error {
				return Convert([]byte{0, 0, 0}, 1, 1, 3, nil, nil)
			},
		},
		{
			name: "wrong subsample",
			fn: func() error {
				return Convert([]byte{0, 0, 0}, 1, 1, 3, image.NewYCbCr(image.Rect(0, 0, 1, 1), image.YCbCrSubsampleRatio444), nil)
			},
		},
		{
			name: "nil matrix",
			fn: func() error {
				return Convert([]byte{0, 0, 0}, 1, 1, 3, image.NewYCbCr(image.Rect(0, 0, 1, 1), image.YCbCrSubsampleRatio420), &Options{Matrix: nil})
			},
		},
		{
			name: "buffer too small",
			fn: func() error {
				return Convert([]byte{0}, 1, 1, 3, image.NewYCbCr(image.Rect(0, 0, 1, 1), image.YCbCrSubsampleRatio420), nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestGammaRoundTrip_AllTransfers(t *testing.T) {
	transfers := []struct {
		name string
		tf   TransferFunc
	}{
		{"BT709", TransferBT709},
		{"BT470M", TransferBT470M},
		{"BT470BG", TransferBT470BG},
		{"BT601", TransferBT601},
		{"SMPTE240", TransferSMPTE240},
		{"SRGB", TransferSRGB},
		{"BT2020_10", TransferBT2020_10},
		{"PQ", TransferPQ},
		{"HLG", TransferHLG},
		{"SMPTE428", TransferSMPTE428},
	}

	for _, tc := range transfers {
		t.Run(tc.name, func(t *testing.T) {
			// Test boundaries
			lin0 := GammaToLinear(0, 8, tc.tf)
			lin255 := GammaToLinear(255, 8, tc.tf)
			_ = lin0
			_ = lin255

			// Round-trip for mid-value
			lin128 := GammaToLinear(128, 8, tc.tf)
			back := LinearToGamma(lin128, 8, tc.tf)
			diff := math.Abs(float64(back) - 128)
			if diff > 3 {
				t.Errorf("round-trip 128: linear=%d, back=%d (diff=%.0f)", lin128, back, diff)
			}
		})
	}
}

func TestConvertSharp_2x2(t *testing.T) {
	// 2x2 with distinct colors.
	rgb := []byte{
		255, 0, 0, 0, 255, 0,
		0, 0, 255, 255, 255, 0,
	}

	yuv := image.NewYCbCr(image.Rect(0, 0, 2, 2), image.YCbCrSubsampleRatio420)
	opts := DefaultOptions()

	err := Convert(rgb, 2, 2, 6, yuv, opts)
	if err != nil {
		t.Fatal(err)
	}

	// Just verify it doesn't crash and produces some Y values.
	if yuv.Y[0] == 0 && yuv.Y[1] == 0 && yuv.Y[2] == 0 && yuv.Y[3] == 0 {
		t.Error("all Y values are 0 for non-black input")
	}
}

func BenchmarkConvertStandard(b *testing.B) {
	width, height := 64, 64
	rgb := make([]byte, width*height*3)
	for i := range rgb {
		rgb[i] = uint8(i % 256)
	}
	yuv := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	opts := &Options{
		Matrix:       GetConversionMatrix(MatrixWebP),
		TransferType: TransferSRGB,
		SharpEnabled: false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Convert(rgb, width, height, width*3, yuv, opts)
	}
}

func BenchmarkConvertSharp(b *testing.B) {
	width, height := 64, 64
	rgb := make([]byte, width*height*3)
	for i := range rgb {
		rgb[i] = uint8(i % 256)
	}
	yuv := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	opts := DefaultOptions()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Convert(rgb, width, height, width*3, yuv, opts)
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
