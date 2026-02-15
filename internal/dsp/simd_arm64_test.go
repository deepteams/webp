//go:build arm64

package dsp

import (
	"math/rand"
	"testing"
)

func BenchmarkFTransformNEON(b *testing.B) {
	rng := rand.New(rand.NewSource(210))
	src := makeRandBuf(rng, 4*BPS)
	ref := makeRandBuf(rng, 4*BPS)
	out := make([]int16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FTransformNEON(src, ref, out)
	}
}

func TestFTransformNEONConformance(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for iter := 0; iter < 1000; iter++ {
		src := makeRandBuf(rng, 4*BPS)
		ref := makeRandBuf(rng, 4*BPS)
		var goOut, neonOut [16]int16
		FTransformDirect(src, ref, goOut[:])
		FTransformNEON(src, ref, neonOut[:])
		for i := 0; i < 16; i++ {
			if goOut[i] != neonOut[i] {
				t.Fatalf("iter %d: mismatch at [%d]: go=%d neon=%d", iter, i, goOut[i], neonOut[i])
			}
		}
	}
}
