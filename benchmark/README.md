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
| **deepteams/webp** (Pure Go) | **80** | 1.6 MB | 124 | **193,298 B** |
| gen2brain/webp (WASM) | 83 | 19 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 109 | 234 KB | 4 | 209,180 B |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| gen2brain/webp (WASM) | **276** | 514 KB | 12 | 2,053,844 B |
| nativewebp (Pure Go) | 427 | 89 MB | 2,156 | 2,011,754 B |
| **deepteams/webp** (Pure Go) | 433 | 115 MB | 1,400 | 1,782,972 B |
| chai2010/webp (CGo) | 1,305 | 3.5 MB | 5 | **1,751,450 B** |

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **13.1** | 7.2 MB | 24 |
| golang.org/x/image/webp | 25.1 | 2.6 MB | 13 |
| **deepteams/webp** (Pure Go) | 26.8 | 6.5 MB | 7 |
| gen2brain/webp (WASM) | 31.9 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **36.0** | 14.7 MB | 33 |
| nativewebp (Pure Go) | 55.0 | 6.4 MB | 50 |
| gen2brain/webp (WASM) | 56.0 | 10.6 MB | 50 |
| **deepteams/webp** (Pure Go) | 62.6 | 8.5 MB | 340 |
| golang.org/x/image/webp | 75.9 | 7.3 MB | 1,416 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.6** | 267 B | 12 |
| **deepteams/webp** (Pure Go) | 4.6 | 29 KB | 80 |
| chai2010/webp (CGo) | 6.3 | 795 KB | 131,077 |

## Changes vs Previous Run

| Benchmark | Metric | Previous | Current | Delta |
|-----------|--------|----------|---------|-------|
| Encode Lossy deepteams | Allocs | 139 | **124** | **-11%** |
| Encode Lossless deepteams | Allocs | 1,461 | **1,400** | **-4%** |
| Decode Lossless deepteams | B/op | 9.2 MB | **8.5 MB** | **-8%** |
| Decode Lossless deepteams | Allocs | 407 | **340** | **-16%** |
| Encode Small Lossy deepteams | Time | 5.1 ms | **4.6 ms** | **-10%** |
| Encode Lossy deepteams | Time | 78 ms | 80 ms | +2% (noise) |
| Decode Lossless deepteams | Time | 60.8 ms | 62.6 ms | +3% (noise) |
| Encode Lossless gen2brain | Time | 372 ms | 276 ms | -26% (WASM variance) |
| Decode Lossless x/image | Time | 58.1 ms | 75.9 ms | +31% (high variance) |

All other metrics within noise margin (<3%).

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (80ms) matches gen2brain WASM (83ms) and is **27% faster than CGo libwebp** (109ms), while producing the smallest lossy files (193 KB vs 209-253 KB).

2. **Best lossless compression among pure Go**: deepteams/webp lossless output (1,783 KB) is 11% smaller than nativewebp (2,012 KB) while approaching chai2010 (CGo). 3x faster than chai2010.

3. **Competitive pure Go lossless speed**: deepteams/webp (433ms) matches nativewebp (427ms) while producing 11% smaller files.

4. **Competitive decode performance**: deepteams/webp lossy decode (27ms) is faster than gen2brain/webp (WASM, 32ms) with the fewest allocations among all decoders (7 allocs).

5. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 29 KB and 80 allocs, vs chai2010/webp which uses 795 KB and 131K allocs due to CGo overhead.

6. **No CGo, no WASM**: deepteams/webp and nativewebp are the only libraries that work without CGo or WASM runtimes. Cross-compilation just works.

7. **Feature completeness**: deepteams/webp is the only pure Go library supporting both lossy and lossless encoding + decoding, plus animation, alpha, and metadata.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=1 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
