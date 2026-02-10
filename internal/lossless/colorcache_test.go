package lossless

import "testing"

func TestNewColorCache(t *testing.T) {
	for bits := 1; bits <= MaxCacheBits; bits++ {
		cc := NewColorCache(bits)
		expectedSize := 1 << bits
		if len(cc.Colors) != expectedSize {
			t.Errorf("NewColorCache(%d): len(Colors) = %d, want %d", bits, len(cc.Colors), expectedSize)
		}
		if cc.HashShift != 32-bits {
			t.Errorf("NewColorCache(%d): HashShift = %d, want %d", bits, cc.HashShift, 32-bits)
		}
		if cc.HashBits != bits {
			t.Errorf("NewColorCache(%d): HashBits = %d, want %d", bits, cc.HashBits, bits)
		}
	}
}

func TestColorCacheInsertLookup(t *testing.T) {
	cc := NewColorCache(8) // 256 entries

	colors := []uint32{0xff000000, 0xff0000ff, 0xffff0000, 0xff00ff00, 0xffffffff}
	for _, c := range colors {
		cc.Insert(c)
	}

	for _, c := range colors {
		key := cc.HashPix(c)
		got := cc.Lookup(key)
		if got != c {
			t.Errorf("Lookup(HashPix(0x%08x)) = 0x%08x, want 0x%08x", c, got, c)
		}
	}
}

func TestColorCacheContains(t *testing.T) {
	cc := NewColorCache(6)

	argb := uint32(0xdeadbeef)
	if _, ok := cc.Contains(argb); ok {
		t.Error("Contains should return false before Insert")
	}

	cc.Insert(argb)
	key, ok := cc.Contains(argb)
	if !ok {
		t.Error("Contains should return true after Insert")
	}
	if key < 0 || key >= (1<<cc.HashBits) {
		t.Errorf("Contains returned invalid key %d", key)
	}
	if cc.Lookup(key) != argb {
		t.Errorf("Lookup(%d) = 0x%08x, want 0x%08x", key, cc.Lookup(key), argb)
	}
}

func TestColorCacheHashCollision(t *testing.T) {
	cc := NewColorCache(1) // only 2 slots, collisions guaranteed
	cc.Insert(0x00000001)
	cc.Insert(0x00000002)
	// At least one should be findable (the last inserted that maps to same slot).
	// We just verify no panic.
}

func TestColorCacheReset(t *testing.T) {
	cc := NewColorCache(4)
	cc.Insert(0x12345678)
	cc.Reset()
	for i, c := range cc.Colors {
		if c != 0 {
			t.Errorf("Colors[%d] = 0x%08x after Reset, want 0", i, c)
		}
	}
}

func TestColorCacheCopy(t *testing.T) {
	src := NewColorCache(4)
	dst := NewColorCache(4)

	src.Insert(0xaabbccdd)
	src.Insert(0x11223344)
	dst.Copy(src)

	for i := range src.Colors {
		if dst.Colors[i] != src.Colors[i] {
			t.Errorf("Copy: Colors[%d] = 0x%08x, want 0x%08x", i, dst.Colors[i], src.Colors[i])
		}
	}
}

func TestHashPixDeterministic(t *testing.T) {
	cc := NewColorCache(8)
	argb := uint32(0xffeeddcc)
	h1 := cc.HashPix(argb)
	h2 := cc.HashPix(argb)
	if h1 != h2 {
		t.Errorf("HashPix not deterministic: %d != %d", h1, h2)
	}
	if h1 < 0 || h1 >= (1<<cc.HashBits) {
		t.Errorf("HashPix out of range: %d", h1)
	}
}
