//go:build amd64

package dsp

//go:noescape
func simpleVFilter16SSE2(p []byte, base, stride, thresh int)
