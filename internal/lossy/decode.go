package lossy

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/deepteams/webp/internal/bitio"
	"github.com/deepteams/webp/internal/dsp"
)

// lossyDecoderPool caches Decoder structs between decode calls so that the
// large backing slab (intraT + yuvB + cacheY/U/V) can be reused.
var lossyDecoderPool sync.Pool

// acquireDecoder returns a Decoder from the pool (or allocates a new one).
// Mutable header/state fields are zeroed; reusable buffers (yuvT, mbInfo,
// fInfo, mbData, slab via cacheY) are kept for reuse-or-grow in initFrame.
func acquireDecoder() *Decoder {
	if v := lossyDecoderPool.Get(); v != nil {
		dec := v.(*Decoder)
		// Zero mutable state — keep slice backing arrays for reuse.
		dec.frmHdr = FrameHeader{}
		dec.picHdr = PictureHeader{}
		dec.filterHdr = FilterHeader{}
		dec.segHdr = SegmentHeader{}
		dec.mbW = 0
		dec.mbH = 0
		dec.mbX = 0
		dec.mbY = 0
		dec.br = nil
		for i := range dec.parts {
			dec.parts[i] = nil
		}
		dec.numPartsMinusOne = 0
		dec.useSkipProba = false
		dec.skipP = 0
		dec.filterType = 0
		dec.AlphaData = nil
		return dec
	}
	return &Decoder{}
}

// ReleaseDecoder returns a Decoder to the pool for reuse.
// The caller must not reference any slices from the decoder after this call.
func ReleaseDecoder(dec *Decoder) {
	if dec == nil {
		return
	}
	// Nil external references to avoid holding onto large input data.
	dec.br = nil
	for i := range dec.parts {
		dec.parts[i] = nil
	}
	dec.AlphaData = nil
	lossyDecoderPool.Put(dec)
}

// BoolSource abstracts the VP8 boolean decoder interface needed by the parser.
type BoolSource interface {
	GetBit(prob uint8) int
	GetValue(numBits int) uint32
	GetSigned(v int) int
	GetSignedValue(numBits int) int32
	EOF() bool
}

// FrameHeader contains per-frame metadata from the VP8 bitstream header.
type FrameHeader struct {
	KeyFrame        bool
	Profile         uint8
	Show            bool
	PartitionLength uint32
}

// PictureHeader contains picture dimensions and scaling info.
type PictureHeader struct {
	Width      int
	Height     int
	XScale     uint8
	YScale     uint8
	Colorspace uint8
	ClampType  uint8
}

// SegmentHeader describes segment-based quantizer/filter overrides.
type SegmentHeader struct {
	UseSegment    bool
	UpdateMap     bool
	AbsoluteDelta bool
	Quantizer     [NumMBSegments]int8
	FilterStrength [NumMBSegments]int8
}

// FilterHeader describes the loop filter parameters.
type FilterHeader struct {
	Simple     bool
	Level      int
	Sharpness  int
	UseLFDelta bool
	RefLFDelta  [NumRefLFDeltas]int
	ModeLFDelta [NumModeLFDeltas]int
}

// FInfo holds per-macroblock filter strength info.
type FInfo struct {
	FLimit   uint8
	FILevel  uint8
	FInner   bool
	HevThresh uint8
}

// MB holds top/left context for coefficient parsing.
type MB struct {
	Nz   uint8 // non-zero AC/DC coeffs (4-bit luma + 4-bit chroma)
	NzDC uint8 // non-zero DC coeff (1 bit)
}

// MBData holds parsed macroblock reconstruction data.
type MBData struct {
	Coeffs    [384]int16 // (16+4+4) * 16
	IsI4x4    bool
	IModes    [16]uint8  // one 16x16 mode or sixteen 4x4 modes
	UVMode    uint8
	NonZeroY  uint32
	NonZeroUV uint32
	Dither    uint8
	Skip      bool
	Segment   uint8
}

// TopSamples holds saved top samples for one macroblock column.
type TopSamples struct {
	Y [16]uint8
	U [8]uint8
	V [8]uint8
}

// OutputFunc is called for each decoded macroblock row.
// y, u, v are the YUV planes; yStride/uvStride are their strides.
// mbY is the macroblock row, mbW is the width in pixels, mbH is the height of
// this row (typically 16, possibly less for the last row).
type OutputFunc func(mbY, mbW, mbH int, y []byte, yStride int, u, v []byte, uvStride int) bool

// Decoder is the VP8 lossy bitstream decoder.
type Decoder struct {
	// Headers
	frmHdr  FrameHeader
	picHdr  PictureHeader
	filterHdr FilterHeader
	segHdr  SegmentHeader

	// Dimensions in macroblock units.
	mbW, mbH int

	// Macroblock position and parsing bounds.
	mbX, mbY   int
	tlMBX, tlMBY int
	brMBX, brMBY int

	// Partitions.
	br    *bitio.BoolReader   // partition 0 (header/modes)
	parts [MaxNumPartitions]*bitio.BoolReader
	numPartsMinusOne uint32

	// Probabilities.
	proba        Proba
	useSkipProba bool
	skipP        uint8

	// Dequantization.
	dqm [NumMBSegments]QuantMatrix

	// Filter.
	filterType int // 0=off, 1=simple, 2=complex
	fstrengths [NumMBSegments][2]FInfo

	// Boundary data.
	intraT []uint8      // top intra modes (4 * mbW)
	intraL [4]uint8     // left intra modes
	yuvT   []TopSamples // top samples (mbW)
	mbInfo []MB         // contextual MB info (mbW + 1); index 0 is the left sentinel
	fInfo  []FInfo      // filter strength info (mbW)
	yuvB   []byte       // YUV reconstruction buffer (YUVSize bytes)
	mbData []MBData     // per-MB data (mbW)

	// Cache for output rows.
	cacheY, cacheU, cacheV []byte
	cacheYStride           int
	cacheUVStride          int
	cacheYOff, cacheUOff, cacheVOff int // offsets into cache for extra filter rows

	// slab is the single backing allocation for intraT+yuvB+cacheY/U/V,
	// kept across pool reuses so initFrame can reuse-or-grow.
	slab []byte

	// Alpha.
	AlphaData []byte // compressed alpha data (set externally)

	// Scratch space reused across macroblock decodes to avoid heap escapes.
	dcScratch [16]int16
}

// DecodeFrame decodes a complete VP8 lossy frame from data.
// It returns the Decoder (for pool reuse), width, height and the decoded YUV
// planes (Y, U, V) plus their strides. The caller must call
// ReleaseDecoder(dec) after consuming the YUV planes.
func DecodeFrame(data []byte) (dec *Decoder, width, height int, y []byte, yStride int, u, v []byte, uvStride int, err error) {
	dec = acquireDecoder()

	if err = dec.parseHeaders(data); err != nil {
		ReleaseDecoder(dec)
		dec = nil
		return
	}

	width = dec.picHdr.Width
	height = dec.picHdr.Height

	if err = dec.initFrame(); err != nil {
		ReleaseDecoder(dec)
		dec = nil
		return
	}

	dec.precomputeFilterStrengths()

	if err = dec.parseFrame(); err != nil {
		ReleaseDecoder(dec)
		dec = nil
		return
	}

	yStride = dec.cacheYStride
	uvStride = dec.cacheUVStride
	y = dec.cacheY[:height*yStride]
	u = dec.cacheU[:((height+1)/2)*uvStride]
	v = dec.cacheV[:((height+1)/2)*uvStride]
	return
}

// parseHeaders reads the VP8 frame and picture headers, segment/filter info,
// partitions, quantizers, and probability tables.
func (dec *Decoder) parseHeaders(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("vp8: truncated header")
	}

	// Paragraph 9.1: Frame tag (3 bytes).
	bits := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
	dec.frmHdr.KeyFrame = (bits & 1) == 0
	dec.frmHdr.Profile = uint8((bits >> 1) & 7)
	dec.frmHdr.Show = ((bits >> 4) & 1) != 0
	dec.frmHdr.PartitionLength = bits >> 5

	if dec.frmHdr.Profile > 3 {
		return fmt.Errorf("vp8: bad profile %d", dec.frmHdr.Profile)
	}
	if !dec.frmHdr.Show {
		return fmt.Errorf("vp8: frame not displayable")
	}
	if !dec.frmHdr.KeyFrame {
		return fmt.Errorf("vp8: not a keyframe")
	}

	buf := data[3:]

	// Paragraph 9.2: Picture header (7 bytes for keyframe).
	if len(buf) < 7 {
		return fmt.Errorf("vp8: truncated picture header")
	}
	// VP8 signature: 0x9D 0x01 0x2A
	if buf[0] != 0x9d || buf[1] != 0x01 || buf[2] != 0x2a {
		return fmt.Errorf("vp8: bad signature")
	}
	dec.picHdr.Width = int(binary.LittleEndian.Uint16(buf[3:5])) & 0x3FFF
	dec.picHdr.XScale = buf[4] >> 6
	dec.picHdr.Height = int(binary.LittleEndian.Uint16(buf[5:7])) & 0x3FFF
	dec.picHdr.YScale = buf[6] >> 6
	buf = buf[7:]

	if dec.picHdr.Width == 0 || dec.picHdr.Height == 0 {
		return fmt.Errorf("vp8: zero dimensions")
	}

	dec.mbW = (dec.picHdr.Width + 15) >> 4
	dec.mbH = (dec.picHdr.Height + 15) >> 4

	// Initialize probabilities and segment header.
	ResetProba(&dec.proba)
	dec.segHdr.AbsoluteDelta = true

	// Partition 0: modes/header partition.
	partLen := int(dec.frmHdr.PartitionLength)
	if partLen > len(buf) {
		return fmt.Errorf("vp8: bad partition length")
	}
	dec.br = bitio.NewBoolReader(buf[:partLen])
	tokenBuf := buf[partLen:]

	// Keyframe-specific header bits.
	dec.picHdr.Colorspace = uint8(dec.br.GetBit(0x80))
	dec.picHdr.ClampType = uint8(dec.br.GetBit(0x80))

	// Parse segment header (Paragraph 9.3).
	if err := dec.parseSegmentHeader(); err != nil {
		return err
	}

	// Parse filter header (Paragraph 9.4).
	dec.parseFilterHeader()

	// Parse token partitions (Paragraph 9.5).
	if err := dec.parsePartitions(tokenBuf); err != nil {
		return err
	}

	// Parse quantizer (Paragraph 9.6).
	ParseQuant(dec.br, &dec.segHdr, dec.dqm[:])

	// Skip 'update_proba' flag.
	dec.br.GetBit(0x80)

	// Parse coefficient probabilities (Paragraph 13).
	parseProba(dec.br, dec)

	return nil
}

// parseSegmentHeader reads segment info from partition 0.
func (dec *Decoder) parseSegmentHeader() error {
	br := dec.br
	hdr := &dec.segHdr

	hdr.UseSegment = br.GetBit(0x80) != 0
	if hdr.UseSegment {
		hdr.UpdateMap = br.GetBit(0x80) != 0
		if br.GetBit(0x80) != 0 { // update data
			hdr.AbsoluteDelta = br.GetBit(0x80) != 0
			for s := 0; s < NumMBSegments; s++ {
				if br.GetBit(0x80) != 0 {
					hdr.Quantizer[s] = int8(br.GetSignedValue(7))
				} else {
					hdr.Quantizer[s] = 0
				}
			}
			for s := 0; s < NumMBSegments; s++ {
				if br.GetBit(0x80) != 0 {
					hdr.FilterStrength[s] = int8(br.GetSignedValue(6))
				} else {
					hdr.FilterStrength[s] = 0
				}
			}
		}
		if hdr.UpdateMap {
			for s := 0; s < MBFeatureTreeProbs; s++ {
				if br.GetBit(0x80) != 0 {
					dec.proba.Segments[s] = uint8(br.GetValue(8))
				} else {
					dec.proba.Segments[s] = 255
				}
			}
		}
	} else {
		hdr.UpdateMap = false
	}

	if br.EOF() {
		return fmt.Errorf("vp8: premature EOF in segment header")
	}
	return nil
}

// parseFilterHeader reads filter parameters from partition 0.
func (dec *Decoder) parseFilterHeader() {
	br := dec.br
	hdr := &dec.filterHdr

	hdr.Simple = br.GetBit(0x80) != 0
	hdr.Level = int(br.GetValue(6))
	hdr.Sharpness = int(br.GetValue(3))
	hdr.UseLFDelta = br.GetBit(0x80) != 0
	if hdr.UseLFDelta {
		if br.GetBit(0x80) != 0 { // update lf-delta
			for i := 0; i < NumRefLFDeltas; i++ {
				if br.GetBit(0x80) != 0 {
					hdr.RefLFDelta[i] = int(br.GetSignedValue(6))
				}
			}
			for i := 0; i < NumModeLFDeltas; i++ {
				if br.GetBit(0x80) != 0 {
					hdr.ModeLFDelta[i] = int(br.GetSignedValue(6))
				}
			}
		}
	}

	if hdr.Level == 0 {
		dec.filterType = 0
	} else if hdr.Simple {
		dec.filterType = 1
	} else {
		dec.filterType = 2
	}
}

// parsePartitions sets up the token-partition bool readers.
func (dec *Decoder) parsePartitions(buf []byte) error {
	dec.numPartsMinusOne = (1 << dec.br.GetValue(2)) - 1
	lastPart := int(dec.numPartsMinusOne)

	if len(buf) < 3*lastPart {
		return fmt.Errorf("vp8: not enough data for partition sizes")
	}

	partStart := buf[lastPart*3:]
	sizeLeft := len(partStart)
	sz := buf

	for p := 0; p < lastPart; p++ {
		psize := int(sz[0]) | int(sz[1])<<8 | int(sz[2])<<16
		if psize > sizeLeft {
			return fmt.Errorf("vp8: partition %d size %d exceeds remaining data %d", p, psize, sizeLeft)
		}
		dec.parts[p] = bitio.NewBoolReader(partStart[:psize])
		partStart = partStart[psize:]
		sizeLeft -= psize
		sz = sz[3:]
	}
	dec.parts[lastPart] = bitio.NewBoolReader(partStart[:sizeLeft])

	// C reference (vp8_dec.c:249-250): the last partition is initialised even
	// when size_left is 0.  An empty (zero-length) partition is not an error at
	// parse time -- the BoolReader will simply report EOF immediately, and any
	// genuine truncation is caught later during macroblock decoding.
	return nil
}

// initFrame allocates (or reuses) all working memory for the decoder.
func (dec *Decoder) initFrame() error {
	mbW := dec.mbW

	// Reuse-or-grow typed slices.
	if cap(dec.yuvT) >= mbW {
		dec.yuvT = dec.yuvT[:mbW]
		clear(dec.yuvT)
	} else {
		dec.yuvT = make([]TopSamples, mbW)
	}
	if cap(dec.mbInfo) >= mbW+1 {
		dec.mbInfo = dec.mbInfo[:mbW+1]
		clear(dec.mbInfo)
	} else {
		dec.mbInfo = make([]MB, mbW+1)
	}
	if cap(dec.fInfo) >= mbW {
		dec.fInfo = dec.fInfo[:mbW]
		clear(dec.fInfo)
	} else {
		dec.fInfo = make([]FInfo, mbW)
	}
	if cap(dec.mbData) >= mbW {
		dec.mbData = dec.mbData[:mbW]
		clear(dec.mbData)
	} else {
		dec.mbData = make([]MBData, mbW)
	}

	// Output cache: single row of macroblocks.
	dec.cacheYStride = 16 * mbW
	dec.cacheUVStride = 8 * mbW

	totalRows := dec.mbH

	// Consolidate byte buffers into a single slab: intraT + yuvB + cacheY + cacheU + cacheV.
	intraTSize := 4 * mbW
	yuvBSize := YUVSize
	cacheYSize := totalRows * 16 * dec.cacheYStride
	cacheUSize := totalRows * 8 * dec.cacheUVStride
	cacheVSize := cacheUSize

	// Validate slab size won't overflow. Max dimensions: mbW=1024, mbH=1024
	// cacheYSize max = 1024 * 16 * 16384 = 268M, fits in int64.
	if uint64(totalRows)*16*uint64(dec.cacheYStride) > 1<<28 {
		return fmt.Errorf("vp8: frame too large")
	}

	slabSize := intraTSize + yuvBSize + cacheYSize + cacheUSize + cacheVSize

	// Reuse-or-grow the byte slab.
	if cap(dec.slab) >= slabSize {
		dec.slab = dec.slab[:slabSize]
		clear(dec.slab)
	} else {
		dec.slab = make([]byte, slabSize)
	}
	slab := dec.slab

	off := 0
	dec.intraT = slab[off : off+intraTSize]
	for i := range dec.intraT {
		dec.intraT[i] = BDCPred
	}
	off += intraTSize

	dec.yuvB = slab[off : off+yuvBSize]
	off += yuvBSize

	dec.cacheY = slab[off : off+cacheYSize]
	off += cacheYSize

	dec.cacheU = slab[off : off+cacheUSize]
	off += cacheUSize

	dec.cacheV = slab[off : off+cacheVSize]

	// Crop/filter bounds default to full image.
	dec.tlMBX = 0
	dec.tlMBY = 0
	dec.brMBX = dec.mbW
	dec.brMBY = dec.mbH

	return nil
}

// parseFrame is the main decode loop over all macroblock rows.
func (dec *Decoder) parseFrame() error {
	for dec.mbY = 0; dec.mbY < dec.brMBY; dec.mbY++ {
		tokenBR := dec.parts[dec.mbY&int(dec.numPartsMinusOne)]

		// Parse intra modes for this row.
		if err := dec.parseIntraModeRow(); err != nil {
			return err
		}

		// Decode macroblocks.
		for dec.mbX = 0; dec.mbX < dec.mbW; dec.mbX++ {
			if err := dec.decodeMB(tokenBR); err != nil {
				return err
			}
		}

		// Reset scanline state.
		dec.initScanline()

		// Reconstruct and filter the row.
		dec.reconstructRow()

		// Apply filtering.
		if dec.filterType > 0 {
			dec.filterRowAt(dec.mbY)
		}
	}
	return nil
}

// initScanline resets left-context at the start of a new macroblock row.
func (dec *Decoder) initScanline() {
	left := &dec.mbInfo[0] // left sentinel
	left.Nz = 0
	left.NzDC = 0
	for i := range dec.intraL {
		dec.intraL[i] = BDCPred
	}
	dec.mbX = 0
}

// kScan maps the 16 sub-4x4 block indices to their offsets in the BPS-strided
// reconstruction buffer (Y plane).
var kScan = [16]int{
	0 + 0*dsp.BPS, 4 + 0*dsp.BPS, 8 + 0*dsp.BPS, 12 + 0*dsp.BPS,
	0 + 4*dsp.BPS, 4 + 4*dsp.BPS, 8 + 4*dsp.BPS, 12 + 4*dsp.BPS,
	0 + 8*dsp.BPS, 4 + 8*dsp.BPS, 8 + 8*dsp.BPS, 12 + 8*dsp.BPS,
	0 + 12*dsp.BPS, 4 + 12*dsp.BPS, 8 + 12*dsp.BPS, 12 + 12*dsp.BPS,
}
