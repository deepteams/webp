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

// TDisto4x4 computes perceptual Hadamard-domain distortion for a 4x4 block.
func TDisto4x4(a, b []byte) int { return tDisto4x4Go(a, b) }

// TDisto16x16 computes perceptual Hadamard-domain distortion for a 16x16 block.
func TDisto16x16(a, b []byte) int { return tDisto16x16Go(a, b) }
