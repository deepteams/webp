// Package sharpyuv implements sharp RGB to YUV420 conversion that minimizes
// chroma subsampling artifacts using iterative error diffusion.
//
// This is a pure Go port of libwebp's sharpyuv library.
package sharpyuv

import "math"

// Range specifies the range of YUV values.
type Range int

const (
	RangeFull    Range = iota // YUV values between [0, 255] for 8-bit
	RangeLimited              // Y in [16, 235], UV in [16, 240] for 8-bit
)

// ColorSpace defines the color primaries for YUV conversion.
type ColorSpace struct {
	Kr       float64 // Luma coefficient for red
	Kb       float64 // Luma coefficient for blue
	BitDepth int     // 8, 10, or 12
	Range    Range
}

// Predefined color spaces.
var (
	BT601 = ColorSpace{Kr: 0.2990, Kb: 0.1140, BitDepth: 8, Range: RangeLimited}
	BT709 = ColorSpace{Kr: 0.2126, Kb: 0.0722, BitDepth: 8, Range: RangeLimited}

	BT601Full = ColorSpace{Kr: 0.2990, Kb: 0.1140, BitDepth: 8, Range: RangeFull}
	BT709Full = ColorSpace{Kr: 0.2126, Kb: 0.0722, BitDepth: 8, Range: RangeFull}
)

// ConversionMatrix holds the RGB to YUV conversion coefficients in 16-bit
// fixed point.
//
// The conversion is:
//
//	y = (RGBToY[0]*r + RGBToY[1]*g + RGBToY[2]*b + RGBToY[3] + (1<<15)) >> 16
//	u = (RGBToU[0]*r + RGBToU[1]*g + RGBToU[2]*b + RGBToU[3] + (1<<15)) >> 16
//	v = (RGBToV[0]*r + RGBToV[1]*g + RGBToV[2]*b + RGBToV[3] + (1<<15)) >> 16
type ConversionMatrix struct {
	RGBToY [4]int32
	RGBToU [4]int32
	RGBToV [4]int32
}

// MatrixType identifies a predefined conversion matrix.
type MatrixType int

const (
	MatrixWebP MatrixType = iota
	MatrixRec601Limited
	MatrixRec601Full
	MatrixRec709Limited
	MatrixRec709Full
)

// Predefined conversion matrices (fixed-point 16-bit precision).
var (
	// WebP's matrix, similar but not identical to Rec601Limited.
	webpMatrix = ConversionMatrix{
		RGBToY: [4]int32{16839, 33059, 6420, 16 << 16},
		RGBToU: [4]int32{-9719, -19081, 28800, 128 << 16},
		RGBToV: [4]int32{28800, -24116, -4684, 128 << 16},
	}
	rec601LimitedMatrix = ConversionMatrix{
		RGBToY: [4]int32{16829, 33039, 6416, 16 << 16},
		RGBToU: [4]int32{-9714, -19071, 28784, 128 << 16},
		RGBToV: [4]int32{28784, -24103, -4681, 128 << 16},
	}
	rec601FullMatrix = ConversionMatrix{
		RGBToY: [4]int32{19595, 38470, 7471, 0},
		RGBToU: [4]int32{-11058, -21710, 32768, 128 << 16},
		RGBToV: [4]int32{32768, -27439, -5329, 128 << 16},
	}
	rec709LimitedMatrix = ConversionMatrix{
		RGBToY: [4]int32{11966, 40254, 4064, 16 << 16},
		RGBToU: [4]int32{-6596, -22189, 28784, 128 << 16},
		RGBToV: [4]int32{28784, -26145, -2639, 128 << 16},
	}
	rec709FullMatrix = ConversionMatrix{
		RGBToY: [4]int32{13933, 46871, 4732, 0},
		RGBToU: [4]int32{-7509, -25259, 32768, 128 << 16},
		RGBToV: [4]int32{32768, -29763, -3005, 128 << 16},
	}
)

// GetConversionMatrix returns a predefined conversion matrix.
func GetConversionMatrix(mt MatrixType) *ConversionMatrix {
	switch mt {
	case MatrixWebP:
		m := webpMatrix
		return &m
	case MatrixRec601Limited:
		m := rec601LimitedMatrix
		return &m
	case MatrixRec601Full:
		m := rec601FullMatrix
		return &m
	case MatrixRec709Limited:
		m := rec709LimitedMatrix
		return &m
	case MatrixRec709Full:
		m := rec709FullMatrix
		return &m
	default:
		return nil
	}
}

func toFixed16(f float64) int32 {
	return int32(math.Floor(f*(1<<16) + 0.5))
}

// ComputeConversionMatrix fills a ConversionMatrix from a ColorSpace definition.
func ComputeConversionMatrix(cs *ColorSpace) *ConversionMatrix {
	kr := cs.Kr
	kb := cs.Kb
	kg := 1.0 - kr - kb
	cb := 0.5 / (1.0 - kb)
	cr := 0.5 / (1.0 - kr)

	shift := cs.BitDepth - 8
	denom := float64(int(1)<<uint(cs.BitDepth) - 1)

	scaleY := 1.0
	addY := 0.0
	scaleU := cb
	scaleV := cr
	addUV := float64(int(128) << shift)

	if cs.Range == RangeLimited {
		scaleY *= float64(int(219)<<shift) / denom
		scaleU *= float64(int(224)<<shift) / denom
		scaleV *= float64(int(224)<<shift) / denom
		addY = float64(int(16) << shift)
	}

	return &ConversionMatrix{
		RGBToY: [4]int32{
			toFixed16(kr * scaleY),
			toFixed16(kg * scaleY),
			toFixed16(kb * scaleY),
			toFixed16(addY),
		},
		RGBToU: [4]int32{
			toFixed16(-kr * scaleU),
			toFixed16(-kg * scaleU),
			toFixed16((1 - kb) * scaleU),
			toFixed16(addUV),
		},
		RGBToV: [4]int32{
			toFixed16((1 - kr) * scaleV),
			toFixed16(-kg * scaleV),
			toFixed16(-kb * scaleV),
			toFixed16(addUV),
		},
	}
}
