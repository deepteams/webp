// Package container defines constants for the WebP/RIFF container format,
// including FourCC values, magic bytes, chunk IDs, and VP8/VP8L format constants.
package container

import "encoding/binary"

// FourCC creates a FourCC value from four bytes (little-endian).
func FourCC(a, b, c, d byte) uint32 {
	return uint32(a) | uint32(b)<<8 | uint32(c)<<16 | uint32(d)<<24
}

// Container FourCC values.
var (
	FourCCRIFF = FourCC('R', 'I', 'F', 'F')
	FourCCWEBP = FourCC('W', 'E', 'B', 'P')
	FourCCVP8  = FourCC('V', 'P', '8', ' ')
	FourCCVP8L = FourCC('V', 'P', '8', 'L')
	FourCCVP8X = FourCC('V', 'P', '8', 'X')
	FourCCALPH = FourCC('A', 'L', 'P', 'H')
	FourCCANIM = FourCC('A', 'N', 'I', 'M')
	FourCCANMF = FourCC('A', 'N', 'M', 'F')
	FourCCICCP = FourCC('I', 'C', 'C', 'P')
	FourCCEXIF = FourCC('E', 'X', 'I', 'F')
	FourCCXMP  = FourCC('X', 'M', 'P', ' ')
)

// VP8 format constants.
const (
	VP8Signature        = 0x9d012a        // Signature in VP8 data
	VP8MaxPartition0    = 1 << 19         // max size of mode partition
	VP8MaxPartitionSize = 1 << 24         // max size for token partition
	VP8FrameHeaderSize  = 10              // Size of the frame header within VP8 data
)

// VP8L format constants.
const (
	VP8LSignatureSize   = 1    // VP8L signature size
	VP8LMagicByte       = 0x2f // VP8L signature byte
	VP8LImageSizeBits   = 14   // Number of bits used to store width and height
	VP8LVersionBits     = 3    // 3 bits reserved for version
	VP8LVersion         = 0    // version 0
	VP8LFrameHeaderSize = 5    // Size of the VP8L frame header
	VP8LMaxNumBitRead   = 24   // Maximum bits the bit-reader can handle at once
)

// VP8L transform types.
const (
	PredictorTransform    = 0
	CrossColorTransform   = 1
	SubtractGreenTransform = 2
	ColorIndexingTransform = 3
	NumTransforms          = 4
)

// Huffman constants.
const (
	MaxPaletteSize        = 256
	MaxCacheBits          = 11
	HuffmanCodesPerMeta   = 5
	ARGBBlack             = 0xff000000
	DefaultCodeLength     = 8
	MaxAllowedCodeLength  = 15
	NumLiteralCodes       = 256
	NumLengthCodes        = 24
	NumDistanceCodes      = 40
	CodeLengthCodes       = 19
	MinHuffmanBits        = 2
	NumHuffmanBits        = 3
	MinTransformBits      = 2
	NumTransformBits      = 3
	TransformPresent      = 1
)

// Alpha constants.
const (
	AlphaHeaderLen           = 1
	AlphaNoCompression       = 0
	AlphaLosslessCompression = 1
	AlphaPreprocessedLevels  = 1
)

// Container structure sizes.
const (
	TagSize         = 4  // Size of a chunk tag (e.g. "VP8L")
	ChunkSizeBytes  = 4  // Size needed to store chunk's size
	ChunkHeaderSize = 8  // Size of a chunk header
	RIFFHeaderSize  = 12 // Size of the RIFF header ("RIFFnnnnWEBP")
	ANMFChunkSize   = 16 // Size of an ANMF chunk
	ANIMChunkSize   = 6  // Size of an ANIM chunk
	VP8XChunkSize   = 10 // Size of a VP8X chunk
)

// Limits.
const (
	MaxCanvasSize    = 1 << 24         // 24-bit max for VP8X width/height
	MaxImageArea     = uint64(1) << 32 // 32-bit max for width x height
	MaxLoopCount     = 1 << 16
	MaxDuration      = 1 << 24
	MaxPositionOff   = 1 << 24
	MaxChunkPayload  = ^uint32(0) - ChunkHeaderSize - 1
)

// Intra prediction modes (from common_dec.h).
const (
	BDCPred   = 0 // 4x4 modes
	BTMPred   = 1
	BVEPred   = 2
	BHEPred   = 3
	BRDPred   = 4
	BVRPred   = 5
	BLDPred   = 6
	BVLPred   = 7
	BHDPred   = 8
	BHUPred   = 9
	NumBModes = 10

	DCPred       = BDCPred
	VPred        = BVEPred
	HPred        = BHEPred
	TMPred       = BTMPred
	BPred        = NumBModes
	NumPredModes = 4

	BDCPredNoTop     = 4
	BDCPredNoLeft    = 5
	BDCPredNoTopLeft = 6
	NumBDCModes      = 7
)

// VP8 probability/partition constants.
const (
	MBFeatureTreeProbs = 3
	NumMBSegments      = 4
	NumRefLFDeltas     = 4
	NumModeLFDeltas    = 4
	MaxNumPartitions   = 8
	NumTypes           = 4 // 0: i16-AC, 1: i16-DC, 2: chroma-AC, 3: i4-AC
	NumBands           = 8
	NumCTX             = 3
	NumProbas          = 11
)

// BPS is the common stride for encoder/decoder block processing.
const BPS = 32

// Filter types for alpha/spatial filtering.
const (
	FilterNone       = 0
	FilterHorizontal = 1
	FilterVertical   = 2
	FilterGradient   = 3
	FilterLast       = FilterGradient + 1
	FilterBest       = FilterLast
	FilterFast       = FilterLast + 1
)

// ReadLE16 reads a little-endian uint16 from data.
func ReadLE16(data []byte) uint16 {
	return binary.LittleEndian.Uint16(data)
}

// ReadLE32 reads a little-endian uint32 from data.
func ReadLE32(data []byte) uint32 {
	return binary.LittleEndian.Uint32(data)
}

// PutLE16 writes a little-endian uint16 to data.
func PutLE16(data []byte, v uint16) {
	binary.LittleEndian.PutUint16(data, v)
}

// PutLE32 writes a little-endian uint32 to data.
func PutLE32(data []byte, v uint32) {
	binary.LittleEndian.PutUint32(data, v)
}
