//go:build arm64

package dsp

// SSE4x4Direct computes SSE for a 4x4 block using NEON assembly.
func SSE4x4Direct(pix, ref []byte) int {
	return sse4x4NEON(pix, ref)
}

// SSE16x16Direct computes SSE for a 16x16 block using NEON assembly.
func SSE16x16Direct(pix, ref []byte) int {
	return sse16x16NEON(pix, ref)
}
