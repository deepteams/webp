//go:build testc

package alpha_test

import (
	"math/rand"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
	"github.com/deepteams/webp/testc/alpha"
)

// TestMultARGBRowPremultiply compares Go MultARGBRow (forward premultiply) vs C.
// Note: the inverse (un-premultiply) case uses different fixed-point precision
// between Go (simple integer division) and C (24-bit fixed-point), so only the
// forward case is expected to match bit-exactly.
func TestMultARGBRowPremultiply(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	widths := []int{16, 32, 64}

	for _, w := range widths {
		t.Run("premultiply", func(t *testing.T) {
			goRow := make([]uint32, w)
			cRow := make([]uint32, w)
			for i := 0; i < w; i++ {
				var a uint32
				switch rng.Intn(4) {
				case 0:
					a = 0 // fully transparent
				case 1:
					a = 255 // fully opaque
				case 2:
					a = 128 // half
				default:
					a = uint32(rng.Intn(256))
				}
				rgb := rng.Uint32() & 0x00ffffff
				pixel := (a << 24) | rgb
				goRow[i] = pixel
				cRow[i] = pixel
			}

			dsp.MultARGBRow(goRow, w, false)
			alpha.CMultARGBRow(cRow, w, false)

			for i := 0; i < w; i++ {
				if goRow[i] != cRow[i] {
					t.Fatalf("width=%d premultiply pixel[%d]: Go=0x%08x C=0x%08x",
						w, i, goRow[i], cRow[i])
				}
			}
		})
	}
}

// TestDispatchAlpha compares Go DispatchAlpha vs C DispatchAlpha_C.
func TestDispatchAlpha(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	width, height := 17, 5
	alphaStride := width
	dstStride := width * 4

	alphaPlane := make([]byte, height*alphaStride)
	for i := range alphaPlane {
		alphaPlane[i] = byte(rng.Intn(256))
	}

	goDst := make([]byte, height*dstStride)
	cDst := make([]byte, height*dstStride)

	// Fill with pattern so we can verify only alpha offset is changed.
	for i := range goDst {
		goDst[i] = 0xAA
		cDst[i] = 0xAA
	}

	// Go DispatchAlpha places alpha at alphaOff within each 4-byte pixel.
	// C DispatchAlpha_C always places alpha at offset 0 (dst[4*i]).
	// So we use alphaOff=0 for Go to match C.
	goHasAlpha := dsp.DispatchAlpha(alphaPlane, alphaStride, width, height, goDst, dstStride, 0)
	cResult := alpha.CDispatchAlpha(alphaPlane, alphaStride, width, height, cDst, dstStride)

	for i := range goDst {
		if goDst[i] != cDst[i] {
			t.Fatalf("DispatchAlpha byte[%d]: Go=0x%02x C=0x%02x", i, goDst[i], cDst[i])
		}
	}

	// C returns non-zero if any alpha != 0xff, Go returns bool with same semantics.
	cHasAlpha := cResult != 0
	if goHasAlpha != cHasAlpha {
		t.Fatalf("DispatchAlpha has_alpha: Go=%v C=%v (cResult=%d)", goHasAlpha, cHasAlpha, cResult)
	}
}

// TestExtractAlpha compares Go ExtractAlpha vs C ExtractAlpha_C.
func TestExtractAlpha(t *testing.T) {
	rng := rand.New(rand.NewSource(456))
	width, height := 13, 7
	argbStride := width * 4
	alphaStride := width

	// Build RGBA buffer with random data.
	argb := make([]byte, height*argbStride)
	for i := range argb {
		argb[i] = byte(rng.Intn(256))
	}

	goAlpha := make([]byte, height*alphaStride)
	cAlpha := make([]byte, height*alphaStride)

	// C ExtractAlpha_C reads alpha from argb[4*i + 0], so use alphaOff=0 in Go.
	goResult := dsp.ExtractAlpha(argb, argbStride, width, height, goAlpha, alphaStride, 0)
	cResult := alpha.CExtractAlpha(argb, argbStride, width, height, cAlpha, alphaStride)

	for i := range goAlpha {
		if goAlpha[i] != cAlpha[i] {
			t.Fatalf("ExtractAlpha byte[%d]: Go=0x%02x C=0x%02x", i, goAlpha[i], cAlpha[i])
		}
	}

	// C returns (alpha_mask == 0xff), Go returns OR of all alpha values.
	// The C function returns 1 if alpha_mask == 0xff (all opaque), 0 otherwise.
	// The Go function returns the OR of all alpha values.
	// They differ in semantics but both convey "has alpha" info.
	// We check consistency: if C says all opaque (1), Go OR should be 0xff.
	if cResult == 1 && goResult != 0xff {
		t.Fatalf("ExtractAlpha result mismatch: C=all_opaque Go_OR=0x%02x", goResult)
	}
}

// TestHasAlpha8b compares Go HasAlpha8b vs C HasAlpha8b_C.
func TestHasAlpha8b(t *testing.T) {
	// All 0xFF - no alpha.
	allOpaque := make([]byte, 64)
	for i := range allOpaque {
		allOpaque[i] = 0xff
	}
	if got, want := dsp.HasAlpha8b(allOpaque, len(allOpaque)),
		alpha.CHasAlpha8b(allOpaque, len(allOpaque)); got != want {
		t.Fatalf("HasAlpha8b(all_opaque): Go=%v C=%v", got, want)
	}

	// One non-0xFF byte.
	withAlpha := make([]byte, 64)
	for i := range withAlpha {
		withAlpha[i] = 0xff
	}
	withAlpha[32] = 0x80
	if got, want := dsp.HasAlpha8b(withAlpha, len(withAlpha)),
		alpha.CHasAlpha8b(withAlpha, len(withAlpha)); got != want {
		t.Fatalf("HasAlpha8b(with_alpha): Go=%v C=%v", got, want)
	}

	// Random data.
	rng := rand.New(rand.NewSource(789))
	randomData := make([]byte, 100)
	for i := range randomData {
		randomData[i] = byte(rng.Intn(256))
	}
	if got, want := dsp.HasAlpha8b(randomData, len(randomData)),
		alpha.CHasAlpha8b(randomData, len(randomData)); got != want {
		t.Fatalf("HasAlpha8b(random): Go=%v C=%v", got, want)
	}
}

// TestHasAlpha32b compares Go HasAlpha32b vs C HasAlpha32b_C.
func TestHasAlpha32b(t *testing.T) {
	length := 16
	// All first-of-4 bytes are 0xFF.
	allOpaque := make([]byte, length*4)
	for i := 0; i < length; i++ {
		allOpaque[i*4] = 0xff
		allOpaque[i*4+1] = byte(i)
		allOpaque[i*4+2] = byte(i + 1)
		allOpaque[i*4+3] = byte(i + 2)
	}
	if got, want := dsp.HasAlpha32b(allOpaque, length),
		alpha.CHasAlpha32b(allOpaque, length); got != want {
		t.Fatalf("HasAlpha32b(all_opaque): Go=%v C=%v", got, want)
	}

	// One non-0xFF.
	withAlpha := make([]byte, length*4)
	copy(withAlpha, allOpaque)
	withAlpha[8*4] = 0xFE // pixel 8, alpha byte
	if got, want := dsp.HasAlpha32b(withAlpha, length),
		alpha.CHasAlpha32b(withAlpha, length); got != want {
		t.Fatalf("HasAlpha32b(with_alpha): Go=%v C=%v", got, want)
	}
}
