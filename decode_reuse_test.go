package webp

import (
	"bytes"
	"image"
	"testing"
)

// encodeTestWebP encodes the standard test image and returns the bytes.
func encodeTestWebP(t *testing.T, lossless bool, withAlpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 160, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 160; x++ {
			i := y*img.Stride + x*4
			img.Pix[i] = uint8(x)
			img.Pix[i+1] = uint8(y)
			img.Pix[i+2] = uint8(x + y)
			if withAlpha {
				img.Pix[i+3] = uint8(255 - x)
			} else {
				img.Pix[i+3] = 255
			}
		}
	}
	buf := &bytes.Buffer{}
	if err := Encode(buf, img, &EncoderOptions{Lossless: lossless, Quality: 75}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeReuseMatchesDecode(t *testing.T) {
	cases := []struct {
		name     string
		lossless bool
		alpha    bool
	}{
		{"lossy", false, false},
		{"lossy-alpha", false, true},
		{"lossless", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := encodeTestWebP(t, tc.lossless, tc.alpha)

			want, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}

			// First call with nil reuse, then feed the result back twice.
			var got image.Image
			for i := 0; i < 3; i++ {
				got, err = DecodeReuse(bytes.NewReader(data), got)
				if err != nil {
					t.Fatalf("iteration %d: %v", i, err)
				}
			}

			if got.Bounds() != want.Bounds() {
				t.Fatalf("bounds: got %v, want %v", got.Bounds(), want.Bounds())
			}
			b := want.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if got.At(x, y) != want.At(x, y) {
						t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got.At(x, y), want.At(x, y))
					}
				}
			}
		})
	}
}

func TestDecodeReuseIncompatibleFallsBack(t *testing.T) {
	dataLossless := encodeTestWebP(t, true, false)

	// Reuse of the wrong type must be ignored, not crash.
	wrongType := image.NewYCbCr(image.Rect(0, 0, 160, 120), image.YCbCrSubsampleRatio420)
	img2, err := DecodeReuse(bytes.NewReader(dataLossless), wrongType)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img2.(*image.NRGBA); !ok {
		t.Fatalf("expected *image.NRGBA, got %T", img2)
	}

	// Too-small reuse must be ignored as well.
	small := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img3, err := DecodeReuse(bytes.NewReader(dataLossless), small)
	if err != nil {
		t.Fatal(err)
	}
	if img3.Bounds().Dx() != 160 || img3.Bounds().Dy() != 120 {
		t.Fatalf("unexpected bounds %v", img3.Bounds())
	}
}

func TestDecodeReuseSharesStorage(t *testing.T) {
	data := encodeTestWebP(t, true, false)

	img1, err := DecodeReuse(bytes.NewReader(data), nil)
	if err != nil {
		t.Fatal(err)
	}
	img2, err := DecodeReuse(bytes.NewReader(data), img1)
	if err != nil {
		t.Fatal(err)
	}
	p1 := img1.(*image.NRGBA).Pix
	p2 := img2.(*image.NRGBA).Pix
	if &p1[0] != &p2[0] {
		t.Fatal("expected Pix storage to be reused")
	}
}
