//go:build testc

package predict

// #cgo CFLAGS: -I../libwebp -DWEBP_DSP_OMIT_C_CODE=0
// #include "wrapper.h"
import "C"

import "unsafe"

func Init() {
	C.init_predict()
}

func CDc4(dst *byte)           { C.c_dc4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CTm4(dst *byte)           { C.c_tm4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CVe4(dst *byte)           { C.c_ve4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CHe4(dst *byte)           { C.c_he4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CRd4(dst *byte)           { C.c_rd4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CVr4(dst *byte)           { C.c_vr4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CLd4(dst *byte)           { C.c_ld4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CVl4(dst *byte)           { C.c_vl4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CHd4(dst *byte)           { C.c_hd4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CHu4(dst *byte)           { C.c_hu4((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc16(dst *byte)          { C.c_dc16((*C.uint8_t)(unsafe.Pointer(dst))) }
func CTm16(dst *byte)          { C.c_tm16((*C.uint8_t)(unsafe.Pointer(dst))) }
func CVe16(dst *byte)          { C.c_ve16((*C.uint8_t)(unsafe.Pointer(dst))) }
func CHe16(dst *byte)          { C.c_he16((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc16NoTop(dst *byte)     { C.c_dc16_no_top((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc16NoLeft(dst *byte)    { C.c_dc16_no_left((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc16NoTopLeft(dst *byte) { C.c_dc16_no_top_left((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc8uv(dst *byte)         { C.c_dc8uv((*C.uint8_t)(unsafe.Pointer(dst))) }
func CTm8uv(dst *byte)         { C.c_tm8uv((*C.uint8_t)(unsafe.Pointer(dst))) }
func CVe8uv(dst *byte)         { C.c_ve8uv((*C.uint8_t)(unsafe.Pointer(dst))) }
func CHe8uv(dst *byte)         { C.c_he8uv((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc8uvNoTop(dst *byte)    { C.c_dc8uv_no_top((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc8uvNoLeft(dst *byte)   { C.c_dc8uv_no_left((*C.uint8_t)(unsafe.Pointer(dst))) }
func CDc8uvNoTopLeft(dst *byte) {
	C.c_dc8uv_no_top_left((*C.uint8_t)(unsafe.Pointer(dst)))
}
