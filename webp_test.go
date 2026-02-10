package webp

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func testdataPath(name string) string {
	return filepath.Join("testdata", name)
}

func readTestFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(testdataPath(name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return data
}

// --- GetFeatures tests ---

func TestGetFeatures_Lossless(t *testing.T) {
	data := readTestFile(t, "red_4x4_lossless.webp")
	feat, err := GetFeatures(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if feat.Width != 4 || feat.Height != 4 {
		t.Errorf("dimensions = %dx%d, want 4x4", feat.Width, feat.Height)
	}
	if feat.Format != "lossless" {
		t.Errorf("format = %q, want %q", feat.Format, "lossless")
	}
	if feat.HasAnimation {
		t.Error("unexpected animation flag")
	}
	if feat.FrameCount != 1 {
		t.Errorf("frame count = %d, want 1", feat.FrameCount)
	}
}

func TestGetFeatures_Lossy(t *testing.T) {
	data := readTestFile(t, "red_4x4_lossy.webp")
	feat, err := GetFeatures(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if feat.Width != 4 || feat.Height != 4 {
		t.Errorf("dimensions = %dx%d, want 4x4", feat.Width, feat.Height)
	}
	if feat.Format != "lossy" {
		t.Errorf("format = %q, want %q", feat.Format, "lossy")
	}
}

func TestGetFeatures_LosslessGradient(t *testing.T) {
	data := readTestFile(t, "gradient_8x8_lossless.webp")
	feat, err := GetFeatures(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if feat.Width != 8 || feat.Height != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", feat.Width, feat.Height)
	}
	if feat.Format != "lossless" {
		t.Errorf("format = %q, want %q", feat.Format, "lossless")
	}
	if !feat.HasAlpha {
		t.Error("expected HasAlpha for RGBA gradient image")
	}
}

// --- DecodeConfig tests ---

func TestDecodeConfig_Lossless(t *testing.T) {
	data := readTestFile(t, "red_4x4_lossless.webp")
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 4 || cfg.Height != 4 {
		t.Errorf("config dimensions = %dx%d, want 4x4", cfg.Width, cfg.Height)
	}
}

func TestDecodeConfig_Lossy(t *testing.T) {
	data := readTestFile(t, "blue_16x16_lossy.webp")
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 16 || cfg.Height != 16 {
		t.Errorf("config dimensions = %dx%d, want 16x16", cfg.Width, cfg.Height)
	}
}

// --- Decode tests ---

func TestDecode_Lossless_Red4x4(t *testing.T) {
	data := readTestFile(t, "red_4x4_lossless.webp")
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 4 || bounds.Dy() != 4 {
		t.Errorf("image size = %dx%d, want 4x4", bounds.Dx(), bounds.Dy())
	}
	// The image should be solid red (255, 0, 0, 255).
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			r8, g8, b8, a8 := uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)
			if r8 != 255 || g8 != 0 || b8 != 0 || a8 != 255 {
				t.Errorf("pixel(%d,%d) = (%d,%d,%d,%d), want (255,0,0,255)", x, y, r8, g8, b8, a8)
			}
		}
	}
}

func TestDecode_Lossless_Gradient8x8(t *testing.T) {
	data := readTestFile(t, "gradient_8x8_lossless.webp")
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 8 || bounds.Dy() != 8 {
		t.Errorf("image size = %dx%d, want 8x8", bounds.Dx(), bounds.Dy())
	}
	// Verify alpha is preserved (source alpha was 200).
	_, _, _, a := img.At(0, 0).RGBA()
	a8 := uint8(a >> 8)
	if a8 != 200 {
		t.Errorf("pixel(0,0) alpha = %d, want 200", a8)
	}
}

func TestDecode_Lossy_Red4x4(t *testing.T) {
	data := readTestFile(t, "red_4x4_lossy.webp")
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 4 || bounds.Dy() != 4 {
		t.Errorf("image size = %dx%d, want 4x4", bounds.Dx(), bounds.Dy())
	}
	// Lossy encoding of solid red: expect red-dominant pixels (lossy may not be exact).
	r0, g0, b0, a0 := img.At(0, 0).RGBA()
	r8, g8, b8, a8 := uint8(r0>>8), uint8(g0>>8), uint8(b0>>8), uint8(a0>>8)
	if r8 < 200 || g8 > 50 || b8 > 50 || a8 != 255 {
		t.Errorf("pixel(0,0) = (%d,%d,%d,%d), want approximately (255,0,0,255)", r8, g8, b8, a8)
	}
}

func TestDecode_Lossy_Blue16x16(t *testing.T) {
	data := readTestFile(t, "blue_16x16_lossy.webp")
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Errorf("image size = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}
	// Lossy encoding of solid blue: expect blue-dominant pixels.
	// Note: VP8 lossy quantization shifts chroma significantly for this file.
	// The reference dwebp produces (1, 128, 255) for the center pixel.
	r0, g0, b0, a0 := img.At(8, 8).RGBA()
	r8, g8, b8, a8 := uint8(r0>>8), uint8(g0>>8), uint8(b0>>8), uint8(a0>>8)
	if b8 < 200 || r8 > 50 || a8 != 255 {
		t.Errorf("pixel(8,8) = (%d,%d,%d,%d), want blue-dominant with B>=200, R<=50", r8, g8, b8, a8)
	}
}

// --- image.RegisterFormat integration ---

func TestImageDecodeFormat(t *testing.T) {
	data := readTestFile(t, "red_4x4_lossless.webp")
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "webp" {
		t.Errorf("format = %q, want %q", format, "webp")
	}
	bounds := img.Bounds()
	if bounds.Dx() != 4 || bounds.Dy() != 4 {
		t.Errorf("dimensions = %dx%d, want 4x4", bounds.Dx(), bounds.Dy())
	}
}

func TestImageDecodeConfigFormat(t *testing.T) {
	data := readTestFile(t, "gradient_8x8_lossless.webp")
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "webp" {
		t.Errorf("format = %q, want %q", format, "webp")
	}
	if cfg.Width != 8 || cfg.Height != 8 {
		t.Errorf("config = %dx%d, want 8x8", cfg.Width, cfg.Height)
	}
}

// --- Error cases ---

func TestDecode_InvalidData(t *testing.T) {
	_, err := Decode(bytes.NewReader([]byte("not a webp file")))
	if err == nil {
		t.Fatal("expected error for invalid data")
	}
}

func TestDecode_Empty(t *testing.T) {
	_, err := Decode(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestDecodeConfig_InvalidData(t *testing.T) {
	_, err := DecodeConfig(bytes.NewReader([]byte{0, 1, 2, 3}))
	if err == nil {
		t.Fatal("expected error for invalid data")
	}
}

func TestGetFeatures_InvalidData(t *testing.T) {
	_, err := GetFeatures(bytes.NewReader([]byte("RIFF")))
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

// --- DecodeConfig color model ---

func TestDecodeConfig_ColorModel_Lossless(t *testing.T) {
	data := readTestFile(t, "gradient_8x8_lossless.webp")
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Lossless with alpha should use NRGBA color model.
	if cfg.ColorModel != color.NRGBAModel {
		t.Errorf("color model = %v, want NRGBAModel", cfg.ColorModel)
	}
}

// --- Tests using libwebp's bundled test.webp (128x128 lossy) ---

const libwebpTestFile = "libwebp/examples/test.webp"

func TestGetFeatures_LibwebpTest(t *testing.T) {
	data, err := os.ReadFile(libwebpTestFile)
	if err != nil {
		t.Skipf("libwebp test file not available: %v", err)
	}
	feat, err := GetFeatures(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if feat.Width != 128 || feat.Height != 128 {
		t.Errorf("dimensions = %dx%d, want 128x128", feat.Width, feat.Height)
	}
	if feat.Format != "lossy" {
		t.Errorf("format = %q, want %q", feat.Format, "lossy")
	}
	if feat.HasAlpha {
		t.Error("unexpected alpha flag for lossy-only image")
	}
	if feat.HasAnimation {
		t.Error("unexpected animation flag")
	}
	if feat.FrameCount != 1 {
		t.Errorf("frame count = %d, want 1", feat.FrameCount)
	}
}

func TestDecodeConfig_LibwebpTest(t *testing.T) {
	data, err := os.ReadFile(libwebpTestFile)
	if err != nil {
		t.Skipf("libwebp test file not available: %v", err)
	}
	cfg, err := DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 128 || cfg.Height != 128 {
		t.Errorf("dimensions = %dx%d, want 128x128", cfg.Width, cfg.Height)
	}
	if cfg.ColorModel != color.YCbCrModel {
		t.Errorf("color model should be YCbCrModel for lossy without alpha")
	}
}

func TestImageDecodeConfig_LibwebpTest(t *testing.T) {
	data, err := os.ReadFile(libwebpTestFile)
	if err != nil {
		t.Skipf("libwebp test file not available: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if format != "webp" {
		t.Errorf("format = %q, want %q", format, "webp")
	}
	if cfg.Width != 128 || cfg.Height != 128 {
		t.Errorf("dimensions = %dx%d, want 128x128", cfg.Width, cfg.Height)
	}
}
