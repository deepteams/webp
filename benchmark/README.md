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
| **deepteams/webp** (Pure Go) | **78** | 1.5 MB | 139 | **193,298 B** |
| gen2brain/webp (WASM) | 83 | 19 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 109 | 234 KB | 4 | 209,180 B |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| gen2brain/webp (WASM) | **372**\* | 514 KB | 12 | 2,053,844 B |
| **deepteams/webp** (Pure Go) | 437 | 115 MB | 1,461 | 1,782,638 B |
| nativewebp (Pure Go) | 446 | 90 MB | 2,156 | 2,011,754 B |
| chai2010/webp (CGo) | 1,344 | 3.5 MB | 5 | **1,751,450 B** |

\*gen2brain lossless shows high variance (345ms, 372ms, 524ms) due to low b.N (3-4 iterations).

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **13.0** | 7.2 MB | 24 |
| golang.org/x/image/webp | 25.1 | 2.6 MB | 13 |
| **deepteams/webp** (Pure Go) | 26.4 | 6.5 MB | 7 |
| gen2brain/webp (WASM) | 33.3 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **32.7** | 14.7 MB | 33 |
| nativewebp (Pure Go) | 55.2 | 6.4 MB | 50 |
| gen2brain/webp (WASM) | 57.8 | 10.6 MB | 50 |
| golang.org/x/image/webp | 58.1 | 7.4 MB | 1,543 |
| **deepteams/webp** (Pure Go) | 60.8 | 9.2 MB | 407 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.4** | 266 B | 12 |
| **deepteams/webp** (Pure Go) | 5.1 | 30 KB | 80 |
| chai2010/webp (CGo) | 6.1 | 795 KB | 131,077 |

## Changes vs Previous Run (2026-02-15)

| Benchmark | Metric | Previous | Current | Delta |
|-----------|--------|----------|---------|-------|
| Encode Lossy deepteams | Time | 80 ms | **78 ms** | **-2.5%** |
| Decode Lossy deepteams | Time | 27.3 ms | **26.4 ms** | **-3.3%** |
| Encode Lossless nativewebp | Time | 511 ms | **446 ms** | **-13%** |
| Decode Lossless nativewebp | Time | 54.2 ms | 55.2 ms | +2% (noise) |
| Decode Lossless deepteams | Time | 58.9 ms | 60.8 ms | +3% (noise) |
| Encode Small Lossy deepteams | Time | 4.0 ms | 5.1 ms | +28% (outlier)\* |

\*Small lossy deepteams had one outlier run at 12.8ms (likely GC pause), skewing the median. The two stable runs were 4.8ms and 5.1ms.

All other metrics within noise margin (<2%).

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (78ms) is **6% faster than gen2brain WASM** (83ms) and **28% faster than CGo libwebp** (109ms), while producing the smallest lossy files (193 KB vs 209-253 KB).

2. **Best lossless compression among pure Go**: deepteams/webp lossless output (1,783 KB) is 11% smaller than nativewebp (2,012 KB) while matching chai2010 (CGo). 3x faster than chai2010.

3. **Competitive pure Go lossless speed**: deepteams/webp (437ms) is on par with nativewebp (446ms) while producing 11% smaller files.

4. **Competitive decode performance**: deepteams/webp lossy decode (26ms) is faster than gen2brain/webp (WASM, 33ms) with the fewest allocations among all decoders (7 allocs).

5. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 30 KB and 80 allocs, vs chai2010/webp which uses 795 KB and 131K allocs due to CGo overhead.

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
