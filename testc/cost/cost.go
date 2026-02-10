//go:build testc

package cost

// #cgo CFLAGS: -I../libwebp
// #include "wrapper.h"
import "C"

func Init() {
	C.init_cost_tables()
}

func CEntropyCost(i int) uint16 {
	return uint16(C.c_entropy_cost(C.int(i)))
}

func CLevelFixedCost(i int) uint16 {
	return uint16(C.c_level_fixed_cost(C.int(i)))
}

func CEncBands(i int) uint8 {
	return uint8(C.c_enc_bands(C.int(i)))
}
