//go:build testc

package transforms

/*
#cgo CFLAGS: -I${SRCDIR}/../libwebp -DWEBP_DSP_OMIT_C_CODE=0 -DWEBP_NEON_OMIT_C_CODE=0
#include "wrapper.h"
*/
import "C"
import "unsafe"

const BPS = 32

func CTransformOne(in []int16, dst []byte) {
	C.c_transform_one((*C.int16_t)(unsafe.Pointer(&in[0])), (*C.uint8_t)(unsafe.Pointer(&dst[0])))
}

func CTransformDC(in []int16, dst []byte) {
	C.c_transform_dc((*C.int16_t)(unsafe.Pointer(&in[0])), (*C.uint8_t)(unsafe.Pointer(&dst[0])))
}

func CTransformAC3(in []int16, dst []byte) {
	C.c_transform_ac3((*C.int16_t)(unsafe.Pointer(&in[0])), (*C.uint8_t)(unsafe.Pointer(&dst[0])))
}

func CTransformWHT(in []int16, out []int16) {
	C.c_transform_wht((*C.int16_t)(unsafe.Pointer(&in[0])), (*C.int16_t)(unsafe.Pointer(&out[0])))
}

func CFTransform(src, ref []byte, out []int16) {
	C.c_ftransform((*C.uint8_t)(unsafe.Pointer(&src[0])), (*C.uint8_t)(unsafe.Pointer(&ref[0])), (*C.int16_t)(unsafe.Pointer(&out[0])))
}

func CFTransformWHT(in, out []int16) {
	C.c_ftransform_wht((*C.int16_t)(unsafe.Pointer(&in[0])), (*C.int16_t)(unsafe.Pointer(&out[0])))
}

func CITransform(ref []byte, in []int16, dst []byte, doTwo bool) {
	dt := 0
	if doTwo {
		dt = 1
	}
	C.c_itransform((*C.uint8_t)(unsafe.Pointer(&ref[0])), (*C.int16_t)(unsafe.Pointer(&in[0])), (*C.uint8_t)(unsafe.Pointer(&dst[0])), C.int(dt))
}
