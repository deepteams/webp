package lossless

// VP8L forward transform selection for lossless encoding.
//
// Implements predictor selection, subtract-green, cross-color transform,
// and color-indexing (palette) construction. All predictors are implemented
// inline for self-containment.
//
// Reference: libwebp/src/enc/vp8l_enc.c, libwebp/src/dsp/lossless.c

import (
	"math"
	"runtime"
	"sort"
	"sync"
)

// numPredictors is the number of VP8L spatial predictors to evaluate (0-13).
const numPredictors = 14

// Multipliers holds the cross-color transform multipliers for a tile.
type Multipliers struct {
	GreenToRed  int8
	GreenToBlue int8
	RedToBlue   int8
}

// ---------------------------------------------------------------------------
// Pixel arithmetic helpers
// ---------------------------------------------------------------------------

// subPixels computes component-wise subtraction (a - b) mod 256.
// The bias constants prevent borrow propagation between adjacent channels,
// matching libwebp's VP8LSubPixels.
func subPixels(a, b uint32) uint32 {
	alphaAndGreen := 0x00ff00ff + (a & 0xff00ff00) - (b & 0xff00ff00)
	redAndBlue := 0xff00ff00 + (a & 0x00ff00ff) - (b & 0x00ff00ff)
	return (alphaAndGreen & 0xff00ff00) | (redAndBlue & 0x00ff00ff)
}

// avg2 computes per-component average of two ARGB pixels without overflow.
func avg2(a, b uint32) uint32 {
	return (((a ^ b) & 0xfefefefe) >> 1) + (a & b)
}

// selectPred implements the VP8L select predictor.
// Compares per-component distance |T-TL| vs |L-TL| to decide T or L.
func selectPred(left, top, topLeft uint32) uint32 {
	pa := int32(0)
	for shift := uint(0); shift < 32; shift += 8 {
		ac := int32((top>>shift)&0xff) - int32((topLeft>>shift)&0xff)
		bc := int32((left>>shift)&0xff) - int32((topLeft>>shift)&0xff)
		if ac < 0 {
			ac = -ac
		}
		if bc < 0 {
			bc = -bc
		}
		pa += ac - bc
	}
	if pa <= 0 {
		return top
	}
	return left
}

// clampByte clamps v to [0, 255].
func clampByte(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// clampAddSubFull computes (a + b - c) per component, clamped to [0, 255].
func clampAddSubFull(a, b, c uint32) uint32 {
	var result uint32
	for shift := uint(0); shift < 32; shift += 8 {
		va := int32((a >> shift) & 0xff)
		vb := int32((b >> shift) & 0xff)
		vc := int32((c >> shift) & 0xff)
		result |= uint32(clampByte(va+vb-vc)) << shift
	}
	return result
}

// clampAddSubHalf computes avg + (avg - c) / 2 per component, clamped.
func clampAddSubHalf(avg, c uint32) uint32 {
	var result uint32
	for shift := uint(0); shift < 32; shift += 8 {
		va := int32((avg >> shift) & 0xff)
		vc := int32((c >> shift) & 0xff)
		result |= uint32(clampByte(va+(va-vc)/2)) << shift
	}
	return result
}

// predictPixel returns the predicted pixel for the given mode using the
// standard VP8L predictor definitions (modes 0-13).
//
//	Mode 0:  ARGB_BLACK (0xff000000)
//	Mode 1:  L
//	Mode 2:  T
//	Mode 3:  TR (top-right)
//	Mode 4:  TL (top-left)
//	Mode 5:  avg2(avg2(L, TR), T)
//	Mode 6:  avg2(L, TL)
//	Mode 7:  avg2(L, T)
//	Mode 8:  avg2(TL, T)
//	Mode 9:  avg2(T, TR)
//	Mode 10: avg2(avg2(L, TL), avg2(T, TR))
//	Mode 11: Select(L, T, TL)
//	Mode 12: ClampedAddSubtractFull(L, T, TL)
//	Mode 13: ClampedAddSubtractHalf(avg2(L, T), TL)
func predictPixel(mode int, left, top, topRight, topLeft uint32) uint32 {
	switch mode {
	case 0:
		return ARGBBlack
	case 1:
		return left
	case 2:
		return top
	case 3:
		return topRight
	case 4:
		return topLeft
	case 5:
		return avg2(avg2(left, topRight), top)
	case 6:
		return avg2(left, topLeft)
	case 7:
		return avg2(left, top)
	case 8:
		return avg2(topLeft, top)
	case 9:
		return avg2(top, topRight)
	case 10:
		return avg2(avg2(left, topLeft), avg2(top, topRight))
	case 11:
		return selectPred(left, top, topLeft)
	case 12:
		return clampAddSubFull(left, top, topLeft)
	case 13:
		return clampAddSubHalf(avg2(left, top), topLeft)
	default:
		return ARGBBlack
	}
}

// ---------------------------------------------------------------------------
// Entropy cost estimation
// ---------------------------------------------------------------------------

// estimateEntropy returns a quick entropy estimate for a tile's prediction
// residuals under the given predictor mode. Uses per-channel histograms
// across all 4 ARGB channels to approximate the bit cost, matching the
// C reference (VP8LResidualImage / PredictionCostSpatialHistogram).
func estimateEntropy(argb []uint32, width, height, tx, ty, bits, mode int) float64 {
	tileSize := 1 << bits
	xStart := tx * tileSize
	yStart := ty * tileSize
	xEnd := xStart + tileSize
	if xEnd > width {
		xEnd = width
	}
	yEnd := yStart + tileSize
	if yEnd > height {
		yEnd = height
	}

	// 4 histograms of 256 bins each: [0]=alpha, [1]=red, [2]=green, [3]=blue
	var histogram [4 * 256]int
	count := 0

	for y := yStart; y < yEnd; y++ {
		for x := xStart; x < xEnd; x++ {
			px := argb[y*width+x]

			var left, top, topRight, topLeft uint32
			if x > 0 {
				left = argb[y*width+x-1]
			}
			if y > 0 {
				top = argb[(y-1)*width+x]
				if x > 0 {
					topLeft = argb[(y-1)*width+x-1]
				}
				if x < width-1 {
					topRight = argb[(y-1)*width+x+1]
				} else {
					topRight = top
				}
			}

			// For the first pixel (0,0) and borders, some neighbours default to 0.
			pred := predictPixel(mode, left, top, topRight, topLeft)
			residual := subPixels(px, pred)

			// Accumulate all 4 channels into their respective histograms.
			histogram[0*256+int((residual>>24)&0xff)]++ // alpha
			histogram[1*256+int((residual>>16)&0xff)]++ // red
			histogram[2*256+int((residual>>8)&0xff)]++  // green
			histogram[3*256+int(residual&0xff)]++       // blue
			count++
		}
	}

	if count == 0 {
		return 0
	}

	// Shannon entropy summed across all 4 channel histograms.
	// Uses fastSLog2(n) = n*log2(n) identity:
	//   H*count = sum_ch(fastSLog2(count) - sum_bins(fastSLog2(h_i)))
	entropy := 0.0
	countU := uint32(count)
	for ch := 0; ch < 4; ch++ {
		channelEntropy := fastSLog2(countU)
		base := ch * 256
		for i := 0; i < 256; i++ {
			if histogram[base+i] > 0 {
				channelEntropy -= fastSLog2(uint32(histogram[base+i]))
			}
		}
		entropy += channelEntropy
	}

	return entropy
}

// ---------------------------------------------------------------------------
// Apply predictor residuals using scratch buffers (forward transform)
// ---------------------------------------------------------------------------

// copyImageWithPrediction computes prediction residuals for the entire image
// using scratch row buffers, matching libwebp's CopyImageWithPrediction.
//
// The key invariant is that predictions are always computed from ORIGINAL pixel
// values, never from already-computed residuals. Two scratch rows (upperRow and
// currentRow) hold copies of the original pixels. Residuals are written
// directly to the output array.
//
// Arguments:
//   - argb: the original (unmodified) pixel data
//   - width, height: image dimensions
//   - bits: tile size exponent (tile side = 1 << bits)
//   - modes: the transform data array with the predictor mode per tile
//   - out: the output array where residuals are written (same length as argb)
func copyImageWithPrediction(argb []uint32, width, height, bits int, modes []uint32, out []uint32) {
	tilesPerRow := VP8LSubSampleSize(width, bits)

	// Scratch buffers: width+1 to allow the top-right pixel to wrap to the
	// leftmost pixel of the next row when at the right edge.
	// Fused into a single allocation.
	rowBuf := make([]uint32, 2*(width+1))
	upperRow := rowBuf[:width+1]
	currentRow := rowBuf[width+1:]

	for y := 0; y < height; y++ {
		// Swap: previous currentRow becomes upperRow.
		upperRow, currentRow = currentRow, upperRow

		// Copy the current row of original pixels into currentRow.
		// Include one extra pixel to the right (wrapping to the next row's
		// first pixel) so that the top-right neighbor is available.
		copyLen := width
		if y+1 < height {
			copyLen = width + 1
		}
		copy(currentRow, argb[y*width:y*width+copyLen])

		for x := 0; x < width; {
			mode := int((modes[(y>>uint(bits))*tilesPerRow+(x>>uint(bits))] >> 8) & 0xff)
			xEnd := (x | ((1 << uint(bits)) - 1)) + 1 // next tile boundary
			if xEnd > width {
				xEnd = width
			}

			for ; x < xEnd; x++ {
				var pred uint32
				// The encoder must override predictor modes at edges to match
				// the decoder (PredictorInverseTransform), which always uses:
				//   row 0, x=0: mode 0 (ARGB_BLACK)
				//   row 0, x>0: mode 1 (left pixel)
				//   row >0, x=0: mode 2 (top pixel)
				// This matches C's GetResidual in predictor_enc.c.
				if y == 0 {
					if x == 0 {
						pred = ARGBBlack
					} else {
						pred = currentRow[x-1] // left
					}
				} else if x == 0 {
					pred = upperRow[x] // top
				} else {
					left := currentRow[x-1]
					top := upperRow[x]
					topLeft := upperRow[x-1]
					var topRight uint32
					if x < width-1 {
						topRight = upperRow[x+1]
					} else {
						topRight = upperRow[width]
					}
					pred = predictPixel(mode, left, top, topRight, topLeft)
				}
				out[y*width+x] = subPixels(currentRow[x], pred)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// ResidualImage: predictor selection + residual computation
// ---------------------------------------------------------------------------

// ResidualImage selects the best predictor per tile and computes prediction
// residuals. Returns the transform data (predictor modes encoded per tile)
// and the residual image.
//
// The implementation is split into two phases to avoid the in-place corruption
// bug where residuals overwrite original pixels before they are needed as
// neighbors for adjacent predictions:
//
//   Phase 1: Select the best predictor mode per tile by evaluating entropy
//            costs on the ORIGINAL (unmodified) pixel data.
//   Phase 2: Compute all residuals using scratch row buffers that hold copies
//            of original pixels, matching libwebp's CopyImageWithPrediction.
func ResidualImage(argb []uint32, width, height, bits, quality int, residualsBuf []uint32) (transformData []uint32, residuals []uint32) {
	tileXSize := VP8LSubSampleSize(width, bits)
	tileYSize := VP8LSubSampleSize(height, bits)
	transformData = make([]uint32, tileXSize*tileYSize)

	// Maximum number of predictors to try depends on quality.
	maxMode := numPredictors
	if quality < 25 {
		maxMode = 4
	} else if quality < 50 {
		maxMode = 8
	}

	// Phase 1: Select best predictor per tile using ORIGINAL pixels.
	// estimateEntropy reads from argb but never modifies it, so all tiles
	// can be evaluated in parallel.
	numTiles := tileXSize * tileYSize
	if numTiles >= 16 {
		// Parallel predictor selection: partition tile rows across goroutines.
		numWorkers := runtime.GOMAXPROCS(0)
		if numWorkers > tileYSize {
			numWorkers = tileYSize
		}
		var wg sync.WaitGroup
		wg.Add(numWorkers)
		rowsPerWorker := (tileYSize + numWorkers - 1) / numWorkers
		for w := 0; w < numWorkers; w++ {
			tyStart := w * rowsPerWorker
			tyEnd := tyStart + rowsPerWorker
			if tyEnd > tileYSize {
				tyEnd = tileYSize
			}
			go func(tyStart, tyEnd int) {
				defer wg.Done()
				for ty := tyStart; ty < tyEnd; ty++ {
					for tx := 0; tx < tileXSize; tx++ {
						bestMode := 0
						bestCost := math.MaxFloat64
						for mode := 0; mode < maxMode; mode++ {
							cost := estimateEntropy(argb, width, height, tx, ty, bits, mode)
							if cost < bestCost {
								bestCost = cost
								bestMode = mode
							}
						}
						transformData[ty*tileXSize+tx] = uint32(bestMode)<<8 | ARGBBlack
					}
				}
			}(tyStart, tyEnd)
		}
		wg.Wait()
	} else {
		for ty := 0; ty < tileYSize; ty++ {
			for tx := 0; tx < tileXSize; tx++ {
				bestMode := 0
				bestCost := math.MaxFloat64
				for mode := 0; mode < maxMode; mode++ {
					cost := estimateEntropy(argb, width, height, tx, ty, bits, mode)
					if cost < bestCost {
						bestCost = cost
						bestMode = mode
					}
				}
				transformData[ty*tileXSize+tx] = uint32(bestMode)<<8 | ARGBBlack
			}
		}
	}

	// Phase 2: Compute residuals using scratch row buffers so that
	// predictions are always computed from original pixel values.
	pixCount := len(argb)
	if cap(residualsBuf) >= pixCount {
		residuals = residualsBuf[:pixCount]
	} else {
		residuals = make([]uint32, pixCount)
	}
	copyImageWithPrediction(argb, width, height, bits, transformData, residuals)

	return transformData, residuals
}

// ---------------------------------------------------------------------------
// Subtract green transform
// ---------------------------------------------------------------------------

// SubtractGreen applies the subtract-green transform in place.
// For each pixel, the green channel value is subtracted from red and blue.
func SubtractGreen(argb []uint32) {
	for i, px := range argb {
		green := (px >> 8) & 0xff
		red := ((px >> 16) & 0xff - green) & 0xff
		blue := (px&0xff - green) & 0xff
		argb[i] = (px & 0xff00ff00) | (red << 16) | blue
	}
}

// ---------------------------------------------------------------------------
// Cross-color transform helpers
// ---------------------------------------------------------------------------

// encColorTransformDelta computes (m * color) >> 5, matching libwebp.
func encColorTransformDelta(m int8, color uint8) int8 {
	return int8((int32(m) * int32(int8(color))) >> 5)
}

// packMultipliers encodes Multipliers into a uint32 for the transform data.
// Layout matches the decoder's colorCodeToMultipliers:
//
//	bits [7:0]   = greenToRed
//	bits [15:8]  = greenToBlue
//	bits [23:16] = redToBlue
func packMultipliers(m Multipliers) uint32 {
	return uint32(uint8(m.GreenToRed)) |
		uint32(uint8(m.GreenToBlue))<<8 |
		uint32(uint8(m.RedToBlue))<<16
}

// applyColorTransformPixel applies the forward cross-color transform to a
// single pixel: subtracts the predicted color shifts from red and blue.
func applyColorTransformPixel(m Multipliers, argb uint32) uint32 {
	green := int8(argb >> 8)
	red := uint8(argb >> 16)
	blue := uint8(argb)

	newRed := (int32(red) - int32(encColorTransformDelta(m.GreenToRed, uint8(green)))) & 0xff
	newBlue := (int32(blue) - int32(encColorTransformDelta(m.GreenToBlue, uint8(green)))) & 0xff
	newBlue = (newBlue - int32(encColorTransformDelta(m.RedToBlue, uint8(red)))) & 0xff

	return (argb & 0xff00ff00) | (uint32(newRed) << 16) | uint32(newBlue)
}

// findBestMultipliers finds the best cross-color multipliers for a tile
// by minimizing the absolute sum of residuals over a sparse search.
// scratch must have length >= 5 * maxTilePixels (greens, reds, blues,
// adjustedReds, adjustedBlues). If nil, buffers are allocated.
func findBestMultipliers(argb []uint32, width, height, tx, ty, bits int, scratch []int32) Multipliers {
	tileSize := 1 << bits
	xStart := tx * tileSize
	yStart := ty * tileSize
	xEnd := xStart + tileSize
	if xEnd > width {
		xEnd = width
	}
	yEnd := yStart + tileSize
	if yEnd > height {
		yEnd = height
	}

	maxPixels := (xEnd - xStart) * (yEnd - yStart)
	if maxPixels == 0 {
		return Multipliers{}
	}

	// Use scratch buffer for all 5 arrays: greens, reds, blues, adjustedReds, adjustedBlues.
	needed := 5 * maxPixels
	if len(scratch) < needed {
		scratch = make([]int32, needed)
	}
	greens := scratch[:maxPixels]
	reds := scratch[maxPixels : 2*maxPixels]
	blues := scratch[2*maxPixels : 3*maxPixels]
	adjustedReds := scratch[3*maxPixels : 4*maxPixels]
	adjustedBlues := scratch[4*maxPixels : 5*maxPixels]

	// Collect green, red, blue samples from the tile.
	n := 0
	for y := yStart; y < yEnd; y++ {
		for x := xStart; x < xEnd; x++ {
			px := argb[y*width+x]
			greens[n] = int32(int8(px >> 8))
			reds[n] = int32(uint8(px >> 16))
			blues[n] = int32(uint8(px))
			n++
		}
	}
	greens = greens[:n]
	reds = reds[:n]
	blues = blues[:n]

	// Search for greenToRed that minimizes sum of |red - (greenToRed * green >> 5)|.
	bestGreenToRed := findBestMultiplier(greens, reds)

	// Compute adjusted reds after greenToRed correction.
	adjustedReds = adjustedReds[:n]
	for i, r := range reds {
		adjustedReds[i] = (r - int32(encColorTransformDelta(bestGreenToRed, uint8(greens[i])))) & 0xff
	}

	// Search for greenToBlue.
	bestGreenToBlue := findBestMultiplier(greens, blues)

	// Search for redToBlue using adjusted reds.
	adjustedBlues = adjustedBlues[:n]
	for i, b := range blues {
		adjustedBlues[i] = (b - int32(encColorTransformDelta(bestGreenToBlue, uint8(greens[i])))) & 0xff
	}
	bestRedToBlue := findBestMultiplier(adjustedReds, adjustedBlues)

	return Multipliers{
		GreenToRed:  bestGreenToRed,
		GreenToBlue: bestGreenToBlue,
		RedToBlue:   bestRedToBlue,
	}
}

// findBestMultiplier finds the int8 multiplier m that minimizes the total
// absolute residual sum |target[i] - (m * source[i] >> 5)| mod 256.
func findBestMultiplier(source, target []int32) int8 {
	bestM := int8(0)
	bestCost := int64(math.MaxInt64)

	// Coarse search: step by 8.
	for m := int32(-128); m <= 127; m += 8 {
		cost := multiplierCost(int8(m), source, target)
		if cost < bestCost {
			bestCost = cost
			bestM = int8(m)
		}
	}

	// Fine search around the best coarse value.
	coarseM := int32(bestM)
	for m := coarseM - 7; m <= coarseM+7; m++ {
		if m < -128 || m > 127 {
			continue
		}
		cost := multiplierCost(int8(m), source, target)
		if cost < bestCost {
			bestCost = cost
			bestM = int8(m)
		}
	}

	return bestM
}

// multiplierDeltaTable is a precomputed [256][256]int32 table that maps
// (multiplier+128, color) -> (multiplier * int8(color)) >> 5.
// Eliminates per-call LUT rebuild in multiplierCost (~25M iterations saved).
var multiplierDeltaTable [256][256]int32

func init() {
	for m := -128; m <= 127; m++ {
		for c := 0; c < 256; c++ {
			multiplierDeltaTable[m+128][c] = (int32(m) * int32(int8(c))) >> 5
		}
	}
}

// multiplierCost computes the total absolute residual for multiplier m.
func multiplierCost(m int8, source, target []int32) int64 {
	deltaLUT := &multiplierDeltaTable[int(m)+128]

	total := int64(0)
	for i := range source {
		residual := ((target[i] - deltaLUT[uint8(source[i])]) & 0xff)
		// Wrap-aware absolute value: min(residual, 256-residual).
		if residual > 128 {
			residual = 256 - residual
		}
		total += int64(residual)
	}
	return total
}

// ---------------------------------------------------------------------------
// ColorSpaceTransform: cross-color transform selection
// ---------------------------------------------------------------------------

// ColorSpaceTransform selects the best cross-color multipliers per tile and
// applies the forward transform in place. Returns the transform data.
func ColorSpaceTransform(argb []uint32, width, height, bits, quality int) []uint32 {
	tileXSize := VP8LSubSampleSize(width, bits)
	tileYSize := VP8LSubSampleSize(height, bits)
	transformData := make([]uint32, tileXSize*tileYSize)

	tileSize := 1 << bits
	maxTilePixels := tileSize * tileSize
	numTiles := tileXSize * tileYSize

	if numTiles >= 16 {
		// Parallel cross-color transform: tiles don't overlap, so both
		// selection and application can run independently per tile.
		numWorkers := runtime.GOMAXPROCS(0)
		if numWorkers > tileYSize {
			numWorkers = tileYSize
		}
		var wg sync.WaitGroup
		wg.Add(numWorkers)
		rowsPerWorker := (tileYSize + numWorkers - 1) / numWorkers
		for w := 0; w < numWorkers; w++ {
			tyStart := w * rowsPerWorker
			tyEnd := tyStart + rowsPerWorker
			if tyEnd > tileYSize {
				tyEnd = tileYSize
			}
			go func(tyStart, tyEnd int) {
				defer wg.Done()
				scratch := make([]int32, 5*maxTilePixels)
				for ty := tyStart; ty < tyEnd; ty++ {
					for tx := 0; tx < tileXSize; tx++ {
						m := findBestMultipliers(argb, width, height, tx, ty, bits, scratch)
						transformData[ty*tileXSize+tx] = packMultipliers(m)
						applyColorTransformTile(argb, width, height, tx, ty, bits, m)
					}
				}
			}(tyStart, tyEnd)
		}
		wg.Wait()
	} else {
		scratch := make([]int32, 5*maxTilePixels)
		for ty := 0; ty < tileYSize; ty++ {
			for tx := 0; tx < tileXSize; tx++ {
				m := findBestMultipliers(argb, width, height, tx, ty, bits, scratch)
				transformData[ty*tileXSize+tx] = packMultipliers(m)
				applyColorTransformTile(argb, width, height, tx, ty, bits, m)
			}
		}
	}

	return transformData
}

// applyColorTransformTile applies the forward cross-color transform to every
// pixel in the given tile.
func applyColorTransformTile(argb []uint32, width, height, tx, ty, bits int, m Multipliers) {
	tileSize := 1 << bits
	xStart := tx * tileSize
	yStart := ty * tileSize
	xEnd := xStart + tileSize
	if xEnd > width {
		xEnd = width
	}
	yEnd := yStart + tileSize
	if yEnd > height {
		yEnd = height
	}

	for y := yStart; y < yEnd; y++ {
		for x := xStart; x < xEnd; x++ {
			idx := y*width + x
			argb[idx] = applyColorTransformPixel(m, argb[idx])
		}
	}
}

// ---------------------------------------------------------------------------
// Color indexing (palette) build
// ---------------------------------------------------------------------------

// ColorIndexBuild scans all pixels to collect unique colors. If the number of
// unique colors is at most MaxPaletteSize (256), it returns the sorted palette
// and true. Otherwise it returns nil, 0, false.
func ColorIndexBuild(argb []uint32, width, height int) (palette []uint32, paletteSize int, ok bool) {
	colorSet := make(map[uint32]struct{}, MaxPaletteSize+1)
	total := width * height

	for i := 0; i < total; i++ {
		colorSet[argb[i]] = struct{}{}
		if len(colorSet) > MaxPaletteSize {
			return nil, 0, false
		}
	}

	palette = make([]uint32, 0, len(colorSet))
	for c := range colorSet {
		palette = append(palette, c)
	}
	sort.Slice(palette, func(i, j int) bool {
		return palette[i] < palette[j]
	})

	return palette, len(palette), true
}

// ---------------------------------------------------------------------------
// ApplyPaletteTransform: replace pixels with palette indices
// ---------------------------------------------------------------------------

// ApplyPaletteTransform replaces each pixel with its palette index (encoded
// in the green channel) and packs multiple indices per uint32 when the palette
// is small enough.
//
// Packing rules:
//   - palette <= 2 colors:  1-bit indices, 8 pixels per uint32
//   - palette <= 4 colors:  2-bit indices, 4 pixels per uint32
//   - palette <= 16 colors: 4-bit indices, 2 pixels per uint32
//   - otherwise:            8-bit indices, 1 pixel per uint32
func ApplyPaletteTransform(argb []uint32, width, height int, palette []uint32) (packed []uint32, packedWidth int) {
	// Build inverse lookup: color -> index.
	invLookup := make(map[uint32]uint32, len(palette))
	for i, c := range palette {
		invLookup[c] = uint32(i)
	}

	paletteSize := len(palette)

	// Determine packing parameters.
	var bitsPerPixel int
	switch {
	case paletteSize <= 2:
		bitsPerPixel = 1
	case paletteSize <= 4:
		bitsPerPixel = 2
	case paletteSize <= 16:
		bitsPerPixel = 4
	default:
		bitsPerPixel = 8
	}

	pixelsPerWord := 8 / bitsPerPixel
	packedWidth = (width + pixelsPerWord - 1) / pixelsPerWord

	packed = make([]uint32, packedWidth*height)

	for y := 0; y < height; y++ {
		srcRow := y * width
		dstRow := y * packedWidth

		if pixelsPerWord == 1 {
			// No packing: encode each index in the green channel.
			for x := 0; x < width; x++ {
				idx := invLookup[argb[srcRow+x]]
				packed[dstRow+x] = ARGBBlack | (idx << 8)
			}
		} else {
			// Pack multiple indices into each uint32.
			bitMask := uint32((1 << bitsPerPixel) - 1)
			for x := 0; x < width; x++ {
				idx := invLookup[argb[srcRow+x]] & bitMask
				wordPos := x / pixelsPerWord
				bitPos := uint((x % pixelsPerWord) * bitsPerPixel)
				if bitPos == 0 {
					packed[dstRow+wordPos] = ARGBBlack
				}
				// Pack index bits into the green channel byte position.
				packed[dstRow+wordPos] |= idx << (8 + bitPos)
			}
		}
	}

	return packed, packedWidth
}
