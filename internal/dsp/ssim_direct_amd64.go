//go:build amd64

package dsp

// SSE4x4Direct computes SSE for a 4x4 block using SSE2 assembly.
func SSE4x4Direct(pix, ref []byte) int {
	return sse4x4SSE2(pix, ref)
}

// SSE16x16Direct computes SSE for a 16x16 block.
// Uses AVX2 when available, falls back to SSE2.
func SSE16x16Direct(pix, ref []byte) int {
	if hasAVX2 {
		return sse16x16AVX2(pix, ref)
	}
	return sse16x16SSE2(pix, ref)
}

// TDisto4x4 computes perceptual Hadamard-domain distortion for a 4x4 block.
// Uses AVX2 dual-lane when available, falls back to SSE2.
func TDisto4x4(a, b []byte) int {
	if hasAVX2 {
		return tDisto4x4AVX2(a, b)
	}
	return tDisto4x4SSE2(a, b)
}

// TDisto16x16 computes perceptual Hadamard-domain distortion for a 16x16 block.
// Uses AVX2 when available, falls back to SSE2.
func TDisto16x16(a, b []byte) int {
	distFn := tDisto4x4SSE2
	if hasAVX2 {
		distFn = tDisto4x4AVX2
	}
	d := 0
	for y := 0; y < 16*BPS; y += 4 * BPS {
		for x := 0; x < 16; x += 4 {
			d += distFn(a[x+y:], b[x+y:])
		}
	}
	return d
}
