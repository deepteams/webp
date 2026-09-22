# WebP Go Libraries Benchmark

Comparative benchmark on `testdata/test_color.png` (1536x1024), plus a 256x256 crop. Apple M5 Max, macOS ARM64, Go 1.27.0, `GOMAXPROCS=18`, CGo enabled.

Last updated: 2026-09-22. Tested deepteams/webp `v1.2.8`, commit `56711c6964e36756335de6d1bca012abba63c346`.

## Libraries Compared

| Library | Version | Backend in this run | Lossy Encode | Lossless Encode | Decode |
|---------|---------|---------------------|:---:|:---:|:---:|
| [deepteams/webp](https://github.com/deepteams/webp) | v1.2.8 | Pure Go | Yes | Yes | Yes |
| [golang.org/x/image/webp](https://pkg.go.dev/golang.org/x/image/webp) | v0.24.0 | Pure Go | - | - | Yes |
| [gen2brain/webp](https://github.com/gen2brain/webp) | v0.4.5 | WASM (wazero v1.7.3) | Yes | Yes | Yes |
| [HugoSmits86/nativewebp](https://github.com/HugoSmits86/nativewebp) | v1.2.1 | Pure Go | - | Yes | Yes |
| [chai2010/webp](https://github.com/chai2010/webp) | v1.1.1 | CGo (libwebp 1.0.2) | Yes | Yes | Yes |

## Methodology

- All reported values are **medians of 10 runs** (`-count=10`), with the default `-benchtime=1s`. The suite runs sequentially, with no other benchmark suite started alongside it.
- Full images are loaded as `*image.RGBA`; the 256x256 crop is `*image.NRGBA`. Image loading and decode-fixture encoding happen outside the timed loops. Encoder output buffers are reused between iterations.
- Existing benchmark settings are unchanged: lossy quality is 75; deepteams uses method 4, while the supplied gen2brain options leave method at 0. Other libraries use the settings in [benchmark_test.go](benchmark_test.go). These are not equal-effort or equal-visual-quality comparisons.
- Each decoder uses its own library's encoded file, except `x/image/webp`, which decodes deepteams' output. Lossy output types also differ: deepteams returns range-correct NRGBA, while `x/image/webp` returns YCbCr for this opaque fixture. The tables therefore do not measure identical inputs and output conversion work across every library.
- `MB/s` is calculated from compressed file bytes, not raw pixel bytes. `B/op` and `Allocs` are Go heap statistics; they do not measure total native/WASM memory or peak process memory. KB and MB below use decimal units.
- gen2brain's dynamic libwebp loading was unavailable, so this run used its WASM fallback. A system with loadable shared libraries may use a different backend.
- Results are specific to this machine, Go version, image and settings; no AMD64 performance was measured.

[Raw results](results/2026-09-22-go1.27.0-arm64.txt) · [benchstat summary with 95% confidence intervals](results/2026-09-22-go1.27.0-arm64-summary.txt)

## Results

### Encode Lossy (Quality 75, 1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| **deepteams/webp** (Pure Go) | 48.2 | 4.01 | 1.24 MB | 171 |
| gen2brain/webp (WASM) | 58.0 | 4.36 | 12.9 KB | 12 |
| chai2010/webp (CGo) | 77.1 | 2.71 | 227.4 KB | 4 |

### Encode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| **deepteams/webp** (Pure Go) | 117.0 | 15.62 | 21.04 MB | 1,176 |
| gen2brain/webp (WASM) | 191.5 | 10.73 | 342.9 KB | 12 |
| nativewebp (Pure Go) | 281.2 | 7.16 | 89.29 MB | 2,155 |
| chai2010/webp (CGo) | 951.4 | 1.84 | 2.63 MB | 4 |

### Decode Lossy (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| chai2010/webp (CGo) | 9.53 | 21.94 | 6.76 MB | 23 |
| **deepteams/webp** (Pure Go) | 11.0 | 17.62 | 6.51 MB | 7 |
| golang.org/x/image/webp | 18.4 | 10.49 | 2.59 MB | 13 |
| gen2brain/webp (WASM) | 22.9 | 11.04 | 622.1 KB | 40 |

### Decode Lossless (1536x1024)

| Library | Time (ms) | MB/s | B/op | Allocs |
|---------|----------:|-----:|-----:|-------:|
| **deepteams/webp** (Pure Go) | 17.4 | 104.81 | 8.68 MB | 225 |
| chai2010/webp (CGo) | 19.1 | 91.74 | 10.65 MB | 30 |
| gen2brain/webp (WASM) | 34.6 | 59.31 | 4.66 MB | 46 |
| nativewebp (Pure Go) | 36.3 | 55.49 | 6.35 MB | 50 |
| golang.org/x/image/webp | 40.0 | 45.62 | 7.14 MB | 966 |

### Encode Lossy Small (Quality 75, 256x256)

| Library | Time (ms) | B/op | Allocs |
|---------|----------:|-----:|-------:|
| gen2brain/webp (WASM) | 2.23 | 257 B | 12 |
| **deepteams/webp** (Pure Go) | 2.40 | 32.3 KB | 127 |
| chai2010/webp (CGo) | 4.00 | 794.7 KB | 131,077 |

## Key Takeaways

1. **Full-image encoding:** deepteams has the lowest median in this run for lossy (48.2 ms) and lossless (117.0 ms) encoding. The median times are 17% and 39% lower than gen2brain respectively, under the settings above.

2. **Lossy decoding:** chai2010 has the lowest median at 9.53 ms; deepteams takes 11.0 ms and allocates 6.51 MB on the Go heap per operation. The previous 9.3 ms / 2.5 MB deepteams figures measured the old YCbCr output path. Since v1.2.8, decoding includes conversion to range-correct NRGBA, so the old claim of fastest lossy decoding no longer describes this benchmark.

3. **Lossless decoding:** deepteams has the lowest median at 17.4 ms, followed by chai2010 at 19.1 ms. These values describe the per-library fixtures used by this suite.

4. **Small-image encoding:** gen2brain has the lowest median at 2.23 ms, followed by deepteams at 2.40 ms. Rankings on the full image do not carry over to the crop.

5. **Buffer reuse is a separate measurement:** these decoder benchmarks call `Decode`. Use the root package's `BenchmarkDecodeLossyReuse` and `BenchmarkDecodeLosslessReuse` to measure `DecodeReuse`; their numbers are not mixed into these tables.

## Running

The comparative suite imports chai2010 unconditionally and requires CGo and a C compiler. Use the Go and dependency versions above when reproducing this run.

```bash
cd benchmark
go test -bench=. -benchmem -count=10 -run='^$' -timeout=30m

# Force gen2brain's WASM backend on systems with shared libwebp installed:
go test -tags=nodynamic -bench=. -benchmem -count=10 -run='^$' -timeout=30m

# File size comparison:
go test -v -run=TestFileSizes -count=1

# From the repository root, benchmark output-buffer reuse separately:
cd ..
go test -bench='^BenchmarkDecode(Lossy|Lossless)Reuse$' -benchmem -count=10 -run='^$'
```
