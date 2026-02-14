package dsp

import "math"

// SSIM and PSNR metric computation matching libwebp ssim.c.

// kWeightSum is the squared sum of the hat-shaped kernel coefficients
// used in SSIMFromStats: sum({1,2,3,4,3,2,1})^2 = 16^2 = 256.
const kWeightSum = 16 * 16

// DistoStats accumulates statistics for SSIM computation over a block.
type DistoStats struct {
	W              uint32 // number of samples
	Xm, Ym        uint32 // sum of x, sum of y
	Xxm, Xym, Yym uint32 // sum of x*x, x*y, y*y
}

// Accumulate adds a single pixel pair (x, y) to the statistics with weight 1.
func (s *DistoStats) Accumulate(x, y uint8) {
	s.W++
	s.Xm += uint32(x)
	s.Ym += uint32(y)
	s.Xxm += uint32(x) * uint32(x)
	s.Xym += uint32(x) * uint32(y)
	s.Yym += uint32(y) * uint32(y)
}

// AccumulateWeighted adds a single pixel pair (x, y) with a given weight
// to the statistics. Matches C libwebp ssim.c weighted accumulation.
func (s *DistoStats) AccumulateWeighted(x, y uint8, w uint32) {
	s.W += w
	s.Xm += w * uint32(x)
	s.Ym += w * uint32(y)
	s.Xxm += w * uint32(x) * uint32(x)
	s.Xym += w * uint32(x) * uint32(y)
	s.Yym += w * uint32(y) * uint32(y)
}

// ssimKernel is the hat-shaped kernel used by the SSIM computation.
// Sum of coefficients is 16. Matches C kWeight[2*VP8_SSIM_KERNEL+1].
const ssimKernel = 3 // VP8_SSIM_KERNEL

var ssimWeight = [2*ssimKernel + 1]uint32{1, 2, 3, 4, 3, 2, 1}

// ssimCalculation computes the SSIM value from accumulated statistics using
// integer arithmetic matching libwebp's SSIMCalculation (ssim.c:30-53).
// N is the number of samples (e.g. kWeightSum or stats.W).
func ssimCalculation(s *DistoStats, N uint32) float64 {
	w2 := uint64(N) * uint64(N)
	C1 := 20 * w2
	C2 := 60 * w2
	C3 := 8 * 8 * w2 // 'dark' limit

	xmxm := uint64(s.Xm) * uint64(s.Xm)
	ymym := uint64(s.Ym) * uint64(s.Ym)

	// Dark zone check: if both signals are very dark, return 1.0.
	if xmxm+ymym < C3 {
		return 1.0
	}

	xmym := int64(s.Xm) * int64(s.Ym)
	sxy := int64(s.Xym)*int64(N) - xmym // can be negative
	sxx := uint64(s.Xxm)*uint64(N) - xmxm
	syy := uint64(s.Yym)*uint64(N) - ymym

	// Clamp negative sxy to 0 for numerator.
	var sxyPos uint64
	if sxy > 0 {
		sxyPos = uint64(sxy)
	}

	// Descale by 8 to prevent overflow during fnum/fden multiply.
	numS := (2*sxyPos + C2) >> 8
	denS := (sxx + syy + C2) >> 8
	fnum := (2*uint64(xmym) + C1) * numS
	fden := (xmxm + ymym + C1) * denS

	if fden == 0 {
		return 1.0
	}
	return float64(fnum) / float64(fden)
}

// SSIMFromStats computes the SSIM value from accumulated statistics
// using the fixed kWeightSum. Matches libwebp VP8SSIMFromStats.
// Returns 0 when the weight is zero (no samples accumulated).
func SSIMFromStats(s *DistoStats) float64 {
	if s.W == 0 {
		return 0
	}
	return ssimCalculation(s, kWeightSum)
}

// SSIMFromStatsClipped computes the SSIM value using the actual
// accumulated weight. Matches libwebp VP8SSIMFromStatsClipped.
func SSIMFromStatsClipped(s *DistoStats) float64 {
	return ssimCalculation(s, s.W)
}

// SSIMFromBlocks computes the SSIM between two blocks of pixels.
// Each block has the given width, height, and stride.
func SSIMFromBlocks(pix, ref []byte, width, height, pixStride, refStride int) float64 {
	var s DistoStats
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			s.Accumulate(pix[x+y*pixStride], ref[x+y*refStride])
		}
	}
	return SSIMFromStatsClipped(&s)
}

// SSIMGet computes the SSIM at a full (non-clipped) 7x7 window using
// the hat-shaped kernel weights. Matches C SSIMGet_C (ssim.c:93-110).
// src1 and src2 must have at least (2*ssimKernel+1) accessible rows/cols.
func SSIMGet(src1 []byte, stride1 int, src2 []byte, stride2 int) float64 {
	var s DistoStats
	for y := 0; y <= 2*ssimKernel; y++ {
		for x := 0; x <= 2*ssimKernel; x++ {
			w := ssimWeight[x] * ssimWeight[y]
			s1 := src1[x+y*stride1]
			s2 := src2[x+y*stride2]
			s.AccumulateWeighted(s1, s2, w)
		}
	}
	return SSIMFromStats(&s)
}

// SSIMGetClipped computes the SSIM at a window that may be clipped at image
// boundaries. Uses the hat-shaped kernel weights. Matches C SSIMGetClipped_C
// (ssim.c:63-91).
func SSIMGetClipped(src1 []byte, stride1 int, src2 []byte, stride2 int,
	xo, yo, W, H int) float64 {
	var s DistoStats
	ymin := yo - ssimKernel
	if ymin < 0 {
		ymin = 0
	}
	ymax := yo + ssimKernel
	if ymax > H-1 {
		ymax = H - 1
	}
	xmin := xo - ssimKernel
	if xmin < 0 {
		xmin = 0
	}
	xmax := xo + ssimKernel
	if xmax > W-1 {
		xmax = W - 1
	}
	for y := ymin; y <= ymax; y++ {
		for x := xmin; x <= xmax; x++ {
			w := ssimWeight[ssimKernel+x-xo] * ssimWeight[ssimKernel+y-yo]
			s1 := src1[x+y*stride1]
			s2 := src2[x+y*stride2]
			s.AccumulateWeighted(s1, s2, w)
		}
	}
	return SSIMFromStatsClipped(&s)
}

// PSNRFromSSE computes the PSNR from the sum of squared errors.
func PSNRFromSSE(sse uint64, count int) float64 {
	if sse == 0 || count == 0 {
		return 99.0 // perfect
	}
	mse := float64(sse) / float64(count)
	return 10.0 * math.Log10(255.0*255.0/mse)
}

// SSE computes the sum of squared errors between two pixel blocks.
func SSE(pix, ref []byte, width, height, pixStride, refStride int) uint64 {
	var sse uint64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			d := int(pix[x+y*pixStride]) - int(ref[x+y*refStride])
			sse += uint64(d * d)
		}
	}
	return sse
}

// MetricFunc types for SSE/SSIM per 4x4 or 16x16 blocks.
type MetricFunc func(pix, ref []byte) int

// sse4x4 computes SSE for a 4x4 block with BPS stride.
// Fully unrolled for performance.
func sse4x4(pix, ref []byte) int {
	_ = pix[3+3*BPS]
	_ = ref[3+3*BPS]
	// Row 0.
	d0 := int(pix[0]) - int(ref[0])
	d1 := int(pix[1]) - int(ref[1])
	d2 := int(pix[2]) - int(ref[2])
	d3 := int(pix[3]) - int(ref[3])
	sse := d0*d0 + d1*d1 + d2*d2 + d3*d3
	// Row 1.
	d0 = int(pix[0+BPS]) - int(ref[0+BPS])
	d1 = int(pix[1+BPS]) - int(ref[1+BPS])
	d2 = int(pix[2+BPS]) - int(ref[2+BPS])
	d3 = int(pix[3+BPS]) - int(ref[3+BPS])
	sse += d0*d0 + d1*d1 + d2*d2 + d3*d3
	// Row 2.
	d0 = int(pix[0+2*BPS]) - int(ref[0+2*BPS])
	d1 = int(pix[1+2*BPS]) - int(ref[1+2*BPS])
	d2 = int(pix[2+2*BPS]) - int(ref[2+2*BPS])
	d3 = int(pix[3+2*BPS]) - int(ref[3+2*BPS])
	sse += d0*d0 + d1*d1 + d2*d2 + d3*d3
	// Row 3.
	d0 = int(pix[0+3*BPS]) - int(ref[0+3*BPS])
	d1 = int(pix[1+3*BPS]) - int(ref[1+3*BPS])
	d2 = int(pix[2+3*BPS]) - int(ref[2+3*BPS])
	d3 = int(pix[3+3*BPS]) - int(ref[3+3*BPS])
	sse += d0*d0 + d1*d1 + d2*d2 + d3*d3
	return sse
}

// sse16x16 computes SSE for a 16x16 block with BPS stride.
// Fully unrolled for performance matching sse4x4 pattern.
func sse16x16(pix, ref []byte) int {
	_ = pix[15+15*BPS]
	_ = ref[15+15*BPS]
	sse := 0
	for j := 0; j < 16; j++ {
		off := j * BPS
		d0 := int(pix[off+0]) - int(ref[off+0])
		d1 := int(pix[off+1]) - int(ref[off+1])
		d2 := int(pix[off+2]) - int(ref[off+2])
		d3 := int(pix[off+3]) - int(ref[off+3])
		d4 := int(pix[off+4]) - int(ref[off+4])
		d5 := int(pix[off+5]) - int(ref[off+5])
		d6 := int(pix[off+6]) - int(ref[off+6])
		d7 := int(pix[off+7]) - int(ref[off+7])
		d8 := int(pix[off+8]) - int(ref[off+8])
		d9 := int(pix[off+9]) - int(ref[off+9])
		d10 := int(pix[off+10]) - int(ref[off+10])
		d11 := int(pix[off+11]) - int(ref[off+11])
		d12 := int(pix[off+12]) - int(ref[off+12])
		d13 := int(pix[off+13]) - int(ref[off+13])
		d14 := int(pix[off+14]) - int(ref[off+14])
		d15 := int(pix[off+15]) - int(ref[off+15])
		sse += d0*d0 + d1*d1 + d2*d2 + d3*d3 +
			d4*d4 + d5*d5 + d6*d6 + d7*d7 +
			d8*d8 + d9*d9 + d10*d10 + d11*d11 +
			d12*d12 + d13*d13 + d14*d14 + d15*d15
	}
	return sse
}

// SSE4x4 is the function variable for 4x4 SSE.
var SSE4x4 MetricFunc

// SSE16x16 is the function variable for 16x16 SSE.
var SSE16x16 MetricFunc

// Perceptual weights for Hadamard-domain distortion (kWeightY from libwebp).
var kWeightY = [16]uint16{
	38, 32, 20, 9,
	32, 28, 17, 7,
	20, 17, 10, 4,
	9, 7, 4, 2,
}

// tTransform computes the weighted Hadamard transform sum for a 4x4 block.
// Matches C TTransform (enc.c:615-648) exactly.
func tTransform(in []byte, w []uint16) int {
	var tmp [16]int

	// Horizontal Hadamard pass.
	// Matches C: a0=in[0]+in[2], a1=in[1]+in[3], a2=in[1]-in[3], a3=in[0]-in[2]
	for i := 0; i < 4; i++ {
		off := i * BPS
		a0 := int(in[off+0]) + int(in[off+2])
		a1 := int(in[off+1]) + int(in[off+3])
		a2 := int(in[off+1]) - int(in[off+3])
		a3 := int(in[off+0]) - int(in[off+2])
		tmp[0+i*4] = a0 + a1
		tmp[1+i*4] = a3 + a2
		tmp[2+i*4] = a3 - a2
		tmp[3+i*4] = a0 - a1
	}

	// Vertical pass with weights.
	// Matches C: a0=tmp[0+i]+tmp[8+i], a1=tmp[4+i]+tmp[12+i],
	//            a2=tmp[4+i]-tmp[12+i], a3=tmp[0+i]-tmp[8+i]
	sum := 0
	for i := 0; i < 4; i++ {
		a0 := tmp[0*4+i] + tmp[2*4+i]
		a1 := tmp[1*4+i] + tmp[3*4+i]
		a2 := tmp[1*4+i] - tmp[3*4+i]
		a3 := tmp[0*4+i] - tmp[2*4+i]

		b0 := a0 + a1
		b1 := a3 + a2
		b2 := a3 - a2
		b3 := a0 - a1

		sum += int(w[0*4+i]) * abs(b0)
		sum += int(w[1*4+i]) * abs(b1)
		sum += int(w[2*4+i]) * abs(b2)
		sum += int(w[3*4+i]) * abs(b3)
	}
	return sum
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// TDisto4x4 computes the perceptual Hadamard-domain distortion for a 4x4 block.
// Both a and b are BPS-strided buffers.
func TDisto4x4(a, b []byte) int {
	sum1 := tTransform(a, kWeightY[:])
	sum2 := tTransform(b, kWeightY[:])
	d := sum2 - sum1
	if d < 0 {
		d = -d
	}
	return d >> 5
}

// TDisto16x16 computes the perceptual Hadamard-domain distortion for a 16x16 block.
// Both a and b are BPS-strided buffers.
func TDisto16x16(a, b []byte) int {
	d := 0
	for y := 0; y < 16*BPS; y += 4 * BPS {
		for x := 0; x < 16; x += 4 {
			d += TDisto4x4(a[x+y:], b[x+y:])
		}
	}
	return d
}

func initSSIM() {
	SSE4x4 = sse4x4
	SSE16x16 = sse16x16
}
