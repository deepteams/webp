//go:build testc

package roundtrip

/*
#cgo CFLAGS: -I${SRCDIR}/../libwebp -I${SRCDIR}/../libwebp/src -DWEBP_DSP_OMIT_C_CODE=0
#include "wrapper.h"
#include <stdlib.h>
*/
import "C"

import (
	"image"
	"unsafe"
)

// CEncLossy encodes RGBA pixels via C libwebp lossy encoder.
func CEncLossy(img []byte, width, height, stride int, quality float32) ([]byte, error) {
	var outPtr *C.uint8_t
	var outSize C.size_t
	ok := C.c_encode_lossy(
		(*C.uint8_t)(unsafe.Pointer(&img[0])),
		C.int(width), C.int(height), C.int(stride),
		C.float(quality),
		&outPtr, &outSize,
	)
	if ok == 0 {
		return nil, image.ErrFormat
	}
	defer C.c_free(outPtr)
	return C.GoBytes(unsafe.Pointer(outPtr), C.int(outSize)), nil
}

// CEncLossless encodes RGBA pixels via C libwebp lossless encoder.
func CEncLossless(img []byte, width, height, stride int) ([]byte, error) {
	var outPtr *C.uint8_t
	var outSize C.size_t
	ok := C.c_encode_lossless(
		(*C.uint8_t)(unsafe.Pointer(&img[0])),
		C.int(width), C.int(height), C.int(stride),
		&outPtr, &outSize,
	)
	if ok == 0 {
		return nil, image.ErrFormat
	}
	defer C.c_free(outPtr)
	return C.GoBytes(unsafe.Pointer(outPtr), C.int(outSize)), nil
}

// CValidateWebP validates a WebP bitstream using C libwebp.
func CValidateWebP(data []byte) (int, int, bool) {
	var w, h C.int
	ok := C.c_validate_webp(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
		&w, &h,
	)
	return int(w), int(h), ok != 0
}

// CDecRGBA decodes a WebP bitstream via C libwebp and returns RGBA pixels + dimensions.
func CDecRGBA(data []byte) ([]byte, int, int, error) {
	var w, h C.int
	var outPtr *C.uint8_t
	ok := C.c_decode_rgba(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.size_t(len(data)),
		&w, &h, &outPtr,
	)
	if ok == 0 {
		return nil, 0, 0, image.ErrFormat
	}
	defer C.c_free(outPtr)

	width := int(w)
	height := int(h)
	pix := C.GoBytes(unsafe.Pointer(outPtr), C.int(width*height*4))
	return pix, width, height, nil
}
