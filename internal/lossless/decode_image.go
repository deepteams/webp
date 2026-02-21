package lossless

// decode_image.go implements the VP8L Huffman code reading and entropy-coded
// image data decoding loop.
//
// Reference: libwebp/src/dec/vp8l_dec.c (ReadHuffmanCode, ReadHuffmanCodes,
// ReadHuffmanCodesHelper, DecodeImageData).

import "github.com/deepteams/webp/internal/bitio"

// readHuffmanCodeLengths decodes Huffman-coded code lengths using a previously
// built code-lengths Huffman table.
func (dec *Decoder) readHuffmanCodeLengths(clTable []HuffmanCode, numSymbols int) ([]int, error) {
	// This returns a new slice because readHuffmanCode will use it as the final
	// codeLengths. Reuse the decoder's buffer if large enough.
	var codeLengths []int
	if cap(dec.codeLengthsBuf) >= numSymbols {
		codeLengths = dec.codeLengthsBuf[:numSymbols]
		for i := range codeLengths {
			codeLengths[i] = 0
		}
	} else {
		codeLengths = make([]int, numSymbols)
		dec.codeLengthsBuf = codeLengths
	}
	prevCodeLen := DefaultCodeLength

	maxSymbol := numSymbols
	if dec.br.ReadBits(1) == 1 { // use length
		lengthNbits := 2 + 2*int(dec.br.ReadBits(3))
		maxSymbol = 2 + int(dec.br.ReadBits(lengthNbits))
		if maxSymbol > numSymbols {
			return nil, ErrBitstream
		}
	}

	symbol := 0
	remaining := maxSymbol
	for symbol < numSymbols {
		if remaining == 0 {
			break
		}
		remaining--
		dec.br.FillBitWindow()
		prefetch := dec.br.PrefetchBits()
		entry := clTable[prefetch&LengthsTableMask]
		dec.br.SetBitPos(dec.br.BitPos() + int(entry.Bits))
		codeLen := int(entry.Value)

		if codeLen < CodeLengthLiterals {
			codeLengths[symbol] = codeLen
			symbol++
			if codeLen != 0 {
				prevCodeLen = codeLen
			}
		} else {
			slot := codeLen - CodeLengthLiterals
			extraBits := int(CodeLengthExtraBits[slot])
			repeatOffset := int(CodeLengthRepeatOffsets[slot])
			repeatCount := int(dec.br.ReadBits(extraBits)) + repeatOffset
			if symbol+repeatCount > numSymbols {
				return nil, ErrBitstream
			}
			usePrev := codeLen == CodeLengthRepeatCode
			length := 0
			if usePrev {
				length = prevCodeLen
			}
			for i := 0; i < repeatCount; i++ {
				codeLengths[symbol] = length
				symbol++
			}
		}
	}

	if dec.br.IsEndOfStream() {
		return nil, ErrBitstream
	}
	return codeLengths, nil
}

// readHuffmanCode reads a single Huffman tree from the bitstream.
// Returns the built lookup table and the maximum code length across all symbols.
// The maxCodeLength is needed for computing the packed table eligibility
// (matching the C reference's max_bits accumulation in ReadHuffmanCodesHelper).
func (dec *Decoder) readHuffmanCode(alphabetSize int) ([]HuffmanCode, int, error) {
	simpleCode := dec.br.ReadBits(1)

	// Reuse codeLengths buffer if large enough.
	var codeLengths []int
	if cap(dec.codeLengthsBuf) >= alphabetSize {
		codeLengths = dec.codeLengthsBuf[:alphabetSize]
		for i := range codeLengths {
			codeLengths[i] = 0
		}
	} else {
		codeLengths = make([]int, alphabetSize)
		dec.codeLengthsBuf = codeLengths
	}

	if simpleCode == 1 {
		// Simple code: 1 or 2 symbols encoded directly.
		numSymbols := int(dec.br.ReadBits(1)) + 1
		firstSymbolLenCode := dec.br.ReadBits(1)
		var symbolBits int
		if firstSymbolLenCode == 0 {
			symbolBits = 1
		} else {
			symbolBits = 8
		}
		symbol := int(dec.br.ReadBits(symbolBits))
		if symbol >= alphabetSize {
			return nil, 0, ErrBitstream
		}
		codeLengths[symbol] = 1
		if numSymbols == 2 {
			symbol2 := int(dec.br.ReadBits(8))
			if symbol2 >= alphabetSize {
				return nil, 0, ErrBitstream
			}
			codeLengths[symbol2] = 1
		}
	} else {
		// Normal code: read code-length code lengths, then decode.
		var clCodeLengths [CodeLengthCodes]int
		numCodes := int(dec.br.ReadBits(4)) + 4
		if numCodes > CodeLengthCodes {
			numCodes = CodeLengthCodes
		}
		for i := 0; i < numCodes; i++ {
			clCodeLengths[CodeLengthCodeOrder[i]] = int(dec.br.ReadBits(3))
		}

		// Build the code-lengths Huffman table.
		// Code-length tables are small (LengthsTableBits=7, max ~128 entries),
		// not worth slab-allocating.
		clTable, err := BuildHuffmanTableScratch(LengthsTableBits, clCodeLengths[:], dec.huffTableScratch())
		if err != nil {
			return nil, 0, err
		}

		decodedLengths, err := dec.readHuffmanCodeLengths(clTable, alphabetSize)
		if err != nil {
			return nil, 0, err
		}
		codeLengths = decodedLengths
	}

	if dec.br.IsEndOfStream() {
		return nil, 0, ErrBitstream
	}

	// Compute the maximum code length across all symbols.
	maxCodeLen := 0
	for _, cl := range codeLengths {
		if cl > maxCodeLen {
			maxCodeLen = cl
		}
	}

	table, err := BuildHuffmanTableScratch(HuffmanTableBits, codeLengths, dec.huffTableScratch())
	if err != nil {
		return nil, 0, err
	}
	return table, maxCodeLen, nil
}

// huffTableScratch returns the decoder's reusable HuffmanTableScratch.
func (dec *Decoder) huffTableScratch() *HuffmanTableScratch {
	return &dec.huffScratch
}

// readHuffmanCodes reads the Huffman meta-image (if present) and all
// Huffman tree groups from the bitstream.
func (dec *Decoder) readHuffmanCodes(xsize, ysize, colorCacheBits int, allowRecursion bool) error {
	numHTreeGroups := 1
	numHTreeGroupsMax := 1
	var huffmanImage []uint32
	var mapping []int // non-nil when remapping is active; mapping[i]==-1 means unused

	if allowRecursion && dec.br.ReadBits(1) == 1 {
		// Meta Huffman codes.
		huffmanPrecision := MinHuffmanBits + int(dec.br.ReadBits(NumHuffmanBits))
		huffmanXSize := VP8LSubSampleSize(xsize, huffmanPrecision)
		huffmanYSize := VP8LSubSampleSize(ysize, huffmanPrecision)
		huffmanPixs := huffmanXSize * huffmanYSize

		subImage, err := dec.decodeSubImage(huffmanXSize, huffmanYSize)
		if err != nil {
			return err
		}

		dec.hdr.huffmanSubsampleBits = huffmanPrecision
		numHTreeGroupsMax = 1
		for i := 0; i < huffmanPixs; i++ {
			group := int((subImage[i] >> 8) & 0xffff)
			subImage[i] = uint32(group)
			if group+1 > numHTreeGroupsMax {
				numHTreeGroupsMax = group + 1
			}
		}

		// Remap if needed. When the number of groups is too large, create
		// a mapping from original indices to a compact [0, numHTreeGroups)
		// range. The mapping is preserved so ReadHuffmanCodesHelper (below)
		// can identify which bitstream groups to keep vs discard.
		if numHTreeGroupsMax > 1000 || numHTreeGroupsMax > xsize*ysize {
			mapping = make([]int, numHTreeGroupsMax)
			for i := range mapping {
				mapping[i] = -1
			}
			numHTreeGroups = 0
			for i := 0; i < huffmanPixs; i++ {
				g := int(subImage[i])
				if mapping[g] == -1 {
					mapping[g] = numHTreeGroups
					numHTreeGroups++
				}
				subImage[i] = uint32(mapping[g])
			}
		} else {
			numHTreeGroups = numHTreeGroupsMax
		}
		huffmanImage = subImage
	}

	if dec.br.IsEndOfStream() {
		return ErrBitstream
	}

	// Read all Huffman tree groups.
	// The C reference (ReadHuffmanCodesHelper) iterates over numHTreeGroupsMax,
	// reading Huffman codes for ALL groups from the bitstream. Unmapped groups
	// (mapping[i] == -1) are read but discarded to keep the bit reader in sync.
	// We only allocate storage for the numHTreeGroups actually used.
	var htreeGroups []HTreeGroup
	if cap(dec.htreeGroupsBuf) >= numHTreeGroups {
		htreeGroups = dec.htreeGroupsBuf[:numHTreeGroups]
		// Zero out reused entries.
		for i := range htreeGroups {
			htreeGroups[i] = HTreeGroup{}
		}
	} else {
		htreeGroups = make([]HTreeGroup, numHTreeGroups)
		dec.htreeGroupsBuf = htreeGroups
	}

	for i := 0; i < numHTreeGroupsMax; i++ {
		// Determine the destination index. If this group is unmapped
		// (not referenced by any pixel in the Huffman image), we still
		// need to read its Huffman codes from the bitstream to stay in
		// sync, but we discard the result.
		mapped := -1
		if mapping != nil {
			mapped = mapping[i]
		} else {
			mapped = i
		}

		if mapped == -1 {
			// Unmapped group: read and discard all 5 Huffman trees.
			for j := 0; j < HuffmanCodesPerMetaCode; j++ {
				alphaSize := kBaseAlphabetSize[j]
				if j == 0 && colorCacheBits > 0 {
					alphaSize += 1 << colorCacheBits
				}
				if _, _, err := dec.readHuffmanCode(alphaSize); err != nil {
					return err
				}
			}
			continue
		}

		// Mapped group: read and store all 5 Huffman trees.
		isTrivialLiteral := true
		totalBits := 0
		maxBits := 0

		for j := 0; j < HuffmanCodesPerMetaCode; j++ {
			alphaSize := kBaseAlphabetSize[j]
			if j == 0 && colorCacheBits > 0 {
				alphaSize += 1 << colorCacheBits
			}

			table, maxCodeLen, err := dec.readHuffmanCode(alphaSize)
			if err != nil {
				return err
			}
			htreeGroups[mapped].HTrees[j] = table

			if isTrivialLiteral && KLiteralMap[j] == 1 {
				isTrivialLiteral = table[0].Bits == 0
			}
			totalBits += int(table[0].Bits)

			// Accumulate the maximum code length per literal channel
			// (green, red, blue, alpha). This matches the C reference's
			// max_bits computation in ReadHuffmanCodesHelper which iterates
			// over all code_lengths to find the per-tree maximum.
			if j <= int(HuffAlpha) {
				maxBits += maxCodeLen
			}
		}

		htreeGroups[mapped].IsTrivialLiteral = isTrivialLiteral
		if isTrivialLiteral {
			red := uint32(htreeGroups[mapped].HTrees[int(HuffRed)][0].Value)
			blue := uint32(htreeGroups[mapped].HTrees[int(HuffBlue)][0].Value)
			alpha := uint32(htreeGroups[mapped].HTrees[int(HuffAlpha)][0].Value)
			htreeGroups[mapped].LiteralARB = (alpha << 24) | (red << 16) | blue
			if totalBits == 0 && htreeGroups[mapped].HTrees[int(HuffGreen)][0].Value < NumLiteralCodes {
				htreeGroups[mapped].IsTrivialCode = true
				htreeGroups[mapped].LiteralARB |= uint32(htreeGroups[mapped].HTrees[int(HuffGreen)][0].Value) << 8
			}
		}
		htreeGroups[mapped].UsePackedTable = !htreeGroups[mapped].IsTrivialCode && maxBits < HuffmanPackedBits
		if htreeGroups[mapped].UsePackedTable {
			buildPackedTable(&htreeGroups[mapped])
		}
	}

	dec.hdr.numHTreeGroups = numHTreeGroups
	dec.hdr.htreeGroups = htreeGroups
	dec.hdr.huffmanImage = huffmanImage
	return nil
}

// buildPackedTable constructs the compact packed_table for an HTreeGroup.
func buildPackedTable(group *HTreeGroup) {
	for code := uint32(0); code < HuffmanPackedTableSize; code++ {
		bits := code
		huff := &group.PackedTable[code]

		hcode := group.HTrees[int(HuffGreen)][bits&HuffmanTableMask]
		if int(hcode.Value) >= NumLiteralCodes {
			huff.Bits = int(hcode.Bits) + bitsSpecialMarker
			huff.Value = uint32(hcode.Value)
		} else {
			huff.Bits = 0
			huff.Value = 0
			n := accumulateHCode(hcode, 8, huff)
			bits >>= n
			n = accumulateHCode(group.HTrees[int(HuffRed)][bits&HuffmanTableMask], 16, huff)
			bits >>= n
			n = accumulateHCode(group.HTrees[int(HuffBlue)][bits&HuffmanTableMask], 0, huff)
			bits >>= n
			accumulateHCode(group.HTrees[int(HuffAlpha)][bits&HuffmanTableMask], 24, huff)
		}
	}
}

const bitsSpecialMarker = 0x100

func accumulateHCode(hcode HuffmanCode, shift int, huff *HuffmanCode32) int {
	huff.Bits += int(hcode.Bits)
	huff.Value |= uint32(hcode.Value) << shift
	return int(hcode.Bits)
}

// getMetaIndex returns the Huffman tree group index for pixel position (x, y).
func (dec *Decoder) getMetaIndex(x, y int) int {
	if dec.hdr.huffmanSubsampleBits == 0 {
		return 0
	}
	return int(dec.hdr.huffmanImage[dec.hdr.huffmanXSize*(y>>dec.hdr.huffmanSubsampleBits)+(x>>dec.hdr.huffmanSubsampleBits)])
}

// getHTreeGroup returns the HTreeGroup for pixel position (x, y).
func (dec *Decoder) getHTreeGroup(x, y int) *HTreeGroup {
	return &dec.hdr.htreeGroups[dec.getMetaIndex(x, y)]
}

// getCopyDistance decodes the distance from a distance symbol.
// Uses concrete *bitio.LosslessReader to enable method inlining.
func getCopyDistance(distanceSymbol int, br *bitio.LosslessReader) int {
	if distanceSymbol < 4 {
		return distanceSymbol + 1
	}
	extraBits := (distanceSymbol - 2) >> 1
	offset := (2 + (distanceSymbol & 1)) << extraBits
	return offset + int(br.ReadBits(extraBits)) + 1
}

// getCopyLength decodes the length from a length symbol.
func getCopyLength(lengthSymbol int, br *bitio.LosslessReader) int {
	return getCopyDistance(lengthSymbol, br) // same encoding
}

// readSymbolFromTree decodes one Huffman symbol from a table using the
// bit reader, performing the necessary fill/prefetch.
// Uses concrete *bitio.LosslessReader so FillBitWindow/PrefetchBits/
// SetBitPos/BitPos can inline (avoiding interface dispatch overhead).
func readSymbolFromTree(table []HuffmanCode, br *bitio.LosslessReader) int {
	br.FillBitWindow()
	val, bitsUsed := ReadSymbol(table, br.PrefetchBits())
	br.SetBitPos(br.BitPos() + bitsUsed)
	return int(val)
}

// readPackedSymbols attempts to decode an entire ARGB pixel from the
// packed table. Returns (value, code) where code == 0 means a full
// literal was decoded into *dst, otherwise code is the non-literal symbol.
// Uses concrete *bitio.LosslessReader for method inlining.
func readPackedSymbols(group *HTreeGroup, br *bitio.LosslessReader) (argb uint32, greenCode int, isLiteral bool) {
	bits := br.PrefetchBits() & (HuffmanPackedTableSize - 1)
	code := group.PackedTable[bits]
	if code.Bits < bitsSpecialMarker {
		br.SetBitPos(br.BitPos() + code.Bits)
		return code.Value, 0, true
	}
	br.SetBitPos(br.BitPos() + code.Bits - bitsSpecialMarker)
	return 0, int(code.Value), false
}

// decodeImageData is the main entropy-coding decode loop. It decodes
// width*height pixels into data[], using the Huffman trees in dec.hdr.
//
// Color cache tracking: like the C reference (libwebp/src/dec/vp8l_dec.c),
// we track lastCached as the exact position of the last pixel inserted into
// the color cache. Pending pixels (from lastCached to pos) are bulk-inserted
// at end-of-row, before backward references, and before color cache lookups.
//
// Performance: readSymbolFromTree (cost 163) and getCopyDistance (cost 94)
// exceed Go's inline budget (80). We manually inline them so that each
// component call (FillBitWindow, PrefetchBits, ReadSymbol, SetBitPos, BitPos)
// inlines individually, keeping hot state in registers. FillBitWindow calls
// are reduced from 5 to 2 per literal pixel by exploiting the 64-bit val
// register guarantee (≥32 bits after fill, each Huffman code ≤15 bits).
func (dec *Decoder) decodeImageData(data []uint32, width, height, lastRow int) error {
	br := dec.br
	hdr := &dec.hdr

	lenCodeLimit := NumLiteralCodes + NumLengthCodes
	colorCacheLimit := lenCodeLimit + hdr.colorCacheSize
	colorCache := hdr.colorCache
	mask := hdr.huffmanMask

	pos := 0
	lastCached := 0 // 8.2: exact position tracking like C's last_cached pointer
	row := 0
	col := 0
	srcEnd := width * height
	srcLast := width * lastRow

	var htreeGroup *HTreeGroup
	if pos < srcLast {
		htreeGroup = dec.getHTreeGroup(col, row)
	}

	for pos < srcLast {
		if (col & mask) == 0 {
			htreeGroup = dec.getHTreeGroup(col, row)
		}

		// 8.5: Fast path: trivial code (single literal for all channels).
		// C does NOT cache trivial code pixels here; they are cached at
		// end-of-row via the lastCached mechanism (goto AdvanceByOne).
		if htreeGroup.IsTrivialCode {
			data[pos] = htreeGroup.LiteralARB
			pos++
			col++
			if col >= width {
				col = 0
				row++
				if colorCache != nil {
					for lastCached < pos {
						colorCache.Insert(data[lastCached])
						lastCached++
					}
				}
			}
			continue
		}

		br.FillBitWindow()

		var code int
		if htreeGroup.UsePackedTable {
			// 8.6: Packed table path. C's ReadPackedSymbols writes directly
			// to *src and returns PACKED_NON_LITERAL_CODE (0) for literals.
			// When literal, write to data[pos] and do AdvanceByOne (no
			// immediate per-pixel cache insertion).
			argb, gc, isLit := readPackedSymbols(htreeGroup, br)
			if br.IsEndOfStream() {
				break
			}
			if isLit {
				data[pos] = argb
				pos++
				col++
				if col >= width {
					col = 0
					row++
					if colorCache != nil {
						for lastCached < pos {
							colorCache.Insert(data[lastCached])
							lastCached++
						}
					}
				}
				continue
			}
			code = gc
		} else {
			// Inline readSymbolFromTree for green.
			// FillBitWindow already called above — no redundant fill needed.
			prefetch := br.PrefetchBits()
			val, bits := ReadSymbol(htreeGroup.HTrees[int(HuffGreen)], prefetch)
			br.SetBitPos(br.BitPos() + bits)
			code = int(val)
		}

		// 8.7: EOS check after GREEN symbol (C has this at line 1259).
		if br.IsEndOfStream() {
			break
		}

		if code < NumLiteralCodes {
			// Literal pixel.
			if htreeGroup.IsTrivialLiteral {
				data[pos] = htreeGroup.LiteralARB | (uint32(code) << 8)
			} else {
				// Inline readSymbolFromTree for red.
				// After green (≤15 bits), ≥17 bits remain — no fill needed.
				prefetch := br.PrefetchBits()
				redVal, redBits := ReadSymbol(htreeGroup.HTrees[int(HuffRed)], prefetch)
				br.SetBitPos(br.BitPos() + redBits)

				// Fill before blue+alpha (green+red consumed ≤30 bits).
				br.FillBitWindow()

				// Inline readSymbolFromTree for blue.
				prefetch = br.PrefetchBits()
				blueVal, blueBits := ReadSymbol(htreeGroup.HTrees[int(HuffBlue)], prefetch)
				br.SetBitPos(br.BitPos() + blueBits)

				// Inline readSymbolFromTree for alpha.
				// After blue (≤15 bits), ≥17 bits remain — no fill needed.
				prefetch = br.PrefetchBits()
				alphaVal, alphaBits := ReadSymbol(htreeGroup.HTrees[int(HuffAlpha)], prefetch)
				br.SetBitPos(br.BitPos() + alphaBits)

				// 8.7: Second EOS check after all symbols (C line 1269).
				if br.IsEndOfStream() {
					break
				}
				data[pos] = (uint32(alphaVal) << 24) | (uint32(redVal) << 16) | (uint32(code) << 8) | uint32(blueVal)
			}
			pos++
			col++
			if col >= width {
				col = 0
				row++
				// 8.2/8.3: Insert all pending pixels from lastCached to pos.
				if colorCache != nil {
					for lastCached < pos {
						colorCache.Insert(data[lastCached])
						lastCached++
					}
				}
			}
		} else if code < lenCodeLimit {
			// Backward reference (LZ77 copy).
			lengthSym := code - NumLiteralCodes

			// Inline getCopyLength (= getCopyDistance encoding).
			var length int
			if lengthSym < 4 {
				length = lengthSym + 1
			} else {
				extraBits := (lengthSym - 2) >> 1
				offset := (2 + (lengthSym & 1)) << extraBits
				br.FillBitWindow()
				length = offset + int(br.PrefetchBits()&uint32((1<<extraBits)-1)) + 1
				br.SetBitPos(br.BitPos() + extraBits)
			}

			// Inline readSymbolFromTree for distance.
			br.FillBitWindow()
			prefetch := br.PrefetchBits()
			distVal, distBits := ReadSymbol(htreeGroup.HTrees[int(HuffDist)], prefetch)
			br.SetBitPos(br.BitPos() + distBits)
			distSymbol := int(distVal)

			// Inline getCopyDistance.
			var distCode int
			if distSymbol < 4 {
				distCode = distSymbol + 1
			} else {
				dExtraBits := (distSymbol - 2) >> 1
				dOffset := (2 + (distSymbol & 1)) << dExtraBits
				br.FillBitWindow()
				distCode = dOffset + int(br.PrefetchBits()&uint32((1<<dExtraBits)-1)) + 1
				br.SetBitPos(br.BitPos() + dExtraBits)
			}
			dist := PlaneCodeToDistance(width, distCode)

			if br.IsEndOfStream() {
				break
			}
			// 8.4: Bounds check. pos is equivalent to C's (src - data).
			if pos < dist || srcEnd-pos < length {
				return ErrBitstream
			}

			// Copy block.
			copyBlock32(data, pos, dist, length)
			pos += length
			col += length
			for col >= width {
				col -= width
				row++
			}
			if col&mask != 0 {
				htreeGroup = dec.getHTreeGroup(col, row)
			}
			// 8.3: Cache ALL pixels from lastCached to pos, including
			// any literals that preceded this backward reference.
			if colorCache != nil {
				for lastCached < pos {
					colorCache.Insert(data[lastCached])
					lastCached++
				}
			}
		} else if code < colorCacheLimit {
			// Color cache lookup.
			key := code - lenCodeLimit
			// 8.1: Insert ALL pending pixels BEFORE lookup, matching C's
			// while (last_cached < src) loop at line 1327-1329.
			if colorCache != nil {
				for lastCached < pos {
					colorCache.Insert(data[lastCached])
					lastCached++
				}
				data[pos] = colorCache.Lookup(key)
			}
			pos++
			col++
			if col >= width {
				col = 0
				row++
				// After color cache lookup + AdvanceByOne, also flush cache at end-of-row.
				if colorCache != nil {
					for lastCached < pos {
						colorCache.Insert(data[lastCached])
						lastCached++
					}
				}
			}
		} else {
			return ErrBitstream
		}
	}

	if br.IsEndOfStream() && pos < srcEnd {
		return ErrBitstream
	}

	return nil
}

// copyBlock32 copies 'length' uint32 values from data[pos-dist..] to data[pos..].
// Optimized: non-overlapping uses copy() (SIMD memmove), dist==1 uses fill,
// small overlapping dist uses a doubling copy pattern.
func copyBlock32(data []uint32, pos, dist, length int) {
	src := pos - dist
	if dist >= length {
		// Non-overlapping: use copy() which maps to runtime memmove (SIMD).
		copy(data[pos:pos+length], data[src:src+length])
	} else if dist == 1 {
		// Single-value fill: repeated pixel.
		val := data[src]
		dst := data[pos : pos+length]
		for i := range dst {
			dst[i] = val
		}
	} else {
		// Overlapping with dist > 1: doubling copy pattern.
		// Copy the first 'dist' elements, then double the copied region
		// until all elements are filled.
		copy(data[pos:pos+dist], data[src:src+dist])
		copied := dist
		for copied < length {
			n := copied
			if n > length-copied {
				n = length - copied
			}
			copy(data[pos+copied:pos+copied+n], data[pos:pos+n])
			copied += n
		}
	}
}
