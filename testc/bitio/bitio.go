//go:build testc

package bitio

// #cgo CFLAGS: -I../libwebp -DWEBP_NEON_OMIT_C_CODE=0
// #include "wrapper.h"
// #include <stdlib.h>
import "C"
import "unsafe"

// CBoolWriteSequence encodes a sequence of (bit, prob) pairs using the C VP8
// boolean encoder and returns the resulting bytes.
func CBoolWriteSequence(bits []int, probs []int) []byte {
	count := len(bits)
	if count == 0 {
		return nil
	}

	cBits := make([]C.int, count)
	cProbs := make([]C.int, count)
	for i := 0; i < count; i++ {
		cBits[i] = C.int(bits[i])
		cProbs[i] = C.int(probs[i])
	}

	// Allocate generous output buffer.
	outBuf := make([]byte, count+4096)
	var outSize C.int

	ok := C.c_bool_write_sequence(
		(*C.int)(unsafe.Pointer(&cBits[0])),
		(*C.int)(unsafe.Pointer(&cProbs[0])),
		C.int(count),
		(*C.uint8_t)(unsafe.Pointer(&outBuf[0])),
		&outSize,
	)
	if ok == 0 {
		return nil
	}
	return append([]byte{}, outBuf[:int(outSize)]...)
}

// CBoolReadSequence decodes a sequence of bits from data using the C VP8
// boolean decoder with the given probabilities.
func CBoolReadSequence(data []byte, probs []int) []int {
	count := len(probs)
	if count == 0 || len(data) == 0 {
		return nil
	}

	cProbs := make([]C.int, count)
	for i := 0; i < count; i++ {
		cProbs[i] = C.int(probs[i])
	}

	outBits := make([]C.int, count)

	C.c_bool_read_sequence(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.int(len(data)),
		(*C.int)(unsafe.Pointer(&cProbs[0])),
		C.int(count),
		(*C.int)(unsafe.Pointer(&outBits[0])),
	)

	result := make([]int, count)
	for i := 0; i < count; i++ {
		result[i] = int(outBits[i])
	}
	return result
}

// CLosslessWriteSequence encodes a sequence of (value, nbits) pairs using the
// C VP8L lossless bit writer and returns the resulting bytes.
func CLosslessWriteSequence(values []uint32, nbits []int) []byte {
	count := len(values)
	if count == 0 {
		return nil
	}

	cValues := make([]C.uint32_t, count)
	cNbits := make([]C.int, count)
	for i := 0; i < count; i++ {
		cValues[i] = C.uint32_t(values[i])
		cNbits[i] = C.int(nbits[i])
	}

	outBuf := make([]byte, count*4+4096)
	var outSize C.int

	ok := C.c_lossless_write_sequence(
		(*C.uint32_t)(unsafe.Pointer(&cValues[0])),
		(*C.int)(unsafe.Pointer(&cNbits[0])),
		C.int(count),
		(*C.uint8_t)(unsafe.Pointer(&outBuf[0])),
		&outSize,
	)
	if ok == 0 {
		return nil
	}
	return append([]byte{}, outBuf[:int(outSize)]...)
}

// CLosslessReadSequence decodes a sequence of values from data using the
// C VP8L lossless bit reader with the given bit counts.
func CLosslessReadSequence(data []byte, nbits []int) []uint32 {
	count := len(nbits)
	if count == 0 || len(data) == 0 {
		return nil
	}

	cNbits := make([]C.int, count)
	for i := 0; i < count; i++ {
		cNbits[i] = C.int(nbits[i])
	}

	outValues := make([]C.uint32_t, count)

	C.c_lossless_read_sequence(
		(*C.uint8_t)(unsafe.Pointer(&data[0])),
		C.int(len(data)),
		(*C.int)(unsafe.Pointer(&cNbits[0])),
		C.int(count),
		(*C.uint32_t)(unsafe.Pointer(&outValues[0])),
	)

	result := make([]uint32, count)
	for i := 0; i < count; i++ {
		result[i] = uint32(outValues[i])
	}
	return result
}
