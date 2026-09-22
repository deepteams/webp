//go:build arm64

package dsp

//go:noescape
func yuvPackedToNRGBABatchNEON(y []byte, packedUV []uint32, dst []byte, width int)

// UpsampleLinePairNRGBA upsamples a pair of chroma rows and converts to NRGBA.
// On arm64, the diamond 4-tap kernel runs in Go and stores packed UV values
// (uint32, same format as loadUV), then batch YUV→NRGBA conversion uses NEON
// (8 pixels/iter) for exact fixed-point multiplication, replacing per-pixel
// table lookups. The result is bit-exact with upsampleLinePairNRGBAGo.
func UpsampleLinePairNRGBA(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	alphaTop, alphaBot []byte,
	width int,
) {
	const maxStackWidth = 2048
	var stackBuf [maxStackWidth * 2]uint32
	UpsampleLinePairNRGBAWithScratch(
		topY, botY, topU, topV, botU, botV, topDst, botDst,
		alphaTop, alphaBot, width, stackBuf[:],
	)
}

// UpsampleLinePairNRGBAWithScratch is equivalent to
// UpsampleLinePairNRGBA but reuses caller-provided packed-UV storage.
func UpsampleLinePairNRGBAWithScratch(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	alphaTop, alphaBot []byte,
	width int,
	packedUV []uint32,
) {
	if width <= 0 {
		return
	}

	uvCount := width
	if botY != nil {
		uvCount = width * 2
	}
	if cap(packedUV) < uvCount {
		packedUV = make([]uint32, uvCount)
	} else {
		packedUV = packedUV[:uvCount]
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

	// Phase 2: Batch YUV→NRGBA conversion. NEON processes 8 pixels/iter,
	// scalar Go handles the tail.
	n8 := width &^ 7
	if n8 > 0 {
		yuvPackedToNRGBABatchNEON(topY[:n8], tUV[:n8], topDst[:n8*4], n8)
	}
	for x := n8; x < width; x++ {
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
		if n8 > 0 {
			yuvPackedToNRGBABatchNEON(botY[:n8], bUV[:n8], botDst[:n8*4], n8)
		}
		for x := n8; x < width; x++ {
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
