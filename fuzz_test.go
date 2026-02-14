package webp

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// addSeedCorpus adds all testdata/*.webp files to the fuzz corpus.
func addSeedCorpus(f *testing.F) {
	f.Helper()
	entries, err := os.ReadDir("testdata")
	if err != nil {
		return // no testdata dir, skip
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := filepath.Ext(e.Name()); ext != ".webp" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		if err != nil {
			continue
		}
		f.Add(data)
	}
}

// addMinimalSeeds adds hand-crafted minimal VP8/VP8L bitstreams to the corpus.
func addMinimalSeeds(f *testing.F) {
	f.Helper()
	// Minimal valid lossless: encode a 1x1 red image.
	{
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		var buf bytes.Buffer
		if err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 75}); err == nil {
			f.Add(buf.Bytes())
		}
	}
	// Minimal valid lossy: encode a 1x1 blue image.
	{
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})
		var buf bytes.Buffer
		if err := Encode(&buf, img, &EncoderOptions{Quality: 75}); err == nil {
			f.Add(buf.Bytes())
		}
	}
	// Minimal lossy with alpha.
	{
		img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
			}
		}
		var buf bytes.Buffer
		if err := Encode(&buf, img, &EncoderOptions{Quality: 75}); err == nil {
			f.Add(buf.Bytes())
		}
	}
}

// FuzzDecode is the primary CVE defense target. Ensures that no input can
// cause a panic in the decoder (guards against CVE-2023-4863 style overflows).
func FuzzDecode(f *testing.F) {
	addSeedCorpus(f)
	addMinimalSeeds(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		Decode(bytes.NewReader(data)) //nolint:errcheck
	})
}

// FuzzDecodeConfig ensures config parsing never panics on arbitrary input.
func FuzzDecodeConfig(f *testing.F) {
	addSeedCorpus(f)
	addMinimalSeeds(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		DecodeConfig(bytes.NewReader(data)) //nolint:errcheck
	})
}

// FuzzGetFeatures ensures feature extraction never panics on arbitrary input.
func FuzzGetFeatures(f *testing.F) {
	addSeedCorpus(f)
	addMinimalSeeds(f)

	f.Fuzz(func(t *testing.T, data []byte) {
		GetFeatures(bytes.NewReader(data)) //nolint:errcheck
	})
}

// FuzzEncodeLossless constructs a small NRGBA image from fuzzer input and
// verifies that the lossless encoder never panics.
func FuzzEncodeLossless(f *testing.F) {
	// Seed with a small gradient.
	seed := make([]byte, 4*4*4) // 4x4 NRGBA
	for i := range seed {
		seed[i] = byte(i)
	}
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}
		// Use first two bytes for dimensions (1-64 each).
		w := int(data[0]%64) + 1
		h := int(data[1]%64) + 1
		pixData := data[2:]
		needed := w * h * 4
		if len(pixData) < needed {
			// Pad with zeros.
			padded := make([]byte, needed)
			copy(padded, pixData)
			pixData = padded
		} else {
			pixData = pixData[:needed]
		}

		img := &image.NRGBA{
			Pix:    pixData,
			Stride: w * 4,
			Rect:   image.Rect(0, 0, w, h),
		}

		var buf bytes.Buffer
		Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 50}) //nolint:errcheck
	})
}

// FuzzEncodeLossy constructs a small NRGBA image from fuzzer input and
// verifies that the lossy encoder never panics.
func FuzzEncodeLossy(f *testing.F) {
	seed := make([]byte, 4*4*4)
	for i := range seed {
		seed[i] = byte(i * 7)
	}
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}
		w := int(data[0]%64) + 1
		h := int(data[1]%64) + 1
		pixData := data[2:]
		needed := w * h * 4
		if len(pixData) < needed {
			padded := make([]byte, needed)
			copy(padded, pixData)
			pixData = padded
		} else {
			pixData = pixData[:needed]
		}

		img := &image.NRGBA{
			Pix:    pixData,
			Stride: w * 4,
			Rect:   image.Rect(0, 0, w, h),
		}

		var buf bytes.Buffer
		Encode(&buf, img, &EncoderOptions{Quality: 75}) //nolint:errcheck
	})
}

// FuzzRoundtrip constructs a small NRGBA image from fuzzer input, encodes it
// lossless, decodes, and verifies dimensions match.
func FuzzRoundtrip(f *testing.F) {
	seed := make([]byte, 8*8*4)
	for i := range seed {
		seed[i] = byte(i * 3)
	}
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 4 {
			return
		}
		w := int(data[0]%32) + 1
		h := int(data[1]%32) + 1
		pixData := data[2:]
		needed := w * h * 4
		if len(pixData) < needed {
			padded := make([]byte, needed)
			copy(padded, pixData)
			pixData = padded
		} else {
			pixData = pixData[:needed]
		}

		img := &image.NRGBA{
			Pix:    pixData,
			Stride: w * 4,
			Rect:   image.Rect(0, 0, w, h),
		}

		var buf bytes.Buffer
		if err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 25}); err != nil {
			return // encoding error is fine for fuzz
		}

		decoded, err := Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("roundtrip: Encode succeeded but Decode failed: %v", err)
		}

		b := decoded.Bounds()
		if b.Dx() != w || b.Dy() != h {
			t.Fatalf("roundtrip: dimensions mismatch: encoded %dx%d, decoded %dx%d", w, h, b.Dx(), b.Dy())
		}
	})
}
