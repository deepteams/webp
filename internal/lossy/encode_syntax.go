package lossy

import (
	"encoding/binary"

	"github.com/deepteams/webp/internal/bitio"
)

// emitFrame assembles the complete VP8 bitstream from the encoded data.
// The output is the raw VP8 frame data (no RIFF container).
func (enc *VP8Encoder) emitFrame() ([]byte, error) {
	// Partition 0: header + mode data.
	part0 := enc.emitPartition0()

	// Token partitions (1 to 8).
	tokenParts := enc.emitTokenPartitions()

	// Store size breakdown for debugging.
	tokenSize := 0
	for _, tp := range tokenParts {
		tokenSize += len(tp)
	}
	enc.stats.HeaderSize = 10 + len(part0) // frame tag + pic header + partition 0
	enc.stats.Residuals = tokenSize

	// Frame tag (3 bytes) + picture header (7 bytes for keyframe).
	return enc.assembleFrame(part0, tokenParts), nil
}

// emitPartition0 writes the mode partition (partition 0).
func (enc *VP8Encoder) emitPartition0() []byte {
	bw := bitio.NewBoolWriter(enc.mbW * enc.mbH * 8)

	// Keyframe-specific bits (Paragraph 9.2): colorspace and clamp_type.
	bw.PutBitUniform(0) // colorspace: 0 = YUV
	bw.PutBitUniform(0) // clamp_type: 0 = clamping required

	// Write segment header.
	enc.writeSegmentHeader(bw)

	// Write filter header.
	enc.writeFilterHeader(bw)

	// Write partition count.
	// log2(numParts) encoded as 2 bits.
	log2Parts := 0
	switch enc.numParts {
	case 2:
		log2Parts = 1
	case 4:
		log2Parts = 2
	case 8:
		log2Parts = 3
	}
	bw.PutBits(uint32(log2Parts), 2)

	// Write quantizer parameters.
	enc.writeQuantParams(bw)

	// Refresh-update flag (always 0 for keyframe).
	bw.PutBitUniform(0)

	// Track coefficient probability table size.
	preProbaPos := bw.Pos()

	// Write coefficient probabilities (Paragraph 13).
	enc.writeCoeffProba(bw)

	postProbaPos := bw.Pos()
	enc.stats.probaSize = int((postProbaPos - preProbaPos + 7) / 8)

	// Write mb_no_coeff_skip flag (Paragraph 9.10).
	if enc.numSkip > 0 {
		bw.PutBitUniform(1) // skip mode enabled
		bw.PutBits(uint32(enc.skipProba), 8)
	} else {
		bw.PutBitUniform(0) // no skip
	}

	// Write macroblock prediction modes.
	enc.writeMBModes(bw)

	return bw.Finish()
}

// emitTokenPartitions writes the token data partitions.
func (enc *VP8Encoder) emitTokenPartitions() [][]byte {
	parts := make([][]byte, enc.numParts)
	for i := 0; i < enc.numParts; i++ {
		bw := bitio.NewBoolWriter(enc.mbW * enc.mbH * 32 / enc.numParts)
		enc.tokens.EmitTokensPartitioned(bw, i, enc.numParts, enc.mbW)
		parts[i] = bw.Finish()
	}
	return parts
}

// assembleFrame constructs the complete VP8 frame from partition 0 and
// token partitions.
func (enc *VP8Encoder) assembleFrame(part0 []byte, tokenParts [][]byte) []byte {
	// Calculate total size.
	totalSize := 3 + 7 + len(part0) // frame tag + picture header + partition 0

	// Token partition sizes (3 bytes each for N-1 partitions).
	if len(tokenParts) > 1 {
		totalSize += 3 * (len(tokenParts) - 1)
	}
	for _, tp := range tokenParts {
		totalSize += len(tp)
	}

	buf := make([]byte, 0, totalSize)

	// Frame tag (3 bytes).
	// Bit 0: 0 = keyframe
	// Bits 1-3: profile (0)
	// Bit 4: show (1)
	// Bits 5-23: partition 0 size
	tag := uint32(0)               // keyframe
	tag |= uint32(0) << 1          // profile 0
	tag |= uint32(1) << 4          // show
	tag |= uint32(len(part0)) << 5 // partition length
	buf = append(buf, byte(tag), byte(tag>>8), byte(tag>>16))

	// Picture header (7 bytes for keyframe).
	// VP8 signature: 0x9D 0x01 0x2A
	buf = append(buf, 0x9d, 0x01, 0x2a)
	// Width (14 bits) + x_scale (2 bits).
	var wbuf [2]byte
	binary.LittleEndian.PutUint16(wbuf[:], uint16(enc.width&0x3FFF))
	buf = append(buf, wbuf[:]...)
	// Height (14 bits) + y_scale (2 bits).
	var hbuf [2]byte
	binary.LittleEndian.PutUint16(hbuf[:], uint16(enc.height&0x3FFF))
	buf = append(buf, hbuf[:]...)

	// Partition 0 data.
	buf = append(buf, part0...)

	// Token partition sizes (for multi-partition: N-1 sizes of 3 bytes each).
	if len(tokenParts) > 1 {
		for i := 0; i < len(tokenParts)-1; i++ {
			sz := len(tokenParts[i])
			buf = append(buf, byte(sz), byte(sz>>8), byte(sz>>16))
		}
	}

	// Token partition data.
	for _, tp := range tokenParts {
		buf = append(buf, tp...)
	}

	return buf
}

// writeSegmentHeader writes the segment header into partition 0.
func (enc *VP8Encoder) writeSegmentHeader(bw *bitio.BoolWriter) {
	hdr := &enc.segmentHdr

	bw.PutBitUniform(boolToIntEnc(hdr.UseSegment))
	if hdr.UseSegment {
		bw.PutBitUniform(boolToIntEnc(hdr.UpdateMap))
		bw.PutBitUniform(1) // update segment data
		bw.PutBitUniform(boolToIntEnc(hdr.AbsoluteDelta))

		// Write quantizer deltas.
		// Must match decoder's GetSignedValue(7): 7 magnitude bits + 1 sign bit.
		for i := 0; i < NumMBSegments; i++ {
			q := int(hdr.Quantizer[i])
			if q != 0 {
				bw.PutBitUniform(1) // present
				absQ := q
				sign := 0
				if q < 0 {
					absQ = -q
					sign = 1
				}
				bw.PutBits(uint32(absQ), 7)
				bw.PutBitUniform(sign)
			} else {
				bw.PutBitUniform(0)
			}
		}

		// Write filter strength deltas.
		// Must match decoder's GetSignedValue(6): 6 magnitude bits + 1 sign bit.
		for i := 0; i < NumMBSegments; i++ {
			f := int(hdr.FilterStrength[i])
			if f != 0 {
				bw.PutBitUniform(1)
				absF := f
				sign := 0
				if f < 0 {
					absF = -f
					sign = 1
				}
				bw.PutBits(uint32(absF), 6)
				bw.PutBitUniform(sign)
			} else {
				bw.PutBitUniform(0)
			}
		}

		// Write segment probabilities.
		// Matching libwebp: only write the probability value if it differs from 255.
		if hdr.UpdateMap {
			for i := 0; i < MBFeatureTreeProbs; i++ {
				if enc.proba.Segments[i] != 255 {
					bw.PutBitUniform(1) // present
					bw.PutBits(uint32(enc.proba.Segments[i]), 8)
				} else {
					bw.PutBitUniform(0) // not present, use default (255)
				}
			}
		}
	}
}

// writeFilterHeader writes the filter parameters into partition 0.
// Matches libwebp's PutFilterHeader: use_lf_delta and need_update are both
// conditional on whether any delta is non-zero.
func (enc *VP8Encoder) writeFilterHeader(bw *bitio.BoolWriter) {
	fhdr := &enc.filterHdr

	bw.PutBitUniform(boolToIntEnc(fhdr.Simple))
	bw.PutBits(uint32(fhdr.Level), 6)
	bw.PutBits(uint32(fhdr.Sharpness), 3)

	// Check if any LF delta is actually non-zero (matching C's use_lf_delta).
	useLFDelta := fhdr.UseLFDelta
	bw.PutBitUniform(boolToIntEnc(useLFDelta))
	if useLFDelta {
		// Check if any delta values need updating (matching C's need_update).
		needUpdate := false
		for i := 0; i < NumRefLFDeltas; i++ {
			if fhdr.RefLFDelta[i] != 0 {
				needUpdate = true
				break
			}
		}
		if !needUpdate {
			for i := 0; i < NumModeLFDeltas; i++ {
				if fhdr.ModeLFDelta[i] != 0 {
					needUpdate = true
					break
				}
			}
		}
		bw.PutBitUniform(boolToIntEnc(needUpdate))
		if needUpdate {
			// Must match decoder's GetSignedValue(6): 6 magnitude bits + 1 sign bit.
			for i := 0; i < NumRefLFDeltas; i++ {
				d := fhdr.RefLFDelta[i]
				if d != 0 {
					bw.PutBitUniform(1)
					absD := d
					sign := 0
					if d < 0 {
						absD = -d
						sign = 1
					}
					bw.PutBits(uint32(absD), 6)
					bw.PutBitUniform(sign)
				} else {
					bw.PutBitUniform(0)
				}
			}
			for i := 0; i < NumModeLFDeltas; i++ {
				d := fhdr.ModeLFDelta[i]
				if d != 0 {
					bw.PutBitUniform(1)
					absD := d
					sign := 0
					if d < 0 {
						absD = -d
						sign = 1
					}
					bw.PutBits(uint32(absD), 6)
					bw.PutBitUniform(sign)
				} else {
					bw.PutBitUniform(0)
				}
			}
		}
	}
}

// writeQuantParams writes the quantizer parameters into partition 0.
func (enc *VP8Encoder) writeQuantParams(bw *bitio.BoolWriter) {
	// Base quantizer (7 bits).
	baseQ := enc.dqm[0].Quant
	bw.PutBits(uint32(baseQ), 7)

	// Delta y1_dc (always 0 in C libwebp).
	bw.PutSignedBits(enc.dqY1DC, 4)
	// Delta y2_dc (always 0 in C libwebp).
	bw.PutSignedBits(enc.dqY2DC, 4)
	// Delta y2_ac (always 0 in C libwebp).
	bw.PutSignedBits(enc.dqY2AC, 4)
	// Delta uv_dc (negative for better chroma quality).
	bw.PutSignedBits(enc.dqUVDC, 4)
	// Delta uv_ac.
	bw.PutSignedBits(enc.dqUVAC, 4)
}

// writeCoeffProba writes the coefficient probability updates into partition 0.
func (enc *VP8Encoder) writeCoeffProba(bw *bitio.BoolWriter) {
	for t := 0; t < NumTypes; t++ {
		for b := 0; b < NumBands; b++ {
			for c := 0; c < NumCTX; c++ {
				for p := 0; p < NumProbas; p++ {
					prob := enc.proba.Bands[t][b].Probas[c][p]
					update := CoeffsUpdateProba[t][b][c][p]
					def := CoeffsProba0[t][b][c][p]

					if prob != def {
						bw.PutBit(1, int(update))
						bw.PutBits(uint32(prob), 8)
					} else {
						bw.PutBit(0, int(update))
					}
				}
			}
		}
	}
}

// writeMBModes writes the macroblock prediction modes into partition 0.
// The encoding must exactly mirror the decoder's parseIntraMode, including
// probabilities and tree structure for I4/I16, I16 mode, I4 modes, and UV mode.
func (enc *VP8Encoder) writeMBModes(bw *bitio.BoolWriter) {
	// Track I4 mode context, matching the decoder's intraT/intraL.
	topModes := make([]uint8, enc.mbW*4)
	var leftModes [4]uint8

	for mbY := 0; mbY < enc.mbH; mbY++ {
		// Reset left context at row start (zero = DCPred = BDCPred).
		leftModes = [4]uint8{}

		for mbX := 0; mbX < enc.mbW; mbX++ {
			idx := mbY*enc.mbW + mbX
			info := &enc.mbInfo[idx]
			top := topModes[4*mbX : 4*mbX+4]

			// Segment index.
			if enc.segmentHdr.UseSegment && enc.segmentHdr.UpdateMap {
				writeSegmentID(bw, int(info.Segment), &enc.proba)
			}

			// Write skip flag if skip mode is enabled.
			if enc.numSkip > 0 {
				skipBit := 0
				if info.Skip {
					skipBit = 1
				}
				bw.PutBit(skipBit, int(enc.skipProba))
			}

			if info.MBType == 0 {
				// I16x16: first bit at prob 145 signals NOT I4x4.
				bw.PutBit(1, 145)
				writeI16Mode(bw, int(info.I16Mode))
				// Context: fill top and left with I16 mode.
				for i := 0; i < 4; i++ {
					top[i] = info.I16Mode
					leftModes[i] = info.I16Mode
				}
			} else {
				// I4x4: first bit at prob 145 signals I4x4.
				bw.PutBit(0, 145)
				for y := 0; y < 4; y++ {
					ymode := leftModes[y]
					for x := 0; x < 4; x++ {
						mode := info.Modes[y*4+x]
						prob := &KBModesProba[top[x]][ymode]
						writeI4ModeBits(bw, int(mode), prob)
						ymode = mode
						top[x] = mode
					}
					leftModes[y] = ymode
				}
			}

			// UV mode.
			writeUVMode(bw, int(info.UVMode))
		}
	}
}

// writeSegmentID encodes a segment ID using the segment tree.
func writeSegmentID(bw *bitio.BoolWriter, id int, proba *Proba) {
	bw.PutBit((id>>1)&1, int(proba.Segments[0]))
	if id >= 2 {
		bw.PutBit(id&1, int(proba.Segments[2]))
	} else {
		bw.PutBit(id&1, int(proba.Segments[1]))
	}
}

// writeI16Mode encodes the 16x16 luma prediction mode.
// Must match the decoder tree in parseIntraMode (probs 156, 163/128).
// The I4/I16 selector bit (prob 145) is written by the caller.
func writeI16Mode(bw *bitio.BoolWriter, mode int) {
	switch mode {
	case DCPred: // bit=0@156, bit=0@163
		bw.PutBit(0, 156)
		bw.PutBit(0, 163)
	case VPred: // bit=0@156, bit=1@163
		bw.PutBit(0, 156)
		bw.PutBit(1, 163)
	case HPred: // bit=1@156, bit=0@128
		bw.PutBit(1, 156)
		bw.PutBit(0, 128)
	case TMPred: // bit=1@156, bit=1@128
		bw.PutBit(1, 156)
		bw.PutBit(1, 128)
	}
}

// writeI4ModeBits encodes a single I4x4 mode using the KYModesIntra4 tree
// with context-dependent probabilities from KBModesProba.
// This exactly mirrors the decoder's tree walk in parseIntraMode.
func writeI4ModeBits(bw *bitio.BoolWriter, mode int, prob *[NumBModes - 1]uint8) {
	// Decoder: i = KYModesIntra4[0 + GetBit(prob[0])]
	//          while i > 0: i = KYModesIntra4[2*i + GetBit(prob[i])]
	// Encoder: at each node, determine which branch leads to the target mode.

	// First step: choose between tree[0] (bit=0) and tree[1] (bit=1).
	left := int(KYModesIntra4[0])
	bit := 0
	if !i4SubtreeContains(left, mode) {
		bit = 1
	}
	bw.PutBit(bit, int(prob[0]))
	i := int(KYModesIntra4[bit])

	// Subsequent steps.
	for i > 0 {
		left = int(KYModesIntra4[2*i])
		bit = 0
		if !i4SubtreeContains(left, mode) {
			bit = 1
		}
		bw.PutBit(bit, int(prob[i]))
		i = int(KYModesIntra4[2*i+bit])
	}
}

// i4SubtreeContains reports whether the subtree rooted at nodeOrLeaf
// contains the given mode value. Negative values are leaves; positive
// values are internal node indices into KYModesIntra4.
func i4SubtreeContains(nodeOrLeaf int, mode int) bool {
	if nodeOrLeaf <= 0 {
		return (-nodeOrLeaf) == mode
	}
	return i4SubtreeContains(int(KYModesIntra4[2*nodeOrLeaf]), mode) ||
		i4SubtreeContains(int(KYModesIntra4[2*nodeOrLeaf+1]), mode)
}

// writeUVMode encodes the chroma prediction mode.
// Must match the decoder tree in parseIntraMode (probs 142, 114, 183).
func writeUVMode(bw *bitio.BoolWriter, mode int) {
	switch mode {
	case DCPred: // bit=0@142
		bw.PutBit(0, 142)
	case VPred: // bit=1@142, bit=0@114
		bw.PutBit(1, 142)
		bw.PutBit(0, 114)
	case HPred: // bit=1@142, bit=1@114, bit=0@183
		bw.PutBit(1, 142)
		bw.PutBit(1, 114)
		bw.PutBit(0, 183)
	case TMPred: // bit=1@142, bit=1@114, bit=1@183
		bw.PutBit(1, 142)
		bw.PutBit(1, 114)
		bw.PutBit(1, 183)
	}
}

// boolToIntEnc converts bool to int for the encoder.
func boolToIntEnc(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AssembleRIFF wraps a VP8 bitstream in a minimal RIFF/WebP container.
func AssembleRIFF(vp8Data []byte) []byte {
	fileSize := 4 + 8 + len(vp8Data) // "WEBP" + chunk header + data
	if len(vp8Data)%2 != 0 {
		fileSize++ // padding
	}

	buf := make([]byte, 0, 12+8+len(vp8Data)+1)

	// RIFF header.
	buf = append(buf, 'R', 'I', 'F', 'F')
	var sizeBuf [4]byte
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(fileSize))
	buf = append(buf, sizeBuf[:]...)
	buf = append(buf, 'W', 'E', 'B', 'P')

	// VP8 chunk.
	buf = append(buf, 'V', 'P', '8', ' ')
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(len(vp8Data)))
	buf = append(buf, sizeBuf[:]...)
	buf = append(buf, vp8Data...)

	// Padding to even size.
	if len(vp8Data)%2 != 0 {
		buf = append(buf, 0)
	}

	return buf
}
