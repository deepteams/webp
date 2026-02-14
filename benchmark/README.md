# WebP Go Libraries Benchmark

Comparative benchmark of Go WebP libraries on a 1536x1024 RGB image (Apple M2 Pro, arm64).

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

| Library | Time (ms) | Allocs | Output Size |
|---------|----------:|-------:|------------:|
| gen2brain/webp (WASM) | **80** | 12 | 252,712 B |
| chai2010/webp (CGo) | 109 | 4 | 209,180 B |
| **deepteams/webp** (Pure Go) | 243 | 11,010,305 | **190,622 B** |

### Encode Lossless (1536x1024)

| Library | Time (ms) | Allocs | Output Size |
|---------|----------:|-------:|------------:|
| gen2brain/webp (WASM) | **280** | 12 | 2,053,844 B |
| nativewebp (Pure Go) | 422 | 2,156 | 2,011,754 B |
| chai2010/webp (CGo) | 1,365 | 5 | **1,751,450 B** |
| **deepteams/webp** (Pure Go) | 1,456 | 3,215,583 | 1,769,522 B |

### Decode Lossy (1536x1024)

| Library | Time (ms) | Allocs |
|---------|----------:|-------:|
| chai2010/webp (CGo) | **13.1** | 24 |
| golang.org/x/image/webp | 24.4 | 13 |
| **deepteams/webp** (Pure Go) | 26.6 | 2,434 |
| gen2brain/webp (WASM) | 31.5 | 41 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | Allocs |
|---------|----------:|-------:|
| chai2010/webp (CGo) | **31.4** | 33 |
| nativewebp (Pure Go) | 54.2 | 50 |
| golang.org/x/image/webp | 56.2 | 1,424 |
| gen2brain/webp (WASM) | 66.0 | 50 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | Allocs |
|---------|----------:|-------:|
| gen2brain/webp (WASM) | **3.3** | 12 |
| chai2010/webp (CGo) | 6.2 | 131,077 |
| **deepteams/webp** (Pure Go) | 7.3 | 92 |

## Key Takeaways

1. **Best compression ratio**: deepteams/webp produces the smallest lossy files (190 KB vs 209-252 KB), indicating better rate-distortion optimization.

2. **Decode performance**: deepteams/webp is competitive with golang.org/x/image/webp for lossy decoding (~27ms vs ~24ms), and faster than gen2brain/webp (WASM).

3. **Encode speed**: The WASM-based gen2brain/webp is fastest for encoding (leveraging compiled C libwebp via WebAssembly). deepteams/webp is ~3x slower for lossy encoding on large images but competitive on small images (256x256).

4. **No CGo required**: deepteams/webp and nativewebp are the only libraries that work without CGo or WASM runtimes.

5. **Feature completeness**: deepteams/webp is the only pure Go library supporting both lossy and lossless encoding + decoding.

## Running

```bash
cd benchmark
go test -bench=. -benchmem -count=3 -run=^$ -timeout=30m

# Without CGo (skip chai2010/webp):
CGO_ENABLED=0 go test -bench=. -benchmem -count=1 -run=^$ -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1
```
