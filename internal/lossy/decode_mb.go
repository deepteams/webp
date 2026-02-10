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

// getLargeValue decodes a coefficient value >= 2 (Section 13.2).
func getLargeValue(br *bitio.BoolReader, p []uint8) int {
	var v int
	if br.GetBit(p[3]) == 0 {
		if br.GetBit(p[4]) == 0 {
			v = 2
		} else {
			v = 3 + br.GetBit(p[5])
		}
	} else {
		if br.GetBit(p[6]) == 0 {
			if br.GetBit(p[7]) == 0 {
				v = 5 + br.GetBit(159)
			} else {
				v = 7 + 2*br.GetBit(165)
				v += br.GetBit(145)
			}
		} else {
			bit1 := br.GetBit(p[8])
			bit0 := br.GetBit(p[9+bit1])
			cat := 2*bit1 + bit0
			v = 0
			for _, tabProb := range kCat3456[cat] {
				if tabProb == 0 {
					break
				}
				v = v + v + br.GetBit(tabProb)
			}
			v += 3 + (8 << uint(cat))
		}
	}
	return v
}

// getCoeffs decodes coefficients for a single sub-block.
// Returns the position of the last non-zero coefficient + 1.
func getCoeffs(br *bitio.BoolReader, bands [16 + 1]*BandProbas, ctx int, dq [2]int, n int, out []int16) int {
	p := bands[n].Probas[ctx][:]
	for ; n < 16; n++ {
		if br.GetBit(p[0]) == 0 {
			return n // previous coeff was last non-zero
		}
		for br.GetBit(p[1]) == 0 { // sequence of zero coeffs
			n++
			p = bands[n].Probas[0][:]
			if n == 16 {
				return 16
			}
		}
		// Non-zero coefficient.
		pCtx := &bands[n+1].Probas
		var v int
		if br.GetBit(p[2]) == 0 {
			v = 1
			p = pCtx[1][:]
		} else {
			v = getLargeValue(br, p)
			p = pCtx[2][:]
		}
		// Dequantize: DC uses dq[0], AC uses dq[1].
		dqIdx := 0
		if n > 0 {
			dqIdx = 1
		}
		out[KZigzag[n]] = int16(br.GetSigned(v) * dq[dqIdx])
	}
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
	var acProba [16 + 1]*BandProbas

	if !block.IsI4x4 {
		// Parse DC (i16-DC = type 1).
		var dc [16]int16
		ctx := int(mb.NzDC) + int(leftMB.NzDC)
		nz := getCoeffs(tokenBR, bands[1], ctx, q.Y2Mat, 0, dc[:])
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
		acProba = bands[0] // i16-AC = type 0
	} else {
		first = 0
		acProba = bands[3] // i4-AC = type 3
	}

	// Luma AC.
	tnz := mb.Nz & 0x0f
	lnz := leftMB.Nz & 0x0f
	for y := 0; y < 4; y++ {
		l := lnz & 1
		var nzCoeffs uint32
		for x := 0; x < 4; x++ {
			ctx := int(l) + int(tnz&1)
			nz := getCoeffs(tokenBR, acProba, ctx, q.Y1Mat, first, dst)
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
				nz := getCoeffs(tokenBR, bands[2], ctx, q.UVMat, 0, dst)
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
