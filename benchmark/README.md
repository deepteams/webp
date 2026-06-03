# WebP Go Libraries Benchmark

Comparative benchmark of Go WebP libraries on a 1536x1024 RGB image (Apple M5 Max, arm64, Go 1.26.3).

Last updated: 2026-06-03 (10-run benchmark)

## Libraries Compared

| Library | Type | Lossy Encode | Lossless Encode | Decode |
|---------|------|:---:|:---:|:---:|
| [deepteams/webp](https://github.com/deepteams/webp) | Pure Go | Yes | Yes | Yes |
| [golang.org/x/image/webp](https://pkg.go.dev/golang.org/x/image/webp) | Pure Go | - | - | Yes |
| [gen2brain/webp](https://github.com/gen2brain/webp) | WASM (wazero) | Yes | Yes | Yes |
| [HugoSmits86/nativewebp](https://github.com/HugoSmits86/nativewebp) | Pure Go | - | Lossless | Yes |
| [chai2010/webp](https://github.com/chai2010/webp) | CGo (libwebp 1.0.2) | Yes | Yes | Yes |

## Results

All values are **medians of 10 runs** (`-count=10`).

### Encode Lossy (Quality 75, 1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| **deepteams/webp** (Pure Go) | **47.3** | 4.1 | 1.2 MB | 172 |
| gen2brain/webp (WASM) | 53.6 | 4.7 | 12 KB | 12 |
| chai2010/webp (CGo) | 71.4 | 2.9 | 227 KB | 4 |

### Encode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|------:|-------:|
| **deepteams/webp** (Pure Go) | **117.4** | 15.6 | 16.6 MB | 221 |
| gen2brain/webp (WASM) | 174.7 | 11.8 | 343 KB | 12 |
| nativewebp (Pure Go) | 265.6 | 7.6 | 89.3 MB | 2,155 |
| chai2010/webp (CGo) | 889.1 | 2.0 | 2.6 MB | 4 |

### Decode Lossy (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| chai2010/webp (CGo) | **8.9** | 23.4 | 6.8 MB | 23 |
| **deepteams/webp** (Pure Go) | **9.3** | 20.7 | 2.6 MB | 7 |
| golang.org/x/image/webp | 17.5 | 11.0 | 2.6 MB | 13 |
| gen2brain/webp (WASM) | 21.1 | 12.0 | 622 KB | 40 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| chai2010/webp (CGo) | **18.5** | 94.8 | 10.6 MB | 30 |
| **deepteams/webp** (Pure Go) | **27.2** | 67.4 | 8.3 MB | 289 |
| gen2brain/webp (WASM) | 32.5 | 63.2 | 4.7 MB | 46 |
| nativewebp (Pure Go) | 35.1 | 57.3 | 6.4 MB | 50 |
| golang.org/x/image/webp | 37.8 | 48.5 | 7.3 MB | 1,126 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| gen2brain/webp (WASM) | **2.11** | 4.5 | 256 B | 12 |
| **deepteams/webp** (Pure Go) | **2.40** | 2.8 | 32 KB | 127 |
| chai2010/webp (CGo) | 3.75 | 1.9 | 795 KB | 131,077 |

## Changes vs Previous Run

The previous published run was from 2026-02-22 on Apple M2 Pro with Go 1.24.2. This run used Apple M5 Max with Go 1.26.3, so the deltas include hardware and toolchain changes.

| Benchmark | Previous | Current | Delta |
|-----------|---------:|--------:|------:|
| Encode Lossy deepteams | 79 ms | **47.3 ms** | -40% |
| Encode Lossless deepteams | 232 ms | **117.4 ms** | -49% |
| Decode Lossy deepteams | 15.0 ms | **9.3 ms** | -38% |
| Decode Lossless deepteams | 41.1 ms | **27.2 ms** | -34% |

## Key Takeaways

1. **Fastest lossy encoder overall**: deepteams/webp encodes lossy WebP in 47.3ms, 12% faster than gen2brain WASM and 34% faster than chai2010 CGo.

2. **Fastest lossless encoder overall**: deepteams/webp encodes lossless WebP in 117.4ms, 33% faster than gen2brain WASM, 56% faster than nativewebp, and 87% faster than chai2010 CGo.

3. **Near-CGo lossy decode with lower memory**: deepteams/webp decodes lossy WebP in 9.3ms, within 5% of chai2010 CGo, while using 2.6 MB and 7 allocs versus 6.8 MB and 23 allocs.

4. **Fastest pure Go lossless decoder**: deepteams/webp decodes lossless WebP in 27.2ms, 22% faster than nativewebp and 28% faster than golang.org/x/image/webp.

5. **Small-image encoding is competitive**: on 256x256 lossy inputs, deepteams/webp completes in 2.40ms with 32 KB allocated; gen2brain WASM is faster at 2.11ms, while chai2010 CGo allocates heavily due to CGo overhead.

6. **Pure Go encoder, no external runtime**: among the compared encoders, deepteams/webp and nativewebp work without CGo or WASM runtimes. deepteams/webp is the only one of those with lossy encode support.

7. **Complete feature set**: deepteams/webp is the only pure Go library in this set supporting lossy and lossless encoding plus decoding, animation, alpha, metadata, and VP8X extended format.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=10 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=10 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
