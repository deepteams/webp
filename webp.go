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

	// Determine color model to match what Decode() actually returns:
	//   - VP8L (lossless) always decodes to *image.NRGBA
	//   - VP8 (lossy) without alpha decodes to *image.YCbCr
	//   - VP8 (lossy) with alpha decodes to *image.NRGBA
	cm := color.NRGBAModel
	if frames := p.Frames(); len(frames) > 0 {
		if !frames[0].IsLossless && frames[0].AlphaData == nil {
			cm = color.YCbCrModel
		}
	} else if !feat.HasAlpha {
		cm = color.YCbCrModel
	}

	return image.Config{
		ColorModel: cm,
		Width:      feat.Width,
		Height:     feat.Height,
	}, nil
}

// GetFeatures reads WebP features (dimensions, format, alpha, animation)
// without decoding pixel data. It parses just the RIFF container and chunk
// headers, making it much cheaper than a full [Decode].
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
	// Fast path for *image.YCbCr (lossy without alpha).
	if ycbcr, ok := img.(*image.YCbCr); ok {
		return ycbcrToNRGBA(ycbcr), nil
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

// ycbcrToNRGBA converts a 4:2:0 YCbCr image to NRGBA using direct
// YCbCr→RGB conversion (no fancy upsampling, as animation compositing
// doesn't require it).
func ycbcrToNRGBA(ycbcr *image.YCbCr) *image.NRGBA {
	w := ycbcr.Rect.Dx()
	h := ycbcr.Rect.Dy()
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		yi := y * ycbcr.YStride
		ci := (y >> 1) * ycbcr.CStride
		di := y * nrgba.Stride
		for x := 0; x < w; x++ {
			yy := int32(ycbcr.Y[yi+x])
			cb := int32(ycbcr.Cb[ci+(x>>1)]) - 128
			cr := int32(ycbcr.Cr[ci+(x>>1)]) - 128
			r := yy + (91881*cr+32768)>>16
			g := yy - (22554*cb+46802*cr+32768)>>16
			b := yy + (116130*cb+32768)>>16
			if r < 0 {
				r = 0
			} else if r > 255 {
				r = 255
			}
			if g < 0 {
				g = 0
			} else if g > 255 {
				g = 255
			}
			if b < 0 {
				b = 0
			} else if b > 255 {
				b = 255
			}
			nrgba.Pix[di] = byte(r)
			nrgba.Pix[di+1] = byte(g)
			nrgba.Pix[di+2] = byte(b)
			nrgba.Pix[di+3] = 255
			di += 4
		}
	}
	return nrgba
}

// decodeLossy decodes a VP8 lossy bitstream.
// Without alpha data it returns *image.YCbCr (4:2:0) — no colour-space
// conversion needed, just a plane copy.  With alpha it falls back to
// *image.NRGBA using fancy chroma upsampling.
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

	// Fast path: no alpha → return *image.YCbCr directly.
	if alphaPlane == nil {
		return buildYCbCr(width, height, yPlane, yStride, uPlane, vPlane, uvStride), nil
	}

	// Slow path: alpha present → NRGBA with fancy chroma upsampling.
	return buildNRGBA(width, height, yPlane, yStride, uPlane, vPlane, uvStride, alphaPlane), nil
}

// buildYCbCr copies the decoder's Y/U/V cache planes into an image.YCbCr.
// The decoder's slab is returned to the pool after this function, so the
// data must be copied out.
func buildYCbCr(width, height int, yPlane []byte, yStride int, uPlane, vPlane []byte, uvStride int) *image.YCbCr {
	chromaH := (height + 1) / 2

	yLen := height * yStride
	cLen := chromaH * uvStride
	buf := make([]byte, yLen+2*cLen)

	copy(buf[:yLen], yPlane[:yLen])
	copy(buf[yLen:yLen+cLen], uPlane[:cLen])
	copy(buf[yLen+cLen:], vPlane[:cLen])

	return &image.YCbCr{
		Y:              buf[:yLen],
		Cb:             buf[yLen : yLen+cLen],
		Cr:             buf[yLen+cLen:],
		YStride:        yStride,
		CStride:        uvStride,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, width, height),
	}
}

// buildNRGBA constructs an *image.NRGBA from raw YUV planes + alpha using
// the diamond-shaped 4-tap fancy upsampler (FANCY_UPSAMPLING from libwebp).
func buildNRGBA(width, height int, yPlane []byte, yStride int, uPlane, vPlane []byte, uvStride int, alphaPlane []byte) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))

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

	if height == 1 {
		dsp.UpsampleLinePairNRGBA(
			yRow(0), nil, uRow(0), vRow(0), uRow(0), vRow(0),
			dstRow(0), nil, aRow(0), nil, width,
		)
		return img
	}

	// Row 0: mirror chroma.
	dsp.UpsampleLinePairNRGBA(
		yRow(0), nil, uRow(0), vRow(0), uRow(0), vRow(0),
		dstRow(0), nil, aRow(0), nil, width,
	)

	// Overlapping pairs.
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

	// Last row for even-height images.
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

	return img
}
