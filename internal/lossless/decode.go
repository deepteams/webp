package lossless

import (
	"errors"
	"image"

	"github.com/deepteams/webp/internal/bitio"
)

// VP8L decoder errors.
var (
	ErrBadSignature  = errors.New("lossless: bad VP8L signature")
	ErrBadVersion    = errors.New("lossless: bad VP8L version")
	ErrBitstream     = errors.New("lossless: bitstream error")
	ErrTooManyGroups = errors.New("lossless: too many Huffman groups")
)

// Decoder decodes a VP8L lossless bitstream into an ARGB pixel buffer.
type Decoder struct {
	br *bitio.LosslessReader

	Width    int
	Height   int
	HasAlpha bool

	// transformWidth is the working width after all transforms have been
	// applied (e.g., reduced by color-indexing pixel packing). It matches
	// the C reference's dec->width which is updated by UpdateDecoder.
	transformWidth int

	// Decoded pixel buffer (ARGB, row-major). After decoding the image
	// stream, this holds the raw pixels before inverse transforms are applied.
	pixels []uint32
	// Scratch cache for applying inverse transforms.
	argbCache []uint32

	// Huffman metadata for the current image level.
	hdr metadata

	// Transforms (applied in reverse order during inverse).
	transforms     [NumTransforms]Transform
	nextTransform  int
	transformsSeen uint32

	// Reusable scratch buffers for Huffman decoding.
	codeLengthsBuf []int             // reusable buffer for readHuffmanCode
	huffScratch    HuffmanTableScratch // slab allocator for Huffman tables
}

// metadata holds the Huffman-related state for the current decode level.
type metadata struct {
	colorCacheSize      int
	colorCache          *ColorCache
	huffmanImage        []uint32
	huffmanSubsampleBits int
	huffmanXSize        int
	huffmanMask         int
	numHTreeGroups      int
	htreeGroups         []HTreeGroup
}

// DecodeVP8L decodes a VP8L bitstream (the payload after the VP8L fourcc and
// chunk size) and returns an NRGBA image.
func DecodeVP8L(data []byte) (*image.NRGBA, error) {
	dec := &Decoder{}
	if err := dec.decodeHeader(data); err != nil {
		return nil, err
	}

	// Pre-allocate the Huffman table slab. 64K entries covers most images;
	// BuildHuffmanTableScratch falls back to make() if the slab is exhausted.
	const huffSlabSize = 1 << 16
	if cap(dec.huffScratch.tableSlab) < huffSlabSize {
		dec.huffScratch.tableSlab = make([]HuffmanCode, huffSlabSize)
	}
	dec.huffScratch.slabOff = 0

	// Decode the full image stream (level-0). This reads transforms,
	// color cache, and Huffman codes. After this call, dec.transformWidth
	// holds the working width (reduced by pixel-packing transforms).
	if err := dec.decodeImageStream(dec.Width, dec.Height, true); err != nil {
		return nil, err
	}

	// Use the transform-adjusted width for pixel allocation and decoding,
	// matching the C reference which uses dec->width (set by UpdateDecoder).
	tw := dec.transformWidth
	if tw == 0 {
		tw = dec.Width // fallback if no transform changed the width
	}

	// Allocate output + cache. The pixel buffer uses the original image
	// dimensions for the final output, but the decoded (packed) data
	// uses transformWidth.
	numPixOrig := dec.Width * dec.Height
	numPixTrans := tw * dec.Height
	// Allocate enough for the larger of the two, plus cache rows.
	numAlloc := numPixOrig
	if numPixTrans > numAlloc {
		numAlloc = numPixTrans
	}
	dec.pixels = make([]uint32, numAlloc+dec.Width+dec.Width*numArgbCacheRows)
	dec.argbCache = dec.pixels[numAlloc+dec.Width:]

	// Decode the entropy-coded image data using the transform width.
	if err := dec.decodeImageData(dec.pixels[:numPixTrans], tw, dec.Height, dec.Height); err != nil {
		return nil, err
	}

	// Apply inverse transforms. The transforms know the original width
	// and will expand packed pixels back to the full image dimensions.
	out := dec.applyInverseTransforms(dec.pixels[:numPixOrig])

	return argbToNRGBA(out, dec.Width, dec.Height), nil
}

// decodeHeader reads the VP8L header: signature, width, height, alpha, version.
func (dec *Decoder) decodeHeader(data []byte) error {
	if len(data) < VP8LHeaderSize {
		return ErrBadSignature
	}
	if data[0] != VP8LMagicByte {
		return ErrBadSignature
	}

	dec.br = bitio.NewLosslessReader(data[1:]) // skip signature byte

	bits := dec.br.ReadBits(VP8LImageSizeBits)
	dec.Width = int(bits) + 1
	bits = dec.br.ReadBits(VP8LImageSizeBits)
	dec.Height = int(bits) + 1
	dec.HasAlpha = dec.br.ReadBits(1) != 0
	version := dec.br.ReadBits(VP8LVersionBits)
	if version != VP8LVersion {
		return ErrBadVersion
	}
	if dec.br.IsEndOfStream() {
		return ErrBitstream
	}
	return nil
}

// decodeImageStream reads transforms, color cache config, and Huffman codes.
// If isLevel0 is true this is the top-level image (transforms are read);
// otherwise it's a recursive sub-image (for transform data or meta Huffman).
func (dec *Decoder) decodeImageStream(xsize, ysize int, isLevel0 bool) error {
	transformXSize := xsize
	transformYSize := ysize

	// Read transforms (level-0 only; may recurse).
	if isLevel0 {
		for dec.br.ReadBits(1) == 1 {
			var err error
			transformXSize, err = dec.readTransform(transformXSize, transformYSize)
			if err != nil {
				return err
			}
		}
	}

	// Color cache.
	colorCacheBits := 0
	if dec.br.ReadBits(1) == 1 {
		colorCacheBits = int(dec.br.ReadBits(4))
		if colorCacheBits < 1 || colorCacheBits > MaxCacheBits {
			return ErrBitstream
		}
	}

	// Read Huffman codes (may recurse for meta Huffman image).
	if err := dec.readHuffmanCodes(transformXSize, transformYSize, colorCacheBits, isLevel0); err != nil {
		return err
	}

	// Set up color cache.
	if colorCacheBits > 0 {
		dec.hdr.colorCacheSize = 1 << colorCacheBits
		dec.hdr.colorCache = NewColorCache(colorCacheBits)
	} else {
		dec.hdr.colorCacheSize = 0
		dec.hdr.colorCache = nil
	}

	dec.updateDecoder(transformXSize, transformYSize)

	if isLevel0 {
		// Level-0: header complete; caller will call decodeImageData.
		return nil
	}

	// Sub-image: decode immediately and return data via the pixels buffer.
	return nil
}

// decodeSubImage reads a complete sub-image (transform data, meta Huffman)
// and returns the decoded ARGB pixels.
func (dec *Decoder) decodeSubImage(xsize, ysize int) ([]uint32, error) {
	// Save current metadata.
	savedHdr := dec.hdr
	dec.hdr = metadata{}

	// Decode the sub-image stream.
	if err := dec.decodeImageStream(xsize, ysize, false); err != nil {
		dec.hdr = savedHdr
		return nil, err
	}

	totalSize := xsize * ysize
	data := make([]uint32, totalSize)

	if err := dec.decodeImageData(data, xsize, ysize, ysize); err != nil {
		dec.hdr = savedHdr
		return nil, err
	}

	// Restore parent metadata.
	dec.hdr = savedHdr
	return data, nil
}

// updateDecoder updates the working width/height and Huffman tile parameters.
// This matches the C reference UpdateDecoder which sets dec->width to the
// transform-adjusted width (e.g., reduced by color-indexing pixel packing).
func (dec *Decoder) updateDecoder(width, height int) {
	dec.transformWidth = width
	numBits := dec.hdr.huffmanSubsampleBits
	dec.hdr.huffmanXSize = VP8LSubSampleSize(width, numBits)
	if numBits == 0 {
		dec.hdr.huffmanMask = ^0 // all bits set => always same group
	} else {
		dec.hdr.huffmanMask = (1 << numBits) - 1
	}
}

const numArgbCacheRows = 16

// argbToNRGBA converts an ARGB pixel buffer to image.NRGBA.
// VP8L internal pixel order is ARGB (alpha in bits 31..24, red 23..16,
// green 15..8, blue 7..0).
func argbToNRGBA(pixels []uint32, width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			argb := pixels[y*width+x]
			a := uint8(argb >> 24)
			r := uint8(argb >> 16)
			g := uint8(argb >> 8)
			b := uint8(argb)
			off := img.PixOffset(x, y)
			img.Pix[off+0] = r
			img.Pix[off+1] = g
			img.Pix[off+2] = b
			img.Pix[off+3] = a
		}
	}
	return img
}

// NRGBAToARGB converts an NRGBA image back to a []uint32 ARGB buffer.
func NRGBAToARGB(img *image.NRGBA) []uint32 {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	pixels := make([]uint32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x+bounds.Min.X, y+bounds.Min.Y)
			pixels[y*w+x] = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		}
	}
	return pixels
}

// ARGBToNRGBA is an alias for the internal conversion used by tests.
func ARGBToNRGBA(pixels []uint32, width, height int) *image.NRGBA {
	return argbToNRGBA(pixels, width, height)
}

