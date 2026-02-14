//go:build !amd64 && !arm64

package dsp

// FTransformDirect is a direct call to fTransform (pure Go fallback).
func FTransformDirect(src, ref []byte, out []int16) {
	fTransform(src, ref, out)
}

// ITransformDirect is a direct call to iTransform (pure Go fallback).
func ITransformDirect(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransform(ref, in, dst, doTwo)
}
