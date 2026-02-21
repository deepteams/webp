//go:build amd64

package dsp

// AVX2 version of YUV→NRGBA batch conversion.

//go:noescape
func yuvPackedToNRGBABatchAVX2(y []byte, packedUV []uint32, dst []byte, width int)
