package lossless

import (
	"image"
	"testing"
)

func TestDecodeHeader_Valid(t *testing.T) {
	// Construct a minimal VP8L header:
	// byte 0: 0x2f (magic)
	// bytes 1-4: width-1 (14 bits) | height-1 (14 bits) | alpha (1 bit) | version (3 bits)
	// width=1, height=1 => (0, 0 in 14 bits), alpha=0, version=0
	data := []byte{0x2f, 0x00, 0x00, 0x00, 0x00}
	dec := &Decoder{}
	err := dec.decodeHeader(data)
	if err != nil {
		t.Fatalf("decodeHeader: %v", err)
	}
	if dec.Width != 1 || dec.Height != 1 {
		t.Errorf("got %dx%d, want 1x1", dec.Width, dec.Height)
	}
	if dec.HasAlpha {
		t.Error("HasAlpha should be false")
	}
}

func TestDecodeHeader_LargerSize(t *testing.T) {
	// width=100, height=50, alpha=1, version=0
	// bits 0..13: width-1 = 99
	// bits 14..27: height-1 = 49
	// bit 28: alpha = 1
	// bits 29..31: version = 0
	// val32 = 99 | (49 << 14) | (1 << 28) = 0x100C4063
	// LE bytes: 0x63, 0x40, 0x0C, 0x10
	data := []byte{0x2f, 0x63, 0x40, 0x0C, 0x10}
	dec := &Decoder{}
	err := dec.decodeHeader(data)
	if err != nil {
		t.Fatalf("decodeHeader: %v", err)
	}
	if dec.Width != 100 {
		t.Errorf("Width = %d, want 100", dec.Width)
	}
	if dec.Height != 50 {
		t.Errorf("Height = %d, want 50", dec.Height)
	}
	if !dec.HasAlpha {
		t.Error("HasAlpha should be true")
	}
}

func TestDecodeHeader_BadSignature(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x00}
	dec := &Decoder{}
	err := dec.decodeHeader(data)
	if err != ErrBadSignature {
		t.Errorf("expected ErrBadSignature, got %v", err)
	}
}

func TestDecodeHeader_TooShort(t *testing.T) {
	data := []byte{0x2f, 0x00}
	dec := &Decoder{}
	err := dec.decodeHeader(data)
	if err != ErrBadSignature {
		t.Errorf("expected ErrBadSignature, got %v", err)
	}
}

func TestArgbToNRGBA(t *testing.T) {
	pixels := []uint32{
		0xffff0000, // opaque red
		0xff00ff00, // opaque green
		0xff0000ff, // opaque blue
		0x80402010, // semi-transparent
	}
	img := argbToNRGBA(pixels, 2, 2)

	tests := []struct {
		x, y    int
		r, g, b, a uint8
	}{
		{0, 0, 0xff, 0x00, 0x00, 0xff}, // red
		{1, 0, 0x00, 0xff, 0x00, 0xff}, // green
		{0, 1, 0x00, 0x00, 0xff, 0xff}, // blue
		{1, 1, 0x40, 0x20, 0x10, 0x80}, // semi-transparent
	}
	for _, tc := range tests {
		c := img.NRGBAAt(tc.x, tc.y)
		if c.R != tc.r || c.G != tc.g || c.B != tc.b || c.A != tc.a {
			t.Errorf("pixel(%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				tc.x, tc.y, c.R, c.G, c.B, c.A, tc.r, tc.g, tc.b, tc.a)
		}
	}
}

func TestNRGBAToARGB_Roundtrip(t *testing.T) {
	pixels := []uint32{0xff112233, 0x80aabbcc}
	img := argbToNRGBA(pixels, 2, 1)
	got := NRGBAToARGB(img)
	for i, want := range pixels {
		if got[i] != want {
			t.Errorf("pixel %d: got 0x%08x, want 0x%08x", i, got[i], want)
		}
	}
}

func TestAddGreenToBlueAndRed(t *testing.T) {
	// Subtract-green inverse: green=0x20 should be added to red and blue.
	// Input ARGB: alpha=0xff, red=0x10, green=0x20, blue=0x30
	// After: red=0x10+0x20=0x30, blue=0x30+0x20=0x50, green unchanged, alpha unchanged.
	src := []uint32{0xff102030}
	dst := make([]uint32, 1)
	addGreenToBlueAndRed(src, 1, dst)

	expected := uint32(0xff302050)
	if dst[0] != expected {
		t.Errorf("addGreenToBlueAndRed: got 0x%08x, want 0x%08x", dst[0], expected)
	}
}

func TestAddGreenToBlueAndRed_Overflow(t *testing.T) {
	// green=0x80, red=0xC0, blue=0xD0
	// red = (0xC0 + 0x80) & 0xff = 0x40
	// blue = (0xD0 + 0x80) & 0xff = 0x50
	src := []uint32{0xffC080D0}
	dst := make([]uint32, 1)
	addGreenToBlueAndRed(src, 1, dst)

	expected := uint32(0xff408050)
	if dst[0] != expected {
		t.Errorf("addGreenToBlueAndRed overflow: got 0x%08x, want 0x%08x", dst[0], expected)
	}
}

func TestClampedAddSubtractFull(t *testing.T) {
	// L=200, T=180, TL=100 per component => result = 200+180-100 = 280 -> clamped to 255
	a := uint32(0xc8c8c8c8)
	b := uint32(0xb4b4b4b4)
	c := uint32(0x64646464)
	result := clampedAddSubtractFull(a, b, c)
	// Each component: 200+180-100=280 -> 255
	expected := uint32(0xffffffff)
	if result != expected {
		t.Errorf("clampedAddSubtractFull: got 0x%08x, want 0x%08x", result, expected)
	}
}

func TestClampedAddSubtractFull_Underflow(t *testing.T) {
	// L=10, T=10, TL=200 => 10+10-200 = -180 -> clamped to 0
	a := uint32(0x0a0a0a0a)
	b := uint32(0x0a0a0a0a)
	c := uint32(0xc8c8c8c8)
	result := clampedAddSubtractFull(a, b, c)
	expected := uint32(0x00000000)
	if result != expected {
		t.Errorf("clampedAddSubtractFull underflow: got 0x%08x, want 0x%08x", result, expected)
	}
}

func TestSelectPredictor(t *testing.T) {
	// If top is closer to topLeft than left is, select top.
	top := uint32(0xff808080)
	left := uint32(0xff000000)
	topLeft := uint32(0xff808080)
	// |top - topLeft| = 0 per component, |left - topLeft| = 128 per component
	// pa = sum(|top-tl|) - sum(|left-tl|) = 0 - 512 < 0 => select top
	result := selectPredictor(left, top, topLeft)
	if result != top {
		t.Errorf("selectPredictor: got 0x%08x, want 0x%08x (top)", result, top)
	}
}

func TestAverage2(t *testing.T) {
	a := uint32(0xff000000) // alpha=255
	b := uint32(0x01000000) // alpha=1
	result := average2(a, b)
	expected := uint32(0x80000000) // (255+1)/2 = 128
	if result != expected {
		t.Errorf("average2: got 0x%08x, want 0x%08x", result, expected)
	}
}

func TestAddPixels(t *testing.T) {
	a := uint32(0x10203040)
	b := uint32(0x01020304)
	result := addPixels(a, b)
	// Per component: (0x10+0x01, 0x20+0x02, 0x30+0x03, 0x40+0x04) & 0xff
	expected := uint32(0x11223344)
	if result != expected {
		t.Errorf("addPixels: got 0x%08x, want 0x%08x", result, expected)
	}
}

func TestColorIndexInverseTransform(t *testing.T) {
	// 4-color palette (bits=2 => 4 pixels per byte, bitsPerPixel=2)
	palette := []uint32{0xff000000, 0xff0000ff, 0xff00ff00, 0xffff0000}
	transform := Transform{
		Type:  ColorIndexingTransform,
		Bits:  2,
		XSize: 4,
		YSize: 1,
		Data:  palette,
	}

	// Pack 4 pixels into one: indices 0,1,2,3
	// Each index is 2 bits: 0b11_10_01_00 = 0xe4
	// Stored in green channel of src: (0xe4 << 8)
	src := []uint32{0x0000e400}
	dst := make([]uint32, 4)

	colorIndexInverseTransform(&transform, 0, 1, src, dst)

	expected := []uint32{0xff000000, 0xff0000ff, 0xff00ff00, 0xffff0000}
	for i := range expected {
		if dst[i] != expected[i] {
			t.Errorf("colorIndexInverse[%d]: got 0x%08x, want 0x%08x", i, dst[i], expected[i])
		}
	}
}

func TestTransformColorInverse(t *testing.T) {
	// Test cross-color inverse with known multipliers.
	m := colorMultipliers{
		greenToRed:  0, // no cross-color
		greenToBlue: 0,
		redToBlue:   0,
	}
	argb := uint32(0xff804020)
	result := transformColorInverse(m, argb)
	if result != argb {
		t.Errorf("transformColorInverse (zero multipliers): got 0x%08x, want 0x%08x", result, argb)
	}
}

func TestExpandColorMap(t *testing.T) {
	// 2-color palette, bits=3 => finalNumColors = 1<<(8>>3) = 2
	palette := []uint32{0xff010203, 0x00040506}
	result := expandColorMap(2, 3, palette)

	if len(result) != 2 {
		t.Fatalf("expandColorMap: len = %d, want 2", len(result))
	}
	// First entry is unchanged.
	if result[0] != 0xff010203 {
		t.Errorf("result[0] = 0x%08x, want 0xff010203", result[0])
	}
	// Second entry is delta-decoded: each byte = palette[1] byte + result[0] byte
	// Blue: (0x06 + 0x03) & 0xff = 0x09
	// Green: (0x05 + 0x02) & 0xff = 0x07
	// Red: (0x04 + 0x01) & 0xff = 0x05
	// Alpha: (0x00 + 0xff) & 0xff = 0xff
	expected1 := uint32(0xff050709)
	if result[1] != expected1 {
		t.Errorf("result[1] = 0x%08x, want 0x%08x", result[1], expected1)
	}
}

func TestCopyBlock32(t *testing.T) {
	data := make([]uint32, 10)
	data[0] = 0xAAAAAAAA
	data[1] = 0xBBBBBBBB
	data[2] = 0xCCCCCCCC

	// Copy 3 values from pos=0 to pos=3 (dist=3)
	copyBlock32(data, 3, 3, 3)
	if data[3] != 0xAAAAAAAA || data[4] != 0xBBBBBBBB || data[5] != 0xCCCCCCCC {
		t.Errorf("copyBlock32: got [0x%08x, 0x%08x, 0x%08x]",
			data[3], data[4], data[5])
	}
}

func TestCopyBlock32_Overlap(t *testing.T) {
	data := make([]uint32, 6)
	data[0] = 0x11111111

	// Copy with dist=1 and length=5: should repeat data[0] five times.
	copyBlock32(data, 1, 1, 5)
	for i := 1; i <= 5; i++ {
		if data[i] != 0x11111111 {
			t.Errorf("copyBlock32 overlap: data[%d] = 0x%08x, want 0x11111111", i, data[i])
		}
	}
}

func TestGetCopyDistance(t *testing.T) {
	// Use a mock bit reader.
	br := &mockBR{bits: 0}

	// distanceSymbol < 4 => distance = symbol + 1
	if d := getCopyDistance(0, br); d != 1 {
		t.Errorf("getCopyDistance(0) = %d, want 1", d)
	}
	if d := getCopyDistance(3, br); d != 4 {
		t.Errorf("getCopyDistance(3) = %d, want 4", d)
	}
}

func TestPlaneCodeToDistance(t *testing.T) {
	// planeCode > 120 => simple subtraction.
	d := PlaneCodeToDistance(100, 121)
	if d != 1 {
		t.Errorf("PlaneCodeToDistance(100, 121) = %d, want 1", d)
	}

	// planeCode = 1 => kCodeToPlane[0] = 0x18 => yoffset=1, xoffset=0 => dist = 1*100+0 = 100
	d = PlaneCodeToDistance(100, 1)
	if d != 100 {
		t.Errorf("PlaneCodeToDistance(100, 1) = %d, want 100", d)
	}
}

// TestARGBToNRGBAImage verifies the conversion produces a valid image.
func TestARGBToNRGBAImage(t *testing.T) {
	pixels := []uint32{0xffff0000, 0xff00ff00}
	img := ARGBToNRGBA(pixels, 2, 1)
	if img.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Errorf("bounds = %v, want (0,0)-(2,1)", img.Bounds())
	}
}

// mockBR is a minimal bit reader for testing getCopyDistance/getCopyLength.
type mockBR struct {
	bits uint32
}

func (m *mockBR) ReadBits(n int) uint32 {
	return m.bits & ((1 << n) - 1)
}
func (m *mockBR) FillBitWindow()       {}
func (m *mockBR) PrefetchBits() uint32 { return m.bits }
func (m *mockBR) BitPos() int          { return 0 }
func (m *mockBR) SetBitPos(int)        {}
func (m *mockBR) IsEndOfStream() bool  { return false }
