// Package animation provides types and canvas logic for animated WebP images.
//
// It defines Frame/Animation structs and canvas reconstruction (blending, disposal)
// per the WebP extended format specification. Actual VP8/VP8L decoding/encoding
// is handled by codec packages; this package deals with container-level animation
// semantics only.
package animation

import (
	"image"
	"image/color"
	"time"
)

// DisposeMethod controls how the frame region is treated after rendering.
type DisposeMethod int

const (
	// DisposeNone leaves the canvas as-is after this frame is rendered.
	DisposeNone DisposeMethod = 0
	// DisposeBackground fills the frame region with the background color
	// after this frame is rendered (before rendering the next frame).
	DisposeBackground DisposeMethod = 1
)

// BlendMethod controls how a frame is composited onto the canvas.
type BlendMethod int

const (
	// BlendAlpha alpha-blends the frame onto the existing canvas.
	BlendAlpha BlendMethod = 0
	// BlendNone overwrites the frame region on the canvas without blending.
	BlendNone BlendMethod = 1
)

// Frame holds a decoded animation frame and its rendering parameters.
type Frame struct {
	// Image is the decoded image for this frame.
	// May be nil if the frame has not been decoded yet.
	Image image.Image

	// Duration is the display duration for this frame.
	Duration time.Duration

	// OffsetX is the horizontal offset of this frame on the canvas.
	OffsetX int

	// OffsetY is the vertical offset of this frame on the canvas.
	OffsetY int

	// Dispose specifies canvas cleanup after this frame is displayed.
	Dispose DisposeMethod

	// Blend specifies how this frame is composited onto the canvas.
	Blend BlendMethod

	// IsKeyframe indicates whether this frame can be independently decoded.
	IsKeyframe bool

	// HasAlpha indicates whether the frame's bitstream signals an alpha channel.
	// This is derived from the VP8L header alpha bit or the presence of an ALPH
	// chunk, matching the C libwebp's per-frame has_alpha flag. It is used for
	// keyframe detection without scanning pixel data.
	HasAlpha bool

	// BitstreamData holds the raw VP8/VP8L bitstream for lazy decoding.
	// May be nil after decoding.
	BitstreamData []byte

	// AlphaData holds the raw ALPH chunk data, if separate from the bitstream.
	AlphaData []byte
}

// Bounds returns the frame's rectangle on the canvas.
func (f *Frame) Bounds() image.Rectangle {
	var w, h int
	if f.Image != nil {
		b := f.Image.Bounds()
		w = b.Dx()
		h = b.Dy()
	}
	return image.Rect(f.OffsetX, f.OffsetY, f.OffsetX+w, f.OffsetY+h)
}

// HasImage reports whether the frame image has been decoded.
func (f *Frame) HasImage() bool {
	return f.Image != nil
}

// toNRGBA converts any image.Image to *image.NRGBA.
func toNRGBA(src image.Image) *image.NRGBA {
	if nrgba, ok := src.(*image.NRGBA); ok {
		return nrgba
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x-b.Min.X, y-b.Min.Y, src.At(x, y))
		}
	}
	return dst
}

// colorToNRGBA converts any color.Color to an NRGBA value.
func colorToNRGBA(c color.Color) color.NRGBA {
	if nrgba, ok := c.(color.NRGBA); ok {
		return nrgba
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{
		R: uint8(r * 0xff / a),
		G: uint8(g * 0xff / a),
		B: uint8(b * 0xff / a),
		A: uint8(a >> 8),
	}
}
