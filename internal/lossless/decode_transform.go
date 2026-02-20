package lossless

// decode_transform.go implements reading VP8L transforms from the bitstream
// and applying inverse transforms to decode the final pixel data.
//
// Reference: libwebp/src/dec/vp8l_dec.c (ReadTransform, ApplyInverseTransforms)
// and libwebp/src/dsp/lossless.c (VP8LInverseTransform).

import (
	"runtime"
	"sync"

	"github.com/deepteams/webp/internal/dsp"
)

// minPixelsForParallel is the minimum number of pixels to justify parallel
// processing in row-independent transforms. Below this threshold, the
// goroutine overhead exceeds the benefit.
const minPixelsForParallel = 100000 // ~316x316

// readTransform reads a single transform from the bitstream. Returns the
// (possibly modified) xsize for subsequent transforms.
func (dec *Decoder) readTransform(xsize, ysize int) (int, error) {
	transformType := TransformType(dec.br.ReadBits(2))

	// Each transform type can only appear once.
	if dec.transformsSeen&(1<<transformType) != 0 {
		return 0, ErrBitstream
	}
	dec.transformsSeen |= 1 << transformType

	t := &dec.transforms[dec.nextTransform]
	t.Type = transformType
	t.XSize = xsize
	t.YSize = ysize
	t.Data = nil
	dec.nextTransform++

	switch transformType {
	case PredictorTransform, CrossColorTransform:
		t.Bits = MinTransformBits + int(dec.br.ReadBits(NumTransformBits))
		subW := VP8LSubSampleSize(t.XSize, t.Bits)
		subH := VP8LSubSampleSize(t.YSize, t.Bits)
		data, err := dec.decodeSubImage(subW, subH)
		if err != nil {
			return 0, err
		}
		t.Data = data

	case ColorIndexingTransform:
		numColors := int(dec.br.ReadBits(8)) + 1
		var bits int
		switch {
		case numColors > 16:
			bits = 0
		case numColors > 4:
			bits = 1
		case numColors > 2:
			bits = 2
		default:
			bits = 3
		}
		t.Bits = bits

		palette, err := dec.decodeSubImage(numColors, 1)
		if err != nil {
			return 0, err
		}
		t.Data = expandColorMap(numColors, bits, palette)
		xsize = VP8LSubSampleSize(t.XSize, bits)

	case SubtractGreenTransform:
		// No data to read.
	}

	return xsize, nil
}

// expandColorMap expands a palette and applies delta-coding:
// palette entries are stored as deltas (per byte) from the previous entry.
func expandColorMap(numColors, bits int, palette []uint32) []uint32 {
	finalNumColors := 1 << (8 >> bits)
	newMap := make([]uint32, finalNumColors)
	if len(palette) > 0 {
		newMap[0] = palette[0]
	}

	// Delta-decode per byte component.
	oldBytes := argbSliceToBytes(palette)
	newBytes := argbSliceToBytes(newMap)

	for i := 4; i < 4*numColors; i++ {
		newBytes[i] = (oldBytes[i] + newBytes[i-4]) & 0xff
	}
	for i := 4 * numColors; i < 4*finalNumColors; i++ {
		newBytes[i] = 0
	}

	// Copy back from bytes to uint32.
	bytesToARGBSlice(newBytes, newMap)
	return newMap
}

// argbSliceToBytes reinterprets a []uint32 as []uint8 in little-endian order
// (blue, green, red, alpha for each ARGB value stored as BGRA byte order).
func argbSliceToBytes(s []uint32) []uint8 {
	b := make([]uint8, len(s)*4)
	for i, v := range s {
		b[i*4+0] = uint8(v)
		b[i*4+1] = uint8(v >> 8)
		b[i*4+2] = uint8(v >> 16)
		b[i*4+3] = uint8(v >> 24)
	}
	return b
}

// bytesToARGBSlice converts a []uint8 back into a []uint32 (little-endian).
func bytesToARGBSlice(b []uint8, s []uint32) {
	for i := range s {
		s[i] = uint32(b[i*4+0]) |
			uint32(b[i*4+1])<<8 |
			uint32(b[i*4+2])<<16 |
			uint32(b[i*4+3])<<24
	}
}

// applyInverseTransforms applies all transforms in reverse order and
// returns the final pixel buffer.
func (dec *Decoder) applyInverseTransforms(pixels []uint32) []uint32 {
	numPix := len(pixels)
	rows := pixels
	out := dec.transformBuf
	if out == nil || len(out) < numPix {
		out = make([]uint32, numPix)
	}

	for n := dec.nextTransform - 1; n >= 0; n-- {
		t := &dec.transforms[n]
		inverseTransform(t, 0, t.YSize, rows, out)
		rows = out
	}

	if dec.nextTransform == 0 {
		// No transforms: output is the original pixels.
		return pixels
	}
	return out[:numPix]
}

// inverseTransform applies a single inverse transform to the pixel data.
// Row-independent transforms (SubtractGreen, CrossColor) are parallelized
// for large images.
func inverseTransform(t *Transform, rowStart, rowEnd int, in, out []uint32) {
	width := t.XSize
	numRows := rowEnd - rowStart
	numPixels := numRows * width

	switch t.Type {
	case SubtractGreenTransform:
		addGreenToBlueAndRed(in, numPixels, out)

	case PredictorTransform:
		predictorInverseTransform(t, rowStart, rowEnd, in, out)

	case CrossColorTransform:
		numWorkers := runtime.GOMAXPROCS(0)
		if numWorkers > 1 && numPixels >= minPixelsForParallel {
			colorSpaceInverseTransformParallel(t, rowStart, rowEnd, in, out, numWorkers)
		} else {
			colorSpaceInverseTransform(t, rowStart, rowEnd, in, out)
		}

	case ColorIndexingTransform:
		colorIndexInverseTransform(t, rowStart, rowEnd, in, out)
	}
}

// addGreenToBlueAndRed applies the inverse of the subtract-green transform:
// adds the green channel value back to blue and red channels.
// Uses SIMD-accelerated DSP function (ARM64 NEON / AMD64 SSE2).
func addGreenToBlueAndRed(src []uint32, numPixels int, dst []uint32) {
	if numPixels <= 0 {
		return
	}
	// Copy src to dst first if different buffers, then apply in-place SIMD.
	if len(src) > 0 && len(dst) > 0 && &src[0] != &dst[0] {
		copy(dst[:numPixels], src[:numPixels])
	}
	dsp.AddGreenToBlueAndRed(dst, numPixels)
}

// predictorInverseTransform applies the inverse predictor transform.
// 14 spatial predictor modes are used, tiled across the image.
// The prediction mode switch is moved outside the inner loop so each tile
// uses a specialized loop without per-pixel branch overhead.
// Row slices are pre-computed for BCE elimination.
func predictorInverseTransform(t *Transform, yStart, yEnd int, in, out []uint32) {
	width := t.XSize
	inOff := 0
	outOff := 0

	if yStart == 0 {
		// First row: pixel 0 uses predictor 0 (black + residual = residual).
		out[outOff] = addPixels(in[inOff], 0xff000000) // predictor 0 = ARGB black
		// Rest of first row uses predictor 1 (left pixel).
		inRow := in[inOff : inOff+width]
		outRow := out[outOff : outOff+width]
		for x := 1; x < width; x++ {
			outRow[x] = addPixels(inRow[x], outRow[x-1])
		}
		inOff += width
		outOff += width
		yStart = 1
	}

	tileWidth := 1 << t.Bits
	tileMask := tileWidth - 1
	tilesPerRow := VP8LSubSampleSize(width, t.Bits)
	tData := t.Data

	for y := yStart; y < yEnd; y++ {
		predModeRow := (y >> t.Bits) * tilesPerRow

		// Pre-slice rows for the compiler to eliminate bounds checks.
		inRow := in[inOff : inOff+width]
		outRow := out[outOff : outOff+width]
		topRow := out[outOff-width : outOff]

		// First pixel of the row: predictor mode 2 (top pixel).
		outRow[0] = addPixels(inRow[0], topRow[0])

		x := 1
		for x < width {
			predMode := int((tData[predModeRow+(x>>t.Bits)] >> 8) & 0xf)
			xEnd := (x & ^tileMask) + tileWidth
			if xEnd > width {
				xEnd = width
			}

			// Specialized inner loops per prediction mode avoid a 14-case
			// switch + function call per pixel.
			switch predMode {
			case 0: // Black
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], 0xff000000)
				}
			case 1: // Left
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], outRow[x-1])
				}
			case 2: // Top
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], topRow[x])
				}
			case 3: // Top-Right
				safeEnd := xEnd
				if safeEnd >= width {
					safeEnd = width - 1
				}
				for ; x < safeEnd; x++ {
					outRow[x] = addPixels(inRow[x], topRow[x+1])
				}
				if x < xEnd { // last pixel of row
					outRow[x] = addPixels(inRow[x], outRow[0])
					x++
				}
			case 4: // Top-Left
				topLeftRow := out[outOff-width-1+1 : outOff]
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], topLeftRow[x-1])
				}
			case 5: // Average2(Average2(L,TR), T)
				safeEnd := xEnd
				if safeEnd >= width {
					safeEnd = width - 1
				}
				for ; x < safeEnd; x++ {
					outRow[x] = addPixels(inRow[x], average2(average2(outRow[x-1], topRow[x+1]), topRow[x]))
				}
				if x < xEnd {
					outRow[x] = addPixels(inRow[x], average2(average2(outRow[x-1], outRow[0]), topRow[x]))
					x++
				}
			case 6: // Average2(L, TL)
				topLeftRow := out[outOff-width-1+1 : outOff]
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], average2(outRow[x-1], topLeftRow[x-1]))
				}
			case 7: // Average2(L, T)
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], average2(outRow[x-1], topRow[x]))
				}
			case 8: // Average2(TL, T)
				topLeftRow := out[outOff-width-1+1 : outOff]
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], average2(topLeftRow[x-1], topRow[x]))
				}
			case 9: // Average2(T, TR)
				safeEnd := xEnd
				if safeEnd >= width {
					safeEnd = width - 1
				}
				for ; x < safeEnd; x++ {
					outRow[x] = addPixels(inRow[x], average2(topRow[x], topRow[x+1]))
				}
				if x < xEnd {
					outRow[x] = addPixels(inRow[x], average2(topRow[x], outRow[0]))
					x++
				}
			case 10: // Average2(Average2(L, TL), Average2(T, TR))
				topLeftRow := out[outOff-width-1+1 : outOff]
				safeEnd := xEnd
				if safeEnd >= width {
					safeEnd = width - 1
				}
				for ; x < safeEnd; x++ {
					outRow[x] = addPixels(inRow[x], average2(average2(outRow[x-1], topLeftRow[x-1]), average2(topRow[x], topRow[x+1])))
				}
				if x < xEnd {
					outRow[x] = addPixels(inRow[x], average2(average2(outRow[x-1], topLeftRow[x-1]), average2(topRow[x], outRow[0])))
					x++
				}
			case 11: // Select
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], selectPredictor(outRow[x-1], topRow[x], out[outOff+x-1-width]))
				}
			case 12: // Clamped add-subtract full
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], clampedAddSubtractFull(outRow[x-1], topRow[x], out[outOff+x-1-width]))
				}
			case 13: // Clamped add-subtract half
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], clampedAddSubtractHalf(average2(outRow[x-1], topRow[x]), out[outOff+x-1-width]))
				}
			default: // Fallback (same as 0)
				for ; x < xEnd; x++ {
					outRow[x] = addPixels(inRow[x], 0xff000000)
				}
			}
		}
		inOff += width
		outOff += width
	}
}

// addPixels adds two ARGB pixels per-component mod 256.
func addPixels(a, b uint32) uint32 {
	alphaAndGreen := (a & 0xff00ff00) + (b & 0xff00ff00)
	redAndBlue := (a & 0x00ff00ff) + (b & 0x00ff00ff)
	return (alphaAndGreen & 0xff00ff00) | (redAndBlue & 0x00ff00ff)
}

// average2 computes per-component average of two ARGB pixels.
func average2(a, b uint32) uint32 {
	return (((a ^ b) & 0xfefefefe) >> 1) + (a & b)
}

// selectPredictor implements the VP8L select predictor.
// Unrolled for all 4 ARGB channels to avoid loop overhead.
func selectPredictor(left, top, topLeft uint32) uint32 {
	ac0 := int32(top&0xff) - int32(topLeft&0xff)
	bc0 := int32(left&0xff) - int32(topLeft&0xff)
	if ac0 < 0 {
		ac0 = -ac0
	}
	if bc0 < 0 {
		bc0 = -bc0
	}
	ac1 := int32((top>>8)&0xff) - int32((topLeft>>8)&0xff)
	bc1 := int32((left>>8)&0xff) - int32((topLeft>>8)&0xff)
	if ac1 < 0 {
		ac1 = -ac1
	}
	if bc1 < 0 {
		bc1 = -bc1
	}
	ac2 := int32((top>>16)&0xff) - int32((topLeft>>16)&0xff)
	bc2 := int32((left>>16)&0xff) - int32((topLeft>>16)&0xff)
	if ac2 < 0 {
		ac2 = -ac2
	}
	if bc2 < 0 {
		bc2 = -bc2
	}
	ac3 := int32(top>>24) - int32(topLeft>>24)
	bc3 := int32(left>>24) - int32(topLeft>>24)
	if ac3 < 0 {
		ac3 = -ac3
	}
	if bc3 < 0 {
		bc3 = -bc3
	}
	pa := (ac0 - bc0) + (ac1 - bc1) + (ac2 - bc2) + (ac3 - bc3)
	if pa <= 0 {
		return top
	}
	return left
}

// clampedAddSubtractFull computes L + T - TL per component, clamped to [0,255].
func clampedAddSubtractFull(a, b, c uint32) uint32 {
	var result uint32
	for shift := uint(0); shift < 32; shift += 8 {
		va := int32((a >> shift) & 0xff)
		vb := int32((b >> shift) & 0xff)
		vc := int32((c >> shift) & 0xff)
		v := va + vb - vc
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		result |= uint32(v) << shift
	}
	return result
}

// clampedAddSubtractHalf computes average(a, b) + (average(a, b) - c) / 2
// per component, clamped.
func clampedAddSubtractHalf(avg, c uint32) uint32 {
	var result uint32
	for shift := uint(0); shift < 32; shift += 8 {
		va := int32((avg >> shift) & 0xff)
		vc := int32((c >> shift) & 0xff)
		v := va + (va-vc)/2
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		result |= uint32(v) << shift
	}
	return result
}

// colorSpaceInverseTransform applies the inverse cross-color transform.
// The multiplier extraction and transform arithmetic are inlined into the
// tile loop to avoid per-pixel function call overhead.
func colorSpaceInverseTransform(t *Transform, yStart, yEnd int, src, dst []uint32) {
	width := t.XSize
	tileWidth := 1 << t.Bits
	tileMask := tileWidth - 1
	safeWidth := width & ^tileMask
	remainingWidth := width - safeWidth
	tilesPerRow := VP8LSubSampleSize(width, t.Bits)
	tData := t.Data

	srcOff := yStart * width
	dstOff := yStart * width

	for y := yStart; y < yEnd; y++ {
		predRow := (y >> t.Bits) * tilesPerRow
		predIdx := 0

		x := 0
		for x < safeWidth {
			// Extract multipliers once per tile (int32 for inner loop).
			colorCode := tData[predRow+predIdx]
			g2r := int32(int8(colorCode))
			g2b := int32(int8(colorCode >> 8))
			r2b := int32(int8(colorCode >> 16))
			predIdx++

			srcSlice := src[srcOff+x:]
			dstSlice := dst[dstOff+x:]
			_ = srcSlice[tileWidth-1] // BCE
			_ = dstSlice[tileWidth-1] // BCE
			for i := 0; i < tileWidth; i++ {
				argb := srcSlice[i]
				green := int32(int8(argb >> 8))
				red := int32((argb >> 16) & 0xff)
				blue := int32(argb & 0xff)

				red += (g2r * green) >> 5
				red &= 0xff
				blue += (g2b * green) >> 5
				blue += (r2b * int32(int8(red))) >> 5
				blue &= 0xff

				dstSlice[i] = (argb & 0xff00ff00) | (uint32(red) << 16) | uint32(blue)
			}
			x += tileWidth
		}
		if x < width {
			colorCode := tData[predRow+predIdx]
			g2r := int32(int8(colorCode))
			g2b := int32(int8(colorCode >> 8))
			r2b := int32(int8(colorCode >> 16))

			srcSlice := src[srcOff+x:]
			dstSlice := dst[dstOff+x:]
			for i := 0; i < remainingWidth; i++ {
				argb := srcSlice[i]
				green := int32(int8(argb >> 8))
				red := int32((argb >> 16) & 0xff)
				blue := int32(argb & 0xff)

				red += (g2r * green) >> 5
				red &= 0xff
				blue += (g2b * green) >> 5
				blue += (r2b * int32(int8(red))) >> 5
				blue &= 0xff

				dstSlice[i] = (argb & 0xff00ff00) | (uint32(red) << 16) | uint32(blue)
			}
		}

		srcOff += width
		dstOff += width
	}
}

// colorSpaceInverseTransformParallel splits the cross-color transform across
// multiple goroutines. Each worker processes a contiguous range of rows.
// Safe because there are no row-to-row dependencies in this transform.
func colorSpaceInverseTransformParallel(t *Transform, yStart, yEnd int, src, dst []uint32, numWorkers int) {
	numRows := yEnd - yStart
	if numWorkers > numRows {
		numWorkers = numRows
	}
	rowsPerWorker := numRows / numWorkers
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for w := 0; w < numWorkers; w++ {
		ys := yStart + w*rowsPerWorker
		ye := ys + rowsPerWorker
		if w == numWorkers-1 {
			ye = yEnd
		}
		go func(ys, ye int) {
			colorSpaceInverseTransform(t, ys, ye, src, dst)
			wg.Done()
		}(ys, ye)
	}
	wg.Wait()
}

// colorIndexInverseTransform applies the inverse color-indexing (palette)
// transform, unpacking sub-byte pixels as needed.
func colorIndexInverseTransform(t *Transform, yStart, yEnd int, src, dst []uint32) {
	width := t.XSize
	colorMap := t.Data
	bitsPerPixel := 8 >> t.Bits

	if bitsPerPixel < 8 {
		pixelsPerByte := 1 << t.Bits
		countMask := pixelsPerByte - 1
		bitMask := uint32((1 << bitsPerPixel) - 1)

		srcOff := 0
		dstOff := 0
		for y := yStart; y < yEnd; y++ {
			var packedPixels uint32
			for x := 0; x < width; x++ {
				if (x & countMask) == 0 {
					packedPixels = getARGBIndex(src[srcOff])
					srcOff++
				}
				idx := packedPixels & bitMask
				if int(idx) < len(colorMap) {
					dst[dstOff] = colorMap[idx]
				}
				dstOff++
				packedPixels >>= bitsPerPixel
			}
		}
	} else {
		// 1:1 mapping (8 bits per pixel, no sub-byte packing).
		srcOff := 0
		dstOff := 0
		for y := yStart; y < yEnd; y++ {
			for x := 0; x < width; x++ {
				idx := getARGBIndex(src[srcOff])
				srcOff++
				if int(idx) < len(colorMap) {
					dst[dstOff] = colorMap[idx]
				}
				dstOff++
			}
		}
	}
}

// getARGBIndex extracts the green channel (byte 1) as the palette index.
func getARGBIndex(argb uint32) uint32 {
	return (argb >> 8) & 0xff
}
