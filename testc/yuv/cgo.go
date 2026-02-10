//go:build testc

package yuv

/*
#cgo CFLAGS: -I${SRCDIR}/../libwebp -DWEBP_DSP_OMIT_C_CODE=0 -DWEBP_NEON_OMIT_C_CODE=0
#include "wrapper.h"
*/
import "C"

func CYUVToR(y, v int) uint8 {
	return uint8(C.c_yuv_to_r(C.int(y), C.int(v)))
}

func CYUVToG(y, u, v int) uint8 {
	return uint8(C.c_yuv_to_g(C.int(y), C.int(u), C.int(v)))
}

func CYUVToB(y, u int) uint8 {
	return uint8(C.c_yuv_to_b(C.int(y), C.int(u)))
}

func CRGBToY(r, g, b, rounding int) uint8 {
	return uint8(C.c_rgb_to_y(C.int(r), C.int(g), C.int(b), C.int(rounding)))
}

func CRGBToU(r, g, b, rounding int) uint8 {
	return uint8(C.c_rgb_to_u(C.int(r), C.int(g), C.int(b), C.int(rounding)))
}

func CRGBToV(r, g, b, rounding int) uint8 {
	return uint8(C.c_rgb_to_v(C.int(r), C.int(g), C.int(b), C.int(rounding)))
}
