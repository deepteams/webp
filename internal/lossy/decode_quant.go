package lossy

// QuantMatrix holds the dequantization factors for one segment.
// Each "mat" is a pair [DC, AC].
type QuantMatrix struct {
	Y1Mat   [2]int // luma DC / AC
	Y2Mat   [2]int // luma-DC (secondary transform) DC / AC
	UVMat   [2]int // chroma DC / AC
	UVQuant int    // U/V quantizer value (for dithering strength)
	Dither  int    // dithering amplitude (0 = off, max 255)
}

// clip clips v to [0, max].
func clip(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// ParseQuant reads quantizer parameters from the bool reader and fills
// the per-segment dequantization matrices.
// Corresponds to VP8ParseQuant (Paragraph 9.6).
func ParseQuant(br BoolSource, segHdr *SegmentHeader, dqm []QuantMatrix) {
	baseQ0 := int(br.GetValue(7))
	dqy1DC := readOptionalSigned(br, 4)
	dqy2DC := readOptionalSigned(br, 4)
	dqy2AC := readOptionalSigned(br, 4)
	dquvDC := readOptionalSigned(br, 4)
	dquvAC := readOptionalSigned(br, 4)

	for i := 0; i < NumMBSegments; i++ {
		var q int
		if segHdr.UseSegment {
			q = int(segHdr.Quantizer[i])
			if !segHdr.AbsoluteDelta {
				q += baseQ0
			}
		} else {
			if i > 0 {
				dqm[i] = dqm[0]
				continue
			}
			q = baseQ0
		}

		m := &dqm[i]
		m.Y1Mat[0] = int(KDcTable[clip(q+dqy1DC, 127)])
		m.Y1Mat[1] = int(KAcTable[clip(q, 127)])

		m.Y2Mat[0] = int(KDcTable[clip(q+dqy2DC, 127)]) * 2
		// y2_ac: (kAcTable[...] * 155 / 100), implemented as (x * 101581) >> 16
		m.Y2Mat[1] = (int(KAcTable[clip(q+dqy2AC, 127)]) * 101581) >> 16
		if m.Y2Mat[1] < 8 {
			m.Y2Mat[1] = 8
		}

		m.UVMat[0] = int(KDcTable[clip(q+dquvDC, 117)])
		m.UVMat[1] = int(KAcTable[clip(q+dquvAC, 127)])

		m.UVQuant = q + dquvAC
	}
}

// readOptionalSigned reads an optional signed value: if a flag bit is set,
// reads numBits as a signed value; otherwise returns 0.
func readOptionalSigned(br BoolSource, numBits int) int {
	if br.GetBit(0x80) != 0 {
		return int(br.GetSignedValue(numBits))
	}
	return 0
}
