// Package webp implements a decoder for the WebP image format.
//
// WebP supports lossy (VP8), lossless (VP8L), and extended (VP8X) formats.
// This package registers itself with the standard library's image package
// so that image.Decode can transparently read WebP files.
package webp

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/deepteams/webp/animation"
	"github.com/deepteams/webp/internal/container"
	"github.com/deepteams/webp/internal/dsp"
	"github.com/deepteams/webp/internal/lossless"
	"github.com/deepteams/webp/internal/lossy"
)

func init() {
	image.RegisterFormat("webp", "RIFF????WEBP", Decode, DecodeConfig)

	// Wire the animation package's frame decoder to our VP8/VP8L decoders.
	animation.FrameDecoderFunc = decodeFrameForAnimation

	// Wire the animation package's frame encoder to our VP8/VP8L encoders.
	animation.FrameEncoderFunc = encodeFrameForAnimation

	// Wire the animation package's simple encoder for single-frame optimization.
	animation.SimpleEncodeFunc = simpleEncodeForAnimation
}

// Errors returned by the decoder.
var (
	ErrUnsupported = errors.New("webp: unsupported format")
	ErrNoFrames    = errors.New("webp: no image frames found")
)

// Features describes a WebP file's properties, as returned by [GetFeatures].
type Features struct {
	Width        int    // Image width in pixels.
	Height       int    // Image height in pixels.
	HasAlpha     bool   // True if the image contains an alpha channel.
	HasAnimation bool   // True if the image is animated (ANIM chunk present).
	Format       string // Container format: "lossy" (VP8), "lossless" (VP8L), or "extended" (VP8X).
	LoopCount    int    // Animation loop count (0 = infinite). Only meaningful when HasAnimation is true.
	FrameCount   int    // Number of frames (1 for still images).
}

// MaxInputSize is the maximum allowed input size for WebP decoding (256 MB).
// Inputs larger than this are rejected to prevent denial-of-service via
// excessive memory allocation.
const MaxInputSize = 256 * 1024 * 1024

// readAll reads all data from r. If r implements Len() int (e.g.
// *bytes.Reader), a single exact-sized allocation is used instead of
// the repeated doublings that io.ReadAll performs.
// Inputs exceeding MaxInputSize are rejected.
func readAll(r io.Reader) ([]byte, error) {
	// Check Len() before wrapping with LimitReader (which hides it).
	if lr, ok := r.(interface{ Len() int }); ok {
		n := lr.Len()
		if n > MaxInputSize {
			return nil, fmt.Errorf("webp: input too large (%d bytes, max %d)", n, MaxInputSize)
		}
		if n > 0 {
			data := make([]byte, n)
			_, err := io.ReadFull(r, data)
			return data, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxInputSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInputSize {
		return nil, fmt.Errorf("webp: input too large (exceeds %d bytes)", MaxInputSize)
	}
	return data, err
}

// Decode reads a WebP image from r and returns it as an image.Image.
// Decode returns decoded pixels as *image.NRGBA.
func Decode(r io.Reader) (image.Image, error) {
	return DecodeReuse(r, nil)
}

// DecodeReuse decodes a WebP image like [Decode], but reuses the pixel
// buffers of reuse — typically the image returned by a previous Decode or
// DecodeReuse call — when its type and capacity match the new image.
// Pass nil for the first call. When the buffers are incompatible (different
// image type, or too small), a fresh image is allocated, so it is always
// safe to feed the previous result back in a loop:
//
//	var img image.Image
//	for _, f := range files {
//	    img, err = webp.DecodeReuse(f, img)
//	    ...
//	}
//
// The returned image may share storage with reuse; the caller must not use
// reuse after the call.
func DecodeReuse(r io.Reader, reuse image.Image) (image.Image, error) {
	if r == nil {
		return nil, errors.New("webp: nil reader")
	}
	data, err := readAll(r)
	if err != nil {
		return nil, fmt.Errorf("webp: reading data: %w", err)
	}
	return decodeBytes(data, reuse)
}

// DecodeConfig returns the color model and dimensions of a WebP image
// without decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	if r == nil {
		return image.Config{}, errors.New("webp: nil reader")
	}
	data, err := readAll(r)
	if err != nil {
		return image.Config{}, fmt.Errorf("webp: reading data: %w", err)
	}

	p, err := container.NewParser(data)
	if err != nil {
		return image.Config{}, fmt.Errorf("webp: parsing container: %w", err)
	}

	feat := p.Features()

	return image.Config{
		ColorModel: color.NRGBAModel,
		Width:      feat.Width,
		Height:     feat.Height,
	}, nil
}

// GetFeatures reads WebP features (dimensions, format, alpha, animation)
// without decoding pixel data. It parses just the RIFF container and chunk
// headers, making it much cheaper than a full [Decode].
func GetFeatures(r io.Reader) (*Features, error) {
	if r == nil {
		return nil, errors.New("webp: nil reader")
	}
	data, err := readAll(r)
	if err != nil {
		return nil, fmt.Errorf("webp: reading data: %w", err)
	}

	p, err := container.NewParser(data)
	if err != nil {
		return nil, fmt.Errorf("webp: parsing container: %w", err)
	}

	feat := p.Features()
	f := &Features{
		Width:        feat.Width,
		Height:       feat.Height,
		HasAlpha:     feat.HasAlpha,
		HasAnimation: feat.HasAnim,
		FrameCount:   len(p.Frames()),
		LoopCount:    feat.LoopCount,
	}

	switch feat.Format {
	case container.FormatVP8:
		f.Format = "lossy"
	case container.FormatVP8L:
		f.Format = "lossless"
	case container.FormatVP8X:
		f.Format = "extended"
	default:
		f.Format = "unknown"
	}

	return f, nil
}

// decodeBytes decodes a complete WebP file from a byte slice, reusing the
// buffers of reuse when compatible (may be nil).
func decodeBytes(data []byte, reuse image.Image) (image.Image, error) {
	p, err := container.NewParser(data)
	if err != nil {
		return nil, fmt.Errorf("webp: parsing container: %w", err)
	}

	frames := p.Frames()
	if len(frames) == 0 {
		return nil, ErrNoFrames
	}

	// Decode the first frame only; use animation.Decode() for multi-frame.
	frame := frames[0]
	return decodeFrame(frame, reuse)
}

// decodeFrame decodes a single image frame.
func decodeFrame(frame container.FrameInfo, reuse image.Image) (image.Image, error) {
	if frame.IsLossless {
		return decodeLossless(frame.Payload, reuse)
	}
	return decodeLossy(frame.Payload, frame.AlphaData, reuse)
}

// decodeLossless decodes a VP8L lossless bitstream.
func decodeLossless(data []byte, reuse image.Image) (image.Image, error) {
	prev, _ := reuse.(*image.NRGBA)
	img, err := lossless.DecodeVP8LReuse(data, prev)
	if err != nil {
		return nil, fmt.Errorf("webp: lossless decode: %w", err)
	}
	return img, nil
}

// encodeFrameForAnimation encodes an image to a raw VP8/VP8L bitstream
// for use by the animation package's FrameEncoderFunc.
func encodeFrameForAnimation(img image.Image, isLossless bool, quality int) ([]byte, error) {
	opts := &EncoderOptions{
		Lossless: isLossless,
		Quality:  float32(quality),
		Method:   4,
	}
	if isLossless {
		bs, _, err := encodeLossless(img, opts)
		return bs, err
	}
	bs, _, err := encodeLossy(img, opts)
	return bs, err
}

// simpleEncodeForAnimation encodes an image as a complete simple (non-animated)
// WebP file for use by the animation package's single-frame optimization.
func simpleEncodeForAnimation(img image.Image, isLossless bool, quality float32) ([]byte, error) {
	var buf bytes.Buffer
	opts := &EncoderOptions{
		Lossless: isLossless,
		Quality:  quality,
		Method:   4,
	}
	if err := Encode(&buf, img, opts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeFrameForAnimation decodes a VP8/VP8L bitstream into an NRGBA image
// for use by the animation package's FrameDecoderFunc.
func decodeFrameForAnimation(bitstreamData, alphaData []byte) (*image.NRGBA, error) {
	// Determine if this is VP8L (lossless) by checking for the VP8L signature byte.
	isLossless := len(bitstreamData) > 0 && bitstreamData[0] == 0x2f
	var img image.Image
	var err error
	if isLossless {
		img, err = decodeLossless(bitstreamData, nil)
	} else {
		img, err = decodeLossy(bitstreamData, alphaData, nil)
	}
	if err != nil {
		return nil, err
	}
	return img.(*image.NRGBA), nil
}

// decodeLossy decodes a VP8 lossy bitstream to NRGBA using fancy chroma
// upsampling and WebP's limited-range BT.601 YUV conversion.
func decodeLossy(data []byte, alphaData []byte, reuse image.Image) (image.Image, error) {
	dec, width, height, yPlane, yStride, uPlane, vPlane, uvStride, err := lossy.DecodeFrame(data)
	if err != nil {
		return nil, fmt.Errorf("webp: lossy decode: %w", err)
	}
	defer lossy.ReleaseDecoder(dec)

	// Decode alpha plane if present.
	var alphaPlane []byte
	if len(alphaData) > 0 {
		alphaPlane, err = lossy.DecodeAlpha(alphaData, width, height)
		if err != nil {
			return nil, fmt.Errorf("webp: alpha decode: %w", err)
		}
	}

	prev, _ := reuse.(*image.NRGBA)
	return buildNRGBA(width, height, yPlane, yStride, uPlane, vPlane, uvStride, alphaPlane, prev), nil
}

// buildNRGBA constructs an *image.NRGBA from raw YUV planes + alpha using
// the diamond-shaped 4-tap fancy upsampler (FANCY_UPSAMPLING from libwebp).
// If reuse is non-nil and its Pix buffer is large enough, it is reused.
func buildNRGBA(width, height int, yPlane []byte, yStride int, uPlane, vPlane []byte, uvStride int, alphaPlane []byte, reuse *image.NRGBA) *image.NRGBA {
	stride := width * 4
	var img *image.NRGBA
	if reuse != nil && cap(reuse.Pix) >= height*stride {
		img = &image.NRGBA{
			Pix:    reuse.Pix[:height*stride],
			Stride: stride,
			Rect:   image.Rect(0, 0, width, height),
		}
	} else {
		img = image.NewNRGBA(image.Rect(0, 0, width, height))
	}

	yRow := func(row int) []byte {
		off := row * yStride
		return yPlane[off : off+width]
	}
	uRow := func(row int) []byte {
		off := row * uvStride
		return uPlane[off : off+(width+1)/2]
	}
	vRow := func(row int) []byte {
		off := row * uvStride
		return vPlane[off : off+(width+1)/2]
	}
	aRow := func(row int) []byte {
		if alphaPlane == nil {
			return nil
		}
		off := row * width
		return alphaPlane[off : off+width]
	}
	dstRow := func(row int) []byte {
		off := row * img.Stride
		return img.Pix[off : off+width*4]
	}
	// Reuse the packed chroma workspace across row pairs. The SIMD dispatch
	// otherwise creates and clears the same temporary buffer for every pair.
	const maxStackWidth = 2048
	var stackScratch [maxStackWidth * 2]uint32
	scratch := stackScratch[:]
	if width > maxStackWidth {
		scratch = make([]uint32, width*2)
	}

	if height == 1 {
		dsp.UpsampleLinePairNRGBAWithScratch(
			yRow(0), nil, uRow(0), vRow(0), uRow(0), vRow(0),
			dstRow(0), nil, aRow(0), nil, width, scratch,
		)
		return img
	}

	// Row 0: mirror chroma.
	dsp.UpsampleLinePairNRGBAWithScratch(
		yRow(0), nil, uRow(0), vRow(0), uRow(0), vRow(0),
		dstRow(0), nil, aRow(0), nil, width, scratch,
	)

	// Overlapping pairs.
	y := 0
	for y+2 < height {
		chromaTop := y / 2
		chromaBot := chromaTop + 1
		dsp.UpsampleLinePairNRGBAWithScratch(
			yRow(y+1), yRow(y+2),
			uRow(chromaTop), vRow(chromaTop),
			uRow(chromaBot), vRow(chromaBot),
			dstRow(y+1), dstRow(y+2),
			aRow(y+1), aRow(y+2),
			width, scratch,
		)
		y += 2
	}

	// Last row for even-height images.
	if height&1 == 0 {
		lastChroma := (height - 1) / 2
		dsp.UpsampleLinePairNRGBAWithScratch(
			yRow(height-1), nil,
			uRow(lastChroma), vRow(lastChroma),
			uRow(lastChroma), vRow(lastChroma),
			dstRow(height-1), nil,
			aRow(height-1), nil,
			width, scratch,
		)
	}

	return img
}
