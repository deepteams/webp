//go:build amd64

package dsp

// AVX2 versions of lossless green channel transforms.

//go:noescape
func addGreenToBlueAndRedAVX2(argb []uint32, numPixels int)

//go:noescape
func subtractGreenAVX2(argb []uint32, numPixels int)
