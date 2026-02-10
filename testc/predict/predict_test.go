//go:build testc

package predict_test

import (
	"math/rand"
	"testing"

	"github.com/deepteams/webp/internal/dsp"
	"github.com/deepteams/webp/testc/predict"
)

const (
	bps     = 32
	trials  = 100
	bufSize = (16 + 4) * bps // enough for 16x16 blocks with ref rows
)

func init() {
	predict.Init()
}

// fillRandRef fills the reference pixels (top row, left column, top-left)
// for a block of the given size in buf at startOffset.
func fillRandRef(rng *rand.Rand, buf []byte, startOff, blockSize int) {
	// Top-left corner
	buf[startOff-bps-1] = uint8(rng.Intn(256))
	// Top row (include extra pixels for 4x4 modes that read up to +8)
	for i := 0; i < blockSize+4; i++ {
		buf[startOff-bps+i] = uint8(rng.Intn(256))
	}
	// Left column
	for j := 0; j < blockSize; j++ {
		buf[startOff-1+j*bps] = uint8(rng.Intn(256))
	}
}

// clearBlock zeroes the block area so prediction results are clean.
func clearBlock(buf []byte, startOff, blockSize int) {
	for j := 0; j < blockSize; j++ {
		for i := 0; i < blockSize; i++ {
			buf[startOff+i+j*bps] = 0
		}
	}
}

// compareBlock checks that the block area matches between Go and C buffers.
func compareBlock(t *testing.T, name string, trial int, goBuf, cBuf []byte, startOff, blockSize int) {
	t.Helper()
	for j := 0; j < blockSize; j++ {
		for i := 0; i < blockSize; i++ {
			idx := startOff + i + j*bps
			if goBuf[idx] != cBuf[idx] {
				t.Fatalf("%s trial %d: mismatch at (%d,%d): Go=%d, C=%d",
					name, trial, i, j, goBuf[idx], cBuf[idx])
			}
		}
	}
}

type predCase struct {
	name      string
	goFunc    func([]byte, int)
	cFunc     func(*byte)
	blockSize int
}

func TestPredict4x4(t *testing.T) {
	cases := []predCase{
		{"DC4", dsp.PredLuma4[0], predict.CDc4, 4},
		{"TM4", dsp.PredLuma4[1], predict.CTm4, 4},
		{"VE4", dsp.PredLuma4[2], predict.CVe4, 4},
		{"HE4", dsp.PredLuma4[3], predict.CHe4, 4},
		{"RD4", dsp.PredLuma4[4], predict.CRd4, 4},
		{"VR4", dsp.PredLuma4[5], predict.CVr4, 4},
		{"LD4", dsp.PredLuma4[6], predict.CLd4, 4},
		{"VL4", dsp.PredLuma4[7], predict.CVl4, 4},
		{"HD4", dsp.PredLuma4[8], predict.CHd4, 4},
		{"HU4", dsp.PredLuma4[9], predict.CHu4, 4},
	}
	runPredCases(t, cases)
}

func TestPredict16x16(t *testing.T) {
	cases := []predCase{
		{"DC16", dsp.PredLuma16[0], predict.CDc16, 16},
		{"TM16", dsp.PredLuma16[1], predict.CTm16, 16},
		{"VE16", dsp.PredLuma16[2], predict.CVe16, 16},
		{"HE16", dsp.PredLuma16[3], predict.CHe16, 16},
		{"DC16NoTop", dsp.PredLuma16[4], predict.CDc16NoTop, 16},
		{"DC16NoLeft", dsp.PredLuma16[5], predict.CDc16NoLeft, 16},
		{"DC16NoTopLeft", dsp.PredLuma16[6], predict.CDc16NoTopLeft, 16},
	}
	runPredCases(t, cases)
}

func TestPredict8x8(t *testing.T) {
	cases := []predCase{
		{"DC8uv", dsp.PredChroma8[0], predict.CDc8uv, 8},
		{"TM8uv", dsp.PredChroma8[1], predict.CTm8uv, 8},
		{"VE8uv", dsp.PredChroma8[2], predict.CVe8uv, 8},
		{"HE8uv", dsp.PredChroma8[3], predict.CHe8uv, 8},
		{"DC8uvNoTop", dsp.PredChroma8[4], predict.CDc8uvNoTop, 8},
		{"DC8uvNoLeft", dsp.PredChroma8[5], predict.CDc8uvNoLeft, 8},
		{"DC8uvNoTopLeft", dsp.PredChroma8[6], predict.CDc8uvNoTopLeft, 8},
	}
	runPredCases(t, cases)
}

func runPredCases(t *testing.T, cases []predCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(42))
			goBuf := make([]byte, bufSize)
			cBuf := make([]byte, bufSize)

			// startOff gives room for top row (-BPS) and left column (-1)
			startOff := bps + 1

			for trial := 0; trial < trials; trial++ {
				// Fill reference pixels in both buffers identically
				fillRandRef(rng, goBuf, startOff, tc.blockSize)
				copy(cBuf, goBuf)

				// Clear block areas
				clearBlock(goBuf, startOff, tc.blockSize)
				clearBlock(cBuf, startOff, tc.blockSize)

				// Run Go prediction
				tc.goFunc(goBuf, startOff)

				// Run C prediction: pass pointer to the block origin
				tc.cFunc(&cBuf[startOff])

				// Compare
				compareBlock(t, tc.name, trial, goBuf, cBuf, startOff, tc.blockSize)
			}
		})
	}
}
