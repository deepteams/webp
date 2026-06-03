package lossless

import (
	"bytes"
	"testing"
)

func TestEncodeInPlaceMatchesEncode(t *testing.T) {
	const width, height = 48, 35

	argb := make([]uint32, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint32((x*17 + y*3) & 0xff)
			g := uint32((x*5 + y*11) & 0xff)
			b := uint32((x*y + x*13 + y*7) & 0xff)
			argb[y*width+x] = 0xff000000 | r<<16 | g<<8 | b
		}
	}

	cfg := &EncoderConfig{
		Quality:             75,
		Method:              4,
		NearLosslessQuality: 100,
	}

	want, err := Encode(argb, width, height, cfg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	for i := 0; i < 2; i++ {
		input := append([]uint32(nil), argb...)
		got, err := EncodeInPlace(input, width, height, cfg)
		if err != nil {
			t.Fatalf("EncodeInPlace run %d: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("EncodeInPlace run %d produced different bitstream: got %d bytes, want %d", i, len(got), len(want))
		}
	}
}

func TestEncodeToWriterInPlaceMatchesEncodeToWriter(t *testing.T) {
	const width, height = 48, 35

	argb := make([]uint32, width*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint32((x*19 + y*5) & 0xff)
			g := uint32((x*7 + y*23) & 0xff)
			b := uint32((x*y + x*3 + y*29) & 0xff)
			argb[y*width+x] = 0xff000000 | r<<16 | g<<8 | b
		}
	}

	cfg := &EncoderConfig{
		Quality:             75,
		Method:              4,
		NearLosslessQuality: 100,
	}

	var want bytes.Buffer
	if err := EncodeToWriter(argb, width, height, cfg, &want, nil); err != nil {
		t.Fatalf("EncodeToWriter: %v", err)
	}

	for i := 0; i < 2; i++ {
		input := append([]uint32(nil), argb...)
		var got bytes.Buffer
		if err := EncodeToWriterInPlace(input, width, height, cfg, &got, nil); err != nil {
			t.Fatalf("EncodeToWriterInPlace run %d: %v", i, err)
		}
		if !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Fatalf("EncodeToWriterInPlace run %d produced different stream: got %d bytes, want %d", i, got.Len(), want.Len())
		}
	}
}
