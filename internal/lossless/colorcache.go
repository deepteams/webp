package lossless

// ColorCache implements the VP8L color cache, a hash-addressed table of
// recently seen ARGB pixel values. It uses the multiplicative hash
// 0x1e35a7bd to map 32-bit ARGB values to cache slots.
//
// Reference: libwebp/src/utils/color_cache_utils.h + .c
type ColorCache struct {
	Colors    []uint32
	HashShift int
	HashBits  int
}

// kHashMul is the multiplicative hash constant used by the VP8L color cache.
const kHashMul = 0x1e35a7bd

// NewColorCache allocates a ColorCache with 2^hashBits entries.
// hashBits must be in [1, MaxCacheBits].
func NewColorCache(hashBits int) *ColorCache {
	size := 1 << hashBits
	return &ColorCache{
		Colors:    make([]uint32, size),
		HashShift: 32 - hashBits,
		HashBits:  hashBits,
	}
}

// HashPix computes the hash-table index for an ARGB value.
func (c *ColorCache) HashPix(argb uint32) int {
	return int((argb * kHashMul) >> uint(c.HashShift))
}

// Insert stores an ARGB value at its hashed position.
func (c *ColorCache) Insert(argb uint32) {
	key := c.HashPix(argb)
	c.Colors[key] = argb
}

// Lookup returns the cached color at the given key (hash index).
func (c *ColorCache) Lookup(key int) uint32 {
	return c.Colors[key]
}

// Set stores an ARGB value at a specific key (hash index).
func (c *ColorCache) Set(key int, argb uint32) {
	c.Colors[key] = argb
}

// Contains checks whether argb is cached at its hash position.
// Returns (key, true) if present, (-1, false) otherwise.
func (c *ColorCache) Contains(argb uint32) (int, bool) {
	key := c.HashPix(argb)
	if c.Colors[key] == argb {
		return key, true
	}
	return -1, false
}

// Reset clears all cache entries to zero.
func (c *ColorCache) Reset() {
	for i := range c.Colors {
		c.Colors[i] = 0
	}
}

// Copy copies all color entries from src into c. Both must have the
// same HashBits.
func (c *ColorCache) Copy(src *ColorCache) {
	copy(c.Colors, src.Colors)
}

// ReuseColorCache returns a reset ColorCache with the given hashBits.
// If existing is non-nil and already has the right capacity, it is reused.
// Otherwise a new ColorCache is allocated.
func ReuseColorCache(existing *ColorCache, hashBits int) *ColorCache {
	size := 1 << hashBits
	if existing != nil && cap(existing.Colors) >= size {
		existing.Colors = existing.Colors[:size]
		existing.HashShift = 32 - hashBits
		existing.HashBits = hashBits
		for i := range existing.Colors {
			existing.Colors[i] = 0
		}
		return existing
	}
	return NewColorCache(hashBits)
}
