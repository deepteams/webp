package lossy

import "github.com/deepteams/webp/internal/dsp"

// ProbaStats accumulates bit frequency counts for coefficient probability optimization.
// Indexed as [type][band][ctx][proba_index][bit_value].
type ProbaStats [NumTypes][NumBands][NumCTX][NumProbas][2]int

// collectCoeffStats mirrors RecordCoeffs logic but only counts bit frequencies.
func collectCoeffStats(coeffs []int16, nCoeffs int, ctxType int, proba *Proba, first int, ctx int, stats *ProbaStats) {
	_ = coeffs[15] // BCE hint
	n := first
	if nCoeffs <= first {
		b := int(KBands[n])
		stats[ctxType][b][ctx][0][0]++ // EOB: bit=0
		return
	}

	for n < 16 {
		b := int(KBands[n])

		if n >= nCoeffs {
			stats[ctxType][b][ctx][0][0]++ // EOB: bit=0
			return
		}

		// Not EOB.
		stats[ctxType][b][ctx][0][1]++ // not-EOB: bit=1

		for {
			v := int(coeffs[KZigzag[n]])
			if v < 0 {
				v = -v
			}

			b = int(KBands[n])

			if v == 0 {
				stats[ctxType][b][ctx][1][0]++ // zero: bit=0
				n++
				if n >= 16 {
					return
				}
				ctx = 0
				continue
			}

			// Non-zero.
			stats[ctxType][b][ctx][1][1]++ // non-zero: bit=1

			// Level tree statistics.
			collectLevelStats(v, ctxType, b, ctx, stats)

			if v == 1 {
				ctx = 1
			} else {
				ctx = 2
			}
			n++
			break
		}
	}
}

// collectLevelStats counts bit frequencies for the coefficient level tree.
func collectLevelStats(level int, ctxType, band, ctx int, stats *ProbaStats) {
	if level == 1 {
		stats[ctxType][band][ctx][2][0]++ // literal 1: bit=0
		return
	}

	stats[ctxType][band][ctx][2][1]++ // not literal 1: bit=1

	if level <= 4 {
		stats[ctxType][band][ctx][3][0]++ // 2-4: bit=0
		if level == 2 {
			stats[ctxType][band][ctx][4][0]++ // 2: bit=0
		} else {
			stats[ctxType][band][ctx][4][1]++ // 3-4: bit=1
			if level == 3 {
				stats[ctxType][band][ctx][5][0]++
			} else {
				stats[ctxType][band][ctx][5][1]++
			}
		}
	} else if level <= 10 {
		stats[ctxType][band][ctx][3][1]++ // 5+: bit=1
		stats[ctxType][band][ctx][6][0]++ // 5-10: bit=0
		if level <= 6 {
			stats[ctxType][band][ctx][7][0]++ // 5-6: bit=0
		} else {
			stats[ctxType][band][ctx][7][1]++ // 7-10: bit=1
		}
	} else {
		stats[ctxType][band][ctx][3][1]++ // 5+: bit=1
		stats[ctxType][band][ctx][6][1]++ // 11+: bit=1

		var cat int
		if level <= 18 {
			cat = 0
		} else if level <= 34 {
			cat = 1
		} else if level <= 66 {
			cat = 2
		} else {
			cat = 3
		}
		bit1 := cat >> 1
		bit0 := cat & 1
		stats[ctxType][band][ctx][8][bit1]++
		stats[ctxType][band][ctx][9+bit1][bit0]++
	}
}

// optimizeProba computes optimal probabilities from bit frequency statistics.
// Returns the number of probability updates applied.
func optimizeProba(stats *ProbaStats, proba *Proba) int {
	numUpdates := 0
	for t := 0; t < NumTypes; t++ {
		for b := 0; b < NumBands; b++ {
			for c := 0; c < NumCTX; c++ {
				for p := 0; p < NumProbas; p++ {
					cnt0 := stats[t][b][c][p][0]
					cnt1 := stats[t][b][c][p][1]
					total := cnt0 + cnt1
					if total == 0 {
						continue
					}

					// Optimal probability: P(bit=0) = 255 - cnt1*255/total.
					// This matches libwebp's CalcTokenProba exactly:
					// return nb ? (255 - nb * 255 / total) : 255;
					newP := 255
					if cnt1 > 0 {
						newP = 255 - cnt1*255/total
					}

					oldP := int(CoeffsProba0[t][b][c][p])

					// Decide whether to update: compare old_cost vs new_cost.
					// old_cost = BranchCost(old_p) + VP8BitCost(0, update_proba)
					// new_cost = BranchCost(new_p) + VP8BitCost(1, update_proba) + 8*256
					// Update if old_cost > new_cost.
					updateProba := CoeffsUpdateProba[t][b][c][p]
					oldCost := branchCost(cnt0, cnt1, oldP) + dsp.VP8BitCost(0, updateProba)
					newCost := branchCost(cnt0, cnt1, newP) + dsp.VP8BitCost(1, updateProba) + 8*256
					if oldCost > newCost {
						proba.Bands[t][b].Probas[c][p] = uint8(newP)
						numUpdates++
					}
				}
			}
		}
	}
	return numUpdates
}

// branchCost computes the cost of encoding cnt0 zeros and cnt1 ones
// using the given probability. Matches libwebp's BranchCost.
func branchCost(cnt0, cnt1, proba int) int {
	if proba < 1 {
		proba = 1
	}
	if proba > 255 {
		proba = 255
	}
	return cnt1*dsp.VP8BitCost(1, uint8(proba)) + cnt0*dsp.VP8BitCost(0, uint8(proba))
}

// collectAllStats collects probability statistics from all macroblock coefficients.
func (enc *VP8Encoder) collectAllStats(stats *ProbaStats) {
	// Reset NZ context for stats collection (mirrors encodeFrame).
	// Use pre-allocated buffers to avoid allocation per call.
	topNz := enc.statTopNz
	topNzDC := enc.statTopNzDC
	for i := range topNz {
		topNz[i] = 0
	}
	for i := range topNzDC {
		topNzDC[i] = 0
	}
	var leftNz uint32
	var leftNzDC uint8

	for mbY := 0; mbY < enc.mbH; mbY++ {
		leftNz = 0
		leftNzDC = 0

		for mbX := 0; mbX < enc.mbW; mbX++ {
			idx := mbY*enc.mbW + mbX
			info := &enc.mbInfo[idx]

			if info.Skip {
				topNz[mbX] = 0
				leftNz = 0
				if info.MBType == 0 {
					topNzDC[mbX] = 0
					leftNzDC = 0
				}
				continue
			}

			topNzVal := topNz[mbX]
			leftNzVal := leftNz
			var outTNz, outLNz uint32

			if info.MBType == 0 {
				// I16 DC block — use pre-computed nzCount.
				dcCtx := int(topNzDC[mbX]) + int(leftNzDC)
				if dcCtx > 2 {
					dcCtx = 2
				}
				nzDC := int(info.NzDC)
				collectCoeffStats(info.Coeffs[384:400], nzDC, 1, &enc.proba, 0, dcCtx, stats)
				if nzDC > 0 {
					topNzDC[mbX] = 1
					leftNzDC = 1
				} else {
					topNzDC[mbX] = 0
					leftNzDC = 0
				}

				// I16 AC blocks — use pre-computed nzCounts.
				first := 1
				tnz := topNzVal & 0x0f
				lnz := leftNzVal & 0x0f
				for y := 0; y < 4; y++ {
					l := lnz & 1
					for x := 0; x < 4; x++ {
						blockIdx := y*4 + x
						off := blockIdx * 16
						ctx := int(l) + int(tnz&1)
						if ctx > 2 {
							ctx = 2
						}
						nz := int(info.NzY[blockIdx])
						collectCoeffStats(info.Coeffs[off:off+16], nz, 0, &enc.proba, first, ctx, stats)
						if nz > first {
							l = 1
						} else {
							l = 0
						}
						tnz = (tnz >> 1) | (l << 7)
					}
					tnz >>= 4
					lnz = (lnz >> 1) | (l << 7)
				}
				outTNz = tnz
				outLNz = lnz >> 4
			} else {
				// I4 blocks — use pre-computed nzCounts.
				tnz := topNzVal & 0x0f
				lnz := leftNzVal & 0x0f
				for y := 0; y < 4; y++ {
					l := lnz & 1
					for x := 0; x < 4; x++ {
						blockIdx := y*4 + x
						off := blockIdx * 16
						ctx := int(l) + int(tnz&1)
						if ctx > 2 {
							ctx = 2
						}
						nz := int(info.NzY[blockIdx])
						collectCoeffStats(info.Coeffs[off:off+16], nz, 3, &enc.proba, 0, ctx, stats)
						if nz > 0 {
							l = 1
						} else {
							l = 0
						}
						tnz = (tnz >> 1) | (l << 7)
					}
					tnz >>= 4
					lnz = (lnz >> 1) | (l << 7)
				}
				outTNz = tnz
				outLNz = lnz >> 4
			}

			// UV blocks — use pre-computed nzCounts.
			for ch := uint(0); ch < 4; ch += 2 {
				tnz := (topNzVal >> (4 + ch)) & 0x0f
				lnz := (leftNzVal >> (4 + ch)) & 0x0f
				for y := 0; y < 2; y++ {
					l := lnz & 1
					for x := 0; x < 2; x++ {
						blockIdx := y*2 + x
						off := (16 + int(ch/2)*4 + blockIdx) * 16
						uvIdx := int(ch/2)*4 + blockIdx
						ctx := int(l) + int(tnz&1)
						if ctx > 2 {
							ctx = 2
						}
						nz := int(info.NzUV[uvIdx])
						collectCoeffStats(info.Coeffs[off:off+16], nz, 2, &enc.proba, 0, ctx, stats)
						if nz > 0 {
							l = 1
						} else {
							l = 0
						}
						tnz = (tnz >> 1) | (l << 3)
					}
					tnz >>= 2
					lnz = (lnz >> 1) | (l << 5)
				}
				outTNz |= (tnz << 4) << ch
				outLNz |= (lnz & 0xf0) << ch
			}

			topNz[mbX] = outTNz
			leftNz = outLNz
		}
	}
}

// rerecordAllTokens resets the token buffer and re-records all MB tokens
// with the current (optimized) probability tables.
func (enc *VP8Encoder) rerecordAllTokens() {
	// Reset NZ context.
	for i := range enc.topNz {
		enc.topNz[i] = 0
	}
	for i := range enc.topNzDC {
		enc.topNzDC[i] = 0
	}
	enc.leftNz = 0
	enc.leftNzDC = 0
	enc.tokens.Reset()

	for mbY := 0; mbY < enc.mbH; mbY++ {
		enc.leftNz = 0
		enc.leftNzDC = 0
		for mbX := 0; mbX < enc.mbW; mbX++ {
			idx := mbY*enc.mbW + mbX
			info := &enc.mbInfo[idx]

			if info.Skip {
				enc.topNz[mbX] = 0
				enc.leftNz = 0
				if info.MBType == 0 {
					enc.topNzDC[mbX] = 0
					enc.leftNzDC = 0
				}
				continue
			}

			// Create a minimal iterator for recordMBTokens.
			tmpIt := &MBIterator{X: mbX, Y: mbY, MBIdx: idx}
			enc.recordMBTokens(tmpIt, info)
		}
	}
}
