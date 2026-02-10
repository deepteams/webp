package dsp

// BPS is the common stride for encoder/decoder block processing.
// Matches the value in libwebp common_dec.h.
const BPS = 32

// Transform function variables for dispatch.
// These are set to pure-Go implementations by Init() and can be overridden
// by platform-specific SIMD implementations in the future.
var (
	// Encoder transforms.
	ITransform    func(ref []byte, in []int16, dst []byte, doTwo bool)
	FTransform    func(src, ref []byte, out []int16)
	FTransform2   func(src, ref []byte, out []int16)
	FTransformWHT func(in, out []int16)

	// Decoder transforms.
	Transform     func(coeffs []int16, dst []byte, doTwo bool)
	TransformAC3  func(coeffs []int16, dst []byte)
	TransformUV   func(coeffs []int16, dst []byte)
	TransformDC   func(coeffs []int16, dst []byte)
	TransformDCUV func(coeffs []int16, dst []byte)
	TransformWHT  func(in, out []int16)
)

// PredFunc is the signature for intra prediction functions.
// buf is the full reconstruction buffer and off is the offset of the block
// origin within buf. Reference pixels (top, left, top-left) are at offsets
// before off, so negative-offset access becomes buf[off-k] with k > 0.
type PredFunc func(buf []byte, off int)

// Prediction function tables, indexed by prediction mode.
var (
	PredLuma16  [7]PredFunc  // NumBDCModes entries for 16x16 luma
	PredChroma8 [7]PredFunc  // NumBDCModes entries for 8x8 chroma
	PredLuma4   [10]PredFunc // NumBModes entries for 4x4 luma
)

// Filter function types matching the loop-filter API.
type SimpleFilterFunc func(p []byte, stride, thresh int)
type LumaFilterFunc func(luma []byte, stride, thresh, ithresh, hevT int)
type ChromaFilterFunc func(u, v []byte, stride, thresh, ithresh, hevT int)

// DspScan is the scan table mapping 4x4 sub-block indices to byte offsets
// into the macroblock buffer. Matches C VP8DspScan[16+4+4] (enc.c:36-45).
// Layout: [16 luma] + [4 U chroma] + [4 V chroma], all as byte offsets.
var DspScan [16 + 4 + 4]int

// DspScanUV is the scan table for chroma sub-blocks (4 U + 4 V).
// Matches C VP8ScanUV[4+4] (quant_enc.c:481-484).
var DspScanUV [4 + 4]int

// initScanTable fills DspScan with byte offsets into the macroblock buffer.
// Matches C VP8Scan[16] (quant_enc.c:473-479) exactly.
// Each 4x4 sub-block is at offset (col*4 + row*4*BPS) within the 16x16 MB.
func initScanTable() {
	// 16 luma 4x4 sub-blocks: byte offsets into the MB buffer.
	// C: { 0+0*BPS, 4+0*BPS, 8+0*BPS, 12+0*BPS,
	//       0+4*BPS, 4+4*BPS, 8+4*BPS, 12+4*BPS,
	//       0+8*BPS, 4+8*BPS, 8+8*BPS, 12+8*BPS,
	//       0+12*BPS, 4+12*BPS, 8+12*BPS, 12+12*BPS }
	for row := 0; row < 4; row++ {
		for col := 0; col < 4; col++ {
			DspScan[row*4+col] = col*4 + row*4*BPS
		}
	}
	// UV byte offsets: 4 U blocks + 4 V blocks.
	// Matches C VP8DspScan[16..23] (enc.c:43-44) and VP8ScanUV (quant_enc.c:481-484).
	// C: { 0+0*BPS, 4+0*BPS, 0+4*BPS, 4+4*BPS,   // U
	//       8+0*BPS, 12+0*BPS, 8+4*BPS, 12+4*BPS } // V
	DspScan[16] = 0 + 0*BPS
	DspScan[17] = 4 + 0*BPS
	DspScan[18] = 0 + 4*BPS
	DspScan[19] = 4 + 4*BPS
	DspScan[20] = 8 + 0*BPS
	DspScan[21] = 12 + 0*BPS
	DspScan[22] = 8 + 4*BPS
	DspScan[23] = 12 + 4*BPS

	// DspScanUV mirrors DspScan[16..23] for callers that prefer a separate table.
	for i := 0; i < 8; i++ {
		DspScanUV[i] = DspScan[16+i]
	}
}

// Init initialises all function pointers to their pure-Go implementations.
// This must be called before any DSP functions are used.
func Init() {
	// Clip tables.
	initClipTables()

	// YUV tables.
	initYUVTables()

	// Cost tables.
	initLevelCosts()

	// Scan table.
	initScanTable()

	// Decoder transforms.
	Transform = transformTwo
	TransformAC3 = transformAC3
	TransformUV = transformUV
	TransformDC = transformDC
	TransformDCUV = transformDCUV
	TransformWHT = transformWHT

	// Encoder transforms.
	ITransform = iTransform
	FTransform = fTransform
	FTransform2 = fTransform2
	FTransformWHT = fTransformWHT

	// Prediction modes.
	initPredictors()

	// Lossless predictors.
	initLosslessPredictors()

	// SSIM metrics.
	initSSIM()
}

func init() {
	Init()
}
