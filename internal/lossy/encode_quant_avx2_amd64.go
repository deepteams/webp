//go:build amd64

package lossy

// AVX2 version of AC quantization.

//go:noescape
func quantizeACAVX2(in, out, sharpen []int16, iQuant, bias int)
