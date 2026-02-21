package lossless

import (
	"errors"
	"image"
	"runtime"
	"sync"

	"github.com/deepteams/webp/internal/bitio"
)

// losslessDecoderPool caches Decoder structs between decode calls so that the
// large pixels buffer and Huffman scratch can be reused.
var losslessDecoderPool sync.Pool

// acquireDecoder returns a Decoder from the pool (or allocates a new one).
// Mutable state is zeroed; reusable buffers (pixels, huffScratch, codeLengthsBuf)
// are kept for reuse.
func acquireDecoder() *Decoder {
	if v := losslessDecoderPool.Get(); v != nil {
		dec := v.(*Decoder)
		dec.br = nil
		dec.Width = 0
		dec.Height = 0
		dec.HasAlpha = false
		dec.transformWidth = 0
		dec.nextTransform = 0
		dec.transformsSeen = 0
		dec.hdr = metadata{}
		// Keep: pixels, codeLengthsBuf, huffScratch (for reuse)
		return dec
	}
	return &Decoder{}
}

// releaseDecoder returns a Decoder to the pool for reuse.
func releaseDecoder(dec *Decoder) {
	if dec == nil {
		return
	}
	// Nil external references to avoid holding onto input data.
	dec.br = nil
	dec.argbCache = nil
	dec.hdr.htreeGroups = nil
	dec.hdr.huffmanImage = nil
	dec.hdr.colorCache = nil
	// Keep pixels, transformBuf, and huffScratch for reuse.
	losslessDecoderPool.Put(dec)
}

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
	// Reusable buffer for applyInverseTransforms output (pooled).
	transformBuf []uint32

	// Huffman metadata for the current image level.
	hdr metadata

	// Transforms (applied in reverse order during inverse).
	transforms     [NumTransforms]Transform
	nextTransform  int
	transformsSeen uint32

	// Reusable scratch buffers for Huffman decoding.
	codeLengthsBuf []int             // reusable buffer for readHuffmanCode
	huffScratch    HuffmanTableScratch // slab allocator for Huffman tables

	// Pooled buffers to reduce allocations across decode calls.
	colorCacheBuf  []uint32    // reusable color cache backing array
	htreeGroupsBuf []HTreeGroup // reusable HTreeGroup slice
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
	dec := acquireDecoder()
	defer releaseDecoder(dec)

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

	// Reuse pixels buffer if large enough.
	needed := numAlloc + dec.Width + dec.Width*numArgbCacheRows
	if cap(dec.pixels) >= needed {
		dec.pixels = dec.pixels[:needed]
	} else {
		dec.pixels = make([]uint32, needed)
	}
	dec.argbCache = dec.pixels[numAlloc+dec.Width:]

	// Reuse transform output buffer if large enough.
	if cap(dec.transformBuf) >= numAlloc {
		dec.transformBuf = dec.transformBuf[:numAlloc]
	} else {
		dec.transformBuf = make([]uint32, numAlloc)
	}

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

	// Set up color cache, reusing pooled buffer when possible.
	if colorCacheBits > 0 {
		size := 1 << colorCacheBits
		dec.hdr.colorCacheSize = size
		if cap(dec.colorCacheBuf) >= size {
			dec.colorCacheBuf = dec.colorCacheBuf[:size]
			for i := range dec.colorCacheBuf {
				dec.colorCacheBuf[i] = 0
			}
		} else {
			dec.colorCacheBuf = make([]uint32, size)
		}
		dec.hdr.colorCache = &ColorCache{
			Colors:    dec.colorCacheBuf,
			HashShift: 32 - colorCacheBits,
			HashBits:  colorCacheBits,
		}
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
// For large images, the conversion is parallelized across rows.
func argbToNRGBA(pixels []uint32, width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	pix := img.Pix
	stride := img.Stride

	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 1 && width*height >= minPixelsForParallel {
		rowsPerWorker := height / numWorkers
		var wg sync.WaitGroup
		wg.Add(numWorkers)
		for w := 0; w < numWorkers; w++ {
			yStart := w * rowsPerWorker
			yEnd := yStart + rowsPerWorker
			if w == numWorkers-1 {
				yEnd = height
			}
			go func(yStart, yEnd int) {
				argbToNRGBARows(pixels, pix, stride, width, yStart, yEnd)
				wg.Done()
			}(yStart, yEnd)
		}
		wg.Wait()
	} else {
		argbToNRGBARows(pixels, pix, stride, width, 0, height)
	}
	return img
}

// argbToNRGBARows converts a range of rows from ARGB to NRGBA byte layout.
func argbToNRGBARows(pixels []uint32, pix []byte, stride, width, yStart, yEnd int) {
	for y := yStart; y < yEnd; y++ {
		row := pixels[y*width : y*width+width]
		dst := pix[y*stride : y*stride+width*4]
		n := len(row)
		// Process 4 pixels at a time.
		i := 0
		for ; i+3 < n; i += 4 {
			off := i * 4
			_ = dst[off+15] // BCE for 4 pixels
			a0 := row[i]
			a1 := row[i+1]
			a2 := row[i+2]
			a3 := row[i+3]
			dst[off+0] = uint8(a0 >> 16)
			dst[off+1] = uint8(a0 >> 8)
			dst[off+2] = uint8(a0)
			dst[off+3] = uint8(a0 >> 24)
			dst[off+4] = uint8(a1 >> 16)
			dst[off+5] = uint8(a1 >> 8)
			dst[off+6] = uint8(a1)
			dst[off+7] = uint8(a1 >> 24)
			dst[off+8] = uint8(a2 >> 16)
			dst[off+9] = uint8(a2 >> 8)
			dst[off+10] = uint8(a2)
			dst[off+11] = uint8(a2 >> 24)
			dst[off+12] = uint8(a3 >> 16)
			dst[off+13] = uint8(a3 >> 8)
			dst[off+14] = uint8(a3)
			dst[off+15] = uint8(a3 >> 24)
		}
		// Remaining pixels.
		for ; i < n; i++ {
			off := i * 4
			argb := row[i]
			_ = dst[off+3] // BCE
			dst[off+0] = uint8(argb >> 16)
			dst[off+1] = uint8(argb >> 8)
			dst[off+2] = uint8(argb)
			dst[off+3] = uint8(argb >> 24)
		}
	}
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

