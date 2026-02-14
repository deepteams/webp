//go:build arm64

package dsp

func init() {
	// Override pure-Go implementations with NEON assembly.
	// This init() runs after dsp.go's init() due to alphabetical ordering.

	// SSE metrics: keep Go versions (scalar assembly is slower than
	// Go's optimized scalar; needs real NEON SIMD to beat the compiler).

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

	// Lossless color transforms.
	AddGreenToBlueAndRedFunc = addGreenToBlueAndRedNEON
	SubtractGreenFunc = subtractGreenNEON
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
