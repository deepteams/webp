package lossless

import (
	"math"
	"runtime"
	"sync"
)

// VP8L histogram clustering for lossless encoding.
//
// Histograms collect per-symbol frequency counts for the five VP8L symbol
// streams (green+length, red, blue, alpha, distance). The encoder builds
// one histogram per tile, then clusters them using entropy-bin pre-sorting,
// stochastic pairing, and greedy merging to reduce the meta-Huffman
// overhead while maintaining good compression.
//
// Reference: libwebp/src/enc/histogram_enc.c

// nonTrivialSym indicates a histogram without a single unique symbol.
const nonTrivialSym = 0xffff

// Clustering constants matching libwebp.
const (
	numPartitions  = 4
	binSize        = numPartitions * numPartitions * numPartitions
	maxHistoGreedy = 100
)

// histogramIndex enumerates the five sub-histogram types.
type histogramIndex int

const (
	histLiteral  histogramIndex = 0
	histRed      histogramIndex = 1
	histBlue     histogramIndex = 2
	histAlpha    histogramIndex = 3
	histDistance  histogramIndex = 4
)

// ---------------------------------------------------------------------------
// Histogram
// ---------------------------------------------------------------------------

// Histogram holds per-symbol frequency counts for the five VP8L symbol streams.
type Histogram struct {
	Literal  []uint32 // green/length/cache indices
	Red      [NumLiteralCodes]uint32
	Blue     [NumLiteralCodes]uint32
	Alpha    [NumLiteralCodes]uint32
	Distance [NumDistanceCodes]uint32

	paletteCodeBits int        // color cache bits (0 = disabled)
	bitCost         float64    // total cached entropy cost
	costs           [5]float64 // per-channel cached entropy costs

	isUsed        [5]bool   // whether each sub-histogram has non-zero counts
	trivialSymbol [5]uint16 // single symbol index if trivial, else nonTrivialSym
	binID         uint16    // entropy bin index used during clustering
}

// histogramNumCodes returns the literal alphabet size for the given cache bits.
func histogramNumCodes(cacheBits int) int {
	n := NumLiteralCodes + NumLengthCodes
	if cacheBits > 0 {
		n += 1 << cacheBits
	}
	return n
}

// NewHistogram allocates a Histogram with the correct literal slice size.
func NewHistogram(cacheBits int) *Histogram {
	h := &Histogram{
		paletteCodeBits: cacheBits,
		Literal:         make([]uint32, histogramNumCodes(cacheBits)),
	}
	h.resetStats()
	return h
}

// resetStats initializes cached cost values and trivial/used flags.
func (h *Histogram) resetStats() {
	for i := 0; i < 5; i++ {
		h.trivialSymbol[i] = nonTrivialSym
		h.isUsed[i] = true
	}
	h.bitCost = 0
	h.costs = [5]float64{}
}

// Clear zeros out all frequency arrays and resets stats.
func (h *Histogram) Clear() {
	for i := range h.Literal {
		h.Literal[i] = 0
	}
	h.Red = [NumLiteralCodes]uint32{}
	h.Blue = [NumLiteralCodes]uint32{}
	h.Alpha = [NumLiteralCodes]uint32{}
	h.Distance = [NumDistanceCodes]uint32{}
	h.resetStats()
}

// AddSingle accumulates a single PixOrCopy token into the histogram.
func (h *Histogram) AddSingle(v *PixOrCopy, xsize, cacheBits int) {
	switch {
	case v.IsLiteral():
		argb := v.Argb()
		h.Alpha[(argb>>24)&0xff]++
		h.Red[(argb>>16)&0xff]++
		h.Literal[(argb>>8)&0xff]++ // green channel
		h.Blue[argb&0xff]++

	case v.IsCacheIdx():
		idx := NumLiteralCodes + NumLengthCodes + v.CacheIndex()
		if idx < len(h.Literal) {
			h.Literal[idx]++
		}

	case v.IsCopy():
		lenCode, _ := PrefixEncodeBitsNoLUT(v.Length())
		code := NumLiteralCodes + lenCode
		if code < len(h.Literal) {
			h.Literal[code]++
		}
		distCode, _ := PrefixEncodeBitsNoLUT(v.Distance())
		if distCode < NumDistanceCodes {
			h.Distance[distCode]++
		}
	}
}

// AddRefs accumulates all PixOrCopy tokens from refs into the histogram.
func (h *Histogram) AddRefs(refs *BackwardRefs, xsize, cacheBits int) {
	for i := range refs.refs {
		h.AddSingle(&refs.refs[i], xsize, cacheBits)
	}
}

// copyFrom copies all data from src into h. Both must share paletteCodeBits.
func (h *Histogram) copyFrom(src *Histogram) {
	copy(h.Literal, src.Literal)
	h.Red = src.Red
	h.Blue = src.Blue
	h.Alpha = src.Alpha
	h.Distance = src.Distance
	h.paletteCodeBits = src.paletteCodeBits
	h.bitCost = src.bitCost
	h.costs = src.costs
	h.isUsed = src.isUsed
	h.trivialSymbol = src.trivialSymbol
	h.binID = src.binID
}

// population returns the frequency slice for the given histogram index.
func (h *Histogram) population(idx histogramIndex) []uint32 {
	switch idx {
	case histLiteral:
		return h.Literal
	case histRed:
		return h.Red[:]
	case histBlue:
		return h.Blue[:]
	case histAlpha:
		return h.Alpha[:]
	case histDistance:
		return h.Distance[:]
	}
	return nil
}

// ---------------------------------------------------------------------------
// Entropy computation
// ---------------------------------------------------------------------------

// bitEntropy holds intermediate entropy calculation results.
type bitEntropy struct {
	entropy     float64
	sum         uint32
	nonzeros    int
	maxVal      uint32
	nonzeroCode uint32
}

// streaks holds run-length statistics for Huffman cost estimation.
type streaks struct {
	counts  [2]int    // [zero, non-zero] number of streaks > 3
	streaks [2][2]int // [zero/non-zero][streak<=3 / streak>3]
}

// getEntropyUnrefinedHelper processes a single streak transition.
func getEntropyUnrefinedHelper(val uint32, i int, valPrev *uint32, iPrev *int,
	be *bitEntropy, st *streaks) {

	streak := i - *iPrev

	if *valPrev != 0 {
		be.sum += *valPrev * uint32(streak)
		be.nonzeros += streak
		be.nonzeroCode = uint32(*iPrev)
		be.entropy += fastSLog2(*valPrev) * float64(streak)
		if be.maxVal < *valPrev {
			be.maxVal = *valPrev
		}
	}

	isNZ := 0
	if *valPrev != 0 {
		isNZ = 1
	}
	longStreak := 0
	if streak > 3 {
		longStreak = 1
	}
	st.counts[isNZ] += longStreak
	st.streaks[isNZ][longStreak] += streak

	*valPrev = val
	*iPrev = i
}

// getEntropyUnrefined computes the unrefined bit entropy and streak stats
// for a population array.
func getEntropyUnrefined(population []uint32) (bitEntropy, streaks) {
	var be bitEntropy
	var st streaks

	if len(population) == 0 {
		return be, st
	}

	iPrev := 0
	xPrev := population[0]

	for i := 1; i < len(population); i++ {
		x := population[i]
		if x != xPrev {
			getEntropyUnrefinedHelper(x, i, &xPrev, &iPrev, &be, &st)
		}
	}
	getEntropyUnrefinedHelper(0, len(population), &xPrev, &iPrev, &be, &st)

	be.entropy = fastSLog2(be.sum) - be.entropy
	return be, st
}

// getCombinedEntropyUnrefined computes the unrefined bit entropy and streak
// stats for the element-wise sum of two equal-length arrays. The streak
// processing logic is fully inlined at both call sites to eliminate closure
// dispatch overhead and enable better register allocation in this 30% CPU hotpath.
func getCombinedEntropyUnrefined(X, Y []uint32) (bitEntropy, streaks) {
	var be bitEntropy
	var st streaks

	length := len(X)
	if length == 0 {
		return be, st
	}

	iPrev := 0
	xyPrev := X[0] + Y[0]

	for i := 1; i < length; i++ {
		xy := X[i] + Y[i]
		if xy != xyPrev {
			// Inline processStreak (transition to new value)
			streak := i - iPrev
			if xyPrev != 0 {
				be.sum += xyPrev * uint32(streak)
				be.nonzeros += streak
				be.nonzeroCode = uint32(iPrev)
				be.entropy += fastSLog2(xyPrev) * float64(streak)
				if be.maxVal < xyPrev {
					be.maxVal = xyPrev
				}
			}
			isNZ := 0
			if xyPrev != 0 {
				isNZ = 1
			}
			longStreak := 0
			if streak > 3 {
				longStreak = 1
			}
			st.counts[isNZ] += longStreak
			st.streaks[isNZ][longStreak] += streak
			xyPrev = xy
			iPrev = i
		}
	}

	// Inline processStreak (final flush with val=0)
	{
		streak := length - iPrev
		if xyPrev != 0 {
			be.sum += xyPrev * uint32(streak)
			be.nonzeros += streak
			be.nonzeroCode = uint32(iPrev)
			be.entropy += fastSLog2(xyPrev) * float64(streak)
			if be.maxVal < xyPrev {
				be.maxVal = xyPrev
			}
		}
		isNZ := 0
		if xyPrev != 0 {
			isNZ = 1
		}
		longStreak := 0
		if streak > 3 {
			longStreak = 1
		}
		st.counts[isNZ] += longStreak
		st.streaks[isNZ][longStreak] += streak
	}

	be.entropy = fastSLog2(be.sum) - be.entropy
	return be, st
}

// bitsEntropyUnrefined computes the unrefined bit entropy for a population
// without streak statistics.
func bitsEntropyUnrefined(array []uint32) bitEntropy {
	var be bitEntropy
	for i, v := range array {
		if v != 0 {
			be.sum += v
			be.nonzeroCode = uint32(i)
			be.nonzeros++
			be.entropy += fastSLog2(v)
			if be.maxVal < v {
				be.maxVal = v
			}
		}
	}
	be.entropy = fastSLog2(be.sum) - be.entropy
	return be
}

// fastSLog2LUTSize is the LUT size for fastSLog2. 4096 entries (32KB) covers
// the vast majority of histogram count values encountered in practice.
const fastSLog2LUTSize = 4096

// fastSLog2LUT is a precomputed lookup table for v * log2(v).
var fastSLog2LUT [fastSLog2LUTSize]float64

func init() {
	fastSLog2LUT[0] = 0
	for i := 1; i < fastSLog2LUTSize; i++ {
		fv := float64(i)
		fastSLog2LUT[i] = fv * math.Log2(fv)
	}
}

// fastSLog2 computes v * log2(v) for v > 0, returning 0 for v == 0.
func fastSLog2(v uint32) float64 {
	if v < fastSLog2LUTSize {
		return fastSLog2LUT[v]
	}
	fv := float64(v)
	return fv * math.Log2(fv)
}

// bitsEntropyRefine applies heuristic refinement to unrefined entropy.
// This matches libwebp BitsEntropyRefine, adapted to float64.
func bitsEntropyRefine(be *bitEntropy) float64 {
	if be.nonzeros < 5 {
		if be.nonzeros <= 1 {
			return 0
		}
		if be.nonzeros == 2 {
			return 0.99*float64(be.sum) + 0.01*be.entropy
		}
		var mix float64
		if be.nonzeros == 3 {
			mix = 0.95
		} else {
			mix = 0.7
		}
		minLimit := float64(2*be.sum - be.maxVal)
		minLimit = mix*minLimit + (1.0-mix)*be.entropy
		if be.entropy < minLimit {
			return minLimit
		}
		return be.entropy
	}

	mix := 0.627
	minLimit := float64(2*be.sum - be.maxVal)
	minLimit = mix*minLimit + (1.0-mix)*be.entropy
	if be.entropy < minLimit {
		return minLimit
	}
	return be.entropy
}

// BitsEntropy returns the refined Shannon-like entropy for a symbol population.
func BitsEntropy(array []uint32) float64 {
	be := bitsEntropyUnrefined(array)
	return bitsEntropyRefine(&be)
}

// initialHuffmanCost returns the initial Huffman overhead bias.
func initialHuffmanCost() float64 {
	return float64(CodeLengthCodes*3) - 9.1
}

// finalHuffmanCost computes the Huffman overhead from streak statistics.
// Constants are empirical, ported from libwebp fixed-point representation.
func finalHuffmanCost(st *streaks) float64 {
	retval := initialHuffmanCost()
	retval += float64(st.counts[0]) * 1.5625
	retval += float64(st.streaks[0][1]) * 0.234375
	retval += float64(st.counts[1]) * 2.578125
	retval += float64(st.streaks[1][1]) * 0.703125
	retval += float64(st.streaks[0][0]) * 1.796875
	retval += float64(st.streaks[1][0]) * 3.28125
	return retval
}

// populationCost computes entropy + Huffman overhead for a population.
// Returns cost, trivial symbol index, and whether the histogram is used.
func populationCost(population []uint32) (cost float64, trivialSym uint16, isUsed bool) {
	be, st := getEntropyUnrefined(population)

	if be.nonzeros == 1 {
		trivialSym = uint16(be.nonzeroCode)
	} else {
		trivialSym = nonTrivialSym
	}

	isUsed = st.streaks[1][0] != 0 || st.streaks[1][1] != 0
	cost = bitsEntropyRefine(&be) + finalHuffmanCost(&st)
	return cost, trivialSym, isUsed
}

// PopulationCost returns the estimated coding cost of a histogram.
func PopulationCost(h *Histogram) float64 {
	var cost float64
	for i := histogramIndex(0); i < 5; i++ {
		pop := h.population(i)
		c, _, _ := populationCost(pop)
		cost += c
	}
	return cost
}

// extraCost computes the extra bits cost for length/distance prefix codes.
func extraCost(population []uint32, length int) float64 {
	if length < 6 {
		return 0
	}
	var cost float64
	cost += float64(population[4] + population[5])
	halfLen := length/2 - 1
	for i := 2; i < halfLen; i++ {
		cost += float64(i) * float64(population[2*i+2]+population[2*i+3])
	}
	return cost
}

// getCombinedEntropy computes the combined entropy of two histograms for
// one sub-histogram channel.
func getCombinedEntropy(h1, h2 *Histogram, idx histogramIndex) float64 {
	isH1Used := h1.isUsed[idx]
	isH2Used := h2.isUsed[idx]
	isTrivial := h1.trivialSymbol[idx] != nonTrivialSym &&
		h1.trivialSymbol[idx] == h2.trivialSymbol[idx]

	if isTrivial || !isH1Used || !isH2Used {
		if isH1Used {
			return h1.costs[idx]
		}
		return h2.costs[idx]
	}

	X := h1.population(idx)
	Y := h2.population(idx)
	be, st := getCombinedEntropyUnrefined(X, Y)
	return bitsEntropyRefine(&be) + finalHuffmanCost(&st)
}

// computeHistogramCost recomputes cached costs, trivial symbols, and used flags.
func (h *Histogram) computeHistogramCost() {
	for i := histogramIndex(0); i < 5; i++ {
		pop := h.population(i)
		c, trivSym, used := populationCost(pop)
		h.costs[i] = c
		h.trivialSymbol[i] = trivSym
		h.isUsed[i] = used
	}
	h.bitCost = h.costs[0] + h.costs[1] + h.costs[2] + h.costs[3] + h.costs[4]
}

// ---------------------------------------------------------------------------
// HistoSet
// ---------------------------------------------------------------------------

// HistoSet is a collection of histograms used during encoding.
type HistoSet struct {
	histos    []*Histogram
	cacheBits int
	curCombo  *Histogram // scratch for entropy bin combining
}

// HistoScratch holds reusable slab buffers for histogram allocation across
// successive encode calls. When capacity is sufficient, the slabs are reused
// instead of allocating fresh memory.
type HistoScratch struct {
	Slab    []Histogram
	LitSlab []uint32
	Ptrs    []*Histogram
}

// allocateHistoSet creates a set pre-populated with initialized histograms.
// Uses slab allocation: 2 large slices (structs + literal data) instead of
// 2*size individual allocations.
func allocateHistoSet(size, cacheBits int) *HistoSet {
	return allocateHistoSetReuse(size, cacheBits, nil)
}

// allocateHistoSetReuse creates a histogram set, reusing slab buffers from
// scratch when capacity is sufficient. Returns the HistoSet; scratch is
// updated in-place to reference the (possibly newly allocated) slabs.
func allocateHistoSetReuse(size, cacheBits int, scratch *HistoScratch) *HistoSet {
	litSize := histogramNumCodes(cacheBits)
	totalLitSize := size * litSize

	var slab []Histogram
	var litSlab []uint32
	var ptrs []*Histogram

	if scratch != nil {
		slab = scratch.Slab
		litSlab = scratch.LitSlab
		ptrs = scratch.Ptrs
	}

	if cap(slab) >= size {
		slab = slab[:size]
	} else {
		slab = make([]Histogram, size)
	}
	if cap(litSlab) >= totalLitSize {
		litSlab = litSlab[:totalLitSize]
	} else {
		litSlab = make([]uint32, totalLitSize)
	}
	if cap(ptrs) >= size {
		ptrs = ptrs[:size]
	} else {
		ptrs = make([]*Histogram, size)
	}

	for i := range slab {
		slab[i].Literal = litSlab[i*litSize : (i+1)*litSize : (i+1)*litSize]
		slab[i].paletteCodeBits = cacheBits
		slab[i].resetStats()
		ptrs[i] = &slab[i]
	}

	if scratch != nil {
		scratch.Slab = slab
		scratch.LitSlab = litSlab
		scratch.Ptrs = ptrs
	}

	hs := &HistoSet{
		histos:    ptrs,
		cacheBits: cacheBits,
	}
	return hs
}

// Size returns the current number of histograms.
func (hs *HistoSet) Size() int { return len(hs.histos) }

// Get returns the histogram at index i.
func (hs *HistoSet) Get(i int) *Histogram { return hs.histos[i] }

// remove removes the histogram at index i by swapping with the last element.
func (hs *HistoSet) remove(i int) {
	last := len(hs.histos) - 1
	hs.histos[i] = hs.histos[last]
	hs.histos[last] = nil
	hs.histos = hs.histos[:last]
}

// clearAll zeros out all histograms in the set.
func (hs *HistoSet) clearAll() {
	for _, h := range hs.histos {
		h.Clear()
	}
}

// ---------------------------------------------------------------------------
// Histogram merging operations
// ---------------------------------------------------------------------------

// histogramAdd merges two histograms: out[i] = a[i] + b[i] for all arrays.
// out may alias a or b.
func histogramAdd(a, b, out *Histogram) {
	litLen := len(a.Literal)
	if bLen := len(b.Literal); bLen < litLen {
		litLen = bLen
	}
	for i := 0; i < litLen; i++ {
		out.Literal[i] = a.Literal[i] + b.Literal[i]
	}
	for i := 0; i < NumLiteralCodes; i++ {
		out.Red[i] = a.Red[i] + b.Red[i]
		out.Blue[i] = a.Blue[i] + b.Blue[i]
		out.Alpha[i] = a.Alpha[i] + b.Alpha[i]
	}
	for i := 0; i < NumDistanceCodes; i++ {
		out.Distance[i] = a.Distance[i] + b.Distance[i]
	}

	for i := 0; i < 5; i++ {
		if a.trivialSymbol[i] == b.trivialSymbol[i] {
			out.trivialSymbol[i] = a.trivialSymbol[i]
		} else {
			out.trivialSymbol[i] = nonTrivialSym
		}
		out.isUsed[i] = a.isUsed[i] || b.isUsed[i]
	}
}

// HistogramAdd adds the frequency counts of src to dst (in-place).
func HistogramAdd(dst, src *Histogram) {
	histogramAdd(dst, src, dst)
}

// HistogramAddEval computes the combined cost of merging a and b
// without modifying either.
func HistogramAddEval(a, b *Histogram) float64 {
	tmp := mergedHistogram(a, b)
	return PopulationCost(tmp)
}

// mergedHistogram creates a new histogram that is the element-wise sum of a and b.
func mergedHistogram(a, b *Histogram) *Histogram {
	litSize := len(a.Literal)
	if len(b.Literal) > litSize {
		litSize = len(b.Literal)
	}
	h := &Histogram{
		Literal: make([]uint32, litSize),
	}
	for i := 0; i < len(a.Literal); i++ {
		h.Literal[i] += a.Literal[i]
	}
	for i := 0; i < len(b.Literal); i++ {
		h.Literal[i] += b.Literal[i]
	}
	for i := 0; i < NumLiteralCodes; i++ {
		h.Red[i] = a.Red[i] + b.Red[i]
		h.Blue[i] = a.Blue[i] + b.Blue[i]
		h.Alpha[i] = a.Alpha[i] + b.Alpha[i]
	}
	for i := 0; i < NumDistanceCodes; i++ {
		h.Distance[i] = a.Distance[i] + b.Distance[i]
	}
	return h
}

// getCombinedHistogramEntropy computes the combined cost of merging a and b.
// Returns (cost, costs, true) if cost < costThreshold; else (0, _, false).
func getCombinedHistogramEntropy(a, b *Histogram, costThreshold float64) (float64, [5]float64, bool) {
	if costThreshold <= 0 {
		return 0, [5]float64{}, false
	}

	var totalCost float64
	var costs [5]float64

	for i := histogramIndex(0); i < 5; i++ {
		costs[i] = getCombinedEntropy(a, b, i)
		totalCost += costs[i]
		if totalCost >= costThreshold {
			return 0, [5]float64{}, false
		}
	}
	return totalCost, costs, true
}

// histogramAddEvalThresh merges a and b into out and returns (combinedCost, true)
// if the combined cost is below the threshold. Returns (0, false) on bail-out.
func histogramAddEvalThresh(a, b, out *Histogram, costThreshold float64) (float64, bool) {
	sumCost := a.bitCost + b.bitCost
	threshold := costThreshold + sumCost

	bitCost, costs, ok := getCombinedHistogramEntropy(a, b, threshold)
	if !ok {
		return 0, false
	}

	histogramAdd(a, b, out)
	out.bitCost = bitCost
	out.costs = costs
	return bitCost, true
}

// histogramAddThresh evaluates the merge cost without storing the result.
// Returns (C(a+b) - C(a), true) if the cost is below threshold.
func histogramAddThresh(a, b *Histogram, costThreshold float64) (float64, bool) {
	threshold := costThreshold + a.bitCost

	cost, _, ok := getCombinedHistogramEntropy(a, b, threshold)
	if !ok {
		return 0, false
	}
	return cost - a.bitCost, true
}

// ---------------------------------------------------------------------------
// Histogram pair queue for greedy combining
// ---------------------------------------------------------------------------

// histogramPair represents a candidate merge between two histograms.
type histogramPair struct {
	idx1      int
	idx2      int
	costDiff  float64 // C(a+b) - C(a) - C(b); negative means savings
	costCombo float64
	costs     [5]float64
}

// histoQueue is a simple priority queue (best = smallest costDiff at [0]).
// maxSize limits queue capacity (0 = unlimited, used in greedy combining).
type histoQueue struct {
	queue   []histogramPair
	maxSize int
}

func (q *histoQueue) size() int { return len(q.queue) }

func (q *histoQueue) popAt(i int) {
	last := len(q.queue) - 1
	q.queue[i] = q.queue[last]
	q.queue = q.queue[:last]
}

// updateHead ensures the pair with the best (most negative) costDiff is at [0].
func (q *histoQueue) updateHead(pairIdx int) {
	if q.queue[pairIdx].costDiff < q.queue[0].costDiff {
		q.queue[0], q.queue[pairIdx] = q.queue[pairIdx], q.queue[0]
	}
}

// push creates a pair from idx1, idx2 if its costDiff < threshold.
// Returns the costDiff on success, or 0 if the pair was not created.
// If maxSize > 0, the queue will not grow beyond maxSize entries.
func (q *histoQueue) push(histograms []*Histogram, idx1, idx2 int, threshold float64) float64 {
	// Stop if the queue is at capacity (matches C HistoQueuePush).
	if q.maxSize > 0 && len(q.queue) >= q.maxSize {
		return 0
	}

	if idx1 > idx2 {
		idx1, idx2 = idx2, idx1
	}

	h1 := histograms[idx1]
	h2 := histograms[idx2]
	sumCost := h1.bitCost + h2.bitCost
	costThreshold := threshold + sumCost

	costCombo, costs, ok := getCombinedHistogramEntropy(h1, h2, costThreshold)
	if !ok {
		return 0
	}

	pair := histogramPair{
		idx1:      idx1,
		idx2:      idx2,
		costDiff:  costCombo - sumCost,
		costCombo: costCombo,
		costs:     costs,
	}

	q.queue = append(q.queue, pair)
	q.updateHead(len(q.queue) - 1)
	return pair.costDiff
}

// fixPair replaces badID with goodID in the pair and ensures idx1 < idx2.
func fixPair(p *histogramPair, badID, goodID int) {
	if p.idx1 == badID {
		p.idx1 = goodID
	}
	if p.idx2 == badID {
		p.idx2 = goodID
	}
	if p.idx1 > p.idx2 {
		p.idx1, p.idx2 = p.idx2, p.idx1
	}
}

// ---------------------------------------------------------------------------
// Entropy bin analysis and combining
// ---------------------------------------------------------------------------

// dominantCostRange tracks the min/max of the three dominant costs.
type dominantCostRange struct {
	literalMin, literalMax float64
	redMin, redMax         float64
	blueMin, blueMax       float64
}

func newDominantCostRange() dominantCostRange {
	return dominantCostRange{
		literalMin: math.MaxFloat64,
		redMin:     math.MaxFloat64,
		blueMin:    math.MaxFloat64,
	}
}

func (c *dominantCostRange) update(h *Histogram) {
	if c.literalMax < h.costs[histLiteral] {
		c.literalMax = h.costs[histLiteral]
	}
	if c.literalMin > h.costs[histLiteral] {
		c.literalMin = h.costs[histLiteral]
	}
	if c.redMax < h.costs[histRed] {
		c.redMax = h.costs[histRed]
	}
	if c.redMin > h.costs[histRed] {
		c.redMin = h.costs[histRed]
	}
	if c.blueMax < h.costs[histBlue] {
		c.blueMax = h.costs[histBlue]
	}
	if c.blueMin > h.costs[histBlue] {
		c.blueMin = h.costs[histBlue]
	}
}

// getBinIDForEntropy returns a partition index for a value within [min, max].
func getBinIDForEntropy(min, max, val float64) int {
	r := max - min
	if r > 0 {
		delta := val - min
		return int(float64(numPartitions-1) * delta / r)
	}
	return 0
}

// getHistoBinIndex returns the combined entropy bin index for a histogram.
func getHistoBinIndex(h *Histogram, c *dominantCostRange, lowEffort bool) int {
	binID := getBinIDForEntropy(c.literalMin, c.literalMax, h.costs[histLiteral])
	if !lowEffort {
		binID = binID*numPartitions +
			getBinIDForEntropy(c.redMin, c.redMax, h.costs[histRed])
		binID = binID*numPartitions +
			getBinIDForEntropy(c.blueMin, c.blueMax, h.costs[histBlue])
	}
	return binID
}

// getCombineCostFactor returns the merge cost factor based on histogram count
// and quality.
func getCombineCostFactor(histoSize, quality int) float64 {
	factor := 16.0
	if quality < 90 {
		if histoSize > 256 {
			factor /= 2
		}
		if histoSize > 512 {
			factor /= 2
		}
		if histoSize > 1024 {
			factor /= 2
		}
		if quality <= 50 {
			factor /= 2
		}
	}
	return factor
}

// histogramCombineEntropyBin merges histograms with the same bin_id when
// doing so reduces coding cost.
func histogramCombineEntropyBin(imageHisto *HistoSet, numBins int,
	combineCostFactor float64, lowEffort bool) {

	type binInfo struct {
		first              int
		numCombineFailures int
	}

	bins := make([]binInfo, numBins)
	for i := range bins {
		bins[i].first = -1
	}

	if imageHisto.curCombo == nil {
		imageHisto.curCombo = NewHistogram(imageHisto.cacheBits)
	}
	curCombo := imageHisto.curCombo
	histograms := imageHisto.histos

	idx := 0
	for idx < len(histograms) {
		bID := int(histograms[idx].binID)
		first := bins[bID].first

		if first == -1 {
			bins[bID].first = idx
			idx++
		} else if lowEffort {
			histogramAdd(histograms[idx], histograms[first], histograms[first])
			imageHisto.remove(idx)
			histograms = imageHisto.histos
		} else {
			bitCost := histograms[idx].bitCost
			bitCostThresh := -(bitCost * combineCostFactor / 100.0)

			_, ok := histogramAddEvalThresh(histograms[first], histograms[idx],
				curCombo, bitCostThresh)
			if ok {
				const maxCombineFailures = 32
				tryCombine := curCombo.trivialSymbol[histRed] != nonTrivialSym &&
					curCombo.trivialSymbol[histBlue] != nonTrivialSym &&
					curCombo.trivialSymbol[histAlpha] != nonTrivialSym
				if !tryCombine {
					tryCombine = histograms[idx].trivialSymbol[histRed] == nonTrivialSym ||
						histograms[idx].trivialSymbol[histBlue] == nonTrivialSym ||
						histograms[idx].trivialSymbol[histAlpha] == nonTrivialSym
					if tryCombine {
						tryCombine = histograms[first].trivialSymbol[histRed] == nonTrivialSym ||
							histograms[first].trivialSymbol[histBlue] == nonTrivialSym ||
							histograms[first].trivialSymbol[histAlpha] == nonTrivialSym
					}
				}
				if tryCombine || bins[bID].numCombineFailures >= maxCombineFailures {
					histograms[first], curCombo = curCombo, histograms[first]
					imageHisto.histos = histograms
					imageHisto.remove(idx)
					histograms = imageHisto.histos
				} else {
					bins[bID].numCombineFailures++
					idx++
				}
			} else {
				idx++
			}
		}
	}

	if lowEffort {
		for i := 0; i < len(histograms); i++ {
			histograms[i].computeHistogramCost()
		}
	}
}

// histogramCombineGreedy repeatedly merges the pair with the largest savings.
func histogramCombineGreedy(imageHisto *HistoSet) {
	histograms := imageHisto.histos
	n := len(histograms)

	var q histoQueue
	q.maxSize = n * n
	q.queue = make([]histogramPair, 0, n*n)

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			q.push(histograms, i, j, 0)
		}
	}

	for q.size() > 0 {
		idx1 := q.queue[0].idx1
		idx2 := q.queue[0].idx2

		histogramAdd(histograms[idx2], histograms[idx1], histograms[idx1])
		histograms[idx1].bitCost = q.queue[0].costCombo
		histograms[idx1].costs = q.queue[0].costs

		lastIdx := len(histograms) - 1
		histograms[idx2] = histograms[lastIdx]
		histograms[lastIdx] = nil
		histograms = histograms[:lastIdx]
		imageHisto.histos = histograms

		// Remove pairs involving idx1 or idx2; fix references to lastIdx.
		i := 0
		for i < q.size() {
			p := &q.queue[i]
			if p.idx1 == idx1 || p.idx2 == idx1 ||
				p.idx1 == idx2 || p.idx2 == idx2 {
				q.popAt(i)
			} else {
				fixPair(p, lastIdx, idx2)
				q.updateHead(i)
				i++
			}
		}

		for i := 0; i < len(histograms); i++ {
			if i == idx1 {
				continue
			}
			q.push(histograms, idx1, i, 0)
		}
	}
}

// lehmerRand implements a Lehmer random number generator.
func lehmerRand(seed *uint32) uint32 {
	*seed = uint32((uint64(*seed) * 48271) % 2147483647)
	return *seed
}

// histogramCombineStochastic performs stochastic histogram merging.
// Returns true if greedy combining should follow.
func histogramCombineStochastic(imageHisto *HistoSet, minClusterSize int) bool {
	histograms := imageHisto.histos
	if len(histograms) < minClusterSize {
		return true
	}

	const histoQueueSize = 9
	var q histoQueue
	q.maxSize = histoQueueSize
	q.queue = make([]histogramPair, 0, histoQueueSize+1)

	var seed uint32 = 1
	triesWithNoSuccess := 0
	outerIters := len(histograms)
	numTriesNoSuccess := outerIters / 2

	// The C reference uses pre-increment (++tries_with_no_success) in the
	// loop condition, so the counter is 1..numTriesNoSuccess-1 on entry.
	// We replicate this by incrementing before the comparison.
	for iter := 0; iter < outerIters && len(histograms) >= minClusterSize; iter++ {
		triesWithNoSuccess++
		if triesWithNoSuccess >= numTriesNoSuccess {
			break
		}

		bestCost := 0.0
		if q.size() > 0 {
			bestCost = q.queue[0].costDiff
		}

		randRange := uint32((len(histograms) - 1) * len(histograms))
		numTries := len(histograms) / 2

		for j := 0; len(histograms) >= 2 && j < numTries; j++ {
			tmp := lehmerRand(&seed) % randRange
			idx1 := int(tmp / uint32(len(histograms)-1))
			idx2 := int(tmp % uint32(len(histograms)-1))
			if idx2 >= idx1 {
				idx2++
			}

			currCost := q.push(histograms, idx1, idx2, bestCost)
			if currCost < 0 {
				bestCost = currCost
				if q.size() == histoQueueSize {
					break
				}
			}
		}

		if q.size() == 0 {
			continue
		}

		bestIdx1 := q.queue[0].idx1
		bestIdx2 := q.queue[0].idx2

		histogramAdd(histograms[bestIdx2], histograms[bestIdx1], histograms[bestIdx1])
		histograms[bestIdx1].bitCost = q.queue[0].costCombo
		histograms[bestIdx1].costs = q.queue[0].costs

		// Remove bestIdx2 by moving the last histogram into its slot.
		// Keep the full slice accessible during pair fixup (like C which
		// only decrements size but doesn't free memory), then truncate
		// after all pairs are processed.
		lastIdx := len(histograms) - 1
		histograms[bestIdx2] = histograms[lastIdx]
		// Don't nil histograms[lastIdx] yet -- pairs may still reference
		// it until we remap lastIdx -> bestIdx2 below.
		// newSize is the logical size after removal, matching C's
		// image_histo->size after HistogramSetRemoveHistogram.
		newSize := lastIdx

		// Parse the queue and update each pair that deals with
		// bestIdx1, bestIdx2, or the moved-from lastIdx.
		j := 0
		for j < q.size() {
			p := &q.queue[j]
			isIdx1Best := p.idx1 == bestIdx1 || p.idx1 == bestIdx2
			isIdx2Best := p.idx2 == bestIdx1 || p.idx2 == bestIdx2
			// The front pair could have been duplicated by a random pick so
			// check for it all the time nevertheless.
			if isIdx1Best && isIdx2Best {
				q.popAt(j)
				continue
			}
			// Any pair containing one of the two best indices should only
			// refer to bestIdx1. Its cost should also be updated.
			if isIdx1Best || isIdx2Best {
				fixPair(p, bestIdx2, bestIdx1)
				// Re-evaluate the cost of an updated pair.
				// The threshold is 0 (only keep pairs with cost_diff < 0).
				// Note: histograms[lastIdx] is still accessible here since
				// we have not yet truncated the slice.
				h1 := histograms[p.idx1]
				h2 := histograms[p.idx2]
				sumCost := h1.bitCost + h2.bitCost
				costCombo, costs, ok := getCombinedHistogramEntropy(h1, h2, sumCost)
				if !ok {
					q.popAt(j)
					continue
				}
				p.costCombo = costCombo
				p.costs = costs
				p.costDiff = costCombo - sumCost
			}
			// Remap references to the old last position (newSize, which
			// equals lastIdx) to bestIdx2 (its new slot).
			fixPair(p, newSize, bestIdx2)
			q.updateHead(j)
			j++
		}

		// Now truncate the slice to the new logical size.
		histograms[lastIdx] = nil // allow GC
		histograms = histograms[:newSize]
		imageHisto.histos = histograms
		triesWithNoSuccess = 0
	}

	return len(histograms) <= minClusterSize
}

// ---------------------------------------------------------------------------
// Histogram remap (refinement pass)
// ---------------------------------------------------------------------------

// histogramRemap reassigns each input histogram to the closest output cluster
// and recomputes the output histograms.
func histogramRemap(origHistos []*Histogram, imageHisto *HistoSet,
	symbols []uint16) {

	outHistos := imageHisto.histos
	outSize := len(outHistos)

	if outSize > 1 {
		n := len(origHistos)
		if n >= 64 {
			// Parallel symbol assignment: each tile is independent.
			// Use sentinel 0xFFFF for nil histograms, then fix up serially.
			const nilSentinel = 0xFFFF
			numWorkers := runtime.GOMAXPROCS(0)
			if numWorkers > n {
				numWorkers = n
			}
			chunk := (n + numWorkers - 1) / numWorkers
			var wg sync.WaitGroup
			wg.Add(numWorkers)
			for w := 0; w < numWorkers; w++ {
				start := w * chunk
				end := start + chunk
				if end > n {
					end = n
				}
				go func(start, end int) {
					defer wg.Done()
					for i := start; i < end; i++ {
						h := origHistos[i]
						if h == nil {
							symbols[i] = nilSentinel
							continue
						}
						bestOut := 0
						bestBits := math.MaxFloat64
						for k := 0; k < outSize; k++ {
							curBits, ok := histogramAddThresh(outHistos[k], h, bestBits)
							if ok {
								bestBits = curBits
								bestOut = k
							}
						}
						symbols[i] = uint16(bestOut)
					}
				}(start, end)
			}
			wg.Wait()

			// Serial fixup: resolve nil sentinel values (left-to-right dependency).
			for i := 0; i < n; i++ {
				if symbols[i] == nilSentinel {
					if i > 0 {
						symbols[i] = symbols[i-1]
					} else {
						symbols[i] = 0
					}
				}
			}
		} else {
			for i, h := range origHistos {
				if h == nil {
					if i > 0 {
						symbols[i] = symbols[i-1]
					}
					continue
				}
				bestOut := 0
				bestBits := math.MaxFloat64
				for k := 0; k < outSize; k++ {
					curBits, ok := histogramAddThresh(outHistos[k], h, bestBits)
					if ok {
						bestBits = curBits
						bestOut = k
					}
				}
				symbols[i] = uint16(bestOut)
			}
		}
	} else {
		for i := range origHistos {
			symbols[i] = 0
		}
	}

	// Recompute output histograms from originals and symbol assignments.
	for _, h := range outHistos {
		h.Clear()
	}
	for i, h := range origHistos {
		if h == nil {
			continue
		}
		idx := int(symbols[i])
		histogramAdd(h, outHistos[idx], outHistos[idx])
	}
}

// ---------------------------------------------------------------------------
// Parallel histogram cost computation
// ---------------------------------------------------------------------------

// parallelComputeHistogramCost computes costs for all histograms in parallel
// when count >= 256, otherwise serially.
func parallelComputeHistogramCost(histos []*Histogram) {
	n := len(histos)
	if n < 256 {
		for _, h := range histos {
			h.computeHistogramCost()
		}
		return
	}
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > n {
		numWorkers = n
	}
	chunk := (n + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for w := 0; w < numWorkers; w++ {
		start := w * chunk
		end := start + chunk
		if end > n {
			end = n
		}
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				histos[i].computeHistogramCost()
			}
		}(start, end)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// GetHistoImageSymbols
// ---------------------------------------------------------------------------

// GetHistoImageSymbols builds per-tile histograms from backward references,
// clusters them into a compact set, and returns the per-tile symbol map
// and the final histogram set.
//
// width, height: image dimensions in pixels
// refs: backward reference tokens
// quality: encoding quality (0..100)
// histoBits: tile subdivision bits (tile size = 1 << histoBits)
// cacheBits: color cache bits (0 = disabled)
func GetHistoImageSymbols(width, height int, refs *BackwardRefs, quality int,
	histoBits, cacheBits int, scratch *HistoScratch) ([]uint16, *HistoSet) {

	lowEffort := quality < 25

	histoXSize := VP8LSubSampleSize(width, histoBits)
	histoYSize := VP8LSubSampleSize(height, histoBits)
	imageHistoRawSize := histoXSize * histoYSize

	// Create per-tile histograms from backward references.
	origHisto := allocateHistoSetReuse(imageHistoRawSize, cacheBits, scratch)
	histogramBuild(width, histoBits, refs, origHisto)

	// Compute costs and build imageHisto as direct pointers into origHisto's
	// slab. This avoids allocating a full copy (~9.5 MB savings). After
	// clustering modifies the shared data, we extract cluster centers into a
	// small slab and rebuild origHisto for the remap pass.
	imageHisto := &HistoSet{
		histos:    make([]*Histogram, 0, imageHistoRawSize),
		cacheBits: cacheBits,
	}

	// Parallel cost computation for initial histograms.
	parallelComputeHistogramCost(origHisto.histos[:imageHistoRawSize])

	// Serial filtering: append non-empty histograms.
	for i := 0; i < imageHistoRawSize; i++ {
		h := origHisto.histos[i]
		if !h.isUsed[histLiteral] && !h.isUsed[histRed] &&
			!h.isUsed[histBlue] && !h.isUsed[histAlpha] &&
			!h.isUsed[histDistance] {
			continue
		}
		imageHisto.histos = append(imageHisto.histos, h)
	}

	// Entropy bin combining.
	entropyCombineNumBins := binSize
	if lowEffort {
		entropyCombineNumBins = numPartitions
	}
	entropyCombine := len(imageHisto.histos) > entropyCombineNumBins*2 && quality < 100

	if entropyCombine {
		combineCostFactor := getCombineCostFactor(imageHistoRawSize, quality)

		costRange := newDominantCostRange()
		for _, h := range imageHisto.histos {
			costRange.update(h)
		}
		for _, h := range imageHisto.histos {
			h.binID = uint16(getHistoBinIndex(h, &costRange, lowEffort))
		}

		histogramCombineEntropyBin(imageHisto, entropyCombineNumBins,
			combineCostFactor, lowEffort)
	}

	// Stochastic + greedy combining.
	if !lowEffort || !entropyCombine {
		thresholdSize := 1 + quality*quality*quality*(maxHistoGreedy-1)/(100*100*100)

		doGreedy := histogramCombineStochastic(imageHisto, thresholdSize)
		if doGreedy {
			histogramCombineGreedy(imageHisto)
		}
	}

	// Extract cluster centers into a small separate slab so origHisto's
	// backing memory can be rebuilt for the remap pass.
	extractClusterCenters(imageHisto, cacheBits)

	// Rebuild origHisto from backward refs. Clustering modified shared data.
	origHisto.clearAll()
	histogramBuild(width, histoBits, refs, origHisto)

	// Parallel cost recomputation after rebuild.
	parallelComputeHistogramCost(origHisto.histos[:imageHistoRawSize])

	// Nil out empty entries (index > 0) for histogramRemap.
	for i := 1; i < imageHistoRawSize; i++ {
		h := origHisto.histos[i]
		if !h.isUsed[histLiteral] && !h.isUsed[histRed] &&
			!h.isUsed[histBlue] && !h.isUsed[histAlpha] &&
			!h.isUsed[histDistance] {
			origHisto.histos[i] = nil
		}
	}

	// Final remap: assign each original tile to the nearest output cluster.
	symbols := make([]uint16, imageHistoRawSize)
	histogramRemap(origHisto.histos, imageHisto, symbols)

	// Recompute final costs.
	for _, h := range imageHisto.histos {
		h.computeHistogramCost()
	}

	return symbols, imageHisto
}

// extractClusterCenters copies the cluster center histograms from the shared
// origHisto slab into a small dedicated slab. This decouples imageHisto from
// origHisto so the latter can be rebuilt for the remap pass.
func extractClusterCenters(imageHisto *HistoSet, cacheBits int) {
	n := len(imageHisto.histos)
	if n == 0 {
		return
	}
	litSize := histogramNumCodes(cacheBits)
	slab := make([]Histogram, n)
	litSlab := make([]uint32, n*litSize)
	for i := 0; i < n; i++ {
		dst := &slab[i]
		dst.Literal = litSlab[i*litSize : (i+1)*litSize : (i+1)*litSize]
		dst.copyFrom(imageHisto.histos[i])
		imageHisto.histos[i] = dst
	}
}

// histogramBuild assigns each backward-reference token to the histogram
// of the tile it starts in.
func histogramBuild(xsize, histoBits int, refs *BackwardRefs, imageHisto *HistoSet) {
	histoXSize := VP8LSubSampleSize(xsize, histoBits)
	imageHisto.clearAll()

	x, y := 0, 0
	for i := range refs.refs {
		v := &refs.refs[i]
		ix := (y>>histoBits)*histoXSize + (x >> histoBits)
		imageHisto.histos[ix].AddSingle(v, xsize, 0)
		x += v.Length()
		for x >= xsize {
			x -= xsize
			y++
		}
	}
}
