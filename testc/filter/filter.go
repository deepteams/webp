//go:build testc

package filter

// #cgo CFLAGS: -I../libwebp -DWEBP_DSP_OMIT_C_CODE=0
// #include "wrapper.h"
import "C"

import "unsafe"

func Init() {
	C.init_filter()
}

func CSimpleVFilter16(p *byte, stride, thresh int) {
	C.c_simple_vfilter16((*C.uint8_t)(unsafe.Pointer(p)), C.int(stride), C.int(thresh))
}

func CSimpleHFilter16(p *byte, stride, thresh int) {
	C.c_simple_hfilter16((*C.uint8_t)(unsafe.Pointer(p)), C.int(stride), C.int(thresh))
}

func CSimpleVFilter16i(p *byte, stride, thresh int) {
	C.c_simple_vfilter16i((*C.uint8_t)(unsafe.Pointer(p)), C.int(stride), C.int(thresh))
}

func CSimpleHFilter16i(p *byte, stride, thresh int) {
	C.c_simple_hfilter16i((*C.uint8_t)(unsafe.Pointer(p)), C.int(stride), C.int(thresh))
}

func CVFilter16(p *byte, stride, thresh, ithresh, hevT int) {
	C.c_vfilter16((*C.uint8_t)(unsafe.Pointer(p)), C.int(stride), C.int(thresh), C.int(ithresh), C.int(hevT))
}

func CHFilter16(p *byte, stride, thresh, ithresh, hevT int) {
	C.c_hfilter16((*C.uint8_t)(unsafe.Pointer(p)), C.int(stride), C.int(thresh), C.int(ithresh), C.int(hevT))
}

func CVFilter8(u, v *byte, stride, thresh, ithresh, hevT int) {
	C.c_vfilter8((*C.uint8_t)(unsafe.Pointer(u)), (*C.uint8_t)(unsafe.Pointer(v)),
		C.int(stride), C.int(thresh), C.int(ithresh), C.int(hevT))
}

func CHFilter8(u, v *byte, stride, thresh, ithresh, hevT int) {
	C.c_hfilter8((*C.uint8_t)(unsafe.Pointer(u)), (*C.uint8_t)(unsafe.Pointer(v)),
		C.int(stride), C.int(thresh), C.int(ithresh), C.int(hevT))
}
