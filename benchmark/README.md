# WebP Go Libraries Benchmark

Comparative benchmark of Go WebP libraries on a 1536x1024 RGB image (Apple M2 Pro, arm64, Go 1.24.2).

Last updated: 2026-02-14

## Libraries Compared

| Library | Type | Lossy Encode | Lossless Encode | Decode |
|---------|------|:---:|:---:|:---:|
| [deepteams/webp](https://github.com/deepteams/webp) | Pure Go | Yes | Yes | Yes |
| [golang.org/x/image/webp](https://pkg.go.dev/golang.org/x/image/webp) | Pure Go | - | - | Yes |
| [gen2brain/webp](https://github.com/gen2brain/webp) | WASM (wazero) | Yes | Yes | Yes |
| [HugoSmits86/nativewebp](https://github.com/HugoSmits86/nativewebp) | Pure Go | - | Lossless | Yes |
| [chai2010/webp](https://github.com/chai2010/webp) | CGo (libwebp 1.0.2) | Yes | Yes | Yes |

## Results

### Encode Lossy (Quality 75, 1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| gen2brain/webp (WASM) | **83** | 20 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 108 | 234 KB | 4 | 209,180 B |
| **deepteams/webp** (Pure Go) | 129 | 2.2 MB | 167 | **190,622 B** |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| gen2brain/webp (WASM) | **276** | 514 KB | 12 | 2,053,844 B |
| nativewebp (Pure Go) | 430 | 89 MB | 2,156 | 2,011,754 B |
| **deepteams/webp** (Pure Go) | 633 | 127 MB | 1,466 | 1,782,638 B |
| chai2010/webp (CGo) | 1,294 | 3.5 MB | 5 | **1,751,450 B** |

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **13.1** | 7.2 MB | 24 |
| golang.org/x/image/webp | 25.3 | 2.6 MB | 13 |
| **deepteams/webp** (Pure Go) | 26.7 | 6.5 MB | 7 |
| gen2brain/webp (WASM) | 32.5 | 1.2 MB | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **32.4** | 14.7 MB | 33 |
| nativewebp (Pure Go) | 53.0 | 6.4 MB | 50 |
| gen2brain/webp (WASM) | 55.3 | 10.6 MB | 50 |
| golang.org/x/image/webp | 57.8 | 7.4 MB | 1,543 |
| **deepteams/webp** (Pure Go) | 59.0 | 9.0 MB | 407 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.5** | 267 B | 12 |
| chai2010/webp (CGo) | 6.2 | 795 KB | 131,077 |
| **deepteams/webp** (Pure Go) | 7.0 | 32 KB | 88 |

## Key Takeaways

1. **Best lossy compression**: deepteams/webp produces the smallest lossy files (191 KB vs 209-253 KB), indicating better rate-distortion optimization.

2. **Best lossless compression among pure Go**: deepteams/webp lossless output (1,783 KB) is 11% smaller than nativewebp (2,012 KB) while matching chai2010 (CGo). 2x faster than chai2010.

3. **Competitive decode performance**: deepteams/webp lossy decode (27ms) is faster than gen2brain/webp (WASM, 33ms) with the fewest allocations among all decoders (7 allocs).

4. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 32 KB and 88 allocs, vs chai2010/webp which uses 795 KB and 131K allocs due to CGo overhead.

5. **No CGo, no WASM**: deepteams/webp and nativewebp are the only libraries that work without CGo or WASM runtimes. Cross-compilation just works.

6. **Feature completeness**: deepteams/webp is the only pure Go library supporting both lossy and lossless encoding + decoding, plus animation, alpha, and metadata.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=1 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
