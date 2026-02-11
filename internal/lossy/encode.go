package lossy

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"

	"github.com/deepteams/webp/internal/dsp"
)

// importUVWorker holds pre-allocated buffers for UV conversion goroutines.
type importUVWorker struct {
	rowR, rowG, rowB, rowA [2][]uint8
	planarR, planarG       []uint8
	planarB, planarA       []uint8
	tmpRGB                 []uint16
}


var importUVWorkerPool sync.Pool

func getImportUVWorker(padW, uvWidth int) *importUVWorker {
	if v := importUVWorkerPool.Get(); v != nil {
		wk := v.(*importUVWorker)
		if cap(wk.rowR[0]) >= padW && cap(wk.tmpRGB) >= uvWidth*4 {
			return wk
		}
	}
	wk := &importUVWorker{}
	for i := 0; i < 2; i++ {
		wk.rowR[i] = make([]uint8, padW)
		wk.rowG[i] = make([]uint8, padW)
		wk.rowB[i] = make([]uint8, padW)
		wk.rowA[i] = make([]uint8, padW)
	}
	wk.planarR = make([]uint8, padW*2)
	wk.planarG = make([]uint8, padW*2)
	wk.planarB = make([]uint8, padW*2)
	wk.planarA = make([]uint8, padW*2)
	wk.tmpRGB = make([]uint16, uvWidth*4)
	return wk
}

// EncodeConfig holds VP8 encoding parameters.
type EncodeConfig struct {
	Quality    int  // 0-100, maps to quantizer.
	TargetSize int  // Target byte size (0 = use quality).
	TargetPSNR float32 // Target PSNR (0 = disabled). Matches C libwebp's target_PSNR.
	Method     int  // 0-6, compression effort.
	SNSStrength int // 0-100, spatial noise shaping.
	FilterStrength int // 0-100, deblocking filter.
	FilterSharpness int // 0-7, filter sharpness.
	FilterType int  // 0=simple, 1=strong loop-filter.
	Partitions int  // 0-3 => 1, 2, 4, 8 partitions.
	Segments   int  // 1-4, number of segments.
	Pass       int  // 1-10, multi-pass encoding.
	Dithering  float32 // Dithering amplitude [0..1] for RGB->YUV conversion.
	QMin       int  // 0-100, minimum quantizer value. Matches C libwebp's qmin.
	QMax       int  // 0-100, maximum quantizer value. Matches C libwebp's qmax. -1 = use default (100).
}

// DefaultConfig returns sensible encoding defaults (quality 75, method 4).
func DefaultConfig(quality int) EncodeConfig {
	if quality < 0 {
		quality = 0
	}
	if quality > 100 {
		quality = 100
	}
	return EncodeConfig{
		Quality:         quality,
		Method:          4,
		SNSStrength:     50,
		FilterStrength:  60,
		FilterSharpness: 0,
		FilterType:      1,
		Partitions:      0,
		Segments:        4,
		Pass:            1,
		QMin:            0,
		QMax:            100,
	}
}

// VP8Encoder is the main VP8 lossy encoder.
type VP8Encoder struct {
	// Config.
	config EncodeConfig

	// Picture dimensions.
	width, height int
	mbW, mbH      int // dimensions in macroblock units

	// YUV planes (working copy, BPS stride for Y; 8-pixel stride for UV).
	yPlane, uPlane, vPlane []byte
	yStride, uvStride      int

	// Saved source planes for multi-pass encoding.
	savedY, savedU, savedV []byte

	// Reconstruction buffers (BPS-strided, for prediction/comparison).
	yuvIn  []byte // original (source) macroblock, BPS-strided
	yuvOut []byte // best reconstructed output, BPS-strided
	yuvOut2 []byte // secondary reconstruction buffer, BPS-strided
	yuvP   []byte // prediction buffer, BPS-strided (33*BPS for top+left context)

	// Per-macroblock info.
	mbInfo []MBEncInfo

	// Segment info.
	dqm  [NumMBSegments]SegmentInfo
	segmentHdr SegmentHeader

	// Probabilities.
	proba  Proba

	// Token buffers (partition 0 = modes, partition 1+ = coefficients).
	tokens TokenBuffer

	// Statistics from encoding loop.
	nzCounts [NumMBSegments][9]int
	stats    EncStats

	// Filter parameters.
	filterHdr FilterHeader

	// Iteration.
	mbIterator MBIterator

	// Number of partitions (1 << config.Partitions).
	numParts int

	// NZ context tracking for token encoding (mirrors decoder's parseResiduals).
	topNz   []uint32 // per-column top NZ bits
	leftNz  uint32   // left NZ bits for current row
	topNzDC []uint8  // per-column top DC NZ (for I16 mode)
	leftNzDC uint8   // left DC NZ for current row

	// Quantization deltas (applied to base q for each channel).
	// Y1 DC delta (always 0 in C libwebp; included for structural parity).
	dqY1DC int
	// Y2 (WHT/DC16) deltas (always 0 in C libwebp; included for structural parity).
	dqY2DC int
	dqY2AC int
	// UV channel deltas (computed from SNS strength / analysis).
	dqUVDC int // UV DC quantizer delta (typically negative for better chroma)
	dqUVAC int // UV AC quantizer delta

	// DC error diffusion buffers for UV channels (matching libwebp).
	// topDerr[mbX][ch][2] stores top-propagated errors; leftDerr[ch][2] stores left.
	topDerr  [][2][2]int8 // indexed by [mbX][channel(0=U,1=V)][position]
	leftDerr [2][2]int8   // indexed by [channel][position]
	useDerr  bool         // true if DC error diffusion is enabled

	// Global susceptibility (matching C enc->alpha, enc->uv_alpha).
	globalAlpha   int // average macroblock complexity (unused for now)
	globalUVAlpha int // average chroma complexity, for UV quantizer delta

	// Base quantizer for segment 0 (matching C enc->base_quant).
	baseQuant int

	// Effective number of segments after simplification (matching C segment_hdr.num_segments).
	numSegments int

	// Skip mode.
	skipProba uint8 // probability for skip flag (P(skip=0))
	numSkip   int   // number of skipped MBs

	// Partition0 size limit for I4 header bits (matching C enc->max_i4_header_bits).
	// Controls how many I4 decisions can be made before fallback to I16 to keep
	// partition0 within size limits. Set by initEncoder, halved when p0 overflows.
	maxI4HeaderBits int

	// Rate control state (for target-size encoding).
	rateCtrl *passStats

	// Pre-allocated temporary buffers for hot encode loops (avoid heap escapes).
	// These buffers are reused across macroblock iterations since encoding is
	// single-threaded. Eliminates ~80% of heap allocations in the encode path.
	tmpCoeffs   [16]int16     // forward DCT output
	tmpQCoeffs  [16]int16     // quantize output
	tmpDQCoeffs [16]int16     // dequantize output
	tmpDCCoeffs [16]int16     // DC coefficient accumulator for WHT
	tmpWHTDQ    [16]int16     // WHT dequantized coefficients
	tmpWHTBuf   [256]int16    // WHT inverse buffer
	tmpAllQ     [16][16]int16 // per-sub-block quantized coeffs (I16 mode eval)
	tmpACLevels [256]int16    // AC levels for flatness checks
	tmpRecon    [128]byte     // I4 4x4 reconstruction buffer (4*BPS)
	tmpUVLevels [128]int16    // UV levels for flatness check
	tmpBestDQ   [16]int16     // best I4 mode dequantized coeffs (saved from PickBestI4ModeRD)
	tmpBestQ    [16]int16     // best I4 mode quantized coeffs (saved from PickBestI4ModeRD)
	tmpBestNz   int           // best I4 mode NZ count (saved from PickBestI4ModeRD)

	// Analysis phase temp buffers (reused since analysis runs before encode).
	tmpAnSrc   [512]byte // luma source (16*BPS)
	tmpAnPred  [512]byte // luma prediction (16*BPS)
	tmpAnSrcU  [256]byte // chroma source U (8*BPS)
	tmpAnSrcV  [256]byte // chroma source V
	tmpAnPredU [256]byte // chroma prediction U
	tmpAnPredV [256]byte // chroma prediction V

	// Pre-allocated buffers for collectAllStats (avoid make per call).
	statTopNz   []uint32
	statTopNzDC []uint8

	// parallelRS: when set, recordAllTokens waits per-row for data availability.
	// Used for overlapping Phase A (parallel encoding) with Phase B (token recording).
	parallelRS *rowSync

	// skipTokens: when true, encodeFrame skips recordMBTokens but still tracks NZ context.
	// Used by statLoop to avoid recording tokens that will be discarded.
	skipTokens bool

	// skipExportPlanes: when true, Export skips writing reconstructed pixels back to
	// yPlane/uPlane/vPlane. Used by statLoop to avoid destroying source pixel data,
	// eliminating the need for saveSourcePixels/restoreSourcePixels.
	skipExportPlanes bool

	// Pre-allocated iterator context arrays (avoid per-InitIterator allocs).
	itTopY     []uint8
	itTopU     []uint8
	itTopV     []uint8
	itTopModes []uint8
	itTopNZ    []uint32

	// Pre-allocated analysis buffers (avoid per-analysis allocs).
	analysisAlphas []int
	segMapTmp      []uint8
}

// MBEncInfo stores per-macroblock encoding decisions.
type MBEncInfo struct {
	MBType   int // 0=i16, 1=i4
	UVMode   uint8
	Segment  uint8
	Alpha    int // complexity alpha from analysis (0-255), used for segment assignment
	Skip     bool
	// Intra prediction modes.
	I16Mode  uint8
	Modes    [16]uint8 // 4x4 modes when MBType==1
	// Quantized coefficients: 16 Y blocks (0-255) + 4 U (256-319) + 4 V (320-383) + 1 WHT DC (384-399).
	Coeffs   [400]int16
	// Non-zero flags.
	NonZeroY  uint32
	NonZeroUV uint32
	// Pre-computed nzCount (zigzag-order last+1) for each block.
	// Populated during encodeFrame; reused by collectAllStats to avoid redundant scans.
	// Layout: NzY[0..15]=Y blocks, NzUV[0..7]=UV blocks (U0-3 then V0-3), NzDC=WHT block.
	NzY  [16]uint8
	NzUV [8]uint8
	NzDC uint8
	// PredCached: when true, the winning I16/UV predictions have been generated
	// in yuvOut during pickBestMode. encodeI16Residuals and reconstructMB skip
	// regenerating them.
	PredCached bool
	// i4Cached: when true, I4 Y coefficients and NzY are already final
	// (trellis-quantized in PickBestI4ModeRDTrellis). encodeI4Residuals
	// can skip PredLuma4 + FTransform + TrellisQuantize per block.
	I4Cached bool
	// DC error diffusion results (matching libwebp rd->derr).
	Derr [2][3]int8 // [channel(U=0,V=1)][err1, err2, err3]
	// Distortion and rate.
	Disto uint64
	Rate  uint64
	Score uint64 // Rate + λ*Disto
}

// SegmentInfo holds quantization parameters for one segment.
type SegmentInfo struct {
	// Quant matrices for this segment.
	Y1  SegmentQuant // luma
	Y2  SegmentQuant // luma DC (secondary transform)
	UV  SegmentQuant // chroma
	// Lambda for RD scoring (mode selection).
	Lambda     int // general lambda (for backward compat)
	LambdaI4   int // I4x4 mode selection lambda
	LambdaI16  int // I16x16 mode selection lambda
	LambdaUV   int // UV mode selection lambda
	LambdaMode int // final I4 vs I16 decision lambda
	// Trellis lambdas (per coefficient type).
	TLambdaI4  int // trellis lambda for I4 blocks (derived from Y1 quantizer)
	TLambdaI16 int // trellis lambda for I16 AC blocks (derived from Y2 quantizer)
	TLambdaUV  int // trellis lambda for UV blocks (derived from UV quantizer)
	TLambda    int // legacy trellis lambda
	TLambdaSD  int // texture distortion lambda (for perceptual SD term)
	Lambda2    int // squared lambda
	MinDisto   int // minimum distortion
	// Global quantizer value.
	Quant      int
	FStrength  int // filter strength for this segment
	// I4 vs I16 penalty.
	I4Penalty  int // cost penalty for choosing I4 over I16 mode
	// For SNS.
	Alpha      int // segment susceptibility [-127..127], from SetSegmentAlphas
	Beta       int
	MaxEdge    int
}

// SegmentQuant holds quantizer/dequantizer values and bias for one component.
// DC and AC coefficients may have different quantizer values (matching VP8 spec).
// Quantization uses QFIX=17 fixed-point: level = (coeff * IQuant + Bias) >> 17.
type SegmentQuant struct {
	// AC quantizer (coefficients 1-15).
	Quant    int    // AC quantizer multiplier (dequantization factor)
	IQuant   int    // AC inverse quantizer multiplier (Q17 fixed-point)
	Bias     int    // AC rounding bias for QUANTDIV
	Zthresh  int    // AC zero threshold
	// DC quantizer (coefficient 0).
	DCQuant  int    // DC quantizer multiplier (dequantization factor)
	DCIQuant int    // DC inverse quantizer multiplier (Q17 fixed-point)
	DCBias   int    // DC rounding bias for QUANTDIV
	DCZthresh int   // DC zero threshold
	Sharpen  [16]int16 // sharpening values added to coefficients before quantization
}

// EncStats collects encoding statistics for multi-pass optimization.
type EncStats struct {
	PSNR      [5]float64 // per-channel + global
	CodedSize  int
	HeaderSize int
	Residuals  int
	probaSize  int // coefficient probability table size in bytes (internal)
}

// ProbaSize returns the size of the coefficient probability table in bytes.
func (s EncStats) ProbaSize() int { return s.probaSize }

// initEncoderParams sets up encoder parameters that depend on the config.
// Matching C libwebp's InitVP8Encoder (webp_enc.c:100-118).
func (enc *VP8Encoder) initEncoderParams() {
	// max_i4_header_bits: upper bound on I4 header bits to keep partition0
	// within size limits. limit = 100 - partition_limit (partition_limit=0 default).
	// Formula: 256 * 16 * 16 * (limit^2) / (100^2).
	// With limit=100 (default): 256*16*16 = 65536.
	limit := 100 // We don't expose partition_limit, use default
	enc.maxI4HeaderBits = 256 * 16 * 16 * (limit * limit) / (100 * 100)
}

// storeMaxDelta records the maximum delta between adjacent DC coefficients
// for detecting "blocky" macroblocks that need stronger deblocking.
// Matching C libwebp's StoreMaxDelta (quant_enc.c:955-964).
func storeMaxDelta(dqm *SegmentInfo, dcs [16]int16) {
	v0 := int(dcs[1])
	if v0 < 0 {
		v0 = -v0
	}
	v1 := int(dcs[2])
	if v1 < 0 {
		v1 = -v1
	}
	v2 := int(dcs[4])
	if v2 < 0 {
		v2 = -v2
	}
	maxV := v0
	if v1 > maxV {
		maxV = v1
	}
	if v2 > maxV {
		maxV = v2
	}
	if maxV > dqm.MaxEdge {
		dqm.MaxEdge = maxV
	}
}

var encoderPool sync.Pool

// ReleaseEncoder returns an encoder to the pool for reuse.
// Must be called after EncodeFrame when the encoder is no longer needed.
func ReleaseEncoder(enc *VP8Encoder) {
	if enc != nil {
		encoderPool.Put(enc)
	}
}

// resetForReuse clears mutable state so a pooled encoder can be reused.
// The caller must have verified that mbW and mbH match.
func (enc *VP8Encoder) resetForReuse(cfg EncodeConfig, width, height int) {
	enc.config = cfg
	enc.width = width
	enc.height = height
	// mbW, mbH unchanged (verified by caller).

	enc.numParts = 1 << uint(cfg.Partitions)
	if enc.numParts > MaxNumPartitions {
		enc.numParts = MaxNumPartitions
	}

	// Clear mutable encoding state.
	enc.leftNz = 0
	enc.leftNzDC = 0
	enc.leftDerr = [2][2]int8{}
	enc.useDerr = cfg.Method >= 3
	enc.dqY1DC = 0
	enc.dqY2DC = 0
	enc.dqY2AC = 0
	enc.dqUVDC = 0
	enc.dqUVAC = 0
	enc.globalAlpha = 0
	enc.globalUVAlpha = 0
	enc.baseQuant = 0
	enc.numSegments = 0
	enc.skipProba = 0
	enc.numSkip = 0
	enc.maxI4HeaderBits = 0
	enc.rateCtrl = nil
	enc.nzCounts = [NumMBSegments][9]int{}
	enc.stats = EncStats{}
	enc.filterHdr = FilterHeader{}
	enc.segmentHdr = SegmentHeader{}
	enc.skipTokens = false
	enc.skipExportPlanes = false
	enc.parallelRS = nil
	enc.savedY = nil
	enc.savedU = nil
	enc.savedV = nil
	enc.tmpBestNz = 0

	// Clear topDerr (already allocated).
	for i := range enc.topDerr {
		enc.topDerr[i] = [2][2]int8{}
	}
}

// NewEncoder creates and initializes a VP8 encoder from an NRGBA image.
func NewEncoder(img image.Image, cfg EncodeConfig) *VP8Encoder {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	mbW := (w + 15) >> 4
	mbH := (h + 15) >> 4

	// Try to reuse a pooled encoder with matching dimensions.
	if v := encoderPool.Get(); v != nil {
		enc := v.(*VP8Encoder)
		if enc.mbW == mbW && enc.mbH == mbH {
			enc.resetForReuse(cfg, w, h)
			enc.importImage(img)
			enc.initSegments()
			enc.initEncoderParams()
			ResetProba(&enc.proba)
			enc.tokens.Reset()
			return enc
		}
	}

	enc := &VP8Encoder{
		config: cfg,
		width:  w,
		height: h,
		mbW:    mbW,
		mbH:    mbH,
	}

	enc.numParts = 1 << uint(cfg.Partitions)
	if enc.numParts > MaxNumPartitions {
		enc.numParts = MaxNumPartitions
	}

	enc.allocateBuffers()
	enc.importImage(img)
	enc.initSegments()
	enc.initEncoderParams()
	ResetProba(&enc.proba)
	enc.tokens.Init(enc.mbW * enc.mbH)

	return enc
}

// NewEncoderFromYUV creates a VP8 encoder from pre-computed YCbCr 4:2:0 planes.
// This is used when SharpYUV conversion has already been performed externally,
// bypassing the standard importImage RGB-to-YUV pipeline.
// The yuv image must use YCbCrSubsampleRatio420.
func NewEncoderFromYUV(yuv *image.YCbCr, width, height int, cfg EncodeConfig) *VP8Encoder {
	mbW := (width + 15) >> 4
	mbH := (height + 15) >> 4

	// Try to reuse a pooled encoder with matching dimensions.
	if v := encoderPool.Get(); v != nil {
		enc := v.(*VP8Encoder)
		if enc.mbW == mbW && enc.mbH == mbH {
			enc.resetForReuse(cfg, width, height)
			enc.importYCbCr(yuv)
			enc.initSegments()
			enc.initEncoderParams()
			ResetProba(&enc.proba)
			enc.tokens.Reset()
			return enc
		}
	}

	enc := &VP8Encoder{
		config: cfg,
		width:  width,
		height: height,
		mbW:    mbW,
		mbH:    mbH,
	}

	enc.numParts = 1 << uint(cfg.Partitions)
	if enc.numParts > MaxNumPartitions {
		enc.numParts = MaxNumPartitions
	}

	enc.allocateBuffers()
	enc.importYCbCr(yuv)
	enc.initSegments()
	enc.initEncoderParams()
	ResetProba(&enc.proba)
	enc.tokens.Init(enc.mbW * enc.mbH)

	return enc
}

// importYCbCr copies pre-computed YCbCr 4:2:0 planes into the encoder's
// internal Y/U/V plane buffers, padding to macroblock boundaries by
// replicating edge pixels.
func (enc *VP8Encoder) importYCbCr(yuv *image.YCbCr) {
	w := enc.width
	h := enc.height
	padW := enc.mbW * 16
	padH := enc.mbH * 16

	// Copy Y plane.
	for y := 0; y < padH; y++ {
		srcY := y
		if srcY >= h {
			srcY = h - 1
		}
		for x := 0; x < padW; x++ {
			srcX := x
			if srcX >= w {
				srcX = w - 1
			}
			enc.yPlane[y*enc.yStride+x] = yuv.Y[srcY*yuv.YStride+srcX]
		}
	}

	// Copy U/V planes.
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	padUVW := (padW + 1) >> 1
	padUVH := (padH + 1) >> 1

	for y := 0; y < padUVH; y++ {
		srcY := y
		if srcY >= uvH {
			srcY = uvH - 1
		}
		for x := 0; x < padUVW; x++ {
			srcX := x
			if srcX >= uvW {
				srcX = uvW - 1
			}
			enc.uPlane[y*enc.uvStride+x] = yuv.Cb[srcY*yuv.CStride+srcX]
			enc.vPlane[y*enc.uvStride+x] = yuv.Cr[srcY*yuv.CStride+srcX]
		}
	}
}

// allocateBuffers pre-allocates all working memory.
// Consolidates multiple small allocations into larger slabs to reduce alloc count.
func (enc *VP8Encoder) allocateBuffers() {
	mbW := enc.mbW
	mbH := enc.mbH
	totalMB := mbW * mbH

	// YUV planes: single slab for Y + U + V.
	enc.yStride = mbW * 16
	enc.uvStride = mbW * 8
	ySize := enc.yStride * mbH * 16
	uSize := enc.uvStride * mbH * 8
	yuvSlab := make([]byte, ySize+uSize+uSize)
	enc.yPlane = yuvSlab[:ySize]
	enc.uPlane = yuvSlab[ySize : ySize+uSize]
	enc.vPlane = yuvSlab[ySize+uSize:]

	// BPS-strided working buffers: single slab for yuvIn + yuvOut + yuvOut2 + yuvP.
	yuvPSize := 33 * dsp.BPS
	workSlab := make([]byte, 3*YUVSize+yuvPSize)
	enc.yuvIn = workSlab[:YUVSize]
	enc.yuvOut = workSlab[YUVSize : 2*YUVSize]
	enc.yuvOut2 = workSlab[2*YUVSize : 3*YUVSize]
	enc.yuvP = workSlab[3*YUVSize:]

	// MB info.
	enc.mbInfo = make([]MBEncInfo, totalMB)

	// NZ context: single slab for topNz + statTopNz (uint32) and topNzDC + statTopNzDC (uint8).
	nzSlab := make([]uint32, mbW*2)
	enc.topNz = nzSlab[:mbW]
	enc.statTopNz = nzSlab[mbW:]
	nzDCSlab := make([]uint8, mbW*2)
	enc.topNzDC = nzDCSlab[:mbW]
	enc.statTopNzDC = nzDCSlab[mbW:]

	// Iterator context arrays (reused across InitIterator calls).
	enc.itTopY = make([]uint8, mbW*16)
	enc.itTopU = make([]uint8, mbW*8)
	enc.itTopV = make([]uint8, mbW*8)
	enc.itTopModes = make([]uint8, mbW*4)
	enc.itTopNZ = make([]uint32, mbW)

	// Analysis buffers (reused across analysis calls).
	enc.analysisAlphas = make([]int, totalMB)
	enc.segMapTmp = make([]uint8, totalMB)

	// DC error diffusion: topDerr always allocated (reused across pool resets);
	// useDerr controls whether it's active (method >= 3, matching libwebp).
	enc.topDerr = make([][2][2]int8, mbW)
	enc.useDerr = enc.config.Method >= 3

}

// importImage converts the input image to YUV420 and stores it in the
// encoder's Y/U/V planes.
// When the image has semi-transparent pixels, alpha-weighted gamma-correct
// chroma averaging is used to prevent color bleeding at transparency edges.
// This matches C libwebp's ImportYUVAFromRGBA pipeline (yuv.c:515-574).
//
// When enc.config.Dithering > 0, pseudo-random dithering is applied to the
// rounding values during RGB->YUV conversion, matching C libwebp's
// ImportYUVAFromRGBA dithered path (picture_csp_enc.c:202-250).
func (enc *VP8Encoder) importImage(img image.Image) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	padW := enc.mbW * 16
	padH := enc.mbH * 16

	// Detect whether the image has meaningful alpha.
	hasAlpha := imageHasAlpha(img)

	// Initialize dithering random generator if dithering is enabled.
	var rg *dsp.VP8Random
	if enc.config.Dithering > 0 {
		rg = &dsp.VP8Random{}
		dsp.InitRandom(rg, enc.config.Dithering)
	}

	uvWidth := (padW + 1) >> 1

	dsp.InitGammaTables()

	// Type-assert for direct pixel access (avoids interface boxing heap allocs).
	nrgba, isNRGBA := img.(*image.NRGBA)

	// extractRow fills the planar buffers for a given source row.
	extractRow := func(srcY int, rBuf, gBuf, bBuf, aBuf []uint8) {
		sy := srcY + bounds.Min.Y
		if sy >= bounds.Min.Y+h {
			sy = bounds.Min.Y + h - 1
		}
		if isNRGBA {
			rowOff := (sy-nrgba.Rect.Min.Y)*nrgba.Stride + (bounds.Min.X-nrgba.Rect.Min.X)*4
			for x := 0; x < padW; x++ {
				sx := x
				if sx >= w {
					sx = w - 1
				}
				off := rowOff + sx*4
				rBuf[x] = nrgba.Pix[off]
				gBuf[x] = nrgba.Pix[off+1]
				bBuf[x] = nrgba.Pix[off+2]
				aBuf[x] = nrgba.Pix[off+3]
			}
		} else {
			for x := 0; x < padW; x++ {
				sx := x + bounds.Min.X
				if sx >= bounds.Min.X+w {
					sx = bounds.Min.X + w - 1
				}
				c := color.NRGBAModel.Convert(img.At(sx, sy)).(color.NRGBA)
				rBuf[x] = c.R
				gBuf[x] = c.G
				bBuf[x] = c.B
				aBuf[x] = c.A
			}
		}
	}

	// Convert to Y plane (full resolution).
	// When dithering is active, use randomized rounding instead of fixed
	// YUV_HALF, matching C ConvertRowToY with VP8RandomBits(rg, YUV_FIX).
	if isNRGBA && rg == nil {
		// Fast parallel path for non-dithered NRGBA.
		nWorkers := runtime.GOMAXPROCS(0)
		if nWorkers > padH {
			nWorkers = padH
		}
		var ywg sync.WaitGroup
		for wi := 0; wi < nWorkers; wi++ {
			startY := wi * padH / nWorkers
			endY := (wi + 1) * padH / nWorkers
			ywg.Add(1)
			go func(startY, endY int) {
				defer ywg.Done()
				minY := bounds.Min.Y
				srcBase := (minY-nrgba.Rect.Min.Y)*nrgba.Stride + (bounds.Min.X-nrgba.Rect.Min.X)*4
				for y := startY; y < endY; y++ {
					sy := y
					if sy >= h {
						sy = h - 1
					}
					rowOff := srcBase + sy*nrgba.Stride
					dstBase := y * enc.yStride
					for x := 0; x < w; x++ {
						off := rowOff + x*4
						enc.yPlane[dstBase+x] = dsp.RGBToY(int(nrgba.Pix[off]), int(nrgba.Pix[off+1]), int(nrgba.Pix[off+2]))
					}
					// Edge replication for padding.
					if padW > w {
						val := enc.yPlane[dstBase+w-1]
						for x := w; x < padW; x++ {
							enc.yPlane[dstBase+x] = val
						}
					}
				}
			}(startY, endY)
		}
		ywg.Wait()
	} else if isNRGBA {
		for y := 0; y < padH; y++ {
			sy := y + bounds.Min.Y
			if sy >= bounds.Min.Y+h {
				sy = bounds.Min.Y + h - 1
			}
			rowOff := (sy-nrgba.Rect.Min.Y)*nrgba.Stride + (bounds.Min.X-nrgba.Rect.Min.X)*4
			for x := 0; x < padW; x++ {
				sx := x
				if sx >= w {
					sx = w - 1
				}
				off := rowOff + sx*4
				ri, gi, bi := int(nrgba.Pix[off]), int(nrgba.Pix[off+1]), int(nrgba.Pix[off+2])
				enc.yPlane[y*enc.yStride+x] = dsp.RGBToYRounding(ri, gi, bi, dsp.RandomBits(rg, dsp.YUVFix))
			}
		}
	} else {
		for y := 0; y < padH; y++ {
			sy := y + bounds.Min.Y
			if sy >= bounds.Min.Y+h {
				sy = bounds.Min.Y + h - 1
			}
			for x := 0; x < padW; x++ {
				sx := x + bounds.Min.X
				if sx >= bounds.Min.X+w {
					sx = bounds.Min.X + w - 1
				}
				c := color.NRGBAModel.Convert(img.At(sx, sy)).(color.NRGBA)
				ri, gi, bi := int(c.R), int(c.G), int(c.B)
				if rg != nil {
					enc.yPlane[y*enc.yStride+x] = dsp.RGBToYRounding(ri, gi, bi, dsp.RandomBits(rg, dsp.YUVFix))
				} else {
					enc.yPlane[y*enc.yStride+x] = dsp.RGBToY(ri, gi, bi)
				}
			}
		}
	}

	// Convert to U/V planes using gamma-corrected averaging with alpha weighting.
	// Process two rows at a time (matching C ImportYUVAFromRGBA_C).
	halfPadH := padH / 2

	if isNRGBA && rg == nil {
		// Fast parallel path for non-dithered NRGBA.
		nUVWorkers := runtime.GOMAXPROCS(0)
		if nUVWorkers > halfPadH {
			nUVWorkers = halfPadH
		}
		var uvwg sync.WaitGroup
		for wi := 0; wi < nUVWorkers; wi++ {
			startPair := wi * halfPadH / nUVWorkers
			endPair := (wi + 1) * halfPadH / nUVWorkers
			uvwg.Add(1)
			go func(startPair, endPair int) {
				defer uvwg.Done()
				// Get pooled or new worker buffers.
				wk := getImportUVWorker(padW, uvWidth)
				if !hasAlpha {
					for i := range wk.planarA {
						wk.planarA[i] = 0xff
					}
				}
				srcBase := (bounds.Min.Y-nrgba.Rect.Min.Y)*nrgba.Stride + (bounds.Min.X-nrgba.Rect.Min.X)*4

				for y := startPair; y < endPair; y++ {
					for row := 0; row < 2; row++ {
						srcY := y*2 + row
						sy := srcY
						if sy >= h {
							sy = h - 1
						}
						rowOff := srcBase + sy*nrgba.Stride
						rBuf := wk.rowR[row]
						gBuf := wk.rowG[row]
						bBuf := wk.rowB[row]
						aBuf := wk.rowA[row]
						for x := 0; x < w; x++ {
							off := rowOff + x*4
							rBuf[x] = nrgba.Pix[off]
							gBuf[x] = nrgba.Pix[off+1]
							bBuf[x] = nrgba.Pix[off+2]
							aBuf[x] = nrgba.Pix[off+3]
						}
						if padW > w {
							for x := w; x < padW; x++ {
								rBuf[x] = rBuf[w-1]
								gBuf[x] = gBuf[w-1]
								bBuf[x] = bBuf[w-1]
								aBuf[x] = aBuf[w-1]
							}
						}
					}
					copy(wk.planarR[:padW], wk.rowR[0])
					copy(wk.planarR[padW:], wk.rowR[1])
					copy(wk.planarG[:padW], wk.rowG[0])
					copy(wk.planarG[padW:], wk.rowG[1])
					copy(wk.planarB[:padW], wk.rowB[0])
					copy(wk.planarB[padW:], wk.rowB[1])
					if hasAlpha {
						copy(wk.planarA[:padW], wk.rowA[0])
						copy(wk.planarA[padW:], wk.rowA[1])
					}
					dsp.AccumulateRGBA(wk.planarR, wk.planarG, wk.planarB, wk.planarA, padW, wk.tmpRGB, padW)
					dsp.ConvertRGBA32ToUV(wk.tmpRGB, enc.uPlane[y*enc.uvStride:], enc.vPlane[y*enc.uvStride:], uvWidth)
				}
				importUVWorkerPool.Put(wk)
			}(startPair, endPair)
		}
		uvwg.Wait()
	} else {
		// Serial path for dithered or non-NRGBA images.
		// Allocate row buffers only when actually needed (fast NRGBA path above skips this).
		rowR := [2][]uint8{make([]uint8, padW), make([]uint8, padW)}
		rowG := [2][]uint8{make([]uint8, padW), make([]uint8, padW)}
		rowB := [2][]uint8{make([]uint8, padW), make([]uint8, padW)}
		rowA := [2][]uint8{make([]uint8, padW), make([]uint8, padW)}
		tmpRGB := make([]uint16, uvWidth*4)
		planarR := make([]uint8, padW*2)
		planarG := make([]uint8, padW*2)
		planarB := make([]uint8, padW*2)
		planarA := make([]uint8, padW*2)
		if !hasAlpha {
			for i := range planarA {
				planarA[i] = 0xff
			}
		}

		for y := 0; y < halfPadH; y++ {
			extractRow(y*2, rowR[0], rowG[0], rowB[0], rowA[0])
			extractRow(y*2+1, rowR[1], rowG[1], rowB[1], rowA[1])
			copy(planarR[:padW], rowR[0])
			copy(planarR[padW:], rowR[1])
			copy(planarG[:padW], rowG[0])
			copy(planarG[padW:], rowG[1])
			copy(planarB[:padW], rowB[0])
			copy(planarB[padW:], rowB[1])
			if hasAlpha {
				copy(planarA[:padW], rowA[0])
				copy(planarA[padW:], rowA[1])
			}
			dsp.AccumulateRGBA(planarR, planarG, planarB, planarA, padW, tmpRGB, padW)
			if rg != nil {
				dsp.ConvertRGBA32ToUVDithered(tmpRGB, enc.uPlane[y*enc.uvStride:], enc.vPlane[y*enc.uvStride:], uvWidth, rg)
			} else {
				dsp.ConvertRGBA32ToUV(tmpRGB, enc.uPlane[y*enc.uvStride:], enc.vPlane[y*enc.uvStride:], uvWidth)
			}
		}
	}
}

// imageHasAlpha reports whether any pixel in the image has a non-opaque alpha value.
func imageHasAlpha(img image.Image) bool {
	// Fast path: direct pixel access for *image.NRGBA.
	// Uses AND-reduction: if all alpha bytes are 0xff, the AND of all is 0xff.
	if nrgba, ok := img.(*image.NRGBA); ok {
		bounds := nrgba.Bounds()
		w := bounds.Dx()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			rowOff := (y-nrgba.Rect.Min.Y)*nrgba.Stride + (bounds.Min.X-nrgba.Rect.Min.X)*4
			// AND-reduce alpha bytes in groups of 4 for fewer branches.
			acc := uint8(0xff)
			x := 0
			for ; x+4 <= w; x += 4 {
				acc &= nrgba.Pix[rowOff+3]
				acc &= nrgba.Pix[rowOff+7]
				acc &= nrgba.Pix[rowOff+11]
				acc &= nrgba.Pix[rowOff+15]
				rowOff += 16
			}
			for ; x < w; x++ {
				acc &= nrgba.Pix[rowOff+3]
				rowOff += 4
			}
			if acc != 0xff {
				return true
			}
		}
		return false
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xffff {
				return true
			}
		}
	}
	return false
}

// initSegments sets up initial per-segment quantization parameters.
// This provides reasonable defaults before analysis() runs and computes
// the proper SNS-modulated per-segment quantizers.
func (enc *VP8Encoder) initSegments() {
	q := qualityToQIndex(enc.config.Quality)
	numSegs := enc.config.Segments
	if numSegs < 1 {
		numSegs = 1
	}
	if numSegs > NumMBSegments {
		numSegs = NumMBSegments
	}
	enc.numSegments = numSegs

	// Initialize UV deltas to zero; setSegmentParams (called from analysis())
	// will compute the proper values from globalUVAlpha and SNS strength.
	enc.dqUVDC = 0
	enc.dqUVAC = 0

	// Set all segments to the base quantizer initially.
	// analysis() -> setSegmentParams() will recompute with proper SNS modulation.
	for i := 0; i < NumMBSegments; i++ {
		setupSegment(enc, i, q)
	}
}

// qualityToCompression maps quality [0..100] to a compression factor [0..1],
// matching C libwebp's QualityToCompression exactly. The compression factor
// represents how much the image can be compressed: higher = less compression
// (better quality). Uses piecewise-linear mapping + cube root.
func qualityToCompression(quality int) float64 {
	if quality <= 0 {
		return 0.0
	}
	if quality >= 100 {
		return 1.0
	}
	c := float64(quality) / 100.0
	// Piecewise linear: maps [0,0.75] → [0,0.5] and [0.75,1] → [0.5,1]
	var linearC float64
	if c < 0.75 {
		linearC = c * (2.0 / 3.0)
	} else {
		linearC = 2.0*c - 1.0
	}
	return math.Pow(linearC, 1.0/3.0)
}

// qualityToQIndex maps quality [0..100] to a VP8 quantizer index [0..127].
// Uses qualityToCompression (matching C libwebp's QualityToCompression) and
// converts the float compression factor to an integer quantizer index.
func qualityToQIndex(quality int) int {
	v := qualityToCompression(quality)
	return clampInt(int(127.0*(1.0-v)), 0, 127)
}

// kBiasMatrices holds per-type quantization bias values [type][is_ac].
// type 0 = Y1 (luma), type 1 = Y2 (WHT), type 2 = UV (chroma).
// BIAS(b) = b << (QFIX - 8) = b << 9 with QFIX=17.
var kBiasMatrices = [3][2]int{
	{96, 110},  // Y1: DC=96, AC=110
	{96, 108},  // Y2: DC=96, AC=108
	{110, 115}, // UV: DC=110, AC=115
}

// kFreqSharpening holds per-frequency sharpening factors for SNS.
// Indexed by raster position. From libwebp quant_enc.c.
var kFreqSharpening = [16]int{
	0, 30, 60, 90, 30, 60, 90, 90,
	60, 90, 90, 90, 90, 90, 90, 90,
}

// setupSegment configures quantization for a single segment.
// The DC and AC quantizers must exactly match what the decoder computes
// in ParseQuant (decode_quant.go) with all delta offsets = 0.
func setupSegment(enc *VP8Encoder, idx, q int) {
	seg := &enc.dqm[idx]
	seg.Quant = q

	// Y1 (luma): DC = KDcTable[clip(q+dqY1DC)], AC = KAcTable[q].
	// C: m->y1.q[0] = kDcTable[clip(q + enc->dq_y1_dc, 0, 127)]
	y1dc := int(KDcTable[clampInt(q+enc.dqY1DC, 0, 127)])
	y1ac := int(KAcTable[clampInt(q, 0, 127)])
	initSegmentQuant(&seg.Y1, y1dc, y1ac, 0) // type 0 = Y1

	// Y2 (luma DC secondary / WHT):
	// C: m->y2.q[0] = kDcTable[clip(q + enc->dq_y2_dc, 0, 127)] * 2
	// C: m->y2.q[1] = kAcTable2[clip(q + enc->dq_y2_ac, 0, 127)]
	y2dc := int(KDcTable[clampInt(q+enc.dqY2DC, 0, 127)]) * 2
	if y2dc < 8 {
		y2dc = 8
	}
	y2ac := int(KAcTable2[clampInt(q+enc.dqY2AC, 0, 127)])
	initSegmentQuant(&seg.Y2, y2dc, y2ac, 1) // type 1 = Y2

	// UV (chroma): DC = KDcTable[clip(q+dqUVDC, 117)], AC = KAcTable[clip(q+dqUVAC, 127)].
	// The UV deltas give chroma better quality than luma, matching libwebp's keyframe behavior.
	uvdc := int(KDcTable[clampInt(q+enc.dqUVDC, 0, 117)])
	uvac := int(KAcTable[clampInt(q+enc.dqUVAC, 0, 127)])
	initSegmentQuant(&seg.UV, uvdc, uvac, 2) // type 2 = UV

	// Lambda for RD (rate-distortion) scoring.
	// In libwebp, lambdas are derived from the average dequant factor:
	//   q_i4 = average of Y1 quantizer values ≈ (DC + 15*AC) / 16
	//   q_i16 = average of Y2 quantizer values
	//   q_uv = average of UV quantizer values
	qI4 := (y1dc + 15*y1ac + 8) >> 4
	qI16 := (y2dc + 15*y2ac + 8) >> 4
	qUV := (uvdc + 15*uvac + 8) >> 4

	seg.LambdaI4 = maxInt((3*qI4*qI4)>>7, 1)
	seg.LambdaI16 = maxInt(3*qI16*qI16, 1)
	seg.LambdaUV = maxInt((3*qUV*qUV)>>6, 1)
	seg.LambdaMode = maxInt((1*qI4*qI4)>>7, 1)
	seg.Lambda = seg.LambdaI4 // backward compat
	seg.Lambda2 = seg.Lambda * seg.Lambda

	// Trellis lambdas (from libwebp quant_enc.c).
	seg.TLambdaI4 = maxInt((7*qI4*qI4)>>3, 1)
	seg.TLambdaI16 = maxInt((qI16*qI16)>>2, 1)
	seg.TLambdaUV = maxInt((qUV*qUV)<<1, 1)
	seg.TLambda = seg.TLambdaI4 // legacy

	// Texture distortion lambda (for perceptual SD term in mode selection).
	// In libwebp: tlambda = (tlambda_scale * q_i4) >> 5
	// tlambda_scale = sns_strength (0-100) for method >= 4, else 0.
	if enc.config.Method >= 4 && enc.config.SNSStrength > 0 {
		seg.TLambdaSD = (enc.config.SNSStrength * qI4) >> 5
	}

	// MinDisto: quantization-aware minimum distortion threshold (matching libwebp).
	// m->min_disto = 20 * m->y1.q[0]
	seg.MinDisto = 20 * seg.Y1.DCQuant

	// MaxEdge: reset to zero (will be filled during encoding).
	seg.MaxEdge = 0

	// I4Penalty: cost penalty for choosing I4 over I16 mode (matching libwebp).
	// m->i4_penalty = 1000 * q_i4 * q_i4
	seg.I4Penalty = 1000 * qI4 * qI4

	// Populate sharpening bias for Y1 only (matching libwebp: type==0 only).
	// Formula: sharpen[i] = (kFreqSharpening[i] * q[i]) >> SHARPEN_BITS
	// where SHARPEN_BITS = 11.
	for i := 0; i < 16; i++ {
		q := seg.Y1.Quant
		if i == 0 {
			q = seg.Y1.DCQuant
		}
		seg.Y1.Sharpen[i] = int16((kFreqSharpening[i] * q) >> 11)
	}
	// Y2 and UV have no sharpening (already zero-initialized).
}

// initSegmentQuant initializes a SegmentQuant with QFIX=17 fixed-point
// quantizer parameters matching libwebp's ExpandMatrix.
// biasType: 0=Y1, 1=Y2, 2=UV.
func initSegmentQuant(sq *SegmentQuant, dcQuant, acQuant, biasType int) {
	const qfix = 17
	// DC
	sq.DCQuant = dcQuant
	sq.DCIQuant = (1 << qfix) / dcQuant
	sq.DCBias = kBiasMatrices[biasType][0] << (qfix - 8) // BIAS(dc_value)
	sq.DCZthresh = ((1 << qfix) - 1 - sq.DCBias) / sq.DCIQuant
	// AC
	sq.Quant = acQuant
	sq.IQuant = (1 << qfix) / acQuant
	sq.Bias = kBiasMatrices[biasType][1] << (qfix - 8) // BIAS(ac_value)
	sq.Zthresh = ((1 << qfix) - 1 - sq.Bias) / sq.IQuant
}

// computeLambda derives the RD lambda from quantizer index.
// This is an approximation; the actual per-type lambdas are set in setupSegment.
func computeLambda(q int) int {
	if q < 1 {
		return 1
	}
	return q >> 2
}

// maxInt returns the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// kLevelsFromDelta is the lookup table mapping (sharpness, delta) to filter
// strength levels, matching libwebp's kLevelsFromDelta in filter_enc.c.
// Dimensions: [8 sharpness levels][64 delta values].
var kLevelsFromDelta = [8][64]uint8{
	// Sharpness 0
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31,
		32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47,
		48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63},
	// Sharpness 1
	{0, 1, 2, 3, 5, 6, 7, 8, 9, 11, 12, 13, 14, 15, 17, 18,
		20, 21, 23, 24, 26, 27, 29, 30, 32, 33, 35, 36, 38, 39, 41, 42,
		44, 45, 47, 48, 50, 51, 53, 54, 56, 57, 59, 60, 62, 63, 63, 63,
		63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63},
	// Sharpness 2
	{0, 1, 2, 3, 5, 6, 7, 8, 9, 11, 12, 13, 14, 16, 17, 19,
		20, 22, 23, 25, 26, 28, 29, 31, 32, 34, 35, 37, 38, 40, 41, 43,
		44, 46, 47, 49, 50, 52, 53, 55, 56, 58, 59, 61, 62, 63, 63, 63,
		63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63},
	// Sharpness 3
	{0, 1, 2, 3, 5, 6, 7, 8, 9, 11, 12, 13, 15, 16, 18, 19,
		21, 22, 24, 25, 27, 28, 30, 31, 33, 34, 36, 37, 39, 40, 42, 43,
		45, 46, 48, 49, 51, 52, 54, 55, 57, 58, 60, 61, 63, 63, 63, 63,
		63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63},
	// Sharpness 4
	{0, 1, 2, 3, 5, 6, 7, 8, 9, 11, 12, 14, 15, 17, 18, 20,
		21, 23, 24, 26, 27, 29, 30, 32, 33, 35, 36, 38, 39, 41, 42, 44,
		45, 47, 48, 50, 51, 53, 54, 56, 57, 59, 60, 62, 63, 63, 63, 63,
		63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63},
	// Sharpness 5
	{0, 1, 2, 4, 5, 7, 8, 9, 11, 12, 13, 15, 16, 17, 19, 20,
		22, 23, 25, 26, 28, 29, 31, 32, 34, 35, 37, 38, 40, 41, 43, 44,
		46, 47, 49, 50, 52, 53, 55, 56, 58, 59, 61, 62, 63, 63, 63, 63,
		63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63},
	// Sharpness 6
	{0, 1, 2, 4, 5, 7, 8, 9, 11, 12, 13, 15, 16, 18, 19, 21,
		22, 24, 25, 27, 28, 30, 31, 33, 34, 36, 37, 39, 40, 42, 43, 45,
		46, 48, 49, 51, 52, 54, 55, 57, 58, 60, 61, 63, 63, 63, 63, 63,
		63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63},
	// Sharpness 7
	{0, 1, 2, 4, 5, 7, 8, 9, 11, 12, 14, 15, 17, 18, 20, 21,
		23, 24, 26, 27, 29, 30, 32, 33, 35, 36, 38, 39, 41, 42, 44, 45,
		47, 48, 50, 51, 53, 54, 56, 57, 59, 60, 62, 63, 63, 63, 63, 63,
		63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63},
}

// filterStrengthFromDelta returns the filter strength for a given sharpness
// and delta, matching libwebp's VP8FilterStrengthFromDelta.
func filterStrengthFromDelta(sharpness, delta int) int {
	pos := delta
	if pos >= 64 {
		pos = 63
	}
	if pos < 0 {
		pos = 0
	}
	return int(kLevelsFromDelta[sharpness][pos])
}

// fstrengthCutoff is the minimum filter strength; values below this are zeroed.
const fstrengthCutoff = 2

// setupFilterStrength computes per-segment filter strength using the
// kLevelsFromDelta lookup table and beta weighting, matching libwebp's
// SetupFilterStrength in quant_enc.c.
func (enc *VP8Encoder) setupFilterStrength() {
	fhdr := &enc.filterHdr
	fhdr.Simple = (enc.config.FilterType == 0)
	fhdr.Sharpness = clampInt(enc.config.FilterSharpness, 0, 7)
	// Matching C libwebp: ref_lf_delta is always zero (never used by the encoder).
	// use_lf_delta is only set when i4x4_lf_delta != 0 (mode delta for I4 blocks).
	// Since we don't currently set any mode deltas, use_lf_delta = false.
	fhdr.RefLFDelta = [4]int{0, 0, 0, 0}
	fhdr.ModeLFDelta = [4]int{0, 0, 0, 0}
	fhdr.UseLFDelta = false

	if enc.config.FilterStrength <= 0 {
		fhdr.Level = 0
		return
	}

	// level0 is in [0..500]. Using '-f 50' as filter_strength is mid-filtering.
	level0 := 5 * enc.config.FilterStrength
	numSegs := enc.config.Segments
	if numSegs < 1 {
		numSegs = 1
	}
	if numSegs > NumMBSegments {
		numSegs = NumMBSegments
	}

	for i := 0; i < numSegs; i++ {
		m := &enc.dqm[i]
		// Focus on the quantization of AC coefficients.
		qstep := int(KAcTable[clampInt(m.Quant, 0, 127)]) >> 2
		baseStrength := filterStrengthFromDelta(fhdr.Sharpness, qstep)
		// Segments with lower complexity ('beta') will be less filtered.
		f := baseStrength * level0 / (256 + m.Beta)
		if f < fstrengthCutoff {
			f = 0
		}
		if f > 63 {
			f = 63
		}
		m.FStrength = f
	}

	// Record the initial strength (mainly for 1-segment case).
	fhdr.Level = enc.dqm[0].FStrength
}

// EncodeFrame is the main entry point: encodes the image and returns the
// VP8 bitstream (without RIFF container).
func (enc *VP8Encoder) EncodeFrame() ([]byte, error) {
	// Analysis pass: assign segments and choose global parameters.
	enc.analysis()
	enc.setSegmentProbas()

	// For the token-buffer path (method >= 3), C libwebp uses VP8EncTokenLoop
	// which does NOT call StatLoop — instead, it relies on mid-stream probability
	// refresh during the encode pass (FinalizeTokenProbas every ~1/8 of MBs).
	// Our encodeFrame already does this via refreshProbas().
	// StatLoop is only used by C's VP8EncLoop (non-token path, method < 3).
	if enc.config.Method < 3 {
		enc.statLoop()
	}

	// Determine if we need multi-pass search (matching C libwebp's do_search).
	doSearch := enc.config.TargetSize > 0 || enc.config.TargetPSNR > 0

	// Save source pixels before main loop if rate control is needed.
	if doSearch {
		enc.saveSourcePixels()
	}

	// Main encoding loop (may iterate for rate control).
	maxPasses := maxInt(enc.config.Pass, 1)
	if doSearch && maxPasses < 3 {
		maxPasses = 3 // ensure enough passes for rate control convergence
	}
	// Use parallel encoding when:
	// - Multiple CPU cores available (GOMAXPROCS > 1)
	// - Enough rows for meaningful parallelism (mbH >= 4)
	// - Method >= 3 (RD-based mode selection, which is the hot path)
	// - Single-pass quality mode (no rate control iteration)
	useParallel := runtime.GOMAXPROCS(0) > 1 && enc.mbH >= 4 && enc.config.Method >= 3 && !doSearch

	var stats ProbaStats
	for pass := 0; pass < maxPasses; pass++ {
		enc.tokens.Reset()
		if useParallel {
			enc.encodeFrameParallel(&stats)
		} else {
			enc.encodeFrame()
		}

		if !doSearch {
			break // quality mode: single pass
		}
		// Rate control: check if we hit the target.
		if enc.adjustQuantForTarget() {
			break
		}
	}

	// Final probability optimization: collect statistics from the coefficient
	// data, compute optimal probability tables, and re-record tokens.
	if !useParallel {
		// Serial path: collect stats separately (not merged into encodeFrame).
		enc.collectAllStats(&stats)
	}
	if optimizeProba(&stats, &enc.proba) > 0 {
		// Re-record tokens with optimized probabilities.
		enc.rerecordAllTokens()
	}

	// Emit the VP8 bitstream.
	frameData, err := enc.emitFrame()
	if err != nil {
		return nil, err
	}
	enc.computeStats(frameData)
	return frameData, nil
}

// statLoop performs preliminary stats-collection passes to optimize coefficient
// probabilities before the actual encoding loop. This matches C libwebp's
// StatLoop() in frame_enc.c.
//
// The key insight: during RD-optimized mode selection, the encoder uses
// probability tables to estimate token costs. Starting with default probabilities
// leads to suboptimal mode decisions. By doing a preliminary encode pass and
// updating probabilities from the collected statistics, the real encode pass
// makes better mode choices.
func (enc *VP8Encoder) statLoop() {
	numPasses := enc.config.Pass
	if numPasses < 1 {
		numPasses = 1
	}
	// Cap stats passes: C libwebp does up to config->pass iterations, but
	// for most cases 1-2 passes suffice. Match C behavior.
	if numPasses > 10 {
		numPasses = 10
	}

	for pass := 0; pass < numPasses; pass++ {
		// Run a full encode pass to fill mbInfo with mode decisions and coefficients.
		// Skip token recording and plane write-back to preserve source pixel data.
		enc.skipTokens = true
		enc.skipExportPlanes = true
		enc.encodeFrame()
		enc.skipTokens = false
		enc.skipExportPlanes = false

		// Collect statistics from the encoded coefficients.
		var stats ProbaStats
		enc.collectAllStats(&stats)

		// Update probabilities from statistics (matching C's FinalizeTokenProbas).
		changed := optimizeProba(&stats, &enc.proba)

		// If probabilities didn't change, further passes won't help.
		if changed == 0 {
			break
		}
	}
}

// passStats holds convergence state for multi-pass rate control,
// matching C libwebp's PassStats structure.
// Supports convergence on either target size or target PSNR,
// controlled by the doSizeSearch flag.
type passStats struct {
	isFirst                bool
	dq                     float64
	q, lastQ               float64
	qmin, qmax             float64
	value, lastValue       float64 // PSNR or size (depending on doSizeSearch)
	target                 float64 // target size or PSNR
	doSizeSearch           bool    // true=converge on size, false=converge on PSNR
}

// initPassStats initializes rate control state, matching C libwebp's
// InitPassStats. Supports both target-size and target-PSNR convergence.
// Returns true if a size-based search is active (doSizeSearch).
func (enc *VP8Encoder) initPassStats() *passStats {
	doSizeSearch := enc.config.TargetSize > 0
	targetPSNR := float64(enc.config.TargetPSNR)

	// Determine the convergence target (matching C InitPassStats).
	var target float64
	if doSizeSearch {
		target = float64(enc.config.TargetSize)
	} else if targetPSNR > 0 {
		target = targetPSNR
	} else {
		target = 40.0 // default, just in case (matching C)
	}

	// Clamp quality to [qmin, qmax] range (matching C behavior).
	qmin := float64(enc.config.QMin)
	qmax := float64(enc.config.QMax)
	if qmax <= 0 {
		qmax = 100.0
	}
	q := float64(enc.config.Quality)
	if q < qmin {
		q = qmin
	}
	if q > qmax {
		q = qmax
	}

	return &passStats{
		isFirst:      true,
		dq:           10.0,
		q:            q,
		lastQ:        q,
		qmin:         qmin,
		qmax:         qmax,
		target:       target,
		doSizeSearch: doSizeSearch,
	}
}

// getPSNR computes PSNR from mean squared error and pixel count,
// matching C libwebp's GetPSNR in frame_enc.c.
func getPSNR(mse uint64, size uint64) float64 {
	if mse > 0 && size > 0 {
		return 10.0 * math.Log10(255.0*255.0*float64(size)/float64(mse))
	}
	return 99.0
}

// computeNextQ computes the next quality value using secant-method interpolation,
// matching C libwebp's ComputeNextQ.
func (s *passStats) computeNextQ() float64 {
	var dq float64
	if s.isFirst {
		if s.value > s.target {
			dq = -s.dq
		} else {
			dq = s.dq
		}
		s.isFirst = false
	} else if s.value != s.lastValue {
		slope := (s.target - s.value) / (s.lastValue - s.value)
		dq = slope * (s.lastQ - s.q)
	} else {
		dq = 0
	}
	// Limit variable to avoid large swings.
	if dq < -30 {
		dq = -30
	}
	if dq > 30 {
		dq = 30
	}
	s.dq = dq
	s.lastQ = s.q
	s.lastValue = s.value
	s.q = s.q + dq
	if s.q < s.qmin {
		s.q = s.qmin
	}
	if s.q > s.qmax {
		s.q = s.qmax
	}
	return s.q
}

// adjustQuantForTarget adjusts quantizers if a target size or PSNR is specified.
// Returns true if the current value is close enough or convergence is reached.
// Matches C libwebp's rate control logic in VP8EncTokenLoop / StatLoop.
func (enc *VP8Encoder) adjustQuantForTarget() bool {
	// Lazy init of rate control state.
	if enc.rateCtrl == nil {
		enc.rateCtrl = enc.initPassStats()
	}

	if enc.rateCtrl.doSizeSearch {
		// Size-based convergence: emit a trial frame and measure byte size.
		trialData, err := enc.emitFrame()
		if err != nil {
			return true // give up on error
		}
		enc.rateCtrl.value = float64(len(trialData))
	} else {
		// PSNR-based convergence: compute PSNR from accumulated distortion.
		// The pixel count is 384 per macroblock (16*16 Y + 8*8 U + 8*8 V),
		// matching C libwebp's pixel_count = mb_w * mb_h * 384.
		pixelCount := uint64(enc.mbW * enc.mbH * 384)
		// Accumulate distortion from all macroblocks.
		var totalDisto uint64
		for i := range enc.mbInfo {
			totalDisto += enc.mbInfo[i].Disto
		}
		enc.rateCtrl.value = getPSNR(totalDisto, pixelCount)
	}

	// Check convergence: DQ_LIMIT = 0.4 (matching C libwebp).
	if math.Abs(enc.rateCtrl.dq) <= 0.4 && !enc.rateCtrl.isFirst {
		return true
	}

	// Compute next quality.
	nextQ := enc.rateCtrl.computeNextQ()

	// Apply the new quality: recompute segment quantizers using proper SNS modulation.
	// The per-segment alpha/beta values were already computed by analysis() and are
	// preserved, so setSegmentParams will re-modulate using the new quality value.
	enc.config.Quality = int(nextQ + 0.5)
	enc.setSegmentParams(enc.numSegments)
	enc.buildSegmentHeader(enc.numSegments)

	// Restore source pixels and re-encode with new quantizers.
	enc.restoreSourcePixels()

	return false // not converged, caller should re-encode
}

// saveSourcePixels saves the source YUV planes before encoding overwrites them.
func (enc *VP8Encoder) saveSourcePixels() {
	if enc.savedY == nil {
		enc.savedY = make([]byte, len(enc.yPlane))
		enc.savedU = make([]byte, len(enc.uPlane))
		enc.savedV = make([]byte, len(enc.vPlane))
	}
	copy(enc.savedY, enc.yPlane)
	copy(enc.savedU, enc.uPlane)
	copy(enc.savedV, enc.vPlane)
}

// restoreSourcePixels restores the saved source YUV planes for re-encoding.
func (enc *VP8Encoder) restoreSourcePixels() {
	if enc.savedY != nil {
		copy(enc.yPlane, enc.savedY)
		copy(enc.uPlane, enc.savedU)
		copy(enc.vPlane, enc.savedV)
	}
}

// computeStats computes encoding statistics after a frame has been encoded.
func (enc *VP8Encoder) computeStats(frameData []byte) {
	enc.stats.CodedSize = len(frameData)
}

// Stats returns the current encoding statistics.
func (enc *VP8Encoder) Stats() EncStats {
	return enc.stats
}

// SegmentQuant returns the quantizer value for a segment.
func (enc *VP8Encoder) SegmentQuant(seg int) int {
	return enc.dqm[seg].Quant
}
