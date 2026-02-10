#include "wrapper.h"

// Include the C implementations directly.
#include "src/utils/bit_writer_utils.c"
#include "src/utils/bit_reader_utils.c"
// Need the inline VP8GetBit definition (includes bit_reader_inl_utils.h).
#include "src/utils/bit_reader_inl_utils.h"

// Boolean writer: encode a sequence of (bit, prob) pairs and return the bytes.
int c_bool_write_sequence(const int* bits, const int* probs, int count,
                          uint8_t* out_buf, int* out_size) {
    VP8BitWriter bw;
    if (!VP8BitWriterInit(&bw, (size_t)(count + 256))) return 0;

    for (int i = 0; i < count; i++) {
        VP8PutBit(&bw, bits[i], probs[i]);
    }
    uint8_t* buf = VP8BitWriterFinish(&bw);
    size_t sz = VP8BitWriterSize(&bw);

    if (bw.error) {
        VP8BitWriterWipeOut(&bw);
        return 0;
    }

    memcpy(out_buf, buf, sz);
    *out_size = (int)sz;
    VP8BitWriterWipeOut(&bw);
    return 1;
}

// Boolean reader: decode a sequence of bits from data using given probs.
void c_bool_read_sequence(const uint8_t* data, int size,
                          const int* probs, int count, int* out_bits) {
    VP8BitReader br;
    VP8InitBitReader(&br, data, (size_t)size);

    for (int i = 0; i < count; i++) {
        out_bits[i] = VP8GetBit(&br, probs[i], "test");
    }
}

// Lossless writer: encode a sequence of (value, nbits) pairs and return bytes.
int c_lossless_write_sequence(const uint32_t* values, const int* nbits,
                              int count, uint8_t* out_buf, int* out_size) {
    VP8LBitWriter bw;
    if (!VP8LBitWriterInit(&bw, (size_t)(count * 4 + 256))) return 0;

    for (int i = 0; i < count; i++) {
        VP8LPutBits(&bw, values[i], nbits[i]);
    }
    uint8_t* buf = VP8LBitWriterFinish(&bw);
    size_t sz = bw.cur - bw.buf;

    if (bw.error) {
        VP8LBitWriterWipeOut(&bw);
        return 0;
    }

    memcpy(out_buf, buf, sz);
    *out_size = (int)sz;
    VP8LBitWriterWipeOut(&bw);
    return 1;
}

// Lossless reader: decode a sequence of values from data using given nbits.
void c_lossless_read_sequence(const uint8_t* data, int size,
                              const int* nbits, int count,
                              uint32_t* out_values) {
    VP8LBitReader br;
    VP8LInitBitReader(&br, data, (size_t)size);

    for (int i = 0; i < count; i++) {
        out_values[i] = VP8LReadBits(&br, nbits[i]);
    }
}
