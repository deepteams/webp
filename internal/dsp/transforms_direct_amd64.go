//go:build amd64

package dsp

// FTransformDirect computes forward DCT using AVX2 when available, SSE2 otherwise.
func FTransformDirect(src, ref []byte, out []int16) {
	if hasAVX2 {
		fTransformAVX2(src, ref, out)
		return
	}
	fTransformSSE2(src, ref, out)
}

// ITransformDirect computes inverse DCT using AVX2 when available, SSE2 otherwise.
func ITransformDirect(ref []byte, in []int16, dst []byte, doTwo bool) {
	if hasAVX2 {
		iTransformOneAVX2(ref, in, dst)
		if doTwo {
			iTransformOneAVX2(ref[4:], in[16:], dst[4:])
		}
		return
	}
	iTransformOneSSE2(ref, in, dst)
	if doTwo {
		iTransformOneSSE2(ref[4:], in[16:], dst[4:])
	}
}
