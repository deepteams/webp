package lossy

import "fmt"

// parseProba reads coefficient probabilities and skip probability
// from partition 0 (Paragraph 13, 9.9).
func parseProba(br BoolSource, dec *Decoder) {
	p := &dec.proba

	for t := 0; t < NumTypes; t++ {
		for b := 0; b < NumBands; b++ {
			for c := 0; c < NumCTX; c++ {
				for pp := 0; pp < NumProbas; pp++ {
					if br.GetBit(CoeffsUpdateProba[t][b][c][pp]) != 0 {
						p.Bands[t][b].Probas[c][pp] = uint8(br.GetValue(8))
					} else {
						p.Bands[t][b].Probas[c][pp] = CoeffsProba0[t][b][c][pp]
					}
				}
			}
		}
		for b := 0; b < 16+1; b++ {
			p.BandsPtr[t][b] = &p.Bands[t][KBands[b]]
		}
	}

	dec.useSkipProba = br.GetBit(0x80) != 0
	if dec.useSkipProba {
		dec.skipP = uint8(br.GetValue(8))
	}
}

// parseIntraModeRow parses intra prediction modes for one macroblock row
// from partition 0. Uses manually-inlined GetBit for performance.
func (dec *Decoder) parseIntraModeRow() error {
	br := dec.br
	brV := br.Value
	brR := br.Range
	brB := br.Bits

	left := dec.intraL[:]
	updateMap := dec.segHdr.UpdateMap
	useSkip := dec.useSkipProba
	skipP := dec.skipP
	seg0 := dec.proba.Segments[0]
	seg1 := dec.proba.Segments[1]
	seg2 := dec.proba.Segments[2]

	for mbX := 0; mbX < dec.mbW; mbX++ {
		top := dec.intraT[4*mbX : 4*mbX+4]
		block := &dec.mbData[mbX]

		// Segment.
		if updateMap {
			if brB < 0 {
				brV, brB = brLoad(br, brV, brB)
			}
			var bit int
			bit, brV, brR, brB = fastBit(seg0, brV, brR, brB)
			if bit == 0 {
				if brB < 0 {
					brV, brB = brLoad(br, brV, brB)
				}
				bit, brV, brR, brB = fastBit(seg1, brV, brR, brB)
				block.Segment = uint8(bit)
			} else {
				if brB < 0 {
					brV, brB = brLoad(br, brV, brB)
				}
				bit, brV, brR, brB = fastBit(seg2, brV, brR, brB)
				block.Segment = uint8(bit) + 2
			}
		} else {
			block.Segment = 0
		}

		// Skip flag.
		if useSkip {
			if brB < 0 {
				brV, brB = brLoad(br, brV, brB)
			}
			var bit int
			bit, brV, brR, brB = fastBit(skipP, brV, brR, brB)
			block.Skip = bit != 0
		}

		// Block size.
		if brB < 0 {
			brV, brB = brLoad(br, brV, brB)
		}
		var bit int
		bit, brV, brR, brB = fastBit(145, brV, brR, brB)
		block.IsI4x4 = bit == 0
		if !block.IsI4x4 {
			// 16x16 mode.
			var ymode uint8
			if brB < 0 {
				brV, brB = brLoad(br, brV, brB)
				if br.EOF() {
					brSync(br, brV, brR, brB)
					return errPrematureEOF
				}
			}
			bit, brV, brR, brB = fastBit(156, brV, brR, brB)
			if bit != 0 {
				if brB < 0 {
					brV, brB = brLoad(br, brV, brB)
					if br.EOF() {
						brSync(br, brV, brR, brB)
						return errPrematureEOF
					}
				}
				bit, brV, brR, brB = fastBit(128, brV, brR, brB)
				if bit != 0 {
					ymode = TMPred
				} else {
					ymode = HPred
				}
			} else {
				if brB < 0 {
					brV, brB = brLoad(br, brV, brB)
					if br.EOF() {
						brSync(br, brV, brR, brB)
						return errPrematureEOF
					}
				}
				bit, brV, brR, brB = fastBit(163, brV, brR, brB)
				if bit != 0 {
					ymode = VPred
				} else {
					ymode = DCPred
				}
			}
			block.IModes[0] = ymode
			top[0] = ymode
			top[1] = ymode
			top[2] = ymode
			top[3] = ymode
			left[0] = ymode
			left[1] = ymode
			left[2] = ymode
			left[3] = ymode
		} else {
			// 4x4 modes using generic tree parsing.
			modes := block.IModes[:]
			for y := 0; y < 4; y++ {
				ymode := left[y]
				for x := 0; x < 4; x++ {
					prob := &KBModesProba[top[x]][ymode]
					// Walk the tree.
					if brB < 0 {
						brV, brB = brLoad(br, brV, brB)
					}
					bit, brV, brR, brB = fastBit(prob[0], brV, brR, brB)
					i := int(KYModesIntra4[bit])
					for i > 0 {
						if brB < 0 {
							brV, brB = brLoad(br, brV, brB)
							if br.EOF() {
								brSync(br, brV, brR, brB)
								return errPrematureEOF
							}
						}
						bit, brV, brR, brB = fastBit(prob[i], brV, brR, brB)
						i = int(KYModesIntra4[2*i+bit])
					}
					ymode = uint8(-i)
					if ymode >= 10 { // NumBModes
						brSync(br, brV, brR, brB)
						return fmt.Errorf("vp8: invalid 4x4 intra mode %d", ymode)
					}
					top[x] = ymode
					modes[y*4+x] = ymode
				}
				left[y] = ymode
			}
		}

		// UV mode.
		if brB < 0 {
			brV, brB = brLoad(br, brV, brB)
		}
		bit, brV, brR, brB = fastBit(142, brV, brR, brB)
		if bit == 0 {
			block.UVMode = DCPred
		} else {
			if brB < 0 {
				brV, brB = brLoad(br, brV, brB)
			}
			bit, brV, brR, brB = fastBit(114, brV, brR, brB)
			if bit == 0 {
				block.UVMode = VPred
			} else {
				if brB < 0 {
					brV, brB = brLoad(br, brV, brB)
				}
				bit, brV, brR, brB = fastBit(183, brV, brR, brB)
				if bit != 0 {
					block.UVMode = TMPred
				} else {
					block.UVMode = HPred
				}
			}
		}
	}

	brSync(br, brV, brR, brB)
	if br.EOF() {
		return errPrematureEOF
	}
	return nil
}

