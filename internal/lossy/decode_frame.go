package lossy

import "github.com/deepteams/webp/internal/dsp"

// checkMode adjusts DC prediction mode for boundary macroblocks.
func checkMode(mbX, mbY, mode int) int {
	if mode == BDCPred {
		if mbX == 0 {
			if mbY == 0 {
				return BDCPredNoTopLeft
			}
			return BDCPredNoLeft
		}
		if mbY == 0 {
			return BDCPredNoTop
		}
	}
	return mode
}

// doTransform applies the appropriate inverse transform based on the 2-bit code.
func doTransform(bits uint32, src []int16, dst []byte) {
	switch bits >> 30 {
	case 3:
		dsp.Transform(src, dst, false)
	case 2:
		dsp.TransformAC3(src, dst)
	case 1:
		// Inline DC-only transform: avoids function-variable dispatch overhead.
		// All 16 pixels get the same DC add value.
		add := (int(src[0]) + 4) >> 3
		_ = dst[3+3*BPS] // BCE hint
		for j := 0; j < 4; j++ {
			off := j * BPS
			dst[off+0] = dsp.Clip8b(int(dst[off+0]) + add)
			dst[off+1] = dsp.Clip8b(int(dst[off+1]) + add)
			dst[off+2] = dsp.Clip8b(int(dst[off+2]) + add)
			dst[off+3] = dsp.Clip8b(int(dst[off+3]) + add)
		}
	default:
		// code == 0: no coefficients, nothing to do.
	}
}

// doUVTransform applies UV inverse transforms based on the non-zero bits.
func doUVTransform(bits uint32, src []int16, dst []byte) {
	if bits&0xff != 0 {
		if bits&0xaa != 0 {
			dsp.TransformUV(src, dst)
		} else {
			// Inline DC-only UV transform for all 4 chroma blocks.
			if src[0] != 0 {
				doTransformDCBlock(src[0:], dst[0:])
			}
			if src[16] != 0 {
				doTransformDCBlock(src[16:], dst[4:])
			}
			if src[32] != 0 {
				doTransformDCBlock(src[32:], dst[4*BPS:])
			}
			if src[48] != 0 {
				doTransformDCBlock(src[48:], dst[4*BPS+4:])
			}
		}
	}
}

// doTransformDCBlock applies an inlined DC-only 4x4 inverse transform.
func doTransformDCBlock(src []int16, dst []byte) {
	add := (int(src[0]) + 4) >> 3
	_ = dst[3+3*BPS] // BCE hint
	for j := 0; j < 4; j++ {
		off := j * BPS
		dst[off+0] = dsp.Clip8b(int(dst[off+0]) + add)
		dst[off+1] = dsp.Clip8b(int(dst[off+1]) + add)
		dst[off+2] = dsp.Clip8b(int(dst[off+2]) + add)
		dst[off+3] = dsp.Clip8b(int(dst[off+3]) + add)
	}
}

// reconstructRow reconstructs all macroblocks in the current row.
// Uses base-offset approach since Go does not support negative slice indices.
func (dec *Decoder) reconstructRow() {
	mbY := dec.mbY
	bps := BPS
	buf := dec.yuvB
	yBase := YOff
	uBase := UOff
	vBase := VOff

	// Initialize left-most column border pixels.
	for j := 0; j < 16; j++ {
		buf[yBase+j*bps-1] = 129
	}
	for j := 0; j < 8; j++ {
		buf[uBase+j*bps-1] = 129
		buf[vBase+j*bps-1] = 129
	}

	// Init top-left corner.
	if mbY > 0 {
		buf[yBase-1-bps] = 129
		buf[uBase-1-bps] = 129
		buf[vBase-1-bps] = 129
	} else {
		fillBytes(buf[yBase-bps-1:], 127, 16+4+1)
		fillBytes(buf[uBase-bps-1:], 127, 8+1)
		fillBytes(buf[vBase-bps-1:], 127, 8+1)
	}

	for mbX := 0; mbX < dec.mbW; mbX++ {
		block := &dec.mbData[mbX]

		// Slices pointing into the buffer at the current offset.
		yDst := buf[yBase:]
		uDst := buf[uBase:]
		vDst := buf[vBase:]

		// Rotate left samples from the previous block.
		if mbX > 0 {
			for j := -1; j < 16; j++ {
				copy(buf[yBase+j*bps-4:yBase+j*bps], buf[yBase+j*bps+12:yBase+j*bps+16])
			}
			for j := -1; j < 8; j++ {
				copy(buf[uBase+j*bps-4:uBase+j*bps], buf[uBase+j*bps+4:uBase+j*bps+8])
				copy(buf[vBase+j*bps-4:vBase+j*bps], buf[vBase+j*bps+4:vBase+j*bps+8])
			}
		}

		// Bring top samples into the cache.
		topYUV := &dec.yuvT[mbX]
		coeffs := block.Coeffs[:]
		bits := block.NonZeroY

		if mbY > 0 {
			copy(buf[yBase-bps:], topYUV.Y[:])
			copy(buf[uBase-bps:], topYUV.U[:])
			copy(buf[vBase-bps:], topYUV.V[:])
		}

		// Predict and add residuals.
		if block.IsI4x4 {
			// 4x4 prediction.
			topRight := buf[yBase-bps+16:]

			if mbY > 0 {
				if mbX >= dec.mbW-1 {
					// On rightmost border: replicate last top pixel.
					fillBytes(topRight, topYUV.Y[15], 4)
				} else {
					copy(topRight[:4], dec.yuvT[mbX+1].Y[:4])
				}
			}
			// Replicate top-right below for each sub-block row.
			// C uses uint32_t* with index BPS, so stride = BPS * sizeof(uint32_t) = BPS*4 bytes.
			// This places the replicated values at rows 3, 7, 11 (one row above each sub-block row).
			for r := 1; r <= 3; r++ {
				off := r * 4 * bps
				copy(topRight[off:off+4], topRight[:4])
			}

			for n := 0; n < 16; n++ {
				blockOff := yBase + kScan[n]
				dsp.PredLuma4Direct(int(block.IModes[n]), buf, blockOff)
				doTransform(bits, coeffs[n*16:], buf[blockOff:])
				bits <<= 2
			}
		} else {
			// 16x16 prediction.
			predFunc := checkMode(mbX, mbY, int(block.IModes[0]))
			dsp.PredLuma16[predFunc](buf, yBase)
			if bits != 0 {
				for n := 0; n < 16; n++ {
					doTransform(bits, coeffs[n*16:], buf[yBase+kScan[n]:])
					bits <<= 2
				}
			}
		}

		// Chroma prediction and transform.
		bitsUV := block.NonZeroUV
		predFunc := checkMode(mbX, mbY, int(block.UVMode))
		dsp.PredChroma8[predFunc](buf, uBase)
		dsp.PredChroma8[predFunc](buf, vBase)
		doUVTransform(bitsUV>>0, coeffs[16*16:], uDst)
		doUVTransform(bitsUV>>8, coeffs[20*16:], vDst)

		// Stash top samples for the next row.
		if mbY < dec.mbH-1 {
			copy(topYUV.Y[:], yDst[15*bps:15*bps+16])
			copy(topYUV.U[:], uDst[7*bps:7*bps+8])
			copy(topYUV.V[:], vDst[7*bps:7*bps+8])
		}

		// Transfer reconstructed samples to the output cache.
		yOffset := mbY * 16 * dec.cacheYStride
		uvOffset := mbY * 8 * dec.cacheUVStride
		yOut := dec.cacheY[mbX*16+yOffset:]
		uOut := dec.cacheU[mbX*8+uvOffset:]
		vOut := dec.cacheV[mbX*8+uvOffset:]
		for j := 0; j < 16; j++ {
			copy(yOut[j*dec.cacheYStride:j*dec.cacheYStride+16], yDst[j*bps:j*bps+16])
		}
		for j := 0; j < 8; j++ {
			copy(uOut[j*dec.cacheUVStride:j*dec.cacheUVStride+8], uDst[j*bps:j*bps+8])
			copy(vOut[j*dec.cacheUVStride:j*dec.cacheUVStride+8], vDst[j*bps:j*bps+8])
		}
	}
}

// precomputeFilterStrengths computes per-segment, per-mode filter levels.
func (dec *Decoder) precomputeFilterStrengths() {
	if dec.filterType <= 0 {
		return
	}
	hdr := &dec.filterHdr
	for s := 0; s < NumMBSegments; s++ {
		var baseLevel int
		if dec.segHdr.UseSegment {
			baseLevel = int(dec.segHdr.FilterStrength[s])
			if !dec.segHdr.AbsoluteDelta {
				baseLevel += hdr.Level
			}
		} else {
			baseLevel = hdr.Level
		}

		for i4x4 := 0; i4x4 <= 1; i4x4++ {
			info := &dec.fstrengths[s][i4x4]
			level := baseLevel
			if hdr.UseLFDelta {
				level += hdr.RefLFDelta[0]
				if i4x4 != 0 {
					level += hdr.ModeLFDelta[0]
				}
			}
			if level < 0 {
				level = 0
			} else if level > 63 {
				level = 63
			}
			if level > 0 {
				ilevel := level
				if hdr.Sharpness > 0 {
					if hdr.Sharpness > 4 {
						ilevel >>= 2
					} else {
						ilevel >>= 1
					}
					if ilevel > 9-hdr.Sharpness {
						ilevel = 9 - hdr.Sharpness
					}
				}
				if ilevel < 1 {
					ilevel = 1
				}
				info.FILevel = uint8(ilevel)
				info.FLimit = uint8(2*level + ilevel)
				if level >= 40 {
					info.HevThresh = 2
				} else if level >= 15 {
					info.HevThresh = 1
				} else {
					info.HevThresh = 0
				}
			} else {
				info.FLimit = 0
			}
			info.FInner = i4x4 != 0
		}
	}
}

// filterRow applies the loop filter to the current macroblock row.
func (dec *Decoder) filterRow() {
	for mbX := dec.tlMBX; mbX < dec.brMBX; mbX++ {
		dec.doFilter(mbX, dec.mbY)
	}
}

// doFilter applies the loop filter to a single macroblock.
// Uses base-offset approach: passes the full cache buffer plus an offset
// so that negative-context access (e.g. p[off-3*bps]) always resolves to
// a valid positive index.
func (dec *Decoder) doFilter(mbX, mbY int) {
	finfo := &dec.fInfo[mbX]
	limit := int(finfo.FLimit)
	if limit == 0 {
		return
	}
	ilevel := int(finfo.FILevel)
	yBPS := dec.cacheYStride
	yOff := mbY*16*yBPS + mbX*16

	if dec.filterType == 1 {
		// Simple filter (luma only).
		if mbX > 0 {
			simpleHFilter16At(dec.cacheY, yOff, yBPS, limit+4)
		}
		if finfo.FInner {
			simpleHFilter16iAt(dec.cacheY, yOff, yBPS, limit)
		}
		if mbY > 0 {
			simpleVFilter16At(dec.cacheY, yOff, yBPS, limit+4)
		}
		if finfo.FInner {
			simpleVFilter16iAt(dec.cacheY, yOff, yBPS, limit)
		}
	} else {
		// Complex filter (luma + chroma).
		uvBPS := dec.cacheUVStride
		uvOff := mbY*8*uvBPS + mbX*8
		hevT := int(finfo.HevThresh)

		if mbX > 0 {
			filterLoop26At(dec.cacheY, yOff, yBPS, 16, limit+4, ilevel, hevT)
			filterLoop26HAt(dec.cacheU, uvOff, uvBPS, 8, limit+4, ilevel, hevT)
			filterLoop26HAt(dec.cacheV, uvOff, uvBPS, 8, limit+4, ilevel, hevT)
		}
		if finfo.FInner {
			hFilter16iAt(dec.cacheY, yOff, yBPS, limit, ilevel, hevT)
			hFilter8iAt(dec.cacheU, dec.cacheV, uvOff, uvBPS, limit, ilevel, hevT)
		}
		if mbY > 0 {
			filterLoop26VAt(dec.cacheY, yOff, yBPS, 16, limit+4, ilevel, hevT)
			filterLoop26VAt(dec.cacheU, uvOff, uvBPS, 8, limit+4, ilevel, hevT)
			filterLoop26VAt(dec.cacheV, uvOff, uvBPS, 8, limit+4, ilevel, hevT)
		}
		if finfo.FInner {
			vFilter16iAt(dec.cacheY, yOff, yBPS, limit, ilevel, hevT)
			vFilter8iAt(dec.cacheU, dec.cacheV, uvOff, uvBPS, limit, ilevel, hevT)
		}
	}
}

// fillBytes fills n bytes at dst with value v.
func fillBytes(dst []byte, v byte, n int) {
	for i := 0; i < n; i++ {
		dst[i] = v
	}
}

// ---------------------------------------------------------------------------
// Loop filter primitives.
//
// All filter functions take the FULL buffer plus an explicit base offset so
// that "negative-context" access (e.g. p[off-3*bps]) always resolves to a
// valid non-negative index within the buffer.
// ---------------------------------------------------------------------------

// simpleVFilter16At applies the simple 2-tap vertical filter at base offset.
// Matches C SimpleVFilter16_C: uses thresh2 = 2*thresh+1, NeedsFilter, DoFilter2.
func simpleVFilter16At(p []byte, base, bps, thresh int) {
	thresh2 := 2*thresh + 1
	for i := 0; i < 16; i++ {
		off := base + i
		p1 := int(p[off-2*bps])
		p0 := int(p[off-bps])
		q0 := int(p[off])
		q1 := int(p[off+bps])
		if 4*abs(p0-q0)+abs(p1-q1) <= thresh2 {
			a := 3*(q0-p0) + sclip1(p1-q1)
			a1 := sclip2((a + 4) >> 3)
			a2 := sclip2((a + 3) >> 3)
			p[off-bps] = clamp255(p0 + a2)
			p[off] = clamp255(q0 - a1)
		}
	}
}

// simpleHFilter16At applies the simple 2-tap horizontal filter at base offset.
func simpleHFilter16At(p []byte, base, bps, thresh int) {
	thresh2 := 2*thresh + 1
	for j := 0; j < 16; j++ {
		off := base + j*bps
		p1 := int(p[off-2])
		p0 := int(p[off-1])
		q0 := int(p[off])
		q1 := int(p[off+1])
		if 4*abs(p0-q0)+abs(p1-q1) <= thresh2 {
			a := 3*(q0-p0) + sclip1(p1-q1)
			a1 := sclip2((a + 4) >> 3)
			a2 := sclip2((a + 3) >> 3)
			p[off-1] = clamp255(p0 + a2)
			p[off] = clamp255(q0 - a1)
		}
	}
}

// simpleVFilter16iAt applies the simple inner vertical filters at base offset.
func simpleVFilter16iAt(p []byte, base, bps, thresh int) {
	for k := 1; k <= 3; k++ {
		simpleVFilter16At(p, base+k*4*bps, bps, thresh)
	}
}

// simpleHFilter16iAt applies the simple inner horizontal filters at base offset.
func simpleHFilter16iAt(p []byte, base, bps, thresh int) {
	for k := 1; k <= 3; k++ {
		simpleHFilter16At(p, base+k*4, bps, thresh)
	}
}

// filterLoop26VAt applies FilterLoop26 (macroblock edge) vertical filter at base offset.
// HEV -> doSimpleFilter2, !HEV -> doSimpleFilter6.
func filterLoop26VAt(p []byte, base, bps, width, thresh, ithresh, hevThresh int) {
	thresh2 := 2*thresh + 1
	for i := 0; i < width; i++ {
		off := base + i
		if !needsFilter2At(p, off, bps, thresh2, ithresh) {
			continue
		}
		if isHEV(p[off-2*bps], p[off-bps], p[off], p[off+bps], hevThresh) {
			doSimpleFilter2(p, off, bps)
		} else {
			doSimpleFilter6(p, off, bps)
		}
	}
}

// filterLoop26HAt applies FilterLoop26 (macroblock edge) horizontal filter at base offset.
func filterLoop26At(p []byte, base, bps, height, thresh, ithresh, hevThresh int) {
	thresh2 := 2*thresh + 1
	for j := 0; j < height; j++ {
		off := base + j*bps
		if !needsFilter2At(p, off, 1, thresh2, ithresh) {
			continue
		}
		if isHEV(p[off-2], p[off-1], p[off], p[off+1], hevThresh) {
			doSimpleFilter2(p, off, 1)
		} else {
			doSimpleFilter6(p, off, 1)
		}
	}
}

// filterLoop26HAt is an alias for horizontal macroblock edge filter.
func filterLoop26HAt(p []byte, base, bps, height, thresh, ithresh, hevThresh int) {
	filterLoop26At(p, base, bps, height, thresh, ithresh, hevThresh)
}

// filterLoop24VAt applies FilterLoop24 (inner edge) vertical filter at base offset.
// HEV -> doSimpleFilter2, !HEV -> doSimpleFilter4.
func filterLoop24VAt(p []byte, base, bps, width, thresh, ithresh, hevThresh int) {
	thresh2 := 2*thresh + 1
	for i := 0; i < width; i++ {
		off := base + i
		if !needsFilter2At(p, off, bps, thresh2, ithresh) {
			continue
		}
		if isHEV(p[off-2*bps], p[off-bps], p[off], p[off+bps], hevThresh) {
			doSimpleFilter2(p, off, bps)
		} else {
			doSimpleFilter4(p, off, bps)
		}
	}
}

// filterLoop24HAt applies FilterLoop24 (inner edge) horizontal filter at base offset.
func filterLoop24HAt(p []byte, base, bps, height, thresh, ithresh, hevThresh int) {
	thresh2 := 2*thresh + 1
	for j := 0; j < height; j++ {
		off := base + j*bps
		if !needsFilter2At(p, off, 1, thresh2, ithresh) {
			continue
		}
		if isHEV(p[off-2], p[off-1], p[off], p[off+1], hevThresh) {
			doSimpleFilter2(p, off, 1)
		} else {
			doSimpleFilter4(p, off, 1)
		}
	}
}

// vFilter16iAt applies inner vertical complex filters at base offset.
func vFilter16iAt(p []byte, base, bps, thresh, ithresh, hevThresh int) {
	for k := 1; k <= 3; k++ {
		filterLoop24VAt(p, base+k*4*bps, bps, 16, thresh, ithresh, hevThresh)
	}
}

// hFilter16iAt applies inner horizontal complex filters at base offset.
func hFilter16iAt(p []byte, base, bps, thresh, ithresh, hevThresh int) {
	for k := 1; k <= 3; k++ {
		filterLoop24HAt(p, base+k*4, bps, 16, thresh, ithresh, hevThresh)
	}
}

// vFilter8iAt applies inner vertical complex UV filters at base offset.
func vFilter8iAt(u, v []byte, base, bps, thresh, ithresh, hevThresh int) {
	filterLoop24VAt(u, base+4*bps, bps, 8, thresh, ithresh, hevThresh)
	filterLoop24VAt(v, base+4*bps, bps, 8, thresh, ithresh, hevThresh)
}

// hFilter8iAt applies inner horizontal complex UV filters at base offset.
func hFilter8iAt(u, v []byte, base, bps, thresh, ithresh, hevThresh int) {
	filterLoop24HAt(u, base+4, bps, 8, thresh, ithresh, hevThresh)
	filterLoop24HAt(v, base+4, bps, 8, thresh, ithresh, hevThresh)
}

// needsFilter2At checks if an edge at the given offset needs complex filtering.
// Matches C NeedsFilter2_C: checks all 8 pixels p3..p0, q0..q3.
func needsFilter2At(p []byte, off, step, thresh, ithresh int) bool {
	p3 := int(p[off-4*step])
	p2 := int(p[off-3*step])
	p1 := int(p[off-2*step])
	p0 := int(p[off-step])
	q0 := int(p[off])
	q1 := int(p[off+step])
	q2 := int(p[off+2*step])
	q3 := int(p[off+3*step])
	if 4*abs(p0-q0)+abs(p1-q1) > thresh {
		return false
	}
	return abs(p3-p2) <= ithresh &&
		abs(p2-p1) <= ithresh &&
		abs(p1-p0) <= ithresh &&
		abs(q3-q2) <= ithresh &&
		abs(q2-q1) <= ithresh &&
		abs(q1-q0) <= ithresh
}

// isHEV returns true if there's high edge variance.
func isHEV(p1, p0, q0, q1 byte, thresh int) bool {
	return abs(int(p1)-int(p0)) > thresh || abs(int(q0)-int(q1)) > thresh
}

// doSimpleFilter2 applies the 2-tap filter (DoFilter2_C).
// a = 3*(q0-p0) + sclip1(p1-q1), updates p0 and q0 only.
func doSimpleFilter2(p []byte, off, step int) {
	p1 := int(p[off-2*step])
	p0 := int(p[off-step])
	q0 := int(p[off])
	q1 := int(p[off+step])
	a := 3*(q0-p0) + sclip1(p1-q1)
	a1 := sclip2((a + 4) >> 3)
	a2 := sclip2((a + 3) >> 3)
	p[off-step] = clamp255(p0 + a2)
	p[off] = clamp255(q0 - a1)
}

// doSimpleFilter4 applies the 4-tap filter (DoFilter4_C).
// a = 3*(q0-p0) (NO p1-q1 term), updates p1, p0, q0, q1.
func doSimpleFilter4(p []byte, off, step int) {
	p1 := int(p[off-2*step])
	p0 := int(p[off-step])
	q0 := int(p[off])
	q1 := int(p[off+step])
	a := 3 * (q0 - p0)
	a1 := sclip2((a + 4) >> 3)
	a2 := sclip2((a + 3) >> 3)
	a3 := (a1 + 1) >> 1
	p[off-2*step] = clamp255(p1 + a3)
	p[off-step] = clamp255(p0 + a2)
	p[off] = clamp255(q0 - a1)
	p[off+step] = clamp255(q1 - a3)
}

// doSimpleFilter6 applies the 6-tap filter (DoFilter6_C).
func doSimpleFilter6(p []byte, off, step int) {
	p2 := int(p[off-3*step])
	p1 := int(p[off-2*step])
	p0 := int(p[off-step])
	q0 := int(p[off])
	q1 := int(p[off+step])
	q2 := int(p[off+2*step])
	a := sclip1(3*(q0-p0) + sclip1(p1-q1))
	a1 := (27*a + 63) >> 7
	a2 := (18*a + 63) >> 7
	a3 := (9*a + 63) >> 7
	p[off-3*step] = clamp255(p2 + a3)
	p[off-2*step] = clamp255(p1 + a2)
	p[off-step] = clamp255(p0 + a1)
	p[off] = clamp255(q0 - a1)
	p[off+step] = clamp255(q1 - a2)
	p[off+2*step] = clamp255(q2 - a3)
}

// Helper math for filters — table-lookup implementations.
// These replace branching versions with single array accesses
// from the precomputed clip tables in dsp/cliptables.go.

func abs(x int) int {
	return int(dsp.Kabs0(x))
}

func sclip1(v int) int {
	return int(dsp.Ksclip1(v))
}

func sclip2(v int) int {
	return int(dsp.Ksclip2(v))
}

func clamp255(v int) byte {
	return dsp.Kclip1(v)
}
