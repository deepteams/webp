//go:build testc

package alpha

// #cgo CFLAGS: -I../libwebp -DWEBP_DSP_OMIT_C_CODE=0
// #include "wrapper.h"
// #include <stdlib.h>
import "C"
import "unsafe"

func CMultARGBRow(row []uint32, width int, inverse bool) {
	inv := C.int(0)
	if inverse {
		inv = 1
	}
	C.c_mult_argb_row((*C.uint32_t)(unsafe.Pointer(&row[0])), C.int(width), inv)
}

func CDispatchAlpha(alpha []byte, alphaStride, width, height int,
	dst []byte, dstStride int) int {
	return int(C.c_dispatch_alpha(
		(*C.uint8_t)(unsafe.Pointer(&alpha[0])), C.int(alphaStride),
		C.int(width), C.int(height),
		(*C.uint8_t)(unsafe.Pointer(&dst[0])), C.int(dstStride)))
}

func CExtractAlpha(argb []byte, argbStride, width, height int,
	alphaOut []byte, alphaStride int) int {
	return int(C.c_extract_alpha(
		(*C.uint8_t)(unsafe.Pointer(&argb[0])), C.int(argbStride),
		C.int(width), C.int(height),
		(*C.uint8_t)(unsafe.Pointer(&alphaOut[0])), C.int(alphaStride)))
}

func CHasAlpha8b(src []byte, length int) bool {
	return C.c_has_alpha_8b((*C.uint8_t)(unsafe.Pointer(&src[0])), C.int(length)) != 0
}

func CHasAlpha32b(src []byte, length int) bool {
	return C.c_has_alpha_32b((*C.uint8_t)(unsafe.Pointer(&src[0])), C.int(length)) != 0
}
