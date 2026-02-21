//go:build amd64

package dsp

// UpsampleLinePairNRGBA upsamples a pair of chroma rows and converts to NRGBA.
// On amd64, the diamond 4-tap kernel runs in Go and stores packed UV values
// (uint32, same format as loadUV), then batch YUV→NRGBA conversion uses
// AVX2 (8 pixels/iter) or SSE2 (4 pixels/iter) for exact fixed-point
// multiplication, replacing per-pixel table lookups.
func UpsampleLinePairNRGBA(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	alphaTop, alphaBot []byte,
	width int,
) {
	if width <= 0 {
		return
	}

	// Packed UV temp buffer (one uint32 per pixel per row).
	// Use stack-allocated array for common widths to avoid heap allocation.
	const maxStackWidth = 2048
	uvCount := width
	if botY != nil {
		uvCount = width * 2
	}
	var stackBuf [maxStackWidth * 2]uint32
	var packedUV []uint32
	if uvCount <= len(stackBuf) {
		packedUV = stackBuf[:uvCount]
	} else {
		packedUV = make([]uint32, uvCount)
	}
	tUV := packedUV[:width]
	var bUV []uint32
	if botY != nil {
		bUV = packedUV[width:]
	}

	// Phase 1: Diamond 4-tap kernel to interpolate chroma per pixel.
	lastPixelPair := (width - 1) >> 1
	tlUV := loadUV(topU[0], topV[0])
	lUV := loadUV(botU[0], botV[0])

	tUV[0] = (3*tlUV + lUV + 0x00020002) >> 2
	if botY != nil {
		bUV[0] = (3*lUV + tlUV + 0x00020002) >> 2
	}

	for x := 1; x <= lastPixelPair; x++ {
		tChroma := loadUV(topU[x], topV[x])
		bChroma := loadUV(botU[x], botV[x])

		avg := tlUV + tChroma + lUV + bChroma + 0x00080008
		diag12 := (avg + 2*(tChroma+lUV)) >> 3
		diag03 := (avg + 2*(tlUV+bChroma)) >> 3

		tUV[2*x-1] = (diag12 + tlUV) >> 1
		tUV[2*x] = (diag03 + tChroma) >> 1

		if botY != nil {
			bUV[2*x-1] = (diag03 + lUV) >> 1
			bUV[2*x] = (diag12 + bChroma) >> 1
		}

		tlUV = tChroma
		lUV = bChroma
	}

	if width&1 == 0 {
		tUV[width-1] = (3*tlUV + lUV + 0x00020002) >> 2
		if botY != nil {
			bUV[width-1] = (3*lUV + tlUV + 0x00020002) >> 2
		}
	}

	// Phase 2: Batch YUV→NRGBA conversion.
	// AVX2 processes 8 pixels/iter, SSE2 handles 4-pixel remainder, scalar for tail.
	batchStart := 0
	if hasAVX2 {
		n8 := width &^ 7
		if n8 > 0 {
			yuvPackedToNRGBABatchAVX2(topY[:n8], tUV[:n8], topDst[:n8*4], n8)
			batchStart = n8
		}
	}

	n4 := width &^ 3
	if batchStart < n4 {
		yuvPackedToNRGBABatchSSE2(topY[batchStart:n4], tUV[batchStart:n4], topDst[batchStart*4:n4*4], n4-batchStart)
	}

	for x := n4; x < width; x++ {
		u := int(tUV[x] & 0xff)
		v := int((tUV[x] >> 16) & 0xff)
		YUVToRGB(int(topY[x]), u, v, topDst[x*4:])
		topDst[x*4+3] = 255
	}
	if alphaTop != nil {
		for x := 0; x < width; x++ {
			topDst[x*4+3] = alphaTop[x]
		}
	}

	// Bottom row.
	if botY != nil {
		batchStart = 0
		if hasAVX2 {
			n8 := width &^ 7
			if n8 > 0 {
				yuvPackedToNRGBABatchAVX2(botY[:n8], bUV[:n8], botDst[:n8*4], n8)
				batchStart = n8
			}
		}

		if batchStart < n4 {
			yuvPackedToNRGBABatchSSE2(botY[batchStart:n4], bUV[batchStart:n4], botDst[batchStart*4:n4*4], n4-batchStart)
		}

		for x := n4; x < width; x++ {
			u := int(bUV[x] & 0xff)
			v := int((bUV[x] >> 16) & 0xff)
			YUVToRGB(int(botY[x]), u, v, botDst[x*4:])
			botDst[x*4+3] = 255
		}
		if alphaBot != nil {
			for x := 0; x < width; x++ {
				botDst[x*4+3] = alphaBot[x]
			}
		}
	}
}
