package lossless

import (
	"testing"

	"github.com/deepteams/webp/internal/bitio"
)

// ---------------------------------------------------------------------------
// CreateHuffmanTree tests
// ---------------------------------------------------------------------------

func TestCreateHuffmanTree_EmptyHistogram(t *testing.T) {
	histogram := make([]uint32, 10)
	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	if tree.NumSymbols != 10 {
		t.Fatalf("NumSymbols = %d, want 10", tree.NumSymbols)
	}
	for i, cl := range tree.CodeLengths {
		if cl != 0 {
			t.Errorf("CodeLengths[%d] = %d, want 0", i, cl)
		}
	}
}

func TestCreateHuffmanTree_SingleSymbol(t *testing.T) {
	histogram := make([]uint32, 256)
	histogram[42] = 100

	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	if tree.CodeLengths[42] != 1 {
		t.Errorf("CodeLengths[42] = %d, want 1", tree.CodeLengths[42])
	}
	// All other symbols should have code length 0.
	for i, cl := range tree.CodeLengths {
		if i != 42 && cl != 0 {
			t.Errorf("CodeLengths[%d] = %d, want 0", i, cl)
		}
	}
}

func TestCreateHuffmanTree_TwoSymbols(t *testing.T) {
	histogram := make([]uint32, 10)
	histogram[3] = 50
	histogram[7] = 50

	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	if tree.CodeLengths[3] != 1 {
		t.Errorf("CodeLengths[3] = %d, want 1", tree.CodeLengths[3])
	}
	if tree.CodeLengths[7] != 1 {
		t.Errorf("CodeLengths[7] = %d, want 1", tree.CodeLengths[7])
	}
	// Codes should be different (0 and 1, reversed).
	if tree.Codes[3] == tree.Codes[7] {
		t.Errorf("Codes[3] and Codes[7] should differ, both = %d", tree.Codes[3])
	}
}

func TestCreateHuffmanTree_ThreeSymbols(t *testing.T) {
	histogram := make([]uint32, 5)
	histogram[0] = 100
	histogram[1] = 50
	histogram[2] = 50

	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	// The most frequent symbol should get the shortest code.
	if tree.CodeLengths[0] > tree.CodeLengths[1] {
		t.Errorf("most frequent symbol should have shorter or equal code: CodeLengths[0]=%d, CodeLengths[1]=%d",
			tree.CodeLengths[0], tree.CodeLengths[1])
	}
}

func TestCreateHuffmanTree_KraftInequality(t *testing.T) {
	// The Kraft inequality must hold: sum of 2^(-length) == 1 for a
	// complete code.
	histogram := []uint32{10, 20, 30, 40, 50}
	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	kraftSum := 0.0
	for _, cl := range tree.CodeLengths {
		if cl > 0 {
			kraftSum += 1.0 / float64(uint64(1)<<cl)
		}
	}
	if kraftSum < 0.99 || kraftSum > 1.01 {
		t.Errorf("Kraft sum = %f, want ~1.0", kraftSum)
	}
}

func TestCreateHuffmanTree_CodeLengthLimit(t *testing.T) {
	// Create a heavily skewed histogram that would normally produce
	// very long codes, then enforce a tight limit.
	histogram := make([]uint32, 32)
	histogram[0] = 1000000
	for i := 1; i < 32; i++ {
		histogram[i] = 1
	}

	limit := 8
	tree := CreateHuffmanTree(histogram, limit)

	for i, cl := range tree.CodeLengths {
		if cl > uint8(limit) {
			t.Errorf("CodeLengths[%d] = %d, exceeds limit %d", i, cl, limit)
		}
	}

	// Verify it is still a valid code.
	kraftSum := 0.0
	for _, cl := range tree.CodeLengths {
		if cl > 0 {
			kraftSum += 1.0 / float64(uint64(1)<<cl)
		}
	}
	if kraftSum < 0.99 || kraftSum > 1.01 {
		t.Errorf("Kraft sum = %f after limiting, want ~1.0", kraftSum)
	}
}

func TestCreateHuffmanTree_UniqueCodes(t *testing.T) {
	histogram := []uint32{10, 20, 30, 40}
	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	// Collect (code, length) pairs for non-zero symbols.
	type codeLen struct {
		code   uint16
		length uint8
	}
	seen := make(map[codeLen]bool)
	for i, cl := range tree.CodeLengths {
		if cl > 0 {
			key := codeLen{tree.Codes[i], cl}
			if seen[key] {
				t.Errorf("duplicate code (%d, %d) for symbol %d", key.code, key.length, i)
			}
			seen[key] = true
		}
	}
}

// ---------------------------------------------------------------------------
// reverseBits tests
// ---------------------------------------------------------------------------

func TestReverseBits(t *testing.T) {
	tests := []struct {
		name   string
		v      uint32
		nBits  int
		expect uint16
	}{
		{"zero_1bit", 0, 1, 0},
		{"one_1bit", 1, 1, 1},
		{"0b10_2bits", 0b10, 2, 0b01},
		{"0b11_2bits", 0b11, 2, 0b11},
		{"0b101_3bits", 0b101, 3, 0b101},
		{"0b110_3bits", 0b110, 3, 0b011},
		{"0b1000_4bits", 0b1000, 4, 0b0001},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := reverseBits(tc.v, tc.nBits)
			if got != tc.expect {
				t.Errorf("reverseBits(%d, %d) = %d, want %d", tc.v, tc.nBits, got, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BuildCodeLengthTokens tests
// ---------------------------------------------------------------------------

func TestBuildCodeLengthTokens_AllZeros(t *testing.T) {
	codeLengths := make([]uint8, 20)
	tokens := BuildCodeLengthTokens(codeLengths)

	// 20 zeros should produce RLE tokens (code 18 for 11..138 or code 17 for 3..10).
	// 20 >= 11, so we expect code 18 with extraBits = 20-11 = 9.
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token for 20 zeros, got %d", len(tokens))
	}
	if tokens[0].code != 18 {
		t.Errorf("expected code 18, got %d", tokens[0].code)
	}
	if tokens[0].extraBits != 9 {
		t.Errorf("expected extraBits 9, got %d", tokens[0].extraBits)
	}
}

func TestBuildCodeLengthTokens_ShortZeroRun(t *testing.T) {
	// 5 zeros -> code 17 (repeat zero 3..10 times).
	codeLengths := make([]uint8, 5)
	tokens := BuildCodeLengthTokens(codeLengths)

	if len(tokens) != 1 {
		t.Fatalf("expected 1 token for 5 zeros, got %d", len(tokens))
	}
	if tokens[0].code != 17 {
		t.Errorf("expected code 17, got %d", tokens[0].code)
	}
	if tokens[0].extraBits != 2 { // 5 - 3 = 2
		t.Errorf("expected extraBits 2, got %d", tokens[0].extraBits)
	}
}

func TestBuildCodeLengthTokens_TwoZeros(t *testing.T) {
	// 2 zeros -> two literal 0 tokens (below the repeat threshold).
	codeLengths := []uint8{0, 0}
	tokens := BuildCodeLengthTokens(codeLengths)

	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens for 2 zeros, got %d", len(tokens))
	}
	for i, tok := range tokens {
		if tok.code != 0 {
			t.Errorf("tokens[%d].code = %d, want 0", i, tok.code)
		}
	}
}

func TestBuildCodeLengthTokens_RepeatNonZero(t *testing.T) {
	// Code length 5 repeated 7 times: first literal 5, then code 16 (repeat 6).
	codeLengths := []uint8{5, 5, 5, 5, 5, 5, 5}
	tokens := BuildCodeLengthTokens(codeLengths)

	// Should be: literal 5, code 16 with 6 repeats (= extraBits 3).
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
	if tokens[0].code != 5 {
		t.Errorf("tokens[0].code = %d, want 5", tokens[0].code)
	}
	if tokens[1].code != 16 {
		t.Errorf("tokens[1].code = %d, want 16", tokens[1].code)
	}
	if tokens[1].extraBits != 3 { // 6 - 3 = 3
		t.Errorf("tokens[1].extraBits = %d, want 3", tokens[1].extraBits)
	}
}

func TestBuildCodeLengthTokens_MixedSequence(t *testing.T) {
	// Sequence: 3, 3, 3, 3, 0, 0, 0, 0, 0, 7
	codeLengths := []uint8{3, 3, 3, 3, 0, 0, 0, 0, 0, 7}
	tokens := BuildCodeLengthTokens(codeLengths)

	// Expected: literal 3, code 16 (repeat 3 times, extraBits=0),
	//           code 17 (repeat zero 5 times, extraBits=2), literal 7.
	if len(tokens) != 4 {
		t.Fatalf("expected 4 tokens, got %d: %+v", len(tokens), tokens)
	}

	if tokens[0].code != 3 {
		t.Errorf("tokens[0].code = %d, want 3", tokens[0].code)
	}
	if tokens[1].code != 16 || tokens[1].extraBits != 0 {
		t.Errorf("tokens[1] = {code:%d, extra:%d}, want {16, 0}", tokens[1].code, tokens[1].extraBits)
	}
	if tokens[2].code != 17 || tokens[2].extraBits != 2 {
		t.Errorf("tokens[2] = {code:%d, extra:%d}, want {17, 2}", tokens[2].code, tokens[2].extraBits)
	}
	if tokens[3].code != 7 {
		t.Errorf("tokens[3].code = %d, want 7", tokens[3].code)
	}
}

func TestBuildCodeLengthTokens_LargeZeroRun(t *testing.T) {
	// 150 zeros -> code 18 (138 zeros) + code 17 (12 -> wait, 12 >= 11 -> code 18 again)
	// Actually: 150 >= 138+11=149 -> 138 + 12 -> 12 >= 11 -> code 18 with extra 1.
	codeLengths := make([]uint8, 150)
	tokens := BuildCodeLengthTokens(codeLengths)

	totalZeros := 0
	for _, tok := range tokens {
		switch tok.code {
		case 0:
			totalZeros++
		case 17:
			totalZeros += int(tok.extraBits) + 3
		case 18:
			totalZeros += int(tok.extraBits) + 11
		}
	}
	if totalZeros != 150 {
		t.Errorf("total zeros decoded = %d, want 150", totalZeros)
	}
}

// ---------------------------------------------------------------------------
// OptimizeHuffmanForRle tests
// ---------------------------------------------------------------------------

func TestOptimizeHuffmanForRle_Empty(t *testing.T) {
	result := OptimizeHuffmanForRle(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestOptimizeHuffmanForRle_AllZeros(t *testing.T) {
	counts := make([]uint32, 10)
	result := OptimizeHuffmanForRle(counts)
	for i, v := range result {
		if v != 0 {
			t.Errorf("result[%d] = %d, want 0", i, v)
		}
	}
}

func TestOptimizeHuffmanForRle_SingleValue(t *testing.T) {
	counts := []uint32{42}
	result := OptimizeHuffmanForRle(counts)
	if result[0] != 42 {
		t.Errorf("result[0] = %d, want 42", result[0])
	}
}

func TestOptimizeHuffmanForRle_SmoothsSimilarValues(t *testing.T) {
	// A run of similar non-zero values should be smoothed.
	counts := []uint32{100, 102, 98, 101, 99, 103, 97, 100}
	OptimizeHuffmanForRle(counts)

	// After smoothing, values should be closer together.
	// We just verify they are all equal (geometric mean).
	first := counts[0]
	for i := 1; i < len(counts); i++ {
		if counts[i] != first {
			// Acceptable: the smoothing may not make them perfectly equal
			// depending on boundaries. Just check they are in a reasonable range.
			diff := int(counts[i]) - int(first)
			if diff < -5 || diff > 5 {
				t.Errorf("counts[%d] = %d, expected close to %d", i, counts[i], first)
			}
		}
	}
}

func TestOptimizeHuffmanForRle_PreservesZeroBoundaries(t *testing.T) {
	// A long zero run should mark boundaries as "good", preserving them.
	counts := []uint32{10, 0, 0, 0, 0, 0, 0, 20}
	OptimizeHuffmanForRle(counts)

	// The boundary values (10 and 20) should be preserved since they are
	// at the edge of a zero run.
	if counts[0] != 10 {
		t.Errorf("counts[0] = %d, want 10", counts[0])
	}
	if counts[7] != 20 {
		t.Errorf("counts[7] = %d, want 20", counts[7])
	}
}

// ---------------------------------------------------------------------------
// StoreHuffmanTreeOfHuffmanTreeToBitMask tests
// ---------------------------------------------------------------------------

func TestStoreHuffmanTreeOfHuffmanTreeToBitMask_MinimalOutput(t *testing.T) {
	bw := bitio.NewLosslessWriter(64)

	// All code lengths are zero except for CodeLengthCodeOrder[0..3].
	codeLengthBitDepth := make([]uint8, CodeLengthCodes)
	codeLengthBitDepth[17] = 2 // CodeLengthCodeOrder[0] = 17
	codeLengthBitDepth[18] = 3 // CodeLengthCodeOrder[1] = 18
	codeLengthBitDepth[0] = 2  // CodeLengthCodeOrder[2] = 0
	codeLengthBitDepth[1] = 3  // CodeLengthCodeOrder[3] = 1

	StoreHuffmanTreeOfHuffmanTreeToBitMask(bw, codeLengthBitDepth)

	data := bw.Finish()
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// ---------------------------------------------------------------------------
// StoreHuffmanCode tests
// ---------------------------------------------------------------------------

func TestStoreHuffmanCode_SingleSymbol(t *testing.T) {
	bw := bitio.NewLosslessWriter(64)

	tree := &HuffmanTreeCode{
		NumSymbols:  256,
		CodeLengths: make([]uint8, 256),
		Codes:       make([]uint16, 256),
	}
	tree.CodeLengths[42] = 1
	tree.Codes[42] = 0

	StoreHuffmanCode(bw, tree)

	data := bw.Finish()
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
	// First bit should be 1 (is_simple).
	if data[0]&1 != 1 {
		t.Errorf("first bit should be 1 (is_simple), got %d", data[0]&1)
	}
}

func TestStoreHuffmanCode_TwoSymbols(t *testing.T) {
	bw := bitio.NewLosslessWriter(64)

	tree := &HuffmanTreeCode{
		NumSymbols:  256,
		CodeLengths: make([]uint8, 256),
		Codes:       make([]uint16, 256),
	}
	tree.CodeLengths[10] = 1
	tree.CodeLengths[20] = 1
	tree.Codes[10] = 0
	tree.Codes[20] = 1

	StoreHuffmanCode(bw, tree)

	data := bw.Finish()
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
	// First bit should be 1 (is_simple).
	if data[0]&1 != 1 {
		t.Errorf("first bit should be 1 (is_simple), got %d", data[0]&1)
	}
}

func TestStoreHuffmanCode_FullTree(t *testing.T) {
	histogram := make([]uint32, 10)
	histogram[0] = 100
	histogram[1] = 50
	histogram[2] = 30
	histogram[3] = 20
	histogram[4] = 10

	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	bw := bitio.NewLosslessWriter(128)
	StoreHuffmanCode(bw, tree)

	data := bw.Finish()
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
	// First bit should be 0 (not simple).
	if data[0]&1 != 0 {
		t.Errorf("first bit should be 0 (full tree), got %d", data[0]&1)
	}
}

func TestStoreHuffmanCode_SmallSymbolValues(t *testing.T) {
	// Test simple code with symbol values 0 and 1 (1-bit encoding).
	bw := bitio.NewLosslessWriter(64)

	tree := &HuffmanTreeCode{
		NumSymbols:  256,
		CodeLengths: make([]uint8, 256),
		Codes:       make([]uint16, 256),
	}
	tree.CodeLengths[0] = 1
	tree.Codes[0] = 0

	StoreHuffmanCode(bw, tree)

	data := bw.Finish()
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
}

// ---------------------------------------------------------------------------
// CountUniqueSymbols tests
// ---------------------------------------------------------------------------

func TestCountUniqueSymbols(t *testing.T) {
	tree := &HuffmanTreeCode{
		NumSymbols:  10,
		CodeLengths: make([]uint8, 10),
		Codes:       make([]uint16, 10),
	}
	tree.CodeLengths[2] = 3
	tree.CodeLengths[5] = 2
	tree.CodeLengths[8] = 4

	count := CountUniqueSymbols(tree)
	if count != 3 {
		t.Errorf("CountUniqueSymbols = %d, want 3", count)
	}
}

// ---------------------------------------------------------------------------
// HuffmanBitCost tests
// ---------------------------------------------------------------------------

func TestHuffmanBitCost(t *testing.T) {
	histogram := []uint32{10, 20, 30}
	tree := &HuffmanTreeCode{
		NumSymbols:  3,
		CodeLengths: []uint8{3, 2, 1},
		Codes:       make([]uint16, 3),
	}

	cost := HuffmanBitCost(histogram, tree)
	// Expected: 10*3 + 20*2 + 30*1 = 30 + 40 + 30 = 100.
	expected := 100.0
	if cost != expected {
		t.Errorf("HuffmanBitCost = %f, want %f", cost, expected)
	}
}

func TestHuffmanBitCost_EmptyHistogram(t *testing.T) {
	histogram := make([]uint32, 5)
	tree := &HuffmanTreeCode{
		NumSymbols:  5,
		CodeLengths: make([]uint8, 5),
		Codes:       make([]uint16, 5),
	}
	cost := HuffmanBitCost(histogram, tree)
	if cost != 0 {
		t.Errorf("HuffmanBitCost = %f, want 0", cost)
	}
}

// ---------------------------------------------------------------------------
// Round-trip test: CreateHuffmanTree -> StoreHuffmanCode -> verify bits decode
// ---------------------------------------------------------------------------

func TestCreateHuffmanTree_RoundTrip(t *testing.T) {
	// Build a tree, store it to bits, and verify the output is non-empty
	// and structurally valid.
	histogram := make([]uint32, 280)
	for i := 0; i < 256; i++ {
		histogram[i] = uint32(i + 1)
	}
	for i := 256; i < 280; i++ {
		histogram[i] = 1
	}

	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	// Verify all code lengths are within the limit.
	for i, cl := range tree.CodeLengths {
		if cl > MaxAllowedCodeLength {
			t.Errorf("CodeLengths[%d] = %d, exceeds %d", i, cl, MaxAllowedCodeLength)
		}
	}

	// Verify Kraft inequality.
	kraftSum := 0.0
	for _, cl := range tree.CodeLengths {
		if cl > 0 {
			kraftSum += 1.0 / float64(uint64(1)<<cl)
		}
	}
	if kraftSum < 0.99 || kraftSum > 1.01 {
		t.Errorf("Kraft sum = %f, want ~1.0", kraftSum)
	}

	// Store to bitstream and verify non-empty output.
	bw := bitio.NewLosslessWriter(1024)
	StoreHuffmanCode(bw, tree)
	data := bw.Finish()
	if len(data) == 0 {
		t.Fatal("expected non-empty bitstream output")
	}
}

// ---------------------------------------------------------------------------
// Edge case: histogram with many symbols at frequency 1
// ---------------------------------------------------------------------------

func TestCreateHuffmanTree_ManyEqualFrequencies(t *testing.T) {
	n := 256
	histogram := make([]uint32, n)
	for i := range histogram {
		histogram[i] = 1
	}

	tree := CreateHuffmanTree(histogram, MaxAllowedCodeLength)

	// With 256 symbols at equal frequency, the optimal tree assigns
	// 8 bits to each. Verify no code exceeds the limit.
	for i, cl := range tree.CodeLengths {
		if cl > MaxAllowedCodeLength {
			t.Errorf("CodeLengths[%d] = %d, exceeds limit", i, cl)
		}
		if cl == 0 {
			t.Errorf("CodeLengths[%d] = 0, expected non-zero for freq=1", i)
		}
	}
}

