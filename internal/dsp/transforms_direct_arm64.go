//go:build arm64

package dsp

// FTransformDirect computes forward DCT using the pure Go implementation.
// NEON is slower than Go for FTransform due to strided byte packing overhead
// (INS chain on M2 Pro: 16.2ns NEON vs 13.3ns Go, benchmarked 2026-02-15).
func FTransformDirect(src, ref []byte, out []int16) {
	fTransform(src, ref, out)
}

// ITransformDirect computes inverse DCT using the pure Go implementation.
// NEON iTransformOneNEON has lower per-call latency but the two-call overhead
// for doTwo=true and asm frame setup costs offset the gain in encoder workloads.
func ITransformDirect(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransform(ref, in, dst, doTwo)
}
