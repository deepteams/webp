package container

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// VP8X feature flags (from the first byte of VP8X chunk payload).
const (
	AnimationFlag uint32 = 0x00000002
	XMPFlag       uint32 = 0x00000004
	EXIFFlag      uint32 = 0x00000008
	AlphaFlag     uint32 = 0x00000010
	ICCPFlag      uint32 = 0x00000020
	AllValidFlags uint32 = 0x0000003e
)

// AnimDispose specifies how to dispose a frame before rendering the next.
type AnimDispose int

const (
	DisposeNone       AnimDispose = 0
	DisposeBackground AnimDispose = 1
)

// AnimBlend specifies how to blend the current frame with the previous canvas.
type AnimBlend int

const (
	BlendAlpha AnimBlend = 0
	BlendNone  AnimBlend = 1
)

// Common errors.
var (
	ErrInvalidRIFF    = errors.New("webp: invalid RIFF header")
	ErrInvalidWebP    = errors.New("webp: invalid WEBP signature")
	ErrTruncated      = errors.New("webp: truncated data")
	ErrInvalidChunk   = errors.New("webp: invalid chunk")
	ErrTooLarge       = errors.New("webp: file too large")
	ErrInvalidVP8X    = errors.New("webp: invalid VP8X chunk")
	ErrInvalidFlags   = errors.New("webp: invalid feature flags")
	ErrUnsupported    = errors.New("webp: unsupported format")
	ErrInvalidImage   = errors.New("webp: invalid image dimensions")
)

// Features describes the high-level properties of a WebP file, extracted from
// its RIFF header and (optional) VP8X extended header.
type Features struct {
	Width       int
	Height      int
	HasAlpha    bool
	HasAnim     bool
	HasICCP     bool
	HasEXIF     bool
	HasXMP      bool
	Format      FormatType // VP8, VP8L, or VP8X (extended)
	LoopCount   int        // animation loop count (0 = infinite)
	BGColor     uint32     // animation background color (ARGB)
	CanvasWidth int        // VP8X canvas width (may differ from individual frame)
	CanvasHeight int       // VP8X canvas height
}

// FormatType identifies the VP8 bitstream format.
type FormatType int

const (
	FormatUndefined FormatType = iota
	FormatVP8                  // lossy
	FormatVP8L                 // lossless
	FormatVP8X                 // extended
)

// String returns a human-readable format name.
func (f FormatType) String() string {
	switch f {
	case FormatVP8:
		return "VP8"
	case FormatVP8L:
		return "VP8L"
	case FormatVP8X:
		return "VP8X"
	default:
		return "undefined"
	}
}

// Chunk represents a single RIFF chunk with its FourCC tag and payload.
type Chunk struct {
	FourCC  uint32
	Payload []byte
}

// RIFFHeader holds the parsed RIFF container header.
type RIFFHeader struct {
	FileSize uint32 // total RIFF file size (excluding 8-byte RIFF header)
}

// ParseRIFFHeader validates and parses the 12-byte RIFF/WEBP header from data.
// Returns the header and the number of bytes consumed.
func ParseRIFFHeader(data []byte) (RIFFHeader, int, error) {
	if len(data) < RIFFHeaderSize {
		return RIFFHeader{}, 0, ErrTruncated
	}

	riffTag := binary.LittleEndian.Uint32(data[0:4])
	if riffTag != FourCCRIFF {
		return RIFFHeader{}, 0, ErrInvalidRIFF
	}

	fileSize := binary.LittleEndian.Uint32(data[4:8])
	if fileSize < ChunkHeaderSize {
		return RIFFHeader{}, 0, ErrInvalidRIFF
	}
	if fileSize > MaxChunkPayload {
		return RIFFHeader{}, 0, ErrTooLarge
	}

	webpTag := binary.LittleEndian.Uint32(data[8:12])
	if webpTag != FourCCWEBP {
		return RIFFHeader{}, 0, ErrInvalidWebP
	}

	return RIFFHeader{FileSize: fileSize}, RIFFHeaderSize, nil
}

// ReadChunkHeader reads a chunk's FourCC tag and payload size from data.
// Returns the fourcc, payload size, and bytes consumed (always 8).
func ReadChunkHeader(data []byte) (fourcc uint32, payloadSize uint32, err error) {
	if len(data) < ChunkHeaderSize {
		return 0, 0, ErrTruncated
	}
	fourcc = binary.LittleEndian.Uint32(data[0:4])
	payloadSize = binary.LittleEndian.Uint32(data[4:8])
	if payloadSize > MaxChunkPayload {
		return 0, 0, ErrTooLarge
	}
	return fourcc, payloadSize, nil
}

// PaddedSize returns the payload size padded to an even number of bytes,
// as required by the RIFF format.
func PaddedSize(size uint32) uint32 {
	return size + (size & 1)
}

// FourCCString returns a human-readable string for a FourCC value.
func FourCCString(fourcc uint32) string {
	b := [4]byte{
		byte(fourcc),
		byte(fourcc >> 8),
		byte(fourcc >> 16),
		byte(fourcc >> 24),
	}
	return string(b[:])
}

// ReadChunk reads a complete chunk (header + payload) from an io.Reader.
func ReadChunk(r io.Reader) (Chunk, error) {
	var hdr [ChunkHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Chunk{}, fmt.Errorf("webp: reading chunk header: %w", err)
	}

	fourcc := binary.LittleEndian.Uint32(hdr[0:4])
	payloadSize := binary.LittleEndian.Uint32(hdr[4:8])
	if payloadSize > MaxChunkPayload {
		return Chunk{}, ErrTooLarge
	}

	padded := PaddedSize(payloadSize)
	payload := make([]byte, padded)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Chunk{}, fmt.Errorf("webp: reading chunk payload: %w", err)
	}
	// Return only the actual payload (not padding byte).
	return Chunk{FourCC: fourcc, Payload: payload[:payloadSize]}, nil
}
