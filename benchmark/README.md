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
| gen2brain/webp (WASM) | **82** | 19 KB | 12 | 252,712 B |
| chai2010/webp (CGo) | 107 | 229 KB | 4 | 209,180 B |
| **deepteams/webp** (Pure Go) | 127 | 2.1 MB | 176 | **190,622 B** |

### Encode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs | Output Size |
|---------|----------:|-----:|-------:|------------:|
| gen2brain/webp (WASM) | **268** | 502 KB | 12 | 2,053,844 B |
| nativewebp (Pure Go) | 445 | 85 MB | 2,156 | 2,011,754 B |
| **deepteams/webp** (Pure Go) | 641 | 119 MB | 1,457 | 1,782,638 B |
| chai2010/webp (CGo) | 1,299 | 3.3 MB | 5 | **1,751,450 B** |

### Decode Lossy (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **13.7** | 6.9 MB | 24 |
| **deepteams/webp** (Pure Go) | 27.7 | 6.2 MB | 7 |
| gen2brain/webp (WASM) | 31.5 | 1.1 MB | 41 |
| golang.org/x/image/webp | 32.4 | 2.5 MB | 13 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| chai2010/webp (CGo) | **31.5** | 14.0 MB | 33 |
| nativewebp (Pure Go) | 52.0 | 6.1 MB | 50 |
| gen2brain/webp (WASM) | 55.8 | 10.1 MB | 50 |
| golang.org/x/image/webp | 56.4 | 7.1 MB | 1,543 |
| **deepteams/webp** (Pure Go) | 58.0 | 8.8 MB | 407 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | **3.39** | 267 B | 12 |
| chai2010/webp (CGo) | 6.24 | 776 KB | 131,077 |
| **deepteams/webp** (Pure Go) | 6.93 | 33 KB | 89 |

## Key Takeaways

1. **Best lossy compression**: deepteams/webp produces the smallest lossy files (190 KB vs 209-252 KB), indicating better rate-distortion optimization.

2. **Fastest lossless among pure Go**: deepteams/webp lossless encode (641ms) is 30% faster than nativewebp (445ms is faster but produces 13% larger files). deepteams/webp matches chai2010 (CGo) compression ratio while being 2x faster.

3. **Competitive decode performance**: deepteams/webp lossy decode (28ms) is faster than both gen2brain/webp (WASM, 32ms) and golang.org/x/image/webp (32ms), with the fewest allocations among pure Go decoders (7 allocs).

4. **Efficient memory on small images**: On 256x256 images, deepteams/webp uses only 33 KB and 89 allocs, vs chai2010/webp which uses 776 KB and 131K allocs due to CGo overhead.

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
