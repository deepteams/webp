//go:build amd64

package dsp

//go:noescape
func yuvPackedToNRGBABatchSSE2(y []byte, packedUV []uint32, dst []byte, width int)
