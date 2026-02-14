package lossless

// decode_transform.go implements reading VP8L transforms from the bitstream
// and applying inverse transforms to decode the final pixel data.
//
// Reference: libwebp/src/dec/vp8l_dec.c (ReadTransform, ApplyInverseTransforms)
// and libwebp/src/dsp/lossless.c (VP8LInverseTransform).

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
func inverseTransform(t *Transform, rowStart, rowEnd int, in, out []uint32) {
	width := t.XSize
	switch t.Type {
	case SubtractGreenTransform:
		addGreenToBlueAndRed(in, (rowEnd-rowStart)*width, out)

	case PredictorTransform:
		predictorInverseTransform(t, rowStart, rowEnd, in, out)

	case CrossColorTransform:
		colorSpaceInverseTransform(t, rowStart, rowEnd, in, out)

	case ColorIndexingTransform:
		colorIndexInverseTransform(t, rowStart, rowEnd, in, out)
	}
}

// addGreenToBlueAndRed applies the inverse of the subtract-green transform:
// adds the green channel value back to blue and red channels.
func addGreenToBlueAndRed(src []uint32, numPixels int, dst []uint32) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		green := (argb >> 8) & 0xff
		redBlue := argb & 0x00ff00ff
		redBlue += (green << 16) | green
		redBlue &= 0x00ff00ff
		dst[i] = (argb & 0xff00ff00) | redBlue
	}
}

// predictorInverseTransform applies the inverse predictor transform.
// 14 spatial predictor modes are used, tiled across the image.
func predictorInverseTransform(t *Transform, yStart, yEnd int, in, out []uint32) {
	width := t.XSize
	inOff := 0
	outOff := 0

	if yStart == 0 {
		// First row: pixel 0 uses predictor 0 (black + residual = residual).
		out[outOff] = addPixels(in[inOff], 0xff000000) // predictor 0 = ARGB black
		// Rest of first row uses predictor 1 (left pixel).
		for x := 1; x < width; x++ {
			out[outOff+x] = addPixels(in[inOff+x], out[outOff+x-1])
		}
		inOff += width
		outOff += width
		yStart = 1
	}

	tileWidth := 1 << t.Bits
	tileMask := tileWidth - 1
	tilesPerRow := VP8LSubSampleSize(width, t.Bits)

	for y := yStart; y < yEnd; y++ {
		predModeRow := (y >> t.Bits) * tilesPerRow

		// First pixel of the row: predictor mode 2 (top pixel).
		out[outOff] = addPixels(in[inOff], out[outOff-width])

		x := 1
		for x < width {
			predMode := int((t.Data[predModeRow+(x>>t.Bits)] >> 8) & 0xf)
			xEnd := (x & ^tileMask) + tileWidth
			if xEnd > width {
				xEnd = width
			}
			for ; x < xEnd; x++ {
				var topRight uint32
				if x < width-1 {
					topRight = out[outOff+x+1-width]
				} else {
					// C reads upper[width] which equals out[0] of the current row.
					topRight = out[outOff]
				}
				pred := predict(predMode, out[outOff+x-1], out[outOff+x-width], out[outOff+x-1-width], topRight)
				out[outOff+x] = addPixels(in[inOff+x], pred)
			}
		}
		inOff += width
		outOff += width
	}
}

// predict returns the prediction for pixel at position (col > 0, row > 0)
// given the predictor mode.
func predict(mode int, left, top, topLeft, topRight uint32) uint32 {
	switch mode {
	case 0: // Black
		return 0xff000000
	case 1: // Left
		return left
	case 2: // Top
		return top
	case 3: // Top-Right
		return topRight
	case 4: // Top-Left
		return topLeft
	case 5: // Average2(Average2(L,TR), T)
		return average2(average2(left, topRight), top)
	case 6: // Average2(L, TL)
		return average2(left, topLeft)
	case 7: // Average2(L, T)
		return average2(left, top)
	case 8: // Average2(TL, T)
		return average2(topLeft, top)
	case 9: // Average2(T, TR)
		return average2(top, topRight)
	case 10: // Average2(Average2(L, TL), Average2(T, TR))
		return average2(average2(left, topLeft), average2(top, topRight))
	case 11: // Select
		return selectPredictor(left, top, topLeft)
	case 12: // Clamped add-subtract full
		return clampedAddSubtractFull(left, top, topLeft)
	case 13: // Clamped add-subtract half
		return clampedAddSubtractHalf(average2(left, top), topLeft)
	default:
		return 0xff000000
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
func selectPredictor(left, top, topLeft uint32) uint32 {
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
func colorSpaceInverseTransform(t *Transform, yStart, yEnd int, src, dst []uint32) {
	width := t.XSize
	tileWidth := 1 << t.Bits
	tileMask := tileWidth - 1
	safeWidth := width & ^tileMask
	remainingWidth := width - safeWidth
	tilesPerRow := VP8LSubSampleSize(width, t.Bits)

	srcOff := 0
	dstOff := 0

	for y := yStart; y < yEnd; y++ {
		predRow := (y >> t.Bits) * tilesPerRow
		predIdx := 0

		x := 0
		for x < safeWidth {
			m := colorCodeToMultipliers(t.Data[predRow+predIdx])
			predIdx++
			for i := 0; i < tileWidth; i++ {
				dst[dstOff+x+i] = transformColorInverse(m, src[srcOff+x+i])
			}
			x += tileWidth
		}
		if x < width {
			m := colorCodeToMultipliers(t.Data[predRow+predIdx])
			for i := 0; i < remainingWidth; i++ {
				dst[dstOff+x+i] = transformColorInverse(m, src[srcOff+x+i])
			}
		}

		srcOff += width
		dstOff += width
	}
}

type colorMultipliers struct {
	greenToRed  uint8
	greenToBlue uint8
	redToBlue   uint8
}

func colorCodeToMultipliers(colorCode uint32) colorMultipliers {
	return colorMultipliers{
		greenToRed:  uint8(colorCode),
		greenToBlue: uint8(colorCode >> 8),
		redToBlue:   uint8(colorCode >> 16),
	}
}

func colorTransformDelta(colorPred int8, clr int8) int32 {
	return (int32(colorPred) * int32(clr)) >> 5
}

func transformColorInverse(m colorMultipliers, argb uint32) uint32 {
	green := int8(argb >> 8)
	red := int32(argb>>16) & 0xff
	blue := int32(argb) & 0xff

	newRed := red + int32(colorTransformDelta(int8(m.greenToRed), green))
	newRed &= 0xff
	newBlue := blue + int32(colorTransformDelta(int8(m.greenToBlue), green))
	newBlue += int32(colorTransformDelta(int8(m.redToBlue), int8(newRed)))
	newBlue &= 0xff

	return (argb & 0xff00ff00) | (uint32(newRed) << 16) | uint32(newBlue)
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
