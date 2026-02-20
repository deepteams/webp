package lossy

import (
	"errors"

	"github.com/deepteams/webp/internal/bitio"
	"github.com/deepteams/webp/internal/dsp"
)

var errPrematureEOF = errors.New("vp8: premature end of data")

// kCat3456 groups the category extra-bit tables for values >= 5+3=8.
var kCat3456 = [4][]uint8{
	KCat3[:], KCat4[:], KCat5[:], KCat6[:],
}

// ---------------------------------------------------------------------------
// Manually-inlined coefficient decoding for performance.
//
// The VP8 boolean decoder's GetBit is the dominant cost (~57% of decode time)
// but Go cannot inline it (cost 152 > budget 80). By manually inlining the
// GetBit arithmetic and keeping BoolReader state in local registers, we
// eliminate per-call overhead and memory load/store traffic.
// ---------------------------------------------------------------------------

// kVP8Log2Range maps range values [0..127] to the number of left-shifts
// needed for normalisation: 7 - floor(log2(range)).
var kVP8Log2Range = [128]uint8{
	7, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4, 3, 3, 3, 3, 3, 3, 3,
	3, 3, 3, 3, 3, 3, 3, 3, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0,
}

// kVP8NewRange maps range values [0..127] to the normalised range after
// shifting: ((range + 1) << kVP8Log2Range[range]) - 1.
var kVP8NewRange = [128]uint8{
	127, 127, 191, 127, 159, 191, 223, 127, 143, 159, 175, 191, 207, 223, 239,
	127, 135, 143, 151, 159, 167, 175, 183, 191, 199, 207, 215, 223, 231, 239,
	247, 127, 131, 135, 139, 143, 147, 151, 155, 159, 163, 167, 171, 175, 179,
	183, 187, 191, 195, 199, 203, 207, 211, 215, 219, 223, 227, 231, 235, 239,
	243, 247, 251, 127, 129, 131, 133, 135, 137, 139, 141, 143, 145, 147, 149,
	151, 153, 155, 157, 159, 161, 163, 165, 167, 169, 171, 173, 175, 177, 179,
	181, 183, 185, 187, 189, 191, 193, 195, 197, 199, 201, 203, 205, 207, 209,
	211, 213, 215, 217, 219, 221, 223, 225, 227, 229, 231, 233, 235, 237, 239,
	241, 243, 245, 247, 249, 251, 253, 127,
}

// fastBit decodes one boolean symbol using LUT-based normalization (GetBitAlt).
// When brR > 0x7e (~50% of calls), normalization is skipped entirely.
// Designed to be inlined by the Go compiler.
func fastBit(prob uint8, brV uint64, brR uint32, brB int) (int, uint64, uint32, int) {
	split := (brR * uint32(prob)) >> 8
	val := uint32(brV >> uint(brB))
	var bit int
	if val > split {
		bit = 1
		brR -= split + 1
		brV -= uint64(split+1) << uint(brB)
	} else {
		brR = split
	}
	if brR <= 0x7e {
		brB -= int(kVP8Log2Range[brR])
		brR = uint32(kVP8NewRange[brR])
	}
	return bit, brV, brR, brB
}

// fastSigned decodes a sign bit (prob=0x80) and returns +v or -v.
// Pure arithmetic, designed to be inlined.
func fastSigned(v int, brV uint64, brR uint32, brB int) (int, uint64, uint32, int) {
	pos := brB
	split := brR >> 1
	val := uint32(brV >> uint(pos))
	mask := int32(split-val) >> 31
	brB--
	brR = (brR + uint32(mask)) | 1
	brV -= uint64((split + 1) & uint32(mask)) << uint(pos)
	return (v ^ int(mask)) - int(mask), brV, brR, brB
}

// brLoad syncs local state to BoolReader and loads more bytes.
// Called rarely (~1 in 56 GetBit calls). Must not be inlined to keep
// the fast path small.
//
//go:noinline
func brLoad(br *bitio.BoolReader, brV uint64, brB int) (uint64, int) {
	br.Value = brV
	br.Bits = brB
	br.LoadNewBytes()
	return br.Value, br.Bits
}

// brSync writes the local state back to the BoolReader.
func brSync(br *bitio.BoolReader, brV uint64, brR uint32, brB int) {
	br.Value = brV
	br.Range = brR
	br.Bits = brB
}

// getCoeffsInline is the hot-path version of getCoeffs with manually-inlined
// GetBit/GetSigned operations. It keeps BoolReader state in local variables
// to minimize memory traffic.
func getCoeffsInline(br *bitio.BoolReader, bands *[17]*BandProbas, ctx int, dq0, dq1 int, n int, out []int16) int {
	// Hoist BoolReader hot state into locals for register residency.
	brV := br.Value
	brR := br.Range
	brB := br.Bits

	p := bands[n].Probas[ctx][:]
	for ; n < 16; n++ {
		// Inline: if br.GetBit(p[0]) == 0 { return n }
		if brB < 0 {
			brV, brB = brLoad(br, brV, brB)
		}
		var bit int
		bit, brV, brR, brB = fastBit(p[0], brV, brR, brB)
		if bit == 0 {
			brSync(br, brV, brR, brB)
			return n
		}

		// Inline: for br.GetBit(p[1]) == 0 { ... zero run ... }
		for {
			if brB < 0 {
				brV, brB = brLoad(br, brV, brB)
			}
			bit, brV, brR, brB = fastBit(p[1], brV, brR, brB)
			if bit != 0 {
				break
			}
			n++
			p = bands[n].Probas[0][:]
			if n == 16 {
				brSync(br, brV, brR, brB)
				return 16
			}
		}

		// Inline: if br.GetBit(p[2]) == 0 { v=1 } else { v=getLargeValue }
		if brB < 0 {
			brV, brB = brLoad(br, brV, brB)
		}
		pCtx := &bands[n+1].Probas
		var v int
		bit, brV, brR, brB = fastBit(p[2], brV, brR, brB)
		if bit == 0 {
			v = 1
			p = pCtx[1][:]
		} else {
			// Inline getLargeValue.
			if brB < 0 {
				brV, brB = brLoad(br, brV, brB)
			}
			bit, brV, brR, brB = fastBit(p[3], brV, brR, brB)
			if bit == 0 {
				if brB < 0 {
					brV, brB = brLoad(br, brV, brB)
				}
				bit, brV, brR, brB = fastBit(p[4], brV, brR, brB)
				if bit == 0 {
					v = 2
				} else {
					if brB < 0 {
						brV, brB = brLoad(br, brV, brB)
					}
					bit, brV, brR, brB = fastBit(p[5], brV, brR, brB)
					v = 3 + bit
				}
			} else {
				if brB < 0 {
					brV, brB = brLoad(br, brV, brB)
				}
				bit, brV, brR, brB = fastBit(p[6], brV, brR, brB)
				if bit == 0 {
					if brB < 0 {
						brV, brB = brLoad(br, brV, brB)
					}
					bit, brV, brR, brB = fastBit(p[7], brV, brR, brB)
					if bit == 0 {
						if brB < 0 {
							brV, brB = brLoad(br, brV, brB)
						}
						bit, brV, brR, brB = fastBit(159, brV, brR, brB)
						v = 5 + bit
					} else {
						if brB < 0 {
							brV, brB = brLoad(br, brV, brB)
						}
						bit, brV, brR, brB = fastBit(165, brV, brR, brB)
						v = 7 + 2*bit
						if brB < 0 {
							brV, brB = brLoad(br, brV, brB)
						}
						bit, brV, brR, brB = fastBit(145, brV, brR, brB)
						v += bit
					}
				} else {
					if brB < 0 {
						brV, brB = brLoad(br, brV, brB)
					}
					var bit1 int
					bit1, brV, brR, brB = fastBit(p[8], brV, brR, brB)
					if brB < 0 {
						brV, brB = brLoad(br, brV, brB)
					}
					var bit0 int
					bit0, brV, brR, brB = fastBit(p[9+bit1], brV, brR, brB)
					cat := 2*bit1 + bit0
					v = 0
					for _, tabProb := range kCat3456[cat] {
						if tabProb == 0 {
							break
						}
						if brB < 0 {
							brV, brB = brLoad(br, brV, brB)
						}
						bit, brV, brR, brB = fastBit(tabProb, brV, brR, brB)
						v = v + v + bit
					}
					v += 3 + (8 << uint(cat))
				}
			}
			p = pCtx[2][:]
		}

		// Inline: GetSigned(v)
		dq := dq1
		if n == 0 {
			dq = dq0
		}
		if brB < 0 {
			brV, brB = brLoad(br, brV, brB)
		}
		var sv int
		sv, brV, brR, brB = fastSigned(v, brV, brR, brB)
		out[KZigzag[n]] = int16(sv * dq)
	}
	brSync(br, brV, brR, brB)
	return 16
}

// nzCodeBits packs 2-bit codes describing how many coefficients are non-zero.
func nzCodeBits(nzCoeffs uint32, nz int, dcNz int) uint32 {
	nzCoeffs <<= 2
	if nz > 3 {
		nzCoeffs |= 3
	} else if nz > 1 {
		nzCoeffs |= 2
	} else {
		nzCoeffs |= uint32(dcNz)
	}
	return nzCoeffs
}

// decodeMB decodes one macroblock's coefficients from the token partition.
func (dec *Decoder) decodeMB(tokenBR *bitio.BoolReader) error {
	left := &dec.mbInfo[0]
	mb := &dec.mbInfo[dec.mbX+1]
	block := &dec.mbData[dec.mbX]

	skip := false
	if dec.useSkipProba {
		skip = block.Skip
	}

	if !skip {
		dec.parseResiduals(mb, left, block, tokenBR)
	} else {
		left.Nz = 0
		mb.Nz = 0
		if !block.IsI4x4 {
			left.NzDC = 0
			mb.NzDC = 0
		}
		block.NonZeroY = 0
		block.NonZeroUV = 0
		block.Dither = 0
	}

	// Store filter info.
	if dec.filterType > 0 {
		finfo := &dec.fInfo[dec.mbX]
		*finfo = dec.fstrengths[block.Segment][b2i(block.IsI4x4)]
		finfo.FInner = finfo.FInner || !skip
	}

	if tokenBR.EOF() {
		return errPrematureEOF
	}
	return nil
}

// b2i converts bool to int (0 or 1).
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parseResiduals decodes all residual coefficients for one macroblock.
func (dec *Decoder) parseResiduals(mb, leftMB *MB, block *MBData, tokenBR *bitio.BoolReader) {
	bands := &dec.proba.BandsPtr
	q := &dec.dqm[block.Segment]
	dst := block.Coeffs[:]

	// Zero out all coefficients.
	for i := range block.Coeffs {
		block.Coeffs[i] = 0
	}

	var nonZeroY uint32
	var nonZeroUV uint32
	var first int
	var acProba *[17]*BandProbas

	if !block.IsI4x4 {
		// Parse DC (i16-DC = type 1).
		// Use decoder-level scratch to avoid heap escape through dsp.TransformWHT
		// (function variable — compiler can't prove the slice doesn't escape).
		dc := &dec.dcScratch
		for i := range dc {
			dc[i] = 0
		}
		ctx := int(mb.NzDC) + int(leftMB.NzDC)
		nz := getCoeffsInline(tokenBR, &bands[1], ctx, q.Y2Mat[0], q.Y2Mat[1], 0, dc[:])
		if nz > 0 {
			mb.NzDC = 1
			leftMB.NzDC = 1
		} else {
			mb.NzDC = 0
			leftMB.NzDC = 0
		}
		if nz > 1 {
			// Full WHT transform.
			dsp.TransformWHT(dc[:], dst)
		} else {
			// Simplified: only DC is non-zero.
			dc0 := int16((int(dc[0]) + 3) >> 3)
			for i := 0; i < 16*16; i += 16 {
				dst[i] = dc0
			}
		}
		first = 1
		acProba = &bands[0] // i16-AC = type 0
	} else {
		first = 0
		acProba = &bands[3] // i4-AC = type 3
	}

	// Luma AC.
	tnz := mb.Nz & 0x0f
	lnz := leftMB.Nz & 0x0f
	for y := 0; y < 4; y++ {
		l := lnz & 1
		var nzCoeffs uint32
		for x := 0; x < 4; x++ {
			ctx := int(l) + int(tnz&1)
			nz := getCoeffsInline(tokenBR, acProba, ctx, q.Y1Mat[0], q.Y1Mat[1], first, dst)
			if nz > first {
				l = 1
			} else {
				l = 0
			}
			tnz = (tnz >> 1) | (l << 7)
			dcNz := 0
			if dst[0] != 0 {
				dcNz = 1
			}
			nzCoeffs = nzCodeBits(nzCoeffs, nz, dcNz)
			dst = dst[16:]
		}
		tnz >>= 4
		lnz = (lnz >> 1) | (l << 7)
		nonZeroY = (nonZeroY << 8) | nzCoeffs
	}
	outTNz := tnz
	outLNz := lnz >> 4

	// Chroma.
	for ch := 0; ch < 4; ch += 2 {
		var nzCoeffs uint32
		tnz = (mb.Nz >> (4 + uint(ch)))
		lnz = (leftMB.Nz >> (4 + uint(ch)))
		for y := 0; y < 2; y++ {
			l := lnz & 1
			for x := 0; x < 2; x++ {
				ctx := int(l) + int(tnz&1)
				nz := getCoeffsInline(tokenBR, &bands[2], ctx, q.UVMat[0], q.UVMat[1], 0, dst)
				if nz > 0 {
					l = 1
				} else {
					l = 0
				}
				tnz = (tnz >> 1) | (l << 3)
				dcNz := 0
				if dst[0] != 0 {
					dcNz = 1
				}
				nzCoeffs = nzCodeBits(nzCoeffs, nz, dcNz)
				dst = dst[16:]
			}
			tnz >>= 2
			lnz = (lnz >> 1) | (l << 5)
		}
		nonZeroUV |= nzCoeffs << uint(4*ch)
		outTNz |= (tnz << 4) << uint(ch)
		outLNz |= (lnz & 0xf0) << uint(ch)
	}

	mb.Nz = outTNz
	leftMB.Nz = outLNz
	block.NonZeroY = nonZeroY
	block.NonZeroUV = nonZeroUV
	block.Dither = 0
	if nonZeroUV&0xaaaa == 0 {
		block.Dither = uint8(q.Dither)
	}
}
