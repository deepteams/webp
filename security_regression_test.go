package webp

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"runtime"
	"testing"
	"time"

	"github.com/deepteams/webp/animation"
	"github.com/deepteams/webp/mux"
)

// Regression tests for the security fixes on the lossy token buffer
// (Partitions > 0), ANMF frame sizing, and RIFF/chunk size handling.
// Sizes are kept small so the tests also run in the 32-bit CI jobs.

func secLE24(b []byte, v int) {
	b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16)
}

func secChunk(tag string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload)+1)
	copy(out, tag)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(payload)))
	out = append(out, payload...)
	if len(payload)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func secRIFF(body []byte) []byte {
	out := make([]byte, 8, 8+len(body))
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(body)))
	return append(out, body...)
}

// secBitstream returns a raw VP8 or VP8L bitstream (no RIFF/chunk header)
// of the given size.
func secBitstream(t *testing.T, w, h int, lossless bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	data := mustEncode(t, img, &EncoderOptions{Lossless: lossless, Quality: 50})
	tag := string(data[12:16])
	if tag != "VP8 " && tag != "VP8L" {
		t.Fatalf("unexpected first chunk %q", tag)
	}
	size := binary.LittleEndian.Uint32(data[16:20])
	return data[20 : 20+size]
}

// secAnimFile builds an animated WebP with one ANMF frame. The ANMF header
// declares (ax,ay,aw,ah) independently of the bitstream's own size.
func secAnimFile(canvasW, canvasH, ax, ay, aw, ah int, bs []byte, lossless bool) []byte {
	vp8x := make([]byte, 10)
	vp8x[0] = 0x02 // animation flag
	secLE24(vp8x[4:], canvasW-1)
	secLE24(vp8x[7:], canvasH-1)
	hdr := make([]byte, 16)
	secLE24(hdr[0:], ax/2)
	secLE24(hdr[3:], ay/2)
	secLE24(hdr[6:], aw-1)
	secLE24(hdr[9:], ah-1)
	secLE24(hdr[12:], 100)
	tag := "VP8 "
	if lossless {
		tag = "VP8L"
	}
	body := []byte("WEBP")
	body = append(body, secChunk("VP8X", vp8x)...)
	body = append(body, secChunk("ANIM", make([]byte, 6))...)
	body = append(body, secChunk("ANMF", append(hdr, secChunk(tag, bs)...))...)
	return secRIFF(body)
}

// A frame whose bitstream is larger than the canvas must be rejected by every
// public entry point, so that the canvas size reported by DecodeConfig (or the
// demuxer) bounds the decoded image size.
func TestSecurityAnimFrameLargerThanCanvas(t *testing.T) {
	for _, lossless := range []bool{false, true} {
		bs := secBitstream(t, 64, 64, lossless)
		for _, anmf := range []int{1, 64} { // ANMF header lies, or tells the truth
			file := secAnimFile(1, 1, 0, 0, anmf, anmf, bs, lossless)
			if cfg, err := DecodeConfig(bytes.NewReader(file)); err == nil {
				t.Errorf("lossless=%v anmf=%d: DecodeConfig accepted %dx%d", lossless, anmf, cfg.Width, cfg.Height)
			}
			if img, err := Decode(bytes.NewReader(file)); err == nil {
				t.Errorf("lossless=%v anmf=%d: Decode accepted, bounds %v", lossless, anmf, img.Bounds())
			}
			if _, err := mux.NewDemuxer(file); err == nil {
				t.Errorf("lossless=%v anmf=%d: mux.NewDemuxer accepted", lossless, anmf)
			}
			if _, err := animation.DecodeBytes(file); err == nil {
				t.Errorf("lossless=%v anmf=%d: animation.DecodeBytes accepted", lossless, anmf)
			}
		}
	}
}

// A frame offset that pushes the frame past the canvas edge is rejected.
func TestSecurityAnimFrameOffsetOutsideCanvas(t *testing.T) {
	bs := secBitstream(t, 16, 16, true)
	file := secAnimFile(32, 32, 18, 0, 16, 16, bs, true) // 18+16 > 32
	if _, err := Decode(bytes.NewReader(file)); err == nil {
		t.Error("Decode accepted a frame extending past the canvas")
	}
	if _, err := animation.DecodeBytes(file); err == nil {
		t.Error("animation.DecodeBytes accepted a frame extending past the canvas")
	}
}

// Like libwebp, an ANMF size that differs from the bitstream size is not an
// error: the bitstream size wins, as long as it fits in the canvas.
func TestSecurityAnimFrameUsesBitstreamSize(t *testing.T) {
	for _, lossless := range []bool{false, true} {
		bs := secBitstream(t, 16, 16, lossless)
		file := secAnimFile(64, 64, 0, 0, 32, 32, bs, lossless)

		img, err := Decode(bytes.NewReader(file))
		if err != nil {
			t.Fatalf("lossless=%v: Decode: %v", lossless, err)
		}
		if got := img.Bounds().Size(); got != image.Pt(16, 16) {
			t.Errorf("lossless=%v: Decode size %v, want 16x16", lossless, got)
		}

		dmx, err := mux.NewDemuxer(file)
		if err != nil {
			t.Fatalf("lossless=%v: mux.NewDemuxer: %v", lossless, err)
		}
		fi, err := dmx.Frame(0)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Width != 16 || fi.Height != 16 {
			t.Errorf("lossless=%v: demuxed frame %dx%d, want 16x16", lossless, fi.Width, fi.Height)
		}
	}
}

// Animations produced by this package (sub-frames with offsets) must still
// parse and decode through both the container and the demuxer.
func TestSecurityOwnAnimationsStillDecode(t *testing.T) {
	frame := func(w, h, step int) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := color.NRGBA{uint8(x * 9), uint8(y * 5), 80, 255}
				if x >= w/3 && x < w/3+4 && y >= step && y < step+4 { // moving square
					c = color.NRGBA{255, 0, 0, 255}
				}
				img.SetNRGBA(x, y, c)
			}
		}
		return img
	}
	for _, opts := range []animation.EncodeOptions{
		{Quality: 75}, {Lossless: true}, {AllowMixed: true, Quality: 60},
	} {
		const w, h = 33, 21
		var buf bytes.Buffer
		enc := animation.NewEncoder(&buf, w, h, &opts)
		for i := 0; i < 4; i++ {
			if err := enc.AddFrame(frame(w, h, i*3), 50*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(bytes.NewReader(buf.Bytes())); err != nil {
			t.Errorf("%+v: Decode: %v", opts, err)
		}
		anim, err := animation.DecodeBytes(buf.Bytes())
		if err != nil {
			t.Fatalf("%+v: animation.DecodeBytes: %v", opts, err)
		}
		if err := anim.DecodeFrames(); err != nil {
			t.Fatalf("%+v: DecodeFrames: %v", opts, err)
		}
		dec, err := animation.NewAnimDecoder(anim)
		if err != nil {
			t.Fatal(err)
		}
		for dec.HasNext() {
			if _, _, err := dec.NextFrame(); err != nil {
				t.Fatalf("%+v: NextFrame: %v", opts, err)
			}
		}
	}
}

// A RIFF size smaller than a chunk header used to panic in the demuxer.
func TestSecurityDemuxSmallRIFFSize(t *testing.T) {
	for size := uint32(0); size < 8; size++ {
		data := []byte("RIFF\x00\x00\x00\x00WEBP")
		binary.LittleEndian.PutUint32(data[4:], size)
		assertNoPanic(t, "mux.NewDemuxer", func() {
			if _, err := mux.NewDemuxer(data); err == nil {
				t.Errorf("size=%d: mux.NewDemuxer accepted", size)
			}
		})
		assertNoPanic(t, "animation.DecodeBytes", func() {
			if _, err := animation.DecodeBytes(data); err == nil {
				t.Errorf("size=%d: animation.DecodeBytes accepted", size)
			}
		})
	}
}

// A chunk size near 4 GiB must be reported as truncated, not overflow int on
// 32-bit platforms and panic.
func TestSecurityReadChunkHugeSize(t *testing.T) {
	data := secChunk("VP8 ", make([]byte, 16))
	binary.LittleEndian.PutUint32(data[4:], 0xFFFFFFF0)
	assertNoPanic(t, "mux.ReadChunk", func() {
		if _, _, err := mux.ReadChunk(data); err == nil {
			t.Error("mux.ReadChunk accepted a truncated huge chunk")
		}
	})
}

// With Partitions > 0, macroblocks skipped because all their coefficients are
// zero used to keep stale token offsets (from an earlier pass, or from an
// earlier Encode on the pooled encoder). The output must decode to the same
// pixels as Partitions=0, independently of what was encoded before.
func TestSecurityLossyPartitionsSkippedMacroblocks(t *testing.T) {
	const w, h = 96, 64
	// Left half flat (skipped MBs), right half noisy (many tokens).
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBA{120, 140, 160, 255}
			if x >= w/2 {
				seed = seed*1664525 + 1013904223
				c = color.NRGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	// A different image of the same size, to dirty the pooled encoder.
	other := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range other.Pix {
		seed = seed*1664525 + 1013904223
		other.Pix[i] = uint8(seed >> 24)
	}

	decodePix := func(data []byte) []uint8 {
		t.Helper()
		out, err := Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		return out.(*image.NRGBA).Pix
	}

	for _, method := range []int{0, 4, 6} {
		want := decodePix(mustEncode(t, img, &EncoderOptions{Quality: 75, Method: method}))
		for parts := 1; parts <= 3; parts++ {
			opts := &EncoderOptions{Quality: 75, Method: method, Partitions: parts}
			for round := 0; round < 2; round++ {
				mustEncode(t, other, &EncoderOptions{Quality: 100, Method: method, Partitions: parts})
				var data []byte
				assertNoPanic(t, "Encode", func() { data = mustEncode(t, img, opts) })
				if data == nil {
					continue
				}
				if got := decodePix(data); !bytes.Equal(got, want) {
					t.Errorf("method=%d partitions=%d round=%d: pixels differ from Partitions=0", method, parts, round)
				}
			}
		}
	}
}

// A palette (<=16 colours, packed pixels) combined with another transform is
// inverted in place; unpacking used to overwrite packed indices not yet read,
// corrupting files produced by this package's own encoder (and by cwebp).
func TestSecurityLosslessPaletteInPlace(t *testing.T) {
	pal := []color.NRGBA{{200, 55, 100, 255}, {0, 255, 0, 255}, {10, 20, 30, 255}, {255, 255, 255, 255}}
	for _, colors := range []int{2, 4, 16} {
		const w, h = 97, 33
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		seed := uint32(colors)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				seed = seed*1664525 + 1013904223
				i := (x + y) % colors
				if x%3 == 0 {
					i = int(seed>>16) % colors
				}
				c := pal[i%len(pal)]
				c.B = uint8(i * 15)
				img.SetNRGBA(x, y, c)
			}
		}
		for _, m := range []int{4, 5, 6} {
			opts := &EncoderOptions{Lossless: true, Method: m, Quality: 100, Exact: true}
			got := encodeAndDecode(t, img, opts).(*image.NRGBA)
			if !bytes.Equal(got.Pix, img.Pix) {
				t.Errorf("colors=%d method=%d: lossless round trip is not exact", colors, m)
			}
		}
	}
}

// Encoding a SubImage as an animation frame must not leak pixels from the
// parent image outside the crop.
func TestSecurityAnimEncoderSubImage(t *testing.T) {
	red := color.NRGBA{255, 0, 0, 255}
	parent := image.NewNRGBA(image.Rect(0, 0, 96, 64))
	for i := 0; i < len(parent.Pix); i += 4 {
		copy(parent.Pix[i:], []uint8{red.R, red.G, red.B, red.A})
	}
	crop := image.Rect(10, 7, 50, 40)
	for y := crop.Min.Y; y < crop.Max.Y; y++ {
		for x := crop.Min.X; x < crop.Max.X; x++ {
			parent.SetNRGBA(x, y, color.NRGBA{uint8(x * 3), 120, uint8(y * 5), 255})
		}
	}
	sub := parent.SubImage(crop).(*image.NRGBA)
	for _, opts := range []animation.EncodeOptions{{Lossless: true}, {AllowMixed: true, Quality: 90}} {
		for _, frames := range []int{1, 3} {
			var buf bytes.Buffer
			enc := animation.NewEncoder(&buf, crop.Dx(), crop.Dy(), &opts)
			for i := 0; i < frames; i++ {
				if err := enc.AddFrame(sub, 50*time.Millisecond); err != nil {
					t.Fatal(err)
				}
			}
			if err := enc.Close(); err != nil {
				t.Fatal(err)
			}
			img, err := Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			n := img.(*image.NRGBA)
			for y := 0; y < crop.Dy(); y++ {
				for x := 0; x < crop.Dx(); x++ {
					if n.NRGBAAt(x, y) != sub.NRGBAAt(crop.Min.X+x, crop.Min.Y+y) {
						t.Fatalf("%+v frames=%d: pixel (%d,%d) = %v, want %v", opts, frames, x, y, n.NRGBAAt(x, y), sub.NRGBAAt(crop.Min.X+x, crop.Min.Y+y))
					}
				}
			}
		}
	}
}

// The demuxer applies the same canvas area limit as the container parser,
// and the animation decoder does not allocate its canvas before a frame is
// available.
func TestSecurityAnimCanvasLimits(t *testing.T) {
	bs := secBitstream(t, 1, 1, true)
	huge := secAnimFile(1<<16, 1<<14, 0, 0, 1, 1, bs, true) // 2^30 pixels
	if _, err := animation.DecodeBytes(huge); err == nil {
		t.Error("animation.DecodeBytes accepted a 2^30-pixel canvas")
	}

	big := secAnimFile(8192, 8192, 0, 0, 1, 1, bs, true) // 256 MiB per canvas buffer
	anim, err := animation.DecodeBytes(big)
	if err != nil {
		t.Fatal(err)
	}
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	dec, err := animation.NewAnimDecoder(anim)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := dec.NextFrame(); err == nil { // frames not decoded yet
		t.Fatal("NextFrame succeeded without decoded frames")
	}
	runtime.ReadMemStats(&m1)
	if d := m1.TotalAlloc - m0.TotalAlloc; d > 16<<20 {
		t.Errorf("NewAnimDecoder + failing NextFrame allocated %d MiB", d>>20)
	}
}
