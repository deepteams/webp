package webp

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/deepteams/webp/internal/container"
	"github.com/deepteams/webp/internal/lossless"
	"github.com/deepteams/webp/internal/lossy"
	"github.com/deepteams/webp/sharpyuv"
)

// MaxDimension is the maximum allowed width or height for a WebP image, in
// pixels. This matches libwebp's WEBP_MAX_DIMENSION constant. Images larger
// than 16383 pixels in either dimension cannot be represented in the WebP
// bitstream format.
const MaxDimension = 16383

// Preset selects a set of encoding parameters tuned for specific content types.
type Preset int

const (
	PresetDefault Preset = iota
	PresetPicture
	PresetPhoto
	PresetDrawing
	PresetIcon
	PresetText
)

// EncoderOptions controls WebP encoding parameters.
type EncoderOptions struct {
	// Lossless enables VP8L lossless encoding.
	// When false (default), VP8 lossy encoding is used.
	Lossless bool

	// Quality is the compression quality (0-100, default 75).
	// For lossy: lower means smaller files with more artifacts.
	// For lossless: controls the compression effort.
	Quality float32

	// Method controls encoding effort (0-6, default 4). Higher values
	// produce smaller files at the cost of longer encoding times:
	//   0 = fastest, least compression
	//   4 = good trade-off between speed and quality (default)
	//   6 = slowest, best compression
	Method int

	// Preset selects encoding parameters tuned for specific content types.
	Preset Preset

	// UseSharpYUV enables sharp (and slow) RGB->YUV conversion.
	UseSharpYUV bool

	// Exact preserves the RGB values under transparent areas. In lossless
	// mode, transparent pixels' RGB are kept as-is instead of being zeroed.
	// In lossy mode, it skips the transparent-area cleanup that normally
	// flattens invisible pixels to reduce encoding cost. Note that lossy
	// VP8 quantization will still modify pixel values regardless of this flag.
	Exact bool

	// TargetSize sets a target output size in bytes (0 = use quality instead).
	TargetSize int

	// TargetPSNR sets a target PSNR value (0 = disabled).
	// When set (and TargetSize is 0), the encoder adjusts quality across
	// multiple passes to converge toward this PSNR level.
	// Matches C libwebp's WebPConfig::target_PSNR.
	TargetPSNR float32

	// Preprocessing selects preprocessing applied before/during encoding
	// (lossy encoding only). This is a bitmask matching C libwebp's
	// WebPConfig::preprocessing field:
	//   0 = none
	//   1 = segment smooth (applies a 3x3 majority-vote filter to the
	//       segment map, reducing noise in segment assignment)
	//   2 = pseudo-random dithering on RGB->YUV conversion
	//   3 = both segment smooth and dithering
	// When bit 1 is set, ordered dithering noise is added to the rounding
	// values during the RGB->YUV color space conversion. The amplitude
	// decreases with quality: max dithering at q=0, 0.5 amplitude at q=100.
	// This reduces banding artifacts at lower quality levels.
	Preprocessing int

	// SNSStrength controls spatial noise shaping strength (0-100, default 50).
	// Higher values give more weight to low-frequency content, improving
	// visual quality at the cost of higher distortion in high-frequency areas.
	// Matches C libwebp's WebPConfig::sns_strength.
	// The default value -1 (or any value < 0) is treated as 50.
	SNSStrength int

	// FilterStrength controls the strength of the deblocking loop filter
	// (0-100, default 60). Higher values produce smoother edges but may
	// blur details. Matches C libwebp's WebPConfig::filter_strength.
	// The default value -1 (or any value < 0) is treated as 60.
	FilterStrength int

	// FilterSharpness controls the sharpness of the loop filter (0-7,
	// default 0). Higher values sharpen the filter effect.
	// Matches C libwebp's WebPConfig::filter_sharpness.
	FilterSharpness int

	// FilterType selects the loop filter type (0=simple, 1=strong, default 1).
	// Strong filtering (1) also filters U/V channels.
	// Matches C libwebp's WebPConfig::filter_type.
	// The default value -1 (or any value < 0) is treated as 1 (strong).
	FilterType int

	// Partitions controls the number of token partitions (0-3, default 0).
	// The actual number of partitions is 1 << Partitions (1, 2, 4, or 8).
	// Matches C libwebp's WebPConfig::partitions.
	Partitions int

	// Segments controls the number of segments to use during encoding
	// (1-4, default 4). Fewer segments speed up encoding at the cost of
	// compression efficiency. Matches C libwebp's WebPConfig::segments.
	// The default value -1 (or any value < 0) is treated as 4.
	Segments int

	// Pass controls the number of entropy-analysis passes (1-10, default 1).
	// Higher values improve compression at the cost of encoding speed.
	// Matches C libwebp's WebPConfig::pass.
	// The default value -1 (or any value < 0) is treated as 1.
	Pass int

	// EmulateJpegSize, when true, tries to produce an output of similar
	// size to a JPEG file of equivalent quality. This is a C libwebp
	// compatibility field (WebPConfig::emulate_jpeg_size). The pure Go
	// encoder does not implement this heuristic; the field is accepted
	// for API compatibility but has no effect on output.
	EmulateJpegSize bool

	// QMin sets the minimum quantizer value (0-100, default 0).
	// Must be <= QMax. Matches C libwebp's WebPConfig::qmin.
	QMin int

	// QMax sets the maximum quantizer value (0-100, default 100).
	// Must be >= QMin. Matches C libwebp's WebPConfig::qmax.
	// The default value -1 (or any value < 0) is treated as 100.
	QMax int

	// AlphaCompression selects the compression method for the alpha channel
	// (lossy encoding only, ignored for lossless which handles alpha natively).
	// Matches C libwebp's WebPConfig::alpha_compression.
	//   0 = no compression (raw alpha bytes)
	//   1 = VP8L lossless compression (default)
	// The default value -1 (or any value < 0) is treated as 1 (lossless).
	AlphaCompression int

	// AlphaFiltering selects the predictive filtering method applied to the
	// alpha plane before compression (lossy encoding only).
	// Matches C libwebp's WebPConfig::alpha_filtering.
	//   0 = none
	//   1 = fast (quick heuristic, default)
	//   2 = best (try all filters, pick smallest)
	// The default value -1 (or any value < 0) is treated as 1 (fast).
	AlphaFiltering int

	// AlphaQuality controls the quality of the alpha channel encoding,
	// independently of the main image quality (lossy encoding only).
	// Range: 0-100. Values below 100 enable alpha level quantization
	// (lossy alpha). Matches C libwebp's WebPConfig::alpha_quality.
	// The default value -1 (or any value < 0) is treated as 100.
	AlphaQuality int

	// ICC holds an ICC color profile to embed in the output.
	// When non-nil, the encoder uses VP8X extended format with the ICCP chunk.
	ICC []byte

	// EXIF holds EXIF metadata to embed in the output.
	// When non-nil, the encoder uses VP8X extended format with the EXIF chunk.
	EXIF []byte

	// XMP holds XMP metadata to embed in the output.
	// When non-nil, the encoder uses VP8X extended format with the XMP chunk.
	XMP []byte
}

// Options is an alias for backward compatibility.
type Options = EncoderOptions

// DefaultOptions returns encoding options with quality 75, lossy, method 4.
// All parameters match C libwebp's WebPConfigInit defaults. Sentinel values
// (-1) are used for fields where Go's zero value differs from the C default,
// ensuring that an uninitialized EncoderOptions{} produces sensible output.
func DefaultOptions() *EncoderOptions {
	return &EncoderOptions{
		Quality:          75,
		Lossless:         false,
		Method:           4,
		SNSStrength:      -1, // sentinel: treated as 50
		FilterStrength:   -1, // sentinel: treated as 60
		FilterSharpness:  0,  // C default is 0; Go zero-value matches
		FilterType:       -1, // sentinel: treated as 1 (strong)
		Partitions:       0,  // C default is 0; Go zero-value matches
		Segments:         -1, // sentinel: treated as 4
		Pass:             -1, // sentinel: treated as 1
		QMin:             0,  // C default is 0; Go zero-value matches
		QMax:             -1, // sentinel: treated as 100
		AlphaCompression: -1, // sentinel: treated as 1 (lossless)
		AlphaFiltering:   -1, // sentinel: treated as 1 (fast)
		AlphaQuality:     -1, // sentinel: treated as 100
	}
}

// OptionsForPreset returns encoding options tuned for the given preset and
// quality, matching C libwebp's WebPConfigPreset (config_enc.c:64-96).
func OptionsForPreset(preset Preset, quality float32) *EncoderOptions {
	opts := DefaultOptions()
	opts.Quality = quality
	opts.Preset = preset

	switch preset {
	case PresetPicture:
		opts.SNSStrength = 80
		opts.FilterSharpness = 4
		opts.FilterStrength = 35
		opts.Preprocessing = opts.Preprocessing &^ 2 // no dithering
	case PresetPhoto:
		opts.SNSStrength = 80
		opts.FilterSharpness = 3
		opts.FilterStrength = 30
		opts.Preprocessing = opts.Preprocessing | 2
	case PresetDrawing:
		opts.SNSStrength = 25
		opts.FilterSharpness = 6
		opts.FilterStrength = 10
	case PresetIcon:
		opts.SNSStrength = 0
		opts.FilterStrength = 0
		opts.Preprocessing = opts.Preprocessing &^ 2 // no dithering
	case PresetText:
		opts.SNSStrength = 0
		opts.FilterStrength = 0
		opts.Preprocessing = opts.Preprocessing &^ 2 // no dithering
		opts.Segments = 2
	case PresetDefault:
		// use defaults
	}

	return opts
}

// validateConfig validates encoder options, matching C libwebp's
// WebPValidateConfig ranges. Returns an error describing the first
// invalid parameter found, or nil if the configuration is valid.
// Negative values are valid sentinels for most int fields (treated as
// C defaults), so only the upper bound (or resolved range) is checked.
func validateConfig(opts *EncoderOptions) error {
	if opts.Quality < 0 || opts.Quality > 100 {
		return fmt.Errorf("webp: invalid Quality %.2f (must be 0-100)", opts.Quality)
	}
	if opts.Method < 0 || opts.Method > 6 {
		return fmt.Errorf("webp: invalid Method %d (must be 0-6)", opts.Method)
	}
	if opts.TargetSize < 0 {
		return fmt.Errorf("webp: invalid TargetSize %d (must be >= 0)", opts.TargetSize)
	}
	if opts.TargetPSNR < 0 {
		return fmt.Errorf("webp: invalid TargetPSNR %.2f (must be >= 0)", opts.TargetPSNR)
	}
	if opts.Preset < PresetDefault || opts.Preset > PresetText {
		return fmt.Errorf("webp: invalid Preset %d", opts.Preset)
	}

	// Validate lossy encoding parameters. Negative values are sentinels
	// (resolved to C defaults at encoding time), so we only reject values
	// that are out of range when non-negative. Zero values are always
	// accepted: for Segments (range 1-4) and Pass (range 1-10), zero acts
	// as a sentinel meaning "use default".
	if opts.SNSStrength > 100 {
		return fmt.Errorf("webp: invalid SNSStrength %d (must be 0-100 or negative sentinel)", opts.SNSStrength)
	}
	if opts.FilterStrength > 100 {
		return fmt.Errorf("webp: invalid FilterStrength %d (must be 0-100 or negative sentinel)", opts.FilterStrength)
	}
	if opts.FilterSharpness < 0 || opts.FilterSharpness > 7 {
		return fmt.Errorf("webp: invalid FilterSharpness %d (must be 0-7)", opts.FilterSharpness)
	}
	if opts.FilterType > 1 {
		return fmt.Errorf("webp: invalid FilterType %d (must be 0 or 1, or negative sentinel)", opts.FilterType)
	}
	if opts.Partitions < 0 || opts.Partitions > 3 {
		return fmt.Errorf("webp: invalid Partitions %d (must be 0-3)", opts.Partitions)
	}
	if opts.Segments > 4 {
		return fmt.Errorf("webp: invalid Segments %d (must be 1-4 or 0/-1 for default)", opts.Segments)
	}
	if opts.Pass > 10 {
		return fmt.Errorf("webp: invalid Pass %d (must be 1-10 or 0/-1 for default)", opts.Pass)
	}
	qmin := opts.QMin
	qmax := resolveQMax(opts.QMax)
	if qmin < 0 || qmax > 100 || qmin > qmax {
		return fmt.Errorf("webp: invalid QMin/QMax %d/%d (must be 0-100, QMin <= QMax)", opts.QMin, opts.QMax)
	}

	// Validate alpha options.
	if opts.AlphaCompression > 1 {
		return fmt.Errorf("webp: invalid AlphaCompression %d (must be 0 or 1)", opts.AlphaCompression)
	}
	if opts.AlphaFiltering > 2 {
		return fmt.Errorf("webp: invalid AlphaFiltering %d (must be 0, 1 or 2)", opts.AlphaFiltering)
	}
	if opts.AlphaQuality > 100 {
		return fmt.Errorf("webp: invalid AlphaQuality %d (must be 0-100)", opts.AlphaQuality)
	}
	return nil
}

// resolveSNSStrength returns the effective SNS strength.
// Negative values (sentinels) map to 50, matching C libwebp's default.
func resolveSNSStrength(v int) int {
	if v < 0 {
		return 50
	}
	return v
}

// resolveFilterStrength returns the effective filter strength.
// Negative values (sentinels) map to 60, matching C libwebp's default.
func resolveFilterStrength(v int) int {
	if v < 0 {
		return 60
	}
	return v
}

// resolveFilterType returns the effective filter type.
// Negative values (sentinels) map to 1 (strong), matching C libwebp's default.
func resolveFilterType(v int) int {
	if v < 0 {
		return 1 // default: strong
	}
	return v
}

// resolveSegments returns the effective segment count.
// Negative values (sentinels) map to 4, matching C libwebp's default.
func resolveSegments(v int) int {
	if v < 0 {
		return 4
	}
	return v
}

// resolvePass returns the effective pass count.
// Negative values (sentinels) map to 1, matching C libwebp's default.
func resolvePass(v int) int {
	if v < 0 {
		return 1
	}
	return v
}

// resolveQMax returns the effective maximum quantizer value.
// Negative values (sentinels) map to 100, matching C libwebp's default.
func resolveQMax(v int) int {
	if v < 0 {
		return 100
	}
	return v
}

// resolveAlphaCompression returns the effective alpha compression method.
// Negative values (sentinels) and the zero-value (for backward compatibility
// with callers that don't set this field) map to 1 (lossless).
func resolveAlphaCompression(v int) int {
	if v < 0 {
		return 1 // default: lossless
	}
	return v
}

// resolveAlphaFiltering returns the effective alpha filtering mode.
// Negative values map to 1 (fast).
func resolveAlphaFiltering(v int) int {
	if v < 0 {
		return 1 // default: fast
	}
	return v
}

// resolveAlphaQuality returns the effective alpha quality.
// Negative values map to 100 (best alpha quality, no quantization).
func resolveAlphaQuality(v int) int {
	if v < 0 {
		return 100 // default: 100
	}
	return v
}

// Encode writes the image img to w in WebP format.
// If opts is nil, DefaultOptions() is used.
// Returns an error if opts contains invalid parameter values.
func Encode(w io.Writer, img image.Image, opts *EncoderOptions) error {
	if opts == nil {
		opts = DefaultOptions()
	}
	if err := validateConfig(opts); err != nil {
		return err
	}

	imgW, imgH := img.Bounds().Dx(), img.Bounds().Dy()
	if imgW > MaxDimension || imgH > MaxDimension {
		return fmt.Errorf("webp: image dimension %dx%d exceeds maximum %d", imgW, imgH, MaxDimension)
	}

	var bitstream []byte
	var fourcc uint32
	var alphaData []byte
	var err error

	if opts.Lossless {
		// VP8L handles alpha natively in the ARGB pixel data.
		bitstream, fourcc, err = encodeLossless(img, opts)
	} else {
		bitstream, alphaData, fourcc, err = encodeLossyWithAlpha(img, opts)
	}
	if err != nil {
		return err
	}

	return writeRIFF(w, fourcc, bitstream, alphaData, img.Bounds().Dx(), img.Bounds().Dy(), opts)
}

// encodeLossyWithAlpha encodes the image as a VP8 lossy bitstream and,
// if the source image has any non-opaque pixels, also encodes the alpha
// plane as an ALPH chunk payload using VP8L lossless compression.
// Returns (vp8Bitstream, alphChunkData, fourcc, error).
func encodeLossyWithAlpha(img image.Image, opts *EncoderOptions) ([]byte, []byte, uint32, error) {
	// Cache alpha detection result to avoid redundant full-image scans.
	hasAlpha := imageHasAlpha(img)
	if !opts.Exact {
		img = cleanupTransparentAreaLossyWith(img, hasAlpha)
	}
	cfg := lossy.DefaultConfig(int(opts.Quality))
	cfg.Method = opts.Method
	if opts.TargetSize > 0 {
		cfg.TargetSize = opts.TargetSize
	}
	if opts.TargetPSNR > 0 {
		cfg.TargetPSNR = opts.TargetPSNR
	}
	// Propagate QMin/QMax for rate control clamping (matching C libwebp).
	cfg.QMin = opts.QMin
	cfg.QMax = resolveQMax(opts.QMax)
	// Propagate lossy encoding options from the public EncoderOptions to
	// the internal EncodeConfig. Fields with sentinel values (< 0) keep
	// the defaults already set by DefaultConfig().
	if opts.SNSStrength >= 0 {
		cfg.SNSStrength = opts.SNSStrength
	}
	if opts.FilterStrength >= 0 {
		cfg.FilterStrength = opts.FilterStrength
	}
	cfg.FilterSharpness = opts.FilterSharpness // 0 == C default, no sentinel needed
	if opts.FilterType >= 0 {
		cfg.FilterType = opts.FilterType
	}
	cfg.Partitions = opts.Partitions // 0 == C default, no sentinel needed
	if opts.Segments > 0 {
		cfg.Segments = opts.Segments
	}
	if opts.Pass > 0 {
		cfg.Pass = opts.Pass
	}

	cfg.Preprocessing = opts.Preprocessing

	// Compute dithering amplitude when preprocessing bit 2 is set.
	// Matches C libwebp webp_enc.c:364-369:
	//   x = quality / 100
	//   dithering = 1.0 + (0.5 - 1.0) * x^4
	// This gives max dithering (~1.0) at low quality, decreasing to 0.5 at q=100.
	if opts.Preprocessing&2 != 0 {
		x := opts.Quality / 100.0
		x2 := x * x
		cfg.Dithering = 1.0 + (0.5-1.0)*x2*x2
	}

	// Pass cached alpha detection to avoid redundant scan in importImage.
	if hasAlpha {
		cfg.HasAlpha = 1
	} else {
		cfg.HasAlpha = 0
	}

	var enc *lossy.VP8Encoder
	if opts.UseSharpYUV {
		yuv, err := sharpYUVConvert(img)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("webp: sharp yuv: %w", err)
		}
		enc = lossy.NewEncoderFromYUV(yuv, img.Bounds().Dx(), img.Bounds().Dy(), cfg)
	} else {
		enc = lossy.NewEncoder(img, cfg)
	}

	defer lossy.ReleaseEncoder(enc)

	bs, err := enc.EncodeFrame()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("webp: lossy encode: %w", err)
	}

	// Check if the source image has any non-opaque alpha.
	alpha := extractAlphaWith(img, hasAlpha)
	if alpha == nil {
		// Fully opaque: simple VP8 with no alpha.
		return bs, nil, container.FourCCVP8, nil
	}

	// Encode the alpha plane using the resolved alpha options.
	// Resolve sentinel / zero-value defaults to match C libwebp:
	//   alpha_compression: 1 (lossless)
	//   alpha_filtering:   1 (fast)
	//   alpha_quality:     100
	bounds := img.Bounds()
	alphaComp := resolveAlphaCompression(opts.AlphaCompression)
	alphaFilt := resolveAlphaFiltering(opts.AlphaFiltering)
	alphaQual := resolveAlphaQuality(opts.AlphaQuality)

	// Map AlphaCompression int to the lossy package constant.
	alphaMethod := lossy.AlphaLosslessCompression
	if alphaComp == 0 {
		alphaMethod = lossy.AlphaNoCompression
	}

	// Map AlphaFiltering (0=none, 1=fast, 2=best) to the internal filter
	// mode constants, matching C alpha_enc.c CompressAlphaJob.
	var alphaFilterMode int
	switch alphaFilt {
	case 0:
		alphaFilterMode = lossy.AlphaFilterModeNone
	case 2:
		alphaFilterMode = lossy.AlphaFilterModeBest
	default: // 1 = fast (default)
		alphaFilterMode = lossy.AlphaFilterModeFast
	}

	alphaCfg := &lossy.AlphaEncoderConfig{
		Quality:     alphaQual,
		Method:      alphaMethod,
		Filter:      alphaFilterMode,
		EffortLevel: opts.Method,
	}
	alphaData, err := lossy.EncodeAlpha(alpha, bounds.Dx(), bounds.Dy(), alphaCfg)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("webp: alpha encode: %w", err)
	}

	return bs, alphaData, container.FourCCVP8, nil
}

// encodeLossy encodes the image as a VP8 lossy bitstream (no alpha).
// Kept for backward compatibility with the animation encoder.
func encodeLossy(img image.Image, opts *EncoderOptions) ([]byte, uint32, error) {
	bs, _, fourcc, err := encodeLossyWithAlpha(img, opts)
	return bs, fourcc, err
}

// encodeLossless encodes the image as a VP8L lossless bitstream.
func encodeLossless(img image.Image, opts *EncoderOptions) ([]byte, uint32, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Convert image to non-premultiplied ARGB uint32 slice.
	// VP8L stores non-premultiplied (NRGBA) pixel values, so we must
	// convert from the image's native format to NRGBA before packing.
	// Using RGBA() directly would give premultiplied values, which causes
	// double-premultiplication when the decoder's argbToNRGBA treats them
	// as non-premultiplied on output.
	argb := make([]uint32, width*height)
	if nrgba, ok := img.(*image.NRGBA); ok {
		for y := 0; y < height; y++ {
			rowOff := (y+bounds.Min.Y-nrgba.Rect.Min.Y)*nrgba.Stride + (bounds.Min.X-nrgba.Rect.Min.X)*4
			for x := 0; x < width; x++ {
				off := rowOff + x*4
				argb[y*width+x] = uint32(nrgba.Pix[off+3])<<24 | uint32(nrgba.Pix[off])<<16 | uint32(nrgba.Pix[off+1])<<8 | uint32(nrgba.Pix[off+2])
			}
		}
	} else if rgba, ok := img.(*image.RGBA); ok {
		for y := 0; y < height; y++ {
			rowOff := (y+bounds.Min.Y-rgba.Rect.Min.Y)*rgba.Stride + (bounds.Min.X-rgba.Rect.Min.X)*4
			for x := 0; x < width; x++ {
				off := rowOff + x*4
				a := rgba.Pix[off+3]
				r, g, b := rgba.Pix[off], rgba.Pix[off+1], rgba.Pix[off+2]
				// Un-premultiply for lossless encoding (VP8L stores NRGBA).
				if a > 0 && a < 255 {
					a16 := uint16(a)
					r = uint8(uint16(r) * 255 / a16)
					g = uint8(uint16(g) * 255 / a16)
					b = uint8(uint16(b) * 255 / a16)
				}
				argb[y*width+x] = uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
			}
		}
	} else {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
				argb[y*width+x] = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
			}
		}
	}

	if !opts.Exact {
		cleanupTransparentAreaLossless(argb)
	}

	lcfg := &lossless.EncoderConfig{
		Quality:             int(opts.Quality),
		Method:              opts.Method,
		NearLosslessQuality: 100,
	}
	bs, err := lossless.Encode(argb, width, height, lcfg)
	if err != nil {
		return nil, 0, fmt.Errorf("webp: lossless encode: %w", err)
	}
	return bs, container.FourCCVP8L, nil
}

// cleanupTransparentAreaLossy smooths out fully-transparent regions of the
// source image before lossy encoding, matching C libwebp's
// WebPCleanupTransparentArea. It operates on 8x8 ARGB blocks:
//   - For partially-transparent blocks, transparent pixels' RGB values are
//     replaced with the average RGB of the opaque pixels in that block.
//   - For fully-transparent blocks, all pixels are flattened to a uniform
//     value (carried from the previous block or read from the first pixel).
//
// This reduces DCT energy under invisible areas, saving bits.
// The function returns the (possibly modified) image; if the source has no
// alpha channel the image is returned unchanged.
func cleanupTransparentAreaLossy(img image.Image) image.Image {
	return cleanupTransparentAreaLossyWith(img, imageHasAlpha(img))
}

func cleanupTransparentAreaLossyWith(img image.Image, hasAlpha bool) image.Image {
	if !hasAlpha {
		return img
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// Convert to NRGBA so we can modify pixels in place.
	nrgba := image.NewNRGBA(image.Rect(0, 0, width, height))
	if src, ok := img.(*image.NRGBA); ok {
		for y := 0; y < height; y++ {
			srcOff := (y+bounds.Min.Y-src.Rect.Min.Y)*src.Stride + (bounds.Min.X-src.Rect.Min.X)*4
			dstOff := y * nrgba.Stride
			copy(nrgba.Pix[dstOff:dstOff+width*4], src.Pix[srcOff:srcOff+width*4])
		}
	} else if src, ok := img.(*image.RGBA); ok {
		for y := 0; y < height; y++ {
			srcOff := (y+bounds.Min.Y-src.Rect.Min.Y)*src.Stride + (bounds.Min.X-src.Rect.Min.X)*4
			dstOff := y * nrgba.Stride
			for x := 0; x < width; x++ {
				soff := srcOff + x*4
				doff := dstOff + x*4
				a := src.Pix[soff+3]
				if a == 0 {
					// nrgba.Pix already zeroed by NewNRGBA
				} else if a == 255 {
					nrgba.Pix[doff] = src.Pix[soff]
					nrgba.Pix[doff+1] = src.Pix[soff+1]
					nrgba.Pix[doff+2] = src.Pix[soff+2]
					nrgba.Pix[doff+3] = 255
				} else {
					a16 := uint16(a)
					nrgba.Pix[doff] = uint8(uint16(src.Pix[soff]) * 255 / a16)
					nrgba.Pix[doff+1] = uint8(uint16(src.Pix[soff+1]) * 255 / a16)
					nrgba.Pix[doff+2] = uint8(uint16(src.Pix[soff+2]) * 255 / a16)
					nrgba.Pix[doff+3] = a
				}
			}
		}
	} else {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
				nrgba.SetNRGBA(x, y, c)
			}
		}
	}

	const blockSize = 8

	for by := 0; by+blockSize <= height; by += blockSize {
		var carryR, carryG, carryB uint8
		needReset := true

		for bx := 0; bx+blockSize <= width; bx += blockSize {
			transparent, opaqueCount, sumR, sumG, sumB := smoothenBlockNRGBA(
				nrgba, bx, by, blockSize, blockSize,
			)
			if transparent {
				// Fully transparent block: flatten to carried value.
				if needReset {
					c := nrgba.NRGBAAt(bx, by)
					carryR, carryG, carryB = c.R, c.G, c.B
					needReset = false
				}
				flattenBlockNRGBA(nrgba, bx, by, blockSize, blockSize, carryR, carryG, carryB)
			} else {
				if opaqueCount > 0 {
					// smoothenBlockNRGBA already replaced transparent pixels
					// with the average; nothing more to do.
					_ = sumR
					_ = sumG
					_ = sumB
				}
				needReset = true
			}
		}

		// Handle right-edge remainder for smoothing only.
		remainder := width % blockSize
		if remainder > 0 {
			bx := width - remainder
			smoothenBlockNRGBA(nrgba, bx, by, remainder, blockSize)
		}
	}

	// Handle bottom-edge remainder rows.
	remainderH := height % blockSize
	if remainderH > 0 {
		by := height - remainderH
		for bx := 0; bx+blockSize <= width; bx += blockSize {
			smoothenBlockNRGBA(nrgba, bx, by, blockSize, remainderH)
		}
		remainder := width % blockSize
		if remainder > 0 {
			bx := width - remainder
			smoothenBlockNRGBA(nrgba, bx, by, remainder, remainderH)
		}
	}

	return nrgba
}

// smoothenBlockNRGBA inspects a block of pixels. For transparent pixels
// (alpha == 0), it replaces their RGB with the average RGB of opaque pixels
// in the block (if any). Returns:
//   - allTransparent: true if every pixel in the block has alpha == 0
//   - opaqueCount: number of pixels with alpha != 0
//   - sumR, sumG, sumB: sum of RGB of opaque pixels (for caller's use)
func smoothenBlockNRGBA(nrgba *image.NRGBA, bx, by, w, h int) (allTransparent bool, opaqueCount int, sumR, sumG, sumB int) {
	for y := by; y < by+h; y++ {
		for x := bx; x < bx+w; x++ {
			c := nrgba.NRGBAAt(x, y)
			if c.A != 0 {
				opaqueCount++
				sumR += int(c.R)
				sumG += int(c.G)
				sumB += int(c.B)
			}
		}
	}
	total := w * h
	if opaqueCount == 0 {
		return true, 0, 0, 0, 0
	}
	if opaqueCount < total {
		avgR := uint8(sumR / opaqueCount)
		avgG := uint8(sumG / opaqueCount)
		avgB := uint8(sumB / opaqueCount)
		for y := by; y < by+h; y++ {
			for x := bx; x < bx+w; x++ {
				c := nrgba.NRGBAAt(x, y)
				if c.A == 0 {
					nrgba.SetNRGBA(x, y, color.NRGBA{R: avgR, G: avgG, B: avgB, A: 0})
				}
			}
		}
	}
	return false, opaqueCount, sumR, sumG, sumB
}

// flattenBlockNRGBA sets every pixel in the block to the given RGB values,
// preserving alpha=0. Used for fully transparent blocks.
func flattenBlockNRGBA(nrgba *image.NRGBA, bx, by, w, h int, r, g, b uint8) {
	for y := by; y < by+h; y++ {
		for x := bx; x < bx+w; x++ {
			nrgba.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 0})
		}
	}
}

// cleanupTransparentAreaLossless replaces the RGB values of fully-transparent
// pixels (alpha == 0) with 0, producing 0x00000000. This matches C libwebp's
// WebPReplaceTransparentPixels(pic, 0x000000) and improves LZ77 compression
// by creating longer runs of identical values in transparent regions.
func cleanupTransparentAreaLossless(argb []uint32) {
	for i, v := range argb {
		if v>>24 == 0 {
			argb[i] = 0x00000000
		}
	}
}

// writeRIFF wraps a VP8/VP8L bitstream in a RIFF/WEBP container and writes it.
// When alphaData or metadata (ICC/EXIF/XMP) is present, it emits the VP8X
// extended format. Otherwise it emits the simple format.
func writeRIFF(w io.Writer, fourcc uint32, bitstream, alphaData []byte, width, height int, opts *EncoderOptions) error {
	hasMetadata := opts != nil && (len(opts.ICC) > 0 || len(opts.EXIF) > 0 || len(opts.XMP) > 0)
	if len(alphaData) > 0 || hasMetadata {
		var icc, exif, xmp []byte
		if opts != nil {
			icc, exif, xmp = opts.ICC, opts.EXIF, opts.XMP
		}
		return writeRIFFExtended(w, fourcc, bitstream, alphaData, width, height, icc, exif, xmp)
	}
	return writeRIFFSimple(w, fourcc, bitstream)
}

// writeRIFFSimple writes the simple RIFF/WEBP container (no VP8X, no ALPH).
func writeRIFFSimple(w io.Writer, fourcc uint32, bitstream []byte) error {
	payloadSize := uint32(len(bitstream))
	paddedPayload := payloadSize + (payloadSize & 1) // RIFF requires even alignment

	// RIFF file size = 4 ("WEBP") + 8 (chunk header) + paddedPayload
	riffSize := 4 + container.ChunkHeaderSize + paddedPayload

	// Total file: 8 (RIFF + size) + riffSize
	buf := make([]byte, 8+riffSize)

	// RIFF header.
	binary.LittleEndian.PutUint32(buf[0:4], container.FourCCRIFF)
	binary.LittleEndian.PutUint32(buf[4:8], riffSize)
	binary.LittleEndian.PutUint32(buf[8:12], container.FourCCWEBP)

	// Chunk header.
	binary.LittleEndian.PutUint32(buf[12:16], fourcc)
	binary.LittleEndian.PutUint32(buf[16:20], payloadSize)

	// Chunk payload.
	copy(buf[20:], bitstream)

	// Padding byte if odd.
	if payloadSize&1 != 0 {
		buf[20+payloadSize] = 0
	}

	_, err := w.Write(buf)
	return err
}

// writeRIFFExtended writes the VP8X extended RIFF/WEBP container.
// The chunk order follows the WebP spec:
//
//	RIFF header -> VP8X -> [ICCP] -> [ALPH] -> VP8/VP8L -> [EXIF] -> [XMP]
func writeRIFFExtended(w io.Writer, fourcc uint32, bitstreamData, alphaData []byte, width, height int, icc, exif, xmp []byte) error {
	const vp8xChunkSize = container.VP8XChunkSize // 10 bytes

	// Build VP8X flags.
	var flags uint32
	if len(alphaData) > 0 {
		flags |= 0x00000010 // bit 4 = alpha
	}
	// VP8L bitstream carries alpha in its header (bit 28 of the packed field).
	if fourcc == container.FourCCVP8L && len(bitstreamData) >= 5 &&
		bitstreamData[0] == container.VP8LMagicByte {
		bits := binary.LittleEndian.Uint32(bitstreamData[1:5])
		if (bits>>28)&0x1 != 0 {
			flags |= 0x00000010
		}
	}
	if len(icc) > 0 {
		flags |= 0x00000020 // bit 5 = ICC (note: bit 0 in some docs, bit 5 per mux/demux.go flagICCP = 1<<5)
	}
	if len(exif) > 0 {
		flags |= 0x00000008 // bit 3 = EXIF
	}
	if len(xmp) > 0 {
		flags |= 0x00000004 // bit 2 = XMP
	}

	// Helper: padded chunk size (header + data + padding).
	paddedChunkSize := func(dataLen int) uint32 {
		n := uint32(dataLen)
		return container.ChunkHeaderSize + n + (n & 1)
	}

	// Calculate total RIFF payload.
	riffSize := uint32(4) + // "WEBP"
		container.ChunkHeaderSize + vp8xChunkSize // VP8X

	if len(icc) > 0 {
		riffSize += paddedChunkSize(len(icc))
	}
	if len(alphaData) > 0 {
		riffSize += paddedChunkSize(len(alphaData))
	}
	riffSize += paddedChunkSize(len(bitstreamData))
	if len(exif) > 0 {
		riffSize += paddedChunkSize(len(exif))
	}
	if len(xmp) > 0 {
		riffSize += paddedChunkSize(len(xmp))
	}

	totalSize := 8 + riffSize
	buf := make([]byte, totalSize)
	off := 0

	// RIFF header.
	binary.LittleEndian.PutUint32(buf[off:], container.FourCCRIFF)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], riffSize)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], container.FourCCWEBP)
	off += 4

	// VP8X chunk.
	binary.LittleEndian.PutUint32(buf[off:], container.FourCCVP8X)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], vp8xChunkSize)
	off += 4
	binary.LittleEndian.PutUint32(buf[off:], flags)
	off += 4
	putLE24(buf[off:], uint32(width-1))
	off += 3
	putLE24(buf[off:], uint32(height-1))
	off += 3

	// writeChunk is a helper to write a chunk inline into buf.
	writeChunk := func(fcc uint32, data []byte) {
		binary.LittleEndian.PutUint32(buf[off:], fcc)
		off += 4
		binary.LittleEndian.PutUint32(buf[off:], uint32(len(data)))
		off += 4
		copy(buf[off:], data)
		off += len(data)
		if len(data)&1 != 0 {
			buf[off] = 0
			off++
		}
	}

	// ICCP chunk.
	if len(icc) > 0 {
		writeChunk(container.FourCCICCP, icc)
	}

	// ALPH chunk.
	if len(alphaData) > 0 {
		writeChunk(container.FourCCALPH, alphaData)
	}

	// VP8/VP8L bitstream chunk.
	writeChunk(fourcc, bitstreamData)

	// EXIF chunk.
	if len(exif) > 0 {
		writeChunk(container.FourCCEXIF, exif)
	}

	// XMP chunk.
	if len(xmp) > 0 {
		writeChunk(container.FourCCXMP, xmp)
	}

	_, err := w.Write(buf)
	return err
}

// putLE24 writes a 24-bit little-endian integer to buf.
func putLE24(buf []byte, v uint32) {
	buf[0] = byte(v)
	buf[1] = byte(v >> 8)
	buf[2] = byte(v >> 16)
}

// imageHasAlpha reports whether the image has any pixel with alpha < 255.
func imageHasAlpha(img image.Image) bool {
	b := img.Bounds()
	if nrgba, ok := img.(*image.NRGBA); ok {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			off := (y-b.Min.Y)*nrgba.Stride + 3 // alpha offset in first pixel
			for x := 0; x < b.Dx(); x++ {
				if nrgba.Pix[off] != 255 {
					return true
				}
				off += 4
			}
		}
		return false
	}
	if rgba, ok := img.(*image.RGBA); ok {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			off := (y-b.Min.Y)*rgba.Stride + 3
			for x := 0; x < b.Dx(); x++ {
				if rgba.Pix[off] != 255 {
					return true
				}
				off += 4
			}
		}
		return false
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a != 0xFFFF {
				return true
			}
		}
	}
	return false
}

// sharpYUVConvert converts an image.Image to YCbCr 4:2:0 using the SharpYUV
// algorithm, which preserves sharp edges during chroma subsampling.
// This replaces the standard averaging-based RGB-to-YUV conversion when
// EncoderOptions.UseSharpYUV is true, matching C libwebp's
// WebPPictureSharpARGBToYUVA behavior.
func sharpYUVConvert(img image.Image) (*image.YCbCr, error) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// Convert to packed RGB (3 bytes per pixel, row-major).
	rgbStride := w * 3
	rgb := make([]byte, h*rgbStride)
	if nrgba, ok := img.(*image.NRGBA); ok {
		for y := 0; y < h; y++ {
			srcOff := (y+bounds.Min.Y-nrgba.Rect.Min.Y)*nrgba.Stride + (bounds.Min.X-nrgba.Rect.Min.X)*4
			dstOff := y * rgbStride
			for x := 0; x < w; x++ {
				rgb[dstOff] = nrgba.Pix[srcOff]
				rgb[dstOff+1] = nrgba.Pix[srcOff+1]
				rgb[dstOff+2] = nrgba.Pix[srcOff+2]
				srcOff += 4
				dstOff += 3
			}
		}
	} else if rgba, ok := img.(*image.RGBA); ok {
		for y := 0; y < h; y++ {
			srcOff := (y+bounds.Min.Y-rgba.Rect.Min.Y)*rgba.Stride + (bounds.Min.X-rgba.Rect.Min.X)*4
			dstOff := y * rgbStride
			for x := 0; x < w; x++ {
				rgb[dstOff] = rgba.Pix[srcOff]
				rgb[dstOff+1] = rgba.Pix[srcOff+1]
				rgb[dstOff+2] = rgba.Pix[srcOff+2]
				srcOff += 4
				dstOff += 3
			}
		}
	} else {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
				off := y*rgbStride + x*3
				rgb[off+0] = c.R
				rgb[off+1] = c.G
				rgb[off+2] = c.B
			}
		}
	}

	// Allocate output YCbCr with 4:2:0 subsampling.
	yuv := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)

	// Run SharpYUV conversion with default options (WebP matrix, sRGB transfer).
	opts := sharpyuv.DefaultOptions()
	if err := sharpyuv.Convert(rgb, w, h, rgbStride, yuv, opts); err != nil {
		return nil, err
	}
	return yuv, nil
}

// extractAlpha extracts the alpha plane from the image as a width*height byte
// slice. Returns nil if all pixels are fully opaque (alpha == 255).
func extractAlpha(img image.Image) []byte {
	return extractAlphaWith(img, imageHasAlpha(img))
}

func extractAlphaWith(img image.Image, hasAlpha bool) []byte {
	if !hasAlpha {
		return nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	alpha := make([]byte, w*h)
	if nrgba, ok := img.(*image.NRGBA); ok {
		for y := 0; y < h; y++ {
			rowOff := (y+b.Min.Y-nrgba.Rect.Min.Y)*nrgba.Stride + (b.Min.X-nrgba.Rect.Min.X)*4 + 3
			for x := 0; x < w; x++ {
				alpha[y*w+x] = nrgba.Pix[rowOff]
				rowOff += 4
			}
		}
		return alpha
	}
	if rgba, ok := img.(*image.RGBA); ok {
		for y := 0; y < h; y++ {
			rowOff := (y+b.Min.Y-rgba.Rect.Min.Y)*rgba.Stride + (b.Min.X-rgba.Rect.Min.X)*4 + 3
			for x := 0; x < w; x++ {
				alpha[y*w+x] = rgba.Pix[rowOff]
				rowOff += 4
			}
		}
		return alpha
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			alpha[y*w+x] = c.A
		}
	}
	return alpha
}
