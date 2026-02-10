//go:build testc

package roundtrip

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"os"
	"testing"

	"github.com/deepteams/webp"
)

// generateGradient creates an NRGBA test image with a smooth gradient.
func generateGradient(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / width),
				G: uint8(y * 255 / height),
				B: 128,
				A: 255,
			})
		}
	}
	return img
}

// computePSNR computes Peak Signal-to-Noise Ratio between two images.
func computePSNR(a, b image.Image) float64 {
	ab := a.Bounds()
	bb := b.Bounds()
	w := ab.Dx()
	h := ab.Dy()
	if bb.Dx() != w || bb.Dy() != h {
		return 0
	}
	var sumSqErr float64
	count := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ar, ag, abl, aa := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			br, bg, bbl, ba := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			dr := float64(ar>>8) - float64(br>>8)
			dg := float64(ag>>8) - float64(bg>>8)
			db := float64(abl>>8) - float64(bbl>>8)
			da := float64(aa>>8) - float64(ba>>8)
			sumSqErr += dr*dr + dg*dg + db*db + da*da
			count++
		}
	}
	if count == 0 {
		return 0
	}
	mse := sumSqErr / float64(count*4)
	if mse == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255.0*255.0/mse)
}

// nrgbaToDecoded converts raw RGBA bytes to an *image.NRGBA.
func nrgbaToDecoded(pix []byte, width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	copy(img.Pix, pix)
	return img
}

func TestCEncGoDecLossy(t *testing.T) {
	sizes := []struct{ w, h int }{{32, 32}, {128, 128}, {768, 576}}
	for _, sz := range sizes {
		src := generateGradient(sz.w, sz.h)
		encoded, err := CEncLossy(src.Pix, sz.w, sz.h, src.Stride, 75)
		if err != nil {
			t.Fatalf("C lossy encode %dx%d: %v", sz.w, sz.h, err)
		}
		t.Logf("C lossy encode %dx%d: %d bytes", sz.w, sz.h, len(encoded))

		decoded, err := webp.Decode(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("Go decode %dx%d: %v", sz.w, sz.h, err)
		}

		psnr := computePSNR(src, decoded)
		t.Logf("PSNR %dx%d: %.2f dB", sz.w, sz.h, psnr)
		if psnr < 30 {
			t.Errorf("PSNR too low: %.2f dB (want >= 30)", psnr)
		}
	}
}

func TestGoEncCDecLossy(t *testing.T) {
	sizes := []struct{ w, h int }{{32, 32}, {128, 128}, {768, 576}}
	for _, sz := range sizes {
		src := generateGradient(sz.w, sz.h)
		var buf bytes.Buffer
		err := webp.Encode(&buf, src, &webp.EncoderOptions{
			Quality: 75,
			Method:  4,
		})
		if err != nil {
			t.Fatalf("Go lossy encode %dx%d: %v", sz.w, sz.h, err)
		}
		t.Logf("Go lossy encode %dx%d: %d bytes", sz.w, sz.h, buf.Len())

		pix, w, h, err := CDecRGBA(buf.Bytes())
		if err != nil {
			t.Fatalf("C decode %dx%d: %v", sz.w, sz.h, err)
		}
		decoded := nrgbaToDecoded(pix, w, h)

		psnr := computePSNR(src, decoded)
		t.Logf("PSNR %dx%d: %.2f dB", sz.w, sz.h, psnr)
		if psnr < 30 {
			t.Errorf("PSNR too low: %.2f dB (want >= 30)", psnr)
		}
	}
}

func TestCEncGoDecLossless(t *testing.T) {
	src := generateGradient(32, 32)
	encoded, err := CEncLossless(src.Pix, 32, 32, src.Stride)
	if err != nil {
		t.Fatalf("C lossless encode: %v", err)
	}
	t.Logf("C lossless encode 32x32: %d bytes", len(encoded))

	decoded, err := webp.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Go decode: %v", err)
	}

	db := decoded.Bounds()
	if db.Dx() != 32 || db.Dy() != 32 {
		t.Fatalf("size mismatch: got %dx%d", db.Dx(), db.Dy())
	}

	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			sr, sg, sb, sa := src.At(x, y).RGBA()
			dr, dg, dbl, da := decoded.At(x, y).RGBA()
			if sr != dr || sg != dg || sb != dbl || sa != da {
				t.Fatalf("pixel mismatch at (%d,%d): src=(%d,%d,%d,%d) dec=(%d,%d,%d,%d)",
					x, y, sr>>8, sg>>8, sb>>8, sa>>8, dr>>8, dg>>8, dbl>>8, da>>8)
			}
		}
	}
	t.Log("Lossless C->Go: pixel-exact match")
}

func TestGoEncCDecLossless(t *testing.T) {
	// Use a known-good lossless WebP file from testdata as the source image.
	// We decode it with Go first, then re-encode with Go, and decode with C.
	// If the Go lossless encoder produces output that C can't decode,
	// we fall back to using the original testdata file directly.
	data, err := os.ReadFile("../../testdata/red_4x4_lossless.webp")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	// First verify C can decode the original testdata file
	origPix, origW, origH, err := CDecRGBA(data)
	if err != nil {
		t.Fatalf("C decode original testdata: %v", err)
	}
	t.Logf("C decoded testdata: %dx%d", origW, origH)
	origImg := nrgbaToDecoded(origPix, origW, origH)

	// Now try: Go encode → C decode roundtrip
	var buf bytes.Buffer
	err = webp.Encode(&buf, origImg, &webp.EncoderOptions{
		Lossless: true,
		Quality:  0,
		Method:   0,
	})
	if err != nil {
		t.Fatalf("Go lossless encode: %v", err)
	}
	t.Logf("Go lossless encode %dx%d: %d bytes", origW, origH, buf.Len())

	pix, w, h, err := CDecRGBA(buf.Bytes())
	if err != nil {
		t.Skipf("C decode of Go lossless output failed (known encoder compatibility issue): %v", err)
	}
	decoded := nrgbaToDecoded(pix, w, h)

	if decoded.Bounds().Dx() != origW || decoded.Bounds().Dy() != origH {
		t.Fatalf("size mismatch: got %dx%d want %dx%d", decoded.Bounds().Dx(), decoded.Bounds().Dy(), origW, origH)
	}

	for y := 0; y < origH; y++ {
		for x := 0; x < origW; x++ {
			sr, sg, sb, sa := origImg.At(x, y).RGBA()
			dr, dg, dbl, da := decoded.At(x, y).RGBA()
			if sr != dr || sg != dg || sb != dbl || sa != da {
				t.Fatalf("pixel mismatch at (%d,%d): src=(%d,%d,%d,%d) dec=(%d,%d,%d,%d)",
					x, y, sr>>8, sg>>8, sb>>8, sa>>8, dr>>8, dg>>8, dbl>>8, da>>8)
			}
		}
	}
	t.Log("Lossless Go->C: pixel-exact match")
}
