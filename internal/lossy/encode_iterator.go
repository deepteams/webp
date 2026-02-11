package lossy

import "github.com/deepteams/webp/internal/dsp"

// MBIterator provides raster-scan iteration over macroblocks for encoding.
// It manages import/export of YUV data to/from the BPS-strided working buffers,
// and maintains left/top context for intra prediction.
type MBIterator struct {
	enc *VP8Encoder

	// Current macroblock position.
	X, Y int
	// Linear macroblock index.
	MBIdx int

	// Left/top prediction context.
	leftY   [16]uint8 // left Y column (16 pixels)
	leftU   [8]uint8  // left U column
	leftV   [8]uint8  // left V column
	topY    []uint8   // top Y row (mbW * 16 pixels), shared across row
	topU    []uint8   // top U row (mbW * 8)
	topV    []uint8   // top V row (mbW * 8)

	// Top-left corner sample for each channel.
	topLeftY uint8
	topLeftU uint8
	topLeftV uint8

	// I4x4 top/left modes for the current MB.
	topModes  []uint8 // 4 * mbW, persistent across row
	leftModes [4]uint8

	// NZ (non-zero) context.
	leftNZ uint32 // left non-zero bits (packed: bits 0-3=Y, 4-5=U, 6-7=V)
	topNZ  []uint32 // per-column top non-zero bits
}

// InitIterator resets the iterator to the first macroblock.
func (enc *VP8Encoder) InitIterator() {
	it := &enc.mbIterator
	it.enc = enc
	it.X = 0
	it.Y = 0
	it.MBIdx = 0

	// Use pre-allocated context arrays from the encoder.
	it.topY = enc.itTopY
	it.topU = enc.itTopU
	it.topV = enc.itTopV
	it.topModes = enc.itTopModes
	it.topNZ = enc.itTopNZ

	// Initialize top context to 127 (DC prediction neutral) and modes to DC.
	for i := range it.topY {
		it.topY[i] = 127
	}
	for i := range it.topU {
		it.topU[i] = 127
	}
	for i := range it.topV {
		it.topV[i] = 127
	}
	for i := range it.topModes {
		it.topModes[i] = BDCPred
	}

	it.resetLeftContext()
}

// resetLeftContext initializes left-column context at the start of a row.
func (it *MBIterator) resetLeftContext() {
	for i := range it.leftY {
		it.leftY[i] = 129
	}
	for i := range it.leftU {
		it.leftU[i] = 129
	}
	for i := range it.leftV {
		it.leftV[i] = 129
	}
	for i := range it.leftModes {
		it.leftModes[i] = BDCPred
	}
	it.leftNZ = 0
	it.topLeftY = 127
	it.topLeftU = 127
	it.topLeftV = 127
}

// IsDone reports whether all macroblocks have been visited.
func (it *MBIterator) IsDone() bool {
	return it.Y >= it.enc.mbH
}

// Next advances to the next macroblock in raster-scan order.
// Returns false when all macroblocks have been visited.
func (it *MBIterator) Next() bool {
	it.X++
	it.MBIdx++
	if it.X >= it.enc.mbW {
		it.X = 0
		it.Y++
		if it.Y >= it.enc.mbH {
			return false
		}
		it.resetLeftContext()
	}
	return true
}

// Import copies the source YUV data for the current macroblock into the
// BPS-strided yuvIn buffer. For boundary macroblocks, pixels beyond the image
// edge are replicated from the last valid pixel, matching C libwebp's
// ImportBlock (iterator_enc.c:111-126).
func (it *MBIterator) Import(enc *VP8Encoder) {
	x := it.X * 16
	y := it.Y * 16

	// Effective width/height within this macroblock.
	w := enc.width - x
	if w > 16 {
		w = 16
	}
	h := enc.height - y
	if h > 16 {
		h = 16
	}

	// Copy Y with edge replication.
	importBlock(enc.yPlane, enc.yStride, enc.yuvIn, YOff, x, y, w, h, 16)

	// Copy U and V with edge replication.
	ux := it.X * 8
	uy := it.Y * 8
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	importBlock(enc.uPlane, enc.uvStride, enc.yuvIn, UOff, ux, uy, uvW, uvH, 8)
	importBlock(enc.vPlane, enc.uvStride, enc.yuvIn, VOff, ux, uy, uvW, uvH, 8)
}

// importBlock copies a w x h region from src (with srcStride) into dst at
// dstOff (BPS-strided), then replicates edges to fill the full size x size
// block. This matches C libwebp's ImportBlock: columns beyond w are filled
// with the last valid pixel, rows beyond h are copies of the last valid row.
func importBlock(src []byte, srcStride int, dst []byte, dstOff, srcX, srcY, w, h, size int) {
	// Fast path: no edge replication needed (common case for interior MBs).
	if w >= size && h >= size {
		for j := 0; j < size; j++ {
			srcRow := (srcY+j)*srcStride + srcX
			dstRow := dstOff + j*dsp.BPS
			copy(dst[dstRow:dstRow+size], src[srcRow:srcRow+size])
		}
		return
	}

	for j := 0; j < h; j++ {
		srcRow := (srcY+j)*srcStride + srcX
		dstRow := dstOff + j*dsp.BPS
		// Copy valid pixels.
		copy(dst[dstRow:dstRow+w], src[srcRow:srcRow+w])
		// Replicate last valid pixel to fill remaining columns.
		if w < size {
			last := dst[dstRow+w-1]
			for i := w; i < size; i++ {
				dst[dstRow+i] = last
			}
		}
	}
	// Replicate last valid row to fill remaining rows.
	for j := h; j < size; j++ {
		lastRow := dstOff + (h-1)*dsp.BPS
		dstRow := dstOff + j*dsp.BPS
		copy(dst[dstRow:dstRow+size], dst[lastRow:lastRow+size])
	}
}

// Export writes the reconstructed macroblock from yuvOut back to the YUV planes
// and updates the left/top context.
// When enc.skipExportPlanes is true, only updates left/top context without
// writing back to the Y/U/V planes (used by statLoop to preserve source pixels).
func (it *MBIterator) Export(enc *VP8Encoder) {
	if !enc.skipExportPlanes {
		x := it.X * 16
		y := it.Y * 16

		// Fast path: interior MBs (no boundary clipping needed).
		// This is the common case for all but the right/bottom edge MBs.
		wY := 16
		hY := 16
		if x+16 > enc.width {
			wY = enc.width - x
		}
		if y+16 > enc.height {
			hY = enc.height - y
		}

		// Write back Y using bulk copy.
		for j := 0; j < hY; j++ {
			srcOff := YOff + j*dsp.BPS
			dstOff := (y+j)*enc.yStride + x
			copy(enc.yPlane[dstOff:dstOff+wY], enc.yuvOut[srcOff:srcOff+wY])
		}

		// Write back U/V using bulk copy.
		ux := it.X * 8
		uy := it.Y * 8
		wUV := 8
		hUV := 8
		if ux+8 > enc.mbW*8 {
			wUV = enc.mbW*8 - ux
		}
		if uy+8 > enc.mbH*8 {
			hUV = enc.mbH*8 - uy
		}
		for j := 0; j < hUV; j++ {
			srcU := UOff + j*dsp.BPS
			srcV := VOff + j*dsp.BPS
			dstU := (uy+j)*enc.uvStride + ux
			dstV := (uy+j)*enc.uvStride + ux
			copy(enc.uPlane[dstU:dstU+wUV], enc.yuvOut[srcU:srcU+wUV])
			copy(enc.vPlane[dstV:dstV+wUV], enc.yuvOut[srcV:srcV+wUV])
		}
	}

	// Update top and left context.
	topOff := it.X * 16
	topUOff := it.X * 8

	// Save top-left BEFORE overwriting top arrays (must read old top[15]/top[7]).
	it.topLeftY = it.topY[topOff+15]
	it.topLeftU = it.topU[topUOff+7]
	it.topLeftV = it.topV[topUOff+7]

	// Update top context: save bottom row of this MB.
	for i := 0; i < 16; i++ {
		it.topY[topOff+i] = enc.yuvOut[YOff+15*dsp.BPS+i]
	}
	for i := 0; i < 8; i++ {
		it.topU[topUOff+i] = enc.yuvOut[UOff+7*dsp.BPS+i]
		it.topV[topUOff+i] = enc.yuvOut[VOff+7*dsp.BPS+i]
	}

	// Update left context: save rightmost column of this MB.
	for j := 0; j < 16; j++ {
		it.leftY[j] = enc.yuvOut[YOff+j*dsp.BPS+15]
	}
	for j := 0; j < 8; j++ {
		it.leftU[j] = enc.yuvOut[UOff+j*dsp.BPS+7]
		it.leftV[j] = enc.yuvOut[VOff+j*dsp.BPS+7]
	}
}

// FillPredictionContext fills the prediction border (yuvP buffer) from
// the left/top context for the current macroblock.
func (it *MBIterator) FillPredictionContext(enc *VP8Encoder) {
	it.FillPredContext(enc)
}

// FillPredContext fills the prediction context (top row, left column, top-left corner)
// in yuvOut for the Y, U, and V planes. The prediction functions use negative
// indexing relative to the block origin (e.g., dst[i-bps] for top, dst[-1+j*bps] for left).
func (it *MBIterator) FillPredContext(enc *VP8Encoder) {
	bps := dsp.BPS

	// --- Y plane context ---
	topOff := it.X * 16

	// Top row: yuvOut[YOff - BPS + i] for i in 0..15.
	for i := 0; i < 16; i++ {
		if it.Y > 0 {
			enc.yuvOut[YOff-bps+i] = it.topY[topOff+i]
		} else {
			enc.yuvOut[YOff-bps+i] = 127
		}
	}

	// Top-right extension for I4x4 VL/LD modes (4 pixels beyond column 15).
	// Must match decoder's handling in decode_frame.go lines 111-124.
	if it.Y > 0 {
		if it.X < it.enc.mbW-1 {
			// Next MB's first 4 top pixels.
			nextOff := (it.X + 1) * 16
			for i := 0; i < 4; i++ {
				enc.yuvOut[YOff-bps+16+i] = it.topY[nextOff+i]
			}
		} else {
			// Rightmost MB: replicate last top pixel.
			val := it.topY[topOff+15]
			for i := 0; i < 4; i++ {
				enc.yuvOut[YOff-bps+16+i] = val
			}
		}
	} else {
		// First row: 127 (matches decoder's fillBytes at line 73).
		for i := 0; i < 4; i++ {
			enc.yuvOut[YOff-bps+16+i] = 127
		}
	}

	// Replicate top-right for each sub-block row.
	// C uses uint32_t* with index BPS → stride = BPS*4 bytes, placing values at rows 3, 7, 11.
	for r := 1; r <= 3; r++ {
		off := r * 4 * bps
		for i := 0; i < 4; i++ {
			enc.yuvOut[YOff-bps+16+off+i] = enc.yuvOut[YOff-bps+16+i]
		}
	}

	// Top-left corner: yuvOut[YOff - BPS - 1].
	if it.X > 0 && it.Y > 0 {
		enc.yuvOut[YOff-bps-1] = it.topLeftY
	} else if it.Y > 0 {
		enc.yuvOut[YOff-bps-1] = 129
	} else {
		enc.yuvOut[YOff-bps-1] = 127
	}

	// Left column: yuvOut[YOff - 1 + j*BPS] for j in 0..15.
	for j := 0; j < 16; j++ {
		if it.X > 0 {
			enc.yuvOut[YOff-1+j*bps] = it.leftY[j]
		} else {
			enc.yuvOut[YOff-1+j*bps] = 129
		}
	}

	// --- U plane context ---
	topUOff := it.X * 8

	for i := 0; i < 8; i++ {
		if it.Y > 0 {
			enc.yuvOut[UOff-bps+i] = it.topU[topUOff+i]
		} else {
			enc.yuvOut[UOff-bps+i] = 127
		}
	}

	if it.X > 0 && it.Y > 0 {
		enc.yuvOut[UOff-bps-1] = it.topLeftU
	} else if it.Y > 0 {
		enc.yuvOut[UOff-bps-1] = 129
	} else {
		enc.yuvOut[UOff-bps-1] = 127
	}

	for j := 0; j < 8; j++ {
		if it.X > 0 {
			enc.yuvOut[UOff-1+j*bps] = it.leftU[j]
		} else {
			enc.yuvOut[UOff-1+j*bps] = 129
		}
	}

	// --- V plane context ---
	for i := 0; i < 8; i++ {
		if it.Y > 0 {
			enc.yuvOut[VOff-bps+i] = it.topV[topUOff+i]
		} else {
			enc.yuvOut[VOff-bps+i] = 127
		}
	}

	if it.X > 0 && it.Y > 0 {
		enc.yuvOut[VOff-bps-1] = it.topLeftV
	} else if it.Y > 0 {
		enc.yuvOut[VOff-bps-1] = 129
	} else {
		enc.yuvOut[VOff-bps-1] = 127
	}

	for j := 0; j < 8; j++ {
		if it.X > 0 {
			enc.yuvOut[VOff-1+j*bps] = it.leftV[j]
		} else {
			enc.yuvOut[VOff-1+j*bps] = 129
		}
	}
}

// SaveTopModes saves the I4x4 prediction modes for the current MB
// to the top context array.
func (it *MBIterator) SaveTopModes(modes [4]uint8) {
	off := it.X * 4
	copy(it.topModes[off:off+4], modes[:])
}

// GetTopModes returns the I4x4 modes from the MB above.
func (it *MBIterator) GetTopModes() [4]uint8 {
	var modes [4]uint8
	if it.Y > 0 {
		off := it.X * 4
		copy(modes[:], it.topModes[off:off+4])
	} else {
		for i := range modes {
			modes[i] = BDCPred
		}
	}
	return modes
}

// SetNZ stores the non-zero flags after encoding a macroblock.
func (it *MBIterator) SetNZ(nz uint32) {
	it.leftNZ = nz
	it.topNZ[it.X] = nz
}

// GetNZContext returns the combined left+top non-zero context for the
// coefficient at the given block index within the macroblock.
func (it *MBIterator) GetNZContext(blockIdx int) int {
	// Simplified: return 0 (no context) for now.
	leftBit := int(it.leftNZ>>uint(blockIdx)) & 1
	topBit := 0
	if it.Y > 0 || it.X > 0 {
		topBit = int(it.topNZ[it.X]>>uint(blockIdx)) & 1
	}
	return leftBit + topBit
}
