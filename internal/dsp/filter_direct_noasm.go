//go:build !amd64

package dsp

// SimpleVFilter16 applies the simple loop filter vertically across a 16-wide edge.
// On non-amd64 platforms, uses the pure Go implementation.
func SimpleVFilter16(p []byte, base, stride, thresh int) {
	simpleVFilter16Go(p, base, stride, thresh)
}
