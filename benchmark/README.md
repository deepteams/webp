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
| **deepteams/webp** (Pure Go) | **78** | 1.5 MB | 126 | **193,298 B** |
| gen2brain/webp (WASM) | 81 | 18 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 106 | 234 KB | 4 | 209,180 B |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| **deepteams/webp** (Pure Go) | **224** | 84 MB | 1,290 | **1,833,338 B** |
| gen2brain/webp (WASM) | 271 | 514 KB | 12 | 2,053,844 B |
| nativewebp (Pure Go) | 419 | 89 MB | 2,156 | 2,011,754 B |
| chai2010/webp (CGo) | 1,317 | 3.5 MB | 5 | 1,751,450 B |

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **12.6** | 7.2 MB | 24 |
| **deepteams/webp** (Pure Go) | **13.4** | 2.6 MB | 7 |
| golang.org/x/image/webp | 24.7 | 2.6 MB | 13 |
| gen2brain/webp (WASM) | 31.7 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **33.8** | 14.7 MB | 33 |
| **deepteams/webp** (Pure Go) | **40.9** | 8.8 MB | 257 |
| nativewebp (Pure Go) | 52.3 | 6.4 MB | 50 |
| golang.org/x/image/webp | 55.5 | 7.3 MB | 1,126 |
| gen2brain/webp (WASM) | 56.9 | 10.6 MB | 50 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.3** | 266 B | 12 |
| **deepteams/webp** (Pure Go) | 3.9 | 30 KB | 80 |
| chai2010/webp (CGo) | 6.1 | 795 KB | 131,077 |

## Changes vs Previous Run (2026-02-15 -> 2026-02-21)

| Benchmark | Metric | Previous | Current | Delta |
|-----------|--------|----------|---------|-------|
| Encode Lossy deepteams | Time | 104 ms | **78 ms** | **-25%** |
| Decode Lossy deepteams | Time | 26.5 ms | **13.4 ms** | **-49%** |
| Decode Lossy deepteams | B/op | 6.5 MB | **2.6 MB** | **-60%** |
| Decode Lossless deepteams | Time | 57.8 ms | **40.9 ms** | **-29%** |
| Decode Lossless deepteams | Allocs | 340 | **257** | **-24%** |
| Encode Lossless deepteams | Time | 400 ms | **224 ms** | **-44%** |
| Encode Lossless deepteams | B/op | 115 MB | **84 MB** | **-27%** |
| Encode Lossless deepteams | Allocs | 1,393 | **1,290** | **-7%** |

All other metrics within noise margin (<5%).

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (78ms) is now the fastest lossy encoder, 4% faster than gen2brain WASM (81ms) and 36% faster than chai2010 CGo (106ms) — while producing the smallest files (193 KB vs 209-253 KB).

2. **Fastest lossless encoder overall**: deepteams/webp lossless encode (224ms) is 17% faster than gen2brain WASM (271ms), 2x faster than nativewebp, and 6x faster than chai2010 CGo — while producing the best compression among pure Go libraries (1,833 KB).

3. **Near-CGo lossy decode in pure Go**: deepteams/webp lossy decode (13.4ms) is only 6% behind chai2010 CGo (12.6ms), 1.8x faster than x/image, and 2.4x faster than gen2brain WASM — with the fewest allocations (7) and lowest memory (2.6 MB) of any decoder.

4. **Fastest pure Go lossless decoder**: deepteams/webp lossless decode (40.9ms) is 22% faster than nativewebp, 27% faster than x/image, and 28% faster than gen2brain, trailing only chai2010 CGo by 21%.

5. **Best lossy compression**: deepteams/webp produces the smallest lossy files (193 KB vs 209-253 KB), 8% smaller than chai2010 CGo and 24% smaller than gen2brain.

6. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 30 KB and 80 allocs, vs chai2010/webp which uses 795 KB and 131K allocs due to CGo overhead.

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
