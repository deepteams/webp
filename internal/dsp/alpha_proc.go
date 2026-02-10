package dsp

// Alpha channel processing from libwebp alpha_processing.c.
// Provides premultiply, dispatch, and extraction routines for the alpha plane.

const (
	alphaMFIX    = 24                  // 24-bit fixed-point arithmetic
	alphaHALF    = 1 << (alphaMFIX-1) // rounding constant = 1<<23
	alphaKINV255 = (1 << alphaMFIX) / 255 // fixed-point reciprocal of 255
)

// alphaMult computes (x * mult + HALF) >> MFIX, matching libwebp's Mult().
func alphaMult(x uint8, mult uint32) uint32 {
	return (uint32(x)*mult + alphaHALF) >> alphaMFIX
}

// alphaGetScale returns the fixed-point scale factor for premultiply or
// inverse premultiply, matching libwebp's GetScale().
func alphaGetScale(a uint32, inverse bool) uint32 {
	if inverse {
		return (255 << alphaMFIX) / a
	}
	return a * alphaKINV255
}

// MultARGBRow premultiplies the RGB channels of each ARGB pixel in row
// by its alpha channel. row is a []uint32 of ARGB pixels.
func MultARGBRow(row []uint32, width int, inverse bool) {
	for i := 0; i < width; i++ {
		argb := row[i]
		if argb >= 0xff000000 { // alpha == 255
			continue
		}
		if argb <= 0x00ffffff { // alpha == 0
			row[i] = 0
			continue
		}
		alpha := (argb >> 24) & 0xff
		scale := alphaGetScale(alpha, inverse)
		out := argb & 0xff000000
		out |= alphaMult(uint8(argb>>0), scale) << 0
		out |= alphaMult(uint8(argb>>8), scale) << 8
		out |= alphaMult(uint8(argb>>16), scale) << 16
		row[i] = out
	}
}

// MultRow premultiplies a row of interleaved RGBA bytes (4 bytes per pixel).
func MultRow(row []byte, width, stride int, inverse bool) {
	for i := 0; i < width; i++ {
		off := i * stride
		a := uint32(row[off+3])
		if a == 255 {
			continue
		}
		if a == 0 {
			row[off+0] = 0
			row[off+1] = 0
			row[off+2] = 0
			continue
		}
		scale := alphaGetScale(a, inverse)
		row[off+0] = uint8(alphaMult(row[off+0], scale))
		row[off+1] = uint8(alphaMult(row[off+1], scale))
		row[off+2] = uint8(alphaMult(row[off+2], scale))
	}
}

// ApplyAlphaMultiply premultiplies (or un-premultiplies) an RGBA or ARGB image
// buffer in place. The image has width x height pixels, with the given stride
// in bytes. Each pixel is 4 bytes. If alphaFirst is true, alpha is at offset 0
// (ARGB layout); otherwise alpha is at offset 3 (RGBA layout).
// Matches C ApplyAlphaMultiply_C (alpha_processing.c:228-245).
func ApplyAlphaMultiply(rgba []byte, alphaFirst bool, width, height, stride int, inverse bool) {
	for y := 0; y < height; y++ {
		row := rgba[y*stride:]
		var rgbOff, alphaOff int
		if alphaFirst {
			rgbOff = 1
			alphaOff = 0
		} else {
			rgbOff = 0
			alphaOff = 3
		}
		for i := 0; i < width; i++ {
			off := i * 4
			a := uint32(row[off+alphaOff])
			if a == 255 {
				continue
			}
			if a == 0 {
				row[off+rgbOff+0] = 0
				row[off+rgbOff+1] = 0
				row[off+rgbOff+2] = 0
				continue
			}
			scale := alphaGetScale(a, inverse)
			row[off+rgbOff+0] = uint8(alphaMult(row[off+rgbOff+0], scale))
			row[off+rgbOff+1] = uint8(alphaMult(row[off+rgbOff+1], scale))
			row[off+rgbOff+2] = uint8(alphaMult(row[off+rgbOff+2], scale))
		}
	}
}

// ApplyAlphaMultiply4444 premultiplies a buffer of RGBA4444 pixels (2 bytes each).
func ApplyAlphaMultiply4444(data []byte, width, height, stride int) {
	for y := 0; y < height; y++ {
		row := data[y*stride:]
		for x := 0; x < width; x++ {
			off := x * 2
			rg := row[off]
			ba := row[off+1]
			a := ba & 0x0f
			if a == 0x0f {
				continue
			}
			if a == 0 {
				row[off] = 0
				row[off+1] = 0
				continue
			}
			r := (rg >> 4) & 0x0f
			g := rg & 0x0f
			b := (ba >> 4) & 0x0f
			r = (r * a + 7) / 15
			g = (g * a + 7) / 15
			b = (b * a + 7) / 15
			row[off] = (r << 4) | g
			row[off+1] = (b << 4) | a
		}
	}
}

// DispatchAlpha disperses the alpha plane into a 4-channel RGBA buffer.
// alpha is a width-byte-per-row alpha plane; dst is the RGBA output with
// the given stride. alphaOff is the byte offset within each 4-byte pixel
// where alpha should be placed (typically 3 for RGBA, 0 for ARGB).
// Returns true if any alpha value is not 0xff (i.e., the image has transparency).
// Matches C DispatchAlpha_C (alpha_processing.c:297-314).
func DispatchAlpha(alpha []byte, alphaStride, width, height int,
	dst []byte, dstStride, alphaOff int) bool {
	alphaMask := uint32(0xff)
	for y := 0; y < height; y++ {
		aRow := alpha[y*alphaStride:]
		dRow := dst[y*dstStride:]
		for x := 0; x < width; x++ {
			alphaValue := uint32(aRow[x])
			dRow[x*4+alphaOff] = byte(alphaValue)
			alphaMask &= alphaValue
		}
	}
	return alphaMask != 0xff
}

// ExtractAlpha extracts the alpha channel from a 4-byte-per-pixel RGBA buffer
// into a separate alpha plane. Returns 1 if all alpha values are 0xff (fully
// opaque), 0 otherwise. Matches C ExtractAlpha_C (alpha_processing.c:330-346).
func ExtractAlpha(src []byte, srcStride, width, height int,
	alpha []byte, alphaStride, alphaOff int) int {
	alphaMask := uint8(0xff)
	for y := 0; y < height; y++ {
		sRow := src[y*srcStride:]
		aRow := alpha[y*alphaStride:]
		for x := 0; x < width; x++ {
			a := sRow[x*4+alphaOff]
			aRow[x] = a
			alphaMask &= a
		}
	}
	if alphaMask == 0xff {
		return 1
	}
	return 0
}

// HasAlpha8b checks whether any byte in a row of alpha values is not 0xff.
// Returns true if transparency is found.
func HasAlpha8b(src []byte, length int) bool {
	for i := 0; i < length; i++ {
		if src[i] != 0xff {
			return true
		}
	}
	return false
}

// HasAlpha32b checks whether any alpha byte (first of every 4 bytes) is not 0xff.
// Returns true if transparency is found.
func HasAlpha32b(src []byte, length int) bool {
	for i := 0; i < length; i++ {
		if src[i*4] != 0xff {
			return true
		}
	}
	return false
}

// AlphaReplace replaces fully-transparent ARGB pixels with the given color.
func AlphaReplace(argb []uint32, length int, color uint32) {
	for i := 0; i < length; i++ {
		if (argb[i] >> 24) == 0 {
			argb[i] = color
		}
	}
}

// DispatchAlphaToGreen writes alpha values into the green channel of ARGB pixels.
// Other channels are zeroed.
func DispatchAlphaToGreen(alpha []byte, alphaStride, width, height int,
	dst []uint32, dstStride int) {
	for y := 0; y < height; y++ {
		aRow := alpha[y*alphaStride:]
		dRow := dst[y*dstStride:]
		for x := 0; x < width; x++ {
			dRow[x] = uint32(aRow[x]) << 8
		}
	}
}

// ExtractGreen extracts the green channel from ARGB pixels into a byte slice.
func ExtractGreen(argb []uint32, alpha []byte, size int) {
	for i := 0; i < size; i++ {
		alpha[i] = uint8(argb[i] >> 8)
	}
}

// PackRGB packs separate R, G, B byte arrays (with given step between pixels)
// into packed ARGB uint32 pixels with alpha=0xff.
func PackRGB(r, g, b []byte, length, step int, out []uint32) {
	offset := 0
	for i := 0; i < length; i++ {
		out[i] = 0xff000000 | uint32(r[offset])<<16 | uint32(g[offset])<<8 | uint32(b[offset])
		offset += step
	}
}
