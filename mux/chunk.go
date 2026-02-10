// Package mux provides muxing and demuxing for the WebP RIFF container format.
//
// The demuxer parses a WebP file into its constituent chunks (VP8/VP8L bitstream,
// alpha channel, animation frames, ICC profile, EXIF, XMP metadata).
// The muxer assembles chunks back into a valid WebP file.
package mux

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/deepteams/webp/internal/container"
)

// ChunkID is a FourCC identifier for a WebP chunk.
type ChunkID = uint32

// Chunk FourCC identifiers re-exported from the container package.
var (
	FourCCRIFF = container.FourCCRIFF
	FourCCWEBP = container.FourCCWEBP
	FourCCVP8  = container.FourCCVP8
	FourCCVP8L = container.FourCCVP8L
	FourCCVP8X = container.FourCCVP8X
	FourCCALPH = container.FourCCALPH
	FourCCANIM = container.FourCCANIM
	FourCCANMF = container.FourCCANMF
	FourCCICCP = container.FourCCICCP
	FourCCEXIF = container.FourCCEXIF
	FourCCXMP  = container.FourCCXMP
)

// Chunk represents a single chunk in a WebP container.
// Data is a sub-slice of the original input (zero-copy).
type Chunk struct {
	ID   ChunkID
	Size uint32
	Data []byte
}

var (
	ErrInvalidChunkHeader = errors.New("mux: invalid chunk header: need at least 8 bytes")
	ErrChunkTooLarge      = errors.New("mux: chunk payload exceeds container limits")
)

// ReadChunkHeader reads a chunk FourCC and payload size from data.
// A chunk header is 8 bytes: 4 bytes FourCC + 4 bytes little-endian size.
func ReadChunkHeader(data []byte) (ChunkID, uint32, error) {
	if len(data) < container.ChunkHeaderSize {
		return 0, 0, ErrInvalidChunkHeader
	}
	id := binary.LittleEndian.Uint32(data[0:4])
	size := binary.LittleEndian.Uint32(data[4:8])
	if size > container.MaxChunkPayload {
		return 0, 0, ErrChunkTooLarge
	}
	return id, size, nil
}

// ReadChunk reads a full chunk (header + payload) from data and returns the chunk
// plus the total number of bytes consumed (including any padding byte).
func ReadChunk(data []byte) (Chunk, int, error) {
	id, size, err := ReadChunkHeader(data)
	if err != nil {
		return Chunk{}, 0, err
	}
	payloadEnd := container.ChunkHeaderSize + int(size)
	if payloadEnd > len(data) {
		return Chunk{}, 0, fmt.Errorf("mux: chunk %s payload truncated: need %d bytes, have %d",
			fourCCString(id), payloadEnd, len(data))
	}
	c := Chunk{
		ID:   id,
		Size: size,
		Data: data[container.ChunkHeaderSize:payloadEnd],
	}
	// Chunks are padded to even byte boundaries.
	consumed := payloadEnd
	if size%2 != 0 && consumed < len(data) {
		consumed++
	}
	return c, consumed, nil
}

// fourCCString returns a human-readable string for a FourCC value.
func fourCCString(id uint32) string {
	return string([]byte{
		byte(id),
		byte(id >> 8),
		byte(id >> 16),
		byte(id >> 24),
	})
}

// writeChunkHeader writes a chunk header (FourCC + size) into buf.
func writeChunkHeader(buf []byte, id ChunkID, size uint32) {
	binary.LittleEndian.PutUint32(buf[0:4], id)
	binary.LittleEndian.PutUint32(buf[4:8], size)
}
