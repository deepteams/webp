package container

import (
	"encoding/binary"
	"fmt"
)

// ParseStatus indicates the result of incremental parsing.
type ParseStatus int

const (
	ParseOK           ParseStatus = iota
	ParseNeedMoreData             // not enough data yet
	ParseError                    // unrecoverable error
)

// FrameInfo holds per-frame metadata extracted from an ANMF chunk.
type FrameInfo struct {
	XOffset       int
	YOffset       int
	Width         int
	Height        int
	Duration      int // milliseconds
	DisposeMethod AnimDispose
	BlendMethod   AnimBlend
	HasAlpha      bool
	IsLossless    bool   // true if VP8L, false if VP8
	Payload       []byte // raw bitstream data (VP8/VP8L)
	AlphaData     []byte // raw ALPH chunk data (nil if no separate alpha)
}

// Parser performs incremental parsing of a WebP RIFF container.
// It processes the file in a single pass over the byte slice.
type Parser struct {
	features Features
	frames   []FrameInfo
	chunks   []Chunk // non-image metadata chunks (ICCP, EXIF, XMP, etc.)
}

// NewParser creates a parser and immediately parses the provided WebP data.
func NewParser(data []byte) (*Parser, error) {
	p := &Parser{}
	if err := p.parse(data); err != nil {
		return nil, err
	}
	return p, nil
}

// Features returns the parsed file features.
func (p *Parser) Features() Features { return p.features }

// Frames returns all parsed animation frames (or a single frame for stills).
func (p *Parser) Frames() []FrameInfo { return p.frames }

// Chunks returns non-image metadata chunks (ICCP, EXIF, XMP, etc.).
func (p *Parser) Chunks() []Chunk { return p.chunks }

// parse processes the complete WebP data buffer.
func (p *Parser) parse(data []byte) error {
	hdr, consumed, err := ParseRIFFHeader(data)
	if err != nil {
		return err
	}

	// Limit parsing to the declared RIFF size.
	riffEnd := int(hdr.FileSize) + ChunkHeaderSize
	if riffEnd > len(data) {
		riffEnd = len(data)
	}
	buf := data[consumed:riffEnd]

	if len(buf) < ChunkHeaderSize {
		return ErrTruncated
	}

	// Peek at the first chunk's FourCC to determine format.
	firstFourCC := binary.LittleEndian.Uint32(buf[0:4])

	switch firstFourCC {
	case FourCCVP8X:
		return p.parseVP8X(buf)
	case FourCCVP8:
		p.features.Format = FormatVP8
		return p.parseSingleImage(buf)
	case FourCCVP8L:
		p.features.Format = FormatVP8L
		return p.parseSingleImage(buf)
	default:
		return fmt.Errorf("%w: unexpected first chunk %s", ErrUnsupported, FourCCString(firstFourCC))
	}
}

// parseSingleImage parses a non-extended WebP file (simple VP8 or VP8L).
func (p *Parser) parseSingleImage(buf []byte) error {
	fourcc, payloadSize, err := ReadChunkHeader(buf)
	if err != nil {
		return err
	}
	padded := int(PaddedSize(payloadSize))
	if ChunkHeaderSize+padded > len(buf) {
		return ErrTruncated
	}

	payload := buf[ChunkHeaderSize : ChunkHeaderSize+int(payloadSize)]

	frame := FrameInfo{
		Payload:    payload,
		IsLossless: fourcc == FourCCVP8L,
	}

	// Extract dimensions from the bitstream header.
	if fourcc == FourCCVP8L {
		w, h, alpha, err := parseVP8LHeader(payload)
		if err != nil {
			return err
		}
		frame.Width = w
		frame.Height = h
		frame.HasAlpha = alpha
		p.features.HasAlpha = alpha
	} else {
		w, h, err := parseVP8Header(payload)
		if err != nil {
			return err
		}
		frame.Width = w
		frame.Height = h
	}

	p.features.Width = frame.Width
	p.features.Height = frame.Height
	p.features.CanvasWidth = frame.Width
	p.features.CanvasHeight = frame.Height
	p.frames = append(p.frames, frame)
	return nil
}

// parseVP8X parses an extended format WebP file.
func (p *Parser) parseVP8X(buf []byte) error {
	p.features.Format = FormatVP8X

	// Read VP8X chunk header.
	_, payloadSize, err := ReadChunkHeader(buf)
	if err != nil {
		return err
	}
	if payloadSize != uint32(VP8XChunkSize) {
		return ErrInvalidVP8X
	}

	padded := int(PaddedSize(payloadSize))
	if ChunkHeaderSize+padded > len(buf) {
		return ErrTruncated
	}

	payload := buf[ChunkHeaderSize : ChunkHeaderSize+int(payloadSize)]

	// Parse VP8X payload: 1 byte flags + 3 bytes reserved + 3 bytes width + 3 bytes height.
	flags := uint32(payload[0])
	if flags & ^AllValidFlags != 0 {
		return ErrInvalidFlags
	}

	p.features.HasAnim = flags&AnimationFlag != 0
	p.features.HasAlpha = flags&AlphaFlag != 0
	p.features.HasICCP = flags&ICCPFlag != 0
	p.features.HasEXIF = flags&EXIFFlag != 0
	p.features.HasXMP = flags&XMPFlag != 0

	// Canvas dimensions: 24-bit LE, stored as value-1.
	p.features.CanvasWidth = 1 + readLE24(payload[4:7])
	p.features.CanvasHeight = 1 + readLE24(payload[7:10])
	p.features.Width = p.features.CanvasWidth
	p.features.Height = p.features.CanvasHeight

	if uint64(p.features.CanvasWidth)*uint64(p.features.CanvasHeight) >= MaxImageArea {
		return ErrInvalidImage
	}

	// Advance past VP8X chunk.
	pos := ChunkHeaderSize + padded

	// Default animation values.
	p.features.LoopCount = 1
	p.features.BGColor = 0xFFFFFFFF

	// Parse remaining chunks.
	return p.parseVP8XChunks(buf[pos:])
}

// parseVP8XChunks iterates over the chunks following VP8X.
func (p *Parser) parseVP8XChunks(buf []byte) error {
	isAnim := p.features.HasAnim
	animChunks := 0

	for len(buf) >= ChunkHeaderSize {
		fourcc, payloadSize, err := ReadChunkHeader(buf)
		if err != nil {
			return err
		}
		padded := int(PaddedSize(payloadSize))
		chunkTotal := ChunkHeaderSize + padded
		if chunkTotal > len(buf) {
			return ErrTruncated
		}

		payload := buf[ChunkHeaderSize : ChunkHeaderSize+int(payloadSize)]

		switch fourcc {
		case FourCCVP8X:
			// Duplicate VP8X is an error.
			return ErrInvalidChunk

		case FourCCANIM:
			if int(payloadSize) < ANIMChunkSize {
				return ErrInvalidChunk
			}
			animChunks++
			p.features.BGColor = binary.LittleEndian.Uint32(payload[0:4])
			p.features.LoopCount = int(binary.LittleEndian.Uint16(payload[4:6]))

		case FourCCANMF:
			if animChunks == 0 {
				return ErrInvalidChunk // ANIM must precede ANMF
			}
			frame, err := parseANMF(payload)
			if err != nil {
				return err
			}
			p.frames = append(p.frames, frame)

		case FourCCVP8, FourCCVP8L:
			// In extended format, image data outside ANMF is only valid for stills.
			if animChunks > 0 || isAnim {
				return ErrInvalidChunk
			}
			return p.parseExtSingleImage(buf)

		case FourCCALPH:
			// Alpha before VP8 in extended format, only valid for stills.
			if animChunks > 0 || isAnim {
				return ErrInvalidChunk
			}
			return p.parseExtSingleImage(buf)

		case FourCCICCP:
			if p.features.HasICCP {
				p.chunks = append(p.chunks, Chunk{FourCC: fourcc, Payload: copyBytes(payload)})
			}

		case FourCCEXIF:
			if p.features.HasEXIF {
				p.chunks = append(p.chunks, Chunk{FourCC: fourcc, Payload: copyBytes(payload)})
			}

		case FourCCXMP:
			if p.features.HasXMP {
				p.chunks = append(p.chunks, Chunk{FourCC: fourcc, Payload: copyBytes(payload)})
			}

		default:
			// Unknown chunks are stored as-is for roundtrip fidelity.
			p.chunks = append(p.chunks, Chunk{FourCC: fourcc, Payload: copyBytes(payload)})
		}

		buf = buf[chunkTotal:]
	}

	return nil
}

// parseExtSingleImage parses a single image from an extended format file.
// buf starts at the ALPH or VP8/VP8L chunk.
func (p *Parser) parseExtSingleImage(buf []byte) error {
	var frame FrameInfo
	var alphPayload []byte

	for len(buf) >= ChunkHeaderSize {
		fourcc, payloadSize, err := ReadChunkHeader(buf)
		if err != nil {
			return err
		}
		padded := int(PaddedSize(payloadSize))
		chunkTotal := ChunkHeaderSize + padded
		if chunkTotal > len(buf) {
			return ErrTruncated
		}

		payload := buf[ChunkHeaderSize : ChunkHeaderSize+int(payloadSize)]

		switch fourcc {
		case FourCCALPH:
			alphPayload = payload
			frame.HasAlpha = true
			p.features.HasAlpha = true
			buf = buf[chunkTotal:]
			continue

		case FourCCVP8L:
			if alphPayload != nil {
				return ErrInvalidChunk // VP8L has its own alpha, no separate ALPH
			}
			w, h, alpha, err := parseVP8LHeader(payload)
			if err != nil {
				return err
			}
			frame.Width = w
			frame.Height = h
			frame.IsLossless = true
			if alpha {
				frame.HasAlpha = true
				p.features.HasAlpha = true
			}
			frame.Payload = payload
			p.features.Width = w
			p.features.Height = h
			p.frames = append(p.frames, frame)
			return nil

		case FourCCVP8:
			w, h, err := parseVP8Header(payload)
			if err != nil {
				return err
			}
			frame.Width = w
			frame.Height = h
			frame.IsLossless = false
			frame.Payload = payload
			frame.AlphaData = alphPayload
			p.features.Width = w
			p.features.Height = h
			p.frames = append(p.frames, frame)
			return nil

		default:
			// Not an image chunk, stop.
			break
		}
		break
	}

	return ErrInvalidChunk
}

// parseANMF parses an ANMF chunk payload into a FrameInfo.
func parseANMF(payload []byte) (FrameInfo, error) {
	if len(payload) < ANMFChunkSize {
		return FrameInfo{}, ErrInvalidChunk
	}

	frame := FrameInfo{
		XOffset:  2 * readLE24(payload[0:3]),
		YOffset:  2 * readLE24(payload[3:6]),
		Width:    1 + readLE24(payload[6:9]),
		Height:   1 + readLE24(payload[9:12]),
		Duration: readLE24(payload[12:15]),
	}

	bits := payload[15]
	if bits&1 != 0 {
		frame.DisposeMethod = DisposeBackground
	}
	if bits&2 != 0 {
		frame.BlendMethod = BlendNone
	}

	if uint64(frame.Width)*uint64(frame.Height) >= MaxImageArea {
		return FrameInfo{}, ErrInvalidImage
	}

	// Parse sub-chunks within the ANMF payload (after the 16-byte header).
	subBuf := payload[ANMFChunkSize:]
	return parseFrameSubChunks(frame, subBuf)
}

// parseFrameSubChunks parses the image data within an ANMF frame.
func parseFrameSubChunks(frame FrameInfo, buf []byte) (FrameInfo, error) {
	var alphPayload []byte

	for len(buf) >= ChunkHeaderSize {
		fourcc, payloadSize, err := ReadChunkHeader(buf)
		if err != nil {
			return FrameInfo{}, err
		}
		padded := int(PaddedSize(payloadSize))
		chunkTotal := ChunkHeaderSize + padded
		if chunkTotal > len(buf) {
			return FrameInfo{}, ErrTruncated
		}

		payload := buf[ChunkHeaderSize : ChunkHeaderSize+int(payloadSize)]

		switch fourcc {
		case FourCCALPH:
			alphPayload = payload
			frame.HasAlpha = true
			buf = buf[chunkTotal:]
			continue

		case FourCCVP8L:
			if alphPayload != nil {
				return FrameInfo{}, ErrInvalidChunk
			}
			w, h, alpha, err := parseVP8LHeader(payload)
			if err != nil {
				return FrameInfo{}, err
			}
			_ = w
			_ = h
			frame.IsLossless = true
			if alpha {
				frame.HasAlpha = true
			}
			frame.Payload = payload
			return frame, nil

		case FourCCVP8:
			frame.IsLossless = false
			frame.Payload = payload
			frame.AlphaData = alphPayload
			return frame, nil

		default:
			break
		}
		break
	}

	// A frame with only alpha and no image is invalid, but a frame
	// with no sub-chunks might be a partially streamed file.
	if alphPayload != nil {
		return FrameInfo{}, ErrInvalidChunk
	}
	return frame, nil
}

// parseVP8Header extracts width and height from a VP8 lossy bitstream header.
// Minimal parsing: 10-byte frame header containing the VP8 signature.
func parseVP8Header(data []byte) (width, height int, err error) {
	if len(data) < VP8FrameHeaderSize {
		return 0, 0, ErrTruncated
	}

	// First 3 bytes: frame tag (keyframe info, version, show, partition size).
	frameTag := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
	isKeyframe := (frameTag & 1) == 0
	if !isKeyframe {
		return 0, 0, fmt.Errorf("webp: VP8 non-keyframe not supported")
	}

	// Bytes 3-5: VP8 signature (0x9D 0x01 0x2A), read as big-endian.
	sig := uint32(data[3])<<16 | uint32(data[4])<<8 | uint32(data[5])
	if sig != VP8Signature {
		return 0, 0, fmt.Errorf("webp: invalid VP8 signature: 0x%06x", sig)
	}

	// Bytes 6-9: width (14 bits + 2 bits scale) and height (14 bits + 2 bits scale).
	width = int(binary.LittleEndian.Uint16(data[6:8])) & 0x3FFF
	height = int(binary.LittleEndian.Uint16(data[8:10])) & 0x3FFF
	if width == 0 || height == 0 {
		return 0, 0, ErrInvalidImage
	}

	return width, height, nil
}

// parseVP8LHeader extracts width, height, and alpha presence from a VP8L
// lossless bitstream header.
func parseVP8LHeader(data []byte) (width, height int, hasAlpha bool, err error) {
	if len(data) < VP8LFrameHeaderSize {
		return 0, 0, false, ErrTruncated
	}

	// First byte: VP8L signature byte (0x2F).
	if data[0] != VP8LMagicByte {
		return 0, 0, false, fmt.Errorf("webp: invalid VP8L signature: 0x%02x", data[0])
	}

	// Bytes 1-4: 32-bit LE containing width-1, height-1, alpha, version.
	bits := binary.LittleEndian.Uint32(data[1:5])
	width = int(bits&0x3FFF) + 1         // 14 bits
	height = int((bits>>14)&0x3FFF) + 1  // 14 bits
	hasAlpha = (bits>>28)&1 != 0         // 1 bit
	version := (bits >> 29) & 0x7        // 3 bits
	if version != VP8LVersion {
		return 0, 0, false, fmt.Errorf("webp: unsupported VP8L version: %d", version)
	}
	if width == 0 || height == 0 {
		return 0, 0, false, ErrInvalidImage
	}

	return width, height, hasAlpha, nil
}

// readLE24 reads a 24-bit little-endian integer from 3 bytes.
func readLE24(b []byte) int {
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16
}

// copyBytes returns a copy of the slice to avoid retaining the original buffer.
func copyBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
