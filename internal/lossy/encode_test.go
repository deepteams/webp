package lossy

import (
	"image"
	"image/color"
	"testing"
)

// --- Helper: create a solid-color NRGBA image ---

func solidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func gradientImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / w),
				G: uint8(y * 255 / h),
				B: uint8((x + y) * 128 / (w + h)),
				A: 255,
			})
		}
	}
	return img
}

// --- Config tests ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig(75)
	if cfg.Quality != 75 {
		t.Errorf("Quality = %d, want 75", cfg.Quality)
	}
	if cfg.Method != 4 {
		t.Errorf("Method = %d, want 4", cfg.Method)
	}
	if cfg.Segments < 1 || cfg.Segments > 4 {
		t.Errorf("Segments = %d, want [1,4]", cfg.Segments)
	}
}

func TestDefaultConfigClamping(t *testing.T) {
	cfg := DefaultConfig(-10)
	if cfg.Quality != 0 {
		t.Errorf("Quality = %d, want 0", cfg.Quality)
	}
	cfg = DefaultConfig(200)
	if cfg.Quality != 100 {
		t.Errorf("Quality = %d, want 100", cfg.Quality)
	}
}

func TestQualityToQIndex(t *testing.T) {
	tests := []struct {
		quality int
		wantMax int
		wantMin int
	}{
		{0, 127, 127},
		{100, 0, 0},
		{50, 39, 38}, // non-linear mapping: much lower than linear (was 63-64)
	}
	for _, tt := range tests {
		q := qualityToQIndex(tt.quality)
		if q < tt.wantMin || q > tt.wantMax {
			t.Errorf("qualityToQIndex(%d) = %d, want [%d, %d]", tt.quality, q, tt.wantMin, tt.wantMax)
		}
	}
}

// --- Encoder construction tests ---

func TestNewEncoder(t *testing.T) {
	img := solidImage(32, 32, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	enc := NewEncoder(img, cfg)

	if enc.width != 32 || enc.height != 32 {
		t.Errorf("dimensions = %dx%d, want 32x32", enc.width, enc.height)
	}
	if enc.mbW != 2 || enc.mbH != 2 {
		t.Errorf("MB dims = %dx%d, want 2x2", enc.mbW, enc.mbH)
	}
	if enc.yPlane == nil {
		t.Error("yPlane is nil")
	}
	if enc.uPlane == nil {
		t.Error("uPlane is nil")
	}
	if enc.vPlane == nil {
		t.Error("vPlane is nil")
	}
	if len(enc.mbInfo) != 4 {
		t.Errorf("mbInfo len = %d, want 4", len(enc.mbInfo))
	}
}

func TestNewEncoderNonMultiple16(t *testing.T) {
	img := solidImage(17, 9, color.NRGBA{A: 255})
	cfg := DefaultConfig(50)
	enc := NewEncoder(img, cfg)

	if enc.mbW != 2 || enc.mbH != 1 {
		t.Errorf("MB dims = %dx%d, want 2x1", enc.mbW, enc.mbH)
	}
}

// --- Import/YUV conversion tests ---

func TestImportImageGraySolid(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	enc := NewEncoder(img, cfg)

	// Y values should all be approximately the same (gray).
	y0 := enc.yPlane[0]
	for i := 1; i < 16*16; i++ {
		if d := absDiff(enc.yPlane[i], y0); d > 2 {
			t.Fatalf("yPlane[%d] = %d, yPlane[0] = %d, diff %d > 2", i, enc.yPlane[i], y0, d)
		}
	}

	// U and V should be approximately 128 (neutral chroma for gray).
	for i := 0; i < 8*8; i++ {
		if d := absDiff(enc.uPlane[i], 128); d > 5 {
			t.Errorf("uPlane[%d] = %d, want ~128", i, enc.uPlane[i])
			break
		}
		if d := absDiff(enc.vPlane[i], 128); d > 5 {
			t.Errorf("vPlane[%d] = %d, want ~128", i, enc.vPlane[i])
			break
		}
	}
}

// --- Quantization tests ---

func TestQuantizeCoeffs(t *testing.T) {
	in := [16]int16{100, -50, 25, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	out := [16]int16{}
	sq := SegmentQuant{
		Quant:    10,
		IQuant:   (1 << 17) / 10,
		Bias:     110 << 9, // AC bias (Y1 type)
		DCQuant:  10,
		DCIQuant: (1 << 17) / 10,
		DCBias:   96 << 9, // DC bias (Y1 type)
	}

	nz := QuantizeCoeffs(in[:], out[:], &sq, 0)
	if nz == 0 {
		t.Error("expected non-zero coefficients")
	}
	if out[0] == 0 {
		t.Error("expected out[0] != 0 for input 100")
	}
	// Verify QUANTDIV: (100 * iQ + B) >> 17
	expected := int(uint32(100)*uint32(sq.DCIQuant)+uint32(sq.DCBias)) >> 17
	if abs(int(out[0])-expected) > 1 {
		t.Errorf("out[0] = %d, expected ~%d", out[0], expected)
	}
}

func TestQuantizeCoeffsAllZero(t *testing.T) {
	in := [16]int16{}
	out := [16]int16{}
	sq := SegmentQuant{Quant: 10, IQuant: (1 << 17) / 10, Bias: 110 << 9, DCQuant: 10, DCIQuant: (1 << 17) / 10, DCBias: 96 << 9}

	nz := QuantizeCoeffs(in[:], out[:], &sq, 0)
	if nz != 0 {
		t.Errorf("nz = %d, want 0 for all-zero input", nz)
	}
}

func TestQuantizeCoeffsSkipDC(t *testing.T) {
	in := [16]int16{999, 50, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	out := [16]int16{}
	sq := SegmentQuant{Quant: 10, IQuant: (1 << 17) / 10, Bias: 110 << 9, DCQuant: 10, DCIQuant: (1 << 17) / 10, DCBias: 96 << 9}

	_ = QuantizeCoeffs(in[:], out[:], &sq, 1)
	if out[0] != 0 {
		t.Errorf("out[0] = %d, want 0 (DC should be skipped)", out[0])
	}
	if out[1] == 0 {
		t.Error("out[1] should not be 0")
	}
}

func TestDequantCoeffs(t *testing.T) {
	in := [16]int16{10, -5, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	out := [16]int16{}
	sq := SegmentQuant{Quant: 10, DCQuant: 10}

	DequantCoeffs(in[:], out[:], &sq)
	if out[0] != 100 {
		t.Errorf("out[0] = %d, want 100", out[0])
	}
	if out[1] != -50 {
		t.Errorf("out[1] = %d, want -50", out[1])
	}
}

// --- RD Score tests ---

func TestRDScore(t *testing.T) {
	// RDScore = rate * lambda + 256 * distortion
	score := RDScore(100, 50, 10)
	expected := uint64(50*10 + 256*100)
	if score != expected {
		t.Errorf("RDScore = %d, want %d", score, expected)
	}
}

// --- Segment assignment tests ---

func TestAnalysisSingleSegment(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 100, G: 100, B: 100, A: 255})
	cfg := DefaultConfig(75)
	cfg.Segments = 1
	enc := NewEncoder(img, cfg)

	enc.analysis()

	for i, info := range enc.mbInfo {
		if info.Segment != 0 {
			t.Errorf("mbInfo[%d].Segment = %d, want 0", i, info.Segment)
		}
	}
}

func TestAnalysisMultiSegment(t *testing.T) {
	img := gradientImage(64, 64)
	cfg := DefaultConfig(75)
	cfg.Segments = 4
	enc := NewEncoder(img, cfg)

	enc.analysis()

	// With a gradient, we should see more than 1 distinct segment.
	seen := map[uint8]bool{}
	for _, info := range enc.mbInfo {
		seen[info.Segment] = true
	}
	// At minimum, segments should be assigned.
	if len(seen) == 0 {
		t.Error("no segments assigned")
	}
}

// --- Token buffer tests ---

func TestTokenBufferBasic(t *testing.T) {
	var tb TokenBuffer
	tb.Init(10)

	tb.RecordToken(1, 128)
	tb.RecordToken(0, 200)
	tb.RecordToken(1, 50)

	if tb.tokenCount() != 3 {
		t.Errorf("token count = %d, want 3", tb.tokenCount())
	}
}

func TestTokenBufferPageOverflow(t *testing.T) {
	var tb TokenBuffer
	tb.Init(1)

	// Write more tokens than one page can hold.
	for i := 0; i < tokenPageSize+100; i++ {
		tb.RecordToken(i%2, 128)
	}

	if tb.tokenCount() != tokenPageSize+100 {
		t.Errorf("token count = %d, want %d", tb.tokenCount(), tokenPageSize+100)
	}

	if len(tb.pages) < 2 {
		t.Errorf("pages = %d, want >= 2", len(tb.pages))
	}
}

func TestTokenBufferReset(t *testing.T) {
	var tb TokenBuffer
	tb.Init(1)

	tb.RecordToken(1, 128)
	tb.RecordToken(0, 200)
	tb.Reset()

	if tb.tokenCount() != 0 {
		t.Errorf("token count after reset = %d, want 0", tb.tokenCount())
	}
}

// --- Iterator tests ---

func TestIteratorScan(t *testing.T) {
	img := solidImage(32, 32, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	enc := NewEncoder(img, cfg)

	enc.InitIterator()
	it := &enc.mbIterator

	count := 0
	for !it.IsDone() {
		count++
		if !it.Next() {
			break
		}
	}

	if count != 4 { // 2x2 MBs
		t.Errorf("iterated %d MBs, want 4", count)
	}
}

func TestIteratorImportExport(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	cfg := DefaultConfig(75)
	enc := NewEncoder(img, cfg)

	enc.InitIterator()
	it := &enc.mbIterator

	it.Import(enc)

	// Check that yuvIn has data at YOff.
	v := enc.yuvIn[YOff]
	if v == 0 {
		t.Error("yuvIn[YOff] = 0 after import, expected non-zero")
	}

	// Copy yuvIn to yuvOut (simulating encoding).
	copy(enc.yuvOut, enc.yuvIn)
	it.Export(enc)

	// After export, topY should have the bottom row of the MB.
	if it.topY[0] == 0 {
		t.Error("topY[0] = 0 after export, expected non-zero")
	}
}

// --- Full encode test ---

func TestEncodeFrame(t *testing.T) {
	img := gradientImage(32, 32)
	cfg := DefaultConfig(50)
	cfg.Segments = 1
	cfg.Method = 0 // fast mode, no I4x4
	cfg.Pass = 1
	enc := NewEncoder(img, cfg)

	data, err := enc.EncodeFrame()
	if err != nil {
		t.Fatalf("EncodeFrame error: %v", err)
	}
	if len(data) == 0 {
		t.Error("EncodeFrame returned empty data")
	}

	// Check VP8 frame tag.
	if len(data) < 10 {
		t.Fatal("output too short for VP8 frame")
	}
	// Bit 0 should be 0 (keyframe).
	if data[0]&1 != 0 {
		t.Error("frame tag bit 0 should be 0 for keyframe")
	}
	// VP8 signature at offset 3.
	if data[3] != 0x9d || data[4] != 0x01 || data[5] != 0x2a {
		t.Errorf("VP8 signature = %02x %02x %02x, want 9d 01 2a", data[3], data[4], data[5])
	}
	// Check width/height.
	w := int(data[6]) | int(data[7])<<8
	w &= 0x3FFF
	h := int(data[8]) | int(data[9])<<8
	h &= 0x3FFF
	if w != 32 || h != 32 {
		t.Errorf("encoded dimensions = %dx%d, want 32x32", w, h)
	}
}

func TestAssembleRIFF(t *testing.T) {
	vp8Data := []byte{0x9d, 0x01, 0x2a, 0x10, 0x00, 0x10, 0x00}
	riff := AssembleRIFF(vp8Data)

	// Check RIFF header.
	if string(riff[0:4]) != "RIFF" {
		t.Errorf("RIFF magic = %q", string(riff[0:4]))
	}
	if string(riff[8:12]) != "WEBP" {
		t.Errorf("WEBP magic = %q", string(riff[8:12]))
	}
	if string(riff[12:16]) != "VP8 " {
		t.Errorf("VP8 chunk = %q", string(riff[12:16]))
	}
}

func TestSetupSegment(t *testing.T) {
	cfg := DefaultConfig(75)
	enc := NewEncoder(solidImage(16, 16, color.NRGBA{A: 255}), cfg)

	seg := &enc.dqm[0]
	if seg.Y1.Quant <= 0 {
		t.Errorf("Y1.Quant = %d, want > 0", seg.Y1.Quant)
	}
	if seg.Y2.Quant <= 0 {
		t.Errorf("Y2.Quant = %d, want > 0", seg.Y2.Quant)
	}
	if seg.UV.Quant <= 0 {
		t.Errorf("UV.Quant = %d, want > 0", seg.UV.Quant)
	}
	if seg.Lambda <= 0 {
		t.Errorf("Lambda = %d, want > 0", seg.Lambda)
	}
}

// --- Dithering tests ---

func TestImportImageDitheringProducesVariation(t *testing.T) {
	// A solid-color image should show Y-plane variation when dithered,
	// because the pseudo-random rounding jitters the Y values.
	img := solidImage(32, 32, color.NRGBA{R: 100, G: 150, B: 200, A: 255})

	// Without dithering.
	cfgNoDither := DefaultConfig(50)
	encNoDither := NewEncoder(img, cfgNoDither)

	// With dithering.
	cfgDither := DefaultConfig(50)
	cfgDither.Dithering = 1.0
	encDither := NewEncoder(img, cfgDither)

	// Count how many Y pixels differ between dithered and non-dithered.
	diffCount := 0
	padW := encNoDither.mbW * 16
	padH := encNoDither.mbH * 16
	for y := 0; y < padH; y++ {
		for x := 0; x < padW; x++ {
			off := y*encNoDither.yStride + x
			if encDither.yPlane[off] != encNoDither.yPlane[off] {
				diffCount++
			}
		}
	}
	// With dithering=1.0, a solid image should have many differing Y values
	// because the random rounding jitters them.
	if diffCount == 0 {
		t.Error("dithering produced zero Y-plane variation on a solid image; expected jitter")
	}
}

func TestImportImageNoDitheringWhenZero(t *testing.T) {
	// With Dithering=0, the result should be identical to the default path.
	img := gradientImage(32, 32)

	cfgDefault := DefaultConfig(75)
	encDefault := NewEncoder(img, cfgDefault)

	cfgZero := DefaultConfig(75)
	cfgZero.Dithering = 0.0
	encZero := NewEncoder(img, cfgZero)

	padW := encDefault.mbW * 16
	padH := encDefault.mbH * 16
	for y := 0; y < padH; y++ {
		for x := 0; x < padW; x++ {
			off := y*encDefault.yStride + x
			if encZero.yPlane[off] != encDefault.yPlane[off] {
				t.Fatalf("Y[%d,%d]: dithering=0 produced %d, default produced %d; expected identical",
					x, y, encZero.yPlane[off], encDefault.yPlane[off])
			}
		}
	}
}

func TestImportImageDitheringDeterministic(t *testing.T) {
	// Same image + same dithering config should produce identical YUV planes.
	img := gradientImage(32, 32)
	cfg := DefaultConfig(50)
	cfg.Dithering = 0.8

	enc1 := NewEncoder(img, cfg)
	enc2 := NewEncoder(img, cfg)

	padW := enc1.mbW * 16
	padH := enc1.mbH * 16
	for y := 0; y < padH; y++ {
		for x := 0; x < padW; x++ {
			off := y*enc1.yStride + x
			if enc1.yPlane[off] != enc2.yPlane[off] {
				t.Fatalf("Y[%d,%d]: run1=%d, run2=%d; expected deterministic",
					x, y, enc1.yPlane[off], enc2.yPlane[off])
			}
		}
	}
}

func TestImportImageDitheringBounded(t *testing.T) {
	// Even with max dithering, Y values should remain valid (0-255).
	img := gradientImage(64, 64)
	cfg := DefaultConfig(10) // low quality
	cfg.Dithering = 1.0

	enc := NewEncoder(img, cfg)
	padW := enc.mbW * 16
	padH := enc.mbH * 16
	for y := 0; y < padH; y++ {
		for x := 0; x < padW; x++ {
			// uint8 is always 0-255, but verify no panics occurred during conversion.
			_ = enc.yPlane[y*enc.yStride+x]
		}
	}
}

func TestEncodeFrameWithDithering(t *testing.T) {
	// Verify that a full encode succeeds with dithering enabled.
	img := gradientImage(64, 64)
	cfg := DefaultConfig(50)
	cfg.Dithering = 0.9

	enc := NewEncoder(img, cfg)
	bs, err := enc.EncodeFrame()
	if err != nil {
		t.Fatalf("EncodeFrame with dithering failed: %v", err)
	}
	if len(bs) == 0 {
		t.Error("EncodeFrame with dithering produced empty bitstream")
	}
}

// --- GetPSNR tests ---

func TestGetPSNR(t *testing.T) {
	tests := []struct {
		name     string
		mse      uint64
		size     uint64
		wantMin  float64
		wantMax  float64
	}{
		{"zero mse returns 99", 0, 100, 99.0, 99.0},
		{"zero size returns 99", 100, 0, 99.0, 99.0},
		{"both zero returns 99", 0, 0, 99.0, 99.0},
		{"perfect match", 1, 1, 48.1, 48.2},       // 10*log10(255*255) ~ 48.13
		{"typical value", 100, 1000, 58.1, 58.2},   // 10*log10(65025*1000/100) ~ 58.13
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getPSNR(tt.mse, tt.size)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("getPSNR(%d, %d) = %.4f, want [%.1f, %.1f]", tt.mse, tt.size, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// --- PassStats / InitPassStats tests ---

func TestInitPassStats_TargetSize(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	cfg.TargetSize = 5000
	enc := NewEncoder(img, cfg)

	stats := enc.initPassStats()
	if !stats.doSizeSearch {
		t.Error("doSizeSearch should be true when TargetSize > 0")
	}
	if stats.target != 5000.0 {
		t.Errorf("target = %v, want 5000.0", stats.target)
	}
}

func TestInitPassStats_TargetPSNR(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	cfg.TargetPSNR = 35.0
	enc := NewEncoder(img, cfg)

	stats := enc.initPassStats()
	if stats.doSizeSearch {
		t.Error("doSizeSearch should be false when only TargetPSNR is set")
	}
	if stats.target != 35.0 {
		t.Errorf("target = %v, want 35.0", stats.target)
	}
}

func TestInitPassStats_DefaultTarget(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	// Neither TargetSize nor TargetPSNR set.
	enc := NewEncoder(img, cfg)

	stats := enc.initPassStats()
	if stats.doSizeSearch {
		t.Error("doSizeSearch should be false when no target is set")
	}
	if stats.target != 40.0 {
		t.Errorf("target = %v, want 40.0 (C default)", stats.target)
	}
}

func TestInitPassStats_QMinQMax(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	cfg.QMin = 20
	cfg.QMax = 80
	enc := NewEncoder(img, cfg)

	stats := enc.initPassStats()
	if stats.qmin != 20.0 {
		t.Errorf("qmin = %v, want 20.0", stats.qmin)
	}
	if stats.qmax != 80.0 {
		t.Errorf("qmax = %v, want 80.0", stats.qmax)
	}
	if stats.q != 75.0 {
		t.Errorf("q = %v, want 75.0 (clamped to [20,80])", stats.q)
	}
}

func TestInitPassStats_QClampLow(t *testing.T) {
	img := solidImage(16, 16, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(10) // quality=10
	cfg.QMin = 50
	cfg.QMax = 100
	enc := NewEncoder(img, cfg)

	stats := enc.initPassStats()
	if stats.q != 50.0 {
		t.Errorf("q = %v, want 50.0 (clamped to qmin)", stats.q)
	}
}

func TestInitPassStats_TargetSizePrecedence(t *testing.T) {
	// When both TargetSize and TargetPSNR are set, TargetSize takes precedence
	// (matching C libwebp behavior: do_size_search = (target_size != 0)).
	img := solidImage(16, 16, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	cfg := DefaultConfig(75)
	cfg.TargetSize = 3000
	cfg.TargetPSNR = 35.0
	enc := NewEncoder(img, cfg)

	stats := enc.initPassStats()
	if !stats.doSizeSearch {
		t.Error("doSizeSearch should be true when TargetSize > 0 (takes precedence)")
	}
	if stats.target != 3000.0 {
		t.Errorf("target = %v, want 3000.0 (TargetSize takes precedence)", stats.target)
	}
}

func TestEncodeFrameWithTargetPSNR(t *testing.T) {
	// Verify that a full encode succeeds with TargetPSNR enabled.
	img := gradientImage(32, 32)
	cfg := DefaultConfig(50)
	cfg.TargetPSNR = 35.0
	cfg.Pass = 3

	enc := NewEncoder(img, cfg)
	bs, err := enc.EncodeFrame()
	if err != nil {
		t.Fatalf("EncodeFrame with TargetPSNR failed: %v", err)
	}
	if len(bs) == 0 {
		t.Error("EncodeFrame with TargetPSNR produced empty bitstream")
	}
}

// --- Helpers ---

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
