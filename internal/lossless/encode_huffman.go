package lossless

import (
	"container/heap"
	"sort"

	"github.com/deepteams/webp/internal/bitio"
)

// HuffmanTreeToken represents a single token in the code-length sequence.
// code is one of 0..15 (literal code length), 16 (repeat previous),
// 17 (repeat zero 3..10 times), or 18 (repeat zero 11..138 times).
// extraBits carries the extra-bits value for codes 16, 17, 18.
type HuffmanTreeToken struct {
	code      uint8
	extraBits uint8
}

// HuffmanTreeCode holds a complete Huffman code for encoding: for each symbol
// in the alphabet, it stores the canonical code length and the bit-reversed
// codeword.
type HuffmanTreeCode struct {
	NumSymbols  int
	CodeLengths []uint8
	Codes       []uint16
}

// huffmanTreeNode is an internal node (or leaf) used while building
// a Huffman tree from symbol frequencies.
type huffmanTreeNode struct {
	totalCount uint32
	value      int // symbol index for leaves, -1 for internal nodes
	left       int // pool index, -1 for none
	right      int // pool index, -1 for none
}

// ---------------------------------------------------------------------------
// Priority queue for tree construction
// ---------------------------------------------------------------------------

type nodeHeap struct {
	pool    []huffmanTreeNode
	indices []int // indices into pool
}

func (h *nodeHeap) Len() int { return len(h.indices) }

func (h *nodeHeap) Less(i, j int) bool {
	a, b := h.pool[h.indices[i]], h.pool[h.indices[j]]
	if a.totalCount != b.totalCount {
		return a.totalCount < b.totalCount
	}
	return h.indices[i] < h.indices[j]
}

func (h *nodeHeap) Swap(i, j int) {
	h.indices[i], h.indices[j] = h.indices[j], h.indices[i]
}

func (h *nodeHeap) Push(x any) {
	h.indices = append(h.indices, x.(int))
}

func (h *nodeHeap) Pop() any {
	old := h.indices
	n := len(old)
	idx := old[n-1]
	h.indices = old[:n-1]
	return idx
}

// ---------------------------------------------------------------------------
// CreateHuffmanTree builds canonical Huffman codes from a symbol histogram.
// ---------------------------------------------------------------------------

// CreateHuffmanTree builds a HuffmanTreeCode from the given histogram
// (symbol frequencies). codeLengthLimit caps the maximum code length
// (typically MaxAllowedCodeLength = 15).
func CreateHuffmanTree(histogram []uint32, codeLengthLimit int) *HuffmanTreeCode {
	numSymbols := len(histogram)
	tree := &HuffmanTreeCode{
		NumSymbols:  numSymbols,
		CodeLengths: make([]uint8, numSymbols),
		Codes:       make([]uint16, numSymbols),
	}

	// Count non-zero symbols and collect them.
	type symCount struct {
		symbol int
		count  uint32
	}
	var nonZero []symCount
	for i, c := range histogram {
		if c > 0 {
			nonZero = append(nonZero, symCount{i, c})
		}
	}

	switch len(nonZero) {
	case 0:
		// Empty histogram: all code lengths stay zero.
		return tree
	case 1:
		// Single symbol: assign code length 1.
		tree.CodeLengths[nonZero[0].symbol] = 1
		generateCanonicalCodes(tree)
		return tree
	case 2:
		// Two symbols: each gets code length 1.
		tree.CodeLengths[nonZero[0].symbol] = 1
		tree.CodeLengths[nonZero[1].symbol] = 1
		generateCanonicalCodes(tree)
		return tree
	}

	// General case: build a Huffman tree using a min-heap and extract depths.
	buildTreeAndExtractLengths(histogram, numSymbols, codeLengthLimit, tree.CodeLengths)
	generateCanonicalCodes(tree)
	return tree
}

// buildTreeAndExtractLengths constructs the Huffman tree from frequencies
// and writes the resulting code lengths into codeLengths. If any code length
// exceeds the limit, it doubles count_min and rebuilds, matching the C
// reference GenerateOptimalTree in huffman_encode_utils.c.
func buildTreeAndExtractLengths(histogram []uint32, numSymbols, limit int, codeLengths []uint8) {
	// Count non-zero symbols.
	treeSize := 0
	for i := 0; i < numSymbols; i++ {
		if histogram[i] != 0 {
			treeSize++
		}
	}
	if treeSize == 0 {
		return
	}

	// Matching C: iterate with increasing count_min until depths fit.
	for countMin := uint32(1); ; countMin *= 2 {
		// Clear code lengths from any previous iteration.
		for i := range codeLengths {
			codeLengths[i] = 0
		}

		// Build leaf nodes with counts clamped to at least count_min.
		maxNodes := 2*numSymbols + 1
		pool := make([]huffmanTreeNode, 0, maxNodes)
		h := &nodeHeap{pool: pool}
		for sym := 0; sym < numSymbols; sym++ {
			if histogram[sym] != 0 {
				count := histogram[sym]
				if count < countMin {
					count = countMin
				}
				idx := len(h.pool)
				h.pool = append(h.pool, huffmanTreeNode{
					totalCount: count,
					value:      sym,
					left:       -1,
					right:      -1,
				})
				h.indices = append(h.indices, idx)
			}
		}

		if len(h.indices) == 1 {
			// Trivial case: single symbol.
			codeLengths[h.pool[h.indices[0]].value] = 1
			return
		}

		heap.Init(h)

		// Merge nodes until a single root remains.
		for h.Len() > 1 {
			leftIdx := heap.Pop(h).(int)
			rightIdx := heap.Pop(h).(int)
			parentIdx := len(h.pool)
			h.pool = append(h.pool, huffmanTreeNode{
				totalCount: h.pool[leftIdx].totalCount + h.pool[rightIdx].totalCount,
				value:      -1,
				left:       leftIdx,
				right:      rightIdx,
			})
			heap.Push(h, parentIdx)
		}

		rootIdx := h.indices[0]

		// Walk the tree to extract code lengths.
		assignCodeLengths(h.pool, rootIdx, 0, codeLengths)

		// Check if all code lengths are within the limit.
		maxDepth := 0
		for _, cl := range codeLengths {
			if int(cl) > maxDepth {
				maxDepth = int(cl)
			}
		}
		if maxDepth <= limit {
			return
		}
		// Depth exceeded: double count_min and retry.
	}
}

// assignCodeLengths performs a recursive DFS to set each leaf symbol's
// code length to its depth in the tree.
func assignCodeLengths(pool []huffmanTreeNode, nodeIdx, depth int, codeLengths []uint8) {
	node := &pool[nodeIdx]
	if node.value >= 0 {
		// Leaf node.
		codeLengths[node.value] = uint8(depth)
		return
	}
	if node.left >= 0 {
		assignCodeLengths(pool, node.left, depth+1, codeLengths)
	}
	if node.right >= 0 {
		assignCodeLengths(pool, node.right, depth+1, codeLengths)
	}
}

// ---------------------------------------------------------------------------
// Canonical code generation
// ---------------------------------------------------------------------------

// generateCanonicalCodes computes bit-reversed canonical codes from the code
// lengths stored in tree.CodeLengths.
func generateCanonicalCodes(tree *HuffmanTreeCode) {
	n := tree.NumSymbols

	// Find the maximum code length.
	maxLen := 0
	for _, cl := range tree.CodeLengths {
		if int(cl) > maxLen {
			maxLen = int(cl)
		}
	}
	if maxLen == 0 {
		return
	}

	// Sort symbols by (code_length, symbol) for canonical assignment.
	type symLen struct {
		symbol int
		length uint8
	}
	symbols := make([]symLen, 0, n)
	for i := 0; i < n; i++ {
		if tree.CodeLengths[i] > 0 {
			symbols = append(symbols, symLen{i, tree.CodeLengths[i]})
		}
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].length != symbols[j].length {
			return symbols[i].length < symbols[j].length
		}
		return symbols[i].symbol < symbols[j].symbol
	})

	// Assign canonical codes and bit-reverse them.
	code := uint32(0)
	prevLen := uint8(0)
	for _, s := range symbols {
		if s.length > prevLen {
			code <<= (s.length - prevLen)
			prevLen = s.length
		}
		tree.Codes[s.symbol] = reverseBits(code, int(s.length))
		code++
	}
}

// reverseBits reverses the lower nBits of v.
func reverseBits(v uint32, nBits int) uint16 {
	var result uint32
	for i := 0; i < nBits; i++ {
		result = (result << 1) | (v & 1)
		v >>= 1
	}
	return uint16(result)
}

// ---------------------------------------------------------------------------
// OptimizeHuffmanForRle pre-processes a histogram to improve RLE compression
// of the resulting code-length sequence.
// ---------------------------------------------------------------------------

// valuesShouldBeCollapsedToStrideAverage returns true when two counts are
// close enough to be merged into a single RLE stride. Matches the C reference
// ValuesShouldBeCollapsedToStrideAverage: abs(a - b) < 4.
func valuesShouldBeCollapsedToStrideAverage(a, b uint32) bool {
	if a > b {
		return a-b < 4
	}
	return b-a < 4
}

// OptimizeHuffmanForRle smooths out the histogram to produce longer runs
// of identical code lengths. This matches the C reference OptimizeHuffmanForRle
// in libwebp/src/utils/huffman_encode_utils.c exactly:
//   - Step 1: trim trailing zeros
//   - Step 2: mark strides of identical values as good_for_rle
//     (>=5 for zeros, >=7 for non-zeros)
//   - Step 3: collapse similar-valued strides using arithmetic mean
func OptimizeHuffmanForRle(counts []uint32) []uint32 {
	length := len(counts)
	if length == 0 {
		return counts
	}

	// Step 1: trim trailing zeros.
	for length > 0 && counts[length-1] == 0 {
		length--
	}
	if length == 0 {
		return counts
	}

	// Step 2: mark positions that already form good RLE strides.
	goodForRle := make([]bool, length)
	{
		symbol := counts[0]
		stride := 0
		for i := 0; i <= length; i++ {
			if i == length || counts[i] != symbol {
				if (symbol == 0 && stride >= 5) || (symbol != 0 && stride >= 7) {
					for k := 0; k < stride; k++ {
						goodForRle[i-k-1] = true
					}
				}
				stride = 1
				if i != length {
					symbol = counts[i]
				}
			} else {
				stride++
			}
		}
	}

	// Step 3: collapse similar-valued strides using arithmetic mean.
	{
		stride := uint32(0)
		limit := counts[0]
		sum := uint32(0)
		for i := 0; i <= length; i++ {
			if i == length || goodForRle[i] ||
				(i != 0 && goodForRle[i-1]) ||
				!valuesShouldBeCollapsedToStrideAverage(counts[i], limit) {
				if stride >= 4 || (stride >= 3 && sum == 0) {
					count := (sum + stride/2) / stride
					if count < 1 {
						count = 1
					}
					if sum == 0 {
						// Don't upgrade an all-zeros stride to ones.
						count = 0
					}
					for k := uint32(0); k < stride; k++ {
						counts[i-int(k)-1] = count
					}
				}
				stride = 0
				sum = 0
				if i < length-3 {
					limit = (counts[i] + counts[i+1] + counts[i+2] + counts[i+3] + 2) / 4
				} else if i < length {
					limit = counts[i]
				} else {
					limit = 0
				}
			}
			stride++
			if i != length {
				sum += counts[i]
				if stride >= 4 {
					limit = (sum + stride/2) / stride
				}
			}
		}
	}

	return counts
}

// ---------------------------------------------------------------------------
// BuildCodeLengthTokens converts a code-length array into a sequence of
// RLE-encoded tokens (codes 0..18).
// ---------------------------------------------------------------------------

// BuildCodeLengthTokens encodes the given code lengths into a sequence of
// HuffmanTreeTokens using the VP8L code-length RLE scheme:
//   - 0..15: literal code length
//   - 16: repeat previous code length 3..6 times (2 extra bits)
//   - 17: repeat zero 3..10 times (3 extra bits)
//   - 18: repeat zero 11..138 times (7 extra bits)
func BuildCodeLengthTokens(codeLengths []uint8) []HuffmanTreeToken {
	n := len(codeLengths)
	var tokens []HuffmanTreeToken

	// prevValue is initialized to 8, matching the C reference
	// (VP8LCreateCompressedHuffmanTree). This means the first code-length
	// value of 8 can use repeat-previous encoding (code 16) instead of a
	// literal, producing a shorter code-length sequence.
	prevValue := uint8(8)

	i := 0
	for i < n {
		value := codeLengths[i]

		// Count consecutive identical values.
		k := i + 1
		for k < n && codeLengths[k] == value {
			k++
		}
		runs := k - i
		i = k

		if value == 0 {
			tokens = codeRepeatedZeros(tokens, runs)
		} else {
			tokens = codeRepeatedValues(tokens, runs, value, prevValue)
			prevValue = value
		}
	}

	return tokens
}

// codeRepeatedZeros encodes a run of zeros using codes 0, 17, and 18.
func codeRepeatedZeros(tokens []HuffmanTreeToken, repetitions int) []HuffmanTreeToken {
	for repetitions >= 1 {
		if repetitions < 3 {
			for i := 0; i < repetitions; i++ {
				tokens = append(tokens, HuffmanTreeToken{code: 0, extraBits: 0})
			}
			break
		} else if repetitions < 11 {
			tokens = append(tokens, HuffmanTreeToken{
				code:      17,
				extraBits: uint8(repetitions - 3),
			})
			break
		} else if repetitions < 139 {
			tokens = append(tokens, HuffmanTreeToken{
				code:      18,
				extraBits: uint8(repetitions - 11),
			})
			break
		} else {
			tokens = append(tokens, HuffmanTreeToken{
				code:      18,
				extraBits: 0x7f, // 138 repeated 0s
			})
			repetitions -= 138
		}
	}
	return tokens
}

// codeRepeatedValues encodes a run of identical non-zero values using
// literal codes and code 16 (repeat previous). If the value matches
// prevValue, the initial literal is skipped and repeat encoding is used
// directly, matching the C reference CodeRepeatedValues.
func codeRepeatedValues(tokens []HuffmanTreeToken, repetitions int, value, prevValue uint8) []HuffmanTreeToken {
	if value != prevValue {
		tokens = append(tokens, HuffmanTreeToken{code: value, extraBits: 0})
		repetitions--
	}
	for repetitions >= 1 {
		if repetitions < 3 {
			for i := 0; i < repetitions; i++ {
				tokens = append(tokens, HuffmanTreeToken{code: value, extraBits: 0})
			}
			break
		} else if repetitions < 7 {
			tokens = append(tokens, HuffmanTreeToken{
				code:      16,
				extraBits: uint8(repetitions - 3),
			})
			break
		} else {
			tokens = append(tokens, HuffmanTreeToken{
				code:      16,
				extraBits: 3, // repeat 6 times
			})
			repetitions -= 6
		}
	}
	return tokens
}

// ---------------------------------------------------------------------------
// Bitstream writing functions
// ---------------------------------------------------------------------------

// StoreHuffmanTreeOfHuffmanTreeToBitMask writes the code-length Huffman tree
// header into the bitstream. It outputs the trimmed count of code-length
// codes followed by each code length (3 bits each) in CodeLengthCodeOrder.
func StoreHuffmanTreeOfHuffmanTreeToBitMask(bw *bitio.LosslessWriter, codeLengthBitDepth []uint8) {
	// Find the last non-zero code length in CodeLengthCodeOrder.
	// Minimum number of codes to write is 4.
	numCodes := 4
	for i := CodeLengthCodes - 1; i >= 4; i-- {
		if codeLengthBitDepth[CodeLengthCodeOrder[i]] != 0 {
			numCodes = i + 1
			break
		}
	}

	bw.WriteBits(uint32(numCodes-4), 4)

	for i := 0; i < numCodes; i++ {
		bw.WriteBits(uint32(codeLengthBitDepth[CodeLengthCodeOrder[i]]), 3)
	}
}

// StoreHuffmanTreeToBitMask encodes the first numTokens code-length tokens
// using the given code-length Huffman tree and writes the result to the bitstream.
func StoreHuffmanTreeToBitMask(bw *bitio.LosslessWriter, tokens []HuffmanTreeToken, numTokens int, codeLengthTree *HuffmanTreeCode) {
	for i := 0; i < numTokens; i++ {
		tok := tokens[i]
		code := tok.code
		// Write the Huffman code for this code-length symbol.
		bw.WriteBits(uint32(codeLengthTree.Codes[code]), int(codeLengthTree.CodeLengths[code]))

		// Write extra bits for repeat codes (16, 17, 18).
		if code >= CodeLengthRepeatCode {
			extraIdx := code - CodeLengthRepeatCode
			nExtraBits := int(CodeLengthExtraBits[extraIdx])
			bw.WriteBits(uint32(tok.extraBits), nExtraBits)
		}
	}
}

// StoreHuffmanCode writes a complete Huffman code to the bitstream.
// It uses the simple encoding for 1 or 2 unique symbols (provided both
// symbols fit in 8 bits, i.e. < 256), and the full code-length tree
// encoding otherwise. This matches the C reference StoreHuffmanCode
// which checks symbols[0] < kMaxSymbol && symbols[1] < kMaxSymbol
// before using the simple path.
func StoreHuffmanCode(bw *bitio.LosslessWriter, tree *HuffmanTreeCode) {
	const kMaxSymbol = 256 // simple encoding uses at most 8-bit symbols

	// Collect unique symbols (those with non-zero code lengths).
	var uniqueSymbols []int
	for i := 0; i < tree.NumSymbols; i++ {
		if tree.CodeLengths[i] > 0 {
			uniqueSymbols = append(uniqueSymbols, i)
		}
	}

	numUnique := len(uniqueSymbols)

	if numUnique == 0 {
		// Empty tree: emit minimal simple code (matches C reference).
		storeSimpleHuffmanCode(bw, tree, uniqueSymbols)
		return
	}

	// Simple encoding is only valid when all symbols fit in 8 bits.
	if numUnique <= 2 {
		allFit := true
		for _, s := range uniqueSymbols {
			if s >= kMaxSymbol {
				allFit = false
				break
			}
		}
		if allFit {
			storeSimpleHuffmanCode(bw, tree, uniqueSymbols)
			return
		}
	}

	storeFullHuffmanCode(bw, tree)
}

// storeSimpleHuffmanCode writes 1- or 2-symbol simple Huffman codes.
func storeSimpleHuffmanCode(bw *bitio.LosslessWriter, tree *HuffmanTreeCode, symbols []int) {
	bw.WriteBits(1, 1) // is_simple = 1

	numSymbols := len(symbols)
	if numSymbols == 0 {
		// Edge case: empty tree. Write as 1 symbol with value 0.
		bw.WriteBits(0, 1) // num_symbols - 1 = 0
		bw.WriteBits(0, 1) // first_symbol_len_code = 0
		bw.WriteBits(0, 1) // symbol = 0
		return
	}

	if numSymbols == 1 {
		bw.WriteBits(0, 1) // num_symbols - 1 = 0
		sym := symbols[0]
		if sym < 2 {
			bw.WriteBits(0, 1) // first_symbol_len_code = 0 -> 1 bit symbol
			bw.WriteBits(uint32(sym), 1)
		} else {
			bw.WriteBits(1, 1) // first_symbol_len_code = 1 -> 8 bit symbol
			bw.WriteBits(uint32(sym), 8)
		}
		return
	}

	// numSymbols == 2
	bw.WriteBits(1, 1) // num_symbols - 1 = 1

	// Sort symbols so the smaller one comes first.
	sym0, sym1 := symbols[0], symbols[1]
	if sym0 > sym1 {
		sym0, sym1 = sym1, sym0
	}

	// First symbol: 1-bit if value <= 1, 8-bit otherwise.
	if sym0 <= 1 {
		bw.WriteBits(0, 1) // first_symbol_len_code = 0 -> 1-bit symbol
		bw.WriteBits(uint32(sym0), 1)
	} else {
		bw.WriteBits(1, 1) // first_symbol_len_code = 1 -> 8-bit symbol
		bw.WriteBits(uint32(sym0), 8)
	}
	// Second symbol: ALWAYS 8 bits (decoder always reads 8 bits).
	bw.WriteBits(uint32(sym1), 8)
}

// clearHuffmanTreeIfOnlyOneSymbol zeroes out code lengths and codes if the
// tree has at most one symbol with a non-zero code length. This matches the
// C reference ClearHuffmanTreeIfOnlyOneSymbol: when there is only one code-
// length code, the decoder reconstructs it from an all-zero code-length table.
func clearHuffmanTreeIfOnlyOneSymbol(tree *HuffmanTreeCode) {
	count := 0
	for _, cl := range tree.CodeLengths {
		if cl != 0 {
			count++
			if count > 1 {
				return
			}
		}
	}
	// 0 or 1 symbol -- clear everything.
	for i := range tree.CodeLengths {
		tree.CodeLengths[i] = 0
		tree.Codes[i] = 0
	}
}

// storeFullHuffmanCode writes a full (non-simple) Huffman code using the
// code-length tree approach. Matches the C reference StoreFullHuffmanCode
// including the write_trimmed_length flag and trailing-zero trimming.
func storeFullHuffmanCode(bw *bitio.LosslessWriter, tree *HuffmanTreeCode) {
	bw.WriteBits(0, 1) // is_simple = 0

	// Build code-length tokens from the tree's code lengths.
	tokens := BuildCodeLengthTokens(tree.CodeLengths)
	numTokens := len(tokens)

	// Build a histogram of the token codes (0..18).
	var tokenHistogram [CodeLengthCodes]uint32
	for _, tok := range tokens {
		tokenHistogram[tok.code]++
	}

	// Create a Huffman tree for the code-length codes themselves.
	codeLengthTree := CreateHuffmanTree(tokenHistogram[:], 7)

	// Write the code-length tree header (num_codes + 3-bit code lengths).
	StoreHuffmanTreeOfHuffmanTreeToBitMask(bw, codeLengthTree.CodeLengths)

	// If the code-length tree has only one symbol, clear it (the decoder
	// handles this case with all-zero code lengths).
	clearHuffmanTreeIfOnlyOneSymbol(codeLengthTree)

	// Compute trimmed_length by removing trailing zero-producing tokens,
	// and count the bits that would be saved by trimming.
	trailingZeroBits := 0
	trimmedLength := numTokens
	for i := numTokens - 1; i >= 0; i-- {
		ix := tokens[i].code
		if ix == 0 || ix == 17 || ix == 18 {
			trimmedLength--
			trailingZeroBits += int(codeLengthTree.CodeLengths[ix])
			if ix == 17 {
				trailingZeroBits += 3
			} else if ix == 18 {
				trailingZeroBits += 7
			}
		} else {
			break
		}
	}

	// Only write trimmed length if it saves enough bits (> 12).
	writeTrimmedLength := trimmedLength > 1 && trailingZeroBits > 12
	length := numTokens
	if writeTrimmedLength {
		length = trimmedLength
	}

	// Write the write_trimmed_length flag.
	if writeTrimmedLength {
		bw.WriteBits(1, 1)
		if trimmedLength == 2 {
			bw.WriteBits(0, 3+2) // nbitpairs=1 (written as 0), trimmed_length=2 (written as 0)
		} else {
			nbits := bitsLog2Floor(trimmedLength - 2)
			nbitpairs := nbits/2 + 1
			bw.WriteBits(uint32(nbitpairs-1), 3)
			bw.WriteBits(uint32(trimmedLength-2), nbitpairs*2)
		}
	} else {
		bw.WriteBits(0, 1)
	}

	// Write the code-length tokens using the code-length Huffman tree.
	StoreHuffmanTreeToBitMask(bw, tokens, length, codeLengthTree)
}

// ---------------------------------------------------------------------------
// Utility: count unique symbols in a Huffman tree code.
// ---------------------------------------------------------------------------

// CountUniqueSymbols returns the number of symbols with non-zero code lengths.
func CountUniqueSymbols(tree *HuffmanTreeCode) int {
	count := 0
	for _, cl := range tree.CodeLengths {
		if cl > 0 {
			count++
		}
	}
	return count
}

// HuffmanBitCost computes the total bit cost of encoding all symbols
// in histogram using the given Huffman tree.
func HuffmanBitCost(histogram []uint32, tree *HuffmanTreeCode) float64 {
	cost := float64(0)
	for i, count := range histogram {
		if count > 0 && i < len(tree.CodeLengths) {
			cost += float64(count) * float64(tree.CodeLengths[i])
		}
	}
	return cost
}
