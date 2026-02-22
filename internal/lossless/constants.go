package lossless

// VP8L format constants derived from libwebp/src/webp/format_constants.h
// and libwebp/src/dec/vp8l_dec.c.

const (
	// VP8LMagicByte is the VP8L signature byte (0x2f).
	VP8LMagicByte = 0x2f

	// VP8LVersionBits is the number of bits reserved for version.
	VP8LVersionBits = 3
	// VP8LVersion is the current VP8L version (0).
	VP8LVersion = 0

	// VP8LImageSizeBits is the number of bits used to store width/height.
	VP8LImageSizeBits = 14

	// VP8LHeaderSize is the size of the VP8L frame header (1 signature + 4 bytes).
	VP8LHeaderSize = 5

	// NumLiteralCodes is the number of literal codes (256 byte values).
	NumLiteralCodes = 256
	// NumLengthCodes is the number of length prefix codes.
	NumLengthCodes = 24
	// NumDistanceCodes is the number of distance prefix codes.
	NumDistanceCodes = 40
	// CodeLengthCodes is the number of code-length codes.
	CodeLengthCodes = 19

	// MaxAllowedCodeLength is the maximum Huffman code length.
	MaxAllowedCodeLength = 15
	// DefaultCodeLength is the default code length used as initial
	// previous code length in ReadHuffmanCodeLengths.
	DefaultCodeLength = 8

	// HuffmanTableBits is the number of bits for the first-level Huffman table.
	HuffmanTableBits = 8
	// HuffmanTableMask is the bitmask for the first-level table.
	HuffmanTableMask = (1 << HuffmanTableBits) - 1

	// LengthsTableBits is the number of bits for the code-lengths Huffman table.
	LengthsTableBits = 7
	// LengthsTableMask is the bitmask for the code-lengths table.
	LengthsTableMask = (1 << LengthsTableBits) - 1

	// HuffmanPackedBits is the number of bits for packed Huffman tables.
	HuffmanPackedBits = 6
	// HuffmanPackedTableSize is the size of the packed table.
	HuffmanPackedTableSize = 1 << HuffmanPackedBits

	// HuffmanCodesPerMetaCode is the number of Huffman codes per meta code
	// (green+length, alpha, red, blue, distance).
	HuffmanCodesPerMetaCode = 5

	// MaxCacheBits is the maximum color cache bit size.
	MaxCacheBits = 11
	// MinCacheBits is the minimum color cache bit size (0 = disabled).
	MinCacheBits = 0

	// MaxPaletteSize is the maximum palette size for color indexing.
	MaxPaletteSize = 256

	// NumTransforms is the maximum number of transforms in a bitstream.
	NumTransforms = 4
	// TransformPresent is the bit indicating a transform follows.
	TransformPresent = 1

	// MinHuffmanBits is the minimum number of Huffman bits.
	MinHuffmanBits = 2
	// NumHuffmanBits is the number of bits used to encode the Huffman precision.
	NumHuffmanBits = 3

	// MinTransformBits is the minimum number of transform bits.
	MinTransformBits = 2
	// NumTransformBits is the number of bits encoding the transform precision.
	NumTransformBits = 3

	// ARGBBlack is the ARGB value for opaque black.
	ARGBBlack = 0xff000000

	// CodeToPlaneCodesCount is the number of entries in the distance map table.
	CodeToPlaneCodesCount = 120
)

// HuffIndex enumerates the 5 Huffman codes per meta code.
type HuffIndex int

const (
	HuffGreen HuffIndex = iota
	HuffRed
	HuffBlue
	HuffAlpha
	HuffDist
)

// CodeLengthCodeOrder maps the order in which code length codes are
// transmitted in the VP8L bitstream (RFC 6386 / WebP spec).
var CodeLengthCodeOrder = [CodeLengthCodes]int{
	17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
}

// KLiteralMap maps each of the 5 Huffman code types to its kind:
// 0 = variable-size alphabet (green+length, distance)
// 1 = fixed 256-entry alphabet (red, blue, alpha)
var KLiteralMap = [HuffmanCodesPerMetaCode]uint8{0, 1, 1, 1, 0}

// kBaseAlphabetSize contains the base alphabet sizes for the 5 Huffman codes
// (before adding color cache entries). Green includes length codes.
var kBaseAlphabetSize = [HuffmanCodesPerMetaCode]int{
	NumLiteralCodes + NumLengthCodes, // green + length
	NumLiteralCodes,                  // red
	NumLiteralCodes,                  // blue
	NumLiteralCodes,                  // alpha
	NumDistanceCodes,                 // distance
}

// AlphabetSize returns the alphabet size for Huffman code index i,
// given the number of color-cache bits. For green (index 0), the cache
// entries are appended to the literal+length codes. For distance (index 4),
// the size is fixed at NumDistanceCodes.
func AlphabetSize(huffIndex HuffIndex, colorCacheBits int) int {
	size := kBaseAlphabetSize[huffIndex]
	if KLiteralMap[huffIndex] == 0 {
		if huffIndex == HuffGreen {
			size += 1 << colorCacheBits
		}
	}
	return size
}

// CodeToPlane maps distance codes (1-based index into the table) to
// packed (yoffset, xoffset) values used by PlaneCodeToDistance.
// Entry i encodes: yoffset = value >> 4, xoffset = 8 - (value & 0xf).
var CodeToPlane = [CodeToPlaneCodesCount]uint8{
	0x18, 0x07, 0x17, 0x19, 0x28, 0x06, 0x27, 0x29, 0x16, 0x1a,
	0x26, 0x2a, 0x38, 0x05, 0x37, 0x39, 0x15, 0x1b, 0x36, 0x3a,
	0x25, 0x2b, 0x48, 0x04, 0x47, 0x49, 0x14, 0x1c, 0x35, 0x3b,
	0x46, 0x4a, 0x24, 0x2c, 0x58, 0x45, 0x4b, 0x34, 0x3c, 0x03,
	0x57, 0x59, 0x13, 0x1d, 0x56, 0x5a, 0x23, 0x2d, 0x44, 0x4c,
	0x55, 0x5b, 0x33, 0x3d, 0x68, 0x02, 0x67, 0x69, 0x12, 0x1e,
	0x66, 0x6a, 0x22, 0x2e, 0x54, 0x5c, 0x43, 0x4d, 0x65, 0x6b,
	0x32, 0x3e, 0x78, 0x01, 0x77, 0x79, 0x53, 0x5d, 0x11, 0x1f,
	0x64, 0x6c, 0x42, 0x4e, 0x76, 0x7a, 0x21, 0x2f, 0x75, 0x7b,
	0x31, 0x3f, 0x63, 0x6d, 0x52, 0x5e, 0x00, 0x74, 0x7c, 0x41,
	0x4f, 0x10, 0x20, 0x62, 0x6e, 0x30, 0x73, 0x7d, 0x51, 0x5f,
	0x40, 0x72, 0x7e, 0x61, 0x6f, 0x50, 0x71, 0x7f, 0x60, 0x70,
}

// PlaneCodeToDistance converts a VP8L distance code to an actual pixel
// distance, given the image width (xsize).
func PlaneCodeToDistance(xsize int, planeCode int) int {
	if planeCode <= 0 {
		return 1
	}
	if planeCode > CodeToPlaneCodesCount {
		return planeCode - CodeToPlaneCodesCount
	}
	distCode := CodeToPlane[planeCode-1]
	yoffset := int(distCode >> 4)
	xoffset := 8 - int(distCode&0xf)
	dist := yoffset*xsize + xoffset
	if dist < 1 {
		return 1
	}
	return dist
}

// PrefixEncodeBitsNoLUT computes the prefix code and extra bits for a
// 1-based distance value, using the VP8L distance/length encoding formula.
func PrefixEncodeBitsNoLUT(distance int) (code int, extraBits int) {
	distance-- // make 0-based
	if distance < 2 {
		return distance, 0
	}
	highestBit := bitsLog2Floor(distance)
	secondHighestBit := (distance >> (highestBit - 1)) & 1
	extraBits = highestBit - 1
	code = 2*highestBit + secondHighestBit
	return code, extraBits
}

// PrefixEncodeNoLUT computes the prefix code, extra bits, and extra bits
// value for a 1-based distance value.
func PrefixEncodeNoLUT(distance int) (code, extraBits, extraBitsValue int) {
	distance-- // make 0-based
	if distance < 2 {
		return distance, 0, 0
	}
	highestBit := bitsLog2Floor(distance)
	secondHighestBit := (distance >> (highestBit - 1)) & 1
	extraBits = highestBit - 1
	extraBitsValue = distance & ((1 << extraBits) - 1)
	code = 2*highestBit + secondHighestBit
	return code, extraBits, extraBitsValue
}

// bitsLog2Floor returns floor(log2(n)) for n > 0.
func bitsLog2Floor(n int) int {
	log := 0
	for n > 1 {
		log++
		n >>= 1
	}
	return log
}

// VP8LSubSampleSize returns ceil(size / (1 << samplingBits)).
func VP8LSubSampleSize(size, samplingBits int) int {
	return (size + (1 << samplingBits) - 1) >> samplingBits
}

// CodeLengthLiterals is the number of literal code-length values (0..15).
const CodeLengthLiterals = 16

// CodeLengthRepeatCode is the first repeat code (code 16).
const CodeLengthRepeatCode = 16

// CodeLengthExtraBits gives the number of extra bits for repeat codes 16, 17, 18.
var CodeLengthExtraBits = [3]uint8{2, 3, 7}

// CodeLengthRepeatOffsets gives the repeat offset for codes 16, 17, 18.
var CodeLengthRepeatOffsets = [3]uint8{3, 3, 11}

// FixedTableSize is the fixed portion of Huffman table memory:
// 630*3 (red, blue, alpha worst-case) + 410 (distance worst-case).
const FixedTableSize = 630*3 + 410

// KTableSize gives total Huffman table sizes for each cache-bits value (0..11).
var KTableSize = [12]int{
	FixedTableSize + 654, FixedTableSize + 656, FixedTableSize + 658,
	FixedTableSize + 662, FixedTableSize + 670, FixedTableSize + 686,
	FixedTableSize + 718, FixedTableSize + 782, FixedTableSize + 912,
	FixedTableSize + 1168, FixedTableSize + 1680, FixedTableSize + 2704,
}
