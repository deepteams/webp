package lossy

import (
	"unsafe"

	"github.com/deepteams/webp/internal/bitio"
)

// tokenPageSize is the number of tokens per page in the TokenBuffer.
// Larger pages reduce allocation frequency. A typical 640x480 Q75 encode
// produces ~200k tokens; 32768 tokens/page means ~6 pages vs ~24 at 8192.
const tokenPageSize = 32768

// Token represents a single symbol in the VP8 coefficient bitstream.
type Token struct {
	Bit  uint8 // symbol value (0 or 1)
	Prob uint8 // probability context for this bit
}

// tokenPage is a fixed-size page of tokens.
type tokenPage struct {
	tokens [tokenPageSize]Token
	count  int
}

// TokenBuffer accumulates tokens during the encoding pass, then
// emits them in a second pass via the boolean encoder.
type TokenBuffer struct {
	pages    []*tokenPage
	curPage  *tokenPage
	totalMB  int
	// Per-macroblock page index (so we can emit partitioned).
	mbStart []int // page index at start of each MB
	// Pool of previously allocated pages to avoid GC pressure.
	allPages []*tokenPage
}

// Init initializes the token buffer for the given number of macroblocks.
func (tb *TokenBuffer) Init(totalMB int) {
	tb.totalMB = totalMB
	tb.mbStart = make([]int, totalMB+1)
	tb.Reset()
}

// Reset clears all tokens for a new encoding pass.
// Retains previously allocated pages to avoid GC pressure.
func (tb *TokenBuffer) Reset() {
	// Reset page counts but keep underlying page memory.
	for _, p := range tb.pages {
		p.count = 0
	}
	tb.pages = tb.pages[:0]
	tb.curPage = nil
	tb.addPage()
}

// addPage reuses a pooled page or allocates a new token page.
func (tb *TokenBuffer) addPage() {
	idx := len(tb.pages)
	if idx < len(tb.allPages) {
		// Reuse existing page from pool.
		p := tb.allPages[idx]
		p.count = 0
		tb.pages = append(tb.pages, p)
		tb.curPage = p
	} else {
		// Allocate new page and add to pool.
		p := &tokenPage{}
		tb.allPages = append(tb.allPages, p)
		tb.pages = append(tb.pages, p)
		tb.curPage = p
	}
}

// RecordToken appends a single bit/probability pair to the buffer.
func (tb *TokenBuffer) RecordToken(bit int, prob uint8) {
	if tb.curPage.count >= tokenPageSize {
		tb.addPage()
	}
	tb.curPage.tokens[tb.curPage.count] = Token{
		Bit:  uint8(bit & 1),
		Prob: prob,
	}
	tb.curPage.count++
}

// MarkMBStart records that the given macroblock index begins at this point
// in the token stream.
func (tb *TokenBuffer) MarkMBStart(mbIdx int) {
	tb.mbStart[mbIdx] = tb.tokenCount()
}

// tokenCount returns the total number of tokens recorded so far.
func (tb *TokenBuffer) tokenCount() int {
	if len(tb.pages) == 0 {
		return 0
	}
	return (len(tb.pages)-1)*tokenPageSize + tb.curPage.count
}

// RecordCoeffs records the tokens for a quantized coefficient block,
// matching the VP8 decoder's getCoeffs token tree exactly.
//
// The decoder has a two-level loop structure:
//   - Outer loop: reads p[0] (EOB/not-EOB) once per "group"
//   - Inner zero-run loop: reads p[1] (zero/nonzero) for each position,
//     advancing n WITHOUT re-reading p[0] between consecutive zeros
//
// coeffs: quantized coefficients (16 values)
// nCoeffs: position after last non-zero coefficient (from nzCount)
// ctxType: coefficient type (0=i16-AC, 1=i16-DC, 2=chroma, 3=i4)
// proba: frame probability tables
// first: first coefficient position (0 normally, 1 for i16-AC to skip DC)
// ctx: initial probability context (0, 1, or 2) from neighbor NZ info
func (tb *TokenBuffer) RecordCoeffs(coeffs []int16, nCoeffs int, ctxType int, proba *Proba, first int, ctx int) int {
	_ = coeffs[15] // BCE hint
	count := 0
	bands := proba.BandsPtr[ctxType]

	n := first
	if nCoeffs <= first {
		// All zero from start position: emit EOB.
		p := bands[n].Probas[ctx]
		tb.RecordToken(0, p[0])
		return 1
	}

	for n < 16 {
		p := bands[n].Probas[ctx]

		if n >= nCoeffs {
			// Past last non-zero: emit EOB.
			tb.RecordToken(0, p[0])
			count++
			return count
		}

		// Not EOB (more data follows).
		tb.RecordToken(1, p[0])
		count++

		// Inner loop: matches decoder's zero-run loop.
		// Read p[1] for each position; if zero, advance n and read p[1]
		// again at the new position WITHOUT emitting another p[0].
		for {
			v := int(coeffs[KZigzag[n]])
			sign := 0
			if v < 0 {
				v = -v
				sign = 1
			}

			if v == 0 {
				// Zero coefficient.
				tb.RecordToken(0, p[1])
				count++
				n++
				if n >= 16 {
					return count
				}
				p = bands[n].Probas[0] // context 0 in zero run
				continue
			}

			// Non-zero coefficient.
			tb.RecordToken(1, p[1])
			count++

			// Encode level starting at p[2], matching decoder's getLargeValue tree.
			count += tb.recordLevelVP8(v, p[:])

			// Sign bit (uniform probability 128).
			tb.RecordToken(sign, 128)
			count++

			// Update context for next outer iteration.
			if v == 1 {
				ctx = 1
			} else {
				ctx = 2
			}
			n++
			break // exit inner loop, proceed to next outer iteration
		}
	}

	return count
}

// recordLevelVP8 encodes a coefficient level matching the VP8 decoder tree.
// p is the full 11-entry probability array for the current band/context.
// The level tree starts at p[2].
//
// Decoder tree (from getLargeValue in decode_mb.go):
//   p[2]: 0=literal 1, 1=larger
//   p[3]: 0=2-4, 1=5+
//   p[4]: 0=2, 1=3-4
//   p[5]: 0=3, 1=4
//   p[6]: 0=5-10, 1=11+
//   p[7]: 0=cat1(5-6), 1=cat2(7-10)
//   cat1: fixed prob 159 for 5 vs 6
//   cat2: fixed probs 165 (high bit), 145 (low bit) for 7-10
//   p[8]: category selector bit1 (for 11+)
//   p[9+bit1]: category selector bit0
//   Then category extra bits from KCat3/4/5/6 tables
func (tb *TokenBuffer) recordLevelVP8(level int, p []uint8) int {
	count := 0

	if level == 1 {
		tb.RecordToken(0, p[2]) // literal 1
		return 1
	}

	tb.RecordToken(1, p[2]) // not literal 1
	count++

	if level <= 4 {
		// Range 2-4.
		tb.RecordToken(0, p[3]) // 2-4 vs 5+
		count++
		if level == 2 {
			tb.RecordToken(0, p[4]) // 2 vs 3-4
			count++
		} else {
			tb.RecordToken(1, p[4]) // 3-4
			count++
			if level == 3 {
				tb.RecordToken(0, p[5])
			} else {
				tb.RecordToken(1, p[5])
			}
			count++
		}
	} else if level <= 10 {
		// Range 5-10.
		tb.RecordToken(1, p[3]) // 5+
		count++
		tb.RecordToken(0, p[6]) // 5-10 vs 11+
		count++
		if level <= 6 {
			// Cat 1: 5-6.
			tb.RecordToken(0, p[7]) // 5-6 vs 7-10
			count++
			tb.RecordToken(level-5, 159) // fixed prob
			count++
		} else {
			// Cat 2: 7-10.
			tb.RecordToken(1, p[7])
			count++
			v := level - 7
			tb.RecordToken(v>>1, 165) // fixed prob
			count++
			tb.RecordToken(v&1, 145) // fixed prob
			count++
		}
	} else {
		// Range 11+.
		tb.RecordToken(1, p[3]) // 5+
		count++
		tb.RecordToken(1, p[6]) // 11+
		count++

		// Category selection: cat = 2*bit1 + bit0.
		var cat int
		if level <= 18 {
			cat = 0 // cat 3: 11-18
		} else if level <= 34 {
			cat = 1 // cat 4: 19-34
		} else if level <= 66 {
			cat = 2 // cat 5: 35-66
		} else {
			cat = 3 // cat 6: 67+
		}

		bit1 := cat >> 1
		bit0 := cat & 1
		tb.RecordToken(bit1, p[8])
		count++
		tb.RecordToken(bit0, p[9+bit1])
		count++

		// Base value: 3 + (8 << cat), matching decoder's v += 3 + (8 << uint(cat)).
		base := 3 + (8 << uint(cat))
		v := level - base

		// Emit extra bits MSB-first using category tables.
		tables := [4][]uint8{KCat3[:], KCat4[:], KCat5[:], KCat6[:]}
		tab := tables[cat]
		nbits := 0
		for tab[nbits] != 0 {
			nbits++
		}
		for i := 0; i < nbits; i++ {
			tb.RecordToken((v>>(nbits-1-i))&1, tab[i])
			count++
		}
	}

	return count
}

// EmitTokens writes all recorded tokens to a boolean writer.
// Uses batch encoding to keep BoolWriter hot state in registers.
func (tb *TokenBuffer) EmitTokens(bw *bitio.BoolWriter) {
	for _, page := range tb.pages {
		count := page.count
		if count == 0 {
			continue
		}
		// Token is {uint8, uint8} = 2 bytes, no padding. Reinterpret as
		// packed byte pairs [bit0, prob0, bit1, prob1, ...] for batch encoding.
		data := unsafe.Slice((*byte)(unsafe.Pointer(&page.tokens[0])), count*2)
		bw.PutBitBatchPacked(data, count)
	}
}

// EmitTokensPartitioned writes tokens partitioned by macroblock row for
// multi-partition output. partIdx selects which partition (0..numParts-1),
// numParts is the total number of partitions.
// Each MB row is assigned to partition (mbY & (numParts-1)), matching libwebp's
// VP8IteratorSetRow which uses: bw = &enc->parts[y & (enc->num_parts - 1)].
func (tb *TokenBuffer) EmitTokensPartitioned(bw *bitio.BoolWriter, partIdx, numParts, mbW int) {
	if numParts <= 1 {
		// Single partition: emit everything.
		tb.EmitTokens(bw)
		return
	}

	totalMB := tb.totalMB
	// Mark the end sentinel so we know the range of the last MB.
	tb.mbStart[totalMB] = tb.tokenCount()

	for mbIdx := 0; mbIdx < totalMB; mbIdx++ {
		mbY := mbIdx / mbW
		if (mbY & (numParts - 1)) != partIdx {
			continue
		}

		startTok := tb.mbStart[mbIdx]
		endTok := tb.mbStart[mbIdx+1]
		// Process tokens in page-aligned chunks for batch encoding.
		for tok := startTok; tok < endTok; {
			pageIdx := tok / tokenPageSize
			tokIdx := tok % tokenPageSize
			page := tb.pages[pageIdx]
			// How many tokens remain on this page for this MB?
			pageEnd := tokenPageSize
			if startOff := endTok - pageIdx*tokenPageSize; startOff < pageEnd {
				pageEnd = startOff
			}
			count := pageEnd - tokIdx
			if count <= 0 {
				tok++
				continue
			}
			data := unsafe.Slice((*byte)(unsafe.Pointer(&page.tokens[tokIdx])), count*2)
			bw.PutBitBatchPacked(data, count)
			tok += count
		}
	}
}
