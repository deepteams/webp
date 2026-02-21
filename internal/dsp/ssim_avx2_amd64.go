//go:build amd64

package dsp

// AVX2 version of SSE 16x16 metric.

//go:noescape
func sse16x16AVX2(pix, ref []byte) int

// AVX2 dual-lane Hadamard distortion.

//go:noescape
func tDisto4x4AVX2(a, b []byte) int
