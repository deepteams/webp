package mux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/deepteams/webp/internal/container"
)

// FrameOptions specifies per-frame parameters for animated WebP.
type FrameOptions struct {
	Duration    int
	OffsetX     int
	OffsetY     int
	BlendMode   BlendMode
	DisposeMode DisposeMode
}

type muxFrame struct {
	data []byte // Raw VP8/VP8L bitstream (may include ALPH prefix chunk).
	opts FrameOptions
}

// Muxer assembles a WebP RIFF container from frames and metadata.
type Muxer struct {
	frames   []muxFrame
	iccData  []byte
	exifData []byte
	xmpData  []byte
	// ANIM parameters.
	bgColor   uint32
	loopCount int
	// Explicit canvas dimensions (VP8X). When set (>0), these take priority
	// over the canvas size computed from frame extents. This matches the C
	// libwebp behavior where the VP8X canvas size is authoritative.
	canvasWidth  int
	canvasHeight int
}

// maxDuration is the maximum frame duration in milliseconds (24-bit max).
// This matches the C libwebp MAX_DURATION constant.
const maxDuration = 0xFFFFFF // 16777215

// maxLoopCount is the maximum animation loop count (16-bit max).
// This matches the C libwebp MAX_LOOP_COUNT constant.
const maxLoopCount = 0xFFFF // 65535

var (
	ErrNoFrames      = errors.New("mux: no frames to assemble")
	ErrFrameEmpty    = errors.New("mux: frame data is empty")
	ErrWriteFailed   = errors.New("mux: write failed")
	ErrMuxValidation = errors.New("mux: validation failed")
)

// NewMuxer creates a new Muxer.
func NewMuxer() *Muxer {
	return &Muxer{}
}

// SetICCProfile sets the ICC color profile data.
func (m *Muxer) SetICCProfile(data []byte) {
	m.iccData = data
}

// SetEXIF sets the EXIF metadata.
func (m *Muxer) SetEXIF(data []byte) {
	m.exifData = data
}

// SetXMP sets the XMP metadata.
func (m *Muxer) SetXMP(data []byte) {
	m.xmpData = data
}

// SetBackgroundColor sets the ANIM background color (ARGB).
func (m *Muxer) SetBackgroundColor(color uint32) {
	m.bgColor = color
}

// SetLoopCount sets the animation loop count (0 = infinite).
// Values are clamped to [0, maxLoopCount] (65535).
func (m *Muxer) SetLoopCount(count int) {
	if count < 0 {
		count = 0
	} else if count > maxLoopCount {
		count = maxLoopCount
	}
	m.loopCount = count
}

// SetCanvasSize explicitly sets the canvas dimensions. When set (both > 0),
// these values take priority over the canvas size computed from frame extents.
// This matches the C libwebp behavior where the VP8X canvas size is
// authoritative. Values are stored as-is; the VP8X chunk will encode them
// as (width-1, height-1) in 24-bit LE.
func (m *Muxer) SetCanvasSize(width, height int) {
	m.canvasWidth = width
	m.canvasHeight = height
}

// clampDuration clamps a frame duration in milliseconds to [0, maxDuration].
func clampDuration(d int) int {
	if d < 0 {
		return 0
	}
	if d > maxDuration {
		return maxDuration
	}
	return d
}

// AddFrame adds a frame. data is the raw VP8/VP8L bitstream.
// opts may be nil for still images. Duration is clamped to [0, maxDuration].
func (m *Muxer) AddFrame(data []byte, opts *FrameOptions) error {
	if len(data) == 0 {
		return ErrFrameEmpty
	}
	fo := FrameOptions{}
	if opts != nil {
		fo = *opts
	}
	fo.Duration = clampDuration(fo.Duration)
	m.frames = append(m.frames, muxFrame{data: data, opts: fo})
	return nil
}

// SetFrameDisposeMode updates the dispose mode of an already-added frame.
// index is 0-based. This is used by the animation encoder to retroactively
// set DISPOSE_BACKGROUND on the previous frame when that produces a smaller
// sub-frame for the current frame.
func (m *Muxer) SetFrameDisposeMode(index int, mode DisposeMode) {
	if index >= 0 && index < len(m.frames) {
		m.frames[index].opts.DisposeMode = mode
	}
}

// SetFrameDuration updates the duration (in milliseconds) of an already-added
// frame. index is 0-based. Duration is clamped to [0, maxDuration]. This is
// used by the animation encoder to extend a frame's duration when merging
// consecutive identical frames.
func (m *Muxer) SetFrameDuration(index int, durationMS int) {
	if index >= 0 && index < len(m.frames) {
		m.frames[index].opts.Duration = clampDuration(durationMS)
	}
}

// FrameDuration returns the duration (in milliseconds) of the frame at the
// given 0-based index. Returns 0 if the index is out of range.
func (m *Muxer) FrameDuration(index int) int {
	if index >= 0 && index < len(m.frames) {
		return m.frames[index].opts.Duration
	}
	return 0
}

// FrameBlendMode returns the blend mode of the frame at the given 0-based
// index. Returns BlendAlpha (0) if the index is out of range.
func (m *Muxer) FrameBlendMode(index int) BlendMode {
	if index >= 0 && index < len(m.frames) {
		return m.frames[index].opts.BlendMode
	}
	return BlendAlpha
}

// NumFrames returns the number of frames added so far.
func (m *Muxer) NumFrames() int {
	return len(m.frames)
}

// AddChunk adds an arbitrary metadata chunk (e.g. ICCP, EXIF, XMP).
func (m *Muxer) AddChunk(id ChunkID, data []byte) {
	switch id {
	case FourCCICCP:
		m.iccData = data
	case FourCCEXIF:
		m.exifData = data
	case FourCCXMP:
		m.xmpData = data
	}
}

// isAnimated returns true if the muxer has multiple frames or any frame has a non-zero duration.
func (m *Muxer) isAnimated() bool {
	if len(m.frames) > 1 {
		return true
	}
	for _, f := range m.frames {
		if f.opts.Duration > 0 {
			return true
		}
	}
	return false
}

// needsVP8X returns true if the file requires the extended format header.
func (m *Muxer) needsVP8X() bool {
	return m.isAnimated() || m.iccData != nil || m.exifData != nil || m.xmpData != nil
}

// Assemble writes the complete WebP file to w.
func (m *Muxer) Assemble(w io.Writer) error {
	if err := m.validate(); err != nil {
		return err
	}

	// If single frame with no metadata, write simple format.
	if !m.needsVP8X() {
		return m.assembleSimple(w)
	}
	return m.assembleExtended(w)
}

// validate checks the muxer state for consistency before assembling.
// This mirrors libwebp's MuxValidate from muxinternal.c.
func (m *Muxer) validate() error {
	if len(m.frames) == 0 {
		return ErrNoFrames
	}
	animated := m.isAnimated()
	if animated {
		// Animated: must have at least 1 frame.
		if len(m.frames) < 1 {
			return fmt.Errorf("%w: animated image must have at least 1 frame", ErrMuxValidation)
		}
	} else {
		// Non-animated: exactly 1 frame.
		if len(m.frames) != 1 {
			return fmt.Errorf("%w: non-animated image must have exactly 1 frame", ErrMuxValidation)
		}
	}
	// Check that frame dimensions fit within the canvas.
	canvasW, canvasH := m.canvasSize()
	for i, f := range m.frames {
		fw, fh := frameDimensions(f.data)
		if fw == 0 || fh == 0 {
			continue // could not parse dimensions, skip check
		}
		endX := f.opts.OffsetX + fw
		endY := f.opts.OffsetY + fh
		// Check for integer overflow.
		if fw > 0 && endX <= f.opts.OffsetX || fh > 0 && endY <= f.opts.OffsetY {
			return fmt.Errorf("%w: frame %d offset+dimension overflow", ErrMuxValidation, i)
		}
		if endX > canvasW || endY > canvasH {
			return fmt.Errorf("%w: frame %d (%dx%d at %d,%d) exceeds canvas (%dx%d)",
				ErrMuxValidation, i, fw, fh, f.opts.OffsetX, f.opts.OffsetY, canvasW, canvasH)
		}
	}
	return nil
}

// hasAlpha scans all frames to detect if any frame contains alpha data.
// For VP8L: the header indicates alpha presence.
// For VP8: alpha is present if the frame data is prefixed with an ALPH chunk.
func (m *Muxer) hasAlpha() bool {
	for _, f := range m.frames {
		data := f.data
		// Check if the data starts with an ALPH chunk header.
		if len(data) >= 12 {
			possibleID := binary.LittleEndian.Uint32(data[0:4])
			if possibleID == FourCCALPH {
				return true
			}
		}
		// Check VP8L header for alpha flag.
		if len(data) >= 5 && data[0] == container.VP8LMagicByte {
			_, _, alpha, err := parseVP8LDimensions(data)
			if err == nil && alpha {
				return true
			}
		}
	}
	return false
}

// assembleSimple writes a simple (non-extended) WebP file.
func (m *Muxer) assembleSimple(w io.Writer) error {
	frame := m.frames[0]
	chunkID := detectBitstreamType(frame.data)
	chunkSize := uint32(len(frame.data))
	paddedChunkSize := chunkSize
	if chunkSize%2 != 0 {
		paddedChunkSize++
	}

	// Total RIFF payload = "WEBP" (4) + chunk header (8) + padded payload.
	riffPayload := 4 + container.ChunkHeaderSize + paddedChunkSize
	buf := make([]byte, container.RIFFHeaderSize+container.ChunkHeaderSize)

	// RIFF header.
	binary.LittleEndian.PutUint32(buf[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(riffPayload))
	binary.LittleEndian.PutUint32(buf[8:12], FourCCWEBP)

	// Chunk header.
	writeChunkHeader(buf[12:20], chunkID, chunkSize)

	if _, err := w.Write(buf); err != nil {
		return err
	}
	if _, err := w.Write(frame.data); err != nil {
		return err
	}
	if chunkSize%2 != 0 {
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
}

// assembleExtended writes an extended (VP8X) WebP file.
func (m *Muxer) assembleExtended(w io.Writer) error {
	animated := m.isAnimated()

	// Build VP8X flags.
	var flags byte
	if animated {
		flags |= flagAnimation
	}
	if m.iccData != nil {
		flags |= flagICCP
	}
	if m.exifData != nil {
		flags |= flagEXIF
	}
	if m.xmpData != nil {
		flags |= flagXMP
	}
	if m.hasAlpha() {
		flags |= flagAlpha
	}

	// Determine canvas size. For simple cases, use first frame dimensions.
	canvasW, canvasH := m.canvasSize()

	// Calculate total RIFF payload size.
	riffPayload := uint32(4) // "WEBP"

	// VP8X chunk: header + 10 bytes.
	riffPayload += container.ChunkHeaderSize + container.VP8XChunkSize

	// ICC chunk.
	if m.iccData != nil {
		riffPayload += chunkTotalSize(uint32(len(m.iccData)))
	}

	// ANIM chunk.
	if animated {
		riffPayload += container.ChunkHeaderSize + container.ANIMChunkSize
	}

	// Frames.
	for _, f := range m.frames {
		if animated {
			// ANMF chunk: header + 16 bytes ANMF header + sub-chunk(s).
			// Split alpha and bitstream to compute sub-chunk sizes correctly.
			alphaData, bitstream := splitAlphaAndBitstream(f.data)
			subSize := frameSubChunksSize(alphaData, bitstream)
			anmfPayload := uint32(container.ANMFChunkSize) + subSize
			riffPayload += container.ChunkHeaderSize + anmfPayload
			if anmfPayload%2 != 0 {
				riffPayload++
			}
		} else {
			riffPayload += chunkTotalSize(uint32(len(f.data)))
		}
	}

	// EXIF chunk.
	if m.exifData != nil {
		riffPayload += chunkTotalSize(uint32(len(m.exifData)))
	}

	// XMP chunk.
	if m.xmpData != nil {
		riffPayload += chunkTotalSize(uint32(len(m.xmpData)))
	}

	// Write RIFF header.
	header := make([]byte, container.RIFFHeaderSize)
	binary.LittleEndian.PutUint32(header[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(header[4:8], riffPayload)
	binary.LittleEndian.PutUint32(header[8:12], FourCCWEBP)
	if _, err := w.Write(header); err != nil {
		return err
	}

	// Write VP8X chunk.
	vp8xBuf := make([]byte, container.ChunkHeaderSize+container.VP8XChunkSize)
	writeChunkHeader(vp8xBuf[0:8], FourCCVP8X, container.VP8XChunkSize)
	vp8xBuf[8] = flags
	// Bytes 9-11 reserved (already zero).
	// Canvas width-1 as 24-bit LE at offset 12..14.
	putLE24(vp8xBuf[12:15], canvasW-1)
	// Canvas height-1 as 24-bit LE at offset 15..17.
	putLE24(vp8xBuf[15:18], canvasH-1)
	if _, err := w.Write(vp8xBuf); err != nil {
		return err
	}

	// Write ICC chunk.
	if m.iccData != nil {
		if err := writeDataChunk(w, FourCCICCP, m.iccData); err != nil {
			return err
		}
	}

	// Write ANIM chunk.
	if animated {
		animBuf := make([]byte, container.ChunkHeaderSize+container.ANIMChunkSize)
		writeChunkHeader(animBuf[0:8], FourCCANIM, container.ANIMChunkSize)
		binary.LittleEndian.PutUint32(animBuf[8:12], m.bgColor)
		binary.LittleEndian.PutUint16(animBuf[12:14], uint16(m.loopCount))
		if _, err := w.Write(animBuf); err != nil {
			return err
		}
	}

	// Write frames.
	for _, f := range m.frames {
		if animated {
			if err := m.writeANMFChunk(w, f); err != nil {
				return err
			}
		} else {
			if err := writeDataChunk(w, detectBitstreamType(f.data), f.data); err != nil {
				return err
			}
		}
	}

	// Write EXIF chunk.
	if m.exifData != nil {
		if err := writeDataChunk(w, FourCCEXIF, m.exifData); err != nil {
			return err
		}
	}

	// Write XMP chunk.
	if m.xmpData != nil {
		if err := writeDataChunk(w, FourCCXMP, m.xmpData); err != nil {
			return err
		}
	}

	return nil
}

// splitAlphaAndBitstream separates frame data into optional ALPH chunk data
// and the VP8/VP8L bitstream. If the frame data starts with an ALPH chunk
// header, it extracts the alpha payload and the remainder as the bitstream.
// Otherwise, alphaData is nil and bitstream is the full frame data.
func splitAlphaAndBitstream(data []byte) (alphaData, bitstream []byte) {
	if len(data) >= container.ChunkHeaderSize {
		possibleID := binary.LittleEndian.Uint32(data[0:4])
		if possibleID == FourCCALPH {
			alphSize := binary.LittleEndian.Uint32(data[4:8])
			alphEnd := container.ChunkHeaderSize + int(alphSize)
			if alphEnd <= len(data) {
				alphaData = data[container.ChunkHeaderSize:alphEnd]
				rest := alphEnd
				// Skip padding byte if needed.
				if alphSize%2 != 0 && rest < len(data) {
					rest++
				}
				bitstream = data[rest:]
				return
			}
		}
	}
	return nil, data
}

// writeANMFChunk writes an ANMF wrapper around a frame's image data.
// The ordering inside ANMF matches libwebp (muxinternal.c:396-409):
//  1. ANMF chunk header
//  2. ALPH sub-chunk (if present, encapsulated inside ANMF)
//  3. VP8/VP8L sub-chunk
func (m *Muxer) writeANMFChunk(w io.Writer, f muxFrame) error {
	alphaData, bitstream := splitAlphaAndBitstream(f.data)
	subSize := frameSubChunksSize(alphaData, bitstream)
	anmfPayload := uint32(container.ANMFChunkSize) + subSize

	// ANMF chunk header.
	hdr := make([]byte, container.ChunkHeaderSize+container.ANMFChunkSize)
	writeChunkHeader(hdr[0:8], FourCCANMF, anmfPayload)

	// ANMF frame header (16 bytes).
	putLE24(hdr[8:11], f.opts.OffsetX/2)
	putLE24(hdr[11:14], f.opts.OffsetY/2)

	// Parse frame dimensions from bitstream.
	fw, fh := frameDimensions(f.data)
	if fw > 0 && fh > 0 {
		putLE24(hdr[14:17], fw-1)
		putLE24(hdr[17:20], fh-1)
	}
	putLE24(hdr[20:23], f.opts.Duration)

	var flagByte byte
	if f.opts.DisposeMode == DisposeBackground {
		flagByte |= 0x01
	}
	if f.opts.BlendMode == BlendNone {
		flagByte |= 0x02
	}
	hdr[23] = flagByte

	if _, err := w.Write(hdr); err != nil {
		return err
	}

	// Write ALPH sub-chunk inside ANMF (if present).
	if alphaData != nil {
		if err := writeDataChunk(w, FourCCALPH, alphaData); err != nil {
			return err
		}
	}

	// Write VP8/VP8L sub-chunk.
	chunkID := detectBitstreamType(bitstream)
	if err := writeDataChunk(w, chunkID, bitstream); err != nil {
		return err
	}

	// Padding for ANMF chunk as a whole.
	if anmfPayload%2 != 0 {
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
}

// canvasSize determines the canvas dimensions.
// If explicit canvas dimensions were set via SetCanvasSize (both > 0), those
// are returned directly. This matches the C libwebp behavior where the VP8X
// canvas size from the container header is authoritative, even if it differs
// from the extent of the contained frames.
// Otherwise, the canvas size is computed from the maximum extent of all frames.
func (m *Muxer) canvasSize() (int, int) {
	// Explicit VP8X canvas size takes priority.
	if m.canvasWidth > 0 && m.canvasHeight > 0 {
		return m.canvasWidth, m.canvasHeight
	}

	// Fallback: compute from frame extents (for simple WebP without VP8X).
	if len(m.frames) == 0 {
		return 1, 1
	}
	maxW, maxH := 0, 0
	for _, f := range m.frames {
		fw, fh := frameDimensions(f.data)
		endX := f.opts.OffsetX + fw
		endY := f.opts.OffsetY + fh
		// Guard against integer overflow.
		if fw > 0 && endX < f.opts.OffsetX {
			endX = math.MaxInt
		}
		if fh > 0 && endY < f.opts.OffsetY {
			endY = math.MaxInt
		}
		if endX > maxW {
			maxW = endX
		}
		if endY > maxH {
			maxH = endY
		}
	}
	if maxW == 0 {
		maxW = 1
	}
	if maxH == 0 {
		maxH = 1
	}
	return maxW, maxH
}

// frameDimensions attempts to read width/height from a bitstream.
// Handles both raw bitstreams and ALPH-prefixed data.
func frameDimensions(data []byte) (int, int) {
	// Skip ALPH chunk prefix if present.
	_, bs := splitAlphaAndBitstream(data)
	if len(bs) >= 5 && bs[0] == container.VP8LMagicByte {
		w, h, _, err := parseVP8LDimensions(bs)
		if err == nil {
			return w, h
		}
	}
	if len(bs) >= 10 {
		w, h, err := parseVP8Dimensions(bs)
		if err == nil {
			return w, h
		}
	}
	return 0, 0
}

// detectBitstreamType returns the chunk ID for the given bitstream data.
func detectBitstreamType(data []byte) ChunkID {
	if len(data) > 0 && data[0] == container.VP8LMagicByte {
		return FourCCVP8L
	}
	return FourCCVP8
}

// frameSubChunksSize returns the total size of all sub-chunks inside an ANMF
// frame (optional ALPH + VP8/VP8L), including chunk headers and padding.
func frameSubChunksSize(alphaData, bitstream []byte) uint32 {
	size := uint32(0)
	if alphaData != nil {
		size += chunkTotalSize(uint32(len(alphaData)))
	}
	size += chunkTotalSize(uint32(len(bitstream)))
	return size
}

// subChunkSize returns the total size of a frame's sub-chunk(s)
// (header + payload + padding), handling ALPH-prefixed data.
func subChunkSize(data []byte) uint32 {
	alphaData, bitstream := splitAlphaAndBitstream(data)
	return frameSubChunksSize(alphaData, bitstream)
}

// chunkTotalSize returns header + payload + optional padding byte.
func chunkTotalSize(payloadSize uint32) uint32 {
	total := uint32(container.ChunkHeaderSize) + payloadSize
	if payloadSize%2 != 0 {
		total++
	}
	return total
}

// writeDataChunk writes a chunk header + data + optional padding.
func writeDataChunk(w io.Writer, id ChunkID, data []byte) error {
	hdr := make([]byte, container.ChunkHeaderSize)
	writeChunkHeader(hdr, id, uint32(len(data)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if len(data)%2 != 0 {
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
}

// putLE24 writes a 24-bit little-endian value into buf[0:3].
func putLE24(buf []byte, v int) {
	buf[0] = byte(v)
	buf[1] = byte(v >> 8)
	buf[2] = byte(v >> 16)
}
