//go:build amd64

package dsp

// AVX2 (VEX-encoded) versions of forward/inverse DCT 4x4 transforms.

//go:noescape
func fTransformAVX2(src, ref []byte, out []int16)

//go:noescape
func iTransformOneAVX2(ref []byte, in []int16, dst []byte)

// fTransform2AVX2 applies fTransformAVX2 to two side-by-side 4x4 blocks.
func fTransform2AVX2(src, ref []byte, out []int16) {
	fTransformAVX2(src, ref, out)
	fTransformAVX2(src[4:], ref[4:], out[16:])
}

// iTransformAVX2 wraps the single-block AVX2 IDCT to handle doTwo.
func iTransformAVX2(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransformOneAVX2(ref, in, dst)
	if doTwo {
		iTransformOneAVX2(ref[4:], in[16:], dst[4:])
	}
}

// transformTwoDecAVX2 wraps the AVX2 IDCT for decoder use.
func transformTwoDecAVX2(in []int16, dst []byte, doTwo bool) {
	iTransformOneAVX2(dst, in, dst)
	if doTwo {
		iTransformOneAVX2(dst[4:], in[16:], dst[4:])
	}
}

// transformUVAVX2 applies AVX2 IDCT for all four chroma 4x4 blocks.
func transformUVAVX2(in []int16, dst []byte) {
	iTransformOneAVX2(dst, in, dst)
	iTransformOneAVX2(dst[4:], in[16:], dst[4:])
	iTransformOneAVX2(dst[4*BPS:], in[32:], dst[4*BPS:])
	iTransformOneAVX2(dst[4*BPS+4:], in[48:], dst[4*BPS+4:])
}
