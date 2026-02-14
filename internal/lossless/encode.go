package lossless

import (
	"errors"
	"sort"
	"sync"

	"github.com/deepteams/webp/internal/bitio"
)

// losslessEncoderPool reuses Encoder structs across successive lossless
// encode calls. On reuse (same or smaller image), all internal scratch
// buffers (hashChain, backwardRefs, distArray, huffmanScratch) are retained,
// eliminating most allocations after the first call.
var losslessEncoderPool = sync.Pool{
	New: func() any { return &Encoder{} },
}

// acquireEncoder returns an Encoder from the pool, resetting it for a new encode.
func acquireEncoder(width, height int, config *EncoderConfig) *Encoder {
	enc := losslessEncoderPool.Get().(*Encoder)
	enc.config = config
	enc.width = width
	enc.height = height
	enc.currentWidth = width
	enc.argbOrig = nil
	enc.transforms = enc.transforms[:0]
	enc.usePalette = false
	enc.paletteSize = 0
	enc.palette = nil
	enc.predictorBits = 0
	enc.crossColorBits = 0
	enc.histogramBits = 0
	enc.cacheBits = 0
	enc.useSubtractGreen = false
	enc.usePredict = false
	enc.useCrossColor = false
	return enc
}

// releaseEncoder returns an Encoder to the pool for reuse.
func releaseEncoder(enc *Encoder) {
	// Clear references to image data so it can be GC'd.
	enc.argb = nil
	enc.argbOrig = nil
	enc.config = nil
	enc.palette = nil
	enc.transforms = enc.transforms[:0]
	losslessEncoderPool.Put(enc)
}

// VP8L lossless encoder entry point.
//
// Encodes an ARGB pixel buffer into a VP8L bitstream. The encoder
// applies forward transforms (predictor, cross-color, subtract-green,
// color-indexing) then generates backward references, clusters histograms,
// builds Huffman codes, and emits the final compressed bitstream.
//
// Reference: libwebp/src/enc/vp8l_enc.c

// EncoderConfig holds configuration for the VP8L lossless encoder.
type EncoderConfig struct {
	// Quality controls encoding effort (0 = fast, 100 = best compression).
	Quality int
	// Method controls encoding method (0 = fast, 6 = best).
	Method int
	// NearLosslessQuality is the near-lossless quality (100 = true lossless).
	NearLosslessQuality int
}

// DefaultEncoderConfig returns a default encoder configuration.
func DefaultEncoderConfig() *EncoderConfig {
	return &EncoderConfig{
		Quality:             75,
		Method:              4,
		NearLosslessQuality: 100,
	}
}

// Encoder holds the state for VP8L lossless encoding.
type Encoder struct {
	config *EncoderConfig
	width  int
	height int

	// ARGB pixel data (may be transformed in place).
	argb    []uint32
	argbOrig []uint32 // original copy for multi-pass

	// Transform state.
	transforms    []Transform
	currentWidth  int

	// Palette.
	usePalette  bool
	paletteSize int
	palette     []uint32

	// Transform parameters.
	predictorBits   int
	crossColorBits  int
	histogramBits   int
	cacheBits       int

	// Flags.
	useSubtractGreen bool
	usePredict       bool
	useCrossColor    bool

	// Reusable scratch buffers (reduce allocations across encodes).
	hashChain      *HashChain       // reusable hash chain
	bestRefs       *BackwardRefs    // reusable backward refs (best)
	candidateRefs  *BackwardRefs    // reusable backward refs (candidate)
	traceRefs      *BackwardRefs    // reusable backward refs (trace result)
	traceDistArray []uint16         // reusable dist array for TraceBackwards
	huffScratch    *HuffmanScratch  // reusable Huffman tree scratch buffers
	brScratch      BackwardRefsScratch // reusable backward refs scratch

	// Minor reusable buffers.
	sortedPalette  []uint32
	deltaPalette   []uint32
	histoImageBuf  []uint32
	subImageHisto  *Histogram
	huffCodes      [][HuffmanCodesPerMetaCode]*HuffmanTreeCode

	// Reusable histogram slabs for GetHistoImageSymbols.
	histoScratch HistoScratch

	// Reusable residuals buffer for ResidualImage.
	residualsBuf []uint32
}

// Errors.
var (
	ErrImageTooLarge = errors.New("lossless: image dimensions too large")
	ErrEncoding      = errors.New("lossless: encoding failed")
)

// maxTransformBits is the maximum bits for predictor/cross-color tile size.
// MAX_TRANSFORM_BITS = MIN_TRANSFORM_BITS + (1 << NUM_TRANSFORM_BITS) - 1 = 9
const maxTransformBits = MinTransformBits + (1 << NumTransformBits) - 1

// maxHuffImageSize is the maximum histogram image size before increasing bits.
// Matches C reference MAX_HUFF_IMAGE_SIZE.
const maxHuffImageSize = 2600

// Encode encodes the ARGB pixel data as a VP8L bitstream and returns the
// raw encoded bytes (without RIFF/WebP container framing).
func Encode(argb []uint32, width, height int, config *EncoderConfig) ([]byte, error) {
	if width <= 0 || height <= 0 || width > 16383 || height > 16383 {
		return nil, ErrImageTooLarge
	}
	if config == nil {
		config = DefaultEncoderConfig()
	}

	enc := acquireEncoder(width, height, config)
	defer releaseEncoder(enc)

	// Copy the pixel data since transforms modify it in place.
	// Reuse the argb buffer if it has sufficient capacity.
	pixelCount := len(argb)
	if cap(enc.argb) >= pixelCount {
		enc.argb = enc.argb[:pixelCount]
	} else {
		enc.argb = make([]uint32, pixelCount)
	}
	copy(enc.argb, argb)

	// Analyze image.
	enc.analyze()

	// Apply near-lossless preprocessing with per-tile best predictor selection.
	if config.NearLosslessQuality < 100 {
		ApplyNearLossless(enc.argb, width, height, enc.predictorBits, config.NearLosslessQuality)
	}

	// Apply transforms.
	enc.applyTransforms()

	// Encode the image.
	return enc.encodeStream()
}

// analyze determines which transforms to use and sets encoding parameters.
func (enc *Encoder) analyze() {
	quality := enc.config.Quality
	method := enc.config.Method
	width := enc.width
	height := enc.height

	// Try palette mode.
	palette, paletteSize, ok := ColorIndexBuild(enc.argb, width, height)
	if ok && paletteSize <= MaxPaletteSize {
		enc.usePalette = true
		enc.paletteSize = paletteSize
		enc.palette = palette
	}

	// Determine transform parameters based on quality/method.
	// The C reference supports kPaletteAndSpatial: when a palette is in use
	// and method >= 5 with quality >= 75, also apply the predictor transform
	// on the palette-indexed image for better compression. Cross-color and
	// subtract-green are never combined with palette.
	if !enc.usePalette {
		enc.useSubtractGreen = quality >= 25
		enc.usePredict = quality >= 10
		enc.useCrossColor = quality >= 50
	} else if method >= 5 && quality >= 75 {
		// kPaletteAndSpatial: combine palette + predictor transform.
		enc.usePredict = true
	}

	// Empirical bit sizes matching the C reference EncoderAnalyze:
	// 1. Compute histogram bits from method and image size.
	// 2. Derive transform bits from method, capped by histogram bits.
	enc.histogramBits = getHistoBits(method, enc.usePalette, width, height)
	transformBits := getTransformBits(method, enc.histogramBits)
	enc.predictorBits = transformBits
	enc.crossColorBits = transformBits

	// Color cache bits: this sets the maximum search range for
	// CalculateBestCacheSize which brute-force picks the optimal value.
	enc.cacheBits = cacheBitsForEncoder(quality, enc.usePalette, enc.paletteSize)
}

// clampBits clamps bits to [minBits, maxBits], increases bits if the
// sub-sampled image exceeds imageSizeMax, and decreases bits if the
// sub-sampled image size collapses to 1.
// Matches the C reference ClampBits in vp8l_enc.c.
func clampBits(width, height, bits, minBits, maxBits, imageSizeMax int) int {
	if bits < minBits {
		bits = minBits
	} else if bits > maxBits {
		bits = maxBits
	}
	imageSize := VP8LSubSampleSize(width, bits) * VP8LSubSampleSize(height, bits)
	for bits < maxBits && imageSize > imageSizeMax {
		bits++
		imageSize = VP8LSubSampleSize(width, bits) * VP8LSubSampleSize(height, bits)
	}
	// In case bits reduce the image too much, decrease until image_size > 1.
	for bits > minBits && imageSize == 1 {
		imageSize = VP8LSubSampleSize(width, bits-1) * VP8LSubSampleSize(height, bits-1)
		if imageSize != 1 {
			break
		}
		bits--
	}
	return bits
}

// getHistoBits computes histogram sub-sampling bits from the encoding method,
// palette usage, and image dimensions. Matches the C reference GetHistoBits.
func getHistoBits(method int, usePalette bool, width, height int) int {
	// Make tile size a function of encoding method (Range: 0 to 6).
	histoBits := 7 - method
	if usePalette {
		histoBits = 9 - method
	}
	return clampBits(width, height, histoBits, MinHuffmanBits, maxHuffmanBits, maxHuffImageSize)
}

// getTransformBits computes predictor/cross-color transform bits from the
// encoding method and histogram bits. Matches the C reference GetTransformBits.
func getTransformBits(method, histoBits int) int {
	var maxBits int
	switch {
	case method < 4:
		maxBits = 6
	case method > 4:
		maxBits = 4
	default: // method == 4
		maxBits = 5
	}
	if histoBits > maxBits {
		return maxBits
	}
	return histoBits
}

// maxColorCacheBitsEnc is the maximum color cache bits the encoder will search
// over. This matches the C reference MAX_COLOR_CACHE_BITS (10) in
// backward_references_enc.h. The decoder constant MaxCacheBits (11) is larger
// because the spec allows it, but the encoder never produces > 10.
const maxColorCacheBitsEnc = 10

// cacheBitsForEncoder returns the maximum color cache bits to search over,
// matching the C reference behavior in vp8l_enc.c.
//
// For quality <= 25 the color cache is disabled (returns 0). Otherwise the
// full brute-force search range 0..maxColorCacheBitsEnc is used, because
// CalculateBestCacheSize will find the optimal value within that range.
//
// When a palette is in use and its size is small enough, the search range
// is capped to ceil(log2(paletteSize)) so the cache is never larger than
// the number of distinct colors. This matches the C reference:
//   cache_bits_init = (*cache_bits == 0) ? MAX_COLOR_CACHE_BITS : *cache_bits;
// where cache_bits is set from BitsLog2Floor(palette_size)+1 for palette images.
func cacheBitsForEncoder(quality int, usePalette bool, paletteSize int) int {
	if quality <= 25 {
		return 0
	}
	if usePalette && paletteSize < (1<<maxColorCacheBitsEnc) {
		// Don't let the cache be bigger than the number of palette entries.
		return bitsLog2Floor(paletteSize) + 1
	}
	return maxColorCacheBitsEnc
}

// applyTransforms applies forward transforms to enc.argb.
func (enc *Encoder) applyTransforms() {
	if enc.usePalette {
		enc.applyPaletteTransform()
		return
	}

	if enc.useSubtractGreen {
		SubtractGreen(enc.argb)
		enc.transforms = append(enc.transforms, Transform{
			Type: SubtractGreenTransform,
		})
	}

	if enc.usePredict {
		data, residuals := ResidualImage(enc.argb, enc.width, enc.height,
			enc.predictorBits, enc.config.Quality, enc.residualsBuf)
		enc.residualsBuf = residuals
		enc.argb = residuals
		enc.transforms = append(enc.transforms, Transform{
			Type:  PredictorTransform,
			Bits:  enc.predictorBits,
			XSize: enc.width,
			YSize: enc.height,
			Data:  data,
		})
	}

	if enc.useCrossColor {
		data := ColorSpaceTransform(enc.argb, enc.width, enc.height,
			enc.crossColorBits, enc.config.Quality)
		enc.transforms = append(enc.transforms, Transform{
			Type:  CrossColorTransform,
			Bits:  enc.crossColorBits,
			XSize: enc.width,
			YSize: enc.height,
			Data:  data,
		})
	}
}

// applyPaletteTransform applies the color indexing transform, and optionally
// the predictor transform on the palette-indexed data (kPaletteAndSpatial).
func (enc *Encoder) applyPaletteTransform() {
	// Sort palette for better compression.
	if cap(enc.sortedPalette) >= enc.paletteSize {
		enc.sortedPalette = enc.sortedPalette[:enc.paletteSize]
	} else {
		enc.sortedPalette = make([]uint32, enc.paletteSize)
	}
	sortedPalette := enc.sortedPalette
	copy(sortedPalette, enc.palette[:enc.paletteSize])
	sort.Slice(sortedPalette, func(i, j int) bool {
		return sortedPalette[i] < sortedPalette[j]
	})

	packed, packedWidth := ApplyPaletteTransform(enc.argb, enc.width, enc.height, sortedPalette)
	enc.argb = packed
	enc.currentWidth = packedWidth

	enc.transforms = append(enc.transforms, Transform{
		Type:  ColorIndexingTransform,
		Bits:  paletteCodeBits(enc.paletteSize),
		XSize: enc.currentWidth,
		YSize: enc.height,
		Data:  sortedPalette,
	})

	// kPaletteAndSpatial: apply predictor transform on the palette-indexed
	// image. This can improve compression when palette indices have spatial
	// correlation. The predictor operates on the packed width, matching the
	// C reference which passes enc->current_width to ApplyPredictFilter.
	if enc.usePredict {
		data, residuals := ResidualImage(enc.argb, enc.currentWidth, enc.height,
			enc.predictorBits, enc.config.Quality, enc.residualsBuf)
		enc.residualsBuf = residuals
		enc.argb = residuals
		enc.transforms = append(enc.transforms, Transform{
			Type:  PredictorTransform,
			Bits:  enc.predictorBits,
			XSize: enc.currentWidth,
			YSize: enc.height,
			Data:  data,
		})
	}
}

// paletteCodeBits returns the number of bits needed for palette indices.
func paletteCodeBits(paletteSize int) int {
	if paletteSize <= 2 {
		return 3 // 1 bit per pixel, but encoded as transform bits = 3
	}
	if paletteSize <= 4 {
		return 2 // 2 bits per pixel
	}
	if paletteSize <= 16 {
		return 1 // 4 bits per pixel
	}
	return 0 // 8 bits per pixel (no packing)
}

// encodeStream encodes the transformed image to a VP8L bitstream.
func (enc *Encoder) encodeStream() ([]byte, error) {
	width := enc.width
	height := enc.height
	quality := enc.config.Quality
	currentWidth := enc.currentWidth

	// Estimated output size.
	estimatedSize := width*height*4/2 + 1024
	bw := bitio.NewLosslessWriter(estimatedSize)

	// Write VP8L header.
	// Signature byte.
	bw.WriteBits(VP8LMagicByte, 8)
	// Width - 1 (14 bits).
	bw.WriteBits(uint32(width-1), VP8LImageSizeBits)
	// Height - 1 (14 bits).
	bw.WriteBits(uint32(height-1), VP8LImageSizeBits)
	// Alpha is used (1 bit).
	bw.WriteBits(1, 1)
	// Version (3 bits).
	bw.WriteBits(VP8LVersion, VP8LVersionBits)

	// Write transforms in forward application order, matching libwebp's
	// vp8l_enc.c which writes each transform as it is applied. The decoder
	// reads transforms in this order and applies their inverses in reverse.
	for i := 0; i < len(enc.transforms); i++ {
		t := &enc.transforms[i]
		bw.WriteBits(TransformPresent, 1)
		bw.WriteBits(uint32(t.Type), 2)
		enc.writeTransformData(bw, t)
	}
	// No more transforms.
	bw.WriteBits(0, 1)

	// Reset tree slab allocator for this encoding pass.
	if enc.huffScratch == nil {
		enc.huffScratch = &HuffmanScratch{}
	}
	enc.huffScratch.ResetTreePool()

	// Build hash chain (reuse if capacity is sufficient).
	pixelCount := currentWidth * height
	hc := enc.hashChain
	if hc == nil || hc.size < pixelCount {
		hc = NewHashChain(pixelCount)
		enc.hashChain = hc
	} else {
		for i := 0; i < pixelCount; i++ {
			hc.OffsetLength[i] = 0
		}
	}
	hc.Fill(enc.argb, quality, currentWidth, height, quality < 25)

	// Get backward references (reuse buffers if available).
	if enc.bestRefs == nil {
		enc.bestRefs = NewBackwardRefs(pixelCount)
	} else {
		enc.bestRefs.Reset()
	}
	refs := enc.bestRefs
	lz77Types := kLZ77Standard | kLZ77RLE | kLZ77Box

	// Prepare scratch buffers for GetBackwardReferences.
	if enc.candidateRefs == nil {
		enc.candidateRefs = NewBackwardRefs(pixelCount)
	}
	if enc.traceRefs == nil {
		enc.traceRefs = NewBackwardRefs(pixelCount)
	}
	if len(enc.traceDistArray) < pixelCount {
		enc.traceDistArray = make([]uint16, pixelCount)
	}
	enc.brScratch.Candidate = enc.candidateRefs
	enc.brScratch.Trace = enc.traceRefs
	enc.brScratch.DistArray = enc.traceDistArray
	cacheBits := GetBackwardReferencesWithScratch(currentWidth, height, enc.argb,
		quality, lz77Types, enc.cacheBits, hc, refs, &enc.brScratch)

	// Build histograms and get symbols.
	symbols, histoSet := GetHistoImageSymbols(
		currentWidth, height, refs, quality, enc.histogramBits, cacheBits,
		&enc.histoScratch)

	// Build Huffman codes for each histogram.
	numHistos := histoSet.Size()
	if cap(enc.huffCodes) >= numHistos {
		enc.huffCodes = enc.huffCodes[:numHistos]
	} else {
		enc.huffCodes = make([][HuffmanCodesPerMetaCode]*HuffmanTreeCode, numHistos)
	}
	huffCodes := enc.huffCodes
	for i := 0; i < numHistos; i++ {
		h := histoSet.Get(i)
		huffCodes[i][0] = CreateHuffmanTreeScratch(h.Literal, MaxAllowedCodeLength, enc.huffScratch)
		huffCodes[i][1] = CreateHuffmanTreeScratch(h.Red[:], MaxAllowedCodeLength, enc.huffScratch)
		huffCodes[i][2] = CreateHuffmanTreeScratch(h.Blue[:], MaxAllowedCodeLength, enc.huffScratch)
		huffCodes[i][3] = CreateHuffmanTreeScratch(h.Alpha[:], MaxAllowedCodeLength, enc.huffScratch)
		huffCodes[i][4] = CreateHuffmanTreeScratch(h.Distance[:], MaxAllowedCodeLength, enc.huffScratch)
	}

	// Write color cache parameters.
	if cacheBits > 0 {
		bw.WriteBits(1, 1) // use_color_cache
		bw.WriteBits(uint32(cacheBits), 4)
	} else {
		bw.WriteBits(0, 1)
	}

	// Write histogram image if multiple histograms.
	histoBits := enc.histogramBits
	if numHistos > 1 {
		// Build the histogram image (symbols packed into green channel),
		// matching the C reference which stores symbol << 8.
		txSize := VP8LSubSampleSize(currentWidth, histoBits)
		tySize := VP8LSubSampleSize(height, histoBits)
		histoImageSize := txSize * tySize
		if cap(enc.histoImageBuf) >= histoImageSize {
			enc.histoImageBuf = enc.histoImageBuf[:histoImageSize]
			for i := range enc.histoImageBuf {
				enc.histoImageBuf[i] = 0
			}
		} else {
			enc.histoImageBuf = make([]uint32, histoImageSize)
		}
		histoImage := enc.histoImageBuf
		for i, s := range symbols {
			if i < histoImageSize {
				histoImage[i] = uint32(s) << 8
			}
		}

		// Optimize sampling: try coarser tiling if histogram image is uniform.
		optimizedBits := optimizeSampling(histoImage, currentWidth, height,
			histoBits, maxHuffmanBits)

		bw.WriteBits(1, 1) // use_meta_huffman
		bw.WriteBits(uint32(optimizedBits-MinHuffmanBits), NumHuffmanBits)

		// Encode the (possibly subsampled) histogram image.
		newTxSize := VP8LSubSampleSize(currentWidth, optimizedBits)
		newTySize := VP8LSubSampleSize(height, optimizedBits)
		enc.encodeSubImage(bw, histoImage[:newTxSize*newTySize], newTxSize, newTySize)

		// Rebuild symbols from the subsampled histogram image for storeImageData.
		if optimizedBits != histoBits {
			newSymSize := newTxSize * newTySize
			symbols = make([]uint16, newSymSize)
			for i := 0; i < newSymSize; i++ {
				symbols[i] = uint16(histoImage[i] >> 8)
			}
			histoBits = optimizedBits
		}
	} else {
		bw.WriteBits(0, 1) // single histogram
	}

	// Write Huffman codes.
	for i := 0; i < numHistos; i++ {
		for j := 0; j < HuffmanCodesPerMetaCode; j++ {
			StoreHuffmanCodeScratch(bw, huffCodes[i][j], enc.huffScratch)
			clearHuffmanTreeIfOnlyOneSymbol(huffCodes[i][j])
		}
	}

	// Write image data using backward refs + Huffman codes.
	enc.storeImageData(bw, refs, symbols, huffCodes, currentWidth, histoBits, cacheBits)

	return bw.Finish(), nil
}

// writeTransformData writes transform-specific data to the bitstream.
func (enc *Encoder) writeTransformData(bw *bitio.LosslessWriter, t *Transform) {
	switch t.Type {
	case PredictorTransform, CrossColorTransform:
		bw.WriteBits(uint32(t.Bits-MinTransformBits), NumTransformBits)
		// Encode the transform data as a sub-image.
		// Use t.XSize/t.YSize (not enc.width/enc.height) because when palette +
		// predict are combined, the predict transform operates on the packed
		// palette-indexed width.
		txSize := VP8LSubSampleSize(t.XSize, t.Bits)
		tySize := VP8LSubSampleSize(t.YSize, t.Bits)
		enc.encodeSubImage(bw, t.Data, txSize, tySize)

	case SubtractGreenTransform:
		// No extra data.

	case ColorIndexingTransform:
		bw.WriteBits(uint32(enc.paletteSize-1), 8)
		// Encode palette as a sub-image (1 x paletteSize).
		enc.encodePalette(bw, t.Data)
	}
}

// encodeSubImage encodes a small sub-image (transform data or histogram image)
// into the bitstream using LZ77 backward references and Huffman coding.
//
// This matches the C reference EncodeImageNoHuffman: a single Huffman group
// (no meta-Huffman), no color cache, but with full LZ77+Huffman compression.
// The "NoHuffman" name in the C code is misleading -- it means "no meta-Huffman
// optimization", not "no Huffman coding".
func (enc *Encoder) encodeSubImage(bw *bitio.LosslessWriter, data []uint32, width, height int) {
	pixelCount := width * height
	quality := enc.config.Quality

	// Build hash chain from the sub-image pixel data (reuse if capacity sufficient).
	hc := enc.hashChain
	if hc == nil || hc.size < pixelCount {
		hc = NewHashChain(pixelCount)
		enc.hashChain = hc
	} else {
		for i := 0; i < pixelCount; i++ {
			hc.OffsetLength[i] = 0
		}
	}
	hc.Fill(data, quality, width, height, quality < 25)

	// Generate backward references using LZ77 standard + RLE strategies.
	// cache_bits = 0 (no color cache for sub-images), matching C reference.
	if enc.candidateRefs == nil {
		enc.candidateRefs = NewBackwardRefs(pixelCount)
	} else {
		enc.candidateRefs.Reset()
	}
	refs := enc.candidateRefs
	cacheBits := 0
	lz77Types := kLZ77Standard | kLZ77RLE
	GetBackwardReferences(width, height, data, quality, lz77Types, cacheBits, hc, refs)

	// Build a single histogram from the backward references (reuse scratch).
	if enc.subImageHisto == nil || len(enc.subImageHisto.Literal) < histogramNumCodes(0) {
		enc.subImageHisto = NewHistogram(0)
	} else {
		enc.subImageHisto.Clear()
	}
	h := enc.subImageHisto
	h.AddRefs(refs, width, 0)
	codes := [HuffmanCodesPerMetaCode]*HuffmanTreeCode{
		CreateHuffmanTreeScratch(h.Literal, MaxAllowedCodeLength, enc.huffScratch),
		CreateHuffmanTreeScratch(h.Red[:], MaxAllowedCodeLength, enc.huffScratch),
		CreateHuffmanTreeScratch(h.Blue[:], MaxAllowedCodeLength, enc.huffScratch),
		CreateHuffmanTreeScratch(h.Alpha[:], MaxAllowedCodeLength, enc.huffScratch),
		CreateHuffmanTreeScratch(h.Distance[:], MaxAllowedCodeLength, enc.huffScratch),
	}

	// No color cache for sub-images. Matches the C reference
	// EncodeImageNoHuffman which writes only the color cache bit.
	// The meta-Huffman (use_meta) bit is NOT written for sub-images
	// because the decoder's ReadHuffmanCodes with allow_recursion=false
	// does not read it.
	bw.WriteBits(0, 1) // no color cache

	// Store Huffman codes.
	for j := 0; j < HuffmanCodesPerMetaCode; j++ {
		StoreHuffmanCodeScratch(bw, codes[j], enc.huffScratch)
		clearHuffmanTreeIfOnlyOneSymbol(codes[j])
	}

	// Write image data using backward references + Huffman codes.
	// Use a single histogram group (index 0), no histogram tiling.
	symbols := []uint16{0}
	huffCodes := [][HuffmanCodesPerMetaCode]*HuffmanTreeCode{codes}
	enc.storeSubImageData(bw, refs, data, symbols, huffCodes, width)
}

// storeSubImageData writes sub-image data using backward references
// and Huffman codes. Unlike storeImageData, this uses no color cache
// (cacheBits=0), no histogram tiling (histoBits=0), and a single
// histogram group.
func (enc *Encoder) storeSubImageData(
	bw *bitio.LosslessWriter,
	refs *BackwardRefs,
	argb []uint32,
	symbols []uint16,
	huffCodes [][HuffmanCodesPerMetaCode]*HuffmanTreeCode,
	width int,
) {
	codes := huffCodes[0]

	x := 0
	y := 0
	for _, v := range refs.Refs() {
		switch {
		case v.IsLiteral():
			argbVal := v.Argb()
			green := (argbVal >> 8) & 0xff
			red := (argbVal >> 16) & 0xff
			blue := argbVal & 0xff
			alpha := (argbVal >> 24) & 0xff
			writeHuffmanCode(bw, codes[0], int(green))
			writeHuffmanCode(bw, codes[1], int(red))
			writeHuffmanCode(bw, codes[2], int(blue))
			writeHuffmanCode(bw, codes[3], int(alpha))
			x++

		case v.IsCopy():
			length := v.Length()
			dist := v.Distance()

			// Encode length.
			lenCode, lenExtraBits, lenExtraVal := PrefixEncodeNoLUT(length)
			writeHuffmanCode(bw, codes[0], NumLiteralCodes+lenCode)
			if lenExtraBits > 0 {
				bw.WriteBits(uint32(lenExtraVal), lenExtraBits)
			}

			// Encode distance.
			distCode, distExtraBits, distExtraVal := PrefixEncodeNoLUT(dist)
			writeHuffmanCode(bw, codes[4], distCode)
			if distExtraBits > 0 {
				bw.WriteBits(uint32(distExtraVal), distExtraBits)
			}

			x += length
		}

		// Update coordinates.
		for x >= width {
			x -= width
			y++
		}
	}
}

// encodePalette writes the palette colors to the bitstream.
func (enc *Encoder) encodePalette(bw *bitio.LosslessWriter, palette []uint32) {
	// Delta-encode palette: first pixel is literal, rest are deltas.
	if cap(enc.deltaPalette) >= len(palette) {
		enc.deltaPalette = enc.deltaPalette[:len(palette)]
	} else {
		enc.deltaPalette = make([]uint32, len(palette))
	}
	deltaPalette := enc.deltaPalette
	deltaPalette[0] = palette[0]
	for i := 1; i < len(palette); i++ {
		deltaPalette[i] = subPixelsEnc(palette[i], palette[i-1])
	}
	enc.encodeSubImage(bw, deltaPalette, len(palette), 1)
}

// maxHuffmanBits is the maximum histogram precision (MIN_HUFFMAN_BITS + (1 << NUM_HUFFMAN_BITS) - 1).
const maxHuffmanBits = MinHuffmanBits + (1 << NumHuffmanBits) - 1

// optimizeSampling checks whether the histogram image can be subsampled to a
// coarser tiling, matching the C reference VP8LOptimizeSampling. It finds the
// largest power-of-2 square_size such that each square_size x square_size
// region in the image has a uniform value, then subsamples in place.
func optimizeSampling(image []uint32, fullWidth, fullHeight, bits, maxBits int) (bestBits int) {
	width := VP8LSubSampleSize(fullWidth, bits)
	height := VP8LSubSampleSize(fullHeight, bits)
	bestBits = bits

	// Check rows first: can we double the tile size vertically?
	for bestBits < maxBits {
		newSquareSize := 1 << uint(bestBits+1-bits)
		squareSize := 1 << uint(bestBits-bits)
		isGood := true
		for y := 0; y+squareSize < height; y += newSquareSize {
			row1 := image[y*width : y*width+width]
			row2 := image[(y+squareSize)*width : (y+squareSize)*width+width]
			for x := 0; x < width; x++ {
				if row1[x] != row2[x] {
					isGood = false
					break
				}
			}
			if !isGood {
				break
			}
		}
		if isGood {
			bestBits++
		} else {
			break
		}
	}
	if bestBits == bits {
		return bits
	}

	// Check columns: verify horizontal uniformity at the best row scale.
	for bestBits > bits {
		squareSize := 1 << uint(bestBits-bits)
		isGood := true
		for y := 0; isGood && y < height; y++ {
			for x := 0; isGood && x < width; x += squareSize {
				end := x + squareSize
				if end > width {
					end = width
				}
				for i := x + 1; i < end; i++ {
					if image[y*width+i] != image[y*width+x] {
						isGood = false
						break
					}
				}
			}
		}
		if isGood {
			break
		}
		bestBits--
	}
	if bestBits == bits {
		return bits
	}

	// Subsample the image in place.
	oldWidth := width
	squareSize := 1 << uint(bestBits-bits)
	newWidth := VP8LSubSampleSize(fullWidth, bestBits)
	newHeight := VP8LSubSampleSize(fullHeight, bestBits)
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			image[y*newWidth+x] = image[squareSize*(y*oldWidth+x)]
		}
	}
	return bestBits
}

// encodeHistogramImage writes the histogram symbol assignment as a sub-image.
func (enc *Encoder) encodeHistogramImage(bw *bitio.LosslessWriter, symbols []uint16, width, height, histoBits int) {
	txSize := VP8LSubSampleSize(width, histoBits)
	tySize := VP8LSubSampleSize(height, histoBits)
	histoImage := make([]uint32, txSize*tySize)
	for i, s := range symbols {
		if i < len(histoImage) {
			// Pack histogram index in the green channel.
			histoImage[i] = uint32(s) << 8
		}
	}
	enc.encodeSubImage(bw, histoImage, txSize, tySize)
}

// storeImageData writes the actual image data using backward references
// and Huffman codes.
func (enc *Encoder) storeImageData(
	bw *bitio.LosslessWriter,
	refs *BackwardRefs,
	symbols []uint16,
	huffCodes [][HuffmanCodesPerMetaCode]*HuffmanTreeCode,
	width, histoBits, cacheBits int,
) {
	var cc *ColorCache
	if cacheBits > 0 {
		cc = NewColorCache(cacheBits)
	}

	x := 0
	y := 0
	for _, v := range refs.Refs() {
		// Determine which histogram to use.
		histoIdx := 0
		if len(huffCodes) > 1 && histoBits > 0 {
			tx := x >> histoBits
			ty := y >> histoBits
			txSize := VP8LSubSampleSize(width, histoBits)
			symIdx := ty*txSize + tx
			if symIdx < len(symbols) {
				histoIdx = int(symbols[symIdx])
			}
		}
		if histoIdx >= len(huffCodes) {
			histoIdx = 0
		}
		codes := huffCodes[histoIdx]

		switch {
		case v.IsLiteral():
			argb := v.Argb()
			green := (argb >> 8) & 0xff
			red := (argb >> 16) & 0xff
			blue := argb & 0xff
			alpha := (argb >> 24) & 0xff
			writeHuffmanCode(bw, codes[0], int(green))
			writeHuffmanCode(bw, codes[1], int(red))
			writeHuffmanCode(bw, codes[2], int(blue))
			writeHuffmanCode(bw, codes[3], int(alpha))
			if cc != nil {
				cc.Insert(argb)
			}
			x++

		case v.IsCacheIdx():
			idx := v.CacheIndex()
			writeHuffmanCode(bw, codes[0], NumLiteralCodes+NumLengthCodes+idx)
			if cc != nil {
				argb := cc.Lookup(idx)
				cc.Insert(argb)
			}
			x++

		case v.IsCopy():
			length := v.Length()
			dist := v.Distance()

			// Encode length.
			lenCode, lenExtraBits, lenExtraVal := PrefixEncodeNoLUT(length)
			writeHuffmanCode(bw, codes[0], NumLiteralCodes+lenCode)
			if lenExtraBits > 0 {
				bw.WriteBits(uint32(lenExtraVal), lenExtraBits)
			}

			// Encode distance.
			distCode, distExtraBits, distExtraVal := PrefixEncodeNoLUT(dist)
			writeHuffmanCode(bw, codes[4], distCode)
			if distExtraBits > 0 {
				bw.WriteBits(uint32(distExtraVal), distExtraBits)
			}

			// Update color cache with matched pixels.
			if cc != nil {
				// We need the actual ARGB values for cache update.
				// Since we're encoding (not decoding), we already have them.
				pos := y*width + x
				for k := 0; k < length && pos+k < len(enc.argb); k++ {
					cc.Insert(enc.argb[pos+k])
				}
			}

			x += length
		}

		// Update coordinates.
		for x >= width {
			x -= width
			y++
		}
	}
}

// writeHuffmanCode writes a single Huffman-encoded symbol to the bitstream.
func writeHuffmanCode(bw *bitio.LosslessWriter, tree *HuffmanTreeCode, symbol int) {
	if tree == nil || symbol >= tree.NumSymbols {
		return
	}
	bw.WriteBits(uint32(tree.Codes[symbol]), int(tree.CodeLengths[symbol]))
}

// subPixelsEnc performs component-wise subtraction mod 256 for palette encoding.
// The bias constants (0x00ff00ff and 0xff00ff00) prevent borrow propagation
// between adjacent channels, matching libwebp's VP8LSubPixels.
func subPixelsEnc(a, b uint32) uint32 {
	alphaAndGreen := 0x00ff00ff + (a & 0xff00ff00) - (b & 0xff00ff00)
	redAndBlue := 0xff00ff00 + (a & 0x00ff00ff) - (b & 0x00ff00ff)
	return (alphaAndGreen & 0xff00ff00) | (redAndBlue & 0x00ff00ff)
}
