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
| **deepteams/webp** (Pure Go) | **80** | 1.4 MB | 119 | **193,298 B** |
| gen2brain/webp (WASM) | 85 | 19 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 108 | 234 KB | 4 | 209,180 B |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| gen2brain/webp (WASM) | **284**\* | 514 KB | 12 | 2,053,844 B |
| **deepteams/webp** (Pure Go) | 435 | 115 MB | 1,457 | 1,782,638 B |
| nativewebp (Pure Go) | 511 | 90 MB | 2,157 | 2,011,754 B |
| chai2010/webp (CGo) | 1,315 | 3.5 MB | 5 | **1,751,450 B** |

\*gen2brain lossless shows high variance (284ms, 592ms, 654ms) due to low b.N (2-4 iterations). Best run used.

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **13.1** | 7.2 MB | 24 |
| golang.org/x/image/webp | 25.6 | 2.6 MB | 13 |
| **deepteams/webp** (Pure Go) | 27.3 | 6.5 MB | 7 |
| gen2brain/webp (WASM) | 33.3 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **32.5** | 14.7 MB | 33 |
| nativewebp (Pure Go) | 54.2 | 6.4 MB | 50 |
| gen2brain/webp (WASM) | 56.7 | 10.6 MB | 50 |
| golang.org/x/image/webp | 58.0 | 7.4 MB | 1,543 |
| **deepteams/webp** (Pure Go) | 58.9 | 8.5 MB | 407 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.4** | 266 B | 12 |
| **deepteams/webp** (Pure Go) | 4.0 | 29 KB | 80 |
| chai2010/webp (CGo) | 6.2 | 795 KB | 131,077 |

## Changes vs Previous Run (2026-02-15)

| Benchmark | Metric | Previous | Current | Delta |
|-----------|--------|----------|---------|-------|
| Encode Lossless deepteams | Time | 503 ms | **435 ms** | **-14%** |
| Encode Lossless deepteams | B/op | 127 MB | **115 MB** | **-9%** |
| Encode Lossless deepteams | Allocs | 1,524 | **1,457** | -4% |
| Encode Lossy deepteams | B/op | 1.5 MB | **1.4 MB** | -7% |
| Encode Lossy deepteams | Allocs | 133 | **119** | -10% |
| Encode Lossy deepteams | Time | 78 ms | 80 ms | +3% (noise) |
| Encode Small Lossy chai2010 | Time | 7.1 ms | **6.2 ms** | -13% |

The lossless encode improvement (503ms -> 435ms) resolves the previous variance issue: the prior 503ms median included a slow GC-affected run (444-608ms range). Current runs are tightly clustered (433-441ms), confirming true performance around **435ms**.

All decode metrics within noise margin (<2%).

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp (80ms) is **6% faster than gen2brain WASM** (85ms) and **26% faster than CGo libwebp** (108ms), while producing the smallest lossy files (193 KB vs 209-253 KB).

2. **Best lossless compression among pure Go**: deepteams/webp lossless output (1,783 KB) is 11% smaller than nativewebp (2,012 KB) while matching chai2010 (CGo). 3x faster than chai2010.

3. **Competitive pure Go lossless speed**: deepteams/webp (435ms) is 15% faster than nativewebp (511ms) while producing 11% smaller files.

4. **Competitive decode performance**: deepteams/webp lossy decode (27ms) is faster than gen2brain/webp (WASM, 33ms) with the fewest allocations among all decoders (7 allocs).

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
