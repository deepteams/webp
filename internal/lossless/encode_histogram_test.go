package lossless

import (
	"math"
	"testing"
)

// TestFastSLog2 verifies the v*log2(v) function.
func TestFastSLog2(t *testing.T) {
	tests := []struct {
		v    uint32
		want float64
	}{
		{0, 0},
		{1, 0},
		{2, 2},
		{4, 8},
	}
	for _, tt := range tests {
		got := fastSLog2(tt.v)
		if math.Abs(got-tt.want) > 0.01 {
			t.Errorf("fastSLog2(%d) = %f, want %f", tt.v, got, tt.want)
		}
	}
}

// TestBitsEntropyRefine verifies the heuristic refinement.
func TestBitsEntropyRefine(t *testing.T) {
	t.Run("single symbol", func(t *testing.T) {
		be := &bitEntropy{nonzeros: 1, sum: 100}
		if got := bitsEntropyRefine(be); got != 0 {
			t.Errorf("expected 0 for single symbol, got %f", got)
		}
	})

	t.Run("zero symbols", func(t *testing.T) {
		be := &bitEntropy{nonzeros: 0}
		if got := bitsEntropyRefine(be); got != 0 {
			t.Errorf("expected 0 for zero symbols, got %f", got)
		}
	})

	t.Run("two symbols", func(t *testing.T) {
		be := &bitEntropy{nonzeros: 2, sum: 100, entropy: 50}
		got := bitsEntropyRefine(be)
		// Expected: 0.99*100 + 0.01*50 = 99.5
		if math.Abs(got-99.5) > 0.01 {
			t.Errorf("expected ~99.5, got %f", got)
		}
	})
}

// TestPopulationCost verifies cost computation for a population.
func TestPopulationCost(t *testing.T) {
	t.Run("empty population", func(t *testing.T) {
		pop := make([]uint32, 256)
		cost, trivSym, isUsed := populationCost(pop)
		if isUsed {
			t.Error("expected isUsed=false for empty population")
		}
		if trivSym != nonTrivialSym {
			t.Errorf("expected nonTrivialSym, got %d", trivSym)
		}
		_ = cost
	})

	t.Run("single symbol", func(t *testing.T) {
		pop := make([]uint32, 256)
		pop[42] = 100
		cost, trivSym, isUsed := populationCost(pop)
		if !isUsed {
			t.Error("expected isUsed=true")
		}
		if trivSym != 42 {
			t.Errorf("expected trivialSymbol=42, got %d", trivSym)
		}
		if cost < 0 {
			t.Errorf("cost should be non-negative, got %f", cost)
		}
	})

	t.Run("uniform distribution", func(t *testing.T) {
		pop := make([]uint32, 256)
		for i := range pop {
			pop[i] = 10
		}
		cost, trivSym, isUsed := populationCost(pop)
		if !isUsed {
			t.Error("expected isUsed=true")
		}
		if trivSym != nonTrivialSym {
			t.Errorf("expected nonTrivialSym, got %d", trivSym)
		}
		if cost <= 0 {
			t.Errorf("cost should be positive for uniform distribution, got %f", cost)
		}
	})
}

// TestHistogramComputeCost verifies that computeHistogramCost sets cached fields.
func TestHistogramComputeCost(t *testing.T) {
	h := NewHistogram(0)
	// Add some data to the green/literal channel.
	h.Literal[0] = 50
	h.Literal[1] = 50
	h.Red[0] = 100
	h.Blue[0] = 100
	h.Alpha[0] = 100

	h.computeHistogramCost()

	if h.bitCost <= 0 {
		t.Errorf("bitCost should be positive, got %f", h.bitCost)
	}
	if h.costs[histLiteral] <= 0 {
		t.Error("literal cost should be positive")
	}
	if !h.isUsed[histLiteral] {
		t.Error("literal should be used")
	}
	// Red has a single symbol (index 0), so trivialSymbol should be 0.
	if h.trivialSymbol[histRed] != 0 {
		t.Errorf("red trivialSymbol should be 0, got %d", h.trivialSymbol[histRed])
	}
}

// TestHistogramAdd verifies merging two histograms.
func TestHistogramAdd(t *testing.T) {
	a := NewHistogram(0)
	b := NewHistogram(0)
	out := NewHistogram(0)

	a.Literal[0] = 10
	a.Red[5] = 20
	b.Literal[0] = 30
	b.Red[5] = 40

	histogramAdd(a, b, out)

	if out.Literal[0] != 40 {
		t.Errorf("Literal[0] = %d, want 40", out.Literal[0])
	}
	if out.Red[5] != 60 {
		t.Errorf("Red[5] = %d, want 60", out.Red[5])
	}
}

// TestHistogramAddInPlace verifies in-place merge (out aliases a).
func TestHistogramAddInPlace(t *testing.T) {
	a := NewHistogram(0)
	b := NewHistogram(0)

	a.Literal[0] = 10
	b.Literal[0] = 20

	histogramAdd(a, b, a)

	if a.Literal[0] != 30 {
		t.Errorf("Literal[0] = %d, want 30", a.Literal[0])
	}
}

// TestGetCombinedHistogramEntropy verifies threshold-based cost computation.
func TestGetCombinedHistogramEntropy(t *testing.T) {
	t.Run("zero threshold returns false", func(t *testing.T) {
		a := NewHistogram(0)
		b := NewHistogram(0)
		_, _, ok := getCombinedHistogramEntropy(a, b, 0)
		if ok {
			t.Error("expected false for zero threshold")
		}
	})

	t.Run("negative threshold returns false", func(t *testing.T) {
		a := NewHistogram(0)
		b := NewHistogram(0)
		_, _, ok := getCombinedHistogramEntropy(a, b, -1)
		if ok {
			t.Error("expected false for negative threshold")
		}
	})
}

// TestHistogramCombineGreedy verifies that greedy combining merges identical histograms.
func TestHistogramCombineGreedy(t *testing.T) {
	// Create 4 identical histograms. They should all merge into 1.
	hs := allocateHistoSet(4, 0)
	for i := 0; i < 4; i++ {
		h := hs.histos[i]
		h.Literal[0] = 100
		h.Literal[1] = 50
		h.Red[0] = 100
		h.Blue[0] = 100
		h.Alpha[0] = 100
		h.computeHistogramCost()
	}

	initialSize := hs.Size()
	histogramCombineGreedy(hs)

	if hs.Size() >= initialSize {
		t.Errorf("greedy combining should reduce histogram count: before=%d, after=%d",
			initialSize, hs.Size())
	}
	// Identical histograms should all merge into 1.
	if hs.Size() != 1 {
		t.Errorf("expected 1 histogram after merging identical histograms, got %d", hs.Size())
	}
}

// TestHistogramCombineGreedyDifferent verifies that different histograms
// may not all merge.
func TestHistogramCombineGreedyDifferent(t *testing.T) {
	// Create histograms with very different distributions.
	hs := allocateHistoSet(3, 0)

	// Histogram 0: concentrated at symbol 0
	hs.histos[0].Literal[0] = 1000
	hs.histos[0].Red[0] = 1000
	hs.histos[0].Blue[0] = 1000
	hs.histos[0].Alpha[0] = 1000

	// Histogram 1: concentrated at symbol 255
	hs.histos[1].Literal[255] = 1000
	hs.histos[1].Red[255] = 1000
	hs.histos[1].Blue[255] = 1000
	hs.histos[1].Alpha[255] = 1000

	// Histogram 2: uniform distribution
	for i := 0; i < 256; i++ {
		hs.histos[2].Literal[i] = 10
		hs.histos[2].Red[i] = 10
		hs.histos[2].Blue[i] = 10
		hs.histos[2].Alpha[i] = 10
	}

	for i := 0; i < 3; i++ {
		hs.histos[i].computeHistogramCost()
	}

	histogramCombineGreedy(hs)

	// With very different distributions, some merges should be rejected.
	// The exact result depends on entropy calculations.
	if hs.Size() < 1 {
		t.Error("should have at least 1 histogram remaining")
	}
}

// TestHistogramCombineStochastic verifies stochastic combining reduces histogram count.
func TestHistogramCombineStochastic(t *testing.T) {
	n := 20
	hs := allocateHistoSet(n, 0)

	// Create similar histograms that should be beneficial to merge.
	for i := 0; i < n; i++ {
		h := hs.histos[i]
		h.Literal[0] = uint32(100 + i)
		h.Literal[1] = uint32(50 + i)
		h.Red[0] = 100
		h.Blue[0] = 100
		h.Alpha[0] = 100
		h.computeHistogramCost()
	}

	// Use a small target cluster size to allow stochastic to do some work.
	doGreedy := histogramCombineStochastic(hs, 5)

	// Stochastic combining should reduce the count or trigger greedy.
	if hs.Size() == n && !doGreedy {
		t.Error("stochastic combining should have reduced histogram count or returned doGreedy=true")
	}
}

// TestHistogramRemap verifies that remap assigns each original to the nearest cluster.
func TestHistogramRemap(t *testing.T) {
	// Create 4 original histograms in 2 groups.
	origHistos := make([]*Histogram, 4)
	for i := 0; i < 4; i++ {
		origHistos[i] = NewHistogram(0)
	}

	// Group A (indices 0, 1): concentrated at symbol 0.
	origHistos[0].Literal[0] = 100
	origHistos[0].Red[0] = 100
	origHistos[0].Blue[0] = 100
	origHistos[0].Alpha[0] = 100
	origHistos[0].computeHistogramCost()

	origHistos[1].Literal[0] = 90
	origHistos[1].Red[0] = 90
	origHistos[1].Blue[0] = 90
	origHistos[1].Alpha[0] = 90
	origHistos[1].computeHistogramCost()

	// Group B (indices 2, 3): concentrated at symbol 128.
	origHistos[2].Literal[128] = 100
	origHistos[2].Red[128] = 100
	origHistos[2].Blue[128] = 100
	origHistos[2].Alpha[128] = 100
	origHistos[2].computeHistogramCost()

	origHistos[3].Literal[128] = 90
	origHistos[3].Red[128] = 90
	origHistos[3].Blue[128] = 90
	origHistos[3].Alpha[128] = 90
	origHistos[3].computeHistogramCost()

	// Create 2 output clusters.
	outHisto := allocateHistoSet(2, 0)
	outHisto.histos[0].copyFrom(origHistos[0])
	outHisto.histos[1].copyFrom(origHistos[2])

	symbols := make([]uint16, 4)
	histogramRemap(origHistos, outHisto, symbols)

	// Indices 0, 1 should map to cluster 0; indices 2, 3 to cluster 1.
	if symbols[0] != 0 || symbols[1] != 0 {
		t.Errorf("group A should map to cluster 0: symbols[0]=%d, symbols[1]=%d",
			symbols[0], symbols[1])
	}
	if symbols[2] != 1 || symbols[3] != 1 {
		t.Errorf("group B should map to cluster 1: symbols[2]=%d, symbols[3]=%d",
			symbols[2], symbols[3])
	}
}

// TestHistogramCombineEntropyBin verifies entropy bin combining.
func TestHistogramCombineEntropyBin(t *testing.T) {
	n := 10
	hs := allocateHistoSet(n, 0)

	// Create histograms with the same binID, so they should be merged.
	for i := 0; i < n; i++ {
		h := hs.histos[i]
		h.Literal[0] = 100
		h.Red[0] = 100
		h.Blue[0] = 100
		h.Alpha[0] = 100
		h.binID = 0
		h.computeHistogramCost()
	}

	histogramCombineEntropyBin(hs, binSize, 16.0, false)

	// All histograms sharing the same bin should be merged.
	if hs.Size() >= n {
		t.Errorf("entropy bin combining should reduce histogram count: before=%d, after=%d",
			n, hs.Size())
	}
}

// TestHistogramCombineEntropyBinLowEffort verifies low-effort entropy bin combining.
func TestHistogramCombineEntropyBinLowEffort(t *testing.T) {
	n := 8
	hs := allocateHistoSet(n, 0)

	for i := 0; i < n; i++ {
		h := hs.histos[i]
		h.Literal[0] = uint32(100 + i)
		h.Red[0] = 100
		h.Blue[0] = 100
		h.Alpha[0] = 100
		h.binID = uint16(i % numPartitions)
		h.computeHistogramCost()
	}

	histogramCombineEntropyBin(hs, numPartitions, 16.0, true)

	// Low-effort mode should merge all histograms sharing a bin.
	if hs.Size() >= n {
		t.Errorf("low-effort combining should reduce count: before=%d, after=%d",
			n, hs.Size())
	}
}

// TestHistoQueuePush verifies queue push with capacity limit.
func TestHistoQueuePush(t *testing.T) {
	t.Run("respects maxSize", func(t *testing.T) {
		// Create two histograms that would produce a beneficial merge.
		histograms := make([]*Histogram, 2)
		for i := range histograms {
			histograms[i] = NewHistogram(0)
			histograms[i].Literal[0] = 100
			histograms[i].Red[0] = 100
			histograms[i].Blue[0] = 100
			histograms[i].Alpha[0] = 100
			histograms[i].computeHistogramCost()
		}

		var q histoQueue
		q.maxSize = 1

		// First push should succeed.
		q.push(histograms, 0, 1, 0)
		if q.size() > 1 {
			t.Errorf("queue should have at most 1 element, got %d", q.size())
		}

		// If first push was accepted, second push should be rejected.
		if q.size() == 1 {
			q.push(histograms, 0, 1, 0)
			if q.size() > 1 {
				t.Errorf("queue should not exceed maxSize=1, got %d", q.size())
			}
		}
	})

	t.Run("unlimited when maxSize is 0", func(t *testing.T) {
		histograms := make([]*Histogram, 2)
		for i := range histograms {
			histograms[i] = NewHistogram(0)
			histograms[i].Literal[0] = 100
			histograms[i].Red[0] = 100
			histograms[i].Blue[0] = 100
			histograms[i].Alpha[0] = 100
			histograms[i].computeHistogramCost()
		}

		var q histoQueue
		q.maxSize = 0 // unlimited

		q.push(histograms, 0, 1, 0)
		// No capacity error expected.
	})
}

// TestLehmerRand verifies the Lehmer PRNG produces expected values.
func TestLehmerRand(t *testing.T) {
	var seed uint32 = 1
	v1 := lehmerRand(&seed)
	if v1 != 48271 {
		t.Errorf("first lehmerRand should be 48271, got %d", v1)
	}
	v2 := lehmerRand(&seed)
	if v2 == v1 {
		t.Error("second lehmerRand should differ from first")
	}
}

// TestGetCombineCostFactor verifies cost factor computation.
func TestGetCombineCostFactor(t *testing.T) {
	tests := []struct {
		histoSize int
		quality   int
		want      float64
	}{
		{100, 100, 16.0},
		{100, 50, 8.0},   // quality<=50 halves: 16/2=8
		{600, 80, 4.0},   // >256 halves once (8), >512 halves again (4)
		{2000, 50, 1.0},  // >256,>512,>1024 (2) + quality<=50 (1)
	}
	for _, tt := range tests {
		got := getCombineCostFactor(tt.histoSize, tt.quality)
		if got != tt.want {
			t.Errorf("getCombineCostFactor(%d, %d) = %f, want %f",
				tt.histoSize, tt.quality, got, tt.want)
		}
	}
}

// TestGetHistoImageSymbols verifies the full clustering pipeline.
func TestGetHistoImageSymbols(t *testing.T) {
	width := 32
	height := 32

	// Create simple backward references: all literal pixels.
	refs := NewBackwardRefs(width * height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Create a pixel with some variation.
			r := uint32(x * 8)
			g := uint32(y * 8)
			b := uint32(128)
			a := uint32(255)
			argb := (a << 24) | (r << 16) | (g << 8) | b
			refs.refs = append(refs.refs, LiteralPixel(argb))
		}
	}

	symbols, histoSet := GetHistoImageSymbols(width, height, refs, 75, 3, 0, nil)

	if histoSet.Size() < 1 {
		t.Error("should have at least 1 histogram")
	}
	if len(symbols) == 0 {
		t.Error("symbols should not be empty")
	}

	// Verify all symbols reference valid histograms.
	for i, s := range symbols {
		if int(s) >= histoSet.Size() {
			t.Errorf("symbol[%d]=%d exceeds histogram count %d", i, s, histoSet.Size())
		}
	}
}
