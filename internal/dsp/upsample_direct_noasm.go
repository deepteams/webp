//go:build !amd64

package dsp

// UpsampleLinePairNRGBA upsamples a pair of chroma rows and converts to NRGBA.
// On non-amd64 platforms, this uses the pure Go implementation.
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
