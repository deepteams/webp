//go:build testc

package ssim

/*
#cgo CFLAGS: -I${SRCDIR}/../libwebp -DWEBP_DSP_OMIT_C_CODE=0
#include "wrapper.h"
*/
import "C"
import "unsafe"

func Init() {
	C.init_enc_dsp()
	C.init_ssim_dsp()
}

func CSSE4x4(a, b []byte) int {
	return int(C.c_sse_4x4((*C.uint8_t)(unsafe.Pointer(&a[0])),
		(*C.uint8_t)(unsafe.Pointer(&b[0]))))
}

func CSSE16x16(a, b []byte) int {
	return int(C.c_sse_16x16((*C.uint8_t)(unsafe.Pointer(&a[0])),
		(*C.uint8_t)(unsafe.Pointer(&b[0]))))
}

func CTDisto4x4(a, b []byte, w []uint16) int {
	return int(C.c_tdisto_4x4((*C.uint8_t)(unsafe.Pointer(&a[0])),
		(*C.uint8_t)(unsafe.Pointer(&b[0])),
		(*C.uint16_t)(unsafe.Pointer(&w[0]))))
}

func CSSIMFromStats(w, xm, ym, xxm, xym, yym uint32) float64 {
	return float64(C.c_ssim_from_stats(C.uint32_t(w), C.uint32_t(xm), C.uint32_t(ym),
		C.uint32_t(xxm), C.uint32_t(xym), C.uint32_t(yym)))
}
