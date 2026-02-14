//go:build arm64

package dsp

// SSE4x4Direct computes SSE for a 4x4 block using the Go implementation.
// The scalar ARM64 assembly is slower than Go's optimized scalar code,
// so we keep the Go version here.
func SSE4x4Direct(pix, ref []byte) int {
	return sse4x4(pix, ref)
}

// SSE16x16Direct computes SSE for a 16x16 block using the Go implementation.
func SSE16x16Direct(pix, ref []byte) int {
	return sse16x16(pix, ref)
}
