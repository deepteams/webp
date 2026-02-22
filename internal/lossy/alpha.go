package lossy

import (
	"fmt"
	"math"

	"github.com/deepteams/webp/internal/lossless"
)

// Alpha compression methods.
const (
	AlphaNoCompression       = 0
	AlphaLosslessCompression = 1
)

// Alpha filtering methods.
const (
	AlphaFilterNone       = 0
	AlphaFilterHorizontal = 1
	AlphaFilterVertical   = 2
	AlphaFilterGradient   = 3
	alphaFilterLast       = 4 // sentinel
)

// Alpha filter mode constants for GetFilterMap / EncodeAlpha.
const (
	AlphaFilterModeNone = 0 // No filtering.
	AlphaFilterModeFast = 4 // Quick estimate of best filter.
	AlphaFilterModeBest = 5 // Try all filters and pick smallest.
)

// alphaPreprocessedLevels is the header flag for pre-processed (quantized) alpha.
const alphaPreprocessedLevels = 1

// AlphaDecoder decodes the alpha plane from a WebP ALPH chunk.
type AlphaDecoder struct {
	width  int
	height int
}

// DecodeAlpha decodes an alpha plane from the given ALPH chunk data.
// Returns the alpha plane as a width*height byte slice.
func DecodeAlpha(data []byte, width, height int) ([]byte, error) {
	if len(data) < 1 {
		return nil, fmt.Errorf("alpha: empty data")
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("alpha: invalid dimensions %dx%d", width, height)
	}
	area := uint64(width) * uint64(height)
	if area > 1<<30 {
		return nil, fmt.Errorf("alpha: plane too large (%dx%d = %d pixels)", width, height, area)
	}

	// Parse the 1-byte alpha header.
	header := data[0]
	compression := (header >> 0) & 0x03
	filtering := (header >> 2) & 0x03
	_ = (header >> 4) & 0x03 // pre-processing (unused in decode)

	payload := data[1:]
	planeSize := int(area)

	var raw []byte

	switch compression {
	case AlphaNoCompression:
		// Raw data.
		if len(payload) < planeSize {
			return nil, fmt.Errorf("alpha: truncated uncompressed data")
		}
		raw = make([]byte, planeSize)
		copy(raw, payload[:planeSize])

	case AlphaLosslessCompression:
		// VP8L-compressed alpha: decode using the lossless decoder.
		// The alpha data is encoded as a VP8L bitstream where the
		// green channel contains the alpha values.
		alphaImage, err := lossless.DecodeVP8L(payload)
		if err != nil {
			return nil, fmt.Errorf("alpha: VP8L decode failed: %w", err)
		}
		// Validate that the decoded image dimensions match expectations.
		bounds := alphaImage.Bounds()
		if bounds.Dx() < width || bounds.Dy() < height {
			return nil, fmt.Errorf("alpha: decoded image %dx%d smaller than expected %dx%d", bounds.Dx(), bounds.Dy(), width, height)
		}
		// Extract the green channel as the alpha plane.
		raw = make([]byte, planeSize)
		pix := alphaImage.Pix
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				off := alphaImage.PixOffset(x, y)
				// Green channel is at offset 1 in NRGBA (R=0, G=1, B=2, A=3).
				if off < 0 || off+1 >= len(pix) {
					return nil, fmt.Errorf("alpha: pixel offset out of bounds at (%d,%d)", x, y)
				}
				raw[y*width+x] = pix[off+1]
			}
		}

	default:
		return nil, fmt.Errorf("alpha: unknown compression method %d", compression)
	}

	// Apply inverse filtering.
	switch filtering {
	case AlphaFilterNone:
		// Nothing.
	case AlphaFilterHorizontal:
		alphaUnfilterHorizontal(raw, width, height)
	case AlphaFilterVertical:
		alphaUnfilterVertical(raw, width, height)
	case AlphaFilterGradient:
		alphaUnfilterGradient(raw, width, height)
	default:
		return nil, fmt.Errorf("alpha: unknown filter method %d", filtering)
	}

	return raw, nil
}

// alphaUnfilterHorizontal applies inverse horizontal prediction.
// Matches libwebp HorizontalUnfilter_C: for row 0, pred starts at 0;
// for rows > 0, pred starts at prev_row[0] (cross-row prediction).
func alphaUnfilterHorizontal(data []byte, width, height int) {
	for y := 0; y < height; y++ {
		row := data[y*width : (y+1)*width]
		// For y==0 (no previous row), initial prediction is 0 (row[0] unchanged).
		// For y>0, initial prediction is prev_row[0].
		if y > 0 {
			row[0] += data[(y-1)*width]
		}
		for x := 1; x < width; x++ {
			row[x] += row[x-1]
		}
	}
}

// alphaUnfilterVertical applies inverse vertical prediction.
// Matches libwebp VerticalUnfilter_C: the first row (prev==NULL) uses
// horizontal unfilter (cumulative sums from 0), not raw deltas.
func alphaUnfilterVertical(data []byte, width, height int) {
	// First row: horizontal unfilter (cumulative sums from initial pred=0).
	alphaUnfilterHorizontalRow(data[:width])
	// Remaining rows: add previous row.
	for y := 1; y < height; y++ {
		curr := data[y*width : (y+1)*width]
		prev := data[(y-1)*width : y*width]
		for x := 0; x < width; x++ {
			curr[x] += prev[x]
		}
	}
}

// alphaUnfilterHorizontalRow applies horizontal unfilter to a single row
// with initial prediction = 0 (used by vertical and gradient for the first row).
func alphaUnfilterHorizontalRow(row []byte) {
	for x := 1; x < len(row); x++ {
		row[x] += row[x-1]
	}
}

// alphaUnfilterGradient applies inverse gradient prediction.
// Matches libwebp GradientUnfilter_C: first row uses horizontal unfilter,
// remaining rows use gradient prediction (left + top - top_left, clamped).
func alphaUnfilterGradient(data []byte, width, height int) {
	// First row: horizontal unfilter (cumulative sums from initial pred=0).
	alphaUnfilterHorizontalRow(data[:width])
	// Remaining rows: gradient prediction (matching C single-pass logic).
	for y := 1; y < height; y++ {
		curr := data[y*width : (y+1)*width]
		prev := data[(y-1)*width : y*width]
		// C: top = prev[0], top_left = top, left = top
		top := prev[0]
		topLeft := top
		left := top
		for x := 0; x < width; x++ {
			top = prev[x]
			pred := int(left) + int(top) - int(topLeft)
			if pred < 0 {
				pred = 0
			} else if pred > 255 {
				pred = 255
			}
			left = curr[x] + byte(pred)
			topLeft = top
			curr[x] = left
		}
	}
}

// ---------------------------------------------------------------------------
// Alpha Encoder (matches libwebp/src/enc/alpha_enc.c)
// ---------------------------------------------------------------------------

// AlphaEncoderConfig holds parameters for alpha plane encoding.
type AlphaEncoderConfig struct {
	Quality     int // 0-100. quality < 100 enables level quantization.
	Method      int // 0 (no compression) or 1 (lossless compression).
	Filter      int // AlphaFilterMode{None,Fast,Best} or a specific filter [0..3].
	EffortLevel int // 0-6, maps to VP8L encoding effort.
}

// EncodeAlpha encodes the alpha plane (width x height byte slice) into an
// ALPH chunk payload (header byte + compressed data).
// This mirrors libwebp's EncodeAlpha (alpha_enc.c:298-367).
func EncodeAlpha(alpha []byte, width, height int, cfg *AlphaEncoderConfig) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("alpha: invalid dimensions %dx%d", width, height)
	}
	dataSize := width * height
	if len(alpha) < dataSize {
		return nil, fmt.Errorf("alpha: input too short (%d < %d)", len(alpha), dataSize)
	}

	quality := cfg.Quality
	if quality < 0 {
		quality = 0
	}
	if quality > 100 {
		quality = 100
	}

	method := cfg.Method
	if method < AlphaNoCompression || method > AlphaLosslessCompression {
		return nil, fmt.Errorf("alpha: invalid method %d", method)
	}

	filter := cfg.Filter
	effortLevel := cfg.EffortLevel
	if effortLevel < 0 {
		effortLevel = 0
	}
	if effortLevel > 6 {
		effortLevel = 6
	}

	if method == AlphaNoCompression {
		// Don't filter when not compressing; filtering won't help.
		filter = AlphaFilterModeNone
	}

	// Copy alpha data; quantization modifies in-place.
	quantAlpha := make([]byte, dataSize)
	copy(quantAlpha, alpha[:dataSize])

	reduceLevels := quality < 100
	if reduceLevels {
		// Map quality to number of alpha levels:
		// Quality:[0, 70] -> Levels:[2, 16]
		// Quality:]70, 100] -> Levels:]16, 256]
		var alphaLevels int
		if quality <= 70 {
			alphaLevels = 2 + quality/5
		} else {
			alphaLevels = 16 + (quality-70)*8
		}
		quantizeLevels(quantAlpha, width, height, alphaLevels)
	}

	// Apply filters and encode, picking the best filter.
	return applyFiltersAndEncode(quantAlpha, width, height, method, filter,
		reduceLevels, effortLevel)
}

// getFilterMap returns an OR'd bit-set of filters to try, matching
// libwebp's GetFilterMap (alpha_enc.c:208-233).
func getFilterMap(alpha []byte, width, height, filter, effortLevel int) uint32 {
	const (
		filterTryNone = 1 << AlphaFilterNone
		filterTryAll  = (1 << alphaFilterLast) - 1
	)
	switch {
	case filter == AlphaFilterModeFast:
		// Quick estimate of the best candidate.
		tryFilterNone := effortLevel > 3
		const kMinColorsForFilterNone = 16
		const kMaxColorsForFilterNone = 192
		numColors := getNumColors(alpha, width, height)
		if numColors <= kMinColorsForFilterNone {
			filter = AlphaFilterNone
		} else {
			filter = estimateBestFilter(alpha, width, height)
		}
		bitMap := uint32(1 << uint(filter))
		if tryFilterNone || numColors > kMaxColorsForFilterNone {
			bitMap |= filterTryNone
		}
		return bitMap
	case filter == AlphaFilterModeNone || filter == AlphaFilterNone:
		return filterTryNone
	default:
		// Best mode or explicit: try all.
		return filterTryAll
	}
}

// getNumColors counts the number of distinct byte values in the alpha plane.
func getNumColors(data []byte, width, height int) int {
	var color [256]bool
	for j := 0; j < height; j++ {
		row := data[j*width : j*width+width]
		for _, v := range row {
			color[v] = true
		}
	}
	n := 0
	for _, c := range color {
		if c {
			n++
		}
	}
	return n
}

// estimateBestFilter estimates which alpha filter will yield the best
// compression. Matches libwebp's WebPEstimateBestFilter (filters_utils.c).
func estimateBestFilter(data []byte, width, height int) int {
	const smax = 16
	sdiff := func(a, b int) int {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d >> 4
	}
	gradientPredictor := func(a, b, c byte) int {
		g := int(a) + int(b) - int(c)
		if g < 0 {
			return 0
		}
		if g > 255 {
			return 255
		}
		return g
	}

	var bins [alphaFilterLast][smax]int

	for j := 2; j < height-1; j += 2 {
		off := j * width
		mean := int(data[off])
		for i := 2; i < width-1; i += 2 {
			cur := int(data[off+i])
			diff0 := sdiff(cur, mean)
			diff1 := sdiff(cur, int(data[off+i-1]))
			diff2 := sdiff(cur, int(data[off+i-width]))
			gradPred := gradientPredictor(data[off+i-1], data[off+i-width], data[off+i-width-1])
			diff3 := sdiff(cur, gradPred)
			if diff0 < smax {
				bins[AlphaFilterNone][diff0] = 1
			}
			if diff1 < smax {
				bins[AlphaFilterHorizontal][diff1] = 1
			}
			if diff2 < smax {
				bins[AlphaFilterVertical][diff2] = 1
			}
			if diff3 < smax {
				bins[AlphaFilterGradient][diff3] = 1
			}
			mean = (3*mean + cur + 2) >> 2
		}
	}

	bestFilter := AlphaFilterNone
	bestScore := math.MaxInt32
	for f := AlphaFilterNone; f < alphaFilterLast; f++ {
		score := 0
		for i := 0; i < smax; i++ {
			if bins[f][i] > 0 {
				score += i
			}
		}
		if score < bestScore {
			bestScore = score
			bestFilter = f
		}
	}
	return bestFilter
}

// alphaFilterHorizontal applies forward horizontal prediction filter.
func alphaFilterHorizontal(in []byte, width, height int, out []byte) {
	// First pixel of first row.
	out[0] = in[0]
	// Rest of first row: predict from left.
	for i := 1; i < width; i++ {
		out[i] = in[i] - in[i-1]
	}
	// Remaining rows.
	for y := 1; y < height; y++ {
		src := in[y*width:]
		dst := out[y*width:]
		prev := in[(y-1)*width:]
		// First pixel: predict from above.
		dst[0] = src[0] - prev[0]
		// Rest: predict from left.
		for x := 1; x < width; x++ {
			dst[x] = src[x] - src[x-1]
		}
	}
}

// alphaFilterVertical applies forward vertical prediction filter.
func alphaFilterVertical(in []byte, width, height int, out []byte) {
	// First pixel.
	out[0] = in[0]
	// Rest of first row: predict from left.
	for i := 1; i < width; i++ {
		out[i] = in[i] - in[i-1]
	}
	// Remaining rows: predict from above.
	for y := 1; y < height; y++ {
		src := in[y*width:]
		dst := out[y*width:]
		prev := in[(y-1)*width:]
		for x := 0; x < width; x++ {
			dst[x] = src[x] - prev[x]
		}
	}
}

// alphaFilterGradient applies forward gradient prediction filter.
func alphaFilterGradient(in []byte, width, height int, out []byte) {
	// First pixel.
	out[0] = in[0]
	// Rest of first row: predict from left.
	for i := 1; i < width; i++ {
		out[i] = in[i] - in[i-1]
	}
	// Remaining rows.
	for y := 1; y < height; y++ {
		src := in[y*width:]
		dst := out[y*width:]
		prev := in[(y-1)*width:]
		// First pixel: predict from above.
		dst[0] = src[0] - prev[0]
		for x := 1; x < width; x++ {
			pred := int(src[x-1]) + int(prev[x]) - int(prev[x-1])
			if pred < 0 {
				pred = 0
			} else if pred > 255 {
				pred = 255
			}
			dst[x] = src[x] - byte(pred)
		}
	}
}

// encodeAlphaInternal encodes alpha data with a specific filter, returning
// the complete ALPH chunk payload (header + data).
func encodeAlphaInternal(data []byte, width, height, method, filter int,
	reduceLevels bool, effortLevel int) ([]byte, int, error) {

	dataSize := width * height

	// Apply filter.
	var alphaSrc []byte
	if filter != AlphaFilterNone {
		filtered := make([]byte, dataSize)
		switch filter {
		case AlphaFilterHorizontal:
			alphaFilterHorizontal(data, width, height, filtered)
		case AlphaFilterVertical:
			alphaFilterVertical(data, width, height, filtered)
		case AlphaFilterGradient:
			alphaFilterGradient(data, width, height, filtered)
		}
		alphaSrc = filtered
	} else {
		alphaSrc = data
	}

	var output []byte

	if method == AlphaLosslessCompression {
		// Encode alpha values via VP8L lossless encoder.
		// Alpha values are placed in the green channel of an ARGB image.
		argb := make([]uint32, dataSize)
		for i, a := range alphaSrc {
			argb[i] = 0xff000000 | (uint32(a) << 8)
		}

		// Configure VP8L encoding effort.
		var q int
		if !reduceLevels && effortLevel == 6 {
			q = 100
		} else {
			q = 8 * effortLevel
		}
		if q > 100 {
			q = 100
		}

		lcfg := &lossless.EncoderConfig{
			Quality: q,
			Method:  effortLevel,
			NearLosslessQuality: 100,
		}
		compressed, err := lossless.Encode(argb, width, height, lcfg)
		if err != nil {
			return nil, 0, fmt.Errorf("alpha: VP8L encode failed: %w", err)
		}

		if len(compressed) > dataSize {
			// Compressed is larger than raw — fall back to uncompressed.
			method = AlphaNoCompression
			output = alphaSrc
		} else {
			output = compressed
		}
	}

	if method == AlphaNoCompression {
		output = alphaSrc
	}

	// Build result: 1-byte header + output.
	header := byte(method) | byte(filter<<2)
	if reduceLevels {
		header |= byte(alphaPreprocessedLevels << 4)
	}

	result := make([]byte, 1+len(output))
	result[0] = header
	copy(result[1:], output)

	return result, len(result), nil
}

// applyFiltersAndEncode tries the filter(s) selected by getFilterMap and
// returns the best (smallest) encoding.
func applyFiltersAndEncode(alpha []byte, width, height, method, filter int,
	reduceLevels bool, effortLevel int) ([]byte, error) {

	tryMap := getFilterMap(alpha, width, height, filter, effortLevel)

	type trial struct {
		data  []byte
		score int
	}
	best := trial{score: math.MaxInt32}

	for f := AlphaFilterNone; f < alphaFilterLast && tryMap != 0; f++ {
		if tryMap&1 != 0 {
			result, score, err := encodeAlphaInternal(alpha, width, height,
				method, f, reduceLevels, effortLevel)
			if err != nil {
				return nil, err
			}
			if score < best.score {
				best.data = result
				best.score = score
			}
		}
		tryMap >>= 1
	}

	if best.data == nil {
		// Fallback: no filter.
		result, _, err := encodeAlphaInternal(alpha, width, height,
			method, AlphaFilterNone, reduceLevels, effortLevel)
		if err != nil {
			return nil, err
		}
		best.data = result
	}

	return best.data, nil
}

// quantizeLevels quantizes the alpha plane to a reduced number of levels.
// Matches libwebp's QuantizeLevels (quant_levels_utils.c).
func quantizeLevels(data []byte, width, height, numLevels int) {
	if numLevels < 2 || numLevels > 256 {
		return
	}
	dataSize := width * height
	if dataSize == 0 {
		return
	}

	const numSymbols = 256
	const maxIter = 6
	const errThreshold = 1e-4

	var freq [numSymbols]int
	minS, maxS := 255, 0
	numLevelsIn := 0
	for i := 0; i < dataSize; i++ {
		v := data[i]
		if freq[v] == 0 {
			numLevelsIn++
		}
		if int(v) < minS {
			minS = int(v)
		}
		if int(v) > maxS {
			maxS = int(v)
		}
		freq[v]++
	}

	if numLevelsIn <= numLevels {
		return // nothing to do
	}

	// Start with uniformly spread centroids.
	var invQLevel [numSymbols]float64
	var qLevel [numSymbols]int
	for i := 0; i < numLevels; i++ {
		invQLevel[i] = float64(minS) + float64(maxS-minS)*float64(i)/float64(numLevels-1)
	}
	qLevel[minS] = 0
	qLevel[maxS] = numLevels - 1

	// K-Means iterations.
	errThreshScaled := errThreshold * float64(dataSize)
	lastErr := 1e38
	for iter := 0; iter < maxIter; iter++ {
		var qSum [numSymbols]float64
		var qCount [numSymbols]float64
		slot := 0

		for s := minS; s <= maxS; s++ {
			for slot < numLevels-1 &&
				2*float64(s) > invQLevel[slot]+invQLevel[slot+1] {
				slot++
			}
			if freq[s] > 0 {
				qSum[slot] += float64(s) * float64(freq[s])
				qCount[slot] += float64(freq[s])
			}
			qLevel[s] = slot
		}

		if numLevels > 2 {
			for slot = 1; slot < numLevels-1; slot++ {
				if qCount[slot] > 0 {
					invQLevel[slot] = qSum[slot] / qCount[slot]
				}
			}
		}

		err := 0.0
		for s := minS; s <= maxS; s++ {
			e := float64(s) - invQLevel[qLevel[s]]
			err += float64(freq[s]) * e * e
		}
		if lastErr-err < errThreshScaled {
			break
		}
		lastErr = err
	}

	// Remap alpha values.
	var remap [numSymbols]byte
	for s := minS; s <= maxS; s++ {
		remap[s] = byte(invQLevel[qLevel[s]] + 0.5)
	}
	for i := 0; i < dataSize; i++ {
		data[i] = remap[data[i]]
	}
}
