package lossless

import "errors"

// HuffmanCode is a single entry in a Huffman lookup table.
// Bits is the number of bits consumed; Value is the decoded symbol or
// a sub-table offset for codes longer than the root table.
type HuffmanCode struct {
	Bits  uint8
	Value uint16
}

// HuffmanCode32 is a 32-bit variant used for packed table entries.
type HuffmanCode32 struct {
	Bits  int
	Value uint32
}

// HTreeGroup bundles the 5 Huffman trees needed for a single meta-code
// (green+length, alpha, red, blue, distance) together with fast-path
// optimisation flags.
type HTreeGroup struct {
	// HTrees holds the decoded Huffman lookup tables. Index order follows
	// HuffIndex: green=0, red=1, blue=2, alpha=3, distance=4.
	HTrees [HuffmanCodesPerMetaCode][]HuffmanCode

	// IsTrivialLiteral is true when the red, blue, and alpha trees each
	// contain only a single code (trivial).
	IsTrivialLiteral bool

	// LiteralARB stores the packed ARGB value of the trivial red/blue/alpha
	// literals (green channel is zero).
	LiteralARB uint32

	// IsTrivialCode is true when IsTrivialLiteral is true AND the green
	// tree also has only a single code.
	IsTrivialCode bool

	// UsePackedTable is true when all literal symbols fit into a compact
	// lookup table (PackedTable).
	UsePackedTable bool

	// PackedTable is the compact lookup table used when UsePackedTable is true.
	PackedTable [HuffmanPackedTableSize]HuffmanCode32
}

// Errors returned by BuildHuffmanTable.
var (
	ErrInvalidTree     = errors.New("lossless: invalid Huffman tree")
	ErrEmptyCodeLengths = errors.New("lossless: all code lengths are zero")
)

// BuildHuffmanTable constructs a two-level Huffman lookup table from an
// array of code lengths (indexed by symbol). rootBits determines the size
// of the first-level table (typically HuffmanTableBits = 8).
//
// The returned slice contains the root table followed by any required
// second-level sub-tables. The function returns an error if the code
// lengths do not form a valid Huffman tree.
//
// This is a pure-Go port of libwebp's static BuildHuffmanTable in
// huffman_utils.c.
// HuffmanTableScratch holds optional pre-allocated buffers for BuildHuffmanTable.
type HuffmanTableScratch struct {
	sorted    []uint16     // reusable sorted symbols buffer
	tableSlab []HuffmanCode // slab for table allocation
	slabOff   int           // current offset in slab
}

func BuildHuffmanTable(rootBits int, codeLengths []int) ([]HuffmanCode, error) {
	return BuildHuffmanTableScratch(rootBits, codeLengths, nil)
}

// BuildHuffmanTableScratch is like BuildHuffmanTable but accepts optional
// scratch buffers to reduce allocations.
func BuildHuffmanTableScratch(rootBits int, codeLengths []int, scratch *HuffmanTableScratch) ([]HuffmanCode, error) {
	codeLengthsSize := len(codeLengths)
	if codeLengthsSize == 0 {
		return nil, ErrEmptyCodeLengths
	}

	// First pass: compute the total table size without writing.
	totalSize := buildHuffmanTableSize(rootBits, codeLengths)
	if totalSize == 0 {
		return nil, ErrInvalidTree
	}

	// Allocate table from slab if available, otherwise make a new slice.
	var table []HuffmanCode
	if scratch != nil && scratch.slabOff+totalSize <= len(scratch.tableSlab) {
		table = scratch.tableSlab[scratch.slabOff : scratch.slabOff+totalSize : scratch.slabOff+totalSize]
		scratch.slabOff += totalSize
		// Zero out reused slab segment.
		for i := range table {
			table[i] = HuffmanCode{}
		}
	} else {
		table = make([]HuffmanCode, totalSize)
	}

	// Sort symbols by code length. Reuse buffer if available.
	var sorted []uint16
	if scratch != nil && cap(scratch.sorted) >= codeLengthsSize {
		sorted = scratch.sorted[:codeLengthsSize]
		for i := range sorted {
			sorted[i] = 0
		}
	} else {
		sorted = make([]uint16, codeLengthsSize)
		if scratch != nil {
			scratch.sorted = sorted
		}
	}

	var count [MaxAllowedCodeLength + 1]int
	for _, cl := range codeLengths {
		if cl > MaxAllowedCodeLength {
			return nil, ErrInvalidTree
		}
		count[cl]++
	}
	if count[0] == codeLengthsSize {
		return nil, ErrEmptyCodeLengths
	}

	var offset [MaxAllowedCodeLength + 1]int
	offset[1] = 0
	for l := 1; l < MaxAllowedCodeLength; l++ {
		if count[l] > (1 << l) {
			return nil, ErrInvalidTree
		}
		offset[l+1] = offset[l] + count[l]
	}
	for symbol, cl := range codeLengths {
		if cl > 0 {
			if offset[cl] >= codeLengthsSize {
				return nil, ErrInvalidTree
			}
			sorted[offset[cl]] = uint16(symbol)
			offset[cl]++
		}
	}

	// Special case: only one non-zero code length symbol.
	if offset[MaxAllowedCodeLength] == 1 {
		code := HuffmanCode{Bits: 0, Value: sorted[0]}
		replicateValue(table, 1, totalSize, code)
		return table, nil
	}

	// Re-compute count histogram (the first pass consumed some offsets).
	for i := range count {
		count[i] = 0
	}
	for _, cl := range codeLengths {
		count[cl]++
	}

	// Fill root table and second-level sub-tables.
	rootTable := table
	tableOff := 0
	tableBits := rootBits
	tableSize := 1 << tableBits
	rootSize := tableSize
	_ = rootSize

	var low uint32 = 0xffffffff
	mask := uint32(tableSize - 1)
	var key uint32
	numNodes := 1
	numOpen := 1
	symbol := 0

	// Fill root table entries for codes with length <= rootBits.
	for l, step := 1, 2; l <= rootBits; l, step = l+1, step<<1 {
		numOpen <<= 1
		numNodes += numOpen
		numOpen -= count[l]
		if numOpen < 0 {
			return nil, ErrInvalidTree
		}
		for ; count[l] > 0; count[l]-- {
			code := HuffmanCode{Bits: uint8(l), Value: sorted[symbol]}
			symbol++
			replicateValue(rootTable[key:], step, tableSize, code)
			key = getNextKey(key, l)
		}
	}

	// Fill second-level sub-tables for codes with length > rootBits.
	for l, step := rootBits+1, 2; l <= MaxAllowedCodeLength; l, step = l+1, step<<1 {
		numOpen <<= 1
		numNodes += numOpen
		numOpen -= count[l]
		if numOpen < 0 {
			return nil, ErrInvalidTree
		}
		for ; count[l] > 0; count[l]-- {
			if (key & mask) != low {
				tableOff += tableSize
				tableBits = nextTableBitSize(count[:], l, rootBits)
				tableSize = 1 << tableBits
				// Bounds check: ensure sub-table fits in allocated table.
				if tableOff+tableSize > totalSize {
					return nil, ErrInvalidTree
				}
				low = key & mask
				rootTable[low] = HuffmanCode{
					Bits:  uint8(tableBits + rootBits),
					Value: uint16(tableOff),
				}
			}
			code := HuffmanCode{
				Bits:  uint8(l - rootBits),
				Value: sorted[symbol],
			}
			symbol++
			off := tableOff + int(key>>uint(rootBits))
			if off >= totalSize {
				return nil, ErrInvalidTree
			}
			replicateValue(table[off:], step, tableSize, code)
			key = getNextKey(key, l)
		}
	}

	// Verify the tree is complete.
	if numNodes != 2*offset[MaxAllowedCodeLength]-1 {
		return nil, ErrInvalidTree
	}

	return table, nil
}

// buildHuffmanTableSize computes the total table size without writing any
// entries. Returns 0 on invalid input (matching the C dual-pass approach).
func buildHuffmanTableSize(rootBits int, codeLengths []int) int {
	codeLengthsSize := len(codeLengths)
	totalSize := 1 << rootBits

	var count [MaxAllowedCodeLength + 1]int
	for _, cl := range codeLengths {
		if cl > MaxAllowedCodeLength {
			return 0
		}
		count[cl]++
	}
	if count[0] == codeLengthsSize {
		return 0
	}

	var offset [MaxAllowedCodeLength + 1]int
	offset[1] = 0
	for l := 1; l < MaxAllowedCodeLength; l++ {
		if count[l] > (1 << l) {
			return 0
		}
		offset[l+1] = offset[l] + count[l]
	}
	for _, cl := range codeLengths {
		if cl > 0 {
			if offset[cl] >= codeLengthsSize {
				return 0
			}
			offset[cl]++
		}
	}

	// Single-value tree.
	if offset[MaxAllowedCodeLength] == 1 {
		return totalSize
	}

	mask := uint32(totalSize - 1)
	var key uint32
	numNodes := 1
	numOpen := 1

	for l, step := 1, 2; l <= rootBits; l, step = l+1, step<<1 {
		numOpen <<= 1
		numNodes += numOpen
		numOpen -= count[l]
		if numOpen < 0 {
			return 0
		}
		for ; count[l] > 0; count[l]-- {
			key = getNextKey(key, l)
		}
	}

	var low uint32 = 0xffffffff
	tableSize := 1 << rootBits // initial table size (root)
	for l := rootBits + 1; l <= MaxAllowedCodeLength; l++ {
		numOpen <<= 1
		numNodes += numOpen
		numOpen -= count[l]
		if numOpen < 0 {
			return 0
		}
		for ; count[l] > 0; count[l]-- {
			if (key & mask) != low {
				tableSize = 1 << nextTableBitSize(count[:], l, rootBits)
				totalSize += tableSize
				low = key & mask
			}
			key = getNextKey(key, l)
		}
	}

	if numNodes != 2*offset[MaxAllowedCodeLength]-1 {
		return 0
	}

	return totalSize
}

// getNextKey returns reverse(reverse(key, len) + 1, len).
func getNextKey(key uint32, length int) uint32 {
	step := uint32(1) << (length - 1)
	for key&step != 0 {
		step >>= 1
	}
	if step != 0 {
		return (key & (step - 1)) + step
	}
	return key
}

// replicateValue fills table[0], table[step], ..., table[end-step] with code.
func replicateValue(table []HuffmanCode, step, end int, code HuffmanCode) {
	for i := end - step; i >= 0; i -= step {
		table[i] = code
	}
}

// nextTableBitSize returns the width (in bits) of the next second-level
// sub-table, ensuring it is large enough to cover all remaining codes.
func nextTableBitSize(count []int, length, rootBits int) int {
	left := 1 << (length - rootBits)
	for length < MaxAllowedCodeLength {
		left -= count[length]
		if left <= 0 {
			break
		}
		length++
		left <<= 1
	}
	return length - rootBits
}

// ReadSymbol decodes the next Huffman symbol from a lookup table,
// given prefetched bits and the current bit position. It returns
// the decoded value and the number of bits consumed.
func ReadSymbol(table []HuffmanCode, prefetchBits uint32) (value uint16, bitsUsed int) {
	entry := table[prefetchBits&HuffmanTableMask]
	nbits := int(entry.Bits) - HuffmanTableBits
	if nbits > 0 {
		// Second level lookup.
		bitsUsed = HuffmanTableBits
		prefetchBits >>= HuffmanTableBits
		idx := int(entry.Value) + int(prefetchBits&((1<<nbits)-1))
		if idx >= len(table) {
			return 0, -1 // sentinel: invalid table index
		}
		entry = table[idx]
		bitsUsed += int(entry.Bits)
		return entry.Value, bitsUsed
	}
	return entry.Value, int(entry.Bits)
}
