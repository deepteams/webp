package lossless

// TransformType enumerates the VP8L image transform types.
type TransformType int

const (
	PredictorTransform    TransformType = 0
	CrossColorTransform   TransformType = 1
	SubtractGreenTransform TransformType = 2
	ColorIndexingTransform TransformType = 3
)

// MaxTransforms is the maximum number of transforms allowed in a VP8L stream.
const MaxTransforms = NumTransforms

// Transform represents a single VP8L image transform.
type Transform struct {
	Type  TransformType
	Bits  int       // subsampling bits defining the transform window
	XSize int       // transform window width
	YSize int       // transform window height
	Data  []uint32  // transform data (predictor modes, color transform, palette, etc.)
}
