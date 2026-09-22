package webp

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"testing"
)

func TestDecodeLossyPreservesVideoRangeEndpoints(t *testing.T) {
	fixtures := []struct {
		name  string
		data  string
		white bool
	}{
		{
			name: "black",
			data: "UklGRjYAAABXRUJQVlA4ICoAAAAwAwCdASpAAEAAPj0ejUQhBEEABMDxPSAAAgbqagCvELcgAP7+BtAAAAA=",
		},
		{
			name:  "white",
			data:  "UklGRjYAAABXRUJQVlA4ICoAAAAwAwCdASpAAEAAPj0ejUQhBEEABMDxPSAAAgbqagCvELcgAP79wIAAAAA=",
			white: true,
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			data, err := base64.StdEncoding.DecodeString(fixture.data)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := decoded.(*image.NRGBA); !ok {
				t.Fatalf("decoded type = %T, want *image.NRGBA", decoded)
			}
			assertEndpoint(t, decoded.At(32, 32), fixture.white)
		})
	}
}

func assertEndpoint(t *testing.T, pixel color.Color, white bool) {
	t.Helper()
	c := color.NRGBAModel.Convert(pixel).(color.NRGBA)
	if white {
		if c.R < 251 || c.G < 251 || c.B < 251 {
			t.Fatalf("white = %v, want every RGB channel >= 251", c)
		}
		return
	}
	if c.R > 4 || c.G > 4 || c.B > 4 {
		t.Fatalf("black = %v, want every RGB channel <= 4", c)
	}
}

func TestLossyDecodeEncodeDoesNotProgressivelyContractRange(t *testing.T) {
	var current image.Image = blackAndWhiteImage()
	for generation := 1; generation <= 3; generation++ {
		data := encodeBlackAndWhiteLossy(t, current)
		decoded, err := Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("generation %d: %v", generation, err)
		}
		assertBlackAndWhiteEndpoints(t, decoded, generation)
		current = decoded
	}
}

func blackAndWhiteImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			c := color.NRGBA{A: 255}
			if x >= img.Bounds().Dx()/2 {
				c.R, c.G, c.B = 255, 255, 255
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func encodeBlackAndWhiteLossy(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, img, &EncoderOptions{Quality: 100, Method: 4}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertBlackAndWhiteEndpoints(t *testing.T, img image.Image, generation int) {
	t.Helper()
	black := color.NRGBAModel.Convert(img.At(8, 32)).(color.NRGBA)
	white := color.NRGBAModel.Convert(img.At(56, 32)).(color.NRGBA)
	if black.R > 4 || black.G > 4 || black.B > 4 {
		t.Errorf("generation %d black = %v, want every RGB channel <= 4", generation, black)
	}
	if white.R < 251 || white.G < 251 || white.B < 251 {
		t.Errorf("generation %d white = %v, want every RGB channel >= 251", generation, white)
	}
}
