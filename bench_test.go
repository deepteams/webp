package webp

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"testing"
)

func loadTestImage(b *testing.B) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return img
}

func BenchmarkEncodeLossy_Q75(b *testing.B) {
	img := loadTestImage(b)
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeLossy_Q50(b *testing.B) {
	img := loadTestImage(b)
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Quality: 50, Method: 4}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkEncodeLossless(b *testing.B) {
	img := loadTestImage(b)
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Lossless: true, Quality: 75}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

func BenchmarkDecodeLossy(b *testing.B) {
	img := loadTestImage(b)
	buf := &bytes.Buffer{}
	Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4})
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(data)))
}

func BenchmarkDecodeLossless(b *testing.B) {
	img := loadTestImage(b)
	buf := &bytes.Buffer{}
	Encode(buf, img, &EncoderOptions{Lossless: true, Quality: 75})
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(data)))
}

// ---------------------------------------------------------------------------
// Helper: create a large test image with a gradient pattern.
// ---------------------------------------------------------------------------

func makeLargeTestImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return img
}

// ---------------------------------------------------------------------------
// 1. Quality sweep (lossy, Q=0..100)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_QualitySweep(b *testing.B) {
	img := loadTestImage(b)
	for _, q := range []float32{0, 25, 50, 75, 100} {
		b.Run(fmt.Sprintf("Q%.0f", q), func(b *testing.B) {
			buf := &bytes.Buffer{}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				if err := Encode(buf, img, &EncoderOptions{Quality: q, Method: 4}); err != nil {
					b.Fatal(err)
				}
			}
			b.SetBytes(int64(buf.Len()))
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Method sweep (lossy, M=0..6)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_MethodSweep(b *testing.B) {
	img := loadTestImage(b)
	for _, m := range []int{0, 2, 4, 6} {
		b.Run(fmt.Sprintf("M%d", m), func(b *testing.B) {
			buf := &bytes.Buffer{}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: m}); err != nil {
					b.Fatal(err)
				}
			}
			b.SetBytes(int64(buf.Len()))
		})
	}
}

// ---------------------------------------------------------------------------
// 3. Method sweep (lossless, M=0..6)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossless_MethodSweep(b *testing.B) {
	img := loadTestImage(b)
	for _, m := range []int{0, 2, 4, 6} {
		b.Run(fmt.Sprintf("M%d", m), func(b *testing.B) {
			buf := &bytes.Buffer{}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				if err := Encode(buf, img, &EncoderOptions{Lossless: true, Quality: 75, Method: m}); err != nil {
					b.Fatal(err)
				}
			}
			b.SetBytes(int64(buf.Len()))
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Lossy encode with alpha channel
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_WithAlpha(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: uint8(128 + (x+y)%128),
			})
		}
	}
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ---------------------------------------------------------------------------
// 5. Lossy encode without alpha (all A=255, same pixel pattern)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_WithoutAlpha(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ---------------------------------------------------------------------------
// 6. Decode lossy with alpha
// ---------------------------------------------------------------------------

func BenchmarkDecodeLossy_WithAlpha(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: uint8(128 + (x+y)%128),
			})
		}
	}
	buf := &bytes.Buffer{}
	if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(data)))
}

// ---------------------------------------------------------------------------
// 7. 1080p lossy encode
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_1080p(b *testing.B) {
	img := makeLargeTestImage(1920, 1080)
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ---------------------------------------------------------------------------
// 8. 1080p lossy decode
// ---------------------------------------------------------------------------

func BenchmarkDecodeLossy_1080p(b *testing.B) {
	img := makeLargeTestImage(1920, 1080)
	buf := &bytes.Buffer{}
	if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(data)))
}

// ---------------------------------------------------------------------------
// 9. 1080p lossless encode
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossless_1080p(b *testing.B) {
	img := makeLargeTestImage(1920, 1080)
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Lossless: true, Quality: 75, Method: 4}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ---------------------------------------------------------------------------
// 10. 1080p lossless decode
// ---------------------------------------------------------------------------

func BenchmarkDecodeLossless_1080p(b *testing.B) {
	img := makeLargeTestImage(1920, 1080)
	buf := &bytes.Buffer{}
	if err := Encode(buf, img, &EncoderOptions{Lossless: true, Quality: 75, Method: 4}); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(data)))
}

// ---------------------------------------------------------------------------
// 11. 4K lossy encode (gated by testing.Short)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_4K(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping 4K benchmark in short mode")
	}
	img := makeLargeTestImage(3840, 2160)
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ---------------------------------------------------------------------------
// 12. 4K lossy decode (gated by testing.Short)
// ---------------------------------------------------------------------------

func BenchmarkDecodeLossy_4K(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping 4K benchmark in short mode")
	}
	img := makeLargeTestImage(3840, 2160)
	buf := &bytes.Buffer{}
	if err := Encode(buf, img, &EncoderOptions{Quality: 75, Method: 4}); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(len(data)))
}

// ---------------------------------------------------------------------------
// 13. Lossy encode with metadata (ICC + EXIF + XMP)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_WithMetadata(b *testing.B) {
	img := loadTestImage(b)
	icc := make([]byte, 4096)  // 4 KB ICC profile
	exif := make([]byte, 2048) // 2 KB EXIF
	xmp := make([]byte, 1024)  // 1 KB XMP
	for i := range icc {
		icc[i] = byte(i)
	}
	for i := range exif {
		exif[i] = byte(i)
	}
	for i := range xmp {
		xmp[i] = byte(i)
	}
	opts := &EncoderOptions{
		Quality: 75,
		Method:  4,
		ICC:     icc,
		EXIF:    exif,
		XMP:     xmp,
	}
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ---------------------------------------------------------------------------
// 14. Lossy encode without metadata (same image, for comparison)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossy_WithoutMetadata(b *testing.B) {
	img := loadTestImage(b)
	opts := &EncoderOptions{
		Quality: 75,
		Method:  4,
	}
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}

// ---------------------------------------------------------------------------
// 15. Lossless encode with metadata (ICC + EXIF)
// ---------------------------------------------------------------------------

func BenchmarkEncodeLossless_WithMetadata(b *testing.B) {
	img := loadTestImage(b)
	icc := make([]byte, 4096)  // 4 KB ICC profile
	exif := make([]byte, 2048) // 2 KB EXIF
	for i := range icc {
		icc[i] = byte(i)
	}
	for i := range exif {
		exif[i] = byte(i)
	}
	opts := &EncoderOptions{
		Lossless: true,
		Quality:  75,
		ICC:      icc,
		EXIF:     exif,
	}
	buf := &bytes.Buffer{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := Encode(buf, img, opts); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(int64(buf.Len()))
}
