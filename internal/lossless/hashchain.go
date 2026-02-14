package lossless

// HashChain implements the VP8L hash chain for LZ77 backward reference matching.
//
// Each position in the image stores the best match as a packed
// (offset, length) pair. The hash table uses 18-bit multiplicative
// hashing to find potential match locations.
//
// Reference: libwebp/src/enc/backward_references_enc.c

const (
	// hashBits is the number of bits for the hash key.
	hashBits = 18
	// hashSize is the number of hash table buckets.
	hashSize = 1 << hashBits

	// maxLengthBits is the number of bits to encode match length.
	maxLengthBits = 12
	// maxLength is the maximum match length (4095).
	maxLength = (1 << maxLengthBits) - 1

	// windowSizeBits is the number of bits for the match window offset.
	windowSizeBits = 20
	// windowSize is the maximum backward distance for matching.
	windowSize = (1 << windowSizeBits) - 120

	// minLength is the minimum profitable copy length.
	minLength = 4
)

// Hash multipliers for 2-pixel hash (9.2).
const (
	kHashMultiplierHi = uint32(0xc6a4a793)
	kHashMultiplierLo = uint32(0x5bd1e996)
)

// getPixPairHash64 computes a hash from TWO consecutive ARGB pixels (9.2).
// This matches libwebp's GetPixPairHash64.
func getPixPairHash64(argb []uint32) uint32 {
	key := argb[1]*kHashMultiplierHi + argb[0]*kHashMultiplierLo
	return key >> (32 - hashBits)
}

// getPixPairHash64Values computes a hash from two explicit ARGB values (9.1).
func getPixPairHash64Values(a, b uint32) uint32 {
	key := b*kHashMultiplierHi + a*kHashMultiplierLo
	return key >> (32 - hashBits)
}

// getMaxItersForQuality returns the maximum number of hash chain lookups
// for a given compression quality. For quality <= 75, uses quality/3 which
// finds most good matches in fewer iterations. For quality > 75, uses the
// original quadratic formula for maximum compression.
func getMaxItersForQuality(quality int) int {
	if quality <= 75 {
		return 8 + quality/3
	}
	return 8 + (quality*quality)/128
}

// findMatchLength returns the length of the match between array1 and array2,
// up to maxLimit. If array1[bestLenMatch] != array2[bestLenMatch], returns 0
// immediately as an optimization (the match can't be better than current best).
func findMatchLength(array1, array2 []uint32, bestLenMatch, maxLimit int) int {
	if bestLenMatch < maxLimit && array1[bestLenMatch] != array2[bestLenMatch] {
		return 0
	}
	matchLen := 0
	for matchLen < maxLimit && array1[matchLen] == array2[matchLen] {
		matchLen++
	}
	return matchLen
}

// HashChain stores the hash chain for LZ77 matching.
type HashChain struct {
	// OffsetLength stores packed (offset, length) for each position.
	// Format: offset = value >> maxLengthBits, length = value & maxLength.
	OffsetLength     []uint32
	size             int
	hashToFirstIndex []int32 // reusable between Fill() calls
}

// NewHashChain creates a hash chain for an image of the given pixel count.
func NewHashChain(size int) *HashChain {
	return &HashChain{
		OffsetLength:     make([]uint32, size),
		size:             size,
		hashToFirstIndex: make([]int32, hashSize),
	}
}

// GetLength returns the match length at position pos.
func (hc *HashChain) GetLength(pos int) int {
	return int(hc.OffsetLength[pos]) & maxLength
}

// GetOffset returns the match offset (distance) at position pos.
func (hc *HashChain) GetOffset(pos int) int {
	return int(hc.OffsetLength[pos]) >> maxLengthBits
}

// GetWindowSizeForHashChain returns the hash chain window size based on quality.
func GetWindowSizeForHashChain(quality int, xsize int) int {
	if quality > 75 {
		return windowSize
	}
	if quality > 50 {
		maxWin := xsize << 8
		if maxWin > windowSize {
			return windowSize
		}
		return maxWin
	}
	if quality > 25 {
		maxWin := xsize << 6
		if maxWin > windowSize {
			return windowSize
		}
		return maxWin
	}
	maxWin := xsize << 4
	if maxWin > windowSize {
		return windowSize
	}
	return maxWin
}

// maxFindCopyLength caps the length to maxLength.
func maxFindCopyLength(length int) int {
	if length < maxLength {
		return length
	}
	return maxLength
}

// Fill builds the hash chain from the ARGB pixel array.
// quality controls the search window size.
func (hc *HashChain) Fill(argb []uint32, quality int, xsize, ysize int, lowEffort bool) {
	size := xsize * ysize
	if size <= 2 {
		hc.OffsetLength[0] = 0
		if size > 1 {
			hc.OffsetLength[size-1] = 0
		}
		return
	}

	iterMax := getMaxItersForQuality(quality) // 9.4
	winSize := uint32(GetWindowSizeForHashChain(quality, xsize))

	// Reuse hashToFirstIndex from the HashChain struct.
	hashToFirstIndex := hc.hashToFirstIndex
	for i := range hashToFirstIndex {
		hashToFirstIndex[i] = -1
	}

	// Temporarily use OffsetLength as chain storage (reinterpreted as int32).
	chainSlice := hc.OffsetLength

	// First pass: build chains with 2-pixel hash and repeated pixel handling (9.1, 9.2).
	argbComp := argb[0] == argb[1]
	for pos := 0; pos < size-2; {
		argbCompNext := argb[pos+1] == argb[pos+2]
		if argbComp && argbCompNext {
			// 9.1: Consecutive pixels with the same color share a combined hash
			// using (pixel, repetition_length).
			tmp0 := argb[pos]
			length := uint32(1)
			for pos+int(length)+2 < size && argb[pos+int(length)+2] == argb[pos] {
				length++
			}
			if length > maxLength {
				// Skip pixels that match for distance=1 and length>maxLength.
				skip := int(length - maxLength)
				for k := 0; k < skip; k++ {
					chainSlice[pos+k] = uint32(0xFFFFFFFF) // -1 as uint32
				}
				pos += skip
				length = maxLength
			}
			for length > 0 {
				hashCode := getPixPairHash64Values(tmp0, length)
				chainSlice[pos] = uint32(hashToFirstIndex[hashCode])
				hashToFirstIndex[hashCode] = int32(pos)
				pos++
				length--
			}
			argbComp = false
		} else {
			// Normal: hash two consecutive pixels.
			hashCode := getPixPairHash64(argb[pos:])
			chainSlice[pos] = uint32(hashToFirstIndex[hashCode])
			hashToFirstIndex[hashCode] = int32(pos)
			pos++
			argbComp = argbCompNext
		}
	}
	// Process the penultimate pixel.
	if size >= 3 {
		chainSlice[size-2] = uint32(hashToFirstIndex[getPixPairHash64(argb[size-2:])])
	}

	// Second pass: find best matches, iterating from right to left (9.3, 9.5).
	// The right-most pixel cannot match anything to the right.
	hc.OffsetLength[0] = 0
	hc.OffsetLength[size-1] = 0

	for basePosition := uint32(size - 2); basePosition > 0; {
		maxLen := maxFindCopyLength(int(uint32(size) - 1 - basePosition))
		argbStart := argb[basePosition:]
		iter := iterMax
		bestLength := 0
		bestDistance := uint32(0)
		minPos := int32(0)
		if basePosition > winSize {
			minPos = int32(basePosition - winSize)
		}
		lengthMax := maxLen
		if lengthMax > 256 {
			lengthMax = 256
		}

		pos := int32(chainSlice[basePosition])

		// 9.5: Spatial heuristics - check pixel above and to the left.
		if !lowEffort {
			// Heuristic: compare with pixel above.
			if basePosition >= uint32(xsize) {
				currLength := findMatchLength(argb[basePosition-uint32(xsize):], argbStart, bestLength, maxLen)
				if currLength > bestLength {
					bestLength = currLength
					bestDistance = uint32(xsize)
				}
				iter--
			}
			// Heuristic: compare with previous pixel.
			currLength := findMatchLength(argb[basePosition-1:], argbStart, bestLength, maxLen)
			if currLength > bestLength {
				bestLength = currLength
				bestDistance = 1
			}
			iter--
			// Skip the chain loop if we already have the maximum.
			if bestLength == maxLength {
				pos = minPos - 1
			}
		}

		bestArgb := argbStart[bestLength]

		for ; pos >= minPos && iter > 0; pos = int32(chainSlice[pos]) {
			iter--

			if argb[pos+int32(bestLength)] != bestArgb {
				continue
			}

			currLength := findMatchLength(argb[pos:], argbStart, 0, maxLen)
			if bestLength < currLength {
				bestLength = currLength
				bestDistance = basePosition - uint32(pos)
				bestArgb = argbStart[bestLength]
				if bestLength >= lengthMax {
					break
				}
			}
		}

		// 9.3: Left-extension - extend match to fill previous positions.
		maxBasePosition := basePosition
		for {
			if bestLength > maxLength {
				bestLength = maxLength
			}
			hc.OffsetLength[basePosition] = (bestDistance << maxLengthBits) | uint32(bestLength)
			basePosition--
			// Stop if we don't have a match or if we are out of bounds.
			if bestDistance == 0 || basePosition == 0 {
				break
			}
			// Stop if we cannot extend the matching intervals to the left.
			if basePosition < bestDistance ||
				argb[basePosition-bestDistance] != argb[basePosition] {
				break
			}
			// Stop if we are matching at its limit because there could be a closer
			// matching interval with the same maximum length. But if the
			// matching interval is as close as possible (bestDistance == 1), continue.
			if bestLength == maxLength && bestDistance != 1 &&
				basePosition+uint32(maxLength) < maxBasePosition {
				break
			}
			if bestLength < maxLength {
				bestLength++
				maxBasePosition = basePosition
			}
		}
	}
}

// DistanceToPlaneCode converts a pixel distance to a VP8L plane distance code.
// xsize is the image width. The plane code optimizes for horizontal/vertical
// distances commonly found in images.
func DistanceToPlaneCode(xsize int, dist int) int {
	yoffset := dist / xsize
	xoffset := dist - yoffset*xsize
	if xoffset <= 8 && yoffset < 8 {
		return int(planeToCodeLUT[yoffset*16+8-xoffset]) + 1
	} else if xoffset > xsize-8 && yoffset < 7 {
		return int(planeToCodeLUT[(yoffset+1)*16+8+(xsize-xoffset)]) + 1
	}
	return dist + CodeToPlaneCodesCount
}

// planeToCodeLUT maps (dy*16 + 8-dx) to distance code for nearby offsets.
// Generated from CodeToPlane table (inverse lookup).
var planeToCodeLUT [128]uint8

func init() {
	// Build inverse lookup from CodeToPlane.
	for i := 0; i < CodeToPlaneCodesCount; i++ {
		code := CodeToPlane[i]
		yoff := int(code >> 4)
		xoff := 8 - int(code&0xf)
		planeToCodeLUT[yoff*16+8-xoff] = uint8(i)
	}
}
