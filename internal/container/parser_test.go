package container

import (
	"encoding/binary"
	"testing"
)

func TestParseRIFFHeader_Valid(t *testing.T) {
	data := make([]byte, 20)
	binary.LittleEndian.PutUint32(data[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(data[4:8], 100) // file size
	binary.LittleEndian.PutUint32(data[8:12], FourCCWEBP)

	hdr, n, err := ParseRIFFHeader(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != RIFFHeaderSize {
		t.Fatalf("consumed %d bytes, want %d", n, RIFFHeaderSize)
	}
	if hdr.FileSize != 100 {
		t.Fatalf("file size = %d, want 100", hdr.FileSize)
	}
}

func TestParseRIFFHeader_TooShort(t *testing.T) {
	_, _, err := ParseRIFFHeader([]byte{0, 1, 2})
	if err != ErrTruncated {
		t.Fatalf("expected ErrTruncated, got %v", err)
	}
}

func TestParseRIFFHeader_BadRIFF(t *testing.T) {
	data := make([]byte, 12)
	copy(data[0:4], "JUNK")
	_, _, err := ParseRIFFHeader(data)
	if err != ErrInvalidRIFF {
		t.Fatalf("expected ErrInvalidRIFF, got %v", err)
	}
}

func TestParseRIFFHeader_BadWEBP(t *testing.T) {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(data[4:8], 100)
	copy(data[8:12], "JUNK")
	_, _, err := ParseRIFFHeader(data)
	if err != ErrInvalidWebP {
		t.Fatalf("expected ErrInvalidWebP, got %v", err)
	}
}

func TestReadChunkHeader(t *testing.T) {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:4], FourCCVP8)
	binary.LittleEndian.PutUint32(data[4:8], 42)

	fourcc, size, err := ReadChunkHeader(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fourcc != FourCCVP8 {
		t.Fatalf("fourcc = 0x%08x, want VP8", fourcc)
	}
	if size != 42 {
		t.Fatalf("size = %d, want 42", size)
	}
}

func TestPaddedSize(t *testing.T) {
	tests := []struct {
		in, want uint32
	}{
		{0, 0},
		{1, 2},
		{2, 2},
		{3, 4},
		{100, 100},
		{101, 102},
	}
	for _, tt := range tests {
		got := PaddedSize(tt.in)
		if got != tt.want {
			t.Errorf("PaddedSize(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFourCCString(t *testing.T) {
	if s := FourCCString(FourCCVP8); s != "VP8 " {
		t.Fatalf("FourCCString(VP8) = %q, want %q", s, "VP8 ")
	}
	if s := FourCCString(FourCCVP8L); s != "VP8L" {
		t.Fatalf("FourCCString(VP8L) = %q, want %q", s, "VP8L")
	}
}

func TestParseVP8Header(t *testing.T) {
	// Build a minimal VP8 keyframe header: 10 bytes.
	data := make([]byte, 10)
	// Frame tag: keyframe (bit0=0), version=0, show_frame=1, partition0_size=0
	// Byte 0: 0x10 => show_frame bit set (bit 4), keyframe (bit0=0)
	data[0] = 0x10 // show=1, keyframe=0
	data[1] = 0x00
	data[2] = 0x00
	// VP8 signature
	data[3] = 0x9d
	data[4] = 0x01
	data[5] = 0x2a
	// Width: 320 (14 bits LE)
	binary.LittleEndian.PutUint16(data[6:8], 320)
	// Height: 240 (14 bits LE)
	binary.LittleEndian.PutUint16(data[8:10], 240)

	w, h, err := parseVP8Header(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 320 || h != 240 {
		t.Fatalf("dimensions = %dx%d, want 320x240", w, h)
	}
}

func TestParseVP8LHeader(t *testing.T) {
	// Build a VP8L header: 5 bytes.
	data := make([]byte, 5)
	data[0] = VP8LMagicByte // 0x2F

	// Bits 1-4: width-1 (14 bits) | height-1 (14 bits) | alpha (1 bit) | version (3 bits)
	// width=100 => width-1=99, height=200 => height-1=199, alpha=true, version=0
	bits := uint32(99) | (uint32(199) << 14) | (1 << 28) | (0 << 29)
	binary.LittleEndian.PutUint32(data[1:5], bits)

	w, h, alpha, err := parseVP8LHeader(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 100 || h != 200 {
		t.Fatalf("dimensions = %dx%d, want 100x200", w, h)
	}
	if !alpha {
		t.Fatal("expected alpha=true")
	}
}

// buildSimpleVP8WebP builds a minimal valid WebP file with a VP8 chunk.
func buildSimpleVP8WebP(width, height int) []byte {
	// VP8 bitstream header (10 bytes).
	vp8 := make([]byte, 10)
	vp8[0] = 0x10 // keyframe, show
	vp8[3] = 0x9d
	vp8[4] = 0x01
	vp8[5] = 0x2a
	binary.LittleEndian.PutUint16(vp8[6:8], uint16(width))
	binary.LittleEndian.PutUint16(vp8[8:10], uint16(height))

	// VP8 chunk: 8 header + 10 payload = 18.
	chunkSize := uint32(len(vp8))
	paddedChunkSize := PaddedSize(chunkSize)
	chunkData := make([]byte, ChunkHeaderSize+paddedChunkSize)
	binary.LittleEndian.PutUint32(chunkData[0:4], FourCCVP8)
	binary.LittleEndian.PutUint32(chunkData[4:8], chunkSize)
	copy(chunkData[ChunkHeaderSize:], vp8)

	// RIFF header: 12 bytes.
	riffPayload := 4 + uint32(len(chunkData)) // "WEBP" + chunk
	out := make([]byte, RIFFHeaderSize+len(chunkData))
	binary.LittleEndian.PutUint32(out[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(out[4:8], riffPayload)
	binary.LittleEndian.PutUint32(out[8:12], FourCCWEBP)
	copy(out[RIFFHeaderSize:], chunkData)
	return out
}

func TestParserSimpleVP8(t *testing.T) {
	data := buildSimpleVP8WebP(640, 480)

	p, err := NewParser(data)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	f := p.Features()
	if f.Format != FormatVP8 {
		t.Fatalf("format = %v, want VP8", f.Format)
	}
	if f.Width != 640 || f.Height != 480 {
		t.Fatalf("dimensions = %dx%d, want 640x480", f.Width, f.Height)
	}
	if f.HasAlpha {
		t.Fatal("unexpected alpha")
	}
	if f.HasAnim {
		t.Fatal("unexpected animation")
	}

	frames := p.Frames()
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].Width != 640 || frames[0].Height != 480 {
		t.Fatalf("frame dimensions = %dx%d, want 640x480", frames[0].Width, frames[0].Height)
	}
}

// buildSimpleVP8LWebP builds a minimal valid WebP file with a VP8L chunk.
func buildSimpleVP8LWebP(width, height int, alpha bool) []byte {
	// VP8L bitstream header (5 bytes).
	vp8l := make([]byte, 5)
	vp8l[0] = VP8LMagicByte
	bits := uint32(width-1) | (uint32(height-1) << 14)
	if alpha {
		bits |= 1 << 28
	}
	binary.LittleEndian.PutUint32(vp8l[1:5], bits)

	chunkSize := uint32(len(vp8l))
	paddedChunkSize := PaddedSize(chunkSize)
	chunkData := make([]byte, ChunkHeaderSize+paddedChunkSize)
	binary.LittleEndian.PutUint32(chunkData[0:4], FourCCVP8L)
	binary.LittleEndian.PutUint32(chunkData[4:8], chunkSize)
	copy(chunkData[ChunkHeaderSize:], vp8l)

	riffPayload := 4 + uint32(len(chunkData))
	out := make([]byte, RIFFHeaderSize+len(chunkData))
	binary.LittleEndian.PutUint32(out[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(out[4:8], riffPayload)
	binary.LittleEndian.PutUint32(out[8:12], FourCCWEBP)
	copy(out[RIFFHeaderSize:], chunkData)
	return out
}

func TestParserSimpleVP8L(t *testing.T) {
	data := buildSimpleVP8LWebP(256, 128, true)

	p, err := NewParser(data)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	f := p.Features()
	if f.Format != FormatVP8L {
		t.Fatalf("format = %v, want VP8L", f.Format)
	}
	if f.Width != 256 || f.Height != 128 {
		t.Fatalf("dimensions = %dx%d, want 256x128", f.Width, f.Height)
	}
	if !f.HasAlpha {
		t.Fatal("expected alpha")
	}
}

func TestParserVP8X_Still(t *testing.T) {
	// Build a VP8X extended file with ICCP and a VP8 image.
	width, height := 320, 240

	// VP8X payload (10 bytes): flags + reserved + canvas W-1 + canvas H-1.
	vp8x := make([]byte, VP8XChunkSize)
	vp8x[0] = byte(ICCPFlag) // ICCP flag set
	// Canvas W-1 (24 bits LE)
	vp8x[4] = byte((width - 1))
	vp8x[5] = byte((width - 1) >> 8)
	vp8x[6] = byte((width - 1) >> 16)
	// Canvas H-1 (24 bits LE)
	vp8x[7] = byte((height - 1))
	vp8x[8] = byte((height - 1) >> 8)
	vp8x[9] = byte((height - 1) >> 16)

	vp8xChunk := makeChunk(FourCCVP8X, vp8x)

	// ICCP chunk with dummy data.
	iccpChunk := makeChunk(FourCCICCP, []byte("fake-icc-profile"))

	// VP8 bitstream.
	vp8Hdr := make([]byte, 10)
	vp8Hdr[0] = 0x10
	vp8Hdr[3] = 0x9d
	vp8Hdr[4] = 0x01
	vp8Hdr[5] = 0x2a
	binary.LittleEndian.PutUint16(vp8Hdr[6:8], uint16(width))
	binary.LittleEndian.PutUint16(vp8Hdr[8:10], uint16(height))
	vp8Chunk := makeChunk(FourCCVP8, vp8Hdr)

	payload := concat(vp8xChunk, iccpChunk, vp8Chunk)
	data := wrapRIFF(payload)

	p, err := NewParser(data)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	f := p.Features()
	if f.Format != FormatVP8X {
		t.Fatalf("format = %v, want VP8X", f.Format)
	}
	if f.CanvasWidth != width || f.CanvasHeight != height {
		t.Fatalf("canvas = %dx%d, want %dx%d", f.CanvasWidth, f.CanvasHeight, width, height)
	}
	if !f.HasICCP {
		t.Fatal("expected ICCP flag")
	}
	if len(p.Chunks()) != 1 {
		t.Fatalf("got %d metadata chunks, want 1", len(p.Chunks()))
	}
	if len(p.Frames()) != 1 {
		t.Fatalf("got %d frames, want 1", len(p.Frames()))
	}
}

func TestReadLE24(t *testing.T) {
	b := []byte{0x56, 0x34, 0x12}
	got := readLE24(b)
	if got != 0x123456 {
		t.Fatalf("readLE24 = 0x%x, want 0x123456", got)
	}
}

// -- test helpers --

func makeChunk(fourcc uint32, payload []byte) []byte {
	size := uint32(len(payload))
	padded := PaddedSize(size)
	out := make([]byte, ChunkHeaderSize+padded)
	binary.LittleEndian.PutUint32(out[0:4], fourcc)
	binary.LittleEndian.PutUint32(out[4:8], size)
	copy(out[ChunkHeaderSize:], payload)
	return out
}

func wrapRIFF(chunks []byte) []byte {
	riffPayload := 4 + uint32(len(chunks)) // "WEBP" + chunks
	out := make([]byte, RIFFHeaderSize+len(chunks))
	binary.LittleEndian.PutUint32(out[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(out[4:8], riffPayload)
	binary.LittleEndian.PutUint32(out[8:12], FourCCWEBP)
	copy(out[RIFFHeaderSize:], chunks)
	return out
}

func concat(slices ...[]byte) []byte {
	total := 0
	for _, s := range slices {
		total += len(s)
	}
	out := make([]byte, 0, total)
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}
