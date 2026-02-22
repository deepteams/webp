//go:build amd64

package dsp

// SimpleVFilter16 applies the simple loop filter vertically across a 16-wide edge.
// Uses AVX2 when available, falls back to SSE2.
func SimpleVFilter16(p []byte, base, stride, thresh int) {
	if hasAVX2 {
		simpleVFilter16AVX2(p, base, stride, thresh)
		return
	}
	simpleVFilter16SSE2(p, base, stride, thresh)
}
