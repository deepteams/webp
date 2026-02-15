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

// Features describes a WebP file's properties.
type Features struct {
	Width        int
	Height       int
	HasAlpha     bool
	HasAnimation bool
	Format       string // "lossy", "lossless", "extended"
	LoopCount    int    // animation loop count (0 = infinite)
	FrameCount   int    // number of frames (1 for still images)
}

// readAll reads all data from r. If r implements Len() int (e.g.
// *bytes.Reader), a single exact-sized allocation is used instead of
// the repeated doublings that io.ReadAll performs.
func readAll(r io.Reader) ([]byte, error) {
	if lr, ok := r.(interface{ Len() int }); ok {
		n := lr.Len()
		if n > 0 {
			data := make([]byte, n)
			_, err := io.ReadFull(r, data)
			return data, err
		}
	}
	return io.ReadAll(r)
}

// Decode reads a WebP image from r and returns it as an image.Image.
// For lossless images the returned type is *image.NRGBA.
// For lossy images the returned type is *image.YCbCr (when available) or *image.NRGBA.
func Decode(r io.Reader) (image.Image, error) {
	data, err := readAll(r)
	if err != nil {
		return nil, fmt.Errorf("webp: reading data: %w", err)
	}
	return decodeBytes(data)
}

// DecodeConfig returns the color model and dimensions of a WebP image
// without decoding the entire image.
func DecodeConfig(r io.Reader) (image.Config, error) {
	data, err := readAll(r)
	if err != nil {
		return image.Config{}, fmt.Errorf("webp: reading data: %w", err)
	}

	p, err := container.NewParser(data)
	if err != nil {
		return image.Config{}, fmt.Errorf("webp: parsing container: %w", err)
	}

	feat := p.Features()
	cm := color.NRGBAModel
	if !feat.HasAlpha {
		cm = color.YCbCrModel
	}

	return image.Config{
		ColorModel: cm,
		Width:      feat.Width,
		Height:     feat.Height,
	}, nil
}

// GetFeatures reads WebP features without decoding pixel data.
func GetFeatures(r io.Reader) (*Features, error) {
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
		Width:      feat.Width,
		Height:     feat.Height,
		HasAlpha:   feat.HasAlpha,
		HasAnimation: feat.HasAnim,
		FrameCount: len(p.Frames()),
		LoopCount:  feat.LoopCount,
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

// decodeBytes decodes a complete WebP file from a byte slice.
func decodeBytes(data []byte) (image.Image, error) {
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
	return decodeFrame(frame)
}

// decodeFrame decodes a single image frame.
func decodeFrame(frame container.FrameInfo) (image.Image, error) {
	if frame.IsLossless {
		return decodeLossless(frame.Payload)
	}
	return decodeLossy(frame.Payload, frame.AlphaData)
}

// decodeLossless decodes a VP8L lossless bitstream.
func decodeLossless(data []byte) (image.Image, error) {
	img, err := lossless.DecodeVP8L(data)
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
		img, err = decodeLossless(bitstreamData)
	} else {
		img, err = decodeLossy(bitstreamData, alphaData)
	}
	if err != nil {
		return nil, err
	}
	// Convert to NRGBA if needed.
	if nrgba, ok := img.(*image.NRGBA); ok {
		return nrgba, nil
	}
	b := img.Bounds()
	nrgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			nrgba.Set(x-b.Min.X, y-b.Min.Y, img.At(x, y))
		}
	}
	return nrgba, nil
}

// decodeLossy decodes a VP8 lossy bitstream and returns an *image.NRGBA.
// If alphaData is non-nil, the alpha plane is decoded and merged into the output.
//
// Chroma upsampling uses the diamond-shaped 4-tap kernel (FANCY_UPSAMPLING)
// from the libwebp reference, processing luma rows in overlapping pairs to
// interpolate between adjacent chroma rows.
func decodeLossy(data []byte, alphaData []byte) (image.Image, error) {
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

	img := image.NewNRGBA(image.Rect(0, 0, width, height))

	// Helper to get a luma row slice.
	yRow := func(row int) []byte {
		off := row * yStride
		return yPlane[off : off+width]
	}
	// Helper to get a chroma row slice.
	uRow := func(row int) []byte {
		off := row * uvStride
		return uPlane[off : off+(width+1)/2]
	}
	vRow := func(row int) []byte {
		off := row * uvStride
		return vPlane[off : off+(width+1)/2]
	}
	// Helper to get an alpha row slice (may return nil).
	aRow := func(row int) []byte {
		if alphaPlane == nil {
			return nil
		}
		off := row * width
		return alphaPlane[off : off+width]
	}
	// Helper to get an NRGBA destination row slice.
	dstRow := func(row int) []byte {
		off := row * img.Stride
		return img.Pix[off : off+width*4]
	}

	// The fancy upsampler follows the libwebp EmitFancyRGB pattern:
	//   1. Row 0 alone: mirror chroma (topU = botU = U[0])
	//   2. Overlapping pairs (1,2), (3,4), ... with adjacent chroma rows
	//   3. Last row alone if height is even: mirror chroma

	if height == 1 {
		// Single row: mirror chroma vertically.
		dsp.UpsampleLinePairNRGBA(
			yRow(0), nil,
			uRow(0), vRow(0),
			uRow(0), vRow(0),
			dstRow(0), nil,
			aRow(0), nil,
			width,
		)
		return img, nil
	}

	// Row 0: first row special case -- mirror chroma, only top output.
	// We emit rows 0 and 1 together using U[0] as both top and bottom chroma,
	// but that would not be correct for the diamond kernel. Instead, follow
	// the C pattern exactly: row 0 alone with mirrored chroma.
	dsp.UpsampleLinePairNRGBA(
		yRow(0), nil,
		uRow(0), vRow(0),
		uRow(0), vRow(0),
		dstRow(0), nil,
		aRow(0), nil,
		width,
	)

	// Overlapping pairs: (1,2), (3,4), (5,6), ...
	// Each pair uses chroma rows (j, j+1) where j = lumaRow/2.
	y := 0
	for y+2 < height {
		chromaTop := y / 2
		chromaBot := chromaTop + 1
		dsp.UpsampleLinePairNRGBA(
			yRow(y+1), yRow(y+2),
			uRow(chromaTop), vRow(chromaTop),
			uRow(chromaBot), vRow(chromaBot),
			dstRow(y+1), dstRow(y+2),
			aRow(y+1), aRow(y+2),
			width,
		)
		y += 2
	}

	// Last row for even-height images: mirror chroma, only top output.
	if height&1 == 0 {
		lastChroma := (height - 1) / 2
		dsp.UpsampleLinePairNRGBA(
			yRow(height-1), nil,
			uRow(lastChroma), vRow(lastChroma),
			uRow(lastChroma), vRow(lastChroma),
			dstRow(height-1), nil,
			aRow(height-1), nil,
			width,
		)
	}

	return img, nil
}
