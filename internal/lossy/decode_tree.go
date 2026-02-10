package lossy

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
// from partition 0.
func (dec *Decoder) parseIntraModeRow() error {
	for mbX := 0; mbX < dec.mbW; mbX++ {
		dec.parseIntraMode(mbX)
	}
	if dec.br.EOF() {
		return errPrematureEOF
	}
	return nil
}

// parseIntraMode parses the prediction mode for a single macroblock.
func (dec *Decoder) parseIntraMode(mbX int) {
	br := dec.br
	top := dec.intraT[4*mbX : 4*mbX+4]
	left := dec.intraL[:]
	block := &dec.mbData[mbX]

	// Segment.
	if dec.segHdr.UpdateMap {
		if br.GetBit(dec.proba.Segments[0]) == 0 {
			block.Segment = uint8(br.GetBit(dec.proba.Segments[1]))
		} else {
			block.Segment = uint8(br.GetBit(dec.proba.Segments[2])) + 2
		}
	} else {
		block.Segment = 0
	}

	// Skip flag.
	if dec.useSkipProba {
		block.Skip = br.GetBit(dec.skipP) != 0
	}

	// Block size.
	block.IsI4x4 = br.GetBit(145) == 0
	if !block.IsI4x4 {
		// 16x16 mode.
		var ymode uint8
		if br.GetBit(156) != 0 {
			if br.GetBit(128) != 0 {
				ymode = TMPred
			} else {
				ymode = HPred
			}
		} else {
			if br.GetBit(163) != 0 {
				ymode = VPred
			} else {
				ymode = DCPred
			}
		}
		block.IModes[0] = ymode
		for i := 0; i < 4; i++ {
			top[i] = ymode
			left[i] = ymode
		}
	} else {
		// 4x4 modes using generic tree parsing.
		modes := block.IModes[:]
		for y := 0; y < 4; y++ {
			ymode := left[y]
			for x := 0; x < 4; x++ {
				prob := &KBModesProba[top[x]][ymode]
				// Walk the tree.
				i := int(KYModesIntra4[br.GetBit(prob[0])])
				for i > 0 {
					i = int(KYModesIntra4[2*i+br.GetBit(prob[i])])
				}
				ymode = uint8(-i)
				top[x] = ymode
				modes[y*4+x] = ymode
			}
			left[y] = ymode
		}
	}

	// UV mode.
	if br.GetBit(142) == 0 {
		block.UVMode = DCPred
	} else if br.GetBit(114) == 0 {
		block.UVMode = VPred
	} else if br.GetBit(183) != 0 {
		block.UVMode = TMPred
	} else {
		block.UVMode = HPred
	}
}
