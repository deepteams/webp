//go:build testc

package cliptables

// #cgo CFLAGS: -I../libwebp
// #include "wrapper.h"
import "C"

func Init() {
	C.init_clip_tables()
}

func CKsclip1(v int) int8 {
	return int8(C.c_ksclip1(C.int(v)))
}

func CKsclip2(v int) int8 {
	return int8(C.c_ksclip2(C.int(v)))
}

func CKclip1(v int) uint8 {
	return uint8(C.c_kclip1(C.int(v)))
}

func CKabs0(v int) uint8 {
	return uint8(C.c_kabs0(C.int(v)))
}

func CClip8b(v int) uint8 {
	return uint8(C.c_clip_8b(C.int(v)))
}
