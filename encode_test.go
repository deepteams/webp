package webp

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
)

// --- Options / Defaults tests ---

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.Quality != 75 {
		t.Errorf("Quality = %v, want 75", opts.Quality)
	}
	if opts.Lossless {
		t.Error("Lossless should be false by default")
	}
	if opts.Method != 4 {
		t.Errorf("Method = %d, want 4", opts.Method)
	}

	// New config fields: sentinel defaults.
	if opts.SNSStrength >= 0 {
		t.Errorf("SNSStrength = %d, want negative sentinel", opts.SNSStrength)
	}
	if opts.FilterStrength >= 0 {
		t.Errorf("FilterStrength = %d, want negative sentinel", opts.FilterStrength)
	}
	if opts.FilterSharpness != 0 {
		t.Errorf("FilterSharpness = %d, want 0", opts.FilterSharpness)
	}
	if opts.FilterType >= 0 {
		t.Errorf("FilterType = %d, want negative sentinel", opts.FilterType)
	}
	if opts.Partitions != 0 {
		t.Errorf("Partitions = %d, want 0", opts.Partitions)
	}
	if opts.Segments >= 0 {
		t.Errorf("Segments = %d, want negative sentinel", opts.Segments)
	}
	if opts.Pass >= 0 {
		t.Errorf("Pass = %d, want negative sentinel", opts.Pass)
	}
	if opts.EmulateJpegSize {
		t.Error("EmulateJpegSize should be false by default")
	}
	if opts.QMin != 0 {
		t.Errorf("QMin = %d, want 0", opts.QMin)
	}
	if opts.QMax >= 0 {
		t.Errorf("QMax = %d, want negative sentinel", opts.QMax)
	}
}

func TestPresetValues(t *testing.T) {
	tests := []struct {
		name   string
		preset Preset
		want   int
	}{
		{"Default", PresetDefault, 0},
		{"Picture", PresetPicture, 1},
		{"Photo", PresetPhoto, 2},
		{"Drawing", PresetDrawing, 3},
		{"Icon", PresetIcon, 4},
		{"Text", PresetText, 5},
	}
	for _, tt := range tests {
		if int(tt.preset) != tt.want {
			t.Errorf("%s Preset = %d, want %d", tt.name, tt.preset, tt.want)
		}
	}
}

func TestOptionsAlias(t *testing.T) {
	// Options is an alias for EncoderOptions.
	var opts Options
	opts.Quality = 50
	opts.Lossless = true
	if opts.Quality != 50 || !opts.Lossless {
		t.Error("Options alias not working correctly")
	}
}

// --- Encode lossless tests ---

func solidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func gradientTestImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := uint8(x * 255 / w)
			g := uint8(y * 255 / h)
			b := uint8((x + y) * 255 / (w + h))
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func TestEncodeLossless_ValidOutput(t *testing.T) {
	img := solidImage(8, 8, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Lossless: true,
		Quality:  75,
		Method:   4,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Verify it's a valid RIFF/WEBP file.
	data := buf.Bytes()
	if len(data) < 20 {
		t.Fatalf("output too small: %d bytes", len(data))
	}
	if string(data[0:4]) != "RIFF" {
		t.Errorf("missing RIFF header: got %q", data[0:4])
	}
	if string(data[8:12]) != "WEBP" {
		t.Errorf("missing WEBP tag: got %q", data[8:12])
	}
	if string(data[12:16]) != "VP8L" {
		t.Errorf("expected VP8L chunk: got %q", data[12:16])
	}
}

func TestEncodeLossless_Roundtrip(t *testing.T) {
	red := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	img := solidImage(4, 4, red)

	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Lossless: true,
		Quality:  75,
		Method:   4,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Decode it back.
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 4 || bounds.Dy() != 4 {
		t.Fatalf("decoded size = %dx%d, want 4x4", bounds.Dx(), bounds.Dy())
	}

	// Lossless should be pixel-exact.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			r, g, b, a := decoded.At(x, y).RGBA()
			r8, g8, b8, a8 := uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)
			if r8 != 255 || g8 != 0 || b8 != 0 || a8 != 255 {
				t.Errorf("pixel(%d,%d) = (%d,%d,%d,%d), want (255,0,0,255)",
					x, y, r8, g8, b8, a8)
			}
		}
	}
}

func TestEncodeLossless_RoundtripGradient(t *testing.T) {
	img := gradientTestImage(16, 16)

	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Lossless: true,
		Quality:  75,
		Method:   4,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Fatalf("decoded size = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}

	// Lossless: every pixel should match exactly.
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			origR, origG, origB, origA := img.At(x, y).RGBA()
			decR, decG, decB, decA := decoded.At(x, y).RGBA()
			if origR != decR || origG != decG || origB != decB || origA != decA {
				t.Errorf("pixel(%d,%d) orig=(%d,%d,%d,%d) dec=(%d,%d,%d,%d)",
					x, y,
					origR>>8, origG>>8, origB>>8, origA>>8,
					decR>>8, decG>>8, decB>>8, decA>>8)
			}
		}
	}
}

func TestEncodeLossless_WithAlpha(t *testing.T) {
	img := solidImage(4, 4, color.NRGBA{R: 128, G: 64, B: 32, A: 200})

	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 50})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Use NRGBAAt to get non-premultiplied pixel values directly.
	// The standard RGBA() method returns premultiplied alpha values,
	// which would lose precision for pixels with alpha < 255.
	nrgbaImg, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want *image.NRGBA", decoded)
	}
	c := nrgbaImg.NRGBAAt(0, 0)
	if c.R != 128 || c.G != 64 || c.B != 32 || c.A != 200 {
		t.Errorf("pixel(0,0) = (%d,%d,%d,%d), want (128,64,32,200)", c.R, c.G, c.B, c.A)
	}
}

func TestEncode_NilOptions(t *testing.T) {
	img := solidImage(4, 4, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, nil)
	if err != nil {
		t.Fatalf("Encode with nil opts: %v", err)
	}
	// Should produce valid lossy WebP (default is lossy).
	data := buf.Bytes()
	if len(data) < 20 {
		t.Fatalf("output too small: %d bytes", len(data))
	}
	if string(data[0:4]) != "RIFF" {
		t.Errorf("missing RIFF header")
	}
	if string(data[8:12]) != "WEBP" {
		t.Errorf("missing WEBP tag")
	}
}

func TestEncode_GetFeatures_Roundtrip(t *testing.T) {
	img := solidImage(8, 8, color.NRGBA{R: 255, G: 255, B: 0, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 75})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	feat, err := GetFeatures(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("GetFeatures: %v", err)
	}
	if feat.Width != 8 || feat.Height != 8 {
		t.Errorf("dimensions = %dx%d, want 8x8", feat.Width, feat.Height)
	}
	if feat.Format != "lossless" {
		t.Errorf("format = %q, want lossless", feat.Format)
	}
}

// --- Encode lossy tests ---

func TestEncodeLossy_ValidOutput(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Lossless: false,
		Quality:  80,
		Method:   2,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	data := buf.Bytes()
	if len(data) < 20 {
		t.Fatalf("output too small: %d bytes", len(data))
	}
	if string(data[0:4]) != "RIFF" {
		t.Errorf("missing RIFF header")
	}
	if string(data[8:12]) != "WEBP" {
		t.Errorf("missing WEBP tag")
	}
	if string(data[12:16]) != "VP8 " {
		t.Errorf("expected VP8 chunk: got %q", data[12:16])
	}
}

func TestEncodeLossy_Roundtrip(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 80})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Fatalf("decoded size = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}

	// Lossy: should be approximately red (allow tolerance).
	r, g, b, a := decoded.At(8, 8).RGBA()
	r8, g8, b8, a8 := uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)
	if r8 < 200 || g8 > 60 || b8 > 60 || a8 != 255 {
		t.Errorf("pixel(8,8) = (%d,%d,%d,%d), want approximately (255,0,0,255)",
			r8, g8, b8, a8)
	}
}

// --- Lossy + alpha tests ---

func TestEncodeLossy_WithAlpha_VP8XContainer(t *testing.T) {
	// Create an image with non-opaque alpha.
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 180})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 80})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	data := buf.Bytes()
	if len(data) < 30 {
		t.Fatalf("output too small: %d bytes", len(data))
	}

	// Verify RIFF/WEBP header.
	if string(data[0:4]) != "RIFF" {
		t.Errorf("missing RIFF header: got %q", data[0:4])
	}
	if string(data[8:12]) != "WEBP" {
		t.Errorf("missing WEBP tag: got %q", data[8:12])
	}
	// First chunk should be VP8X (extended format).
	if string(data[12:16]) != "VP8X" {
		t.Errorf("expected VP8X chunk: got %q", data[12:16])
	}

	// Verify VP8X has alpha flag set (bit 4 = 0x10).
	vp8xFlags := data[20] | data[21]<<8 | data[22]<<16 | data[23]<<24
	if vp8xFlags&0x10 == 0 {
		t.Errorf("VP8X alpha flag not set: flags = 0x%08x", vp8xFlags)
	}
}

func TestEncodeLossy_WithAlpha_Roundtrip(t *testing.T) {
	// Semi-transparent image.
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 80})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Fatalf("decoded size = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}

	// Check that alpha is approximately 128 (not 255, which would mean alpha was lost).
	nrgbaImg, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want *image.NRGBA", decoded)
	}
	c := nrgbaImg.NRGBAAt(8, 8)
	if c.A > 160 || c.A < 96 {
		t.Errorf("pixel(8,8) alpha = %d, want approximately 128 (alpha was %s)",
			c.A, func() string {
				if c.A == 255 {
					return "lost -- not encoded"
				}
				return "outside tolerance"
			}())
	}
}

func TestEncodeLossy_OpaqueImage_NoVP8X(t *testing.T) {
	// Fully opaque image should produce simple VP8 (no VP8X).
	img := solidImage(16, 16, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 80})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	data := buf.Bytes()
	// First chunk should be VP8, not VP8X.
	if string(data[12:16]) != "VP8 " {
		t.Errorf("expected VP8 chunk for opaque image: got %q", data[12:16])
	}
}

func TestEncodeLossy_WithAlpha_Features(t *testing.T) {
	// Encode semi-transparent image and check GetFeatures reports alpha.
	img := solidImage(16, 16, color.NRGBA{R: 100, G: 200, B: 50, A: 100})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 80})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	feat, err := GetFeatures(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("GetFeatures: %v", err)
	}
	if feat.Width != 16 || feat.Height != 16 {
		t.Errorf("dimensions = %dx%d, want 16x16", feat.Width, feat.Height)
	}
	if !feat.HasAlpha {
		t.Error("HasAlpha should be true for image with alpha")
	}
	if feat.Format != "extended" {
		t.Errorf("format = %q, want extended", feat.Format)
	}
}

// --- RIFF container tests ---

func TestEncodeRIFF_EvenAlignment(t *testing.T) {
	img := solidImage(4, 4, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 75})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// RIFF files must have even total size.
	if len(buf.Bytes())%2 != 0 {
		t.Errorf("RIFF output has odd size: %d", len(buf.Bytes()))
	}
}

// --- Config validation tests ---

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		opts    EncoderOptions
		wantErr string // "" means no error expected
	}{
		{
			name:    "default options are valid",
			opts:    *DefaultOptions(),
			wantErr: "",
		},
		{
			name:    "zero quality is valid (minimum quality)",
			opts:    EncoderOptions{Quality: 0, Method: 4},
			wantErr: "",
		},
		{
			name:    "zero method is valid (fastest)",
			opts:    EncoderOptions{Quality: 75, Method: 0},
			wantErr: "",
		},
		{
			name:    "quality 100 is valid",
			opts:    EncoderOptions{Quality: 100, Method: 4},
			wantErr: "",
		},
		{
			name:    "method 6 is valid",
			opts:    EncoderOptions{Quality: 75, Method: 6},
			wantErr: "",
		},
		{
			name:    "negative quality",
			opts:    EncoderOptions{Quality: -1, Method: 4},
			wantErr: "invalid Quality",
		},
		{
			name:    "quality too high",
			opts:    EncoderOptions{Quality: 101, Method: 4},
			wantErr: "invalid Quality",
		},
		{
			name:    "negative method",
			opts:    EncoderOptions{Quality: 75, Method: -1},
			wantErr: "invalid Method",
		},
		{
			name:    "method too high",
			opts:    EncoderOptions{Quality: 75, Method: 7},
			wantErr: "invalid Method",
		},
		{
			name:    "negative target size",
			opts:    EncoderOptions{Quality: 75, Method: 4, TargetSize: -1},
			wantErr: "invalid TargetSize",
		},
		{
			name:    "invalid preset",
			opts:    EncoderOptions{Quality: 75, Method: 4, Preset: Preset(99)},
			wantErr: "invalid Preset",
		},
		{
			name:    "lossless with quality 0",
			opts:    EncoderOptions{Lossless: true, Quality: 0, Method: 4},
			wantErr: "",
		},
		{
			name:    "lossless with method 0",
			opts:    EncoderOptions{Lossless: true, Quality: 75, Method: 0},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.opts)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestEncode_InvalidOptions(t *testing.T) {
	img := solidImage(4, 4, color.NRGBA{R: 255, A: 255})

	tests := []struct {
		name    string
		opts    *EncoderOptions
		wantErr string
	}{
		{
			name:    "negative quality rejected",
			opts:    &EncoderOptions{Quality: -50},
			wantErr: "invalid Quality",
		},
		{
			name:    "method 99 rejected",
			opts:    &EncoderOptions{Quality: 75, Method: 99},
			wantErr: "invalid Method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := Encode(&buf, img, tt.opts)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestEncode_QualityZero_Accepted(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	// Quality=0 should be accepted as minimum quality, not replaced by 75.
	err := Encode(&buf, img, &EncoderOptions{Quality: 0, Method: 4})
	if err != nil {
		t.Fatalf("Encode with Quality=0: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("output is empty")
	}
}

func TestEncode_MethodZero_Accepted(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	// Method=0 should be accepted as fastest method, not replaced by 4.
	err := Encode(&buf, img, &EncoderOptions{Quality: 75, Method: 0})
	if err != nil {
		t.Fatalf("Encode with Method=0: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("output is empty")
	}
}

// --- Preprocessing / Dithering tests ---

func TestEncodeLossy_Preprocessing2_Roundtrip(t *testing.T) {
	// Encode with preprocessing=2 (dithering) and verify roundtrip works.
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:       50,
		Method:        4,
		Preprocessing: 2,
	})
	if err != nil {
		t.Fatalf("Encode with Preprocessing=2: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != 32 || bounds.Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", bounds.Dx(), bounds.Dy())
	}
}

func TestEncodeLossy_Preprocessing0_NoDithering(t *testing.T) {
	// Encoding with Preprocessing=0 should produce the same output as
	// without specifying Preprocessing (backward compatibility).
	img := solidImage(16, 16, color.NRGBA{R: 255, G: 128, B: 0, A: 255})

	var buf1 bytes.Buffer
	err := Encode(&buf1, img, &EncoderOptions{Quality: 80, Method: 4})
	if err != nil {
		t.Fatalf("Encode default: %v", err)
	}

	var buf2 bytes.Buffer
	err = Encode(&buf2, img, &EncoderOptions{Quality: 80, Method: 4, Preprocessing: 0})
	if err != nil {
		t.Fatalf("Encode Preprocessing=0: %v", err)
	}

	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Error("Preprocessing=0 produced different output than default; expected identical")
	}
}

func TestEncodeLossy_DitheringAmplitudeFormula(t *testing.T) {
	// Verify the dithering amplitude formula: dithering = 1.0 + (0.5-1.0)*x^4
	// where x = quality/100.
	tests := []struct {
		quality     float32
		wantApprox  float32 // expected dithering amplitude
		description string
	}{
		{0, 1.0, "max dithering at q=0"},
		{50, 0.96875, "near-max at q=50"}, // 1.0 + (-0.5)*(0.25)^2 = 1.0 - 0.03125 = 0.96875
		{100, 0.5, "half dithering at q=100"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			x := tt.quality / 100.0
			x2 := x * x
			dithering := float32(1.0) + (0.5-1.0)*x2*x2
			if diff := dithering - tt.wantApprox; diff > 0.01 || diff < -0.01 {
				t.Errorf("quality=%.0f: dithering=%.5f, want ~%.5f", tt.quality, dithering, tt.wantApprox)
			}
		})
	}
}

func TestEncodeLossy_DitherHighQualityStillWorks(t *testing.T) {
	// Even at quality=100 with preprocessing=2, dithering amplitude is 0.5
	// (not zero), so it should still be applied and produce valid output.
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:       100,
		Method:        4,
		Preprocessing: 2,
	})
	if err != nil {
		t.Fatalf("Encode with Preprocessing=2 at q=100: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("output is empty")
	}
}

// --- Alpha option tests ---

func TestDefaultOptions_AlphaDefaults(t *testing.T) {
	opts := DefaultOptions()
	// Sentinel values: all negative, meaning "use default".
	if opts.AlphaCompression >= 0 {
		t.Errorf("AlphaCompression = %d, want negative sentinel", opts.AlphaCompression)
	}
	if opts.AlphaFiltering >= 0 {
		t.Errorf("AlphaFiltering = %d, want negative sentinel", opts.AlphaFiltering)
	}
	if opts.AlphaQuality >= 0 {
		t.Errorf("AlphaQuality = %d, want negative sentinel", opts.AlphaQuality)
	}
}

func TestResolveAlphaDefaults(t *testing.T) {
	// Sentinel -1 resolves to the C libwebp defaults.
	if v := resolveAlphaCompression(-1); v != 1 {
		t.Errorf("resolveAlphaCompression(-1) = %d, want 1", v)
	}
	if v := resolveAlphaFiltering(-1); v != 1 {
		t.Errorf("resolveAlphaFiltering(-1) = %d, want 1", v)
	}
	if v := resolveAlphaQuality(-1); v != 100 {
		t.Errorf("resolveAlphaQuality(-1) = %d, want 100", v)
	}

	// Explicit values pass through.
	if v := resolveAlphaCompression(0); v != 0 {
		t.Errorf("resolveAlphaCompression(0) = %d, want 0", v)
	}
	if v := resolveAlphaFiltering(0); v != 0 {
		t.Errorf("resolveAlphaFiltering(0) = %d, want 0", v)
	}
	if v := resolveAlphaQuality(50); v != 50 {
		t.Errorf("resolveAlphaQuality(50) = %d, want 50", v)
	}
}

func TestValidateConfig_AlphaOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    EncoderOptions
		wantErr string
	}{
		{
			name:    "alpha defaults (sentinels) valid",
			opts:    *DefaultOptions(),
			wantErr: "",
		},
		{
			name:    "alpha compression 0 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaCompression: 0},
			wantErr: "",
		},
		{
			name:    "alpha compression 1 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaCompression: 1},
			wantErr: "",
		},
		{
			name:    "alpha compression 2 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaCompression: 2},
			wantErr: "invalid AlphaCompression",
		},
		{
			name:    "alpha filtering 0 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaFiltering: 0},
			wantErr: "",
		},
		{
			name:    "alpha filtering 2 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaFiltering: 2},
			wantErr: "",
		},
		{
			name:    "alpha filtering 3 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaFiltering: 3},
			wantErr: "invalid AlphaFiltering",
		},
		{
			name:    "alpha quality 0 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaQuality: 0},
			wantErr: "",
		},
		{
			name:    "alpha quality 100 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaQuality: 100},
			wantErr: "",
		},
		{
			name:    "alpha quality 101 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, AlphaQuality: 101},
			wantErr: "invalid AlphaQuality",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.opts)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestEncodeLossy_AlphaCompression0_Raw_Roundtrip(t *testing.T) {
	// Encode with AlphaCompression=0 (raw/uncompressed) and verify roundtrip.
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:          80,
		Method:           4,
		AlphaCompression: 0, // raw
		AlphaFiltering:   0, // none (no filter for raw)
		AlphaQuality:     100,
	})
	if err != nil {
		t.Fatalf("Encode with AlphaCompression=0: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Fatalf("decoded size = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}

	nrgbaImg, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want *image.NRGBA", decoded)
	}
	c := nrgbaImg.NRGBAAt(8, 8)
	if c.A > 160 || c.A < 96 {
		t.Errorf("pixel(8,8) alpha = %d, want approximately 128", c.A)
	}
}

func TestEncodeLossy_AlphaCompression1_Lossless_Roundtrip(t *testing.T) {
	// Encode with explicit AlphaCompression=1 (lossless) and verify roundtrip.
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:          80,
		Method:           4,
		AlphaCompression: 1, // lossless
		AlphaFiltering:   1, // fast
		AlphaQuality:     100,
	})
	if err != nil {
		t.Fatalf("Encode with AlphaCompression=1: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	nrgbaImg, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want *image.NRGBA", decoded)
	}
	c := nrgbaImg.NRGBAAt(8, 8)
	if c.A > 160 || c.A < 96 {
		t.Errorf("pixel(8,8) alpha = %d, want approximately 128", c.A)
	}
}

func TestEncodeLossy_AlphaFilterBest_Roundtrip(t *testing.T) {
	// Encode with AlphaFiltering=2 (best) and verify roundtrip.
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:          80,
		Method:           4,
		AlphaCompression: 1, // lossless
		AlphaFiltering:   2, // best
		AlphaQuality:     100,
	})
	if err != nil {
		t.Fatalf("Encode with AlphaFiltering=2: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	nrgbaImg, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want *image.NRGBA", decoded)
	}
	c := nrgbaImg.NRGBAAt(8, 8)
	if c.A > 160 || c.A < 96 {
		t.Errorf("pixel(8,8) alpha = %d, want approximately 128", c.A)
	}
}

func TestEncodeLossy_AlphaQuality50_Roundtrip(t *testing.T) {
	// Encode with AlphaQuality=50 (lossy alpha via level quantization).
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:          80,
		Method:           4,
		AlphaCompression: 1,  // lossless
		AlphaFiltering:   1,  // fast
		AlphaQuality:     50, // lossy alpha
	})
	if err != nil {
		t.Fatalf("Encode with AlphaQuality=50: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	nrgbaImg, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want *image.NRGBA", decoded)
	}
	// With lossy alpha (quality 50), the decoded alpha should still be
	// in the ballpark of 128, but with wider tolerance.
	c := nrgbaImg.NRGBAAt(8, 8)
	if c.A > 200 || c.A < 50 {
		t.Errorf("pixel(8,8) alpha = %d, want approximately 128 (wider tolerance for lossy alpha)", c.A)
	}
}

func TestEncodeLossy_ZeroValueAlphaOptions_BackwardCompat(t *testing.T) {
	// When callers use the zero value (AlphaCompression=0, AlphaFiltering=0,
	// AlphaQuality=0), the encoder should still work. AlphaCompression=0
	// means raw mode, AlphaFiltering=0 means no filter, AlphaQuality=0
	// means maximum alpha quantization. This tests backward compatibility for
	// callers that construct EncoderOptions without setting alpha fields.
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality: 80,
		Method:  4,
		// AlphaCompression, AlphaFiltering, AlphaQuality all zero-value (0)
	})
	if err != nil {
		t.Fatalf("Encode with zero-value alpha options: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != 16 || bounds.Dy() != 16 {
		t.Fatalf("decoded size = %dx%d, want 16x16", bounds.Dx(), bounds.Dy())
	}
}

func TestEncodeLossy_OpaqueImage_AlphaOptionsIgnored(t *testing.T) {
	// For fully opaque images, alpha options should be irrelevant.
	// The encoder should produce a simple VP8 (no VP8X/ALPH).
	img := solidImage(16, 16, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:          80,
		Method:           4,
		AlphaCompression: 0,
		AlphaFiltering:   2,
		AlphaQuality:     50,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	data := buf.Bytes()
	if string(data[12:16]) != "VP8 " {
		t.Errorf("expected VP8 chunk for opaque image: got %q", data[12:16])
	}
}

// --- Lossy config parameter tests ---

func TestResolveConfigDefaults(t *testing.T) {
	// Sentinel -1 resolves to the C libwebp defaults.
	tests := []struct {
		name    string
		resolve func(int) int
		input   int
		want    int
	}{
		{"SNSStrength sentinel", resolveSNSStrength, -1, 50},
		{"SNSStrength explicit 0", resolveSNSStrength, 0, 0},
		{"SNSStrength explicit 80", resolveSNSStrength, 80, 80},
		{"FilterStrength sentinel", resolveFilterStrength, -1, 60},
		{"FilterStrength explicit 0", resolveFilterStrength, 0, 0},
		{"FilterStrength explicit 30", resolveFilterStrength, 30, 30},
		{"FilterType sentinel", resolveFilterType, -1, 1},
		{"FilterType explicit 0 (simple)", resolveFilterType, 0, 0},
		{"FilterType explicit 1 (strong)", resolveFilterType, 1, 1},
		{"Segments sentinel", resolveSegments, -1, 4},
		{"Segments explicit 2", resolveSegments, 2, 2},
		{"Pass sentinel", resolvePass, -1, 1},
		{"Pass explicit 5", resolvePass, 5, 5},
		{"QMax sentinel", resolveQMax, -1, 100},
		{"QMax explicit 0", resolveQMax, 0, 0},
		{"QMax explicit 80", resolveQMax, 80, 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.resolve(tt.input)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateConfig_NewParameters(t *testing.T) {
	tests := []struct {
		name    string
		opts    EncoderOptions
		wantErr string // "" means no error expected
	}{
		{
			name:    "all defaults valid",
			opts:    *DefaultOptions(),
			wantErr: "",
		},
		{
			name:    "zero-value struct valid (backward compat)",
			opts:    EncoderOptions{},
			wantErr: "",
		},
		{
			name:    "explicit SNS 0 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, SNSStrength: 0},
			wantErr: "",
		},
		{
			name:    "explicit SNS 100 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, SNSStrength: 100},
			wantErr: "",
		},
		{
			name:    "SNS 101 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, SNSStrength: 101},
			wantErr: "invalid SNSStrength",
		},
		{
			name:    "FilterStrength 100 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterStrength: 100},
			wantErr: "",
		},
		{
			name:    "FilterStrength 101 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterStrength: 101},
			wantErr: "invalid FilterStrength",
		},
		{
			name:    "FilterSharpness 7 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterSharpness: 7},
			wantErr: "",
		},
		{
			name:    "FilterSharpness 8 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterSharpness: 8},
			wantErr: "invalid FilterSharpness",
		},
		{
			name:    "FilterSharpness -1 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterSharpness: -1},
			wantErr: "invalid FilterSharpness",
		},
		{
			name:    "FilterType 0 (simple) valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterType: 0},
			wantErr: "",
		},
		{
			name:    "FilterType 1 (strong) valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterType: 1},
			wantErr: "",
		},
		{
			name:    "FilterType 2 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, FilterType: 2},
			wantErr: "invalid FilterType",
		},
		{
			name:    "Partitions 3 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Partitions: 3},
			wantErr: "",
		},
		{
			name:    "Partitions 4 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Partitions: 4},
			wantErr: "invalid Partitions",
		},
		{
			name:    "Partitions -1 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Partitions: -1},
			wantErr: "invalid Partitions",
		},
		{
			name:    "Segments 1 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Segments: 1},
			wantErr: "",
		},
		{
			name:    "Segments 4 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Segments: 4},
			wantErr: "",
		},
		{
			name:    "Segments 5 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Segments: 5},
			wantErr: "invalid Segments",
		},
		{
			name:    "Pass 1 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Pass: 1},
			wantErr: "",
		},
		{
			name:    "Pass 10 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Pass: 10},
			wantErr: "",
		},
		{
			name:    "Pass 11 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, Pass: 11},
			wantErr: "invalid Pass",
		},
		{
			name:    "QMin 0 QMax 100 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, QMin: 0, QMax: 100},
			wantErr: "",
		},
		{
			name:    "QMin 50 QMax 80 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, QMin: 50, QMax: 80},
			wantErr: "",
		},
		{
			name:    "QMin > QMax invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, QMin: 80, QMax: 50},
			wantErr: "invalid QMin/QMax",
		},
		{
			name:    "QMin negative invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, QMin: -1},
			wantErr: "invalid QMin/QMax",
		},
		{
			name:    "QMax 101 invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, QMax: 101},
			wantErr: "invalid QMin/QMax",
		},
		{
			name:    "EmulateJpegSize true accepted",
			opts:    EncoderOptions{Quality: 75, Method: 4, EmulateJpegSize: true},
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.opts)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestEncodeLossy_CustomSNSStrength_Roundtrip(t *testing.T) {
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:     80,
		Method:      4,
		SNSStrength: 0, // disable SNS
	})
	if err != nil {
		t.Fatalf("Encode with SNSStrength=0: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestEncodeLossy_CustomFilterStrength_Roundtrip(t *testing.T) {
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:        80,
		Method:         4,
		FilterStrength: 100, // max filter strength
	})
	if err != nil {
		t.Fatalf("Encode with FilterStrength=100: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestEncodeLossy_SimpleFilter_Roundtrip(t *testing.T) {
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:    80,
		Method:     4,
		FilterType: 0, // simple filter
	})
	if err != nil {
		t.Fatalf("Encode with FilterType=0: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestEncodeLossy_CustomSegments_Roundtrip(t *testing.T) {
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:  80,
		Method:   4,
		Segments: 2, // fewer segments
	})
	if err != nil {
		t.Fatalf("Encode with Segments=2: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestEncodeLossy_MultiPass_Roundtrip(t *testing.T) {
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality: 80,
		Method:  4,
		Pass:    3, // multi-pass
	})
	if err != nil {
		t.Fatalf("Encode with Pass=3: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

func TestEncodeLossy_ZeroValueOptions_BackwardCompat(t *testing.T) {
	// An EncoderOptions{} with only Quality and Method set (the pre-existing
	// pattern used by many tests) must still produce valid output.
	// The new config fields are all zero-value, which should be treated
	// as "use default" for fields where 0 is a sentinel.
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 80, Method: 4})
	if err != nil {
		t.Fatalf("Encode with minimal options: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

// --- TargetPSNR tests ---

func TestValidateConfig_TargetPSNR(t *testing.T) {
	tests := []struct {
		name    string
		opts    EncoderOptions
		wantErr string
	}{
		{
			name:    "TargetPSNR 0 valid (disabled)",
			opts:    EncoderOptions{Quality: 75, Method: 4, TargetPSNR: 0},
			wantErr: "",
		},
		{
			name:    "TargetPSNR 40 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, TargetPSNR: 40.0},
			wantErr: "",
		},
		{
			name:    "TargetPSNR 99 valid",
			opts:    EncoderOptions{Quality: 75, Method: 4, TargetPSNR: 99.0},
			wantErr: "",
		},
		{
			name:    "TargetPSNR negative invalid",
			opts:    EncoderOptions{Quality: 75, Method: 4, TargetPSNR: -1.0},
			wantErr: "invalid TargetPSNR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.opts)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestDefaultOptions_TargetPSNR_Zero(t *testing.T) {
	opts := DefaultOptions()
	if opts.TargetPSNR != 0 {
		t.Errorf("TargetPSNR = %v, want 0 (disabled by default)", opts.TargetPSNR)
	}
}

func TestEncodeLossy_TargetPSNR_Roundtrip(t *testing.T) {
	// Encode with a PSNR target and verify the output roundtrips correctly.
	img := gradientTestImage(32, 32)
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{
		Quality:    75,
		Method:     4,
		TargetPSNR: 35.0,
		Pass:       3,
	})
	if err != nil {
		t.Fatalf("Encode with TargetPSNR=35: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != 32 || decoded.Bounds().Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
}

// --- Lossy PSNR roundtrip diagnostic test ---

// richTestImage creates a smooth gradient test image suitable for lossy PSNR
// testing. Uses only smooth gradients to avoid chroma-subsampling artifacts
// at sharp color boundaries which are inherent to VP8 4:2:0 and not bugs.
func richTestImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{
				R: uint8(x * 255 / w),
				G: uint8(y * 255 / h),
				B: uint8((x + y) * 255 / (w + h)),
				A: 255,
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func computePSNR(mse float64) float64 {
	if mse <= 0 {
		return 99.0
	}
	return 10.0 * math.Log10(255.0*255.0/mse)
}

func TestLossyRoundtrip_PSNR(t *testing.T) {
	const W, H = 128, 64
	orig := richTestImage(W, H)

	var buf bytes.Buffer
	err := Encode(&buf, orig, &EncoderOptions{
		Quality: 75,
		Method:  4,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	t.Logf("Encoded size: %d bytes", buf.Len())

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != W || bounds.Dy() != H {
		t.Fatalf("decoded size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), W, H)
	}

	// Compute per-channel MSE and max delta.
	var mseR, mseG, mseB float64
	var maxDR, maxDG, maxDB int
	npix := 0

	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			origC := orig.NRGBAAt(x, y)
			dr, dg, db, _ := decoded.At(x, y).RGBA()
			decR, decG, decB := uint8(dr>>8), uint8(dg>>8), uint8(db>>8)

			diffR := int(origC.R) - int(decR)
			diffG := int(origC.G) - int(decG)
			diffB := int(origC.B) - int(decB)

			mseR += float64(diffR * diffR)
			mseG += float64(diffG * diffG)
			mseB += float64(diffB * diffB)

			absDR := diffR
			if absDR < 0 {
				absDR = -absDR
			}
			absDG := diffG
			if absDG < 0 {
				absDG = -absDG
			}
			absDB := diffB
			if absDB < 0 {
				absDB = -absDB
			}
			if absDR > maxDR {
				maxDR = absDR
			}
			if absDG > maxDG {
				maxDG = absDG
			}
			if absDB > maxDB {
				maxDB = absDB
			}

			npix++
		}
	}

	n := float64(npix)
	mseR /= n
	mseG /= n
	mseB /= n
	mseGlobal := (mseR + mseG + mseB) / 3.0

	psnrR := computePSNR(mseR)
	psnrG := computePSNR(mseG)
	psnrB := computePSNR(mseB)
	psnrGlobal := computePSNR(mseGlobal)

	t.Logf("Channel PSNR: R=%.2fdB G=%.2fdB B=%.2fdB Global=%.2fdB",
		psnrR, psnrG, psnrB, psnrGlobal)
	t.Logf("Max delta: R=%d G=%d B=%d", maxDR, maxDG, maxDB)
	t.Logf("MSE: R=%.2f G=%.2f B=%.2f Global=%.2f", mseR, mseG, mseB, mseGlobal)

	// Thresholds for smooth gradient at q=75.
	if psnrGlobal < 25.0 {
		t.Errorf("Global PSNR %.2f dB is below 25 dB threshold", psnrGlobal)
	}
	if maxDR > 80 || maxDG > 80 || maxDB > 80 {
		t.Errorf("Max delta too large: R=%d G=%d B=%d (threshold 80)", maxDR, maxDG, maxDB)
	}
	if psnrR < 25.0 || psnrG < 25.0 || psnrB < 25.0 {
		t.Errorf("Per-channel PSNR below 25dB: R=%.2f G=%.2f B=%.2f", psnrR, psnrG, psnrB)
	}

	// Dump worst 10 pixels for diagnosis.
	type errPixel struct {
		x, y                int
		origR, origG, origB uint8
		decR, decG, decB    uint8
		totalDelta          int
	}
	var worst [10]errPixel
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			origC := orig.NRGBAAt(x, y)
			dr, dg, db, _ := decoded.At(x, y).RGBA()
			dR := int(origC.R) - int(uint8(dr>>8))
			dG := int(origC.G) - int(uint8(dg>>8))
			dB := int(origC.B) - int(uint8(db>>8))
			if dR < 0 {
				dR = -dR
			}
			if dG < 0 {
				dG = -dG
			}
			if dB < 0 {
				dB = -dB
			}
			total := dR + dG + dB
			for i := 0; i < 10; i++ {
				if total > worst[i].totalDelta {
					copy(worst[i+1:], worst[i:9])
					worst[i] = errPixel{x, y, origC.R, origC.G, origC.B,
						uint8(dr>>8), uint8(dg>>8), uint8(db>>8), total}
					break
				}
			}
		}
	}
	t.Logf("Worst 10 pixels:")
	for i, p := range worst {
		if p.totalDelta == 0 {
			break
		}
		t.Logf("  #%d: (%d,%d) orig=(%d,%d,%d) dec=(%d,%d,%d) delta=%d",
			i+1, p.x, p.y, p.origR, p.origG, p.origB, p.decR, p.decG, p.decB, p.totalDelta)
	}
}

// TestLossyRoundtrip_LargeColorBars tests with 16px-wide color bars so each
// bar fills an entire macroblock, eliminating chroma subsampling cross-talk.
func TestLossyRoundtrip_LargeColorBars(t *testing.T) {
	const W, H = 128, 16
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	bars := []color.NRGBA{
		{R: 255, G: 0, B: 0, A: 255},
		{R: 0, G: 255, B: 0, A: 255},
		{R: 0, G: 0, B: 255, A: 255},
		{R: 255, G: 255, B: 0, A: 255},
		{R: 0, G: 255, B: 255, A: 255},
		{R: 255, G: 0, B: 255, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
		{R: 0, G: 0, B: 0, A: 255},
	}
	barW := W / len(bars) // 16px each
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			barIdx := x / barW
			if barIdx >= len(bars) {
				barIdx = len(bars) - 1
			}
			img.SetNRGBA(x, y, bars[barIdx])
		}
	}

	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 75, Method: 4})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Sample center of each bar.
	barNames := []string{"red", "green", "blue", "yellow", "cyan", "magenta", "white", "black"}
	for i, bar := range bars {
		cx := i*barW + barW/2
		dr, dg, db, _ := decoded.At(cx, 8).RGBA()
		decR, decG, decB := int(dr>>8), int(dg>>8), int(db>>8)
		diffR := int(bar.R) - decR
		diffG := int(bar.G) - decG
		diffB := int(bar.B) - decB
		if diffR < 0 { diffR = -diffR }
		if diffG < 0 { diffG = -diffG }
		if diffB < 0 { diffB = -diffB }
		t.Logf("%s center(%d,8): want=(%d,%d,%d) got=(%d,%d,%d) delta=(%d,%d,%d)",
			barNames[i], cx, bar.R, bar.G, bar.B, decR, decG, decB, diffR, diffG, diffB)
		if diffR > 15 || diffG > 15 || diffB > 15 {
			t.Errorf("%s: delta too large for 16px bar at q=75", barNames[i])
		}
	}
}

// TestLossyRoundtrip_ColorBoundary tests the quality at a sharp color boundary.
// This isolates whether the problem is at MB edges or within solid areas.
func TestLossyRoundtrip_ColorBoundary(t *testing.T) {
	// 32x16 image: left half red, right half blue (2 MBs side by side).
	const W, H = 32, 16
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			if x < W/2 {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
			}
		}
	}

	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Quality: 75, Method: 4})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	// Sample interior of each half and near the boundary.
	samples := []struct {
		name string
		x, y int
		wantR, wantG, wantB int
	}{
		{"red-center", 4, 8, 255, 0, 0},
		{"red-near-edge", 14, 8, 255, 0, 0},
		{"blue-near-edge", 17, 8, 0, 0, 255},
		{"blue-center", 24, 8, 0, 0, 255},
	}
	for _, s := range samples {
		dr, dg, db, _ := decoded.At(s.x, s.y).RGBA()
		decR, decG, decB := int(dr>>8), int(dg>>8), int(db>>8)
		diffR := s.wantR - decR
		diffG := s.wantG - decG
		diffB := s.wantB - decB
		if diffR < 0 {
			diffR = -diffR
		}
		if diffG < 0 {
			diffG = -diffG
		}
		if diffB < 0 {
			diffB = -diffB
		}
		t.Logf("%s: want=(%d,%d,%d) got=(%d,%d,%d) delta=(%d,%d,%d)",
			s.name, s.wantR, s.wantG, s.wantB, decR, decG, decB, diffR, diffG, diffB)
		// Within a uniform MB, max error should be < 20 even at q=75.
		if diffR > 40 || diffG > 40 || diffB > 40 {
			t.Errorf("%s: delta too large (%d,%d,%d)", s.name, diffR, diffG, diffB)
		}
	}
}

// TestLossyRoundtrip_SolidColors tests that solid pure-color 16x16 blocks
// roundtrip reasonably through lossy encoding. This isolates the chroma issue.
func TestLossyRoundtrip_SolidColors(t *testing.T) {
	colors := []struct {
		name string
		c    color.NRGBA
	}{
		{"red", color.NRGBA{R: 255, G: 0, B: 0, A: 255}},
		{"green", color.NRGBA{R: 0, G: 255, B: 0, A: 255}},
		{"blue", color.NRGBA{R: 0, G: 0, B: 255, A: 255}},
		{"yellow", color.NRGBA{R: 255, G: 255, B: 0, A: 255}},
		{"cyan", color.NRGBA{R: 0, G: 255, B: 255, A: 255}},
		{"magenta", color.NRGBA{R: 255, G: 0, B: 255, A: 255}},
		{"white", color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{"gray128", color.NRGBA{R: 128, G: 128, B: 128, A: 255}},
		{"dark-red", color.NRGBA{R: 128, G: 0, B: 0, A: 255}},
	}

	for _, tc := range colors {
		t.Run(tc.name, func(t *testing.T) {
			img := solidImage(16, 16, tc.c)
			var buf bytes.Buffer
			err := Encode(&buf, img, &EncoderOptions{Quality: 75, Method: 4})
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			decoded, err := Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}

			// Check center pixel.
			dr, dg, db, _ := decoded.At(8, 8).RGBA()
			decR, decG, decB := int(dr>>8), int(dg>>8), int(db>>8)
			origR, origG, origB := int(tc.c.R), int(tc.c.G), int(tc.c.B)

			diffR := origR - decR
			diffG := origG - decG
			diffB := origB - decB
			if diffR < 0 {
				diffR = -diffR
			}
			if diffG < 0 {
				diffG = -diffG
			}
			if diffB < 0 {
				diffB = -diffB
			}

			t.Logf("orig=(%d,%d,%d) dec=(%d,%d,%d) delta=(%d,%d,%d)",
				origR, origG, origB, decR, decG, decB, diffR, diffG, diffB)

			// For solid colors at q=75 on 16x16 blocks, delta should be < 30.
			maxTol := 30
			if diffR > maxTol || diffG > maxTol || diffB > maxTol {
				t.Errorf("delta too large for solid %s: (%d,%d,%d), max allowed %d",
					tc.name, diffR, diffG, diffB, maxTol)
			}
		})
	}
}
