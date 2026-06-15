package webp_test

import (
	"bytes"
	"image"
	"image/color"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/deepteams/webp"
)

func gradient(w, h, seed int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x + seed) & 0xFF),
				G: uint8((y + seed) & 0xFF),
				B: 128,
				A: 255,
			})
		}
	}
	return img
}

// TestConcurrentEncodeDeterminism encodes image A while goroutines hammer the
// shared encoder pool with image B at matching dimensions, then checks every
// image-A encode is byte-identical to a canonical reference.
func TestConcurrentEncodeDeterminism(t *testing.T) {
	imgA := gradient(640, 480, 0)
	imgB := gradient(640, 480, 1)
	opts := &webp.EncoderOptions{Quality: 80}

	var canonical bytes.Buffer
	if err := webp.Encode(&canonical, imgA, opts); err != nil {
		t.Fatalf("canonical encode: %v", err)
	}

	const iterations = 50
	const noisemakers = 16
	fail := 0

	for i := 0; i < iterations; i++ {
		var wg sync.WaitGroup
		wg.Add(noisemakers)
		for j := 0; j < noisemakers; j++ {
			go func() {
				defer wg.Done()
				_ = webp.Encode(io.Discard, imgB, opts)
			}()
		}

		var buf bytes.Buffer
		if err := webp.Encode(&buf, imgA, opts); err != nil {
			t.Errorf("iter %d: encode A err = %v", i, err)
			wg.Wait()
			continue
		}
		wg.Wait()

		if !bytes.Equal(buf.Bytes(), canonical.Bytes()) {
			fail++
		}
	}

	if fail != 0 {
		t.Fatalf("%d/%d iterations produced non-canonical bytes; pool state is leaking across encodes", fail, iterations)
	}
}

func TestConcurrentEncodeRace(t *testing.T) {
	img := gradient(256, 256, 0)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_ = webp.Encode(io.Discard, img, &webp.EncoderOptions{Quality: 80})
		}()
	}
	wg.Wait()
}

func TestConcurrentEncodeLosslessRace(t *testing.T) {
	img := gradient(64, 64, 0)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_ = webp.Encode(io.Discard, img, &webp.EncoderOptions{Lossless: true})
		}()
	}
	wg.Wait()
}

func TestConcurrentDecodeRace(t *testing.T) {
	lossy, err := os.ReadFile("testdata/blue_16x16_lossy.webp")
	if err != nil {
		t.Fatal(err)
	}
	lossless, err := os.ReadFile("testdata/red_4x4_lossless.webp")
	if err != nil {
		t.Fatal(err)
	}

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n * 2)
	for range n {
		go func() {
			defer wg.Done()
			_, _ = webp.Decode(bytes.NewReader(lossy))
		}()
		go func() {
			defer wg.Done()
			_, _ = webp.Decode(bytes.NewReader(lossless))
		}()
	}
	wg.Wait()
}

func TestConcurrentEncodeDecodeRace(t *testing.T) {
	img := gradient(128, 128, 0)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			var buf bytes.Buffer
			if err := webp.Encode(&buf, img, &webp.EncoderOptions{Quality: 75}); err != nil {
				t.Errorf("encode: %v", err)
				return
			}
			if _, err := webp.Decode(&buf); err != nil {
				t.Errorf("decode: %v", err)
			}
		}()
	}
	wg.Wait()
}
