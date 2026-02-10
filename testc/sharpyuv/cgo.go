//go:build testc

package sharpyuv

/*
#cgo CFLAGS: -I${SRCDIR}/../libwebp -I${SRCDIR}/../libwebp/src -DWEBP_DSP_OMIT_C_CODE=0 -DWEBP_NEON_OMIT_C_CODE=0
#include "wrapper.h"
*/
import "C"

import (
	"unsafe"

	"github.com/deepteams/webp/sharpyuv"
)

// CComputeConversionMatrix calls the C SharpYuvComputeConversionMatrix via wrapper.
func CComputeConversionMatrix(kr, kb float64, bitDepth int, r sharpyuv.Range) (y, u, v [4]int32) {
	var rangeMin int
	if r == sharpyuv.RangeLimited {
		rangeMin = 16 // non-zero signals limited
	}
	var cy, cu, cv [4]C.int
	C.c_compute_conversion_matrix(
		C.float(kr), C.float(kb), C.int(bitDepth),
		C.int(rangeMin), C.int(0),
		&cy[0], &cu[0], &cv[0],
	)
	for i := 0; i < 4; i++ {
		y[i] = int32(cy[i])
		u[i] = int32(cu[i])
		v[i] = int32(cv[i])
	}
	return
}

// CSharpYuvConvert calls the C SharpYuvConvert via wrapper.
// The C implementation internally scales matrix offsets ([3]) by Shift(v, sfix)
// where sfix=GetPrecisionShift(8)=2, i.e. offset << 2. The Go implementation
// also scales the offsets by the same amount in convertWRGBToYUV. We pass the
// matrix offsets as-is so that C's internal Shift produces the correct scaled
// values, matching Go's behavior.
func CSharpYuvConvert(rgb []byte, width, height, rgbStride int, matrix *sharpyuv.ConversionMatrix) (yPlane, uPlane, vPlane []byte, yStride, uStride, vStride int) {
	uvW := (width + 1) / 2
	uvH := (height + 1) / 2

	yStride = width
	uStride = uvW
	vStride = uvW

	yPlane = make([]byte, yStride*height)
	uPlane = make([]byte, uStride*uvH)
	vPlane = make([]byte, vStride*uvH)

	var cy, cu, cv [4]C.int
	for i := 0; i < 4; i++ {
		cy[i] = C.int(matrix.RGBToY[i])
		cu[i] = C.int(matrix.RGBToU[i])
		cv[i] = C.int(matrix.RGBToV[i])
	}

	C.c_sharp_yuv_convert(
		(*C.uint8_t)(unsafe.Pointer(&rgb[0])),
		C.int(width), C.int(height), C.int(rgbStride),
		(*C.uint8_t)(unsafe.Pointer(&yPlane[0])), C.int(yStride),
		(*C.uint8_t)(unsafe.Pointer(&uPlane[0])), C.int(uStride),
		(*C.uint8_t)(unsafe.Pointer(&vPlane[0])), C.int(vStride),
		&cy[0], &cu[0], &cv[0],
	)
	return
}
