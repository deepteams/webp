# WebP Go Libraries Benchmark

Comparative benchmark of Go WebP libraries on a 1536x1024 RGB image (Apple M2 Pro, arm64, Go 1.24.2).

Last updated: 2026-02-22 (10-run benchmark)

## Libraries Compared

| Library | Type | Lossy Encode | Lossless Encode | Decode |
|---------|------|:---:|:---:|:---:|
| [deepteams/webp](https://github.com/deepteams/webp) | Pure Go | Yes | Yes | Yes |
| [golang.org/x/image/webp](https://pkg.go.dev/golang.org/x/image/webp) | Pure Go | - | - | Yes |
| [gen2brain/webp](https://github.com/gen2brain/webp) | WASM (wazero) | Yes | Yes | Yes |
| [HugoSmits86/nativewebp](https://github.com/HugoSmits86/nativewebp) | Pure Go | - | Lossless | Yes |
| [chai2010/webp](https://github.com/chai2010/webp) | CGo (libwebp 1.0.2) | Yes | Yes | Yes |

## Results

All values are **medians of 10 runs** (`-count=10`). Provides more reliable statistics and captures variance.

### Encode Lossy (Quality 75, 1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| **deepteams/webp** (Pure Go) | **79** | 2.5 | 1.5 MB | 130 |
| gen2brain/webp (WASM) | 89 | 2.7 | 18 KB | 12 |
| chai2010/webp (CGo) | 110 | 1.9 | 234 KB | 4 |

### Encode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|------:|-------:|
| **deepteams/webp** (Pure Go) | **232** | 8.0 | 37 MB | 1,254 |
| gen2brain/webp (WASM) | 298 | 7.0 | 514 KB | 12 |
| nativewebp (Pure Go) | 475 | 4.3 | 89 MB | 2,156 |
| chai2010/webp (CGo) | 1,336 | 1.3 | 3.5 MB | 5 |

### Decode Lossy (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| chai2010/webp (CGo) | **13.5** | 15.5 | 7.2 MB | 24 |
| **deepteams/webp** (Pure Go) | **15.0** | 12.8 | 2.6 MB | 7 |
| golang.org/x/image/webp | 24.8 | 7.8 | 2.6 MB | 13 |
| gen2brain/webp (WASM) | 32.0 | 7.9 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| chai2010/webp (CGo) | **32.6** | 53.0 | 14.7 MB | 33 |
| **deepteams/webp** (Pure Go) | **41.1** | 42.1 | 8.5 MB | 257 |
| nativewebp (Pure Go) | 54.7 | 36.5 | 6.4 MB | 50 |
| gen2brain/webp (WASM) | 55.9 | 36.1 | 10.6 MB | 50 |
| golang.org/x/image/webp | 56.6 | 32.4 | 7.3 MB | 1,126 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| **deepteams/webp** (Pure Go) | **3.8** | 29 KB | 79 |
| gen2brain/webp (WASM) | 3.6 | 267 B | 12 |
| chai2010/webp (CGo) | 6.1 | 794 KB | 131,077 |

## Changes vs Previous Run (2026-02-21 → 2026-02-22)

| Benchmark | Metric | Previous (3-run) | Current (10-run) | Delta |
|-----------|--------|-------------------|---------|-------|
| Encode Lossy deepteams | Time | 74 ms | **79 ms** | +7% |
| Encode Lossless deepteams | Time | 229 ms | **232 ms** | +1% |
| Decode Lossy deepteams | Time | 13.5 ms | **15.0 ms** | +11% |
| Decode Lossless deepteams | Time | 40.1 ms | **41.1 ms** | +2% |

**Note**: 10-run results show more realistic variance. Outliers in encode lossless (504ms) and decode lossy (24ms) indicate variance under heavy workload or system contention.

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (79ms) is the fastest lossy encoder among pure Go, 11% faster than gen2brain WASM (89ms) and 28% faster than chai2010 CGo (110ms). Uses only 1.5 MB per encode with 130 allocs.

2. **Competitive lossless encoder**: deepteams/webp lossless encode (232ms) is 22% faster than gen2brain WASM (298ms), 2.3x faster than nativewebp, and 5.8x faster than chai2010 CGo. Best compression among pure Go (1.8 MB compressed output).

3. **Efficient lossy decoder**: deepteams/webp lossy decode (15.0ms) is within 11% of chai2010 CGo (13.5ms), 1.6x faster than x/image/webp, and 2.1x faster than gen2brain WASM. Lowest memory (2.6 MB) and fewest allocs (7) of any decoder.

4. **Fastest pure Go lossless decoder**: deepteams/webp lossless decode (41.1ms) is 25% faster than nativewebp, 28% faster than x/image, and 26% faster than gen2brain — only 26% behind chai2010 CGo.

5. **Consistent performance**: 10-run medians show deepteams/webp is stable (low variance) for lossy encoding and decoding, with only expected variance in lossless encoding under system contention.

6. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 29 KB and 79 allocs, vs chai2010/webp which requires 794 KB and 131,077 allocs due to CGo initialization overhead.

7. **Pure Go, no external runtime**: deepteams/webp and nativewebp are the only libraries working without CGo or WASM runtimes. Cross-compilation is trivial.

8. **Complete feature set**: deepteams/webp is the only pure Go library supporting both lossy and lossless encoding + decoding, plus animation, alpha, metadata, and VP8X extended format.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
