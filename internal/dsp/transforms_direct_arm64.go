//go:build arm64

package dsp

// FTransformDirect computes forward DCT using the pure Go implementation.
// NEON is slower than Go for FTransform due to strided byte packing overhead
// (INS chain on M2 Pro: 16.2ns NEON vs 13.3ns Go, benchmarked 2026-02-15).
func FTransformDirect(src, ref []byte, out []int16) {
	fTransform(src, ref, out)
}

// ITransformDirect computes inverse DCT using NEON assembly.
// Unlike FTransform, NEON is faster for ITransform because the output is
// byte-typed (UQXTN/SQXTUN pack efficiently) and the ref+residual addition
// maps well to UADDW instructions.
func ITransformDirect(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransformOneNEON(ref, in, dst)
	if doTwo {
		iTransformOneNEON(ref[4:], in[16:], dst[4:])
	}
}
