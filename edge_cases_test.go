package webp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/deepteams/webp/animation"
)

// --- Helpers ---

func makeNRGBA(w, h int, fill color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		off := y * img.Stride
		for x := 0; x < w; x++ {
			img.Pix[off] = fill.R
			img.Pix[off+1] = fill.G
			img.Pix[off+2] = fill.B
			img.Pix[off+3] = fill.A
			off += 4
		}
	}
	return img
}

func makeGradient(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := uint8(x * 255 / max(w-1, 1))
			g := uint8(y * 255 / max(h-1, 1))
			b := uint8((x + y) * 127 / max(w+h-2, 1))
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func makeColorPalette(w, h, numColors int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(42))
	palette := make([]color.NRGBA, numColors)
	for i := range palette {
		palette[i] = color.NRGBA{
			R: uint8(rng.Intn(256)),
			G: uint8(rng.Intn(256)),
			B: uint8(rng.Intn(256)),
			A: 255,
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, palette[(y*w+x)%numColors])
		}
	}
	return img
}

func encodeAndDecode(t *testing.T, img image.Image, opts *EncoderOptions) image.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return decoded
}

func mustEncode(t *testing.T, img image.Image, opts *EncoderOptions) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, img, opts); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return buf.Bytes()
}

func assertNoPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", name, r)
		}
	}()
	fn()
}

// --- S1: Extreme Dimensions ---

func TestEdge_1x1_Lossy(t *testing.T) {
	img := makeNRGBA(1, 1, color.NRGBA{R: 128, G: 64, B: 32, A: 255})
	decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75})
	if b := decoded.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Errorf("decoded size = %dx%d, want 1x1", b.Dx(), b.Dy())
	}
}

func TestEdge_1x1_Lossless(t *testing.T) {
	img := makeNRGBA(1, 1, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
	b := decoded.Bounds()
	if b.Dx() != 1 || b.Dy() != 1 {
		t.Fatalf("decoded size = %dx%d, want 1x1", b.Dx(), b.Dy())
	}
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	c := nrgba.NRGBAAt(0, 0)
	if c.R != 200 || c.G != 100 || c.B != 50 || c.A != 255 {
		t.Errorf("pixel = %v, want (200,100,50,255)", c)
	}
}

func TestEdge_Nx1_Lossy(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 15, 16, 17, 32} {
		t.Run(intName(n), func(t *testing.T) {
			img := makeGradient(n, 1)
			decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75})
			if b := decoded.Bounds(); b.Dx() != n || b.Dy() != 1 {
				t.Errorf("decoded size = %dx%d, want %dx1", b.Dx(), b.Dy(), n)
			}
		})
	}
}

func TestEdge_1xN_Lossy(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 15, 16, 17, 32} {
		t.Run(intName(n), func(t *testing.T) {
			img := makeGradient(1, n)
			decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75})
			if b := decoded.Bounds(); b.Dx() != 1 || b.Dy() != n {
				t.Errorf("decoded size = %dx%d, want 1x%d", b.Dx(), b.Dy(), n)
			}
		})
	}
}

func TestEdge_Nx1_Lossless(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 15, 16, 17, 32} {
		t.Run(intName(n), func(t *testing.T) {
			img := makeGradient(n, 1)
			decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
			if b := decoded.Bounds(); b.Dx() != n || b.Dy() != 1 {
				t.Errorf("decoded size = %dx%d, want %dx1", b.Dx(), b.Dy(), n)
			}
		})
	}
}

func TestEdge_1xN_Lossless(t *testing.T) {
	for _, n := range []int{1, 2, 3, 4, 15, 16, 17, 32} {
		t.Run(intName(n), func(t *testing.T) {
			img := makeGradient(1, n)
			decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
			if b := decoded.Bounds(); b.Dx() != 1 || b.Dy() != n {
				t.Errorf("decoded size = %dx%d, want 1x%d", b.Dx(), b.Dy(), n)
			}
		})
	}
}

func TestEdge_NonMultiple16_Lossy(t *testing.T) {
	for _, dim := range [][2]int{{17, 17}, {15, 15}, {33, 33}, {3, 7}} {
		w, h := dim[0], dim[1]
		t.Run(dimName(w, h), func(t *testing.T) {
			img := makeGradient(w, h)
			decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75})
			if b := decoded.Bounds(); b.Dx() != w || b.Dy() != h {
				t.Errorf("decoded size = %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
			}
		})
	}
}

func TestEdge_MaxDimension_Rejected(t *testing.T) {
	img := makeNRGBA(16384, 1, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, nil)
	if err == nil {
		t.Fatal("expected error for 16384x1, got nil")
	}
}

func TestEdge_MaxDimension_Lossless(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large dimension test in -short mode")
	}
	img := makeNRGBA(16383, 1, color.NRGBA{R: 128, G: 64, B: 32, A: 255})
	decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 25})
	if b := decoded.Bounds(); b.Dx() != 16383 || b.Dy() != 1 {
		t.Errorf("decoded size = %dx%d, want 16383x1", b.Dx(), b.Dy())
	}
}

// --- S2: VP8L Color Indexing & Pixel Packing ---

func TestEdge_Lossless_FewColors(t *testing.T) {
	for _, nc := range []int{1, 2, 3, 4, 5, 16, 17, 256} {
		t.Run(intName(nc), func(t *testing.T) {
			img := makeColorPalette(32, 32, nc)
			decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
			b := decoded.Bounds()
			if b.Dx() != 32 || b.Dy() != 32 {
				t.Fatalf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
			}
			nrgba, ok := decoded.(*image.NRGBA)
			if !ok {
				t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
			}
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					orig := img.NRGBAAt(x, y)
					dec := nrgba.NRGBAAt(x, y)
					if orig != dec {
						t.Errorf("pixel(%d,%d) orig=%v dec=%v", x, y, orig, dec)
						return
					}
				}
			}
		})
	}
}

func TestEdge_Lossless_SingleColor(t *testing.T) {
	img := makeNRGBA(16, 16, color.NRGBA{R: 42, G: 42, B: 42, A: 255})
	decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := nrgba.NRGBAAt(x, y)
			if c.R != 42 || c.G != 42 || c.B != 42 || c.A != 255 {
				t.Fatalf("pixel(%d,%d) = %v, want (42,42,42,255)", x, y, c)
			}
		}
	}
}

func TestEdge_Lossless_TwoColors(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if (x+y)%2 == 0 {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
			}
		}
	}
	decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			orig := img.NRGBAAt(x, y)
			dec := nrgba.NRGBAAt(x, y)
			if orig != dec {
				t.Fatalf("pixel(%d,%d) orig=%v dec=%v", x, y, orig, dec)
			}
		}
	}
}

// --- S3: Alpha Channel ---

func TestEdge_Lossy_AlphaGradient(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			a := uint8(x * 255 / 31)
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: a})
		}
	}
	decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 80})
	b := decoded.Bounds()
	if b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
	}
	// Check that alpha varies across the image.
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	aMin, aMax := nrgba.NRGBAAt(0, 16).A, nrgba.NRGBAAt(31, 16).A
	if aMax-aMin < 100 {
		t.Errorf("alpha range too narrow: min=%d max=%d, expected spread > 100", aMin, aMax)
	}
}

func TestEdge_Lossy_FullyTransparent(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 16), G: uint8(y * 16), B: 128, A: 0})
		}
	}
	decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 80})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := nrgba.NRGBAAt(x, y)
			if c.A > 5 {
				t.Errorf("pixel(%d,%d) alpha=%d, want ~0", x, y, c.A)
				return
			}
		}
	}
}

func TestEdge_Lossy_SingleTransparentPixel(t *testing.T) {
	img := makeNRGBA(16, 16, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	img.SetNRGBA(8, 8, color.NRGBA{R: 0, G: 255, B: 0, A: 128})

	data := mustEncode(t, img, &EncoderOptions{Quality: 80})
	// Should produce VP8X format (with ALPH chunk).
	if len(data) < 16 {
		t.Fatal("output too small")
	}
	if string(data[12:16]) != "VP8X" {
		t.Errorf("expected VP8X chunk for image with alpha, got %q", data[12:16])
	}

	decoded, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("decoded size = %dx%d, want 16x16", b.Dx(), b.Dy())
	}
}

func TestEdge_Lossy_AlphaRaw_NoFilter(t *testing.T) {
	img := makeNRGBA(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	decoded := encodeAndDecode(t, img, &EncoderOptions{
		Quality:          80,
		AlphaCompression: 0,
		AlphaFiltering:   0,
		AlphaQuality:     100,
	})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	c := nrgba.NRGBAAt(8, 8)
	if c.A < 100 || c.A > 156 {
		t.Errorf("alpha=%d, want ~128", c.A)
	}
}

func TestEdge_Lossy_AlphaQuality0(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: uint8(x*16 + y)})
		}
	}
	decoded := encodeAndDecode(t, img, &EncoderOptions{
		Quality:      80,
		AlphaQuality: 0,
	})
	if b := decoded.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("decoded size = %dx%d, want 16x16", b.Dx(), b.Dy())
	}
}

func TestEdge_Lossless_AllAlphaValues(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 256, 1))
	for x := 0; x < 256; x++ {
		img.SetNRGBA(x, 0, color.NRGBA{R: 128, G: 64, B: 32, A: uint8(x)})
	}
	// Exact=true preserves RGB under transparent pixels.
	decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75, Exact: true})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for x := 0; x < 256; x++ {
		orig := img.NRGBAAt(x, 0)
		dec := nrgba.NRGBAAt(x, 0)
		if orig != dec {
			t.Errorf("pixel(%d,0) orig=%v dec=%v", x, orig, dec)
			return
		}
	}
}

func TestEdge_Lossless_TransparentExact(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x < 4 {
				img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 0})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{R: 100, G: 200, B: 150, A: 255})
			}
		}
	}
	decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75, Exact: true})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	// With Exact=true, transparent pixels should preserve RGB.
	c := nrgba.NRGBAAt(0, 0)
	if c.R != 200 || c.G != 100 || c.B != 50 || c.A != 0 {
		t.Errorf("pixel(0,0) = %v, want (200,100,50,0) with Exact=true", c)
	}
}

// --- S4: Quality & Method Boundaries ---

func TestEdge_AllQualityLevels(t *testing.T) {
	img := makeGradient(32, 32)
	for _, q := range []float32{0, 1, 50, 99, 100} {
		t.Run(floatName(q), func(t *testing.T) {
			decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: q})
			if b := decoded.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
				t.Errorf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
			}
		})
	}
}

func TestEdge_AllMethodLevels(t *testing.T) {
	img := makeGradient(32, 32)
	for m := 0; m <= 6; m++ {
		t.Run(intName(m), func(t *testing.T) {
			decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75, Method: m})
			if b := decoded.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
				t.Errorf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
			}
		})
	}
}

func TestEdge_AllPartitions(t *testing.T) {
	img := makeGradient(32, 32)
	for p := 0; p <= 3; p++ {
		t.Run(intName(p), func(t *testing.T) {
			decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75, Partitions: p})
			if b := decoded.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
				t.Errorf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
			}
		})
	}
}

func TestEdge_AllPresets(t *testing.T) {
	img := makeGradient(32, 32)
	presets := []Preset{PresetDefault, PresetPicture, PresetPhoto, PresetDrawing, PresetIcon, PresetText}
	names := []string{"Default", "Picture", "Photo", "Drawing", "Icon", "Text"}
	for i, p := range presets {
		t.Run(names[i], func(t *testing.T) {
			opts := OptionsForPreset(p, 75)
			decoded := encodeAndDecode(t, img, opts)
			if b := decoded.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
				t.Errorf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
			}
		})
	}
}

func TestEdge_SharpYUV(t *testing.T) {
	img := makeGradient(32, 32)
	decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75, UseSharpYUV: true})
	if b := decoded.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Errorf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
	}
}

func TestEdge_Lossless_AllQualityLevels(t *testing.T) {
	img := makeGradient(32, 32)
	for _, q := range []float32{0, 25, 50, 75, 90, 100} {
		t.Run(floatName(q), func(t *testing.T) {
			decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: q})
			b := decoded.Bounds()
			if b.Dx() != 32 || b.Dy() != 32 {
				t.Errorf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
			}
			// Lossless: pixel-exact.
			nrgba, ok := decoded.(*image.NRGBA)
			if !ok {
				t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
			}
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					if img.NRGBAAt(x, y) != nrgba.NRGBAAt(x, y) {
						t.Fatalf("Q=%.0f: pixel(%d,%d) mismatch: orig=%v dec=%v",
							q, x, y, img.NRGBAAt(x, y), nrgba.NRGBAAt(x, y))
					}
				}
			}
		})
	}
}

// --- S5: Image Type Inputs ---

func TestEdge_ImageRGBA_Roundtrip(t *testing.T) {
	// *image.RGBA uses premultiplied alpha.
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 200})
		}
	}
	decoded := encodeAndDecode(t, src, &EncoderOptions{Quality: 80})
	if b := decoded.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("decoded size = %dx%d, want 16x16", b.Dx(), b.Dy())
	}
}

func TestEdge_ImageGray_Roundtrip(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8(x*16 + y)})
		}
	}
	decoded := encodeAndDecode(t, src, &EncoderOptions{Quality: 80})
	if b := decoded.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("decoded size = %dx%d, want 16x16", b.Dx(), b.Dy())
	}
}

func TestEdge_ImagePaletted_Roundtrip(t *testing.T) {
	pal := color.Palette{
		color.NRGBA{R: 255, A: 255},
		color.NRGBA{G: 255, A: 255},
		color.NRGBA{B: 255, A: 255},
		color.NRGBA{R: 255, G: 255, A: 255},
	}
	src := image.NewPaletted(image.Rect(0, 0, 16, 16), pal)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.SetColorIndex(x, y, uint8((x+y)%4))
		}
	}
	decoded := encodeAndDecode(t, src, &EncoderOptions{Quality: 80})
	if b := decoded.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
		t.Errorf("decoded size = %dx%d, want 16x16", b.Dx(), b.Dy())
	}
}

func TestEdge_SubImage_Roundtrip(t *testing.T) {
	full := makeGradient(64, 64)
	sub := full.SubImage(image.Rect(10, 10, 42, 42)).(*image.NRGBA)
	decoded := encodeAndDecode(t, sub, &EncoderOptions{Lossless: true, Quality: 75})
	b := decoded.Bounds()
	if b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("decoded size = %dx%d, want 32x32", b.Dx(), b.Dy())
	}
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			orig := sub.NRGBAAt(10+x, 10+y)
			dec := nrgba.NRGBAAt(x, y)
			if orig != dec {
				t.Fatalf("pixel(%d,%d) orig=%v dec=%v", x, y, orig, dec)
			}
		}
	}
}

// --- S6: Malformed Input Safety ---

func TestEdge_Decode_RandomBytes(t *testing.T) {
	rng := rand.New(rand.NewSource(0xDEAD))
	for i := 0; i < 100; i++ {
		size := rng.Intn(512) + 12
		data := make([]byte, size)
		// Wrap in RIFF/WEBP header so the container parser gets reached.
		binary.LittleEndian.PutUint32(data[0:4], 0x46464952) // RIFF
		binary.LittleEndian.PutUint32(data[4:8], uint32(size-8))
		copy(data[8:12], "WEBP")
		rng.Read(data[12:])

		assertNoPanic(t, "RandomBytes", func() {
			Decode(bytes.NewReader(data))
		})
	}
}

func TestEdge_Decode_TruncatedVP8L(t *testing.T) {
	img := makeGradient(32, 32)
	data := mustEncode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
	for _, pct := range []int{25, 50, 75, 90} {
		t.Run(intName(pct), func(t *testing.T) {
			truncated := data[:len(data)*pct/100]
			assertNoPanic(t, "TruncatedVP8L", func() {
				Decode(bytes.NewReader(truncated))
			})
		})
	}
}

func TestEdge_Decode_TruncatedVP8(t *testing.T) {
	img := makeGradient(32, 32)
	data := mustEncode(t, img, &EncoderOptions{Quality: 75})
	for _, pct := range []int{25, 50, 75, 90} {
		t.Run(intName(pct), func(t *testing.T) {
			truncated := data[:len(data)*pct/100]
			assertNoPanic(t, "TruncatedVP8", func() {
				Decode(bytes.NewReader(truncated))
			})
		})
	}
}

func TestEdge_Decode_CorruptedBits(t *testing.T) {
	img := makeGradient(32, 32)
	original := mustEncode(t, img, &EncoderOptions{Quality: 75})
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 50; i++ {
		data := make([]byte, len(original))
		copy(data, original)
		// Flip 1-5 random bits in the bitstream area (after header).
		nFlips := rng.Intn(5) + 1
		for j := 0; j < nFlips; j++ {
			pos := 20 + rng.Intn(len(data)-20) // after RIFF+chunk header
			bit := byte(1 << uint(rng.Intn(8)))
			data[pos] ^= bit
		}
		assertNoPanic(t, "CorruptedBits", func() {
			Decode(bytes.NewReader(data))
		})
	}
}

func TestEdge_DecodeConfig_Arbitrary(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	for i := 0; i < 50; i++ {
		size := rng.Intn(256) + 1
		data := make([]byte, size)
		rng.Read(data)
		assertNoPanic(t, "DecodeConfig", func() {
			DecodeConfig(bytes.NewReader(data))
		})
	}
}

func TestEdge_GetFeatures_Arbitrary(t *testing.T) {
	rng := rand.New(rand.NewSource(456))
	for i := 0; i < 50; i++ {
		size := rng.Intn(256) + 1
		data := make([]byte, size)
		rng.Read(data)
		assertNoPanic(t, "GetFeatures", func() {
			GetFeatures(bytes.NewReader(data))
		})
	}
}

// --- S7: Container/RIFF Robustness ---

func TestEdge_Decode_TruncatedRIFF(t *testing.T) {
	// Only 8 bytes — missing WEBP tag.
	data := []byte("RIFF\x04\x00\x00\x00")
	assertNoPanic(t, "TruncatedRIFF", func() {
		_, err := Decode(bytes.NewReader(data))
		if err == nil {
			t.Error("expected error for truncated RIFF")
		}
	})
}

func TestEdge_Decode_RIFFSizeOverflow(t *testing.T) {
	// Claim fileSize=1M but only provide 100 bytes.
	data := make([]byte, 100)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 1<<20)
	copy(data[8:12], "WEBP")
	copy(data[12:16], "VP8L")
	binary.LittleEndian.PutUint32(data[16:20], 1<<20-12)

	assertNoPanic(t, "RIFFSizeOverflow", func() {
		Decode(bytes.NewReader(data))
	})
}

func TestEdge_Decode_OddChunkSize(t *testing.T) {
	// Build a VP8L chunk with odd payload size.
	img := makeNRGBA(4, 4, color.NRGBA{R: 255, A: 255})
	data := mustEncode(t, img, &EncoderOptions{Lossless: true, Quality: 75})
	// The RIFF output should handle padding correctly.
	if len(data)%2 != 0 {
		t.Errorf("RIFF output has odd total size: %d", len(data))
	}
	// Decode should succeed.
	_, err := Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
}

func TestEdge_Decode_EmptyPayload(t *testing.T) {
	// VP8L chunk with 0-byte payload.
	data := make([]byte, 20)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 12) // filesize = 4+8+0
	copy(data[8:12], "WEBP")
	copy(data[12:16], "VP8L")
	binary.LittleEndian.PutUint32(data[16:20], 0) // payload = 0

	assertNoPanic(t, "EmptyPayload", func() {
		_, err := Decode(bytes.NewReader(data))
		if err == nil {
			t.Error("expected error for empty VP8L payload")
		}
	})
}

func TestEdge_Decode_ZeroDimensions(t *testing.T) {
	// Craft a minimal VP8 frame header claiming 0x0 dimensions.
	data := make([]byte, 40)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 32)
	copy(data[8:12], "WEBP")
	copy(data[12:16], "VP8 ")
	binary.LittleEndian.PutUint32(data[16:20], 20)
	// VP8 frame tag: keyframe, version=0, show=1, partition0_size=0
	data[20] = 0x9d
	data[21] = 0x01
	data[22] = 0x2a
	// Width=0, Height=0 (little-endian 16-bit each).
	data[23] = 0
	data[24] = 0
	data[25] = 0
	data[26] = 0

	assertNoPanic(t, "ZeroDimensions", func() {
		Decode(bytes.NewReader(data))
	})
}

func TestEdge_Decode_CanvasOverflow(t *testing.T) {
	// VP8X with maximum 24-bit canvas dimensions (16777215 x 16777215).
	data := make([]byte, 30)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 22)
	copy(data[8:12], "WEBP")
	copy(data[12:16], "VP8X")
	binary.LittleEndian.PutUint32(data[16:20], 10)
	binary.LittleEndian.PutUint32(data[20:24], 0) // flags
	// width-1 = 0xFFFFFF (24-bit LE)
	data[24] = 0xFF
	data[25] = 0xFF
	data[26] = 0xFF
	// height-1 = 0xFFFFFF (24-bit LE)
	data[27] = 0xFF
	data[28] = 0xFF
	data[29] = 0xFF

	assertNoPanic(t, "CanvasOverflow", func() {
		Decode(bytes.NewReader(data))
	})
}

// --- S8: Concurrent Safety ---

func TestEdge_Concurrent_EncodeLossy(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			img := makeGradient(32+seed, 32+seed)
			var buf bytes.Buffer
			if err := Encode(&buf, img, &EncoderOptions{Quality: 75}); err != nil {
				t.Errorf("goroutine %d: Encode: %v", seed, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestEdge_Concurrent_EncodeLossless(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			img := makeGradient(16+seed, 16+seed)
			var buf bytes.Buffer
			if err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 50}); err != nil {
				t.Errorf("goroutine %d: Encode: %v", seed, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestEdge_Concurrent_Decode(t *testing.T) {
	img := makeGradient(32, 32)
	data := mustEncode(t, img, &EncoderOptions{Quality: 75})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Errorf("Decode: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestEdge_Concurrent_Mixed(t *testing.T) {
	img := makeGradient(32, 32)
	data := mustEncode(t, img, &EncoderOptions{Quality: 75})

	var wg sync.WaitGroup
	// 4 encoders.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			encImg := makeGradient(32+seed, 32)
			var buf bytes.Buffer
			if err := Encode(&buf, encImg, &EncoderOptions{Quality: 75}); err != nil {
				t.Errorf("Encode: %v", err)
			}
		}(i)
	}
	// 4 decoders.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Errorf("Decode: %v", err)
			}
		}()
	}
	wg.Wait()
}

// --- S9: Animation Edge Cases ---

func TestEdge_Anim_SingleFrame(t *testing.T) {
	img := makeNRGBA(32, 32, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 32, 32, &animation.EncodeOptions{
		Lossless: true,
		Quality:  75,
	})
	if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
	// Decode and verify.
	anim, err := animation.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) < 1 {
		t.Fatalf("got %d frames, want >= 1", len(anim.Frames))
	}
}

func TestEdge_Anim_FrameDuration0(t *testing.T) {
	img := makeNRGBA(16, 16, color.NRGBA{G: 255, A: 255})
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 16, 16, &animation.EncodeOptions{
		Lossless: true,
		Quality:  75,
	})
	if err := enc.AddFrame(img, 0); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 2: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
}

func TestEdge_Anim_LoopCount0(t *testing.T) {
	img := makeNRGBA(16, 16, color.NRGBA{B: 255, A: 255})
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 16, 16, &animation.EncodeOptions{
		Lossless:  true,
		Quality:   75,
		LoopCount: 0, // infinite
	})
	if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	anim, err := animation.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if anim.LoopCount != 0 {
		t.Errorf("LoopCount = %d, want 0 (infinite)", anim.LoopCount)
	}
}

func TestEdge_Anim_LoopCount65535(t *testing.T) {
	img1 := makeNRGBA(16, 16, color.NRGBA{R: 255, A: 255})
	img2 := makeNRGBA(16, 16, color.NRGBA{G: 255, A: 255})
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 16, 16, &animation.EncodeOptions{
		Lossless:  true,
		Quality:   75,
		LoopCount: 65535,
	})
	if err := enc.AddFrame(img1, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.AddFrame(img2, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	anim, err := animation.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if anim.LoopCount != 65535 {
		t.Errorf("LoopCount = %d, want 65535", anim.LoopCount)
	}
}

func TestEdge_Anim_DisposeBackground(t *testing.T) {
	red := makeNRGBA(16, 16, color.NRGBA{R: 255, A: 255})
	green := makeNRGBA(16, 16, color.NRGBA{G: 255, A: 128})
	blue := makeNRGBA(16, 16, color.NRGBA{B: 255, A: 255})

	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 16, 16, &animation.EncodeOptions{
		Lossless: true,
		Quality:  75,
	})
	// Frame 1: opaque red.
	if err := enc.AddFrame(red, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 1: %v", err)
	}
	// Frame 2: semi-transparent green.
	if err := enc.AddFrame(green, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 2: %v", err)
	}
	// Frame 3: opaque blue.
	if err := enc.AddFrame(blue, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 3: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
	// Verify we can decode all frames.
	anim, err := animation.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) < 3 {
		t.Fatalf("got %d frames, want >= 3", len(anim.Frames))
	}
}

func TestEdge_Anim_BlendAlpha(t *testing.T) {
	bg := makeNRGBA(16, 16, color.NRGBA{R: 255, A: 255})
	overlay := makeNRGBA(16, 16, color.NRGBA{G: 255, A: 128})

	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 16, 16, &animation.EncodeOptions{
		Lossless: true,
		Quality:  75,
	})
	if err := enc.AddFrame(bg, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame bg: %v", err)
	}
	if err := enc.AddFrame(overlay, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame overlay: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
}

func TestEdge_Anim_FrameOffset(t *testing.T) {
	canvas := 32
	frame := makeNRGBA(16, 16, color.NRGBA{R: 255, A: 255})

	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, canvas, canvas, &animation.EncodeOptions{
		Lossless: true,
		Quality:  75,
	})
	// First full-canvas frame (required to establish canvas).
	bg := makeNRGBA(canvas, canvas, color.NRGBA{A: 255})
	if err := enc.AddFrame(bg, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame bg: %v", err)
	}
	// Second frame: a 16x16 image placed at offset.
	// Note: the AnimEncoder will place it on a full canvas.
	full := image.NewNRGBA(image.Rect(0, 0, canvas, canvas))
	for y := 10; y < 26; y++ {
		for x := 10; x < 26; x++ {
			full.SetNRGBA(x, y, frame.NRGBAAt(x-10, y-10))
		}
	}
	if err := enc.AddFrame(full, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame frame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
}

func TestEdge_Anim_IdenticalFrames(t *testing.T) {
	img := makeNRGBA(16, 16, color.NRGBA{R: 100, G: 200, B: 50, A: 255})
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 16, 16, &animation.EncodeOptions{
		Lossless: true,
		Quality:  75,
	})
	for i := 0; i < 3; i++ {
		if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
			t.Fatalf("AddFrame %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
}

func TestEdge_Anim_MixedCodec(t *testing.T) {
	img1 := makeGradient(16, 16)
	img2 := makeNRGBA(16, 16, color.NRGBA{B: 200, A: 255})
	var buf bytes.Buffer
	enc := animation.NewEncoder(&buf, 16, 16, &animation.EncodeOptions{
		Quality:    75,
		AllowMixed: true,
	})
	if err := enc.AddFrame(img1, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 1: %v", err)
	}
	if err := enc.AddFrame(img2, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 2: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty output")
	}
}

// --- Name helpers ---

func intName(n int) string {
	return fmt.Sprintf("%d", n)
}

func floatName(f float32) string {
	return fmt.Sprintf("%.0f", f)
}

func dimName(w, h int) string {
	return fmt.Sprintf("%dx%d", w, h)
}

// --- S10: Additional Max Dimension Tests ---

func TestEdge_MaxDimension_Height_Rejected(t *testing.T) {
	img := makeNRGBA(1, 16384, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, nil)
	if err == nil {
		t.Fatal("expected error for 1x16384, got nil")
	}
}

func TestEdge_MaxDimension_Both_Rejected(t *testing.T) {
	img := makeNRGBA(16384, 16384, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, nil)
	if err == nil {
		t.Fatal("expected error for 16384x16384, got nil")
	}
}

func TestEdge_MaxDimension_Lossless_Rejected(t *testing.T) {
	img := makeNRGBA(16384, 1, color.NRGBA{R: 128, G: 64, B: 32, A: 255})
	var buf bytes.Buffer
	err := Encode(&buf, img, &EncoderOptions{Lossless: true, Quality: 75})
	if err == nil {
		t.Fatal("expected error for 16384x1 lossless, got nil")
	}
}

func TestEdge_MaxDimension_Boundary_Lossy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large dimension test in -short mode")
	}
	img := makeNRGBA(16383, 1, color.NRGBA{R: 128, G: 64, B: 32, A: 255})
	decoded := encodeAndDecode(t, img, &EncoderOptions{Quality: 75})
	if b := decoded.Bounds(); b.Dx() != 16383 || b.Dy() != 1 {
		t.Errorf("decoded size = %dx%d, want 16383x1", b.Dx(), b.Dy())
	}
}

// --- S11: Lossless Exact + Transparent RGB ---

func TestEdge_Lossless_ExactPreservesTransparentRGBGradient(t *testing.T) {
	const W, H = 16, 16
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 16),
				G: uint8(y * 16),
				B: uint8((x + y) * 8),
				A: 0,
			})
		}
	}
	decoded := encodeAndDecode(t, img, &EncoderOptions{Lossless: true, Quality: 75, Exact: true})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			orig := img.NRGBAAt(x, y)
			dec := nrgba.NRGBAAt(x, y)
			if orig != dec {
				t.Fatalf("pixel(%d,%d) orig=%v dec=%v", x, y, orig, dec)
			}
		}
	}
}

// --- S12: Non-NRGBA Image Type Lossless Roundtrips ---

func TestEdge_ImageGray_Lossless_Roundtrip(t *testing.T) {
	const W, H = 16, 16
	src := image.NewGray(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8(x*16 + y)})
		}
	}
	decoded := encodeAndDecode(t, src, &EncoderOptions{Lossless: true, Quality: 75})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			grayVal := src.GrayAt(x, y)
			expected := color.NRGBAModel.Convert(grayVal).(color.NRGBA)
			dec := nrgba.NRGBAAt(x, y)
			if dec.R != expected.R || dec.G != expected.G || dec.B != expected.B || dec.A != expected.A {
				t.Fatalf("pixel(%d,%d) dec=%v, want %v (gray=%d)", x, y, dec, expected, grayVal.Y)
			}
		}
	}
}

func TestEdge_ImageGray16_Lossless_Roundtrip(t *testing.T) {
	const W, H = 16, 16
	src := image.NewGray16(image.Rect(0, 0, W, H))
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			v := uint16((x*16 + y) * 256)
			src.SetGray16(x, y, color.Gray16{Y: v})
		}
	}
	decoded := encodeAndDecode(t, src, &EncoderOptions{Lossless: true, Quality: 75})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			gray16Val := src.Gray16At(x, y)
			expected := color.NRGBAModel.Convert(gray16Val).(color.NRGBA)
			dec := nrgba.NRGBAAt(x, y)
			if dec.R != expected.R || dec.G != expected.G || dec.B != expected.B || dec.A != expected.A {
				t.Fatalf("pixel(%d,%d) dec=%v, want %v (gray16=%d)", x, y, dec, expected, gray16Val.Y)
			}
		}
	}
}

func TestEdge_ImagePaletted_Lossless_Roundtrip(t *testing.T) {
	const W, H = 16, 16
	pal := color.Palette{
		color.NRGBA{R: 255, G: 0, B: 0, A: 255},
		color.NRGBA{R: 0, G: 255, B: 0, A: 255},
		color.NRGBA{R: 0, G: 0, B: 255, A: 255},
		color.NRGBA{R: 255, G: 255, B: 0, A: 255},
	}
	src := image.NewPaletted(image.Rect(0, 0, W, H), pal)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			src.SetColorIndex(x, y, uint8((x+y)%4))
		}
	}
	decoded := encodeAndDecode(t, src, &EncoderOptions{Lossless: true, Quality: 75})
	nrgba, ok := decoded.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded type %T, want *image.NRGBA", decoded)
	}
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			srcColor := src.At(x, y)
			expected := color.NRGBAModel.Convert(srcColor).(color.NRGBA)
			dec := nrgba.NRGBAAt(x, y)
			if dec != expected {
				t.Fatalf("pixel(%d,%d) dec=%v, want %v", x, y, dec, expected)
			}
		}
	}
}
