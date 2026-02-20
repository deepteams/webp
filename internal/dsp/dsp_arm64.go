//go:build arm64

package dsp

func init() {
	// Override pure-Go implementations with NEON assembly.
	// This init() runs after dsp.go's init() due to alphabetical ordering.

	// SSE metrics (NEON SIMD).
	SSE4x4 = sse4x4NEON
	SSE16x16 = sse16x16NEON

	// WHT transforms.
	FTransformWHT = fTransformWHTNEON
	TransformWHT = transformWHTNEON

	// 16x16 luma prediction modes.
	PredLuma16[0] = dc16NEON
	PredLuma16[1] = tm16NEON
	PredLuma16[2] = ve16NEON
	PredLuma16[3] = he16NEON

	// 8x8 chroma prediction modes.
	PredChroma8[0] = dc8uvNEON
	PredChroma8[1] = tm8uvNEON
	PredChroma8[2] = ve8uvNEON
	PredChroma8[3] = he8uvNEON

	// DCT transforms.
	ITransform = iTransformNEON
	Transform = transformTwoDecNEON
	// FTransform: NEON is slower than Go for 4x4 blocks on M2 Pro (16.2ns vs 13.3ns,
	// benchmarked 2026-02-15). Due to strided byte packing overhead (INS chain).
	// Keep pure Go — the compiler generates excellent scalar code.

	// Lossless color transforms.
	AddGreenToBlueAndRedFunc = addGreenToBlueAndRedNEON
	SubtractGreenFunc = subtractGreenNEON
}

// iTransformNEON wraps the single-block NEON IDCT to handle doTwo.
func iTransformNEON(ref []byte, in []int16, dst []byte, doTwo bool) {
	iTransformOneNEON(ref, in, dst)
	if doTwo {
		iTransformOneNEON(ref[4:], in[16:], dst[4:])
	}
}

// transformTwoDecNEON wraps the encoder NEON IDCT for decoder use.
// The decoder IDCT adds residuals to prediction already in dst, which is
// equivalent to iTransformOneNEON(dst, in, dst) since the asm reads all
// ref bytes before writing any dst bytes.
func transformTwoDecNEON(in []int16, dst []byte, doTwo bool) {
	iTransformOneNEON(dst, in, dst)
	if doTwo {
		iTransformOneNEON(dst[4:], in[16:], dst[4:])
	}
}

// --- NEON assembly function stubs ---

//go:noescape
func sse4x4NEON(pix, ref []byte) int

//go:noescape
func sse16x16NEON(pix, ref []byte) int

//go:noescape
func fTransformWHTNEON(in []int16, out []int16)

//go:noescape
func transformWHTNEON(in []int16, out []int16)

//go:noescape
func ve16asmNEON(dst []byte, off int)

//go:noescape
func he16asmNEON(dst []byte, off int)

//go:noescape
func dc16asmNEON(dst []byte, off int)

//go:noescape
func tm16asmNEON(dst []byte, off int)

//go:noescape
func ve8uvasmNEON(dst []byte, off int)

//go:noescape
func he8uvasmNEON(dst []byte, off int)

//go:noescape
func dc8uvasmNEON(dst []byte, off int)

//go:noescape
func tm8uvasmNEON(dst []byte, off int)

//go:noescape
func addGreenToBlueAndRedNEON(argb []uint32, numPixels int)

//go:noescape
func subtractGreenNEON(argb []uint32, numPixels int)

// --- Go wrappers matching PredFunc signature ---

func dc16NEON(dst []byte, off int)   { dc16asmNEON(dst, off) }
func tm16NEON(dst []byte, off int)   { tm16asmNEON(dst, off) }
func ve16NEON(dst []byte, off int)   { ve16asmNEON(dst, off) }
func he16NEON(dst []byte, off int)   { he16asmNEON(dst, off) }
func dc8uvNEON(dst []byte, off int)  { dc8uvasmNEON(dst, off) }
func tm8uvNEON(dst []byte, off int)  { tm8uvasmNEON(dst, off) }
func ve8uvNEON(dst []byte, off int)  { ve8uvasmNEON(dst, off) }
func he8uvNEON(dst []byte, off int)  { he8uvasmNEON(dst, off) }

//go:noescape
func iTransformOneNEON(ref []byte, in []int16, dst []byte)

//go:noescape
func fTransformNEON(src, ref []byte, out []int16)

// FTransformNEON is exported for benchmarking the NEON forward DCT.
func FTransformNEON(src, ref []byte, out []int16) {
	fTransformNEON(src, ref, out)
}
