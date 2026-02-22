package mux

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/deepteams/webp/internal/container"
)

// --- Chunk tests ---

func TestFourCCString(t *testing.T) {
	tests := []struct {
		id   ChunkID
		want string
	}{
		{FourCCVP8, "VP8 "},
		{FourCCVP8L, "VP8L"},
		{FourCCVP8X, "VP8X"},
		{FourCCALPH, "ALPH"},
		{FourCCANIM, "ANIM"},
		{FourCCANMF, "ANMF"},
		{FourCCICCP, "ICCP"},
		{FourCCEXIF, "EXIF"},
		{FourCCXMP, "XMP "},
	}
	for _, tt := range tests {
		got := fourCCString(tt.id)
		if got != tt.want {
			t.Errorf("fourCCString(0x%08x) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestReadChunkHeader(t *testing.T) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], FourCCVP8L)
	binary.LittleEndian.PutUint32(buf[4:8], 42)

	id, size, err := ReadChunkHeader(buf)
	if err != nil {
		t.Fatalf("ReadChunkHeader: %v", err)
	}
	if id != FourCCVP8L {
		t.Errorf("id = 0x%08x, want VP8L 0x%08x", id, FourCCVP8L)
	}
	if size != 42 {
		t.Errorf("size = %d, want 42", size)
	}
}

func TestReadChunkHeaderTooShort(t *testing.T) {
	_, _, err := ReadChunkHeader([]byte{1, 2, 3})
	if err != ErrInvalidChunkHeader {
		t.Errorf("expected ErrInvalidChunkHeader, got %v", err)
	}
}

func TestReadChunk(t *testing.T) {
	payload := []byte("hello")
	buf := make([]byte, container.ChunkHeaderSize+len(payload)+1) // +1 for padding
	writeChunkHeader(buf[0:8], FourCCEXIF, uint32(len(payload)))
	copy(buf[8:], payload)
	buf[13] = 0 // padding byte

	c, consumed, err := ReadChunk(buf)
	if err != nil {
		t.Fatalf("ReadChunk: %v", err)
	}
	if c.ID != FourCCEXIF {
		t.Errorf("chunk ID = 0x%08x, want EXIF", c.ID)
	}
	if c.Size != 5 {
		t.Errorf("chunk Size = %d, want 5", c.Size)
	}
	if !bytes.Equal(c.Data, payload) {
		t.Errorf("chunk Data = %q, want %q", c.Data, payload)
	}
	// 8 header + 5 payload + 1 padding = 14
	if consumed != 14 {
		t.Errorf("consumed = %d, want 14", consumed)
	}
}

func TestReadChunkEvenPayload(t *testing.T) {
	payload := []byte("hi")
	buf := make([]byte, container.ChunkHeaderSize+len(payload))
	writeChunkHeader(buf[0:8], FourCCEXIF, uint32(len(payload)))
	copy(buf[8:], payload)

	_, consumed, err := ReadChunk(buf)
	if err != nil {
		t.Fatalf("ReadChunk: %v", err)
	}
	// 8 header + 2 payload, no padding
	if consumed != 10 {
		t.Errorf("consumed = %d, want 10", consumed)
	}
}

// --- Helper to build raw VP8 frame data ---

func makeVP8Keyframe(width, height int) []byte {
	// Minimal VP8 keyframe: 3-byte frame tag + signature + dimensions.
	data := make([]byte, 10)
	data[0] = 0 // keyframe bit=0
	data[1] = 0
	data[2] = 0
	data[3] = 0x9d // VP8 signature
	data[4] = 0x01
	data[5] = 0x2a
	binary.LittleEndian.PutUint16(data[6:8], uint16(width))
	binary.LittleEndian.PutUint16(data[8:10], uint16(height))
	return data
}

func makeVP8LData(width, height int, hasAlpha bool) []byte {
	data := make([]byte, 5)
	data[0] = container.VP8LMagicByte
	bits := uint32(width-1) | uint32(height-1)<<14
	if hasAlpha {
		bits |= 1 << 28
	}
	binary.LittleEndian.PutUint32(data[1:5], bits)
	return data
}

// --- Helper to build a minimal WebP file ---

func buildSimpleWebP(bitstreamChunkID ChunkID, bitstreamData []byte) []byte {
	chunkSize := uint32(len(bitstreamData))
	paddedSize := chunkSize
	if chunkSize%2 != 0 {
		paddedSize++
	}
	riffPayload := 4 + container.ChunkHeaderSize + paddedSize
	total := container.RIFFHeaderSize + container.ChunkHeaderSize + int(paddedSize)
	buf := make([]byte, total)

	binary.LittleEndian.PutUint32(buf[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(riffPayload))
	binary.LittleEndian.PutUint32(buf[8:12], FourCCWEBP)
	writeChunkHeader(buf[12:20], bitstreamChunkID, chunkSize)
	copy(buf[20:], bitstreamData)
	return buf
}

func buildVP8XWebP(flags byte, canvasW, canvasH int, chunks ...Chunk) []byte {
	var buf bytes.Buffer

	// Placeholder for RIFF header — written at end.
	buf.Write(make([]byte, container.RIFFHeaderSize))

	// VP8X chunk.
	vp8x := make([]byte, container.ChunkHeaderSize+container.VP8XChunkSize)
	writeChunkHeader(vp8x[0:8], FourCCVP8X, container.VP8XChunkSize)
	vp8x[8] = flags
	putLE24(vp8x[12:15], canvasW-1)
	putLE24(vp8x[15:18], canvasH-1)
	buf.Write(vp8x)

	// Write additional chunks.
	for _, c := range chunks {
		hdr := make([]byte, container.ChunkHeaderSize)
		writeChunkHeader(hdr, c.ID, c.Size)
		buf.Write(hdr)
		buf.Write(c.Data)
		if c.Size%2 != 0 {
			buf.WriteByte(0)
		}
	}

	data := buf.Bytes()
	riffPayload := uint32(len(data) - 8) // total - "RIFF" - size field
	binary.LittleEndian.PutUint32(data[0:4], FourCCRIFF)
	binary.LittleEndian.PutUint32(data[4:8], riffPayload)
	binary.LittleEndian.PutUint32(data[8:12], FourCCWEBP)
	return data
}

// --- Demux tests ---

func TestDemuxSimpleVP8(t *testing.T) {
	bs := makeVP8Keyframe(320, 240)
	webp := buildSimpleWebP(FourCCVP8, bs)

	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	feat := d.GetFeatures()
	if feat.Width != 320 || feat.Height != 240 {
		t.Errorf("dimensions = %dx%d, want 320x240", feat.Width, feat.Height)
	}
	if feat.Format != FormatLossy {
		t.Errorf("format = %v, want FormatLossy", feat.Format)
	}
	if feat.HasAlpha {
		t.Error("HasAlpha should be false")
	}
	if d.NumFrames() != 1 {
		t.Errorf("NumFrames = %d, want 1", d.NumFrames())
	}

	fi, err := d.Frame(0)
	if err != nil {
		t.Fatalf("Frame(0): %v", err)
	}
	if fi.Width != 320 || fi.Height != 240 {
		t.Errorf("frame dimensions = %dx%d, want 320x240", fi.Width, fi.Height)
	}
	if !fi.IsKeyframe {
		t.Error("frame should be keyframe")
	}
}

func TestDemuxSimpleVP8L(t *testing.T) {
	bs := makeVP8LData(128, 64, true)
	webp := buildSimpleWebP(FourCCVP8L, bs)

	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	feat := d.GetFeatures()
	if feat.Width != 128 || feat.Height != 64 {
		t.Errorf("dimensions = %dx%d, want 128x64", feat.Width, feat.Height)
	}
	if feat.Format != FormatLossless {
		t.Errorf("format = %v, want FormatLossless", feat.Format)
	}
	if !feat.HasAlpha {
		t.Error("HasAlpha should be true")
	}
}

func TestDemuxExtendedWithMetadata(t *testing.T) {
	bs := makeVP8Keyframe(640, 480)
	iccPayload := []byte("fake-icc-profile-data")
	exifPayload := []byte("fake-exif")

	chunks := []Chunk{
		{ID: FourCCICCP, Size: uint32(len(iccPayload)), Data: iccPayload},
		{ID: FourCCVP8, Size: uint32(len(bs)), Data: bs},
		{ID: FourCCEXIF, Size: uint32(len(exifPayload)), Data: exifPayload},
	}
	flags := byte(flagICCP | flagEXIF)
	webp := buildVP8XWebP(flags, 640, 480, chunks...)

	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	feat := d.GetFeatures()
	if feat.Width != 640 || feat.Height != 480 {
		t.Errorf("canvas = %dx%d, want 640x480", feat.Width, feat.Height)
	}
	if feat.Format != FormatExtended {
		t.Errorf("format = %v, want FormatExtended", feat.Format)
	}
	if !feat.HasICC {
		t.Error("HasICC should be true")
	}
	if !feat.HasEXIF {
		t.Error("HasEXIF should be true")
	}

	icc, err := d.GetChunk(FourCCICCP)
	if err != nil {
		t.Fatalf("GetChunk(ICCP): %v", err)
	}
	if !bytes.Equal(icc, iccPayload) {
		t.Errorf("ICC = %q, want %q", icc, iccPayload)
	}

	exif, err := d.GetChunk(FourCCEXIF)
	if err != nil {
		t.Fatalf("GetChunk(EXIF): %v", err)
	}
	if !bytes.Equal(exif, exifPayload) {
		t.Errorf("EXIF = %q, want %q", exif, exifPayload)
	}

	_, err = d.GetChunk(FourCCXMP)
	if err != ErrChunkNotFound {
		t.Errorf("GetChunk(XMP): expected ErrChunkNotFound, got %v", err)
	}
}

func TestDemuxAnimatedFrames(t *testing.T) {
	frame1 := makeVP8Keyframe(100, 100)
	frame2 := makeVP8Keyframe(100, 100)

	// Build ANIM chunk.
	animData := make([]byte, container.ANIMChunkSize)
	binary.LittleEndian.PutUint32(animData[0:4], 0xFFFFFFFF) // bg color
	binary.LittleEndian.PutUint16(animData[4:6], 0)          // loop = infinite

	// Build ANMF chunks.
	anmf1 := buildANMFData(0, 0, 100, 100, 50, BlendAlpha, DisposeNone, FourCCVP8, frame1)
	anmf2 := buildANMFData(0, 0, 100, 100, 100, BlendNone, DisposeBackground, FourCCVP8, frame2)

	chunks := []Chunk{
		{ID: FourCCANIM, Size: uint32(len(animData)), Data: animData},
		{ID: FourCCANMF, Size: uint32(len(anmf1)), Data: anmf1},
		{ID: FourCCANMF, Size: uint32(len(anmf2)), Data: anmf2},
	}
	flags := byte(flagAnimation)
	webp := buildVP8XWebP(flags, 100, 100, chunks...)

	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	feat := d.GetFeatures()
	if !feat.HasAnimation {
		t.Error("HasAnimation should be true")
	}
	if d.NumFrames() != 2 {
		t.Fatalf("NumFrames = %d, want 2", d.NumFrames())
	}
	if d.LoopCount() != 0 {
		t.Errorf("LoopCount = %d, want 0", d.LoopCount())
	}
	if d.BackgroundColor() != 0xFFFFFFFF {
		t.Errorf("BackgroundColor = 0x%08x, want 0xFFFFFFFF", d.BackgroundColor())
	}

	fi1, err := d.Frame(0)
	if err != nil {
		t.Fatalf("Frame(0): %v", err)
	}
	if fi1.Duration != 50 {
		t.Errorf("frame 0 duration = %d, want 50", fi1.Duration)
	}
	if fi1.Width != 100 || fi1.Height != 100 {
		t.Errorf("frame 0 size = %dx%d, want 100x100", fi1.Width, fi1.Height)
	}
	if fi1.BlendMode != BlendAlpha {
		t.Errorf("frame 0 blend = %d, want BlendAlpha", fi1.BlendMode)
	}
	if fi1.DisposeMode != DisposeNone {
		t.Errorf("frame 0 dispose = %d, want DisposeNone", fi1.DisposeMode)
	}

	fi2, err := d.Frame(1)
	if err != nil {
		t.Fatalf("Frame(1): %v", err)
	}
	if fi2.Duration != 100 {
		t.Errorf("frame 1 duration = %d, want 100", fi2.Duration)
	}
	if fi2.BlendMode != BlendNone {
		t.Errorf("frame 1 blend = %d, want BlendNone", fi2.BlendMode)
	}
	if fi2.DisposeMode != DisposeBackground {
		t.Errorf("frame 1 dispose = %d, want DisposeBackground", fi2.DisposeMode)
	}
}

func TestDemuxFrameIterator(t *testing.T) {
	frame1 := makeVP8Keyframe(50, 50)
	frame2 := makeVP8Keyframe(50, 50)

	animData := make([]byte, container.ANIMChunkSize)
	anmf1 := buildANMFData(0, 0, 50, 50, 10, BlendAlpha, DisposeNone, FourCCVP8, frame1)
	anmf2 := buildANMFData(0, 0, 50, 50, 20, BlendAlpha, DisposeNone, FourCCVP8, frame2)

	chunks := []Chunk{
		{ID: FourCCANIM, Size: uint32(len(animData)), Data: animData},
		{ID: FourCCANMF, Size: uint32(len(anmf1)), Data: anmf1},
		{ID: FourCCANMF, Size: uint32(len(anmf2)), Data: anmf2},
	}
	webp := buildVP8XWebP(byte(flagAnimation), 50, 50, chunks...)

	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	it := d.NewFrameIterator()
	count := 0
	for it.HasNext() {
		fi, err := it.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if fi == nil {
			t.Fatal("Next returned nil frame")
		}
		count++
	}
	if count != 2 {
		t.Errorf("iterator yielded %d frames, want 2", count)
	}

	// Next after exhaustion.
	_, err = it.Next()
	if err != ErrFrameOutRange {
		t.Errorf("expected ErrFrameOutRange, got %v", err)
	}
}

func TestDemuxInvalidRIFF(t *testing.T) {
	_, err := NewDemuxer([]byte{1, 2, 3, 4})
	if err != ErrInvalidRIFF {
		t.Errorf("expected ErrInvalidRIFF, got %v", err)
	}
}

func TestDemuxFrameOutOfRange(t *testing.T) {
	bs := makeVP8Keyframe(1, 1)
	webp := buildSimpleWebP(FourCCVP8, bs)

	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	_, err = d.Frame(-1)
	if err != ErrFrameOutRange {
		t.Errorf("Frame(-1): expected ErrFrameOutRange, got %v", err)
	}
	_, err = d.Frame(1)
	if err != ErrFrameOutRange {
		t.Errorf("Frame(1): expected ErrFrameOutRange, got %v", err)
	}
}

// --- Mux tests ---

func TestMuxSimpleVP8(t *testing.T) {
	bs := makeVP8Keyframe(320, 240)

	m := NewMuxer()
	if err := m.AddFrame(bs, nil); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}

	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// Verify by demuxing.
	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("Demux roundtrip: %v", err)
	}
	feat := d.GetFeatures()
	if feat.Width != 320 || feat.Height != 240 {
		t.Errorf("roundtrip dimensions = %dx%d, want 320x240", feat.Width, feat.Height)
	}
	if feat.Format != FormatLossy {
		t.Errorf("roundtrip format = %v, want FormatLossy", feat.Format)
	}
}

func TestMuxSimpleVP8L(t *testing.T) {
	bs := makeVP8LData(128, 64, false)

	m := NewMuxer()
	if err := m.AddFrame(bs, nil); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}

	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("Demux roundtrip: %v", err)
	}
	feat := d.GetFeatures()
	if feat.Width != 128 || feat.Height != 64 {
		t.Errorf("roundtrip dimensions = %dx%d, want 128x64", feat.Width, feat.Height)
	}
	if feat.Format != FormatLossless {
		t.Errorf("roundtrip format = %v, want FormatLossless", feat.Format)
	}
}

func TestMuxWithMetadata(t *testing.T) {
	bs := makeVP8Keyframe(640, 480)
	iccData := []byte("fake-icc-profile")
	exifData := []byte("fake-exif-data")
	xmpData := []byte("fake-xmp-data")

	m := NewMuxer()
	m.SetICCProfile(iccData)
	m.SetEXIF(exifData)
	m.SetXMP(xmpData)
	if err := m.AddFrame(bs, nil); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}

	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("Demux roundtrip: %v", err)
	}

	feat := d.GetFeatures()
	if feat.Format != FormatExtended {
		t.Errorf("format = %v, want FormatExtended", feat.Format)
	}
	if !feat.HasICC {
		t.Error("HasICC should be true")
	}
	if !feat.HasEXIF {
		t.Error("HasEXIF should be true")
	}
	if !feat.HasXMP {
		t.Error("HasXMP should be true")
	}

	icc, err := d.GetChunk(FourCCICCP)
	if err != nil {
		t.Fatalf("GetChunk(ICCP): %v", err)
	}
	if !bytes.Equal(icc, iccData) {
		t.Errorf("ICC roundtrip mismatch")
	}

	exif, err := d.GetChunk(FourCCEXIF)
	if err != nil {
		t.Fatalf("GetChunk(EXIF): %v", err)
	}
	if !bytes.Equal(exif, exifData) {
		t.Errorf("EXIF roundtrip mismatch")
	}

	xmp, err := d.GetChunk(FourCCXMP)
	if err != nil {
		t.Fatalf("GetChunk(XMP): %v", err)
	}
	if !bytes.Equal(xmp, xmpData) {
		t.Errorf("XMP roundtrip mismatch")
	}
}

func TestMuxAnimated(t *testing.T) {
	frame1 := makeVP8Keyframe(100, 100)
	frame2 := makeVP8Keyframe(100, 100)

	m := NewMuxer()
	m.SetLoopCount(3)
	m.SetBackgroundColor(0xFF000000)

	if err := m.AddFrame(frame1, &FrameOptions{Duration: 50}); err != nil {
		t.Fatalf("AddFrame 1: %v", err)
	}
	if err := m.AddFrame(frame2, &FrameOptions{Duration: 100, BlendMode: BlendNone, DisposeMode: DisposeBackground}); err != nil {
		t.Fatalf("AddFrame 2: %v", err)
	}

	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// Verify by demuxing.
	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("Demux animated roundtrip: %v", err)
	}

	feat := d.GetFeatures()
	if !feat.HasAnimation {
		t.Error("HasAnimation should be true")
	}
	if d.NumFrames() != 2 {
		t.Fatalf("NumFrames = %d, want 2", d.NumFrames())
	}
	if d.LoopCount() != 3 {
		t.Errorf("LoopCount = %d, want 3", d.LoopCount())
	}
	if d.BackgroundColor() != 0xFF000000 {
		t.Errorf("BackgroundColor = 0x%08x, want 0xFF000000", d.BackgroundColor())
	}

	fi1, _ := d.Frame(0)
	if fi1.Duration != 50 {
		t.Errorf("frame 0 duration = %d, want 50", fi1.Duration)
	}

	fi2, _ := d.Frame(1)
	if fi2.Duration != 100 {
		t.Errorf("frame 1 duration = %d, want 100", fi2.Duration)
	}
	if fi2.BlendMode != BlendNone {
		t.Errorf("frame 1 blend = %d, want BlendNone", fi2.BlendMode)
	}
	if fi2.DisposeMode != DisposeBackground {
		t.Errorf("frame 1 dispose = %d, want DisposeBackground", fi2.DisposeMode)
	}
}

func TestMuxNoFrames(t *testing.T) {
	m := NewMuxer()
	var buf bytes.Buffer
	err := m.Assemble(&buf)
	if err != ErrNoFrames {
		t.Errorf("expected ErrNoFrames, got %v", err)
	}
}

func TestMuxEmptyFrame(t *testing.T) {
	m := NewMuxer()
	err := m.AddFrame(nil, nil)
	if err != ErrFrameEmpty {
		t.Errorf("expected ErrFrameEmpty, got %v", err)
	}
}

func TestMuxAddChunk(t *testing.T) {
	m := NewMuxer()
	if err := m.AddChunk(FourCCICCP, []byte("icc")); err != nil {
		t.Fatal(err)
	}
	if err := m.AddChunk(FourCCEXIF, []byte("exif")); err != nil {
		t.Fatal(err)
	}
	if err := m.AddChunk(FourCCXMP, []byte("xmp")); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(m.iccData, []byte("icc")) {
		t.Error("AddChunk(ICCP) did not set iccData")
	}
	if !bytes.Equal(m.exifData, []byte("exif")) {
		t.Error("AddChunk(EXIF) did not set exifData")
	}
	if !bytes.Equal(m.xmpData, []byte("xmp")) {
		t.Error("AddChunk(XMP) did not set xmpData")
	}
}

func TestFormatString(t *testing.T) {
	tests := []struct {
		f    Format
		want string
	}{
		{FormatLossy, "VP8"},
		{FormatLossless, "VP8L"},
		{FormatExtended, "VP8X"},
		{FormatUndefined, "undefined"},
	}
	for _, tt := range tests {
		if got := tt.f.String(); got != tt.want {
			t.Errorf("Format(%d).String() = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestMuxOddPayloadPadding(t *testing.T) {
	// VP8L with odd-length payload to verify RIFF padding.
	bs := makeVP8LData(3, 3, false)
	extra := append(bs, 0xFF) // Make it 6 bytes (even).

	m := NewMuxer()
	if err := m.AddFrame(extra, nil); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}

	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// Should be parseable.
	_, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("Demux roundtrip: %v", err)
	}
}

// --- frameDataHasAlpha tests (M-23) ---

func TestFrameDataHasAlpha_VP8L_WithAlpha(t *testing.T) {
	data := makeVP8LData(100, 100, true)
	if !frameDataHasAlpha(data) {
		t.Error("VP8L with alpha bit set should report HasAlpha=true")
	}
}

func TestFrameDataHasAlpha_VP8L_NoAlpha(t *testing.T) {
	data := makeVP8LData(100, 100, false)
	if frameDataHasAlpha(data) {
		t.Error("VP8L without alpha bit should report HasAlpha=false")
	}
}

func TestFrameDataHasAlpha_VP8_Lossy(t *testing.T) {
	// VP8 lossy bitstream never signals alpha (it comes from ALPH chunk).
	data := makeVP8Keyframe(100, 100)
	if frameDataHasAlpha(data) {
		t.Error("VP8 lossy bitstream should report HasAlpha=false")
	}
}

func TestFrameDataHasAlpha_TooShort(t *testing.T) {
	if frameDataHasAlpha([]byte{0x2f, 0x00}) {
		t.Error("too-short data should report HasAlpha=false")
	}
}

func TestFrameDataHasAlpha_Empty(t *testing.T) {
	if frameDataHasAlpha(nil) {
		t.Error("nil data should report HasAlpha=false")
	}
}

// --- Demuxer HasAlpha propagation (M-23) ---

func TestDemuxHasAlpha_SimpleVP8L_Alpha(t *testing.T) {
	bs := makeVP8LData(64, 64, true)
	webp := buildSimpleWebP(FourCCVP8L, bs)
	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	fi, _ := d.Frame(0)
	if !fi.HasAlpha {
		t.Error("VP8L frame with alpha bit should have HasAlpha=true")
	}
}

func TestDemuxHasAlpha_SimpleVP8L_NoAlpha(t *testing.T) {
	bs := makeVP8LData(64, 64, false)
	webp := buildSimpleWebP(FourCCVP8L, bs)
	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	fi, _ := d.Frame(0)
	if fi.HasAlpha {
		t.Error("VP8L frame without alpha bit should have HasAlpha=false")
	}
}

func TestDemuxHasAlpha_SimpleVP8_NoAlpha(t *testing.T) {
	bs := makeVP8Keyframe(64, 64)
	webp := buildSimpleWebP(FourCCVP8, bs)
	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	fi, _ := d.Frame(0)
	if fi.HasAlpha {
		t.Error("VP8 lossy frame without ALPH chunk should have HasAlpha=false")
	}
}

func TestDemuxHasAlpha_AnimatedVP8L_Alpha(t *testing.T) {
	// Build an animated WebP with VP8L frames that have the alpha bit set.
	frame1 := makeVP8LData(50, 50, true)
	frame2 := makeVP8LData(50, 50, false)

	animData := make([]byte, container.ANIMChunkSize)
	anmf1 := buildANMFData(0, 0, 50, 50, 50, BlendAlpha, DisposeNone, FourCCVP8L, frame1)
	anmf2 := buildANMFData(0, 0, 50, 50, 50, BlendAlpha, DisposeNone, FourCCVP8L, frame2)

	chunks := []Chunk{
		{ID: FourCCANIM, Size: uint32(len(animData)), Data: animData},
		{ID: FourCCANMF, Size: uint32(len(anmf1)), Data: anmf1},
		{ID: FourCCANMF, Size: uint32(len(anmf2)), Data: anmf2},
	}
	webp := buildVP8XWebP(byte(flagAnimation), 50, 50, chunks...)

	d, err := NewDemuxer(webp)
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}

	fi1, _ := d.Frame(0)
	if !fi1.HasAlpha {
		t.Error("VP8L frame 0 with alpha bit should have HasAlpha=true")
	}
	fi2, _ := d.Frame(1)
	if fi2.HasAlpha {
		t.Error("VP8L frame 1 without alpha bit should have HasAlpha=false")
	}
}

// --- Bounds clamping tests (DIFF-AN9) ---

func TestClampDuration(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"negative", -1, 0},
		{"zero", 0, 0},
		{"normal", 100, 100},
		{"at_max", 0xFFFFFF, 0xFFFFFF},
		{"over_max", 0x1000000, 0xFFFFFF},
		{"way_over", 0x7FFFFFFF, 0xFFFFFF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampDuration(tt.in)
			if got != tt.want {
				t.Errorf("clampDuration(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSetLoopCountClamping(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"negative", -1, 0},
		{"zero_infinite", 0, 0},
		{"normal", 3, 3},
		{"at_max", 0xFFFF, 0xFFFF},
		{"over_max", 0x10000, 0xFFFF},
		{"way_over", 0x7FFFFFFF, 0xFFFF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMuxer()
			m.SetLoopCount(tt.in)
			if m.loopCount != tt.want {
				t.Errorf("SetLoopCount(%d): loopCount = %d, want %d", tt.in, m.loopCount, tt.want)
			}
		})
	}
}

func TestAddFrameDurationClamping(t *testing.T) {
	bs := makeVP8Keyframe(10, 10)
	m := NewMuxer()
	if err := m.AddFrame(bs, &FrameOptions{Duration: 0x1000000}); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	got := m.FrameDuration(0)
	if got != 0xFFFFFF {
		t.Errorf("duration after AddFrame with overflow = %d, want %d", got, 0xFFFFFF)
	}
}

func TestAddFrameDurationNegativeClamping(t *testing.T) {
	bs := makeVP8Keyframe(10, 10)
	m := NewMuxer()
	if err := m.AddFrame(bs, &FrameOptions{Duration: -50}); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	got := m.FrameDuration(0)
	if got != 0 {
		t.Errorf("duration after AddFrame with negative = %d, want 0", got)
	}
}

func TestSetFrameDurationClamping(t *testing.T) {
	bs := makeVP8Keyframe(10, 10)
	m := NewMuxer()
	if err := m.AddFrame(bs, &FrameOptions{Duration: 100}); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	m.SetFrameDuration(0, 0x2000000)
	got := m.FrameDuration(0)
	if got != 0xFFFFFF {
		t.Errorf("duration after SetFrameDuration overflow = %d, want %d", got, 0xFFFFFF)
	}
}

func TestLoopCountRoundtrip(t *testing.T) {
	// Verify that a loop count of 0xFFFF survives mux -> demux roundtrip.
	frame := makeVP8Keyframe(10, 10)
	m := NewMuxer()
	m.SetLoopCount(0xFFFF)
	if err := m.AddFrame(frame, &FrameOptions{Duration: 100}); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	if d.LoopCount() != 0xFFFF {
		t.Errorf("roundtrip loopCount = %d, want %d", d.LoopCount(), 0xFFFF)
	}
}

func TestDurationMaxRoundtrip(t *testing.T) {
	// Verify that a duration of 0xFFFFFF survives mux -> demux roundtrip.
	frame := makeVP8Keyframe(10, 10)
	m := NewMuxer()
	if err := m.AddFrame(frame, &FrameOptions{Duration: 0xFFFFFF}); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	fi, err := d.Frame(0)
	if err != nil {
		t.Fatalf("Frame(0): %v", err)
	}
	if fi.Duration != 0xFFFFFF {
		t.Errorf("roundtrip duration = %d, want %d", fi.Duration, 0xFFFFFF)
	}
}

// --- SetCanvasSize tests ---

// TestCanvasSizeExplicitTakesPriority verifies that when SetCanvasSize is called
// on the Muxer, the explicit dimensions are used in the VP8X chunk instead of
// the computed frame extent. This matches C libwebp behavior where the VP8X
// canvas size is authoritative.
func TestCanvasSizeExplicitTakesPriority(t *testing.T) {
	bs := makeVP8Keyframe(100, 80)
	m := NewMuxer()
	m.SetICCProfile([]byte{0x00}) // Force VP8X extended format.
	m.SetCanvasSize(200, 160)     // Larger than frame (100x80).
	if err := m.AddFrame(bs, nil); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// Demux and verify the canvas size is 200x160, not 100x80.
	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	feat := d.GetFeatures()
	if feat.Width != 200 || feat.Height != 160 {
		t.Errorf("canvas = %dx%d, want 200x160", feat.Width, feat.Height)
	}
}

// TestCanvasSizeFallbackFromFrameExtent verifies that when SetCanvasSize is NOT
// called, the canvas size is computed from frame extents (backward compatible).
func TestCanvasSizeFallbackFromFrameExtent(t *testing.T) {
	bs := makeVP8Keyframe(100, 80)
	m := NewMuxer()
	m.SetICCProfile([]byte{0x00}) // Force VP8X extended format.
	// No SetCanvasSize call -- should fall back to frame extent.
	if err := m.AddFrame(bs, nil); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	feat := d.GetFeatures()
	if feat.Width != 100 || feat.Height != 80 {
		t.Errorf("canvas = %dx%d, want 100x80", feat.Width, feat.Height)
	}
}

// TestCanvasSizeSmallerthanFrameExtent verifies that when SetCanvasSize specifies
// a canvas smaller than the frame extent, the explicit canvas is still used.
// The validate() check will catch frames exceeding the canvas, but
// canvasSize() itself should return the explicit value.
func TestCanvasSizeSmallerThanFrameExtent(t *testing.T) {
	m := NewMuxer()
	m.SetCanvasSize(50, 40)
	// canvasSize should return the explicit value even though no frames exist yet.
	w, h := m.canvasSize()
	if w != 50 || h != 40 {
		t.Errorf("canvasSize = %dx%d, want 50x40", w, h)
	}
}

// TestCanvasSizeAnimationEncoder verifies that the animation encoder sets the
// canvas size on the muxer, so the VP8X canvas reflects the encoder's canvas
// and not just the frame extent.
func TestCanvasSizeAnimationExplicit(t *testing.T) {
	bs1 := makeVP8Keyframe(50, 50)
	bs2 := makeVP8Keyframe(50, 50)
	m := NewMuxer()
	m.SetCanvasSize(100, 100) // Canvas larger than frames.
	if err := m.AddFrame(bs1, &FrameOptions{Duration: 100}); err != nil {
		t.Fatalf("AddFrame(0): %v", err)
	}
	if err := m.AddFrame(bs2, &FrameOptions{Duration: 100, OffsetX: 50, OffsetY: 50}); err != nil {
		t.Fatalf("AddFrame(1): %v", err)
	}
	var buf bytes.Buffer
	if err := m.Assemble(&buf); err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	d, err := NewDemuxer(buf.Bytes())
	if err != nil {
		t.Fatalf("NewDemuxer: %v", err)
	}
	feat := d.GetFeatures()
	if feat.Width != 100 || feat.Height != 100 {
		t.Errorf("canvas = %dx%d, want 100x100", feat.Width, feat.Height)
	}
}

// --- Test helpers ---

// buildANMFData constructs raw ANMF payload (16-byte header + sub-chunk).
func buildANMFData(offsetX, offsetY, width, height, duration int, blend BlendMode, dispose DisposeMode, subID ChunkID, subData []byte) []byte {
	var buf bytes.Buffer

	// 16-byte ANMF header.
	hdr := make([]byte, container.ANMFChunkSize)
	putLE24(hdr[0:3], offsetX/2)
	putLE24(hdr[3:6], offsetY/2)
	putLE24(hdr[6:9], width-1)
	putLE24(hdr[9:12], height-1)
	putLE24(hdr[12:15], duration)
	var flagByte byte
	if dispose == DisposeBackground {
		flagByte |= 0x01
	}
	if blend == BlendNone {
		flagByte |= 0x02
	}
	hdr[15] = flagByte
	buf.Write(hdr)

	// Sub-chunk: header + data.
	subHdr := make([]byte, container.ChunkHeaderSize)
	writeChunkHeader(subHdr, subID, uint32(len(subData)))
	buf.Write(subHdr)
	buf.Write(subData)
	if len(subData)%2 != 0 {
		buf.WriteByte(0)
	}

	return buf.Bytes()
}
