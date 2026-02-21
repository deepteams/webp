# WebP Go Libraries Benchmark

Comparative benchmark of Go WebP libraries on a 1536x1024 RGB image (Apple M2 Pro, arm64, Go 1.24.2).

Last updated: 2026-02-21

## Libraries Compared

| Library | Type | Lossy Encode | Lossless Encode | Decode |
|---------|------|:---:|:---:|:---:|
| [deepteams/webp](https://github.com/deepteams/webp) | Pure Go | Yes | Yes | Yes |
| [golang.org/x/image/webp](https://pkg.go.dev/golang.org/x/image/webp) | Pure Go | - | - | Yes |
| [gen2brain/webp](https://github.com/gen2brain/webp) | WASM (wazero) | Yes | Yes | Yes |
| [HugoSmits86/nativewebp](https://github.com/HugoSmits86/nativewebp) | Pure Go | - | Lossless | Yes |
| [chai2010/webp](https://github.com/chai2010/webp) | CGo (libwebp 1.0.2) | Yes | Yes | Yes |

## Results

All values are **medians of 3 runs** (`-count=3`). File sizes are identical across runs.

### Encode Lossy (Quality 75, 1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| **deepteams/webp** (Pure Go) | **74** | 1.5 MB | 135 | **193,298 B** |
| gen2brain/webp (WASM) | 83 | 18 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 105 | 234 KB | 4 | 209,180 B |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| **deepteams/webp** (Pure Go) | **229** | 58 MB | 1,264 | **1,833,338 B** |
| gen2brain/webp (WASM) | 275 | 514 KB | 12 | 2,053,844 B |
| nativewebp (Pure Go) | 429 | 89 MB | 2,156 | 2,011,754 B |
| chai2010/webp (CGo) | 1,283 | 3.5 MB | 5 | 1,751,450 B |

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **12.7** | 7.2 MB | 24 |
| **deepteams/webp** (Pure Go) | **13.5** | 2.6 MB | 7 |
| golang.org/x/image/webp | 24.8 | 2.6 MB | 13 |
| gen2brain/webp (WASM) | 31.6 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **32.2** | 14.7 MB | 33 |
| **deepteams/webp** (Pure Go) | **40.1** | 8.8 MB | 258 |
| nativewebp (Pure Go) | 52.2 | 6.4 MB | 50 |
| golang.org/x/image/webp | 55.9 | 7.3 MB | 1,126 |
| gen2brain/webp (WASM) | 54.8 | 10.6 MB | 50 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.4** | 266 B | 12 |
| **deepteams/webp** (Pure Go) | 3.7 | 29 KB | 79 |
| chai2010/webp (CGo) | 6.1 | 795 KB | 131,077 |

## Changes vs Previous Run (2026-02-21 refresh)

| Benchmark | Metric | Previous (02-21) | Current | Delta |
|-----------|--------|-------------------|---------|-------|
| Encode Lossy deepteams | Time | 78 ms | **74 ms** | **-5%** |
| Encode Lossless deepteams | B/op | 84 MB | **58 MB** | **-31%** |
| Decode Lossless deepteams | Time | 40.9 ms | **40.1 ms** | -2% |

All other metrics within noise margin (<5%).

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (74ms) is the fastest lossy encoder, 12% faster than gen2brain WASM (83ms) and 30% faster than chai2010 CGo (105ms) — while producing the smallest files (193 KB vs 209-253 KB).

2. **Fastest lossless encoder overall**: deepteams/webp lossless encode (229ms) is 17% faster than gen2brain WASM (275ms), 1.9x faster than nativewebp, and 5.6x faster than chai2010 CGo — while producing the best compression among pure Go libraries (1,833 KB).

3. **Near-CGo lossy decode in pure Go**: deepteams/webp lossy decode (13.5ms) is only 6% behind chai2010 CGo (12.7ms), 1.8x faster than x/image, and 2.3x faster than gen2brain WASM — with the fewest allocations (7) and lowest memory (2.6 MB) of any decoder.

4. **Fastest pure Go lossless decoder**: deepteams/webp lossless decode (40.1ms) is 23% faster than nativewebp, 28% faster than x/image, and 27% faster than gen2brain, trailing only chai2010 CGo by 25%.

5. **Best lossy compression**: deepteams/webp produces the smallest lossy files (193 KB vs 209-253 KB), 8% smaller than chai2010 CGo and 24% smaller than gen2brain.

6. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 29 KB and 79 allocs, vs chai2010/webp which uses 795 KB and 131K allocs due to CGo overhead.

7. **No CGo, no WASM**: deepteams/webp and nativewebp are the only libraries that work without CGo or WASM runtimes. Cross-compilation just works.

8. **Feature completeness**: deepteams/webp is the only pure Go library supporting both lossy and lossless encoding + decoding, plus animation, alpha, and metadata.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
