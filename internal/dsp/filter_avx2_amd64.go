//go:build amd64

package dsp

// AVX2 version of simple vertical loop filter.

//go:noescape
func simpleVFilter16AVX2(p []byte, base, stride, thresh int)
