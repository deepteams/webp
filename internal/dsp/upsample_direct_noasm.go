//go:build !amd64 && !arm64

package dsp

// UpsampleLinePairNRGBA upsamples a pair of chroma rows and converts to NRGBA.
// On platforms without an assembly implementation, this uses the pure Go
// implementation.
func UpsampleLinePairNRGBA(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	alphaTop, alphaBot []byte,
	width int,
) {
	upsampleLinePairNRGBAGo(topY, botY, topU, topV, botU, botV, topDst, botDst, alphaTop, alphaBot, width)
}

// UpsampleLinePairNRGBAWithScratch is equivalent to
// UpsampleLinePairNRGBA. The pure Go implementation does not need scratch.
func UpsampleLinePairNRGBAWithScratch(
	topY, botY []byte,
	topU, topV []byte,
	botU, botV []byte,
	topDst, botDst []byte,
	alphaTop, alphaBot []byte,
	width int,
	_ []uint32,
) {
	upsampleLinePairNRGBAGo(topY, botY, topU, topV, botU, botV, topDst, botDst, alphaTop, alphaBot, width)
}
