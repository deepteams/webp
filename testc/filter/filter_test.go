//go:build testc

package filter_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
	"github.com/deepteams/webp/testc/filter"
)

const (
	bps     = 32
	trials  = 200
	bufSize = 32 * bps
)

func init() {
	filter.Init()
}

// fillRandBuf fills buf with random pixel values.
func fillRandBuf(rng *rand.Rand, buf []byte) {
	for i := range buf {
		buf[i] = uint8(rng.Intn(256))
	}
}

// compareBuf checks that Go and C buffers match byte-by-byte.
func compareBuf(t *testing.T, name string, trial int, goBuf, cBuf []byte) {
	t.Helper()
	for i := range goBuf {
		if goBuf[i] != cBuf[i] {
			row := i / bps
			col := i % bps
			t.Fatalf("%s trial %d: mismatch at byte %d (row=%d, col=%d): Go=%d, C=%d",
				name, trial, i, row, col, goBuf[i], cBuf[i])
		}
	}
}

func TestSimpleVFilter16(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	thresholds := []int{1, 5, 10, 40, 63}

	for _, thresh := range thresholds {
		for trial := 0; trial < trials; trial++ {
			goBuf := make([]byte, bufSize)
			cBuf := make([]byte, bufSize)
			fillRandBuf(rng, goBuf)
			copy(cBuf, goBuf)

			off := 4 * bps
			dsp.SimpleVFilter16(goBuf, off, bps, thresh)
			filter.CSimpleVFilter16(&cBuf[off], bps, thresh)

			compareBuf(t, "SimpleVFilter16", trial, goBuf, cBuf)
		}
	}
}

func TestSimpleHFilter16(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	thresholds := []int{1, 5, 10, 40, 63}

	for _, thresh := range thresholds {
		for trial := 0; trial < trials; trial++ {
			goBuf := make([]byte, bufSize)
			cBuf := make([]byte, bufSize)
			fillRandBuf(rng, goBuf)
			copy(cBuf, goBuf)

			off := 4
			dsp.SimpleHFilter16(goBuf, off, bps, thresh)
			filter.CSimpleHFilter16(&cBuf[off], bps, thresh)

			compareBuf(t, "SimpleHFilter16", trial, goBuf, cBuf)
		}
	}
}

func TestSimpleVFilter16i(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	thresholds := []int{1, 5, 10, 40, 63}

	for _, thresh := range thresholds {
		for trial := 0; trial < trials; trial++ {
			goBuf := make([]byte, bufSize)
			cBuf := make([]byte, bufSize)
			fillRandBuf(rng, goBuf)
			copy(cBuf, goBuf)

			off := 2 * bps
			dsp.SimpleVFilter16i(goBuf, off, bps, thresh)
			filter.CSimpleVFilter16i(&cBuf[off], bps, thresh)

			compareBuf(t, "SimpleVFilter16i", trial, goBuf, cBuf)
		}
	}
}

func TestSimpleHFilter16i(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	thresholds := []int{1, 5, 10, 40, 63}

	for _, thresh := range thresholds {
		for trial := 0; trial < trials; trial++ {
			goBuf := make([]byte, bufSize)
			cBuf := make([]byte, bufSize)
			fillRandBuf(rng, goBuf)
			copy(cBuf, goBuf)

			off := 2
			dsp.SimpleHFilter16i(goBuf, off, bps, thresh)
			filter.CSimpleHFilter16i(&cBuf[off], bps, thresh)

			compareBuf(t, "SimpleHFilter16i", trial, goBuf, cBuf)
		}
	}
}

func TestVFilter16(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	type params struct {
		thresh, ithresh, hevT int
	}
	combos := []params{
		{1, 0, 0}, {5, 1, 0}, {10, 5, 1}, {40, 1, 2}, {63, 5, 2},
	}

	for _, p := range combos {
		t.Run(fmt.Sprintf("t%d_i%d_h%d", p.thresh, p.ithresh, p.hevT), func(t *testing.T) {
			for trial := 0; trial < trials; trial++ {
				goBuf := make([]byte, bufSize)
				cBuf := make([]byte, bufSize)
				fillRandBuf(rng, goBuf)
				copy(cBuf, goBuf)

				off := 8 * bps
				dsp.VFilter16(goBuf, off, bps, p.thresh, p.ithresh, p.hevT)
				filter.CVFilter16(&cBuf[off], bps, p.thresh, p.ithresh, p.hevT)

				compareBuf(t, "VFilter16", trial, goBuf, cBuf)
			}
		})
	}
}

func TestHFilter16(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	type params struct {
		thresh, ithresh, hevT int
	}
	combos := []params{
		{1, 0, 0}, {5, 1, 0}, {10, 5, 1}, {40, 1, 2}, {63, 5, 2},
	}

	for _, p := range combos {
		t.Run(fmt.Sprintf("t%d_i%d_h%d", p.thresh, p.ithresh, p.hevT), func(t *testing.T) {
			for trial := 0; trial < trials; trial++ {
				goBuf := make([]byte, bufSize)
				cBuf := make([]byte, bufSize)
				fillRandBuf(rng, goBuf)
				copy(cBuf, goBuf)

				off := 8
				dsp.HFilter16(goBuf, off, bps, p.thresh, p.ithresh, p.hevT)
				filter.CHFilter16(&cBuf[off], bps, p.thresh, p.ithresh, p.hevT)

				compareBuf(t, "HFilter16", trial, goBuf, cBuf)
			}
		})
	}
}

func TestVFilter8(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	type params struct {
		thresh, ithresh, hevT int
	}
	combos := []params{
		{1, 0, 0}, {5, 1, 0}, {10, 5, 1}, {40, 1, 2}, {63, 5, 2},
	}

	for _, p := range combos {
		t.Run(fmt.Sprintf("t%d_i%d_h%d", p.thresh, p.ithresh, p.hevT), func(t *testing.T) {
			for trial := 0; trial < trials; trial++ {
				goU := make([]byte, bufSize)
				goV := make([]byte, bufSize)
				cU := make([]byte, bufSize)
				cV := make([]byte, bufSize)
				fillRandBuf(rng, goU)
				fillRandBuf(rng, goV)
				copy(cU, goU)
				copy(cV, goV)

				off := 8 * bps
				dsp.VFilter8(goU, goV, off, off, bps, p.thresh, p.ithresh, p.hevT)
				filter.CVFilter8(&cU[off], &cV[off], bps, p.thresh, p.ithresh, p.hevT)

				compareBuf(t, "VFilter8/U", trial, goU, cU)
				compareBuf(t, "VFilter8/V", trial, goV, cV)
			}
		})
	}
}

func TestHFilter8(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	type params struct {
		thresh, ithresh, hevT int
	}
	combos := []params{
		{1, 0, 0}, {5, 1, 0}, {10, 5, 1}, {40, 1, 2}, {63, 5, 2},
	}

	for _, p := range combos {
		t.Run(fmt.Sprintf("t%d_i%d_h%d", p.thresh, p.ithresh, p.hevT), func(t *testing.T) {
			for trial := 0; trial < trials; trial++ {
				goU := make([]byte, bufSize)
				goV := make([]byte, bufSize)
				cU := make([]byte, bufSize)
				cV := make([]byte, bufSize)
				fillRandBuf(rng, goU)
				fillRandBuf(rng, goV)
				copy(cU, goU)
				copy(cV, goV)

				off := 8
				dsp.HFilter8(goU, goV, off, off, bps, p.thresh, p.ithresh, p.hevT)
				filter.CHFilter8(&cU[off], &cV[off], bps, p.thresh, p.ithresh, p.hevT)

				compareBuf(t, "HFilter8/U", trial, goU, cU)
				compareBuf(t, "HFilter8/V", trial, goV, cV)
			}
		})
	}
}
