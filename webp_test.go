package webp

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/deepteams/webp/animation"
	"github.com/deepteams/webp/mux"
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

// --- image.RegisterFormat integration (lossy) ---

func TestImageDecodeFormat_Lossy(t *testing.T) {
	data := readTestFile(t, "red_4x4_lossy.webp")
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

func TestImageDecode_ViaOsOpen(t *testing.T) {
	// Test with os.Open (not just bytes.Reader) to exercise the io.Reader path.
	f, err := os.Open(testdataPath("red_4x4_lossless.webp"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	img, format, err := image.Decode(f)
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

// --- DecodeConfig color model for lossy+alpha ---

func TestDecodeConfig_ColorModel_LossyWithAlpha(t *testing.T) {
	// Encode a semi-transparent lossy image (produces VP8X+ALPH).
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
		}
	}
	var buf bytes.Buffer
	if err := Encode(&buf, img, &EncoderOptions{Quality: 80}); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	cfg, err := DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.ColorModel != color.NRGBAModel {
		t.Errorf("color model = %v, want NRGBAModel for lossy+alpha", cfg.ColorModel)
	}
	if cfg.Width != 16 || cfg.Height != 16 {
		t.Errorf("dimensions = %dx%d, want 16x16", cfg.Width, cfg.Height)
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

// --- Animated image tests ---

func TestDecodeAnimatedReturnsFirstFrame(t *testing.T) {
	// Create a 3-frame animation.
	const W, H = 16, 16
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, W, H, nil)

	for i := 0; i < 3; i++ {
		img := image.NewNRGBA(image.Rect(0, 0, W, H))
		c := color.NRGBA{R: uint8(i * 80), G: 128, B: 0, A: 255}
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
			t.Fatalf("AddFrame %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Decode with the top-level Decode (should return first frame).
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != W || bounds.Dy() != H {
		t.Errorf("decoded size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), W, H)
	}
}

func TestDecodeConfigEdgeCases(t *testing.T) {
	t.Run("1x1_lossless", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, color.NRGBA{R: 42, G: 84, B: 126, A: 255})

		var buf bytes.Buffer
		if err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 75}); err != nil {
			t.Fatalf("Encode: %v", err)
		}

		cfg, err := DecodeConfig(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("DecodeConfig: %v", err)
		}
		if cfg.Width != 1 || cfg.Height != 1 {
			t.Errorf("config = %dx%d, want 1x1", cfg.Width, cfg.Height)
		}
	})

	t.Run("with_alpha", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 100, G: 200, B: 50, A: 128})
			}
		}

		var buf bytes.Buffer
		if err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 75}); err != nil {
			t.Fatalf("Encode: %v", err)
		}

		cfg, err := DecodeConfig(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("DecodeConfig: %v", err)
		}
		if cfg.Width != 4 || cfg.Height != 4 {
			t.Errorf("config = %dx%d, want 4x4", cfg.Width, cfg.Height)
		}
		if cfg.ColorModel != color.NRGBAModel {
			t.Errorf("color model = %v, want NRGBAModel", cfg.ColorModel)
		}
	})

	t.Run("animated", func(t *testing.T) {
		const W, H = 8, 8
		var buf bytes.Buffer
		enc := animation.NewEncoder(&buf, W, H, nil)
		for i := 0; i < 2; i++ {
			img := image.NewNRGBA(image.Rect(0, 0, W, H))
			enc.AddFrame(img, 50*time.Millisecond)
		}
		enc.Close()

		cfg, err := DecodeConfig(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("DecodeConfig: %v", err)
		}
		if cfg.Width != W || cfg.Height != H {
			t.Errorf("config = %dx%d, want %dx%d", cfg.Width, cfg.Height, W, H)
		}
	})
}

// --- Metadata round-trip tests ---

func TestMetadataRoundTrip(t *testing.T) {
	const W, H = 8, 8
	iccData := []byte("fake ICC profile data for testing")
	exifData := []byte("fake EXIF data for testing")
	xmpData := []byte("fake XMP data for testing")

	// Encode a 2-frame animation with ICC, EXIF, and XMP metadata.
	// Two frames are needed because the single-frame optimization in
	// AnimEncoder.Close() produces a simple VP8/VP8L file that strips
	// VP8X metadata.
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, W, H, nil)
	enc.SetICCProfile(iccData)
	enc.SetEXIF(exifData)
	enc.SetXMP(xmpData)

	for f := 0; f < 2; f++ {
		img := image.NewNRGBA(image.Rect(0, 0, W, H))
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(f * 128), G: 255, A: 255})
			}
		}
		if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
			t.Fatalf("AddFrame %d: %v", f, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Decode with mux.Demuxer and verify metadata.
	data := buf.Bytes()
	d, err := mux.NewDemuxer(data)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	feat := d.GetFeatures()
	if !feat.HasICC {
		t.Error("HasICC should be true")
	}
	if !feat.HasEXIF {
		t.Error("HasEXIF should be true")
	}
	if !feat.HasXMP {
		t.Error("HasXMP should be true")
	}

	icc, err := d.GetChunk(mux.FourCCICCP)
	if err != nil {
		t.Fatalf("GetChunk(ICCP): %v", err)
	}
	if !bytes.Equal(icc, iccData) {
		t.Errorf("ICC = %q, want %q", icc, iccData)
	}

	exif, err := d.GetChunk(mux.FourCCEXIF)
	if err != nil {
		t.Fatalf("GetChunk(EXIF): %v", err)
	}
	if !bytes.Equal(exif, exifData) {
		t.Errorf("EXIF = %q, want %q", exif, exifData)
	}

	xmp, err := d.GetChunk(mux.FourCCXMP)
	if err != nil {
		t.Fatalf("GetChunk(XMP): %v", err)
	}
	if !bytes.Equal(xmp, xmpData) {
		t.Errorf("XMP = %q, want %q", xmp, xmpData)
	}
}

// TestEncodeWithMetadata_Lossy verifies that Encode() with ICC/EXIF/XMP in
// EncoderOptions produces a VP8X extended file that a Demuxer can parse.
func TestEncodeWithMetadata_Lossy(t *testing.T) {
	const W, H = 16, 16
	iccData := []byte("test ICC profile payload")
	exifData := []byte("test EXIF metadata payload")
	xmpData := []byte("test XMP metadata payload")

	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	for i := range img.Pix {
		img.Pix[i] = 128
	}

	opts := DefaultOptions()
	opts.ICC = iccData
	opts.EXIF = exifData
	opts.XMP = xmpData

	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	data := buf.Bytes()
	d, err := mux.NewDemuxer(data)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	feat := d.GetFeatures()
	if !feat.HasICC {
		t.Error("HasICC should be true")
	}
	if !feat.HasEXIF {
		t.Error("HasEXIF should be true")
	}
	if !feat.HasXMP {
		t.Error("HasXMP should be true")
	}
	if feat.Width != W || feat.Height != H {
		t.Errorf("dimensions = %dx%d, want %dx%d", feat.Width, feat.Height, W, H)
	}

	icc, err := d.GetChunk(mux.FourCCICCP)
	if err != nil {
		t.Fatalf("GetChunk(ICCP): %v", err)
	}
	if !bytes.Equal(icc, iccData) {
		t.Errorf("ICC mismatch: got %d bytes, want %d", len(icc), len(iccData))
	}

	exif, err := d.GetChunk(mux.FourCCEXIF)
	if err != nil {
		t.Fatalf("GetChunk(EXIF): %v", err)
	}
	if !bytes.Equal(exif, exifData) {
		t.Errorf("EXIF mismatch: got %d bytes, want %d", len(exif), len(exifData))
	}

	xmp, err := d.GetChunk(mux.FourCCXMP)
	if err != nil {
		t.Fatalf("GetChunk(XMP): %v", err)
	}
	if !bytes.Equal(xmp, xmpData) {
		t.Errorf("XMP mismatch: got %d bytes, want %d", len(xmp), len(xmpData))
	}

	// Verify the image is still decodable.
	decoded, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bounds().Dx() != W || decoded.Bounds().Dy() != H {
		t.Errorf("decoded dimensions = %dx%d, want %dx%d",
			decoded.Bounds().Dx(), decoded.Bounds().Dy(), W, H)
	}
}

// TestEncodeWithMetadata_Lossless verifies VP8L + metadata round-trip.
func TestEncodeWithMetadata_Lossless(t *testing.T) {
	const W, H = 8, 8
	iccData := []byte("lossless ICC profile")
	exifData := []byte("lossless EXIF data")

	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 32), G: uint8(y * 32), B: 100, A: 255})
		}
	}

	opts := DefaultOptions()
	opts.Lossless = true
	opts.ICC = iccData
	opts.EXIF = exifData

	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	data := buf.Bytes()
	d, err := mux.NewDemuxer(data)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	feat := d.GetFeatures()
	if !feat.HasICC {
		t.Error("HasICC should be true")
	}
	if !feat.HasEXIF {
		t.Error("HasEXIF should be true")
	}
	if feat.HasXMP {
		t.Error("HasXMP should be false (not set)")
	}

	icc, err := d.GetChunk(mux.FourCCICCP)
	if err != nil {
		t.Fatalf("GetChunk(ICCP): %v", err)
	}
	if !bytes.Equal(icc, iccData) {
		t.Errorf("ICC mismatch")
	}

	exif, err := d.GetChunk(mux.FourCCEXIF)
	if err != nil {
		t.Fatalf("GetChunk(EXIF): %v", err)
	}
	if !bytes.Equal(exif, exifData) {
		t.Errorf("EXIF mismatch")
	}

	// Verify lossless round-trip pixel accuracy.
	decoded, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			want := img.NRGBAAt(x, y)
			r, g, b, a := decoded.At(x, y).RGBA()
			got := color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
			if want != got {
				t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

// TestEncodeWithMetadata_NoMetadata verifies that Encode without metadata
// produces a simple (non-VP8X) container.
func TestEncodeWithMetadata_NoMetadata(t *testing.T) {
	const W, H = 4, 4
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
		}
	}

	opts := DefaultOptions()
	// No ICC/EXIF/XMP set.

	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Simple format should NOT have VP8X.
	data := buf.Bytes()
	if len(data) < 16 {
		t.Fatal("output too small")
	}
	// Check the first chunk after RIFF header (12 bytes) is VP8 or VP8L, not VP8X.
	chunkID := string(data[12:16])
	if chunkID == "VP8X" {
		t.Error("simple encode without metadata should not produce VP8X container")
	}
}
