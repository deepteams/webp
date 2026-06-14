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

func TestConcurrentEncodeRace(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))

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
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0, 255})
		}
	}

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
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.Set(x, y, color.RGBA{uint8(x ^ y), uint8(x), uint8(y), 255})
		}
	}

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
