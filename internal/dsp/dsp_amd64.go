//go:build amd64

package dsp

func init() {
	// Override pure-Go implementations with SSE2 assembly.
	// This init() runs after dsp.go's init() due to alphabetical ordering.

	// SSE metrics.
	SSE4x4 = sse4x4SSE2
	SSE16x16 = sse16x16SSE2

	// WHT transforms.
	FTransformWHT = fTransformWHTSSE2
	TransformWHT = transformWHTSSE2

	// 16x16 luma prediction modes.
	PredLuma16[0] = dc16SSE2
	PredLuma16[1] = tm16SSE2
	PredLuma16[2] = ve16SSE2
	PredLuma16[3] = he16SSE2

	// 8x8 chroma prediction modes.
	PredChroma8[0] = dc8uvSSE2
	PredChroma8[1] = tm8uvSSE2
	PredChroma8[2] = ve8uvSSE2
	PredChroma8[3] = he8uvSSE2

	// DCT transforms.
	FTransform = fTransformSSE2
	ITransform = iTransformSSE2
	Transform = transformTwoDecSSE2
	TransformUV = transformUVSSE2

	// Lossless color transforms.
	AddGreenToBlueAndRedFunc = addGreenToBlueAndRedSSE2
	SubtractGreenFunc = subtractGreenSSE2

	// Override with AVX2 where available.
	if hasAVX2 {
		SSE16x16 = sse16x16AVX2
		AddGreenToBlueAndRedFunc = addGreenToBlueAndRedAVX2
		SubtractGreenFunc = subtractGreenAVX2

		// DCT transforms (VEX-encoded, eliminates SSE/AVX transition penalties).
		FTransform = fTransformAVX2
		FTransform2 = fTransform2AVX2
		ITransform = iTransformAVX2
		Transform = transformTwoDecAVX2
		TransformUV = transformUVAVX2
	}
}

// --- SSE2 assembly function stubs ---

//go:noescape
func sse4x4SSE2(pix, ref []byte) int

//go:noescape
func sse16x16SSE2(pix, ref []byte) int

//go:noescape
func fTransformWHTSSE2(in []int16, out []int16)

//go:noescape
func transformWHTSSE2(in []int16, out []int16)

//go:noescape
func ve16asmSSE2(dst []byte, off int)

//go:noescape
func he16asmSSE2(dst []byte, off int)

//go:noescape
func dc16asmSSE2(dst []byte, off int)

//go:noescape
func tm16asmSSE2(dst []byte, off int)

//go:noescape
func ve8uvasmSSE2(dst []byte, off int)

//go:noescape
func he8uvasmSSE2(dst []byte, off int)

//go:noescape
func dc8uvasmSSE2(dst []byte, off int)

//go:noescape
func tm8uvasmSSE2(dst []byte, off int)

//go:noescape
func fTransformSSE2(src, ref []byte, out []int16)

//go:noescape
func iTransformOneSSE2(ref []byte, in []int16, dst []byte)

//go:noescape
func addGreenToBlueAndRedSSE2(argb []uint32, numPixels int)

//go:noescape
func subtractGreenSSE2(argb []uint32, numPixels int)

//go:noescape
func tDisto4x4SSE2(a, b []byte) int

// --- Go wrappers matching PredFunc signature ---

func dc16SSE2(dst []byte, off int)   { dc16asmSSE2(dst, off) }
func tm16SSE2(dst []byte, off int)   { tm16asmSSE2(dst, off) }
func ve16SSE2(dst []byte, off int)   { ve16asmSSE2(dst, off) }
func he16SSE2(dst []byte, off int)   { he16asmSSE2(dst, off) }
func dc8uvSSE2(dst []byte, off int)  { dc8uvasmSSE2(dst, off) }
func tm8uvSSE2(dst []byte, off int)  { tm8uvasmSSE2(dst, off) }
func ve8uvSSE2(dst []byte, off int)  { ve8uvasmSSE2(dst, off) }
func he8uvSSE2(dst []byte, off int)  { he8uvasmSSE2(dst, off) }

// iTransformSSE2 wraps the single-block SSE2 IDCT to handle doTwo.
func iTransformSSE2(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransformOneSSE2(ref, in, dst)
	if doTwo {
		iTransformOneSSE2(ref[4:], in[16:], dst[4:])
	}
}

// transformTwoDecSSE2 wraps the encoder SSE2 IDCT for decoder use.
// The decoder IDCT adds residuals to prediction already in dst, which is
// equivalent to iTransformOneSSE2(dst, in, dst) since the asm processes each
// row sequentially (read 4B → compute → write 4B) at stride 32, so each row's
// ref bytes are consumed before that row's dst bytes are written.
func transformTwoDecSSE2(in []int16, dst []byte, doTwo bool) {
	iTransformOneSSE2(dst, in, dst)
	if doTwo {
		iTransformOneSSE2(dst[4:], in[16:], dst[4:])
	}
}

// transformUVSSE2 applies SSE2 IDCT for all four chroma 4x4 blocks.
func transformUVSSE2(in []int16, dst []byte) {
	iTransformOneSSE2(dst, in, dst)
	iTransformOneSSE2(dst[4:], in[16:], dst[4:])
	iTransformOneSSE2(dst[4*BPS:], in[32:], dst[4*BPS:])
	iTransformOneSSE2(dst[4*BPS+4:], in[48:], dst[4*BPS+4:])
}
