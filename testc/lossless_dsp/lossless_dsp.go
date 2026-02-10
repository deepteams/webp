//go:build testc

package lossless_dsp

// #include "wrapper.h"
import "C"
import "unsafe"

// CPredictor calls the C predictor for the given mode (0-13).
// topSlice must have at least 3 elements: [TL, T, TR].
// For C: we pass &topSlice[1] so that top[0]=T, top[-1]=TL, top[1]=TR.
func CPredictor(mode int, left uint32, topSlice []uint32) uint32 {
	l := C.uint32_t(left)
	// topSlice layout: [TL, T, TR, ...]
	// C expects: top points to T, top[-1]=TL, top[1]=TR
	return uint32(C.c_predictor(C.int(mode),
		(*C.uint32_t)(unsafe.Pointer(&l)),
		(*C.uint32_t)(unsafe.Pointer(&topSlice[1]))))
}

// CAddGreen calls the C VP8LAddGreenToBlueAndRed_C.
func CAddGreen(src []uint32, numPixels int, dst []uint32) {
	C.c_add_green(
		(*C.uint32_t)(unsafe.Pointer(&src[0])),
		C.int(numPixels),
		(*C.uint32_t)(unsafe.Pointer(&dst[0])))
}

// CSubtractGreen calls the C VP8LSubtractGreenFromBlueAndRed_C.
func CSubtractGreen(argb []uint32, numPixels int) {
	C.c_subtract_green(
		(*C.uint32_t)(unsafe.Pointer(&argb[0])),
		C.int(numPixels))
}

// CTransformColor calls the C VP8LTransformColor_C.
func CTransformColor(greenToRed, greenToBlue, redToBlue uint8,
	data []uint32, numPixels int) {
	m := C.CMultipliers{
		green_to_red:  C.uint8_t(greenToRed),
		green_to_blue: C.uint8_t(greenToBlue),
		red_to_blue:   C.uint8_t(redToBlue),
	}
	C.c_transform_color(&m,
		(*C.uint32_t)(unsafe.Pointer(&data[0])),
		C.int(numPixels))
}

// CTransformColorInverse calls the C VP8LTransformColorInverse_C.
func CTransformColorInverse(greenToRed, greenToBlue, redToBlue uint8,
	src []uint32, numPixels int, dst []uint32) {
	m := C.CMultipliers{
		green_to_red:  C.uint8_t(greenToRed),
		green_to_blue: C.uint8_t(greenToBlue),
		red_to_blue:   C.uint8_t(redToBlue),
	}
	C.c_transform_color_inverse(&m,
		(*C.uint32_t)(unsafe.Pointer(&src[0])),
		C.int(numPixels),
		(*C.uint32_t)(unsafe.Pointer(&dst[0])))
}
