package mux

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/deepteams/webp/internal/container"
)

// BlendMode specifies how a frame is blended with the previous canvas.
type BlendMode int

const (
	BlendAlpha BlendMode = 0 // Alpha-blend with previous canvas.
	BlendNone  BlendMode = 1 // Do not blend; overwrite.
)

// DisposeMode specifies how the frame area is treated after rendering.
type DisposeMode int

const (
	DisposeNone       DisposeMode = 0 // Leave as-is.
	DisposeBackground DisposeMode = 1 // Fill with background color.
)

// Format describes the encoding format of a WebP file.
type Format int

const (
	FormatUndefined Format = 0
	FormatLossy     Format = 1
	FormatLossless  Format = 2
	FormatExtended  Format = 3
)

func (f Format) String() string {
	switch f {
	case FormatLossy:
		return "VP8"
	case FormatLossless:
		return "VP8L"
	case FormatExtended:
		return "VP8X"
	default:
		return "undefined"
	}
}

// VP8X flag bits.
const (
	flagAnimation = 1 << 1
	flagXMP       = 1 << 2
	flagEXIF      = 1 << 3
	flagAlpha     = 1 << 4
	flagICCP      = 1 << 5
)

// Features describes the features present in a WebP file.
type Features struct {
	Width        int
	Height       int
	HasAlpha     bool
	HasAnimation bool
	HasICC       bool
	HasEXIF      bool
	HasXMP       bool
	Format       Format
}

// FrameInfo holds data and metadata for a single animation frame (or the sole image).
type FrameInfo struct {
	Data        []byte // VP8/VP8L bitstream data (may include ALPH chunk prefix).
	AlphaData   []byte // Standalone ALPH chunk data, if any.
	Width       int
	Height      int
	OffsetX     int
	OffsetY     int
	Duration    int // Milliseconds (0 for still images).
	IsKeyframe  bool
	HasAlpha    bool // True if the frame's bitstream signals alpha (ALPH chunk or VP8L alpha bit).
	BlendMode   BlendMode
	DisposeMode DisposeMode
}

// Demuxer parses a WebP RIFF container.
type Demuxer struct {
	data     []byte
	chunks   []Chunk
	features Features
	frames   []FrameInfo
	// Metadata chunk data (nil if not present).
	iccData  []byte
	exifData []byte
	xmpData  []byte
	// ANIM parameters.
	bgColor   uint32
	loopCount int
}

// maxMetadataSize is the maximum allowed size for a single metadata chunk
// (ICC, EXIF, XMP) to prevent memory exhaustion from malicious inputs.
const maxMetadataSize = 100 * 1024 * 1024 // 100 MB

// maxFrames is the maximum number of animation frames allowed to prevent
// memory exhaustion from malicious inputs.
const maxFrames = 10000

var (
	ErrInvalidRIFF    = errors.New("mux: not a valid WebP file (bad RIFF header)")
	ErrTruncated      = errors.New("mux: data truncated")
	ErrNoImage        = errors.New("mux: no image data found")
	ErrInvalidVP8X    = errors.New("mux: invalid VP8X chunk")
	ErrInvalidANIM    = errors.New("mux: invalid ANIM chunk")
	ErrInvalidANMF    = errors.New("mux: invalid ANMF chunk")
	ErrInvalidFrame   = errors.New("mux: invalid frame bitstream")
	ErrFrameOutRange  = errors.New("mux: frame index out of range")
	ErrChunkNotFound  = errors.New("mux: chunk not found")
	ErrMetadataTooLarge = errors.New("mux: metadata chunk too large")
	ErrTooManyFrames  = errors.New("mux: too many frames")
)

// NewDemuxer parses a WebP file from data and returns a Demuxer.
func NewDemuxer(data []byte) (*Demuxer, error) {
	d := &Demuxer{data: data}
	if err := d.parse(); err != nil {
		return nil, err
	}
	return d, nil
}

// GetFeatures returns the features extracted from the WebP file.
func (d *Demuxer) GetFeatures() Features {
	return d.features
}

// NumFrames returns the number of frames.
func (d *Demuxer) NumFrames() int {
	return len(d.frames)
}

// Frame returns frame info for the given 0-based index.
func (d *Demuxer) Frame(index int) (*FrameInfo, error) {
	if index < 0 || index >= len(d.frames) {
		return nil, ErrFrameOutRange
	}
	fi := d.frames[index]
	return &fi, nil
}

// GetChunk returns the payload for the first chunk with the given ID.
func (d *Demuxer) GetChunk(id ChunkID) ([]byte, error) {
	switch id {
	case FourCCICCP:
		if d.iccData != nil {
			return d.iccData, nil
		}
	case FourCCEXIF:
		if d.exifData != nil {
			return d.exifData, nil
		}
	case FourCCXMP:
		if d.xmpData != nil {
			return d.xmpData, nil
		}
	default:
		for _, c := range d.chunks {
			if c.ID == id {
				return c.Data, nil
			}
		}
	}
	return nil, ErrChunkNotFound
}

// LoopCount returns the animation loop count (0 = infinite).
func (d *Demuxer) LoopCount() int {
	return d.loopCount
}

// BackgroundColor returns the ANIM background color (ARGB).
func (d *Demuxer) BackgroundColor() uint32 {
	return d.bgColor
}

// FrameIterator provides streaming access to frames.
type FrameIterator struct {
	d   *Demuxer
	pos int
}

// NewFrameIterator returns a new iterator starting at frame 0.
func (d *Demuxer) NewFrameIterator() *FrameIterator {
	return &FrameIterator{d: d, pos: 0}
}

// HasNext reports whether more frames are available.
func (it *FrameIterator) HasNext() bool {
	return it.pos < len(it.d.frames)
}

// Next returns the next frame and advances the iterator.
func (it *FrameIterator) Next() (*FrameInfo, error) {
	if !it.HasNext() {
		return nil, ErrFrameOutRange
	}
	fi := it.d.frames[it.pos]
	it.pos++
	return &fi, nil
}

// parse validates the RIFF header and iterates through all chunks.
func (d *Demuxer) parse() error {
	if len(d.data) < container.RIFFHeaderSize {
		return ErrInvalidRIFF
	}
	// Validate RIFF header.
	riffTag := binary.LittleEndian.Uint32(d.data[0:4])
	if riffTag != FourCCRIFF {
		return ErrInvalidRIFF
	}
	fileSize := binary.LittleEndian.Uint32(d.data[4:8])
	webpTag := binary.LittleEndian.Uint32(d.data[8:12])
	if webpTag != FourCCWEBP {
		return ErrInvalidRIFF
	}
	// fileSize is the size after the first 8 bytes (RIFF + size field).
	totalSize := int(fileSize) + 8
	if totalSize > len(d.data) {
		// Allow truncated data — work with what we have.
		totalSize = len(d.data)
	}
	payload := d.data[container.RIFFHeaderSize:totalSize]

	// Parse the first chunk to determine format.
	if len(payload) < container.ChunkHeaderSize {
		return ErrNoImage
	}
	firstTag := binary.LittleEndian.Uint32(payload[0:4])

	switch firstTag {
	case FourCCVP8X:
		return d.parseExtended(payload)
	case FourCCVP8:
		return d.parseSimpleVP8(payload)
	case FourCCVP8L:
		return d.parseSimpleVP8L(payload)
	default:
		return fmt.Errorf("mux: unknown first chunk %s", fourCCString(firstTag))
	}
}

// parseSimpleVP8 handles a non-extended lossy WebP file.
func (d *Demuxer) parseSimpleVP8(payload []byte) error {
	c, _, err := ReadChunk(payload)
	if err != nil {
		return err
	}
	w, h, err := parseVP8Dimensions(c.Data)
	if err != nil {
		return err
	}
	d.features = Features{
		Width:  w,
		Height: h,
		Format: FormatLossy,
	}
	d.frames = []FrameInfo{{
		Data:       c.Data,
		Width:      w,
		Height:     h,
		IsKeyframe: true,
	}}
	d.chunks = []Chunk{c}
	return nil
}

// parseSimpleVP8L handles a non-extended lossless WebP file.
func (d *Demuxer) parseSimpleVP8L(payload []byte) error {
	c, _, err := ReadChunk(payload)
	if err != nil {
		return err
	}
	w, h, hasAlpha, err := parseVP8LDimensions(c.Data)
	if err != nil {
		return err
	}
	d.features = Features{
		Width:    w,
		Height:   h,
		HasAlpha: hasAlpha,
		Format:   FormatLossless,
	}
	d.frames = []FrameInfo{{
		Data:       c.Data,
		Width:      w,
		Height:     h,
		HasAlpha:   hasAlpha,
		IsKeyframe: true,
	}}
	d.chunks = []Chunk{c}
	return nil
}

// parseExtended handles VP8X-extended WebP files.
func (d *Demuxer) parseExtended(payload []byte) error {
	// Read VP8X chunk.
	vp8x, consumed, err := ReadChunk(payload)
	if err != nil {
		return err
	}
	if vp8x.Size < container.VP8XChunkSize {
		return ErrInvalidVP8X
	}

	flags := vp8x.Data[0]
	// Canvas dimensions are 24-bit LE at offset 4..6 and 7..9, stored as value-1.
	canvasWidth := int(vp8x.Data[4]) | int(vp8x.Data[5])<<8 | int(vp8x.Data[6])<<16
	canvasWidth++
	canvasHeight := int(vp8x.Data[7]) | int(vp8x.Data[8])<<8 | int(vp8x.Data[9])<<16
	canvasHeight++

	d.features = Features{
		Width:        canvasWidth,
		Height:       canvasHeight,
		HasAlpha:     flags&flagAlpha != 0,
		HasAnimation: flags&flagAnimation != 0,
		HasICC:       flags&flagICCP != 0,
		HasEXIF:      flags&flagEXIF != 0,
		HasXMP:       flags&flagXMP != 0,
		Format:       FormatExtended,
	}
	d.chunks = append(d.chunks, vp8x)

	// Iterate remaining chunks.
	pos := consumed
	for pos+container.ChunkHeaderSize <= len(payload) {
		c, n, err := ReadChunk(payload[pos:])
		if err != nil {
			break
		}
		d.chunks = append(d.chunks, c)
		switch c.ID {
		case FourCCICCP:
			if len(c.Data) > maxMetadataSize {
				return fmt.Errorf("%w: ICCP chunk %d bytes, max %d", ErrMetadataTooLarge, len(c.Data), maxMetadataSize)
			}
			d.iccData = c.Data
		case FourCCEXIF:
			if len(c.Data) > maxMetadataSize {
				return fmt.Errorf("%w: EXIF chunk %d bytes, max %d", ErrMetadataTooLarge, len(c.Data), maxMetadataSize)
			}
			d.exifData = c.Data
		case FourCCXMP:
			if len(c.Data) > maxMetadataSize {
				return fmt.Errorf("%w: XMP chunk %d bytes, max %d", ErrMetadataTooLarge, len(c.Data), maxMetadataSize)
			}
			d.xmpData = c.Data
		case FourCCANIM:
			if err := d.parseANIM(c.Data); err != nil {
				return err
			}
		case FourCCANMF:
			if err := d.parseANMF(c.Data); err != nil {
				return err
			}
		case FourCCVP8, FourCCVP8L, FourCCALPH:
			// Non-animated extended file: single image with possible alpha.
			if !d.features.HasAnimation && len(d.frames) == 0 {
				if err := d.parseSingleExtendedFrame(payload[pos:]); err != nil {
					return err
				}
			}
		}
		pos += n
	}

	if len(d.frames) == 0 {
		return ErrNoImage
	}
	return nil
}

// parseANIM extracts animation parameters (background color, loop count).
func (d *Demuxer) parseANIM(data []byte) error {
	if len(data) < container.ANIMChunkSize {
		return ErrInvalidANIM
	}
	d.bgColor = binary.LittleEndian.Uint32(data[0:4])
	d.loopCount = int(binary.LittleEndian.Uint16(data[4:6]))
	return nil
}

// parseANMF extracts a single animation frame from an ANMF chunk payload.
func (d *Demuxer) parseANMF(data []byte) error {
	if len(data) < container.ANMFChunkSize {
		return ErrInvalidANMF
	}
	offsetX := (int(data[0]) | int(data[1])<<8 | int(data[2])<<16) * 2
	offsetY := (int(data[3]) | int(data[4])<<8 | int(data[5])<<16) * 2
	width := (int(data[6]) | int(data[7])<<8 | int(data[8])<<16) + 1
	height := (int(data[9]) | int(data[10])<<8 | int(data[11])<<16) + 1
	duration := int(data[12]) | int(data[13])<<8 | int(data[14])<<16
	flagByte := data[15]

	dispose := DisposeNone
	if flagByte&0x01 != 0 {
		dispose = DisposeBackground
	}
	blend := BlendAlpha
	if flagByte&0x02 != 0 {
		blend = BlendNone
	}

	// The rest of the ANMF payload contains the frame's image sub-chunks.
	framePayload := data[container.ANMFChunkSize:]
	var imageData []byte
	var alphaData []byte

	pos := 0
	for pos+container.ChunkHeaderSize <= len(framePayload) {
		subID, subSize, err := ReadChunkHeader(framePayload[pos:])
		if err != nil {
			break
		}
		subEnd := container.ChunkHeaderSize + int(subSize)
		if subEnd > len(framePayload[pos:]) {
			break
		}
		subData := framePayload[pos+container.ChunkHeaderSize : pos+subEnd]

		switch subID {
		case FourCCVP8, FourCCVP8L:
			imageData = subData
		case FourCCALPH:
			alphaData = subData
		}
		advance := subEnd
		if subSize%2 != 0 && pos+advance < len(framePayload) {
			advance++
		}
		pos += advance
	}

	// Determine per-frame alpha from the bitstream, matching the C demuxer:
	// alpha is present if there's a standalone ALPH chunk, or if the VP8L
	// header's alpha bit is set.
	hasAlpha := len(alphaData) > 0
	if !hasAlpha && len(imageData) > 0 {
		hasAlpha = frameDataHasAlpha(imageData)
	}

	if len(d.frames) >= maxFrames {
		return fmt.Errorf("%w: exceeded limit of %d", ErrTooManyFrames, maxFrames)
	}

	fi := FrameInfo{
		Data:        imageData,
		AlphaData:   alphaData,
		Width:       width,
		Height:      height,
		OffsetX:     offsetX,
		OffsetY:     offsetY,
		Duration:    duration,
		IsKeyframe:  len(d.frames) == 0,
		HasAlpha:    hasAlpha,
		BlendMode:   blend,
		DisposeMode: dispose,
	}
	d.frames = append(d.frames, fi)
	return nil
}

// parseSingleExtendedFrame parses a non-animated VP8X file's image data.
// payload starts at the ALPH or VP8/VP8L chunk.
func (d *Demuxer) parseSingleExtendedFrame(payload []byte) error {
	var imageData []byte
	var alphaData []byte

	pos := 0
	for pos+container.ChunkHeaderSize <= len(payload) {
		c, n, err := ReadChunk(payload[pos:])
		if err != nil {
			break
		}
		switch c.ID {
		case FourCCALPH:
			alphaData = c.Data
		case FourCCVP8:
			imageData = c.Data
		case FourCCVP8L:
			imageData = c.Data
		case FourCCEXIF, FourCCXMP, FourCCICCP:
			// Metadata — skip, already captured at top level.
		default:
			// Unknown or post-image chunk — stop scanning for image data.
		}
		if imageData != nil {
			break
		}
		pos += n
	}

	if imageData == nil {
		return ErrNoImage
	}
	hasAlpha := len(alphaData) > 0
	if !hasAlpha {
		hasAlpha = frameDataHasAlpha(imageData)
	}
	d.frames = []FrameInfo{{
		Data:       imageData,
		AlphaData:  alphaData,
		Width:      d.features.Width,
		Height:     d.features.Height,
		HasAlpha:   hasAlpha,
		IsKeyframe: true,
	}}
	return nil
}

// parseVP8Dimensions extracts width/height from a VP8 bitstream header.
func parseVP8Dimensions(data []byte) (int, int, error) {
	// VP8 keyframe: 3-byte frame tag, then 7 bytes of header.
	if len(data) < 10 {
		return 0, 0, ErrInvalidFrame
	}
	// Frame tag: byte 0 bit 0 = keyframe (0), bytes 1-2 ignored here.
	// Bytes 3-5: VP8 signature 0x9d 0x01 0x2a.
	if data[3] != 0x9d || data[4] != 0x01 || data[5] != 0x2a {
		return 0, 0, ErrInvalidFrame
	}
	width := int(binary.LittleEndian.Uint16(data[6:8])) & 0x3fff
	height := int(binary.LittleEndian.Uint16(data[8:10])) & 0x3fff
	return width, height, nil
}

// parseVP8LDimensions extracts width/height/alpha from a VP8L bitstream header.
func parseVP8LDimensions(data []byte) (int, int, bool, error) {
	// VP8L header: 1-byte signature (0x2f), then 4 bytes of packed width/height/alpha/version.
	if len(data) < 5 {
		return 0, 0, false, ErrInvalidFrame
	}
	if data[0] != container.VP8LMagicByte {
		return 0, 0, false, ErrInvalidFrame
	}
	bits := binary.LittleEndian.Uint32(data[1:5])
	width := int(bits&0x3fff) + 1
	height := int((bits>>14)&0x3fff) + 1
	hasAlpha := (bits >> 28) & 0x1
	return width, height, hasAlpha != 0, nil
}

// frameDataHasAlpha checks the raw bitstream data for an alpha flag.
// For VP8L (signature byte 0x2f), it reads the alpha bit from the header.
// For VP8 (lossy), the bitstream itself does not carry alpha; alpha comes
// from a separate ALPH chunk which the caller checks independently.
func frameDataHasAlpha(data []byte) bool {
	if len(data) < 5 {
		return false
	}
	// VP8L signature byte.
	if data[0] == container.VP8LMagicByte {
		bits := binary.LittleEndian.Uint32(data[1:5])
		return (bits>>28)&0x1 != 0
	}
	return false
}
