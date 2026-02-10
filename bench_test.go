package webp

import (
	"bytes"
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
