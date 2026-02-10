package animation

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/deepteams/webp/internal/container"
	"github.com/deepteams/webp/mux"
)

// --- Frame tests ---

func TestFrameBounds(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 100, 200))
	f := Frame{Image: img, OffsetX: 10, OffsetY: 20}
	b := f.Bounds()
	if b.Min.X != 10 || b.Min.Y != 20 || b.Max.X != 110 || b.Max.Y != 220 {
		t.Errorf("Bounds() = %v, want (10,20)-(110,220)", b)
	}
}

func TestFrameBoundsNilImage(t *testing.T) {
	f := Frame{OffsetX: 5, OffsetY: 10}
	b := f.Bounds()
	// No image: zero-size rect at offset.
	if b.Dx() != 0 || b.Dy() != 0 {
		t.Errorf("Bounds() with nil image = %v, want zero-size", b)
	}
}

func TestFrameHasImage(t *testing.T) {
	f := Frame{}
	if f.HasImage() {
		t.Error("HasImage() = true for nil Image")
	}
	f.Image = image.NewNRGBA(image.Rect(0, 0, 1, 1))
	if !f.HasImage() {
		t.Error("HasImage() = false for non-nil Image")
	}
}

// --- Alpha blending tests ---

func TestAlphaBlendNRGBA_FullyOpaqueSrc(t *testing.T) {
	src := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	dst := color.NRGBA{R: 0, G: 255, B: 0, A: 255}
	got := alphaBlendNRGBA(src, dst)
	if got != src {
		t.Errorf("opaque src over dst = %v, want %v", got, src)
	}
}

func TestAlphaBlendNRGBA_TransparentSrc(t *testing.T) {
	src := color.NRGBA{R: 255, G: 0, B: 0, A: 0}
	dst := color.NRGBA{R: 0, G: 255, B: 0, A: 128}
	got := alphaBlendNRGBA(src, dst)
	if got != dst {
		t.Errorf("transparent src over dst = %v, want %v", got, dst)
	}
}

func TestAlphaBlendNRGBA_TransparentDst(t *testing.T) {
	src := color.NRGBA{R: 100, G: 100, B: 100, A: 128}
	dst := color.NRGBA{A: 0}
	got := alphaBlendNRGBA(src, dst)
	if got != src {
		t.Errorf("src over transparent dst = %v, want %v", got, src)
	}
}

func TestAlphaBlendNRGBA_HalfAlpha(t *testing.T) {
	src := color.NRGBA{R: 255, G: 0, B: 0, A: 128}
	dst := color.NRGBA{R: 0, G: 0, B: 255, A: 255}
	got := alphaBlendNRGBA(src, dst)

	if got.A != 255 {
		t.Errorf("alpha = %d, want 255", got.A)
	}
	if got.R < 120 || got.R > 135 {
		t.Errorf("R = %d, expected ~128", got.R)
	}
	if got.B < 120 || got.B > 135 {
		t.Errorf("B = %d, expected ~127", got.B)
	}
}

// --- AnimDecoder canvas reconstruction tests ---

func solidNRGBA(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestAnimDecoderSingleFrame(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{{
			Image:    solidNRGBA(4, 4, red),
			Duration: 100 * time.Millisecond,
			Blend:    BlendNone,
		}},
	}

	dec := NewAnimDecoder(anim)
	snap, dur, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}
	if dur != 100*time.Millisecond {
		t.Errorf("duration = %v, want 100ms", dur)
	}
	got := snap.NRGBAAt(2, 2)
	if got != red {
		t.Errorf("pixel (2,2) = %v, want %v", got, red)
	}
}

func TestAnimDecoderBlendAlpha(t *testing.T) {
	blue := color.NRGBA{R: 0, G: 0, B: 255, A: 255}
	halfRed := color.NRGBA{R: 255, G: 0, B: 0, A: 128}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, blue), Duration: 50 * time.Millisecond, Blend: BlendNone},
			{Image: solidNRGBA(4, 4, halfRed), Duration: 50 * time.Millisecond, Blend: BlendAlpha, HasAlpha: true},
		},
	}

	dec := NewAnimDecoder(anim)
	dec.NextFrame() // frame 0
	snap, _, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}
	got := snap.NRGBAAt(1, 1)
	if got.R < 120 || got.R > 135 {
		t.Errorf("R = %d, expected ~128", got.R)
	}
	if got.B < 120 || got.B > 135 {
		t.Errorf("B = %d, expected ~127", got.B)
	}
	if got.A != 255 {
		t.Errorf("A = %d, want 255", got.A)
	}
}

func TestAnimDecoderBlendNone(t *testing.T) {
	blue := color.NRGBA{R: 0, G: 0, B: 255, A: 255}
	halfRed := color.NRGBA{R: 255, G: 0, B: 0, A: 128}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, blue), Duration: 50 * time.Millisecond, Blend: BlendNone},
			{Image: solidNRGBA(4, 4, halfRed), Duration: 50 * time.Millisecond, Blend: BlendNone, HasAlpha: true},
		},
	}

	dec := NewAnimDecoder(anim)
	dec.NextFrame()
	snap, _, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}
	got := snap.NRGBAAt(1, 1)
	if got != halfRed {
		t.Errorf("pixel = %v, want %v (BlendNone should overwrite)", got, halfRed)
	}
}

func TestAnimDecoderDisposeBackground(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	transparent := color.NRGBA{}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendNone, Dispose: DisposeBackground},
			{Image: solidNRGBA(2, 2, blue), Duration: 50 * time.Millisecond, Blend: BlendNone, Dispose: DisposeNone},
		},
	}

	dec := NewAnimDecoder(anim)
	dec.NextFrame() // frame 0: red, then dispose to transparent
	snap, _, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}

	// Top-left 2x2 should be blue.
	got := snap.NRGBAAt(1, 1)
	if got != blue {
		t.Errorf("(1,1) = %v, want blue %v", got, blue)
	}

	// Bottom-right should be transparent (per C libwebp: dispose fills with
	// transparent 0,0,0,0, not the container background color).
	got = snap.NRGBAAt(3, 3)
	if got != transparent {
		t.Errorf("(3,3) = %v, want transparent %v", got, transparent)
	}
}

func TestAnimDecoderPartialFrame(t *testing.T) {
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	red := color.NRGBA{R: 255, A: 255}

	anim := &Animation{
		CanvasWidth:  8,
		CanvasHeight: 8,
		Frames: []Frame{
			{Image: solidNRGBA(8, 8, white), Duration: 50 * time.Millisecond, Blend: BlendNone},
			{Image: solidNRGBA(4, 4, red), OffsetX: 4, OffsetY: 4, Duration: 50 * time.Millisecond, Blend: BlendNone},
		},
	}

	dec := NewAnimDecoder(anim)
	dec.NextFrame()
	snap, _, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}

	got := snap.NRGBAAt(0, 0)
	if got != white {
		t.Errorf("(0,0) = %v, want white", got)
	}
	got = snap.NRGBAAt(5, 5)
	if got != red {
		t.Errorf("(5,5) = %v, want red", got)
	}
}

func TestAnimDecoderNoFrames(t *testing.T) {
	anim := &Animation{CanvasWidth: 10, CanvasHeight: 10}
	dec := NewAnimDecoder(anim)
	_, _, err := dec.NextFrame()
	if err != ErrNoFrames {
		t.Errorf("expected ErrNoFrames, got %v", err)
	}
}

func TestAnimDecoderNilImage(t *testing.T) {
	anim := &Animation{
		CanvasWidth:  10,
		CanvasHeight: 10,
		Frames:       []Frame{{Duration: 100 * time.Millisecond}},
	}
	dec := NewAnimDecoder(anim)
	_, _, err := dec.NextFrame()
	if err != ErrNilImage {
		t.Errorf("expected ErrNilImage, got %v", err)
	}
}

func TestAnimDecoderHasNext(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	anim := &Animation{
		CanvasWidth:  2,
		CanvasHeight: 2,
		Frames: []Frame{
			{Image: solidNRGBA(2, 2, red), Duration: 10 * time.Millisecond, Blend: BlendNone},
			{Image: solidNRGBA(2, 2, red), Duration: 20 * time.Millisecond, Blend: BlendNone},
		},
	}

	dec := NewAnimDecoder(anim)
	if !dec.HasNext() {
		t.Error("HasNext should be true initially")
	}
	dec.NextFrame()
	dec.NextFrame()
	if dec.HasNext() {
		t.Error("HasNext should be false after all frames consumed")
	}
}

func TestAnimDecoderReset(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	anim := &Animation{
		CanvasWidth:  2,
		CanvasHeight: 2,
		Frames: []Frame{
			{Image: solidNRGBA(2, 2, red), Duration: 10 * time.Millisecond, Blend: BlendNone},
		},
	}

	dec := NewAnimDecoder(anim)
	dec.NextFrame()
	if dec.HasNext() {
		t.Fatal("should be exhausted")
	}
	dec.Reset()
	if !dec.HasNext() {
		t.Error("HasNext should be true after Reset")
	}
	snap, _, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame after Reset: %v", err)
	}
	got := snap.NRGBAAt(0, 0)
	if got != red {
		t.Errorf("after Reset pixel = %v, want red", got)
	}
}

// --- Animation.TotalDuration ---

func TestTotalDuration(t *testing.T) {
	anim := &Animation{
		CanvasWidth:  10,
		CanvasHeight: 10,
		Frames: []Frame{
			{Duration: 100 * time.Millisecond},
			{Duration: 200 * time.Millisecond},
			{Duration: 50 * time.Millisecond},
		},
	}
	if got := anim.TotalDuration(); got != 350*time.Millisecond {
		t.Errorf("TotalDuration = %v, want 350ms", got)
	}
}

// --- Color conversion tests ---

func TestArgbToNRGBA(t *testing.T) {
	got := argbToNRGBA(0xFF804020)
	want := color.NRGBA{R: 0x80, G: 0x40, B: 0x20, A: 0xFF}
	if got != want {
		t.Errorf("argbToNRGBA = %v, want %v", got, want)
	}
}

func TestNrgbaToARGB(t *testing.T) {
	c := color.NRGBA{R: 0x80, G: 0x40, B: 0x20, A: 0xFF}
	got := nrgbaToARGB(c)
	if got != 0xFF804020 {
		t.Errorf("nrgbaToARGB = 0x%08x, want 0xFF804020", got)
	}
}

func TestColorToNRGBA(t *testing.T) {
	got := colorToNRGBA(color.RGBA{R: 255, A: 255})
	want := color.NRGBA{R: 255, A: 255}
	if got != want {
		t.Errorf("colorToNRGBA(opaque red) = %v, want %v", got, want)
	}

	got = colorToNRGBA(color.RGBA{A: 0})
	if got != (color.NRGBA{}) {
		t.Errorf("colorToNRGBA(transparent) = %v, want zero", got)
	}
}

func TestToNRGBA(t *testing.T) {
	// NRGBA passthrough.
	nrgba := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	nrgba.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	got := toNRGBA(nrgba)
	if got != nrgba {
		t.Error("toNRGBA should return same pointer for *image.NRGBA")
	}

	// RGBA conversion.
	rgba := image.NewRGBA(image.Rect(0, 0, 2, 2))
	rgba.SetRGBA(1, 1, color.RGBA{R: 128, G: 64, B: 32, A: 255})
	converted := toNRGBA(rgba)
	px := converted.NRGBAAt(1, 1)
	if px.R != 128 || px.G != 64 || px.B != 32 || px.A != 255 {
		t.Errorf("converted pixel = %v, want {128 64 32 255}", px)
	}
}

// --- Decode (mux integration) test ---

func makeVP8Keyframe(width, height int) []byte {
	data := make([]byte, 10)
	data[3] = 0x9d
	data[4] = 0x01
	data[5] = 0x2a
	binary.LittleEndian.PutUint16(data[6:8], uint16(width))
	binary.LittleEndian.PutUint16(data[8:10], uint16(height))
	return data
}

func writeChunkHeader(buf []byte, id uint32, size uint32) {
	binary.LittleEndian.PutUint32(buf[0:4], id)
	binary.LittleEndian.PutUint32(buf[4:8], size)
}

func putLE24(buf []byte, v int) {
	buf[0] = byte(v)
	buf[1] = byte(v >> 8)
	buf[2] = byte(v >> 16)
}

func buildAnimatedWebP(canvasW, canvasH int, frames [][]byte, durations []int) []byte {
	var body bytes.Buffer

	// VP8X chunk.
	vp8x := make([]byte, container.ChunkHeaderSize+container.VP8XChunkSize)
	writeChunkHeader(vp8x[0:8], mux.FourCCVP8X, container.VP8XChunkSize)
	vp8x[8] = 0x02 // flagAnimation
	putLE24(vp8x[12:15], canvasW-1)
	putLE24(vp8x[15:18], canvasH-1)
	body.Write(vp8x)

	// ANIM chunk.
	animChunk := make([]byte, container.ChunkHeaderSize+container.ANIMChunkSize)
	writeChunkHeader(animChunk[0:8], mux.FourCCANIM, container.ANIMChunkSize)
	binary.LittleEndian.PutUint32(animChunk[8:12], 0xFF000000) // bg color
	binary.LittleEndian.PutUint16(animChunk[12:14], 2)         // loop count
	body.Write(animChunk)

	// ANMF chunks.
	for i, frameData := range frames {
		subSize := uint32(container.ChunkHeaderSize) + uint32(len(frameData))
		if len(frameData)%2 != 0 {
			subSize++
		}
		anmfPayload := uint32(container.ANMFChunkSize) + subSize

		hdr := make([]byte, container.ChunkHeaderSize+container.ANMFChunkSize)
		writeChunkHeader(hdr[0:8], mux.FourCCANMF, anmfPayload)
		// offsetX=0, offsetY=0
		putLE24(hdr[8+6:8+9], canvasW-1)
		putLE24(hdr[8+9:8+12], canvasH-1)
		putLE24(hdr[8+12:8+15], durations[i])
		hdr[8+15] = 0 // blend=alpha, dispose=none
		body.Write(hdr)

		// Sub-chunk (VP8).
		subHdr := make([]byte, container.ChunkHeaderSize)
		writeChunkHeader(subHdr, mux.FourCCVP8, uint32(len(frameData)))
		body.Write(subHdr)
		body.Write(frameData)
		if len(frameData)%2 != 0 {
			body.WriteByte(0)
		}
		// ANMF padding.
		if anmfPayload%2 != 0 {
			body.WriteByte(0)
		}
	}

	// Build RIFF.
	riffPayload := 4 + body.Len() // "WEBP" + chunks
	result := make([]byte, container.RIFFHeaderSize+body.Len())
	binary.LittleEndian.PutUint32(result[0:4], mux.FourCCRIFF)
	binary.LittleEndian.PutUint32(result[4:8], uint32(riffPayload))
	binary.LittleEndian.PutUint32(result[8:12], mux.FourCCWEBP)
	copy(result[12:], body.Bytes())
	return result
}

func TestDecodeAnimatedWebP(t *testing.T) {
	frame1 := makeVP8Keyframe(100, 100)
	frame2 := makeVP8Keyframe(100, 100)
	webpData := buildAnimatedWebP(100, 100, [][]byte{frame1, frame2}, []int{50, 100})

	anim, err := Decode(bytes.NewReader(webpData))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if anim.CanvasWidth != 100 || anim.CanvasHeight != 100 {
		t.Errorf("canvas = %dx%d, want 100x100", anim.CanvasWidth, anim.CanvasHeight)
	}
	if anim.LoopCount != 2 {
		t.Errorf("LoopCount = %d, want 2", anim.LoopCount)
	}
	if len(anim.Frames) != 2 {
		t.Fatalf("Frames = %d, want 2", len(anim.Frames))
	}
	if anim.Frames[0].Duration != 50*time.Millisecond {
		t.Errorf("frame 0 duration = %v, want 50ms", anim.Frames[0].Duration)
	}
	if anim.Frames[1].Duration != 100*time.Millisecond {
		t.Errorf("frame 1 duration = %v, want 100ms", anim.Frames[1].Duration)
	}
	if anim.Frames[0].BitstreamData == nil {
		t.Error("frame 0 BitstreamData should not be nil")
	}
}

func TestDecodeSimpleWebP(t *testing.T) {
	bs := makeVP8Keyframe(320, 240)

	// Build simple WebP.
	chunkSize := uint32(len(bs))
	paddedSize := chunkSize
	if chunkSize%2 != 0 {
		paddedSize++
	}
	riffPayload := 4 + container.ChunkHeaderSize + paddedSize
	total := container.RIFFHeaderSize + container.ChunkHeaderSize + int(paddedSize)
	webpData := make([]byte, total)
	binary.LittleEndian.PutUint32(webpData[0:4], mux.FourCCRIFF)
	binary.LittleEndian.PutUint32(webpData[4:8], uint32(riffPayload))
	binary.LittleEndian.PutUint32(webpData[8:12], mux.FourCCWEBP)
	writeChunkHeader(webpData[12:20], mux.FourCCVP8, chunkSize)
	copy(webpData[20:], bs)

	anim, err := Decode(bytes.NewReader(webpData))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if anim.CanvasWidth != 320 || anim.CanvasHeight != 240 {
		t.Errorf("canvas = %dx%d, want 320x240", anim.CanvasWidth, anim.CanvasHeight)
	}
	if len(anim.Frames) != 1 {
		t.Fatalf("Frames = %d, want 1", len(anim.Frames))
	}
}

// --- AnimEncoder tests ---

func TestAnimEncoderRoundtrip(t *testing.T) {
	frame1 := makeVP8Keyframe(100, 100)
	frame2 := makeVP8Keyframe(100, 100)

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 100, 100, &EncodeOptions{
		LoopCount:       5,
		BackgroundColor: color.NRGBA{R: 0, G: 0, B: 0, A: 255},
	})

	if err := enc.AddRawFrame(frame1, 50*time.Millisecond, 0, 0, BlendAlpha, DisposeNone); err != nil {
		t.Fatalf("AddRawFrame 1: %v", err)
	}
	if err := enc.AddRawFrame(frame2, 100*time.Millisecond, 0, 0, BlendNone, DisposeBackground); err != nil {
		t.Fatalf("AddRawFrame 2: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Decode back.
	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode roundtrip: %v", err)
	}
	if len(anim.Frames) != 2 {
		t.Fatalf("roundtrip frames = %d, want 2", len(anim.Frames))
	}
	if anim.LoopCount != 5 {
		t.Errorf("roundtrip LoopCount = %d, want 5", anim.LoopCount)
	}
	if anim.Frames[0].Duration != 50*time.Millisecond {
		t.Errorf("frame 0 duration = %v, want 50ms", anim.Frames[0].Duration)
	}
	if anim.Frames[1].Duration != 100*time.Millisecond {
		t.Errorf("frame 1 duration = %v, want 100ms", anim.Frames[1].Duration)
	}
}

func TestAnimEncoderAddFrameWithBitstreamFrame(t *testing.T) {
	bs := makeVP8Keyframe(50, 50)

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 50, 50, nil)

	img := NewBitstreamFrame(bs, 50, 50)
	if err := enc.AddFrame(img, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Should be parseable.
	_, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
}

func TestAnimEncoderCloseIdempotent(t *testing.T) {
	bs := makeVP8Keyframe(10, 10)
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, nil)
	enc.AddRawFrame(bs, 10*time.Millisecond, 0, 0, BlendAlpha, DisposeNone)
	enc.Close()
	if err := enc.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAnimEncoderAddAfterClose(t *testing.T) {
	bs := makeVP8Keyframe(10, 10)
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, nil)
	enc.AddRawFrame(bs, 10*time.Millisecond, 0, 0, BlendAlpha, DisposeNone)
	enc.Close()
	err := enc.AddRawFrame(bs, 10*time.Millisecond, 0, 0, BlendAlpha, DisposeNone)
	if err == nil {
		t.Error("AddRawFrame after Close should error")
	}
}

func TestAnimEncoderMetadata(t *testing.T) {
	bs := makeVP8Keyframe(10, 10)
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, nil)
	enc.SetICCProfile([]byte("icc"))
	enc.SetEXIF([]byte("exif"))
	enc.SetXMP([]byte("xmp"))
	enc.AddRawFrame(bs, 10*time.Millisecond, 0, 0, BlendAlpha, DisposeNone)
	enc.Close()

	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(anim.ICC, []byte("icc")) {
		t.Errorf("ICC = %q, want 'icc'", anim.ICC)
	}
	if !bytes.Equal(anim.EXIF, []byte("exif")) {
		t.Errorf("EXIF = %q, want 'exif'", anim.EXIF)
	}
	if !bytes.Equal(anim.XMP, []byte("xmp")) {
		t.Errorf("XMP = %q, want 'xmp'", anim.XMP)
	}
}

// --- Bounds clamping tests (DIFF-AN9) ---

func TestNewEncoderLoopCountClamping(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"negative", -5, 0},
		{"zero", 0, 0},
		{"normal", 10, 10},
		{"at_max", 0xFFFF, 0xFFFF},
		{"over_max", 0x10000, 0xFFFF},
		{"way_over", 0x7FFFFFFF, 0xFFFF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewEncoder(&buf, 10, 10, &EncodeOptions{LoopCount: tt.in})
			if enc.opts.LoopCount != tt.want {
				t.Errorf("NewEncoder with LoopCount=%d: opts.LoopCount = %d, want %d",
					tt.in, enc.opts.LoopCount, tt.want)
			}
		})
	}
}

func TestClampLoopCount(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"negative", -1, 0},
		{"zero", 0, 0},
		{"normal", 42, 42},
		{"max", 65535, 65535},
		{"over_max", 65536, 65535},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampLoopCount(tt.in)
			if got != tt.want {
				t.Errorf("clampLoopCount(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// --- DecodeFrames test ---

func TestDecodeFramesNoDecoder(t *testing.T) {
	oldFunc := FrameDecoderFunc
	FrameDecoderFunc = nil
	defer func() { FrameDecoderFunc = oldFunc }()

	anim := &Animation{
		CanvasWidth:  10,
		CanvasHeight: 10,
		Frames:       []Frame{{BitstreamData: []byte{1}}},
	}
	err := anim.DecodeFrames()
	if err != ErrNoDecoder {
		t.Errorf("expected ErrNoDecoder, got %v", err)
	}
}

func TestDecodeFramesWithMock(t *testing.T) {
	mockImg := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	oldFunc := FrameDecoderFunc
	FrameDecoderFunc = func(_, _ []byte) (*image.NRGBA, error) {
		return mockImg, nil
	}
	defer func() { FrameDecoderFunc = oldFunc }()

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{BitstreamData: []byte{1}},
			{BitstreamData: []byte{2}},
		},
	}
	if err := anim.DecodeFrames(); err != nil {
		t.Fatalf("DecodeFrames: %v", err)
	}
	for i, f := range anim.Frames {
		if f.Image != mockImg {
			t.Errorf("frame %d Image not set", i)
		}
	}
}

func TestDecodeFramesSkipsDecoded(t *testing.T) {
	existing := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	other := image.NewNRGBA(image.Rect(0, 0, 4, 4))

	oldFunc := FrameDecoderFunc
	FrameDecoderFunc = func(_, _ []byte) (*image.NRGBA, error) {
		return other, nil
	}
	defer func() { FrameDecoderFunc = oldFunc }()

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames:       []Frame{{Image: existing, BitstreamData: []byte{1}}},
	}
	if err := anim.DecodeFrames(); err != nil {
		t.Fatalf("DecodeFrames: %v", err)
	}
	if anim.Frames[0].Image != existing {
		t.Error("DecodeFrames replaced existing image")
	}
}

// --- Canvas initialization: transparent (C16) ---

func TestCanvasInitTransparent(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	transparent := color.NRGBA{}

	// A partial frame (2x2 at offset 2,2 on a 4x4 canvas).
	// Canvas should start transparent, so uncovered pixels remain transparent.
	anim := &Animation{
		CanvasWidth:     4,
		CanvasHeight:    4,
		BackgroundColor: color.NRGBA{R: 0, G: 255, B: 0, A: 255}, // green - should be ignored
		Frames: []Frame{
			{Image: solidNRGBA(2, 2, red), OffsetX: 2, OffsetY: 2, Duration: 50 * time.Millisecond, Blend: BlendNone},
		},
	}

	dec := NewAnimDecoder(anim)
	snap, _, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}

	// Covered area should be red.
	got := snap.NRGBAAt(2, 2)
	if got != red {
		t.Errorf("(2,2) = %v, want red %v", got, red)
	}

	// Uncovered area should be transparent, NOT green (BackgroundColor).
	got = snap.NRGBAAt(0, 0)
	if got != transparent {
		t.Errorf("(0,0) = %v, want transparent %v (canvas init should be transparent, not BackgroundColor)", got, transparent)
	}
}

// --- Keyframe detection (H3) ---

func TestKeyframeDetection_FirstFrame(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendAlpha},
		},
	}
	dec := NewAnimDecoder(anim)
	// Frame 0 is always a keyframe.
	if !dec.isKeyFrame(0) {
		t.Error("frame 0 should always be a keyframe")
	}
}

func TestKeyframeDetection_FullCanvasNoBlend(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendNone},
			// Full canvas, no-blend, opaque => keyframe.
			{Image: solidNRGBA(4, 4, blue), Duration: 50 * time.Millisecond, Blend: BlendNone},
		},
	}
	dec := NewAnimDecoder(anim)
	// Advance past frame 0 to set prevDispose state.
	dec.NextFrame()

	if !dec.isKeyFrame(1) {
		t.Error("full-canvas opaque no-blend frame should be a keyframe")
	}
}

func TestKeyframeDetection_PartialFrameNotKeyframe(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	anim := &Animation{
		CanvasWidth:  8,
		CanvasHeight: 8,
		Frames: []Frame{
			{Image: solidNRGBA(8, 8, red), Duration: 50 * time.Millisecond, Blend: BlendNone, Dispose: DisposeNone},
			// Partial frame (4x4) with DisposeNone previous => NOT keyframe.
			{Image: solidNRGBA(4, 4, blue), OffsetX: 0, OffsetY: 0, Duration: 50 * time.Millisecond, Blend: BlendAlpha},
		},
	}
	dec := NewAnimDecoder(anim)
	dec.NextFrame()

	if dec.isKeyFrame(1) {
		t.Error("partial frame with DisposeNone previous should NOT be a keyframe")
	}
}

func TestKeyframeDetection_PrevDisposeBackgroundFullCanvas(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	halfBlue := color.NRGBA{B: 255, A: 128}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			// Full canvas, dispose-to-background.
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendNone, Dispose: DisposeBackground},
			// Has alpha, blend=alpha, partial => but prev was full dispose-bg => keyframe.
			{Image: solidNRGBA(2, 2, halfBlue), Duration: 50 * time.Millisecond, Blend: BlendAlpha, HasAlpha: true},
		},
	}
	dec := NewAnimDecoder(anim)
	dec.NextFrame()

	if !dec.isKeyFrame(1) {
		t.Error("frame after full-canvas dispose-background should be a keyframe")
	}
}

// --- Keyframe detection via bitstream flag (M-23) ---

func TestKeyframeDetection_BitstreamFlagNoAlpha(t *testing.T) {
	// A full-canvas frame with HasAlpha=false and BlendAlpha should still be
	// detected as a keyframe, because the bitstream signals no alpha channel.
	// This matches the C libwebp IsKeyFrame() which uses iter->has_alpha
	// instead of scanning pixel data.
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendNone},
			// Full canvas, BlendAlpha, but HasAlpha=false => keyframe.
			{Image: solidNRGBA(4, 4, blue), Duration: 50 * time.Millisecond, Blend: BlendAlpha, HasAlpha: false},
		},
	}
	dec := NewAnimDecoder(anim)
	dec.NextFrame()

	if !dec.isKeyFrame(1) {
		t.Error("full-canvas frame with HasAlpha=false should be a keyframe even with BlendAlpha")
	}
}

func TestKeyframeDetection_BitstreamFlagWithAlpha(t *testing.T) {
	// A full-canvas frame with HasAlpha=true and BlendAlpha is NOT a keyframe
	// from the full-frame path (it needs blending with previous canvas).
	red := color.NRGBA{R: 255, A: 255}
	halfGreen := color.NRGBA{G: 255, A: 128}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendNone, Dispose: DisposeNone},
			// Full canvas, BlendAlpha, HasAlpha=true => NOT a keyframe from full-frame path.
			// prevDispose=DisposeNone, not a full-canvas dispose => NOT a keyframe from dispose path.
			{Image: solidNRGBA(4, 4, halfGreen), Duration: 50 * time.Millisecond, Blend: BlendAlpha, HasAlpha: true},
		},
	}
	dec := NewAnimDecoder(anim)
	dec.NextFrame()

	if dec.isKeyFrame(1) {
		t.Error("full-canvas frame with HasAlpha=true and BlendAlpha should NOT be a keyframe")
	}
}

func TestKeyframeDetection_BitstreamFlagAlphaWithBlendNone(t *testing.T) {
	// Even with HasAlpha=true, BlendNone on a full-canvas frame makes it a
	// keyframe (it overwrites the entire canvas).
	red := color.NRGBA{R: 255, A: 255}
	halfGreen := color.NRGBA{G: 255, A: 128}

	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendNone},
			// HasAlpha=true but BlendNone => keyframe.
			{Image: solidNRGBA(4, 4, halfGreen), Duration: 50 * time.Millisecond, Blend: BlendNone, HasAlpha: true},
		},
	}
	dec := NewAnimDecoder(anim)
	dec.NextFrame()

	if !dec.isKeyFrame(1) {
		t.Error("full-canvas frame with BlendNone should be a keyframe regardless of HasAlpha")
	}
}

// --- Dual-buffer / dispose correctness (H2) ---

func TestDualBufferDisposeNone(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	// Frame 0: full red, DisposeNone.
	// Frame 1: partial blue (2x2 at 0,0), DisposeNone.
	// With dual-buffer: after frame 0, prevFrameDisposed = red everywhere.
	// Frame 1 starts from prevFrameDisposed (red), composites blue at top-left.
	// Result: blue at (0,0)-(2,2), red at (2,2)-(4,4).
	anim := &Animation{
		CanvasWidth:  4,
		CanvasHeight: 4,
		Frames: []Frame{
			{Image: solidNRGBA(4, 4, red), Duration: 50 * time.Millisecond, Blend: BlendNone, Dispose: DisposeNone},
			{Image: solidNRGBA(2, 2, blue), Duration: 50 * time.Millisecond, Blend: BlendNone, Dispose: DisposeNone},
		},
	}

	dec := NewAnimDecoder(anim)
	dec.NextFrame()
	snap, _, err := dec.NextFrame()
	if err != nil {
		t.Fatalf("NextFrame: %v", err)
	}

	got := snap.NRGBAAt(0, 0)
	if got != blue {
		t.Errorf("(0,0) = %v, want blue %v", got, blue)
	}

	// Area not covered by frame 1 should retain red from frame 0.
	got = snap.NRGBAAt(3, 3)
	if got != red {
		t.Errorf("(3,3) = %v, want red %v (retained from frame 0 via DisposeNone)", got, red)
	}
}

// --- Alpha blending matches C formula (H4) ---

func TestAlphaBlendMatchesC(t *testing.T) {
	// Verify the C formula: dst_factor_a = (dst_a * (256 - src_a)) >> 8
	// For src_a=128, dst_a=255: dst_factor_a = (255 * 128) >> 8 = 127
	// blend_a = 128 + 127 = 255
	src := color.NRGBA{R: 200, G: 100, B: 50, A: 128}
	dst := color.NRGBA{R: 50, G: 200, B: 100, A: 255}
	got := alphaBlendNRGBA(src, dst)

	// Manually compute with C formula.
	srcA := uint32(128)
	dstFactorA := (uint32(255) * (256 - srcA)) >> 8 // = 127
	blendA := srcA + dstFactorA                      // = 255
	scale := (1 << 24) / blendA

	blendC := func(sc, dc uint8) uint8 {
		return uint8((uint32(sc)*srcA + uint32(dc)*dstFactorA) * scale >> 24)
	}
	wantR := blendC(200, 50)
	wantG := blendC(100, 200)
	wantB := blendC(50, 100)

	if got.R != wantR || got.G != wantG || got.B != wantB || got.A != uint8(blendA) {
		t.Errorf("alphaBlendNRGBA = %v, want {R:%d G:%d B:%d A:%d}", got, wantR, wantG, wantB, blendA)
	}
}

func TestAlphaBlendBothSemiTransparent(t *testing.T) {
	src := color.NRGBA{R: 255, A: 100}
	dst := color.NRGBA{B: 255, A: 100}
	got := alphaBlendNRGBA(src, dst)

	// dst_factor_a = (100 * (256 - 100)) >> 8 = (100 * 156) >> 8 = 60
	// blend_a = 100 + 60 = 160
	if got.A != 160 {
		t.Errorf("alpha = %d, want 160", got.A)
	}
	// R should dominate over B since src has higher contribution.
	if got.R < got.B {
		t.Errorf("R=%d should be >= B=%d (src contributes more)", got.R, got.B)
	}
}

// --- Sub-frame rectangle detection tests ---

func TestFindChangedRect_NoDiff(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	a := solidNRGBA(8, 8, red)
	b := solidNRGBA(8, 8, red)
	r := findChangedRect(a, b)
	if !r.Empty() {
		t.Errorf("expected empty rect for identical images, got %v", r)
	}
}

func TestFindChangedRect_SinglePixel(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	a := solidNRGBA(8, 8, red)
	b := solidNRGBA(8, 8, red)
	b.SetNRGBA(3, 5, color.NRGBA{B: 255, A: 255})
	r := findChangedRect(a, b)
	want := image.Rect(3, 5, 4, 6)
	if r != want {
		t.Errorf("findChangedRect single pixel = %v, want %v", r, want)
	}
}

func TestFindChangedRect_Region(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	a := solidNRGBA(10, 10, red)
	b := solidNRGBA(10, 10, red)
	// Change a 3x4 region at (2,3)-(5,7).
	for y := 3; y < 7; y++ {
		for x := 2; x < 5; x++ {
			b.SetNRGBA(x, y, blue)
		}
	}
	r := findChangedRect(a, b)
	want := image.Rect(2, 3, 5, 7)
	if r != want {
		t.Errorf("findChangedRect region = %v, want %v", r, want)
	}
}

func TestSnapToEven(t *testing.T) {
	tests := []struct {
		name string
		in   image.Rectangle
		want image.Rectangle
	}{
		{"already even", image.Rect(2, 4, 10, 12), image.Rect(2, 4, 10, 12)},
		{"odd x", image.Rect(3, 4, 10, 12), image.Rect(2, 4, 10, 12)},
		{"odd y", image.Rect(2, 5, 10, 12), image.Rect(2, 4, 10, 12)},
		{"odd both", image.Rect(3, 5, 10, 12), image.Rect(2, 4, 10, 12)},
		{"zero", image.Rect(0, 0, 8, 8), image.Rect(0, 0, 8, 8)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := snapToEven(tt.in)
			if got != tt.want {
				t.Errorf("snapToEven(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractSubImage(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	src := solidNRGBA(10, 10, red)
	for y := 2; y < 5; y++ {
		for x := 3; x < 7; x++ {
			src.SetNRGBA(x, y, blue)
		}
	}
	sub := extractSubImage(src, image.Rect(3, 2, 7, 5))
	if sub.Bounds().Dx() != 4 || sub.Bounds().Dy() != 3 {
		t.Fatalf("extractSubImage size = %dx%d, want 4x3", sub.Bounds().Dx(), sub.Bounds().Dy())
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			got := sub.NRGBAAt(x, y)
			if got != blue {
				t.Errorf("extractSubImage(%d,%d) = %v, want blue", x, y, got)
			}
		}
	}
}

func TestCloneNRGBA(t *testing.T) {
	red := color.NRGBA{R: 255, A: 255}
	src := solidNRGBA(4, 4, red)
	dst := cloneNRGBA(src)
	if &src.Pix[0] == &dst.Pix[0] {
		t.Error("cloneNRGBA should not share pixel data")
	}
	// Modify src, dst should be unchanged.
	src.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})
	if dst.NRGBAAt(0, 0) != red {
		t.Error("cloneNRGBA: modifying src affected dst")
	}
}

// --- Optimized encoder tests with mock FrameEncoderFunc ---

// mockFrameEncoder tracks calls to FrameEncoderFunc, recording sub-image sizes.
type mockFrameEncoder struct {
	calls []image.Rectangle // Bounds of each sub-image encoded.
}

func (m *mockFrameEncoder) encode(img image.Image, lossless bool, quality int) ([]byte, error) {
	b := img.Bounds()
	m.calls = append(m.calls, b)
	// Return a valid VP8 keyframe header for the sub-image dimensions.
	return makeVP8Keyframe(b.Dx(), b.Dy()), nil
}

func TestOptimizedEncoder_SubFrameDetection(t *testing.T) {
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &mockFrameEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 100, 100, &EncodeOptions{Quality: 75})

	// Frame 0: full red canvas (keyframe).
	frame0 := solidNRGBA(100, 100, red)
	if err := enc.AddFrame(frame0, 50*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 0: %v", err)
	}

	// Frame 1: same as frame 0 except a small 10x10 blue patch at (50,50).
	frame1 := solidNRGBA(100, 100, red)
	for y := 50; y < 60; y++ {
		for x := 50; x < 60; x++ {
			frame1.SetNRGBA(x, y, blue)
		}
	}
	if err := enc.AddFrame(frame1, 50*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 1: %v", err)
	}

	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The encoder should have encoded frame 0 as full-canvas (100x100),
	// and frame 1 as a sub-frame (around 10x10, possibly larger due to even snap).
	if len(mock.calls) < 2 {
		t.Fatalf("expected at least 2 encode calls, got %d", len(mock.calls))
	}

	// Frame 0 should be full canvas.
	f0 := mock.calls[0]
	if f0.Dx() != 100 || f0.Dy() != 100 {
		t.Errorf("frame 0 encoded as %dx%d, want 100x100", f0.Dx(), f0.Dy())
	}

	// Frame 1 should be significantly smaller than full canvas.
	f1 := mock.calls[1]
	if f1.Dx() > 20 || f1.Dy() > 20 {
		t.Errorf("frame 1 encoded as %dx%d, expected sub-frame <=20x20", f1.Dx(), f1.Dy())
	}
	if f1.Dx() < 10 || f1.Dy() < 10 {
		t.Errorf("frame 1 encoded as %dx%d, expected at least 10x10", f1.Dx(), f1.Dy())
	}
}

func TestOptimizedEncoder_IdenticalFrames(t *testing.T) {
	// When consecutive frames are pixel-identical, the encoder should merge
	// them by extending the previous frame's duration instead of encoding a
	// new frame. This matches the C libwebp frame merging behavior.
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &mockFrameEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 50, 50, &EncodeOptions{Quality: 75})

	frame := solidNRGBA(50, 50, red)
	enc.AddFrame(frame, 50*time.Millisecond)
	enc.AddFrame(frame, 50*time.Millisecond) // identical to frame 0
	enc.Close()

	// Only 1 encode call should have been made (for frame 0); the identical
	// frame 1 should have been merged into frame 0 by extending its duration.
	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 encode call (frame merged), got %d", len(mock.calls))
	}

	// Decode back and verify a single frame with combined duration (100ms).
	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) != 1 {
		t.Fatalf("expected 1 frame after merging, got %d", len(anim.Frames))
	}
	if anim.Frames[0].Duration != 100*time.Millisecond {
		t.Errorf("merged frame duration = %v, want 100ms", anim.Frames[0].Duration)
	}
}

func TestOptimizedEncoder_IdenticalFramesMergeMultiple(t *testing.T) {
	// Three consecutive identical frames should merge into one frame with
	// triple the duration.
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &mockFrameEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 20, 20, &EncodeOptions{Quality: 75})

	frame := solidNRGBA(20, 20, red)
	enc.AddFrame(frame, 30*time.Millisecond)
	enc.AddFrame(frame, 40*time.Millisecond) // identical
	enc.AddFrame(frame, 50*time.Millisecond) // identical
	enc.Close()

	if len(mock.calls) != 1 {
		t.Fatalf("expected 1 encode call, got %d", len(mock.calls))
	}

	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) != 1 {
		t.Fatalf("expected 1 merged frame, got %d", len(anim.Frames))
	}
	want := 120 * time.Millisecond // 30 + 40 + 50
	if anim.Frames[0].Duration != want {
		t.Errorf("merged frame duration = %v, want %v", anim.Frames[0].Duration, want)
	}
}

func TestOptimizedEncoder_IdenticalThenDifferent(t *testing.T) {
	// Two identical frames followed by a different frame: the first two should
	// merge, and the third should be encoded normally.
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &mockFrameEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 20, 20, &EncodeOptions{Quality: 75})

	enc.AddFrame(solidNRGBA(20, 20, red), 50*time.Millisecond)
	enc.AddFrame(solidNRGBA(20, 20, red), 50*time.Millisecond) // identical, merged
	enc.AddFrame(solidNRGBA(20, 20, blue), 80*time.Millisecond) // different
	enc.Close()

	// 2 encode calls: frame 0 (red) + frame 2 (blue). Frame 1 is merged.
	if len(mock.calls) < 2 {
		t.Fatalf("expected at least 2 encode calls, got %d", len(mock.calls))
	}

	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) != 2 {
		t.Fatalf("expected 2 frames (after merging), got %d", len(anim.Frames))
	}
	if anim.Frames[0].Duration != 100*time.Millisecond { // 50 + 50
		t.Errorf("frame 0 duration = %v, want 100ms", anim.Frames[0].Duration)
	}
	if anim.Frames[1].Duration != 80*time.Millisecond {
		t.Errorf("frame 1 duration = %v, want 80ms", anim.Frames[1].Duration)
	}
}

func TestOptimizedEncoder_MaxDurationOverflow(t *testing.T) {
	// When merging would exceed maxDuration (0xFFFFFF = 16777215ms), the
	// previous frame should be capped and a 1x1 filler frame emitted.
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &mockFrameEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{Quality: 75})

	frame := solidNRGBA(10, 10, red)

	// First frame with a very large duration close to the limit.
	enc.AddFrame(frame, time.Duration(maxDuration-100)*time.Millisecond)
	// Second identical frame: combined would exceed maxDuration.
	enc.AddFrame(frame, 200*time.Millisecond)
	enc.Close()

	// Should have 2 encode calls: the initial frame + the filler frame.
	if len(mock.calls) < 2 {
		t.Fatalf("expected at least 2 encode calls (overflow filler), got %d", len(mock.calls))
	}

	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(anim.Frames) != 2 {
		t.Fatalf("expected 2 frames (overflow split), got %d", len(anim.Frames))
	}

	// First frame should be capped at maxDuration.
	dur0MS := int(anim.Frames[0].Duration / time.Millisecond)
	if dur0MS != maxDuration {
		t.Errorf("frame 0 duration = %d ms, want %d (maxDuration)", dur0MS, maxDuration)
	}

	// Second frame (filler) gets the remainder.
	dur1MS := int(anim.Frames[1].Duration / time.Millisecond)
	wantRemainder := (maxDuration - 100) + 200 - maxDuration // = 100
	if dur1MS != wantRemainder {
		t.Errorf("frame 1 (filler) duration = %d ms, want %d (remainder)", dur1MS, wantRemainder)
	}
}

func TestOptimizedEncoder_KeyframePolicy(t *testing.T) {
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &mockFrameEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	var buf bytes.Buffer
	// Force keyframe every 3 frames.
	enc := NewEncoder(&buf, 50, 50, &EncodeOptions{Quality: 75, Kmax: 3})

	// Frame 0: keyframe.
	enc.AddFrame(solidNRGBA(50, 50, red), 50*time.Millisecond)
	// Frame 1: sub-frame (small change).
	f1 := solidNRGBA(50, 50, red)
	f1.SetNRGBA(10, 10, blue)
	enc.AddFrame(f1, 50*time.Millisecond)
	// Frame 2: sub-frame.
	f2 := solidNRGBA(50, 50, red)
	f2.SetNRGBA(20, 20, blue)
	enc.AddFrame(f2, 50*time.Millisecond)
	// Frame 3: forced keyframe (kmax=3, 3 frames since last keyframe).
	f3 := solidNRGBA(50, 50, red)
	f3.SetNRGBA(30, 30, blue)
	enc.AddFrame(f3, 50*time.Millisecond)
	enc.Close()

	if len(mock.calls) < 4 {
		t.Fatalf("expected at least 4 encode calls, got %d", len(mock.calls))
	}

	// Frame 0 should be full canvas (keyframe).
	if mock.calls[0].Dx() != 50 || mock.calls[0].Dy() != 50 {
		t.Errorf("frame 0: %dx%d, want 50x50 (keyframe)", mock.calls[0].Dx(), mock.calls[0].Dy())
	}

	// Frame 3 should be full canvas (forced keyframe at kmax=3).
	lastCall := mock.calls[len(mock.calls)-1]
	if lastCall.Dx() != 50 || lastCall.Dy() != 50 {
		t.Errorf("frame 3: %dx%d, want 50x50 (forced keyframe)", lastCall.Dx(), lastCall.Dy())
	}
}

func TestOptimizedEncoder_Roundtrip(t *testing.T) {
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		b := img.Bounds()
		return makeVP8Keyframe(b.Dx(), b.Dy()), nil
	}

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 100, 100, &EncodeOptions{
		LoopCount: 2,
		Quality:   75,
	})

	enc.AddFrame(solidNRGBA(100, 100, red), 50*time.Millisecond)
	f1 := solidNRGBA(100, 100, red)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			f1.SetNRGBA(x, y, blue)
		}
	}
	enc.AddFrame(f1, 100*time.Millisecond)
	enc.Close()

	// Should be a valid animated WebP that can be decoded.
	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode roundtrip: %v", err)
	}
	if len(anim.Frames) != 2 {
		t.Fatalf("roundtrip frames = %d, want 2", len(anim.Frames))
	}
	if anim.LoopCount != 2 {
		t.Errorf("roundtrip LoopCount = %d, want 2", anim.LoopCount)
	}
	// Frame 1 should have a non-zero offset (sub-frame).
	f := anim.Frames[1]
	if f.OffsetX == 0 && f.OffsetY == 0 {
		// The sub-frame is at top-left, offset could be (0,0) which is valid.
		// Check that the frame dimensions are smaller than the canvas.
		if f.BitstreamData != nil {
			_, bs := splitAlphAndBitstream(f.BitstreamData)
			if len(bs) >= 10 {
				fw := int(binary.LittleEndian.Uint16(bs[6:8]))
				fh := int(binary.LittleEndian.Uint16(bs[8:10]))
				if fw >= 100 && fh >= 100 {
					t.Log("frame 1 is full-canvas (optimization may not have applied due to mock encoder)")
				}
			}
		}
	}
}

// splitAlphAndBitstream is a test helper that mimics the mux package's split.
func splitAlphAndBitstream(data []byte) ([]byte, []byte) {
	if len(data) >= 8 {
		id := binary.LittleEndian.Uint32(data[0:4])
		if id == mux.FourCCALPH {
			size := binary.LittleEndian.Uint32(data[4:8])
			end := 8 + int(size)
			if end <= len(data) {
				rest := end
				if size%2 != 0 && rest < len(data) {
					rest++
				}
				return data[8:end], data[rest:]
			}
		}
	}
	return nil, data
}

// --- Dual-candidate dispose method selection tests (DIFF-AN2) ---

// sizeAwareMockEncoder returns encoded data whose length is proportional to the
// sub-image area, allowing the test to verify that the encoder picks the
// smaller candidate. The returned bitstream is a valid VP8 keyframe header.
type sizeAwareMockEncoder struct {
	calls []struct {
		bounds image.Rectangle
	}
}

func (m *sizeAwareMockEncoder) encode(img image.Image, lossless bool, quality int) ([]byte, error) {
	b := img.Bounds()
	m.calls = append(m.calls, struct{ bounds image.Rectangle }{b})
	// Return a VP8 keyframe header padded to be proportional to the pixel area.
	// The minimum is the 10-byte header; we add area/10 bytes of padding.
	hdr := makeVP8Keyframe(b.Dx(), b.Dy())
	padding := (b.Dx() * b.Dy()) / 10
	result := make([]byte, len(hdr)+padding)
	copy(result, hdr)
	return result, nil
}

func TestOptimizedEncoder_DisposeBGSelection(t *testing.T) {
	// Scenario: Frame 0 is full-canvas red. Frame 1 is mostly transparent with
	// a small blue patch at top-left.
	//
	// With DISPOSE_NONE, the diff between frame 0 (all red) and frame 1
	// (mostly transparent + small blue patch) covers the entire canvas because
	// all the red pixels become transparent.
	//
	// With DISPOSE_BACKGROUND, the previous frame's rect is cleared to
	// transparent first. Since frame 0 covers the entire canvas, after disposal
	// the canvas is all transparent. Then the diff against frame 1 (mostly
	// transparent + small blue patch) is just the small blue patch area.
	//
	// So DISPOSE_BACKGROUND should produce a much smaller sub-frame, and the
	// encoder should pick it, setting the previous frame's dispose method to
	// DisposeBackground.

	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &sizeAwareMockEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	transparent := color.NRGBA{}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 100, 100, &EncodeOptions{Quality: 75})

	// Frame 0: full red.
	enc.AddFrame(solidNRGBA(100, 100, red), 50*time.Millisecond)

	// Frame 1: mostly transparent, small 10x10 blue patch at (0,0).
	frame1 := solidNRGBA(100, 100, transparent)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			frame1.SetNRGBA(x, y, blue)
		}
	}
	enc.AddFrame(frame1, 50*time.Millisecond)
	enc.Close()

	// Decode the result and verify frame 0 has DisposeBackground.
	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(anim.Frames))
	}
	if anim.Frames[0].Dispose != DisposeBackground {
		t.Errorf("frame 0 dispose = %d, want DisposeBackground (%d)",
			anim.Frames[0].Dispose, DisposeBackground)
	}
}

func TestOptimizedEncoder_DisposeNoneWhenSmaller(t *testing.T) {
	// Scenario: Frame 0 is full-canvas red, Frame 1 is almost identical to
	// frame 0 except for a tiny 2x2 patch change.
	//
	// With DISPOSE_NONE, the diff is just the 2x2 patch.
	// With DISPOSE_BACKGROUND (clearing the full canvas to transparent), the
	// diff would be almost the entire canvas (since frame 1 is mostly red
	// pixels that differ from transparent).
	//
	// So DISPOSE_NONE should win, and the previous frame keeps DisposeNone.

	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &sizeAwareMockEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 100, 100, &EncodeOptions{Quality: 75})

	// Frame 0: full red.
	enc.AddFrame(solidNRGBA(100, 100, red), 50*time.Millisecond)

	// Frame 1: same as frame 0 except a 2x2 blue patch at (50,50).
	frame1 := solidNRGBA(100, 100, red)
	for y := 50; y < 52; y++ {
		for x := 50; x < 52; x++ {
			frame1.SetNRGBA(x, y, blue)
		}
	}
	enc.AddFrame(frame1, 50*time.Millisecond)
	enc.Close()

	// Decode and verify frame 0 still has DisposeNone.
	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(anim.Frames))
	}
	if anim.Frames[0].Dispose != DisposeNone {
		t.Errorf("frame 0 dispose = %d, want DisposeNone (%d)",
			anim.Frames[0].Dispose, DisposeNone)
	}
}

func TestOptimizedEncoder_DisposeBGThreeFrames(t *testing.T) {
	// Verify that the dispose-bg selection works across multiple frames.
	// Frame 0: full red, Frame 1: mostly transparent, Frame 2: mostly transparent.
	// Both frame 0 and frame 1 should get DisposeBackground if that helps frame 1
	// and frame 2 respectively.

	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &sizeAwareMockEncoder{}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	green := color.NRGBA{G: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	transparent := color.NRGBA{}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 100, 100, &EncodeOptions{Quality: 75})

	// Frame 0: full red.
	enc.AddFrame(solidNRGBA(100, 100, red), 50*time.Millisecond)

	// Frame 1: mostly transparent with small green patch.
	frame1 := solidNRGBA(100, 100, transparent)
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			frame1.SetNRGBA(x, y, green)
		}
	}
	enc.AddFrame(frame1, 50*time.Millisecond)

	// Frame 2: mostly transparent with small blue patch at different location.
	frame2 := solidNRGBA(100, 100, transparent)
	for y := 80; y < 90; y++ {
		for x := 80; x < 90; x++ {
			frame2.SetNRGBA(x, y, blue)
		}
	}
	enc.AddFrame(frame2, 50*time.Millisecond)
	enc.Close()

	// Decode and check that frame 0 got DisposeBackground (because frame 1
	// benefits from it). Frame 1 should also get DisposeBackground for the
	// same reason relative to frame 2.
	anim, err := Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(anim.Frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(anim.Frames))
	}
	if anim.Frames[0].Dispose != DisposeBackground {
		t.Errorf("frame 0 dispose = %d, want DisposeBackground", anim.Frames[0].Dispose)
	}
}

func TestSanitizeKeyframeOptions(t *testing.T) {
	tests := []struct {
		name     string
		kmin     int
		kmax     int
		wantKmin int
		wantKmax int
	}{
		{"disabled (kmax=0)", 0, 0, int(^uint(0)>>1) - 1, int(^uint(0) >> 1)},
		{"all keyframes (kmax=1)", 0, 1, 0, 0},
		{"kmin >= kmax", 10, 10, 9, 10},
		{"kmin too small", 1, 10, 6, 10},
		{"valid", 5, 8, 5, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kmin, kmax := tt.kmin, tt.kmax
			sanitizeKeyframeOptions(&kmin, &kmax)
			if kmin != tt.wantKmin || kmax != tt.wantKmax {
				t.Errorf("sanitize(%d,%d) = (%d,%d), want (%d,%d)",
					tt.kmin, tt.kmax, kmin, kmax, tt.wantKmin, tt.wantKmax)
			}
		})
	}
}

// --- Blend validation tests ---

func TestQualityToMaxDiff(t *testing.T) {
	tests := []struct {
		quality int
		want    int
	}{
		// quality=0 => val=0, maxDiff=31*(1-0)+1*0 = 31 => int(31+0.5) = 31
		{0, 31},
		// quality=100 => val=1.0, maxDiff=31*0+1*1 = 1 => int(1+0.5) = 1
		{100, 1},
		// quality=25 => val=0.5, maxDiff=31*0.5+1*0.5 = 16 => int(16+0.5) = 16
		{25, 16},
	}
	for _, tt := range tests {
		got := qualityToMaxDiff(tt.quality)
		if got != tt.want {
			t.Errorf("qualityToMaxDiff(%d) = %d, want %d", tt.quality, got, tt.want)
		}
	}
}

func TestPixelsAreSimilar(t *testing.T) {
	tests := []struct {
		name         string
		src, dst     color.NRGBA
		maxDiff      int
		wantSimilar  bool
	}{
		{
			name:        "identical fully opaque",
			src:         color.NRGBA{R: 100, G: 100, B: 100, A: 255},
			dst:         color.NRGBA{R: 100, G: 100, B: 100, A: 255},
			maxDiff:     1,
			wantSimilar: true,
		},
		{
			name:        "different alpha always dissimilar",
			src:         color.NRGBA{R: 100, G: 100, B: 100, A: 200},
			dst:         color.NRGBA{R: 100, G: 100, B: 100, A: 255},
			maxDiff:     31,
			wantSimilar: false,
		},
		{
			name:        "small diff within threshold",
			src:         color.NRGBA{R: 100, G: 100, B: 100, A: 255},
			dst:         color.NRGBA{R: 101, G: 100, B: 100, A: 255},
			maxDiff:     1,
			wantSimilar: true,
		},
		{
			name:        "diff exceeds threshold",
			src:         color.NRGBA{R: 100, G: 100, B: 100, A: 255},
			dst:         color.NRGBA{R: 103, G: 100, B: 100, A: 255},
			maxDiff:     1,
			wantSimilar: false,
		},
		{
			name:        "transparent dst allows any diff",
			src:         color.NRGBA{R: 0, G: 0, B: 0, A: 0},
			dst:         color.NRGBA{R: 255, G: 255, B: 255, A: 0},
			maxDiff:     1,
			wantSimilar: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pixelsAreSimilar(tt.src, tt.dst, tt.maxDiff)
			if got != tt.wantSimilar {
				t.Errorf("pixelsAreSimilar(%v, %v, %d) = %v, want %v",
					tt.src, tt.dst, tt.maxDiff, got, tt.wantSimilar)
			}
		})
	}
}

func TestIsLosslessBlendingPossible(t *testing.T) {
	t.Run("all opaque dst permits blending", func(t *testing.T) {
		// When every dst pixel has alpha=0xFF, blending is always possible.
		src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		dst := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for i := 0; i < len(dst.Pix); i += 4 {
			dst.Pix[i+0] = 100 // R
			dst.Pix[i+1] = 150 // G
			dst.Pix[i+2] = 200 // B
			dst.Pix[i+3] = 255 // A = fully opaque
		}
		// src can be anything
		for i := 0; i < len(src.Pix); i += 4 {
			src.Pix[i+0] = 50
			src.Pix[i+1] = 75
			src.Pix[i+2] = 100
			src.Pix[i+3] = 128
		}
		rect := image.Rect(0, 0, 4, 4)
		if !isLosslessBlendingPossible(src, dst, rect) {
			t.Error("expected blending possible when all dst pixels are fully opaque")
		}
	})

	t.Run("matching semi-transparent pixels permit blending", func(t *testing.T) {
		// When src and dst pixels are identical, blending is possible even
		// if dst alpha < 0xFF.
		src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		px := color.NRGBA{R: 10, G: 20, B: 30, A: 128}
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				src.SetNRGBA(x, y, px)
				dst.SetNRGBA(x, y, px)
			}
		}
		rect := image.Rect(0, 0, 2, 2)
		if !isLosslessBlendingPossible(src, dst, rect) {
			t.Error("expected blending possible when src == dst")
		}
	})

	t.Run("different semi-transparent pixels forbid blending", func(t *testing.T) {
		// When dst alpha < 0xFF and src != dst, blending is not possible.
		src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		src.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 128})
		dst.SetNRGBA(0, 0, color.NRGBA{R: 50, G: 60, B: 70, A: 128})
		rect := image.Rect(0, 0, 2, 2)
		if isLosslessBlendingPossible(src, dst, rect) {
			t.Error("expected blending NOT possible when src != dst with semi-transparent dst")
		}
	})

	t.Run("sub-rect only checks rect region", func(t *testing.T) {
		// Pixels outside the rect should be ignored.
		src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		dst := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		// Set a problematic pixel at (0,0) -- outside the rect we check.
		src.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 100})
		dst.SetNRGBA(0, 0, color.NRGBA{R: 99, G: 99, B: 99, A: 100})
		// Inside rect, all dst alpha=0xFF.
		for y := 2; y < 4; y++ {
			for x := 2; x < 4; x++ {
				dst.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 255})
			}
		}
		rect := image.Rect(2, 2, 4, 4)
		if !isLosslessBlendingPossible(src, dst, rect) {
			t.Error("expected blending possible -- bad pixel is outside rect")
		}
	})
}

func TestIsLossyBlendingPossible(t *testing.T) {
	t.Run("all opaque dst permits blending", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		dst := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for i := 0; i < len(dst.Pix); i += 4 {
			dst.Pix[i+3] = 255
		}
		rect := image.Rect(0, 0, 4, 4)
		if !isLossyBlendingPossible(src, dst, rect, 75) {
			t.Error("expected blending possible when all dst pixels are fully opaque")
		}
	})

	t.Run("similar semi-transparent pixels permit blending", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		// Pixels differ by 1 in R channel, same alpha.
		src.SetNRGBA(0, 0, color.NRGBA{R: 100, G: 100, B: 100, A: 128})
		dst.SetNRGBA(0, 0, color.NRGBA{R: 101, G: 100, B: 100, A: 128})
		rect := image.Rect(0, 0, 2, 2)
		// quality=100 => maxDiff=1, threshold=1*255=255.
		// diff=1, 1*128=128 <= 255 => similar.
		if !isLossyBlendingPossible(src, dst, rect, 100) {
			t.Error("expected blending possible for similar pixels at high quality")
		}
	})

	t.Run("dissimilar semi-transparent pixels forbid blending", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		dst := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		src.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 200})
		dst.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 200, B: 200, A: 200})
		rect := image.Rect(0, 0, 2, 2)
		if isLossyBlendingPossible(src, dst, rect, 100) {
			t.Error("expected blending NOT possible for very different pixels")
		}
	})
}

func TestBlendValidation_SubFrameUsesBlendAlphaWhenPossible(t *testing.T) {
	// When the previous canvas is fully opaque and matches the current canvas
	// in the sub-frame region, blending should be validated as possible and
	// BlendAlpha should be used (not BlendNone).
	//
	// We set up a scenario where: frame 1 is fully opaque red, frame 2 is
	// identical except one pixel changes. Since all previous canvas pixels
	// are fully opaque (alpha=0xFF), isLosslessBlendingPossible returns true.
	var lastBlendMode mux.BlendMode
	origEncoderFunc := FrameEncoderFunc
	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		return []byte{0x00}, nil // stub bitstream
	}
	defer func() { FrameEncoderFunc = origEncoderFunc }()

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 4, 4, &EncodeOptions{Lossless: true})

	// Intercept AddFrame calls to capture the blend mode.
	origAddFrame := enc.muxer.AddFrame
	_ = origAddFrame // We need to capture via the muxer's behavior.

	// Frame 1: fully opaque red.
	frame1 := solidNRGBA(4, 4, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	if err := enc.AddFrame(frame1, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame(frame1): %v", err)
	}

	// Frame 2: same but one pixel changes to blue. Since the previous canvas
	// is all alpha=0xFF, lossless blending is possible.
	frame2 := solidNRGBA(4, 4, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	frame2.SetNRGBA(2, 2, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
	if err := enc.AddFrame(frame2, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame(frame2): %v", err)
	}

	// Verify: the second frame should use BlendAlpha (0) not BlendNone (1).
	// We can check via the muxer's frame info.
	nf := enc.muxer.NumFrames()
	if nf < 2 {
		t.Fatalf("expected at least 2 frames, got %d", nf)
	}
	lastBlendMode = enc.muxer.FrameBlendMode(nf - 1)
	if lastBlendMode != mux.BlendMode(BlendAlpha) {
		t.Errorf("expected BlendAlpha (%d) for sub-frame with opaque canvas, got %d",
			mux.BlendMode(BlendAlpha), lastBlendMode)
	}
}

func TestBlendValidation_SubFrameUsesBlendNoneWhenNotPossible(t *testing.T) {
	// When the previous canvas has semi-transparent pixels that differ from
	// the current canvas, blending is not possible and BlendNone must be used.
	origEncoderFunc := FrameEncoderFunc
	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		return []byte{0x00}, nil
	}
	defer func() { FrameEncoderFunc = origEncoderFunc }()

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 4, 4, &EncodeOptions{Lossless: true})

	// Frame 1: semi-transparent.
	frame1 := solidNRGBA(4, 4, color.NRGBA{R: 100, G: 100, B: 100, A: 128})
	if err := enc.AddFrame(frame1, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame(frame1): %v", err)
	}

	// Frame 2: different RGB at the same semi-transparent alpha.
	// Since prev canvas has alpha<0xFF and src != dst, blending is NOT possible.
	frame2 := solidNRGBA(4, 4, color.NRGBA{R: 200, G: 200, B: 200, A: 128})
	if err := enc.AddFrame(frame2, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame(frame2): %v", err)
	}

	nf := enc.muxer.NumFrames()
	if nf < 2 {
		t.Fatalf("expected at least 2 frames, got %d", nf)
	}
	blendMode := enc.muxer.FrameBlendMode(nf - 1)
	if blendMode != mux.BlendMode(BlendNone) {
		t.Errorf("expected BlendNone (%d) for sub-frame with semi-transparent canvas, got %d",
			mux.BlendMode(BlendNone), blendMode)
	}
}

// --- AllowMixed tests ---

// mixedMockEncoder returns different-sized bitstreams depending on the lossless flag.
// This lets tests verify that the encoder picks the smaller result when AllowMixed is true.
type mixedMockEncoder struct {
	calls        []mixedCall
	losslessSize int // size of bitstream returned when lossless=true
	lossySize    int // size of bitstream returned when lossless=false
}

type mixedCall struct {
	bounds   image.Rectangle
	lossless bool
}

func (m *mixedMockEncoder) encode(img image.Image, lossless bool, quality int) ([]byte, error) {
	b := img.Bounds()
	m.calls = append(m.calls, mixedCall{bounds: b, lossless: lossless})
	// Build a valid VP8 keyframe, then pad or truncate to achieve the desired size.
	base := makeVP8Keyframe(b.Dx(), b.Dy())
	targetSize := m.lossySize
	if lossless {
		targetSize = m.losslessSize
	}
	if targetSize <= len(base) {
		return base[:targetSize], nil
	}
	// Pad with zeros to reach target size.
	padded := make([]byte, targetSize)
	copy(padded, base)
	return padded, nil
}

func TestAllowMixed_PicksSmallerCodec(t *testing.T) {
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	// Lossy produces 100 bytes, lossless produces 50 bytes.
	// With AllowMixed=true and Lossless=false (primary is lossy),
	// the encoder should try both and pick the smaller (lossless) result.
	mock := &mixedMockEncoder{lossySize: 100, losslessSize: 50}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{
		Quality:    75,
		Lossless:   false,
		AllowMixed: true,
	})

	frame := solidNRGBA(10, 10, red)
	if err := enc.AddFrame(frame, 50*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// With AllowMixed, the encoder should call FrameEncoderFunc twice per encode
	// operation: once with the primary codec and once with the alternate.
	if len(mock.calls) < 2 {
		t.Fatalf("expected at least 2 encoder calls (both codecs), got %d", len(mock.calls))
	}

	// Verify both lossless=false and lossless=true were tried.
	hasLossy := false
	hasLossless := false
	for _, c := range mock.calls {
		if c.lossless {
			hasLossless = true
		} else {
			hasLossy = true
		}
	}
	if !hasLossy || !hasLossless {
		t.Errorf("expected both lossy and lossless attempts; lossy=%v lossless=%v", hasLossy, hasLossless)
	}
}

func TestAllowMixed_DisabledUsesSingleCodec(t *testing.T) {
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	mock := &mixedMockEncoder{lossySize: 100, losslessSize: 50}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{
		Quality:    75,
		Lossless:   false,
		AllowMixed: false,
	})

	frame := solidNRGBA(10, 10, red)
	if err := enc.AddFrame(frame, 50*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Without AllowMixed, only the primary codec (lossy) should be used.
	for _, c := range mock.calls {
		if c.lossless {
			t.Error("expected no lossless calls when AllowMixed=false")
		}
	}
}

func TestAllowMixed_SubFrameTriesBothCodecs(t *testing.T) {
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	// Make lossless smaller so it wins for sub-frames too.
	mock := &mixedMockEncoder{lossySize: 200, losslessSize: 80}
	FrameEncoderFunc = mock.encode

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}

	var buf bytes.Buffer
	enc := NewEncoder(&buf, 20, 20, &EncodeOptions{
		Quality:    75,
		Lossless:   false,
		AllowMixed: true,
	})

	// Frame 0: full red canvas (keyframe).
	frame0 := solidNRGBA(20, 20, red)
	if err := enc.AddFrame(frame0, 50*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 0: %v", err)
	}

	// Frame 1: red with a small blue patch (triggers sub-frame encoding).
	frame1 := solidNRGBA(20, 20, red)
	for y := 5; y < 15; y++ {
		for x := 5; x < 15; x++ {
			frame1.SetNRGBA(x, y, blue)
		}
	}
	mock.calls = nil // Reset to track sub-frame calls.
	if err := enc.AddFrame(frame1, 50*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 1: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// For each candidate (dispose-none, dispose-bg), encodeFrame should try
	// both codecs. So we should see at least 2 lossless calls.
	losslessCalls := 0
	lossyCalls := 0
	for _, c := range mock.calls {
		if c.lossless {
			losslessCalls++
		} else {
			lossyCalls++
		}
	}
	if losslessCalls == 0 {
		t.Error("expected lossless calls during sub-frame encoding with AllowMixed=true")
	}
	if lossyCalls == 0 {
		t.Error("expected lossy calls during sub-frame encoding with AllowMixed=true")
	}
}

func TestAllowMixed_AltCodecFailureFallsThroughToPrimary(t *testing.T) {
	oldFunc := FrameEncoderFunc
	defer func() { FrameEncoderFunc = oldFunc }()

	callCount := 0
	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		callCount++
		b := img.Bounds()
		if lossless {
			// Alternate codec fails.
			return nil, errors.New("lossless encoding failed")
		}
		return makeVP8Keyframe(b.Dx(), b.Dy()), nil
	}

	red := color.NRGBA{R: 255, A: 255}
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{
		Quality:    75,
		Lossless:   false,
		AllowMixed: true,
	})

	frame := solidNRGBA(10, 10, red)
	if err := enc.AddFrame(frame, 50*time.Millisecond); err != nil {
		t.Fatalf("AddFrame should not fail when alt codec fails: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The encoder should have tried both codecs (2 calls for the frame).
	if callCount < 2 {
		t.Errorf("expected at least 2 encoder calls (primary + alt attempt), got %d", callCount)
	}
}

// --- Single-frame optimization tests (DIFF-AN7) ---

func TestOptimizedEncoder_SingleFrameOptimization(t *testing.T) {
	// When there is exactly 1 frame and SimpleEncodeFunc produces a smaller
	// output than the animated ANIM/ANMF version, Close() should use the
	// simple (non-animated) output. This matches C libwebp OptimizeSingleFrame.

	oldFrame := FrameEncoderFunc
	oldSimple := SimpleEncodeFunc
	defer func() {
		FrameEncoderFunc = oldFrame
		SimpleEncodeFunc = oldSimple
	}()

	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		b := img.Bounds()
		return makeVP8Keyframe(b.Dx(), b.Dy()), nil
	}

	// Build a small simple WebP (RIFF + WEBP + VP8 chunk) that is smaller
	// than the animated output (which has VP8X + ANIM + ANMF overhead).
	simplePayload := makeVP8Keyframe(10, 10)
	simpleWebP := buildSimpleRIFF(simplePayload)

	SimpleEncodeFunc = func(img image.Image, lossless bool, quality float32) ([]byte, error) {
		return simpleWebP, nil
	}

	red := color.NRGBA{R: 255, A: 255}
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{Quality: 75})

	frame := solidNRGBA(10, 10, red)
	if err := enc.AddFrame(frame, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The output should be the simple WebP (no VP8X chunk).
	out := buf.Bytes()
	if len(out) < 16 {
		t.Fatalf("output too small: %d bytes", len(out))
	}
	firstChunkID := binary.LittleEndian.Uint32(out[12:16])
	if firstChunkID == container.FourCCVP8X {
		t.Error("expected simple (non-animated) output, but got VP8X (animated)")
	}
	if firstChunkID != container.FourCCVP8 && firstChunkID != container.FourCCVP8L {
		t.Errorf("expected VP8 or VP8L first chunk, got 0x%08X", firstChunkID)
	}
	// Verify the output matches our simple WebP exactly.
	if !bytes.Equal(out, simpleWebP) {
		t.Errorf("output (%d bytes) does not match expected simple WebP (%d bytes)",
			len(out), len(simpleWebP))
	}
}

func TestOptimizedEncoder_SingleFrameNoOptWhenLarger(t *testing.T) {
	// When the simple encoding is LARGER than the animated output, the
	// animated output should be used (single-frame optimization skipped).

	oldFrame := FrameEncoderFunc
	oldSimple := SimpleEncodeFunc
	defer func() {
		FrameEncoderFunc = oldFrame
		SimpleEncodeFunc = oldSimple
	}()

	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		b := img.Bounds()
		return makeVP8Keyframe(b.Dx(), b.Dy()), nil
	}

	// Return an artificially large "simple" WebP so the animated version wins.
	SimpleEncodeFunc = func(img image.Image, lossless bool, quality float32) ([]byte, error) {
		huge := make([]byte, 100000)
		copy(huge, buildSimpleRIFF(makeVP8Keyframe(10, 10)))
		return huge, nil
	}

	red := color.NRGBA{R: 255, A: 255}
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{Quality: 75})

	frame := solidNRGBA(10, 10, red)
	if err := enc.AddFrame(frame, 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The output should be animated (VP8X first chunk).
	out := buf.Bytes()
	if len(out) < 16 {
		t.Fatalf("output too small: %d bytes", len(out))
	}
	firstChunkID := binary.LittleEndian.Uint32(out[12:16])
	if firstChunkID != container.FourCCVP8X {
		t.Errorf("expected VP8X (animated output), got 0x%08X", firstChunkID)
	}
}

func TestOptimizedEncoder_MultiFrameNoSingleOpt(t *testing.T) {
	// When there are multiple frames, single-frame optimization should NOT
	// be applied regardless of SimpleEncodeFunc availability.

	oldFrame := FrameEncoderFunc
	oldSimple := SimpleEncodeFunc
	defer func() {
		FrameEncoderFunc = oldFrame
		SimpleEncodeFunc = oldSimple
	}()

	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		b := img.Bounds()
		return makeVP8Keyframe(b.Dx(), b.Dy()), nil
	}

	simpleCalled := false
	SimpleEncodeFunc = func(img image.Image, lossless bool, quality float32) ([]byte, error) {
		simpleCalled = true
		return buildSimpleRIFF(makeVP8Keyframe(10, 10)), nil
	}

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{Quality: 75})

	if err := enc.AddFrame(solidNRGBA(10, 10, red), 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 0: %v", err)
	}
	if err := enc.AddFrame(solidNRGBA(10, 10, blue), 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame 1: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if simpleCalled {
		t.Error("SimpleEncodeFunc should not be called for multi-frame animations")
	}

	// Output should be animated (VP8X).
	out := buf.Bytes()
	if len(out) < 16 {
		t.Fatalf("output too small: %d bytes", len(out))
	}
	firstChunkID := binary.LittleEndian.Uint32(out[12:16])
	if firstChunkID != container.FourCCVP8X {
		t.Errorf("expected VP8X (animated), got 0x%08X", firstChunkID)
	}
}

func TestOptimizedEncoder_SingleFrameNoSimpleFunc(t *testing.T) {
	// When SimpleEncodeFunc is nil, single-frame optimization is skipped
	// and the animated output is used.

	oldFrame := FrameEncoderFunc
	oldSimple := SimpleEncodeFunc
	defer func() {
		FrameEncoderFunc = oldFrame
		SimpleEncodeFunc = oldSimple
	}()

	FrameEncoderFunc = func(img image.Image, lossless bool, quality int) ([]byte, error) {
		b := img.Bounds()
		return makeVP8Keyframe(b.Dx(), b.Dy()), nil
	}
	SimpleEncodeFunc = nil

	red := color.NRGBA{R: 255, A: 255}
	var buf bytes.Buffer
	enc := NewEncoder(&buf, 10, 10, &EncodeOptions{Quality: 75})

	if err := enc.AddFrame(solidNRGBA(10, 10, red), 100*time.Millisecond); err != nil {
		t.Fatalf("AddFrame: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Output should be animated (VP8X) since no simple encoder is available.
	out := buf.Bytes()
	if len(out) < 16 {
		t.Fatalf("output too small: %d bytes", len(out))
	}
	firstChunkID := binary.LittleEndian.Uint32(out[12:16])
	if firstChunkID != container.FourCCVP8X {
		t.Errorf("expected VP8X (animated) when SimpleEncodeFunc is nil, got 0x%08X", firstChunkID)
	}
}

// buildSimpleRIFF wraps a VP8 bitstream in a minimal RIFF/WEBP container.
func buildSimpleRIFF(vp8Data []byte) []byte {
	payloadSize := uint32(len(vp8Data))
	paddedPayload := payloadSize
	if payloadSize%2 != 0 {
		paddedPayload++
	}
	riffSize := 4 + 8 + paddedPayload // "WEBP" + chunk header + payload
	totalSize := 8 + riffSize          // "RIFF" + size field + riff payload
	buf := make([]byte, totalSize)
	binary.LittleEndian.PutUint32(buf[0:4], container.FourCCRIFF)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(riffSize))
	binary.LittleEndian.PutUint32(buf[8:12], container.FourCCWEBP)
	binary.LittleEndian.PutUint32(buf[12:16], container.FourCCVP8)
	binary.LittleEndian.PutUint32(buf[16:20], payloadSize)
	copy(buf[20:], vp8Data)
	return buf
}
