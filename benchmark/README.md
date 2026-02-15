# WebP Go Libraries Benchmark

Comparative benchmark of Go WebP libraries on a 1536x1024 RGB image (Apple M2 Pro, arm64, Go 1.24.2).

Last updated: 2026-02-15

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
| **deepteams/webp** (Pure Go) | **79** | 1.5 MB | 124 | **193,298 B** |
| gen2brain/webp (WASM) | 82 | 18 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 108 | 234 KB | 4 | 209,180 B |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| gen2brain/webp (WASM) | **275** | 514 KB | 12 | 2,053,844 B |
| **deepteams/webp** (Pure Go) | 400 | 115 MB | 1,393 | **1,782,972 B** |
| nativewebp (Pure Go) | 428 | 89 MB | 2,157 | 2,011,754 B |
| chai2010/webp (CGo) | 1,320 | 3.5 MB | 5 | 1,751,450 B |

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **12.8** | 7.2 MB | 24 |
| golang.org/x/image/webp | 25.2 | 2.6 MB | 13 |
| **deepteams/webp** (Pure Go) | 26.5 | 6.5 MB | 7 |
| gen2brain/webp (WASM) | 31.9 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **32.0** | 14.7 MB | 33 |
| nativewebp (Pure Go) | 52.9 | 6.4 MB | 50 |
| gen2brain/webp (WASM) | 54.9 | 10.6 MB | 50 |
| golang.org/x/image/webp | 56.7 | 7.3 MB | 1,416 |
| **deepteams/webp** (Pure Go) | 57.8 | 8.5 MB | 340 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.4** | 267 B | 12 |
| **deepteams/webp** (Pure Go) | 3.9 | 29 KB | 80 |
| chai2010/webp (CGo) | 6.1 | 795 KB | 131,077 |

## Changes vs Previous Run

| Benchmark | Metric | Previous | Current | Delta |
|-----------|--------|----------|---------|-------|
| Encode Lossless deepteams | Time | 574 ms | **400 ms** | **-30%** |
| Encode Lossless deepteams | B/op | 127 MB | **115 MB** | **-9%** |
| Encode Lossless deepteams | Allocs | 1,455 | **1,393** | **-4%** |
| Encode Lossy deepteams | Time | 82 ms | **79 ms** | **-4%** |
| Decode Lossy deepteams | Time | 27.7 ms | **26.5 ms** | **-4%** |
| Decode Lossless deepteams | Time | 58.7 ms | **57.8 ms** | -2% |
| Encode Small Lossy deepteams | Time | 4.0 ms | **3.9 ms** | -3% |

All other metrics within noise margin (<3%).

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (79ms) beats gen2brain WASM (82ms) and is **27% faster than CGo libwebp** (108ms), while producing the smallest lossy files (193 KB vs 209-253 KB).

2. **Best lossless compression among pure Go**: deepteams/webp lossless output (1,783 KB) is 11% smaller than nativewebp (2,012 KB) and approaches chai2010 CGo (1,751 KB). 3.3x faster than chai2010.

3. **Competitive decode performance**: deepteams/webp lossy decode (26.5ms) is faster than gen2brain/webp (WASM, 31.9ms) with the fewest allocations among all decoders (7 allocs).

4. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 29 KB and 80 allocs, vs chai2010/webp which uses 795 KB and 131K allocs due to CGo overhead.

5. **No CGo, no WASM**: deepteams/webp and nativewebp are the only libraries that work without CGo or WASM runtimes. Cross-compilation just works.

6. **Feature completeness**: deepteams/webp is the only pure Go library supporting both lossy and lossless encoding + decoding, plus animation, alpha, and metadata.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
