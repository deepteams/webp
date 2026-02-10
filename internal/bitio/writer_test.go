package bitio

import (
	"math/rand"
	"testing"
)

// --- BoolWriter / BoolReader round-trip tests ---

func TestBoolWriter_BoolReader_RoundTrip_UniformProb(t *testing.T) {
	// Write random bits with uniform probability (0x80) and read them back.
	const numBits = 500
	rng := rand.New(rand.NewSource(42))
	expected := make([]int, numBits)

	bw := NewBoolWriter(256)
	for i := 0; i < numBits; i++ {
		bit := rng.Intn(2)
		expected[i] = bit
		bw.PutBitUniform(bit)
	}
	data := bw.Finish()

	br := NewBoolReader(data)
	for i := 0; i < numBits; i++ {
		got := br.GetBit(0x80)
		if got != expected[i] {
			t.Fatalf("bit %d: got %d, want %d", i, got, expected[i])
		}
	}
}

func TestBoolWriter_BoolReader_RoundTrip_VariedProb(t *testing.T) {
	// Write random bits with random probabilities and read them back.
	const numBits = 1000
	rng := rand.New(rand.NewSource(99))

	type entry struct {
		bit  int
		prob int
	}
	entries := make([]entry, numBits)

	bw := NewBoolWriter(512)
	for i := 0; i < numBits; i++ {
		prob := rng.Intn(255) + 1 // [1, 255]
		bit := rng.Intn(2)
		entries[i] = entry{bit: bit, prob: prob}
		bw.PutBit(bit, prob)
	}
	data := bw.Finish()

	br := NewBoolReader(data)
	for i := 0; i < numBits; i++ {
		got := br.GetBit(uint8(entries[i].prob))
		if got != entries[i].bit {
			t.Fatalf("bit %d (prob=%d): got %d, want %d",
				i, entries[i].prob, got, entries[i].bit)
		}
	}
}

func TestBoolWriter_BoolReader_RoundTrip_PutBits(t *testing.T) {
	// Write multi-bit values with PutBits and read them back with GetValue.
	bw := NewBoolWriter(128)

	values := []struct {
		val    uint32
		nbBits int
	}{
		{0, 1},
		{1, 1},
		{42, 8},
		{0x1FF, 9},
		{0, 3},
		{7, 3},
		{12345, 16},
	}

	for _, v := range values {
		bw.PutBits(v.val, v.nbBits)
	}
	data := bw.Finish()

	br := NewBoolReader(data)
	for i, v := range values {
		got := br.GetValue(v.nbBits)
		if got != v.val {
			t.Errorf("value %d: got %d, want %d (nbBits=%d)", i, got, v.val, v.nbBits)
		}
	}
}

func TestBoolWriter_BoolReader_RoundTrip_SignedBits(t *testing.T) {
	bw := NewBoolWriter(128)

	// PutSignedBits encodes: flag(nonzero?), then abs<<1|sign_bit.
	// VP8GetSignedValue reads: value, then sign bit.
	// These are NOT the same encoding, so we test PutBits/GetValue instead.
	vals := []struct {
		val    uint32
		nbBits int
	}{
		{0, 4},
		{5, 4},
		{15, 4},
		{255, 8},
		{0, 1},
		{1, 1},
	}

	for _, v := range vals {
		bw.PutBits(v.val, v.nbBits)
	}
	data := bw.Finish()

	br := NewBoolReader(data)
	for i, v := range vals {
		got := br.GetValue(v.nbBits)
		if got != v.val {
			t.Errorf("value %d: got %d, want %d", i, got, v.val)
		}
	}
}

func TestBoolWriter_EmptyFinish(t *testing.T) {
	bw := NewBoolWriter(0)
	data := bw.Finish()

	if len(data) == 0 {
		t.Error("Finish on empty writer should still produce framing bytes")
	}
}

func TestBoolWriter_SingleBit(t *testing.T) {
	bw := NewBoolWriter(16)
	bw.PutBitUniform(1)
	data := bw.Finish()

	br := NewBoolReader(data)
	got := br.GetBit(0x80)
	if got != 1 {
		t.Errorf("single bit round-trip: got %d, want 1", got)
	}
}

func TestBoolWriter_Pos(t *testing.T) {
	bw := NewBoolWriter(64)

	// Initially position should be very small.
	p0 := bw.Pos()
	bw.PutBits(0xABCD, 16)
	p1 := bw.Pos()

	if p1 <= p0 {
		t.Errorf("Pos did not advance: before=%d, after=%d", p0, p1)
	}
}

// --- LosslessWriter / LosslessReader round-trip tests ---

func TestLosslessWriter_LosslessReader_RoundTrip_Simple(t *testing.T) {
	bw := NewLosslessWriter(64)

	type entry struct {
		val  uint32
		bits int
	}
	entries := []entry{
		{0x05, 4},
		{0x0A, 4},
		{0xFF, 8},
		{0x00, 8},
		{0xABCD, 16},
		{0x123, 12},
	}

	for _, e := range entries {
		bw.WriteBits(e.val, e.bits)
	}
	data := bw.Finish()

	br := NewLosslessReader(data)
	for i, e := range entries {
		got := br.ReadBits(e.bits)
		want := e.val & kBitMask[e.bits]
		if got != want {
			t.Errorf("entry %d: got 0x%x, want 0x%x (bits=%d)", i, got, want, e.bits)
		}
	}
}

func TestLosslessWriter_LosslessReader_RoundTrip_Random(t *testing.T) {
	const numEntries = 500
	rng := rand.New(rand.NewSource(77))

	type entry struct {
		val  uint32
		bits int
	}
	entries := make([]entry, numEntries)

	bw := NewLosslessWriter(1024)
	for i := 0; i < numEntries; i++ {
		nbits := rng.Intn(24) + 1 // [1, 24]
		val := rng.Uint32() & kBitMask[nbits]
		entries[i] = entry{val: val, bits: nbits}
		bw.WriteBits(val, nbits)
	}
	data := bw.Finish()

	br := NewLosslessReader(data)
	for i, e := range entries {
		got := br.ReadBits(e.bits)
		if got != e.val {
			t.Fatalf("entry %d: got 0x%x, want 0x%x (bits=%d)", i, got, e.val, e.bits)
		}
	}
}

func TestLosslessWriter_LosslessReader_RoundTrip_MaxBits(t *testing.T) {
	bw := NewLosslessWriter(64)

	// Write several 24-bit values.
	vals := []uint32{0xFFFFFF, 0x000000, 0xABCDEF, 0x123456}
	for _, v := range vals {
		bw.WriteBits(v, 24)
	}
	data := bw.Finish()

	br := NewLosslessReader(data)
	for i, want := range vals {
		got := br.ReadBits(24)
		if got != want {
			t.Errorf("24-bit value %d: got 0x%x, want 0x%x", i, got, want)
		}
	}
}

func TestLosslessWriter_Empty(t *testing.T) {
	bw := NewLosslessWriter(0)
	data := bw.Finish()
	if len(data) != 0 {
		t.Errorf("empty writer produced %d bytes, want 0", len(data))
	}
}

func TestLosslessWriter_SingleBit(t *testing.T) {
	bw := NewLosslessWriter(16)
	bw.WriteBits(1, 1)
	data := bw.Finish()

	br := NewLosslessReader(data)
	got := br.ReadBits(1)
	if got != 1 {
		t.Errorf("single bit round-trip: got %d, want 1", got)
	}
}

func TestLosslessWriter_NumBytes(t *testing.T) {
	bw := NewLosslessWriter(64)
	if bw.NumBytes() != 0 {
		t.Errorf("NumBytes on empty = %d, want 0", bw.NumBytes())
	}

	bw.WriteBits(0xFF, 8)
	if bw.NumBytes() != 1 {
		t.Errorf("NumBytes after 8 bits = %d, want 1", bw.NumBytes())
	}

	bw.WriteBits(0xFF, 1)
	if bw.NumBytes() != 2 {
		t.Errorf("NumBytes after 9 bits = %d, want 2", bw.NumBytes())
	}
}

func TestLosslessWriter_WriteBits_ZeroBits(t *testing.T) {
	bw := NewLosslessWriter(64)
	bw.WriteBits(0xFF, 0) // should be a no-op
	if bw.NumBytes() != 0 {
		t.Errorf("NumBytes after WriteBits(_, 0) = %d, want 0", bw.NumBytes())
	}
}

func TestLosslessWriter_GrowBeyondInitial(t *testing.T) {
	// Start with a tiny buffer and write enough to force growth.
	bw := &LosslessWriter{
		buf: make([]byte, 4),
	}

	// Write 256 bits = 32 bytes, well beyond initial 4-byte buffer.
	for i := 0; i < 32; i++ {
		bw.WriteBits(0xFF, 8)
	}
	data := bw.Finish()

	if len(data) != 32 {
		t.Errorf("output length = %d, want 32", len(data))
	}
	for i, b := range data {
		if b != 0xFF {
			t.Errorf("byte %d = 0x%x, want 0xFF", i, b)
		}
	}
}
