package sharpyuv

import (
	"errors"
	"image"
)

const (
	numIterations = 4
	yuvFix        = 16
	yuvHalf       = 1 << (yuvFix - 1)
	maxBitDepth   = 14
)

// Options controls the SharpYUV conversion.
type Options struct {
	Matrix       *ConversionMatrix
	TransferType TransferFunc
	SharpEnabled bool // When false, use standard (averaging) downsampling
}

// DefaultOptions returns default options using the WebP matrix and sRGB transfer.
func DefaultOptions() *Options {
	return &Options{
		Matrix:       GetConversionMatrix(MatrixWebP),
		TransferType: TransferSRGB,
		SharpEnabled: true,
	}
}

// Convert performs RGB to YUV420 conversion. When opts.SharpEnabled is true,
// it uses the sharp iterative algorithm that minimizes chroma subsampling
// artifacts. Otherwise it uses simple averaging.
//
// rgb must be packed RGB (3 bytes per pixel, row-major). The result is written
// to the provided YCbCr image which must be allocated with 4:2:0 subsampling
// and matching dimensions.
func Convert(rgb []byte, width, height, rgbStride int, yuv *image.YCbCr, opts *Options) error {
	if width <= 0 || height <= 0 || rgbStride <= 0 {
		return errors.New("sharpyuv: invalid dimensions")
	}
	if yuv == nil {
		return errors.New("sharpyuv: nil YCbCr output")
	}
	if yuv.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return errors.New("sharpyuv: output must be YCbCr 4:2:0")
	}
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.Matrix == nil {
		return errors.New("sharpyuv: nil conversion matrix")
	}
	// Use uint64 arithmetic to prevent integer overflow in buffer size check.
	if uint64(height-1)*uint64(rgbStride)+uint64(width)*3 > uint64(len(rgb)) {
		return errors.New("sharpyuv: rgb buffer too small")
	}

	if !opts.SharpEnabled {
		return convertStandard(rgb, width, height, rgbStride, yuv, opts.Matrix)
	}
	return convertSharp(rgb, width, height, rgbStride, yuv, opts.Matrix, opts.TransferType)
}

// --- Standard (simple averaging) conversion ---

func convertStandard(rgb []byte, width, height, rgbStride int, yuv *image.YCbCr, matrix *ConversionMatrix) error {
	// Compute full-resolution Y, and average U/V over 2x2 blocks.
	for j := 0; j < height; j++ {
		for i := 0; i < width; i++ {
			off := j*rgbStride + i*3
			r := int32(rgb[off+0])
			g := int32(rgb[off+1])
			b := int32(rgb[off+2])

			y := rgbToYUVComponent(r, g, b, matrix.RGBToY)
			yuv.Y[j*yuv.YStride+i] = clipU8(y)
		}
	}

	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	for j := 0; j < uvH; j++ {
		for i := 0; i < uvW; i++ {
			var sumR, sumG, sumB, count int32
			for dy := 0; dy < 2; dy++ {
				yy := j*2 + dy
				if yy >= height {
					continue
				}
				for dx := 0; dx < 2; dx++ {
					xx := i*2 + dx
					if xx >= width {
						continue
					}
					off := yy*rgbStride + xx*3
					sumR += int32(rgb[off+0])
					sumG += int32(rgb[off+1])
					sumB += int32(rgb[off+2])
					count++
				}
			}
			avgR := (sumR + count/2) / count
			avgG := (sumG + count/2) / count
			avgB := (sumB + count/2) / count

			u := rgbToYUVComponent(avgR, avgG, avgB, matrix.RGBToU)
			v := rgbToYUVComponent(avgR, avgG, avgB, matrix.RGBToV)
			yuv.Cb[j*yuv.CStride+i] = clipU8(u)
			yuv.Cr[j*yuv.CStride+i] = clipU8(v)
		}
	}
	return nil
}

func rgbToYUVComponent(r, g, b int32, coeffs [4]int32) int32 {
	luma := int64(coeffs[0])*int64(r) + int64(coeffs[1])*int64(g) + int64(coeffs[2])*int64(b) + int64(coeffs[3]) + int64(yuvHalf)
	return int32(luma >> yuvFix)
}

func clipU8(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// --- Sharp conversion (iterative error diffusion) ---

// getPrecisionShift returns the bit shift to apply to input samples for
// additional internal precision.
func getPrecisionShift(bitDepth int) int {
	if bitDepth+2 <= maxBitDepth {
		return 2
	}
	return maxBitDepth - bitDepth
}

// rgbToGray computes a fixed-point luminance from linear R, G, B values.
func rgbToGray(r, g, b int64) int {
	luma := 13933*r + 46871*g + 4732*b + int64(yuvHalf)
	return int(luma >> yuvFix)
}

// scaleDown averages a 2x2 block in the gamma domain, going through linear
// space for gamma-aware blending.
func scaleDown(a, b, c, d uint16, bitDepth int, tf TransferFunc) uint32 {
	la := GammaToLinear(a, bitDepth, tf)
	lb := GammaToLinear(b, bitDepth, tf)
	lc := GammaToLinear(c, bitDepth, tf)
	ld := GammaToLinear(d, bitDepth, tf)
	return uint32(LinearToGamma((la+lb+lc+ld+2)>>2, bitDepth, tf))
}

func clipBitDepth(y, bitDepth int) uint16 {
	max := (1 << uint(bitDepth)) - 1
	if y < 0 {
		return 0
	}
	if y > max {
		return uint16(max)
	}
	return uint16(y)
}

func convertSharp(rgb []byte, width, height, rgbStride int, yuv *image.YCbCr, matrix *ConversionMatrix, tf TransferFunc) error {
	initGammaTables()

	w := (width + 1) & ^1  // round up to even
	h := (height + 1) & ^1
	uvW := w >> 1
	uvH := h >> 1
	sfix := getPrecisionShift(8) // 8-bit RGB input
	yBitDepth := 8 + sfix

	// Allocate working buffers.
	tmpRow1 := make([]uint16, 3*w)   // one row of RGB (interleaved as R[w], G[w], B[w])
	tmpRow2 := make([]uint16, 3*w)   // second row
	bestY := make([]uint16, w*h)     // best Y (in working precision)
	targetY := make([]uint16, w*h)   // target Y (gamma-corrected luminance)
	bestUV := make([]int16, 3*uvW*uvH) // best UV residuals (R-W, G-W, B-W)
	targetUV := make([]int16, 3*uvW*uvH)
	bestRGBY := make([]uint16, w*2)
	bestRGBUV := make([]int16, 3*uvW)

	// Phase 1: Import RGB rows and compute initial Y and UV.
	for j := 0; j < height; j += 2 {
		isLastRow := (j == height-1)
		importOneRow(rgb, j, rgbStride, width, w, sfix, tmpRow1)
		if !isLastRow {
			importOneRow(rgb, j+1, rgbStride, width, w, sfix, tmpRow2)
		} else {
			copy(tmpRow2, tmpRow1)
		}

		byOff := (j / 2) * 2 * w
		tyOff := byOff
		buvOff := (j / 2) * 3 * uvW
		tuvOff := buvOff

		storeGray(tmpRow1, bestY[byOff:], w)
		storeGray(tmpRow2, bestY[byOff+w:], w)

		updateW(tmpRow1, targetY[tyOff:], w, yBitDepth, tf)
		updateW(tmpRow2, targetY[tyOff+w:], w, yBitDepth, tf)
		updateChroma(tmpRow1, tmpRow2, targetUV[tuvOff:], uvW, yBitDepth, tf)
		copy(bestUV[buvOff:buvOff+3*uvW], targetUV[tuvOff:tuvOff+3*uvW])
	}

	// Phase 2: Iterative refinement.
	diffYThreshold := uint64(3 * w * h)
	prevDiffYSum := ^uint64(0)

	for iter := 0; iter < numIterations; iter++ {
		var diffYSum uint64

		for j := 0; j < h; j += 2 {
			jUV := j / 2
			curUVOff := jUV * 3 * uvW
			prevUVOff := curUVOff
			if jUV > 0 {
				prevUVOff = (jUV - 1) * 3 * uvW
			}
			nextUVOff := curUVOff
			if j < h-2 {
				nextUVOff = (jUV + 1) * 3 * uvW
			}

			byOff := j * w
			tyOff := j * w

			interpolateTwoRows(
				bestY[byOff:], bestUV[prevUVOff:], bestUV[curUVOff:], bestUV[nextUVOff:],
				w, tmpRow1, tmpRow2, yBitDepth,
			)

			updateW(tmpRow1, bestRGBY[:w], w, yBitDepth, tf)
			updateW(tmpRow2, bestRGBY[w:], w, yBitDepth, tf)
			updateChroma(tmpRow1, tmpRow2, bestRGBUV, uvW, yBitDepth, tf)

			diffYSum += sharpYUVUpdateY(targetY[tyOff:], bestRGBY[:], bestY[byOff:], 2*w, yBitDepth)
			sharpYUVUpdateRGB(targetUV[jUV*3*uvW:], bestRGBUV, bestUV[curUVOff:], 3*uvW)
		}

		if iter > 0 {
			if diffYSum < diffYThreshold {
				break
			}
			if diffYSum > prevDiffYSum {
				break
			}
		}
		prevDiffYSum = diffYSum
	}

	// Phase 3: Final conversion from internal W/RGB representation to YUV.
	convertWRGBToYUV(bestY, bestUV, yuv, width, height, w, uvW, uvH, sfix, matrix)
	return nil
}

func importOneRow(rgb []byte, row, rgbStride, picWidth, w, sfix int, dst []uint16) {
	off := row * rgbStride
	for i := 0; i < picWidth; i++ {
		pix := off + i*3
		dst[i] = uint16(shiftVal(int(rgb[pix+0]), sfix))
		dst[i+w] = uint16(shiftVal(int(rgb[pix+1]), sfix))
		dst[i+2*w] = uint16(shiftVal(int(rgb[pix+2]), sfix))
	}
	// Replicate rightmost pixel if width is odd.
	if picWidth < w {
		dst[picWidth] = dst[picWidth-1]
		dst[picWidth+w] = dst[picWidth+w-1]
		dst[picWidth+2*w] = dst[picWidth+2*w-1]
	}
}

func storeGray(src []uint16, y []uint16, w int) {
	for i := 0; i < w; i++ {
		y[i] = uint16(rgbToGray(int64(src[i]), int64(src[i+w]), int64(src[i+2*w])))
	}
}

func updateW(src []uint16, dst []uint16, w, bitDepth int, tf TransferFunc) {
	for i := 0; i < w; i++ {
		r := GammaToLinear(src[i], bitDepth, tf)
		g := GammaToLinear(src[i+w], bitDepth, tf)
		b := GammaToLinear(src[i+2*w], bitDepth, tf)
		y := rgbToGray(int64(r), int64(g), int64(b))
		dst[i] = LinearToGamma(uint32(y), bitDepth, tf)
	}
}

func updateChroma(src1, src2 []uint16, dst []int16, uvW, bitDepth int, tf TransferFunc) {
	w := uvW * 2
	for i := 0; i < uvW; i++ {
		i2 := i * 2
		r := int(scaleDown(src1[i2], src1[i2+1], src2[i2], src2[i2+1], bitDepth, tf))
		g := int(scaleDown(src1[i2+w], src1[i2+w+1], src2[i2+w], src2[i2+w+1], bitDepth, tf))
		b := int(scaleDown(src1[i2+2*w], src1[i2+2*w+1], src2[i2+2*w], src2[i2+2*w+1], bitDepth, tf))
		grayVal := rgbToGray(int64(r), int64(g), int64(b))
		dst[i] = int16(r - grayVal)
		dst[i+uvW] = int16(g - grayVal)
		dst[i+2*uvW] = int16(b - grayVal)
	}
}

func filter2(a, b, w0, bitDepth int) uint16 {
	v0 := (a*3 + b + 2) >> 2
	return clipBitDepth(v0+w0, bitDepth)
}

func interpolateTwoRows(bestY []uint16, prevUV, curUV, nextUV []int16, w int, out1, out2 []uint16, bitDepth int) {
	uvW := w >> 1
	filterLen := (w - 1) >> 1

	for k := 0; k < 3; k++ {
		kUV := k * uvW
		kW := k * w

		// Boundary case i==0
		out1[kW] = filter2(int(curUV[kUV]), int(prevUV[kUV]), int(bestY[0]), bitDepth)
		out2[kW] = filter2(int(curUV[kUV]), int(nextUV[kUV]), int(bestY[w]), bitDepth)

		// Inner pixels via bilinear filter
		for i := 0; i < filterLen; i++ {
			a0 := int(curUV[kUV+i])
			a1 := int(curUV[kUV+i+1])
			b0 := int(prevUV[kUV+i])
			b1 := int(prevUV[kUV+i+1])
			v0 := (a0*9 + a1*3 + b0*3 + b1 + 8) >> 4
			v1 := (a1*9 + a0*3 + b1*3 + b0 + 8) >> 4
			out1[kW+2*i+1] = clipBitDepth(int(bestY[2*i+1])+v0, bitDepth)
			out1[kW+2*i+2] = clipBitDepth(int(bestY[2*i+2])+v1, bitDepth)

			nb0 := int(nextUV[kUV+i])
			nb1 := int(nextUV[kUV+i+1])
			nv0 := (a0*9 + a1*3 + nb0*3 + nb1 + 8) >> 4
			nv1 := (a1*9 + a0*3 + nb1*3 + nb0 + 8) >> 4
			out2[kW+2*i+1] = clipBitDepth(int(bestY[w+2*i+1])+nv0, bitDepth)
			out2[kW+2*i+2] = clipBitDepth(int(bestY[w+2*i+2])+nv1, bitDepth)
		}

		// Boundary case for even width
		if w&1 == 0 {
			out1[kW+w-1] = filter2(int(curUV[kUV+uvW-1]), int(prevUV[kUV+uvW-1]), int(bestY[w-1]), bitDepth)
			out2[kW+w-1] = filter2(int(curUV[kUV+uvW-1]), int(nextUV[kUV+uvW-1]), int(bestY[2*w-1]), bitDepth)
		}
	}
}

func sharpYUVUpdateY(target, src, dst []uint16, length, bitDepth int) uint64 {
	var diff uint64
	maxY := (1 << uint(bitDepth)) - 1
	for i := 0; i < length; i++ {
		diffY := int(target[i]) - int(src[i])
		newY := int(dst[i]) + diffY
		if newY < 0 {
			dst[i] = 0
		} else if newY > maxY {
			dst[i] = uint16(maxY)
		} else {
			dst[i] = uint16(newY)
		}
		if diffY < 0 {
			diff += uint64(-diffY)
		} else {
			diff += uint64(diffY)
		}
	}
	return diff
}

func sharpYUVUpdateRGB(target, src, dst []int16, length int) {
	for i := 0; i < length; i++ {
		diffUV := target[i] - src[i]
		dst[i] += diffUV
	}
}

func convertWRGBToYUV(bestY []uint16, bestUV []int16, yuv *image.YCbCr, width, height, w, uvW, uvH, sfix int, matrix *ConversionMatrix) {
	srounder := int64(1) << uint(yuvFix+sfix-1)

	// Scale the matrix offsets by the precision shift factor.
	// The input RGB values are already shifted left by sfix bits (from
	// importOneRow), so the coefficient products have yuvFix+sfix bits of
	// precision. The offset (coeffs[3]) is only in yuvFix precision and
	// must be shifted left by sfix to match, as done in the C reference
	// (Shift(yuv_matrix->rgb_to_y[3], sfix)).
	yOff := int64(shiftVal(int(matrix.RGBToY[3]), sfix))
	uOff := int64(shiftVal(int(matrix.RGBToU[3]), sfix))
	vOff := int64(shiftVal(int(matrix.RGBToV[3]), sfix))

	// Y plane
	for j := 0; j < height; j++ {
		for i := 0; i < width; i++ {
			yIdx := j*w + i
			uvIdx := (j/2)*3*uvW + (i >> 1)
			wVal := int64(bestY[yIdx])
			r := int64(bestUV[uvIdx]) + wVal
			g := int64(bestUV[uvIdx+uvW]) + wVal
			b := int64(bestUV[uvIdx+2*uvW]) + wVal
			yVal := int64(matrix.RGBToY[0])*r + int64(matrix.RGBToY[1])*g + int64(matrix.RGBToY[2])*b + yOff + srounder
			yuv.Y[j*yuv.YStride+i] = clipU8(int32(yVal >> uint(yuvFix+sfix)))
		}
	}

	// U/V planes
	for j := 0; j < uvH; j++ {
		for i := 0; i < uvW; i++ {
			uvIdx := j*3*uvW + i
			r := int64(bestUV[uvIdx])
			g := int64(bestUV[uvIdx+uvW])
			b := int64(bestUV[uvIdx+2*uvW])
			uVal := int64(matrix.RGBToU[0])*r + int64(matrix.RGBToU[1])*g + int64(matrix.RGBToU[2])*b + uOff + srounder
			vVal := int64(matrix.RGBToV[0])*r + int64(matrix.RGBToV[1])*g + int64(matrix.RGBToV[2])*b + vOff + srounder
			if j < len(yuv.Cb)/yuv.CStride && i < yuv.CStride {
				yuv.Cb[j*yuv.CStride+i] = clipU8(int32(uVal >> uint(yuvFix+sfix)))
				yuv.Cr[j*yuv.CStride+i] = clipU8(int32(vVal >> uint(yuvFix+sfix)))
			}
		}
	}
}
