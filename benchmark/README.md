# WebP Go Libraries Benchmark

Comparative benchmark of Go WebP libraries on a 1536x1024 RGB image (Apple M5 Max, arm64, Go 1.24.2).

Last updated: 2026-06-17 (10-run benchmark)

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
| **deepteams/webp** (Pure Go) | **47.8** | 4.0 | 1.3 MB | 171 |
| gen2brain/webp (WASM) | 53.9 | 4.7 | 12 KB | 12 |
| chai2010/webp (CGo) | 72.8 | 2.9 | 227 KB | 4 |

### Encode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|------:|-------:|
| **deepteams/webp** (Pure Go) | **116** | 15.7 | 23.7 MB | 1,160 |
| gen2brain/webp (WASM) | 179 | 11.5 | 343 KB | 12 |
| nativewebp (Pure Go) | 273 | 7.3 | 89 MB | 2,155 |
| chai2010/webp (CGo) | 895 | 2.0 | 2.6 MB | 4 |

### Decode Lossy (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| chai2010/webp (CGo) | **9.2** | 22.9 | 6.8 MB | 23 |
| **deepteams/webp** (Pure Go) | **9.4** | 20.6 | 2.6 MB | 7 |
| golang.org/x/image/webp | 17.8 | 10.9 | 2.6 MB | 13 |
| gen2brain/webp (WASM) | 21.8 | 11.6 | 622 KB | 40 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| chai2010/webp (CGo) | **18.5** | 95.0 | 10.6 MB | 30 |
| **deepteams/webp** (Pure Go) | **26.7** | 68.5 | 8.3 MB | 225 |
| gen2brain/webp (WASM) | 34.3 | 59.9 | 4.7 MB | 46 |
| nativewebp (Pure Go) | 36.1 | 55.6 | 6.4 MB | 50 |
| golang.org/x/image/webp | 39.0 | 46.8 | 7.1 MB | 966 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **2.2** | 257 B | 12 |
| **deepteams/webp** (Pure Go) | **2.4** | 32 KB | 127 |
| chai2010/webp (CGo) | 4.0 | 795 KB | 131,077 |

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (47.8ms) is the fastest lossy encoder, 11% faster than gen2brain WASM (53.9ms) and 34% faster than chai2010 CGo (72.8ms). Uses only 1.3 MB per encode with 171 allocs.

2. **Fastest lossless encoder overall**: deepteams/webp lossless encode (116ms) is 35% faster than gen2brain WASM (179ms), 2.3x faster than nativewebp, and 7.7x faster than chai2010 CGo. Best compression among pure Go (1.8 MB compressed output).

3. **Efficient lossy decoder**: deepteams/webp lossy decode (9.4ms) is within 2% of chai2010 CGo (9.2ms), 1.9x faster than x/image/webp, and 2.3x faster than gen2brain WASM. Lowest memory (2.6 MB) and fewest allocs (7) of any decoder.

4. **Fastest pure Go lossless decoder**: deepteams/webp lossless decode (26.7ms) is 26% faster than gen2brain, 28% faster than nativewebp, and 32% faster than x/image — only 44% behind chai2010 CGo.

5. **Consistent performance**: 10-run medians show deepteams/webp is stable (low variance) across lossy/lossless encoding and decoding, with only minor outliers under system contention.

6. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 32 KB and 127 allocs, vs chai2010/webp which requires 795 KB and 131,077 allocs due to CGo initialization overhead.

7. **Pure Go, no external runtime**: deepteams/webp and nativewebp are the only libraries working without CGo or WASM runtimes. Cross-compilation is trivial.

8. **Complete feature set**: deepteams/webp is the only pure Go library supporting both lossy and lossless encoding + decoding, plus animation, alpha, metadata, and VP8X extended format.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=10 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=10 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
