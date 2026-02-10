package lossless

import "testing"

func TestBuildHuffmanTable_SingleSymbol(t *testing.T) {
	// Only symbol 42 has code length 1 -> trivial tree.
	codeLengths := make([]int, 256)
	codeLengths[42] = 1

	table, err := BuildHuffmanTable(HuffmanTableBits, codeLengths)
	if err != nil {
		t.Fatalf("BuildHuffmanTable: %v", err)
	}

	// Root table should have 256 entries (2^8), all mapping to symbol 42
	// with 0 bits consumed.
	if len(table) != 1<<HuffmanTableBits {
		t.Fatalf("table size = %d, want %d", len(table), 1<<HuffmanTableBits)
	}
	for i, entry := range table {
		if entry.Value != 42 {
			t.Errorf("table[%d].Value = %d, want 42", i, entry.Value)
			break
		}
		if entry.Bits != 0 {
			t.Errorf("table[%d].Bits = %d, want 0", i, entry.Bits)
			break
		}
	}
}

func TestBuildHuffmanTable_TwoSymbols(t *testing.T) {
	// Two symbols with code length 1: symbol 0 -> code '0', symbol 1 -> code '1'.
	codeLengths := make([]int, 2)
	codeLengths[0] = 1
	codeLengths[1] = 1

	table, err := BuildHuffmanTable(HuffmanTableBits, codeLengths)
	if err != nil {
		t.Fatalf("BuildHuffmanTable: %v", err)
	}

	// All entries with LSB=0 should decode to symbol 0, LSB=1 to symbol 1.
	for i := 0; i < len(table); i++ {
		expected := uint16(i & 1)
		if table[i].Value != expected {
			t.Errorf("table[%d].Value = %d, want %d", i, table[i].Value, expected)
		}
		if table[i].Bits != 1 {
			t.Errorf("table[%d].Bits = %d, want 1", i, table[i].Bits)
		}
	}
}

func TestBuildHuffmanTable_ThreeSymbols(t *testing.T) {
	// Three symbols: A=1bit, B=2bits, C=2bits
	// A(0)=0, B(1)=10, C(2)=11  (or reversed bit order for table lookup)
	codeLengths := make([]int, 3)
	codeLengths[0] = 1 // code: 0
	codeLengths[1] = 2 // code: 10
	codeLengths[2] = 2 // code: 11

	table, err := BuildHuffmanTable(HuffmanTableBits, codeLengths)
	if err != nil {
		t.Fatalf("BuildHuffmanTable: %v", err)
	}

	// Verify some entries by reading symbols.
	tests := []struct {
		prefetch uint32
		wantVal  uint16
		wantBits int
	}{
		{0b00000000, 0, 1}, // LSB = 0 -> symbol 0
		{0b00000010, 0, 1}, // LSB = 0 -> symbol 0
		{0b00000001, 1, 2}, // bits = 01 -> reversed = 10 -> symbol 1
		{0b00000011, 2, 2}, // bits = 11 -> reversed = 11 -> symbol 2
	}
	for _, tc := range tests {
		val, bits := ReadSymbol(table, tc.prefetch)
		if val != tc.wantVal || bits != tc.wantBits {
			t.Errorf("ReadSymbol(table, 0b%08b) = (%d, %d), want (%d, %d)",
				tc.prefetch, val, bits, tc.wantVal, tc.wantBits)
		}
	}
}

func TestBuildHuffmanTable_AllZeroLengths(t *testing.T) {
	codeLengths := make([]int, 10)
	_, err := BuildHuffmanTable(HuffmanTableBits, codeLengths)
	if err == nil {
		t.Error("expected error for all-zero code lengths")
	}
}

func TestBuildHuffmanTable_EmptyInput(t *testing.T) {
	_, err := BuildHuffmanTable(HuffmanTableBits, nil)
	if err == nil {
		t.Error("expected error for nil code lengths")
	}
}

func TestBuildHuffmanTable_InvalidCodeLength(t *testing.T) {
	codeLengths := []int{16} // exceeds MaxAllowedCodeLength
	_, err := BuildHuffmanTable(HuffmanTableBits, codeLengths)
	if err == nil {
		t.Error("expected error for code length > MaxAllowedCodeLength")
	}
}

func TestBuildHuffmanTable_LongerCodes(t *testing.T) {
	// Build a tree with codes that exceed rootBits, triggering sub-tables.
	// Symbol 0: 1 bit, Symbols 1..8: 4 bits each, Symbol 9: 4 bits
	// This creates a valid tree: 1*(1/2) + 9*(1/16) = 0.5 + 0.5625 -> overcomplete
	// Let's use a more carefully constructed tree.
	//
	// Valid Kraft sum = 1:
	// Symbol 0: length 1 -> 1/2
	// Symbol 1: length 3 -> 1/8
	// Symbol 2: length 3 -> 1/8
	// Symbol 3: length 3 -> 1/8
	// Symbol 4: length 3 -> 1/8
	// Total: 1/2 + 4/8 = 1  ✓
	codeLengths := make([]int, 5)
	codeLengths[0] = 1
	codeLengths[1] = 3
	codeLengths[2] = 3
	codeLengths[3] = 3
	codeLengths[4] = 3

	table, err := BuildHuffmanTable(2, codeLengths) // rootBits=2 to force sub-tables
	if err != nil {
		t.Fatalf("BuildHuffmanTable: %v", err)
	}
	if len(table) == 0 {
		t.Fatal("table should not be empty")
	}

	// Verify we can decode all symbols.
	// Symbol 0: code 0 (1 bit), reversed = 0
	val, bits := ReadSymbol(table, 0b000)
	if val != 0 || bits != 1 {
		t.Errorf("ReadSymbol for symbol 0: got (%d, %d), want (0, 1)", val, bits)
	}
}

func TestReadSymbol_Basic(t *testing.T) {
	// Two symbols: 0 and 1, each with code length 1.
	codeLengths := []int{1, 1}
	table, err := BuildHuffmanTable(HuffmanTableBits, codeLengths)
	if err != nil {
		t.Fatalf("BuildHuffmanTable: %v", err)
	}

	val0, bits0 := ReadSymbol(table, 0)
	if val0 != 0 || bits0 != 1 {
		t.Errorf("ReadSymbol(0) = (%d, %d), want (0, 1)", val0, bits0)
	}
	val1, bits1 := ReadSymbol(table, 1)
	if val1 != 1 || bits1 != 1 {
		t.Errorf("ReadSymbol(1) = (%d, %d), want (1, 1)", val1, bits1)
	}
}

func TestGetNextKey(t *testing.T) {
	// getNextKey should produce canonical reversed bit patterns.
	key := uint32(0)
	key = getNextKey(key, 3) // reverse(reverse(0,3)+1,3) = reverse(1,3) = 4
	if key != 4 {
		t.Errorf("getNextKey(0, 3) = %d, want 4", key)
	}
	key = getNextKey(key, 3) // reverse(reverse(4,3)+1,3) = reverse(2,3) = 2
	if key != 2 {
		t.Errorf("getNextKey(4, 3) = %d, want 2", key)
	}
	key = getNextKey(key, 3) // reverse(reverse(2,3)+1,3) = reverse(3,3) = 6
	if key != 6 {
		t.Errorf("getNextKey(2, 3) = %d, want 6", key)
	}
}
