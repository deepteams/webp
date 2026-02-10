package bitio

import "testing"

func TestNewLosslessReader_InitialState(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	br := NewLosslessReader(data)

	if br.eos {
		t.Error("unexpected eos after init")
	}
	if br.bitPos != 0 {
		t.Errorf("bitPos = %d, want 0", br.bitPos)
	}
	if br.pos != 8 {
		t.Errorf("pos = %d, want 8 (all bytes loaded)", br.pos)
	}
}

func TestLosslessReader_ReadBits_SingleByte(t *testing.T) {
	// 0xA5 = 1010_0101. In LE bit order the lowest bits come first.
	data := []byte{0xA5, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	br := NewLosslessReader(data)

	// Read 4 bits: lower nibble of 0xA5 = 0x5 = 0101.
	v := br.ReadBits(4)
	if v != 0x5 {
		t.Errorf("ReadBits(4) = 0x%x, want 0x5", v)
	}

	// Read next 4 bits: upper nibble of 0xA5 = 0xA = 1010.
	v = br.ReadBits(4)
	if v != 0xA {
		t.Errorf("ReadBits(4) = 0x%x, want 0xA", v)
	}
}

func TestLosslessReader_ReadBits_MultipleBytes(t *testing.T) {
	data := []byte{0xFF, 0x00, 0xAB, 0xCD, 0x00, 0x00, 0x00, 0x00}
	br := NewLosslessReader(data)

	// Read 8 bits: should be 0xFF.
	v := br.ReadBits(8)
	if v != 0xFF {
		t.Errorf("ReadBits(8) = 0x%x, want 0xFF", v)
	}

	// Read 8 bits: should be 0x00.
	v = br.ReadBits(8)
	if v != 0x00 {
		t.Errorf("ReadBits(8) = 0x%x, want 0x00", v)
	}

	// Read 16 bits: should be 0xCDAB (little-endian 16-bit from bytes AB CD).
	v = br.ReadBits(16)
	if v != 0xCDAB {
		t.Errorf("ReadBits(16) = 0x%x, want 0xCDAB", v)
	}
}

func TestLosslessReader_ReadBits_MaxBits(t *testing.T) {
	data := []byte{0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00}
	br := NewLosslessReader(data)

	// Read 24 bits (the max).
	v := br.ReadBits(24)
	if v != 0xFFFFFF {
		t.Errorf("ReadBits(24) = 0x%x, want 0xFFFFFF", v)
	}
}

func TestLosslessReader_ReadBits_ExceedsMax(t *testing.T) {
	data := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00}
	br := NewLosslessReader(data)

	// Reading more than 24 bits should set eos and return 0.
	v := br.ReadBits(25)
	if v != 0 {
		t.Errorf("ReadBits(25) = %d, want 0", v)
	}
	if !br.eos {
		t.Error("expected eos after reading > 24 bits")
	}
}

func TestLosslessReader_PrefetchBits_SetBitPos(t *testing.T) {
	data := []byte{0x3C, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	br := NewLosslessReader(data)

	// 0x3C = 0011_1100. PrefetchBits at bit 0 sees the whole 64-bit val.
	pf := br.PrefetchBits()
	low8 := pf & 0xFF
	if low8 != 0x3C {
		t.Errorf("PrefetchBits low byte = 0x%x, want 0x3C", low8)
	}

	// Skip 4 bits via SetBitPos and re-prefetch.
	br.SetBitPos(4)
	pf = br.PrefetchBits()
	low4 := pf & 0xF
	// Upper nibble of 0x3C is 0x3.
	if low4 != 0x3 {
		t.Errorf("PrefetchBits after skip(4) low nibble = 0x%x, want 0x3", low4)
	}
}

func TestLosslessReader_FillBitWindow_Boundary(t *testing.T) {
	// Create data long enough to require multiple fills.
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i)
	}
	br := NewLosslessReader(data)

	// Read 8 bytes worth of bits (64 bits) to force at least one fill.
	for i := 0; i < 8; i++ {
		br.FillBitWindow()
		v := br.ReadBits(8)
		if v != uint32(i) {
			t.Errorf("byte %d: got 0x%x, want 0x%x", i, v, i)
		}
	}
}

func TestLosslessReader_EOS_EmptyData(t *testing.T) {
	br := NewLosslessReader([]byte{})

	v := br.ReadBits(1)
	if v != 0 {
		t.Errorf("ReadBits(1) on empty = %d, want 0", v)
	}
}

func TestLosslessReader_EOS_ShortData(t *testing.T) {
	br := NewLosslessReader([]byte{0x42})

	// Read all 8 bits.
	v := br.ReadBits(8)
	if v != 0x42 {
		t.Errorf("ReadBits(8) = 0x%x, want 0x42", v)
	}

	// The single byte was fully consumed during init (pos == len_ == 1).
	// After reading 8 bits, bitPos becomes 8. shiftBytes cannot load more,
	// so bitPos stays >= 8. A subsequent read attempt that pushes bitPos
	// beyond 64 will trigger EOS. Reading enough bits to exhaust the
	// remaining prefetched zeros in val should eventually set eos.
	for i := 0; i < 10; i++ {
		_ = br.ReadBits(8)
	}
	if !br.IsEndOfStream() {
		t.Error("expected eos after exhausting single byte and reading past end")
	}
}

func TestLosslessReader_ReadBits_ZeroBits(t *testing.T) {
	data := []byte{0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	br := NewLosslessReader(data)

	v := br.ReadBits(0)
	if v != 0 {
		t.Errorf("ReadBits(0) = %d, want 0", v)
	}
	// Bit position should not advance.
	if br.bitPos != 0 {
		t.Errorf("bitPos after ReadBits(0) = %d, want 0", br.bitPos)
	}
}

func TestLosslessReader_kBitMask(t *testing.T) {
	for i := 0; i <= vp8lMaxNumBitRead; i++ {
		want := uint32((1 << uint(i)) - 1)
		if kBitMask[i] != want {
			t.Errorf("kBitMask[%d] = 0x%x, want 0x%x", i, kBitMask[i], want)
		}
	}
}
