package dsp

// VP8L color transforms (batch versions) from lossless.c.
// These operate on slices of ARGB uint32 pixels.

// AddGreenToBlueAndRed adds the green channel to both the red and blue channels
// for each pixel in the row. This is the inverse of the SubtractGreen transform.
func AddGreenToBlueAndRed(argb []uint32, numPixels int) {
	for i := 0; i < numPixels; i++ {
		p := argb[i]
		green := (p >> 8) & 0xff
		redBlue := (p & 0x00ff00ff) + (green * 0x00010001)
		redBlue &= 0x00ff00ff
		argb[i] = (p & 0xff00ff00) | redBlue
	}
}

// SubtractGreen subtracts the green channel from both the red and blue channels
// for each pixel. This is the forward SubtractGreen transform used in encoding.
func SubtractGreen(argb []uint32, numPixels int) {
	for i := 0; i < numPixels; i++ {
		p := argb[i]
		green := (p >> 8) & 0xff
		r := ((p >> 16) & 0xff) - green
		b := (p & 0xff) - green
		argb[i] = (p & 0xff00ff00) | ((r & 0xff) << 16) | (b & 0xff)
	}
}

// TransformColorInverse applies the inverse color-space transform to a row
// of pixels using the given multipliers.
func TransformColorInverse(m *Multipliers, src []uint32, numPixels int, dst []uint32) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		green := int32((argb >> 8) & 0xff)
		red := int32((argb >> 16) & 0xff)
		blue := int32(argb & 0xff)

		// Apply inverse transform: add back the green-based prediction.
		red += colorTransformDelta(int8(m.GreenToRed), green)
		red &= 0xff
		blue += colorTransformDelta(int8(m.GreenToBlue), green)
		blue += colorTransformDelta(int8(m.RedToBlue), red)
		blue &= 0xff

		dst[i] = (argb & 0xff00ff00) | (uint32(red) << 16) | uint32(blue)
	}
}

// TransformColor applies the forward color-space transform to a row of pixels.
func TransformColor(m *Multipliers, src []uint32, numPixels int, dst []uint32) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		green := int32((argb >> 8) & 0xff)
		red := int32((argb >> 16) & 0xff)
		blue := int32(argb & 0xff)

		newRed := red - colorTransformDelta(int8(m.GreenToRed), green)
		newRed &= 0xff
		newBlue := blue - colorTransformDelta(int8(m.GreenToBlue), green)
		newBlue -= colorTransformDelta(int8(m.RedToBlue), red)
		newBlue &= 0xff

		dst[i] = (argb & 0xff00ff00) | (uint32(newRed) << 16) | uint32(newBlue)
	}
}

// colorTransformDelta computes (multiplier * int8(value)) >> 5, sign-extending.
// This matches the C ColorTransformDelta(int8_t color_pred, int8_t color) from
// lossless.c:278 where both arguments are int8_t.
func colorTransformDelta(multiplier int8, value int32) int32 {
	return (int32(multiplier) * int32(int8(value))) >> 5
}

// ColorIndexInverseTransform applies the inverse color-indexing transform,
// mapping indices to palette entries.
func ColorIndexInverseTransform(palette []uint32, src []uint32, numPixels int, dst []uint32) {
	paletteLen := len(palette)
	if paletteLen == 0 {
		return
	}
	for i := 0; i < numPixels; i++ {
		idx := int(src[i] & 0xff) // green channel holds the index
		if idx >= paletteLen {
			idx = 0
		}
		dst[i] = palette[idx]
	}
}

// BundleColorMap packs multiple palette indices (uint8) into uint32 pixels.
// This matches VP8LBundleColorMap_C from libwebp.
//
// xbits controls the packing density:
//   - xbits=0: 8-bit indices, 1 index per uint32 (no packing)
//   - xbits=1: 4-bit indices, 2 indices per uint32
//   - xbits=2: 2-bit indices, 4 indices per uint32
//   - xbits=3: 1-bit indices, 8 indices per uint32
//
// Each output uint32 has alpha=0xff and indices packed in the green channel byte
// at bit offsets 8 + bit_depth*0, 8 + bit_depth*1, etc.
func BundleColorMap(row []uint8, width int, xbits int, dst []uint32) {
	if xbits > 0 {
		bitDepth := uint(1 << (3 - uint(xbits)))
		mask := (1 << uint(xbits)) - 1
		var code uint32
		for x := 0; x < width; x++ {
			xsub := x & mask
			if xsub == 0 {
				code = 0xff000000
			}
			code |= uint32(row[x]) << (8 + bitDepth*uint(xsub))
			dst[x>>uint(xbits)] = code
		}
	} else {
		for x := 0; x < width; x++ {
			dst[x] = 0xff000000 | (uint32(row[x]) << 8)
		}
	}
}

// --- BGRA-to-* conversion functions ---
// In WebP lossless, pixels are stored as ARGB uint32 in native byte order:
//   bits [31:24] = A, [23:16] = R, [15:8] = G, [7:0] = B
// The "BGRA" naming in libwebp refers to the internal storage format.

// ConvertBGRAToRGBA converts ARGB uint32 pixels to interleaved RGBA bytes.
func ConvertBGRAToRGBA(src []uint32, numPixels int, dst []byte) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		off := i * 4
		dst[off+0] = uint8(argb >> 16)  // R
		dst[off+1] = uint8(argb >> 8)   // G
		dst[off+2] = uint8(argb)        // B
		dst[off+3] = uint8(argb >> 24)  // A
	}
}

// ConvertBGRAToRGB converts ARGB uint32 pixels to interleaved RGB bytes.
func ConvertBGRAToRGB(src []uint32, numPixels int, dst []byte) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		off := i * 3
		dst[off+0] = uint8(argb >> 16) // R
		dst[off+1] = uint8(argb >> 8)  // G
		dst[off+2] = uint8(argb)       // B
	}
}

// ConvertBGRAToBGR converts ARGB uint32 pixels to interleaved BGR bytes.
func ConvertBGRAToBGR(src []uint32, numPixels int, dst []byte) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		off := i * 3
		dst[off+0] = uint8(argb)        // B
		dst[off+1] = uint8(argb >> 8)   // G
		dst[off+2] = uint8(argb >> 16)  // R
	}
}

// ConvertBGRAToRGBA4444 converts ARGB uint32 pixels to RGBA4444 (2 bytes each).
// Each component is reduced from 8 to 4 bits.
func ConvertBGRAToRGBA4444(src []uint32, numPixels int, dst []byte) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		off := i * 2
		rg := uint8(((argb >> 16) & 0xf0) | ((argb >> 12) & 0x0f))
		ba := uint8(((argb >> 0) & 0xf0) | ((argb >> 28) & 0x0f))
		dst[off+0] = rg
		dst[off+1] = ba
	}
}

// ConvertBGRAToRGB565 converts ARGB uint32 pixels to RGB565 (2 bytes each).
func ConvertBGRAToRGB565(src []uint32, numPixels int, dst []byte) {
	for i := 0; i < numPixels; i++ {
		argb := src[i]
		off := i * 2
		rg := uint8(((argb >> 16) & 0xf8) | ((argb >> 13) & 0x07))
		gb := uint8(((argb >> 5) & 0xe0) | ((argb >> 3) & 0x1f))
		dst[off+0] = rg
		dst[off+1] = gb
	}
}

// MapColor32b maps ARGB pixels through a color palette.
// For each source pixel, the green channel (bits 15:8) is used as an index
// into the colorMap, and the output pixel is the palette entry.
func MapColor32b(src []uint32, colorMap []uint32, dst []uint32, yStart, yEnd, width int) {
	si := 0
	di := 0
	for y := yStart; y < yEnd; y++ {
		for x := 0; x < width; x++ {
			idx := (src[si] >> 8) & 0xff // VP8GetARGBIndex
			dst[di] = colorMap[idx]       // VP8GetARGBValue
			si++
			di++
		}
	}
}

// MapColor8b maps alpha (uint8) values through a color palette.
// For each source byte, it is used as a direct index into the colorMap,
// and the green channel of the palette entry is written to the output.
func MapColor8b(src []uint8, colorMap []uint32, dst []uint8, yStart, yEnd, width int) {
	si := 0
	di := 0
	for y := yStart; y < yEnd; y++ {
		for x := 0; x < width; x++ {
			idx := src[si]                       // VP8GetAlphaIndex
			dst[di] = uint8(colorMap[idx] >> 8)  // VP8GetAlphaValue
			si++
			di++
		}
	}
}
