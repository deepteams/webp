package lossless

// PixOrCopy represents a pixel-or-copy token used in VP8L backward
// reference encoding. Each token is one of:
//   - Literal: a single ARGB pixel value
//   - CacheIdx: an index into the color cache
//   - Copy: a length+distance back-reference
//
// Reference: libwebp/src/enc/backward_references_enc.h
type PixOrCopy struct {
	mode           uint8
	len            uint16
	argbOrDistance  uint32
}

// Token mode constants.
const (
	modeLiteral  uint8 = 0
	modeCacheIdx uint8 = 1
	modeCopy     uint8 = 2
)

// LiteralPixel creates a literal ARGB token.
func LiteralPixel(argb uint32) PixOrCopy {
	return PixOrCopy{
		mode:          modeLiteral,
		argbOrDistance: argb,
		len:           1,
	}
}

// CachePixel creates a color-cache index token.
func CachePixel(idx int) PixOrCopy {
	return PixOrCopy{
		mode:          modeCacheIdx,
		argbOrDistance: uint32(idx),
		len:           1,
	}
}

// CopyPixel creates a back-reference copy token.
func CopyPixel(length int, distance int) PixOrCopy {
	return PixOrCopy{
		mode:          modeCopy,
		argbOrDistance: uint32(distance),
		len:           uint16(length),
	}
}

// IsLiteral returns true if the token is a literal ARGB pixel.
func (p *PixOrCopy) IsLiteral() bool {
	return p.mode == modeLiteral
}

// IsCacheIdx returns true if the token is a color-cache index.
func (p *PixOrCopy) IsCacheIdx() bool {
	return p.mode == modeCacheIdx
}

// IsCopy returns true if the token is a back-reference copy.
func (p *PixOrCopy) IsCopy() bool {
	return p.mode == modeCopy
}

// Length returns the copy length (1 for literal and cache-index tokens).
func (p *PixOrCopy) Length() int {
	return int(p.len)
}

// Distance returns the copy distance. Only valid for copy tokens.
func (p *PixOrCopy) Distance() int {
	return int(p.argbOrDistance)
}

// Argb returns the ARGB value. Only valid for literal tokens.
func (p *PixOrCopy) Argb() uint32 {
	return p.argbOrDistance
}

// LiteralComponent returns a single 8-bit component from the literal ARGB
// value. Component 0 is the lowest byte (blue in BGRA order), component 3
// is the highest byte (alpha).
func (p *PixOrCopy) LiteralComponent(component int) uint32 {
	return (p.argbOrDistance >> (component * 8)) & 0xff
}

// CacheIndex returns the color-cache index. Only valid for cache-index tokens.
func (p *PixOrCopy) CacheIndex() int {
	return int(p.argbOrDistance)
}
