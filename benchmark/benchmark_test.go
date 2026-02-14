// Package benchmark provides comparative benchmarks between deepteams/webp
// and other Go WebP libraries.
//
// Run with:
//
//	go test -bench=. -benchmem -count=3
//	go test -bench=. -benchmem -count=3 -run=^$ -timeout=10m
//
// To skip CGo-based libraries (chai2010/webp):
//
//	CGO_ENABLED=0 go test -bench=. -benchmem -count=3
package benchmark

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"testing"

	// Our library
	deepteams "github.com/deepteams/webp"

	// Competitors
	chai2010 "github.com/chai2010/webp"
	gen2brain "github.com/gen2brain/webp"
	nativewebp "github.com/HugoSmits86/nativewebp"
	xwebp "golang.org/x/image/webp"
)

// testImage holds the decoded PNG used as source for encode benchmarks.
var testImage image.Image

// testImageSmall is a 256x256 crop for faster benchmarks.
var testImageSmall image.Image

// Pre-encoded WebP buffers for decode benchmarks.
var (
	webpLossyDeepteams    []byte
	webpLosslessDeepteams []byte
	webpLossyChai         []byte
	webpLosslessChai      []byte
	webpLossyGen2brain    []byte
	webpLosslessGen2brain []byte
	webpLosslessNative    []byte
)

func TestMain(m *testing.M) {
	// Load test image (768x576 RGBA PNG).
	f, err := os.Open("../testdata/test_color.png")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open test image: %v\n", err)
		os.Exit(1)
	}
	testImage, err = png.Decode(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot decode test image: %v\n", err)
		os.Exit(1)
	}

	// Create a 256x256 crop.
	b := testImage.Bounds()
	cropped := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256 && y+b.Min.Y < b.Max.Y; y++ {
		for x := 0; x < 256 && x+b.Min.X < b.Max.X; x++ {
			cropped.Set(x, y, testImage.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	testImageSmall = cropped

	// Pre-encode for decode benchmarks.
	webpLossyDeepteams = mustEncodeDeepteamsLossy(testImage)
	webpLosslessDeepteams = mustEncodeDeepteamsLossless(testImage)
	webpLossyChai = mustEncodeChaiLossy(testImage)
	webpLosslessChai = mustEncodeChaiLossless(testImage)
	webpLossyGen2brain = mustEncodeGen2brainLossy(testImage)
	webpLosslessGen2brain = mustEncodeGen2brainLossless(testImage)
	webpLosslessNative = mustEncodeNativeLossless(testImage)

	os.Exit(m.Run())
}

// ============================================================================
// Helper encode functions (for pre-encoding decode test data)
// ============================================================================

func mustEncodeDeepteamsLossy(img image.Image) []byte {
	var buf bytes.Buffer
	opts := deepteams.DefaultOptions()
	opts.Quality = 75
	if err := deepteams.Encode(&buf, img, opts); err != nil {
		panic("deepteams lossy encode: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeDeepteamsLossless(img image.Image) []byte {
	var buf bytes.Buffer
	opts := deepteams.DefaultOptions()
	opts.Lossless = true
	opts.Quality = 75
	if err := deepteams.Encode(&buf, img, opts); err != nil {
		panic("deepteams lossless encode: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeChaiLossy(img image.Image) []byte {
	var buf bytes.Buffer
	if err := chai2010.Encode(&buf, img, &chai2010.Options{Lossless: false, Quality: 75}); err != nil {
		panic("chai2010 lossy encode: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeChaiLossless(img image.Image) []byte {
	var buf bytes.Buffer
	if err := chai2010.Encode(&buf, img, &chai2010.Options{Lossless: true, Quality: 75}); err != nil {
		panic("chai2010 lossless encode: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeGen2brainLossy(img image.Image) []byte {
	var buf bytes.Buffer
	if err := gen2brain.Encode(&buf, img, gen2brain.Options{Quality: 75, Lossless: false}); err != nil {
		panic("gen2brain lossy encode: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeGen2brainLossless(img image.Image) []byte {
	var buf bytes.Buffer
	if err := gen2brain.Encode(&buf, img, gen2brain.Options{Quality: 75, Lossless: true}); err != nil {
		panic("gen2brain lossless encode: " + err.Error())
	}
	return buf.Bytes()
}

func mustEncodeNativeLossless(img image.Image) []byte {
	var buf bytes.Buffer
	if err := nativewebp.Encode(&buf, img, nil); err != nil {
		panic("nativewebp lossless encode: " + err.Error())
	}
	return buf.Bytes()
}

// ============================================================================
// Size report (not a benchmark, but prints file sizes for comparison)
// ============================================================================

func TestFileSizes(t *testing.T) {
	t.Logf("Source image: %dx%d", testImage.Bounds().Dx(), testImage.Bounds().Dy())
	t.Log("")
	t.Log("=== Lossy Q75 file sizes ===")
	t.Logf("  deepteams/webp:    %6d bytes", len(webpLossyDeepteams))
	t.Logf("  chai2010/webp:     %6d bytes", len(webpLossyChai))
	t.Logf("  gen2brain/webp:    %6d bytes", len(webpLossyGen2brain))
	t.Log("")
	t.Log("=== Lossless file sizes ===")
	t.Logf("  deepteams/webp:    %6d bytes", len(webpLosslessDeepteams))
	t.Logf("  chai2010/webp:     %6d bytes", len(webpLosslessChai))
	t.Logf("  gen2brain/webp:    %6d bytes", len(webpLosslessGen2brain))
	t.Logf("  nativewebp:        %6d bytes", len(webpLosslessNative))
}

// ============================================================================
// ENCODE BENCHMARKS — Lossy Q75
// ============================================================================

func BenchmarkEncodeLossy_Deepteams(b *testing.B) {
	opts := deepteams.DefaultOptions()
	opts.Quality = 75
	var buf bytes.Buffer
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := deepteams.Encode(&buf, testImage, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeLossy_Chai2010(b *testing.B) {
	var buf bytes.Buffer
	opts := &chai2010.Options{Lossless: false, Quality: 75}
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := chai2010.Encode(&buf, testImage, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeLossy_Gen2brain(b *testing.B) {
	var buf bytes.Buffer
	opts := gen2brain.Options{Quality: 75, Lossless: false}
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := gen2brain.Encode(&buf, testImage, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ============================================================================
// ENCODE BENCHMARKS — Lossless
// ============================================================================

func BenchmarkEncodeLossless_Deepteams(b *testing.B) {
	opts := deepteams.DefaultOptions()
	opts.Lossless = true
	opts.Quality = 75
	var buf bytes.Buffer
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := deepteams.Encode(&buf, testImage, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeLossless_Chai2010(b *testing.B) {
	var buf bytes.Buffer
	opts := &chai2010.Options{Lossless: true, Quality: 75}
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := chai2010.Encode(&buf, testImage, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeLossless_Gen2brain(b *testing.B) {
	var buf bytes.Buffer
	opts := gen2brain.Options{Quality: 75, Lossless: true}
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := gen2brain.Encode(&buf, testImage, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeLossless_NativeWebP(b *testing.B) {
	var buf bytes.Buffer
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := nativewebp.Encode(&buf, testImage, nil); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ============================================================================
// DECODE BENCHMARKS — Lossy
// ============================================================================

func BenchmarkDecodeLossy_Deepteams(b *testing.B) {
	b.SetBytes(int64(len(webpLossyDeepteams)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := deepteams.Decode(bytes.NewReader(webpLossyDeepteams)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLossy_XImage(b *testing.B) {
	b.SetBytes(int64(len(webpLossyDeepteams)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := xwebp.Decode(bytes.NewReader(webpLossyDeepteams)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLossy_Chai2010(b *testing.B) {
	b.SetBytes(int64(len(webpLossyChai)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := chai2010.Decode(bytes.NewReader(webpLossyChai)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLossy_Gen2brain(b *testing.B) {
	b.SetBytes(int64(len(webpLossyGen2brain)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := gen2brain.Decode(bytes.NewReader(webpLossyGen2brain)); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// DECODE BENCHMARKS — Lossless
// ============================================================================

func BenchmarkDecodeLossless_Deepteams(b *testing.B) {
	// Verify decode works before benchmarking.
	if _, err := deepteams.Decode(bytes.NewReader(webpLosslessDeepteams)); err != nil {
		b.Skipf("deepteams lossless decode not working for this image: %v", err)
	}
	b.SetBytes(int64(len(webpLosslessDeepteams)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := deepteams.Decode(bytes.NewReader(webpLosslessDeepteams)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLossless_XImage(b *testing.B) {
	b.SetBytes(int64(len(webpLosslessDeepteams)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := xwebp.Decode(bytes.NewReader(webpLosslessDeepteams)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLossless_Chai2010(b *testing.B) {
	b.SetBytes(int64(len(webpLosslessChai)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := chai2010.Decode(bytes.NewReader(webpLosslessChai)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLossless_Gen2brain(b *testing.B) {
	b.SetBytes(int64(len(webpLosslessGen2brain)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := gen2brain.Decode(bytes.NewReader(webpLosslessGen2brain)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeLossless_NativeWebP(b *testing.B) {
	b.SetBytes(int64(len(webpLosslessNative)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := nativewebp.Decode(bytes.NewReader(webpLosslessNative)); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================================
// ENCODE BENCHMARKS — Small image (256x256) for faster iteration
// ============================================================================

func BenchmarkEncodeSmallLossy_Deepteams(b *testing.B) {
	opts := deepteams.DefaultOptions()
	opts.Quality = 75
	var buf bytes.Buffer
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := deepteams.Encode(&buf, testImageSmall, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeSmallLossy_Chai2010(b *testing.B) {
	var buf bytes.Buffer
	opts := &chai2010.Options{Lossless: false, Quality: 75}
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := chai2010.Encode(&buf, testImageSmall, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeSmallLossy_Gen2brain(b *testing.B) {
	var buf bytes.Buffer
	opts := gen2brain.Options{Quality: 75, Lossless: false}
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := gen2brain.Encode(&buf, testImageSmall, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}
