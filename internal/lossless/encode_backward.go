package lossless

import "math"

// VP8L backward reference generation.
//
// This module generates backward references (PixOrCopy tokens) for VP8L
// lossless encoding. It provides LZ77 and RLE strategies, a cost model
// for evaluating encoding efficiency, and a top-level function that
// selects the best strategy.
//
// Reference: libwebp/src/enc/backward_references_enc.c

// LZ77 strategy type constants.
const (
	kLZ77Standard = 1
	kLZ77RLE      = 2
	kLZ77Box      = 4
)

// ---------------------------------------------------------------------------
// BackwardRefs
// ---------------------------------------------------------------------------

// BackwardRefs is a growable collection of PixOrCopy tokens produced by
// backward reference generation.
type BackwardRefs struct {
	refs []PixOrCopy
}

// NewBackwardRefs allocates a BackwardRefs with pre-allocated capacity.
func NewBackwardRefs(capacity int) *BackwardRefs {
	return &BackwardRefs{
		refs: make([]PixOrCopy, 0, capacity),
	}
}

// Add appends a PixOrCopy token to the collection.
func (br *BackwardRefs) Add(v PixOrCopy) {
	br.refs = append(br.refs, v)
}

// Reset clears all stored tokens while retaining the underlying memory.
func (br *BackwardRefs) Reset() {
	br.refs = br.refs[:0]
}

// Len returns the number of stored tokens.
func (br *BackwardRefs) Len() int {
	return len(br.refs)
}

// Refs returns the underlying token slice.
func (br *BackwardRefs) Refs() []PixOrCopy {
	return br.refs
}

// ---------------------------------------------------------------------------
// BackwardReferencesLz77
// ---------------------------------------------------------------------------

// BackwardReferencesLz77 generates backward references using an LZ77 hash
// chain. For each pixel position, the hash chain is queried for a match. If
// a match of at least minLength is found, a CopyPixel token is emitted with
// the raw pixel distance. Otherwise a LiteralPixel or CachePixel is emitted.
// The color cache is updated after every emitted pixel.
//
// Distances stored here are raw pixel offsets. They are converted to VP8L
// plane codes by a separate BackwardReferences2DLocality post-processing
// pass, matching the C reference implementation.
func BackwardReferencesLz77(xsize, ysize int, argb []uint32, cacheBits int, hc *HashChain, refs *BackwardRefs) {
	size := xsize * ysize
	refs.Reset()

	var cc *ColorCache
	if cacheBits > 0 {
		cc = NewColorCache(cacheBits)
	}

	for i := 0; i < size; {
		offset := hc.GetOffset(i)
		length := hc.GetLength(i)

		if length >= minLength {
			// Store raw distance (not plane code). The conversion to
			// plane codes happens in BackwardReferences2DLocality.
			refs.Add(CopyPixel(length, offset))
			if cc != nil {
				for k := 0; k < length; k++ {
					cc.Insert(argb[i+k])
				}
			}
			i += length
		} else {
			if cc != nil {
				if idx, ok := cc.Contains(argb[i]); ok {
					refs.Add(CachePixel(idx))
				} else {
					refs.Add(LiteralPixel(argb[i]))
				}
				cc.Insert(argb[i])
			} else {
				refs.Add(LiteralPixel(argb[i]))
			}
			i++
		}
	}
}

// ---------------------------------------------------------------------------
// BackwardReferencesRle
// ---------------------------------------------------------------------------

// BackwardReferencesRle generates backward references using run-length
// encoding only. Consecutive identical pixels are encoded as CopyPixel tokens
// with distance 1. Non-repeating pixels become LiteralPixel tokens.
func BackwardReferencesRle(xsize, ysize int, argb []uint32, cacheBits int, refs *BackwardRefs) {
	size := xsize * ysize
	refs.Reset()

	var cc *ColorCache
	if cacheBits > 0 {
		cc = NewColorCache(cacheBits)
	}

	for i := 0; i < size; {
		// Count the run of identical consecutive pixels starting at i.
		runLen := 1
		for i+runLen < size && argb[i+runLen] == argb[i] {
			runLen++
			if runLen >= maxLength {
				break
			}
		}

		if runLen >= minLength {
			// Emit the first pixel as a literal (or cache hit), then the
			// remaining identical pixels as a copy with distance 1.
			if cc != nil {
				if idx, ok := cc.Contains(argb[i]); ok {
					refs.Add(CachePixel(idx))
				} else {
					refs.Add(LiteralPixel(argb[i]))
				}
				cc.Insert(argb[i])
			} else {
				refs.Add(LiteralPixel(argb[i]))
			}

			// Emit copy for the remaining (runLen - 1) pixels.
			copyLen := runLen - 1
			for copyLen > 0 {
				chunk := copyLen
				if chunk > maxLength {
					chunk = maxLength
				}
				refs.Add(CopyPixel(chunk, 1))
				if cc != nil {
					for k := 0; k < chunk; k++ {
						cc.Insert(argb[i+1+k])
					}
				}
				copyLen -= chunk
			}
			i += runLen
		} else {
			if cc != nil {
				if idx, ok := cc.Contains(argb[i]); ok {
					refs.Add(CachePixel(idx))
				} else {
					refs.Add(LiteralPixel(argb[i]))
				}
				cc.Insert(argb[i])
			} else {
				refs.Add(LiteralPixel(argb[i]))
			}
			i++
		}
	}
}

// ---------------------------------------------------------------------------
// BackwardReferencesLz77Box (9.6)
// ---------------------------------------------------------------------------

// windowOffsetsMaxSize is the maximum number of plane-code offsets for box mode.
const windowOffsetsMaxSize = 32

// BackwardReferencesLz77Box computes an LZ77 by forcing matches to happen
// within a restricted window defined by the lowest 32 PlaneCode values.
// This produces matches with small distance codes.
func BackwardReferencesLz77Box(xsize, ysize int, argb []uint32, cacheBits int,
	hashChainBest *HashChain, refs *BackwardRefs) {

	pixCount := xsize * ysize

	// counts[i] counts how many times a pixel is repeated starting at position i.
	countsIni := make([]uint16, pixCount)
	countsIni[pixCount-1] = 1
	for i := pixCount - 2; i >= 0; i-- {
		if argb[i] == argb[i+1] {
			c := countsIni[i+1] + 1
			if c > maxLength {
				c = maxLength
			}
			countsIni[i] = c
		} else {
			countsIni[i] = 1
		}
	}

	// Figure out the window offsets around a pixel, stored in spiraling order
	// as defined by DistanceToPlaneCode.
	var windowOffsets [windowOffsetsMaxSize]int
	var windowOffsetsNew [windowOffsetsMaxSize]int
	windowOffsetsSize := 0
	windowOffsetsNewSize := 0

	for y := 0; y <= 6; y++ {
		for x := -6; x <= 6; x++ {
			offset := y*xsize + x
			if offset <= 0 {
				continue
			}
			planeCode := DistanceToPlaneCode(xsize, offset) - 1
			if planeCode >= windowOffsetsMaxSize {
				continue
			}
			windowOffsets[planeCode] = offset
		}
	}
	// Remove zero entries (narrow images may not fill all plane codes).
	for i := 0; i < windowOffsetsMaxSize; i++ {
		if windowOffsets[i] == 0 {
			continue
		}
		windowOffsets[windowOffsetsSize] = windowOffsets[i]
		windowOffsetsSize++
	}
	// Find offsets that reach pixels unreachable from P-1.
	for i := 0; i < windowOffsetsSize; i++ {
		isReachable := false
		for j := 0; j < windowOffsetsSize && !isReachable; j++ {
			isReachable = windowOffsets[i] == windowOffsets[j]+1
		}
		if !isReachable {
			windowOffsetsNew[windowOffsetsNewSize] = windowOffsets[i]
			windowOffsetsNewSize++
		}
	}

	// Build box-mode hash chain.
	boxHC := NewHashChain(pixCount)
	boxHC.OffsetLength[0] = 0
	bestOffsetPrev := -1
	bestLengthPrev := -1

	for i := 1; i < pixCount; i++ {
		bestLength := hashChainBest.GetLength(i)
		bestOffset := 0
		doCompute := true

		if bestLength >= maxLength {
			bestOffset = hashChainBest.GetOffset(i)
			for ind := 0; ind < windowOffsetsSize; ind++ {
				if bestOffset == windowOffsets[ind] {
					doCompute = false
					break
				}
			}
		}

		if doCompute {
			usePrev := bestLengthPrev > 1 && bestLengthPrev < maxLength
			numInd := windowOffsetsSize
			if usePrev {
				numInd = windowOffsetsNewSize
				bestLength = bestLengthPrev - 1
				bestOffset = bestOffsetPrev
			} else {
				bestLength = 0
				bestOffset = 0
			}

			for ind := 0; ind < numInd; ind++ {
				var jOffset int
				if usePrev {
					jOffset = i - windowOffsetsNew[ind]
				} else {
					jOffset = i - windowOffsets[ind]
				}
				if jOffset < 0 || argb[jOffset] != argb[i] {
					continue
				}
				// The longest match is the sum of how many times each pixel is repeated.
				currLength := 0
				j := i
				for {
					countsJ := int(countsIni[j])
					countsJOff := int(countsIni[jOffset])
					if countsJOff != countsJ {
						if countsJOff < countsJ {
							currLength += countsJOff
						} else {
							currLength += countsJ
						}
						break
					}
					currLength += countsJOff
					jOffset += countsJOff
					j += countsJOff
					if currLength > maxLength || j >= pixCount || jOffset >= pixCount || argb[jOffset] != argb[j] {
						break
					}
				}

				if bestLength < currLength {
					if usePrev {
						bestOffset = windowOffsetsNew[ind]
					} else {
						bestOffset = windowOffsets[ind]
					}
					if currLength >= maxLength {
						bestLength = maxLength
						break
					}
					bestLength = currLength
				}
			}
		}

		if bestLength <= minLength {
			boxHC.OffsetLength[i] = 0
			bestOffsetPrev = 0
			bestLengthPrev = 0
		} else {
			boxHC.OffsetLength[i] = uint32(bestOffset)<<maxLengthBits | uint32(bestLength)
			bestOffsetPrev = bestOffset
			bestLengthPrev = bestLength
		}
	}

	BackwardReferencesLz77(xsize, ysize, argb, cacheBits, boxHC, refs)
}

// ---------------------------------------------------------------------------
// BackwardReferences2DLocality
// ---------------------------------------------------------------------------

// BackwardReferences2DLocality converts raw pixel distances in copy tokens
// to VP8L plane distance codes. This is a post-processing pass applied after
// all LZ77 matching is complete, matching the C reference implementation
// (BackwardReferences2DLocality in backward_references_enc.c).
//
// Separating distance-to-plane-code conversion from LZ77 matching allows the
// hash chain to work with raw pixel distances, which produces better matches.
func BackwardReferences2DLocality(xsize int, refs *BackwardRefs) {
	for i := range refs.refs {
		if refs.refs[i].IsCopy() {
			dist := int(refs.refs[i].argbOrDistance)
			refs.refs[i].argbOrDistance = uint32(DistanceToPlaneCode(xsize, dist))
		}
	}
}

// ---------------------------------------------------------------------------
// CalculateBestCacheSize
// ---------------------------------------------------------------------------

// CalculateBestCacheSize evaluates optimal color cache bits for a given set of
// backward references by brute-force searching over cacheBits 0..cacheBitsMax.
// For each candidate, it builds histograms (simulating which pixels would be
// cache hits) and picks the value with the lowest estimated entropy.
//
// This matches the C reference CalculateBestCacheSize in backward_references_enc.c.
// quality <= 25 disables the color cache (returns 0).
func CalculateBestCacheSize(argb []uint32, quality int, refs *BackwardRefs, cacheBitsMax int) int {
	if quality <= 25 {
		return 0
	}
	if cacheBitsMax <= 0 {
		return 0
	}
	if cacheBitsMax > MaxCacheBits {
		cacheBitsMax = MaxCacheBits
	}

	// Allocate one histogram and one color cache per candidate cache size.
	// Slab-allocate histogram structs and color cache structs to reduce allocs.
	numHistos := cacheBitsMax + 1
	histos := make([]*Histogram, numHistos)
	histoSlab := make([]Histogram, numHistos)

	// Each histogram has a different literal size, so compute total and allocate one slab.
	totalLitSize := 0
	for i := 0; i < numHistos; i++ {
		totalLitSize += histogramNumCodes(i)
	}
	litSlab := make([]uint32, totalLitSize)
	litOff := 0
	for i := 0; i < numHistos; i++ {
		ls := histogramNumCodes(i)
		histoSlab[i].Literal = litSlab[litOff : litOff+ls : litOff+ls]
		histoSlab[i].paletteCodeBits = i
		histoSlab[i].resetStats()
		histos[i] = &histoSlab[i]
		litOff += ls
	}

	caches := make([]*ColorCache, numHistos)
	cacheSlab := make([]ColorCache, numHistos)
	// Total color cache entries: sum of 2^i for i=1..cacheBitsMax
	totalCacheSize := 0
	for i := 1; i < numHistos; i++ {
		totalCacheSize += 1 << i
	}
	colorSlab := make([]uint32, totalCacheSize)
	colorOff := 0
	for i := 1; i < numHistos; i++ {
		sz := 1 << i
		cacheSlab[i].Colors = colorSlab[colorOff : colorOff+sz : colorOff+sz]
		cacheSlab[i].HashShift = 32 - i
		cacheSlab[i].HashBits = i
		caches[i] = &cacheSlab[i]
		colorOff += sz
	}

	// Walk through the backward references once, updating all histograms
	// simultaneously. For a literal pixel, check each cache size to see if
	// it would be a cache hit or a literal miss. For copy tokens, only the
	// length prefix code matters (same for all cache sizes), plus cache
	// updates for the copied pixels.
	//
	// The key optimization from the C reference: cache keys for smaller
	// cache sizes can be derived from the largest one by right-shifting.
	argbIdx := 0
	for ri := range refs.refs {
		v := &refs.refs[ri]
		if v.IsLiteral() {
			pix := argb[argbIdx]
			a := (pix >> 24) & 0xff
			r := (pix >> 16) & 0xff
			g := (pix >> 8) & 0xff
			b := pix & 0xff

			// Compute the hash key for the largest cache, then shift for smaller ones.
			key := int((pix * kHashMul) >> uint(32-cacheBitsMax))

			// cache_bits = 0: always a literal (no cache).
			histos[0].Blue[b]++
			histos[0].Literal[g]++
			histos[0].Red[r]++
			histos[0].Alpha[a]++

			// cache_bits = cacheBitsMax down to 1.
			for i := cacheBitsMax; i >= 1; i-- {
				if i < cacheBitsMax {
					key >>= 1
				}
				if caches[i].Colors[key] == pix {
					// Cache hit: record as cache index in the literal histogram.
					idx := NumLiteralCodes + NumLengthCodes + key
					if idx < len(histos[i].Literal) {
						histos[i].Literal[idx]++
					}
				} else {
					// Cache miss: record as literal and update cache.
					caches[i].Colors[key] = pix
					histos[i].Blue[b]++
					histos[i].Literal[g]++
					histos[i].Red[r]++
					histos[i].Alpha[a]++
				}
			}
			argbIdx++
		} else {
			// Copy token. The length prefix code contribution is the same
			// for all cache sizes. Distance histogram is also the same, so
			// we can skip it (it cancels out in comparison).
			length := v.Length()
			lenCode, _ := PrefixEncodeBitsNoLUT(length)
			code := NumLiteralCodes + lenCode
			for i := 0; i <= cacheBitsMax; i++ {
				if code < len(histos[i].Literal) {
					histos[i].Literal[code]++
				}
			}

			// Update color caches for all the copied pixels.
			argbPrev := argb[argbIdx] ^ 0xffffffff // force initial mismatch
			for k := 0; k < length; k++ {
				pix := argb[argbIdx]
				if pix != argbPrev {
					// Efficiency: only recompute hash when color changes.
					key := int((pix * kHashMul) >> uint(32-cacheBitsMax))
					for i := cacheBitsMax; i >= 1; i-- {
						if i < cacheBitsMax {
							key >>= 1
						}
						caches[i].Colors[key] = pix
					}
					argbPrev = pix
				}
				argbIdx++
			}
		}
	}

	// Find the cache size with the lowest entropy estimate.
	bestCacheBits := 0
	bestCost := uint64(math.MaxUint64)
	for i := 0; i <= cacheBitsMax; i++ {
		cost := histogramEstimateBitsUint64(histos[i])
		if i == 0 || cost < bestCost {
			bestCost = cost
			bestCacheBits = i
		}
	}
	return bestCacheBits
}

// histogramEstimateBitsUint64 computes the estimated bit cost for a histogram,
// matching VP8LHistogramEstimateBits in the C reference. It sums the population
// cost over all 5 sub-histograms plus extra bits for length and distance codes.
func histogramEstimateBitsUint64(h *Histogram) uint64 {
	cost := PopulationCost(h)
	cost += extraCost(h.Literal[NumLiteralCodes:], NumLengthCodes)
	cost += extraCost(h.Distance[:], NumDistanceCodes)
	return uint64(cost)
}

// ---------------------------------------------------------------------------
// BackwardRefsWithLocalCache
// ---------------------------------------------------------------------------

// BackwardRefsWithLocalCache applies a color cache to an existing backward
// reference stream, converting literal pixels to cache-hit codes where the
// pixel is already present in the cache. Copy tokens are not converted but
// the cache is updated for the copied pixels.
//
// This matches BackwardRefsWithLocalCache in the C reference.
func BackwardRefsWithLocalCache(argb []uint32, cacheBits int, refs *BackwardRefs) {
	if cacheBits <= 0 {
		return
	}
	cc := NewColorCache(cacheBits)
	pixelIndex := 0

	for ri := range refs.refs {
		v := &refs.refs[ri]
		if v.IsLiteral() {
			argbLiteral := v.Argb()
			if idx, ok := cc.Contains(argbLiteral); ok {
				// Cache hit: convert literal to cache index.
				refs.refs[ri] = CachePixel(idx)
			} else {
				// Cache miss: keep as literal, insert into cache.
				cc.Insert(argbLiteral)
			}
			pixelIndex++
		} else {
			// Copy token: update cache for all copied pixels.
			length := v.Length()
			for k := 0; k < length; k++ {
				cc.Insert(argb[pixelIndex])
				pixelIndex++
			}
		}
	}
}

// ---------------------------------------------------------------------------
// GetBackwardReferences
// ---------------------------------------------------------------------------

// GetBackwardReferences tries the requested LZ77 strategies and picks the
// one with the lowest estimated coding cost. The winning tokens are stored
// in best. It returns the cacheBits value used by the best candidate.
//
// This matches the C reference GetBackwardReferences which:
//   1. Runs each LZ77 strategy with cacheBits=0
//   2. Selects the best strategy by entropy cost
//   3. Brute-force searches for the optimal cacheBits via CalculateBestCacheSize
//   4. Applies BackwardRefsWithLocalCache if cacheBits > 0
//   5. Optionally applies TraceBackwards for high quality
//
// lz77Types is a bitmask of kLZ77Standard, kLZ77RLE, and kLZ77Box.
// BackwardRefsScratch holds optional pre-allocated buffers for GetBackwardReferences.
type BackwardRefsScratch struct {
	Candidate *BackwardRefs // temporary candidate refs
	Trace     *BackwardRefs // trace result refs
	DistArray []uint16      // dist array for TraceBackwards
	Histo     *Histogram    // scratch histogram for cost estimation
}

func GetBackwardReferences(
	width, height int,
	argb []uint32,
	quality int,
	lz77Types int,
	cacheBitsMax int,
	hc *HashChain,
	best *BackwardRefs,
) int {
	return GetBackwardReferencesWithScratch(width, height, argb, quality,
		lz77Types, cacheBitsMax, hc, best, nil)
}

func GetBackwardReferencesWithScratch(
	width, height int,
	argb []uint32,
	quality int,
	lz77Types int,
	cacheBitsMax int,
	hc *HashChain,
	best *BackwardRefs,
	scratch *BackwardRefsScratch,
) int {
	bestCost := uint64(math.MaxUint64)
	bestLz77Type := 0

	pixCount := width * height

	// Use scratch buffers if provided, otherwise allocate.
	var candidate *BackwardRefs
	var histoScratch *Histogram
	if scratch != nil {
		candidate = scratch.Candidate
		histoScratch = scratch.Histo
	}
	if candidate == nil {
		candidate = NewBackwardRefs(pixCount)
	} else {
		candidate.Reset()
	}
	if histoScratch == nil {
		histoScratch = NewHistogram(cacheBitsMax)
	}

	// Phase 1: Try each LZ77 strategy with cacheBits=0 to find the best one.
	// The C reference runs all strategies without cache first, then applies
	// the cache in a separate pass.
	if lz77Types&kLZ77Standard != 0 {
		BackwardReferencesLz77(width, height, argb, 0, hc, candidate)
		cost := histogramEstimateBitsFromRefsScratch(candidate, 0, histoScratch)
		if cost < bestCost {
			bestCost = cost
			bestLz77Type = kLZ77Standard
			copyRefs(best, candidate)
		}
	}

	if lz77Types&kLZ77RLE != 0 {
		BackwardReferencesRle(width, height, argb, 0, candidate)
		cost := histogramEstimateBitsFromRefsScratch(candidate, 0, histoScratch)
		if cost < bestCost {
			bestCost = cost
			bestLz77Type = kLZ77RLE
			copyRefs(best, candidate)
		}
	}

	if lz77Types&kLZ77Box != 0 {
		BackwardReferencesLz77Box(width, height, argb, 0, hc, candidate)
		cost := histogramEstimateBitsFromRefsScratch(candidate, 0, histoScratch)
		if cost < bestCost {
			bestCost = cost
			bestLz77Type = kLZ77Box
			copyRefs(best, candidate)
		}
	}

	// Phase 2: Find the optimal color cache size via brute-force search.
	// Use the best LZ77 refs (in best) as input.
	bestCacheBits := CalculateBestCacheSize(argb, quality, best, cacheBitsMax)

	// Phase 3: If a color cache is beneficial, apply it to the refs.
	if bestCacheBits > 0 {
		BackwardRefsWithLocalCache(argb, bestCacheBits, best)
	}

	// Recompute cost with the chosen cache bits.
	bestCost = histogramEstimateBitsFromRefsScratch(best, bestCacheBits, histoScratch)

	// Phase 4: Improve on simple LZ77 using TraceBackwards for high quality,
	// matching the C reference threshold (quality >= 25).
	if (bestLz77Type == kLZ77Standard || bestLz77Type == kLZ77Box) && quality >= 25 {
		var traceResult *BackwardRefs
		var distArray []uint16
		if scratch != nil {
			traceResult = scratch.Trace
			distArray = scratch.DistArray
		}
		if traceResult == nil {
			traceResult = NewBackwardRefs(pixCount)
		} else {
			traceResult.Reset()
		}
		if len(distArray) < pixCount {
			distArray = make([]uint16, pixCount)
		} else {
			distArray = distArray[:pixCount]
			for i := range distArray {
				distArray[i] = 0
			}
		}
		if backwardReferencesTraceBackwardsWithDist(width, height, argb, bestCacheBits,
			hc, best, traceResult, distArray) {
			traceCost := histogramEstimateBitsFromRefsScratch(traceResult, bestCacheBits, histoScratch)
			if traceCost < bestCost {
				bestCost = traceCost
				copyRefs(best, traceResult)
			}
		}
	}

	// Convert raw pixel distances to VP8L plane codes as a post-processing
	// step, after all LZ77 cost comparisons are done with raw distances.
	BackwardReferences2DLocality(width, best)

	return bestCacheBits
}

// histogramEstimateBitsFromRefs builds a histogram from backward references
// and returns the estimated bit cost as a uint64.
func histogramEstimateBitsFromRefs(refs *BackwardRefs, cacheBits int) uint64 {
	return histogramEstimateBitsFromRefsScratch(refs, cacheBits, nil)
}

// histogramEstimateBitsFromRefsScratch is like histogramEstimateBitsFromRefs
// but accepts an optional pre-allocated scratch histogram to avoid allocation.
func histogramEstimateBitsFromRefsScratch(refs *BackwardRefs, cacheBits int, scratch *Histogram) uint64 {
	if refs.Len() == 0 {
		return math.MaxUint64
	}
	h := scratch
	if h == nil || len(h.Literal) < histogramNumCodes(cacheBits) {
		h = NewHistogram(cacheBits)
	} else {
		h.Clear()
	}
	h.AddRefs(refs, 0, cacheBits)
	return histogramEstimateBitsUint64(h)
}

// copyRefs replaces the contents of dst with the tokens from src.
func copyRefs(dst, src *BackwardRefs) {
	dst.Reset()
	srcRefs := src.Refs()
	if cap(dst.refs) < len(srcRefs) {
		dst.refs = make([]PixOrCopy, len(srcRefs))
	} else {
		dst.refs = dst.refs[:len(srcRefs)]
	}
	copy(dst.refs, srcRefs)
}

// ---------------------------------------------------------------------------
// TraceBackwards cost-based optimization
// ---------------------------------------------------------------------------
//
// This implements the Zopfli-like TraceBackwards algorithm from
// libwebp/src/enc/backward_references_cost_enc.c. It computes globally
// optimal backward references by:
//   1. Building a cost model from initial greedy LZ77 references
//   2. Running a forward pass (BackwardReferencesHashChainDistanceOnly)
//      that assigns optimal costs to each pixel using interval lists
//   3. Tracing backwards through the cost array to find the optimal path
//   4. Replaying the hash chain along the chosen path to emit final tokens

// costModelTrace holds the TraceBackwards cost model, matching the C struct.
// The literal array holds green values (0..255), length prefix codes
// (256..279), and color cache indices (280+). This layout matches the C
// reference's CostModel where literal is a unified histogram.
type costModelTrace struct {
	alpha    [NumLiteralCodes]float64
	red      [NumLiteralCodes]float64
	blue     [NumLiteralCodes]float64
	distance [NumDistanceCodes]float64
	literal  []float64 // size = NumLiteralCodes + NumLengthCodes + cacheSize
}

// newCostModelTrace allocates a TraceBackwards cost model.
func newCostModelTrace(cacheBits int) *costModelTrace {
	cm := &costModelTrace{}
	n := NumLiteralCodes + NumLengthCodes
	if cacheBits > 0 {
		n += 1 << cacheBits
	}
	cm.literal = make([]float64, n)
	return cm
}

// convertPopulationCountToBitEstimates converts frequency histogram to
// bit cost estimates, matching the C function
// ConvertPopulationCountTableToBitEstimates.
// cost[i] = log2(sum) - log2(count[i]). If only one non-zero symbol,
// all costs are 0.
func convertPopulationCountToBitEstimates(counts []uint32, output []float64) {
	sum := uint32(0)
	nonzeros := 0
	for _, c := range counts {
		sum += c
		if c > 0 {
			nonzeros++
		}
	}
	if nonzeros <= 1 {
		for i := range output {
			output[i] = 0
		}
		return
	}
	logsum := math.Log2(float64(sum))
	for i, c := range counts {
		if c > 0 {
			output[i] = logsum - math.Log2(float64(c))
		} else {
			output[i] = logsum
		}
	}
}

// buildCostModelTrace populates the cost model from backward references,
// matching CostModelBuild in the C reference. It builds a histogram from
// the refs (converting distances to plane codes), then converts to bit
// cost estimates.
func (cm *costModelTrace) build(xsize, cacheBits int, refs *BackwardRefs) {
	// Build histogram matching VP8LHistogramStoreRefs with distance conversion.
	literalSize := NumLiteralCodes + NumLengthCodes
	if cacheBits > 0 {
		literalSize += 1 << cacheBits
	}
	literalCounts := make([]uint32, literalSize)
	var redCounts [NumLiteralCodes]uint32
	var blueCounts [NumLiteralCodes]uint32
	var alphaCounts [NumLiteralCodes]uint32
	var distCounts [NumDistanceCodes]uint32

	for i := range refs.refs {
		v := &refs.refs[i]
		switch {
		case v.IsLiteral():
			a := (v.Argb() >> 24) & 0xff
			r := (v.Argb() >> 16) & 0xff
			g := (v.Argb() >> 8) & 0xff
			b := v.Argb() & 0xff
			literalCounts[g]++
			redCounts[r]++
			blueCounts[b]++
			alphaCounts[a]++

		case v.IsCacheIdx():
			idx := v.CacheIndex()
			literalIdx := NumLiteralCodes + NumLengthCodes + idx
			if literalIdx < literalSize {
				literalCounts[literalIdx]++
			}

		case v.IsCopy():
			lenCode, _ := PrefixEncodeBitsNoLUT(v.Length())
			if lenCode < NumLengthCodes {
				literalCounts[NumLiteralCodes+lenCode]++
			}
			// Convert distance to plane code for the histogram.
			dist := v.Distance()
			planeCode := DistanceToPlaneCode(xsize, dist)
			distCode, _ := PrefixEncodeBitsNoLUT(planeCode)
			if distCode < NumDistanceCodes {
				distCounts[distCode]++
			}
		}
	}

	// Convert population counts to bit estimates.
	convertPopulationCountToBitEstimates(literalCounts, cm.literal)
	convertPopulationCountToBitEstimates(redCounts[:], cm.red[:])
	convertPopulationCountToBitEstimates(blueCounts[:], cm.blue[:])
	convertPopulationCountToBitEstimates(alphaCounts[:], cm.alpha[:])
	convertPopulationCountToBitEstimates(distCounts[:], cm.distance[:])
}

func (cm *costModelTrace) getLiteralCost(v uint32) float64 {
	return cm.alpha[(v>>24)&0xff] +
		cm.red[(v>>16)&0xff] +
		cm.literal[(v>>8)&0xff] +
		cm.blue[v&0xff]
}

func (cm *costModelTrace) getCacheCost(idx int) float64 {
	literalIdx := NumLiteralCodes + NumLengthCodes + idx
	if literalIdx >= len(cm.literal) {
		return math.MaxFloat64
	}
	return cm.literal[literalIdx]
}

func (cm *costModelTrace) getLengthCost(length int) float64 {
	code, extraBits := PrefixEncodeBitsNoLUT(length)
	if code >= NumLengthCodes {
		return math.MaxFloat64
	}
	return cm.literal[NumLiteralCodes+code] + float64(extraBits)
}

func (cm *costModelTrace) getDistanceCost(distance int) float64 {
	code, extraBits := PrefixEncodeBitsNoLUT(distance)
	if code >= NumDistanceCodes {
		return math.MaxFloat64
	}
	return cm.distance[code] + float64(extraBits)
}

// ---------------------------------------------------------------------------
// CostInterval and CostManager
// ---------------------------------------------------------------------------

// costInterval represents a range [start, end) of pixel indices where a
// particular (cost, position) pair may provide the minimum cost.
type costInterval struct {
	cost     float64
	start    int
	end      int
	index    int // the pixel position that generated this interval
	previous *costInterval
	next     *costInterval
}

// costCacheInterval caches the GetLengthCost values grouped by equal-cost
// ranges, reducing the number of interval insertions.
type costCacheInterval struct {
	cost  float64
	start int
	end   int // exclusive
}

// costCacheIntervalSizeMax is the maximum number of active intervals in the
// cost manager. When exceeded, intervals are serialized directly into costs.
const costCacheIntervalSizeMax = 500

// costManager tracks a linked list of costIntervals sorted by start position,
// maintaining the invariant that no two intervals overlap. It also caches
// the per-length costs to reduce redundant computation.
type costManager struct {
	head  *costInterval
	count int

	cacheIntervals     []costCacheInterval
	cacheIntervalsSize int

	costCache [maxLength]float64 // costCache[k] = getLengthCost(k)
	costs     []float64
	distArray []uint16

	// Free list for interval reuse. In Go we use a simple pool slice
	// instead of the C approach of pre-allocated array + malloc fallback.
	freeList []*costInterval
}

// newCostManager initializes a CostManager.
func newCostManager(distArray []uint16, pixCount int, cm *costModelTrace) *costManager {
	mgr := &costManager{
		distArray: distArray,
	}

	costCacheSize := pixCount
	if costCacheSize > maxLength {
		costCacheSize = maxLength
	}

	// Fill cost_cache.
	for i := 0; i < costCacheSize; i++ {
		mgr.costCache[i] = cm.getLengthCost(i)
	}

	// Count the number of distinct cost intervals.
	mgr.cacheIntervalsSize = 1
	for i := 1; i < costCacheSize; i++ {
		if mgr.costCache[i] != mgr.costCache[i-1] {
			mgr.cacheIntervalsSize++
		}
	}

	// Build cache intervals.
	mgr.cacheIntervals = make([]costCacheInterval, mgr.cacheIntervalsSize)
	cur := 0
	mgr.cacheIntervals[0].start = 0
	mgr.cacheIntervals[0].end = 1
	mgr.cacheIntervals[0].cost = mgr.costCache[0]
	for i := 1; i < costCacheSize; i++ {
		costVal := mgr.costCache[i]
		if costVal != mgr.cacheIntervals[cur].cost {
			cur++
			mgr.cacheIntervals[cur].start = i
			mgr.cacheIntervals[cur].cost = costVal
		}
		mgr.cacheIntervals[cur].end = i + 1
	}

	// Allocate costs array, initialized to +Inf.
	mgr.costs = make([]float64, pixCount)
	for i := range mgr.costs {
		mgr.costs[i] = math.MaxFloat64
	}

	return mgr
}

// allocInterval returns a costInterval, reusing from the free list if possible.
func (mgr *costManager) allocInterval() *costInterval {
	if len(mgr.freeList) > 0 {
		iv := mgr.freeList[len(mgr.freeList)-1]
		mgr.freeList = mgr.freeList[:len(mgr.freeList)-1]
		*iv = costInterval{}
		return iv
	}
	return &costInterval{}
}

// freeInterval returns an interval to the free list.
func (mgr *costManager) freeInterval(iv *costInterval) {
	mgr.freeList = append(mgr.freeList, iv)
}

// connectIntervals links prev and next, updating mgr.head if prev is nil.
func (mgr *costManager) connectIntervals(prev, next *costInterval) {
	if prev != nil {
		prev.next = next
	} else {
		mgr.head = next
	}
	if next != nil {
		next.previous = prev
	}
}

// popInterval removes an interval from the linked list.
func (mgr *costManager) popInterval(iv *costInterval) {
	if iv == nil {
		return
	}
	mgr.connectIntervals(iv.previous, iv.next)
	mgr.freeInterval(iv)
	mgr.count--
}

// updateCost updates the cost at pixel i if (cost + costCache[i-position])
// is cheaper than the current cost.
func (mgr *costManager) updateCost(i, position int, cost float64) {
	k := i - position
	if mgr.costs[i] > cost {
		mgr.costs[i] = cost
		mgr.distArray[i] = uint16(k + 1)
	}
}

// updateCostPerInterval updates costs for all pixels in [start, end).
func (mgr *costManager) updateCostPerInterval(start, end, position int, cost float64) {
	for i := start; i < end; i++ {
		mgr.updateCost(i, position, cost)
	}
}

// updateCostAtIndex updates the cost at pixel i by checking all intervals
// that overlap with i. If doClean is true, outdated intervals (end <= i)
// are removed.
func (mgr *costManager) updateCostAtIndex(i int, doClean bool) {
	current := mgr.head
	for current != nil && current.start <= i {
		next := current.next
		if current.end <= i {
			if doClean {
				mgr.popInterval(current)
			}
		} else {
			mgr.updateCost(i, current.index, current.cost)
		}
		current = next
	}
}

// positionOrphanInterval inserts current into the sorted linked list,
// using previous as a starting hint.
func (mgr *costManager) positionOrphanInterval(current, previous *costInterval) {
	if previous == nil {
		previous = mgr.head
	}
	for previous != nil && current.start < previous.start {
		previous = previous.previous
	}
	for previous != nil && previous.next != nil &&
		previous.next.start < current.start {
		previous = previous.next
	}
	if previous != nil {
		mgr.connectIntervals(current, previous.next)
	} else {
		mgr.connectIntervals(current, mgr.head)
	}
	mgr.connectIntervals(previous, current)
}

// insertInterval adds a new interval [start, end) into the sorted list.
// If the manager is at capacity, the interval is serialized directly.
func (mgr *costManager) insertInterval(intervalIn *costInterval, cost float64, position, start, end int) {
	if start >= end {
		return
	}
	if mgr.count >= costCacheIntervalSizeMax {
		mgr.updateCostPerInterval(start, end, position, cost)
		return
	}
	iv := mgr.allocInterval()
	iv.cost = cost
	iv.index = position
	iv.start = start
	iv.end = end
	mgr.positionOrphanInterval(iv, intervalIn)
	mgr.count++
}

// pushInterval processes a new interval defined by (distanceCost, position, length).
// It merges with, splits, or replaces existing intervals as needed.
func (mgr *costManager) pushInterval(distanceCost float64, position, length int) {
	interval := mgr.head

	// For short intervals, serialize directly.
	const kSkipDistance = 10
	if length < kSkipDistance {
		for j := position; j < position+length; j++ {
			k := j - position
			costTmp := distanceCost + mgr.costCache[k]
			if mgr.costs[j] > costTmp {
				mgr.costs[j] = costTmp
				mgr.distArray[j] = uint16(k + 1)
			}
		}
		return
	}

	for ci := 0; ci < mgr.cacheIntervalsSize && mgr.cacheIntervals[ci].start < length; ci++ {
		start := position + mgr.cacheIntervals[ci].start
		end := position + mgr.cacheIntervals[ci].end
		if end > position+length {
			end = position + length
		}
		cost := distanceCost + mgr.cacheIntervals[ci].cost

		for interval != nil && interval.start < end {
			intervalNext := interval.next

			// Ensure overlap.
			if start >= interval.end {
				interval = intervalNext
				continue
			}

			if cost >= interval.cost {
				// New interval is worse: insert before the existing interval,
				// then skip past it.
				startNew := interval.end
				mgr.insertInterval(interval, cost, position, start, interval.start)
				start = startNew
				if start >= end {
					break
				}
				interval = intervalNext
				continue
			}

			if start <= interval.start {
				if interval.end <= end {
					// Existing interval is fully contained: remove it.
					mgr.popInterval(interval)
				} else {
					// Existing interval extends past end: shrink its start.
					interval.start = end
					break
				}
			} else {
				if end < interval.end {
					// Existing interval fully contains the new one: split.
					endOriginal := interval.end
					interval.end = start
					mgr.insertInterval(interval, interval.cost, interval.index, end, endOriginal)
					interval = interval.next
					break
				} else {
					// Existing interval overlaps on the left: shrink its end.
					interval.end = start
				}
			}
			interval = intervalNext
		}
		// Insert the remaining new interval.
		mgr.insertInterval(interval, cost, position, start, end)
	}
}

// addSingleLiteralWithCostModel evaluates the cost of encoding pixel at idx
// as a literal or cache hit, updating costs/distArray if cheaper.
func addSingleLiteralWithCostModel(
	argb []uint32, cc *ColorCache, cm *costModelTrace,
	idx int, useColorCache bool, prevCost float64,
	costs []float64, distArray []uint16,
) {
	costVal := prevCost
	color := argb[idx]

	ix := -1
	if useColorCache {
		if key, ok := cc.Contains(color); ok {
			ix = key
		}
	}

	if ix >= 0 {
		// Color cache hit: scale by 68/100 matching C's DivRound heuristic.
		costVal += cm.getCacheCost(ix) * 0.68
	} else {
		if useColorCache {
			cc.Insert(color)
		}
		// Literal: scale by 82/100 matching C's DivRound heuristic.
		costVal += cm.getLiteralCost(color) * 0.82
	}

	if costs[idx] > costVal {
		costs[idx] = costVal
		distArray[idx] = 1 // single pixel step
	}
}

// backwardReferencesHashChainDistanceOnly computes optimal costs for each
// pixel using the hash chain and interval-based cost tracking. The result
// is stored in distArray where distArray[i] encodes the step size (number
// of pixels to skip back from position i).
func backwardReferencesHashChainDistanceOnly(
	xsize, ysize int, argb []uint32, cacheBits int,
	hc *HashChain, refs *BackwardRefs, distArray []uint16,
) bool {
	pixCount := xsize * ysize
	useColorCache := cacheBits > 0

	cm := newCostModelTrace(cacheBits)
	cm.build(xsize, cacheBits, refs)

	mgr := newCostManager(distArray, pixCount, cm)

	var cc *ColorCache
	if useColorCache {
		cc = NewColorCache(cacheBits)
	}

	// First pixel.
	distArray[0] = 0
	addSingleLiteralWithCostModel(argb, cc, cm, 0, useColorCache, 0,
		mgr.costs, distArray)

	offsetPrev := -1
	lenPrev := -1
	var offsetCost float64
	firstOffsetIsConstant := -1
	reach := 0

	for i := 1; i < pixCount; i++ {
		prevCost := mgr.costs[i-1]
		offset := hc.GetOffset(i)
		length := hc.GetLength(i)

		// Try adding the pixel as a literal.
		addSingleLiteralWithCostModel(argb, cc, cm, i, useColorCache, prevCost,
			mgr.costs, distArray)

		// If we have a copy match.
		if length >= 2 {
			if offset != offsetPrev {
				code := DistanceToPlaneCode(xsize, offset)
				offsetCost = cm.getDistanceCost(code)
				firstOffsetIsConstant = 1
				mgr.pushInterval(prevCost+offsetCost, i, length)
			} else {
				// Optimization for consecutive pixels with same offset.
				if firstOffsetIsConstant == 1 {
					reach = i - 1 + lenPrev - 1
					firstOffsetIsConstant = 0
				}

				if i+length-1 > reach {
					// Need to extend: find the last consecutive pixel with
					// same offset and push a new interval from there.
					j := i
					var offsetJ, lenJ int
					for j <= reach {
						offsetJ = hc.GetOffset(j + 1)
						lenJ = hc.GetLength(j + 1)
						if offsetJ != offset {
							offsetJ = hc.GetOffset(j)
							lenJ = hc.GetLength(j)
							break
						}
						j++
					}
					if j > reach {
						offsetJ = hc.GetOffset(j)
						lenJ = hc.GetLength(j)
					}

					// Update costs at j-1 and j.
					mgr.updateCostAtIndex(j-1, false)
					mgr.updateCostAtIndex(j, false)

					mgr.pushInterval(mgr.costs[j-1]+offsetCost, j, lenJ)
					reach = j + lenJ - 1
				}
			}
		}

		mgr.updateCostAtIndex(i, true)
		offsetPrev = offset
		lenPrev = length
	}

	return true
}

// traceBackwards walks the distArray from right to left, packing the chosen
// path at the end of the array. Returns (path slice, path length).
func traceBackwards(distArray []uint16, distArraySize int) ([]uint16, int) {
	// We pack the path at the end of distArray.
	pathEnd := distArraySize
	cur := distArraySize - 1
	for cur >= 0 {
		k := int(distArray[cur])
		pathEnd--
		distArray[pathEnd] = uint16(k)
		cur -= k
	}
	return distArray[pathEnd:distArraySize], distArraySize - pathEnd
}

// backwardReferencesHashChainFollowChosenPath replays the hash chain using
// the chosen path distances to produce the final backward reference tokens.
func backwardReferencesHashChainFollowChosenPath(
	argb []uint32, cacheBits int,
	chosenPath []uint16, chosenPathSize int,
	hc *HashChain, refs *BackwardRefs,
) bool {
	useColorCache := cacheBits > 0
	var cc *ColorCache
	if useColorCache {
		cc = NewColorCache(cacheBits)
	}

	refs.Reset()
	i := 0
	for ix := 0; ix < chosenPathSize; ix++ {
		length := int(chosenPath[ix])
		if length != 1 {
			offset := hc.GetOffset(i)
			refs.Add(CopyPixel(length, offset))
			if useColorCache {
				for k := 0; k < length; k++ {
					cc.Insert(argb[i+k])
				}
			}
			i += length
		} else {
			if useColorCache {
				if idx, ok := cc.Contains(argb[i]); ok {
					refs.Add(CachePixel(idx))
				} else {
					cc.Insert(argb[i])
					refs.Add(LiteralPixel(argb[i]))
				}
			} else {
				refs.Add(LiteralPixel(argb[i]))
			}
			i++
		}
	}
	return true
}

// BackwardReferencesTraceBackwards computes optimal backward references
// using the TraceBackwards algorithm. It takes initial references (refsSrc)
// as the cost model seed and writes optimized references to refsDst.
func BackwardReferencesTraceBackwards(
	xsize, ysize int, argb []uint32, cacheBits int,
	hc *HashChain, refsSrc *BackwardRefs, refsDst *BackwardRefs,
) bool {
	distArraySize := xsize * ysize
	distArray := make([]uint16, distArraySize)
	return backwardReferencesTraceBackwardsWithDist(xsize, ysize, argb, cacheBits,
		hc, refsSrc, refsDst, distArray)
}

// backwardReferencesTraceBackwardsWithDist is like BackwardReferencesTraceBackwards
// but accepts a pre-allocated distArray to avoid allocation.
func backwardReferencesTraceBackwardsWithDist(
	xsize, ysize int, argb []uint32, cacheBits int,
	hc *HashChain, refsSrc *BackwardRefs, refsDst *BackwardRefs,
	distArray []uint16,
) bool {
	distArraySize := xsize * ysize

	if !backwardReferencesHashChainDistanceOnly(
		xsize, ysize, argb, cacheBits, hc, refsSrc, distArray) {
		return false
	}

	chosenPath, chosenPathSize := traceBackwards(distArray, distArraySize)

	if !backwardReferencesHashChainFollowChosenPath(
		argb, cacheBits, chosenPath, chosenPathSize, hc, refsDst) {
		return false
	}

	return true
}
