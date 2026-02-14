//go:build !amd64 && !arm64

package dsp

// SSE4x4Direct computes SSE for a 4x4 block (pure Go fallback).
func SSE4x4Direct(pix, ref []byte) int {
	return sse4x4(pix, ref)
}

// SSE16x16Direct computes SSE for a 16x16 block (pure Go fallback).
func SSE16x16Direct(pix, ref []byte) int {
	return sse16x16(pix, ref)
}
