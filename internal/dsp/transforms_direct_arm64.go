//go:build arm64

package dsp

// FTransformDirect computes forward DCT using the pure Go implementation.
func FTransformDirect(src, ref []byte, out []int16) {
	fTransform(src, ref, out)
}

// ITransformDirect computes inverse DCT using the pure Go implementation.
func ITransformDirect(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransform(ref, in, dst, doTwo)
}
