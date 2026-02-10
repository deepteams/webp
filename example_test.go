package webp_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"

	"github.com/deepteams/webp"
)

func ExampleDecode() {
	f, err := os.Open("testdata/red_4x4_lossy.webp")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()

	img, err := webp.Decode(f)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("bounds: %v\n", img.Bounds())
	// Output:
	// bounds: (0,0)-(4,4)
}

func ExampleDecodeConfig() {
	f, err := os.Open("testdata/blue_16x16_lossy.webp")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()

	cfg, err := webp.DecodeConfig(f)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%dx%d\n", cfg.Width, cfg.Height)
	// Output:
	// 16x16
}

func ExampleGetFeatures() {
	f, err := os.Open("testdata/red_4x4_lossless.webp")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()

	feat, err := webp.GetFeatures(f)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("size: %dx%d\n", feat.Width, feat.Height)
	fmt.Printf("format: %s\n", feat.Format)
	fmt.Printf("alpha: %v\n", feat.HasAlpha)
	// Output:
	// size: 4x4
	// format: lossless
	// alpha: false
}

func ExampleEncode_lossy() {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	err := webp.Encode(&buf, img, &webp.EncoderOptions{
		Quality: 80,
		Method:  4,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("encoded %d bytes\n", buf.Len())
	if buf.Len() > 0 {
		fmt.Println("ok")
	}
	// Output:
	// encoded 208 bytes
	// ok
}

func ExampleEncode_lossless() {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	var buf bytes.Buffer
	err := webp.Encode(&buf, img, &webp.EncoderOptions{
		Lossless: true,
		Quality:  75,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	if buf.Len() > 0 {
		fmt.Println("ok")
	}
	// Output:
	// ok
}

func ExampleEncode_roundtrip() {
	// Create a simple image.
	original := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			original.SetNRGBA(x, y, color.NRGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}

	// Encode to WebP lossless.
	var buf bytes.Buffer
	err := webp.Encode(&buf, original, &webp.EncoderOptions{Lossless: true})
	if err != nil {
		fmt.Println(err)
		return
	}

	// Decode back.
	decoded, err := webp.Decode(&buf)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Verify pixel values match (lossless is exact).
	p := decoded.(*image.NRGBA).NRGBAAt(0, 0)
	fmt.Printf("R=%d G=%d B=%d A=%d\n", p.R, p.G, p.B, p.A)
	// Output:
	// R=100 G=150 B=200 A=255
}

func ExampleDefaultOptions() {
	opts := webp.DefaultOptions()
	fmt.Printf("quality: %.0f\n", opts.Quality)
	fmt.Printf("lossless: %v\n", opts.Lossless)
	fmt.Printf("method: %d\n", opts.Method)
	// Output:
	// quality: 75
	// lossless: false
	// method: 4
}

func ExampleOptionsForPreset() {
	opts := webp.OptionsForPreset(webp.PresetPhoto, 90)
	fmt.Printf("quality: %.0f\n", opts.Quality)
	fmt.Printf("sns: %d\n", opts.SNSStrength)
	// Output:
	// quality: 90
	// sns: 80
}
