# webp

[![CI](https://github.com/deepteams/webp/actions/workflows/ci.yml/badge.svg)](https://github.com/deepteams/webp/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/deepteams/webp.svg)](https://pkg.go.dev/github.com/deepteams/webp)
[![Go Report Card](https://goreportcard.com/badge/github.com/deepteams/webp)](https://goreportcard.com/report/github.com/deepteams/webp)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Pure Go encoder and decoder for the [WebP](https://developers.google.com/speed/webp) image format. Zero dependencies, zero CGo.

```
go get github.com/deepteams/webp
```

**Requires Go 1.24+**

## Features

- **Lossy** encoding & decoding (VP8)
- **Lossless** encoding & decoding (VP8L)
- **Alpha channel** support (ALPH chunk with VP8L compression)
- **Animation** (ANIM/ANMF) with sub-frame optimization, keyframe control, mixed codec mode
- **Extended format** (VP8X) with ICC, EXIF, XMP metadata
- **Sharp YUV** conversion for high-quality chroma subsampling
- **Presets** for photos, pictures, drawings, icons, text
- Transparent integration with Go's `image` package (`image.Decode` just works)
- CLI tool (`gwebp`) for encoding, decoding and inspecting WebP files

## Quick Start

### Decode

```go
package main

import (
    "image"
    "image/png"
    "os"

    _ "github.com/deepteams/webp" // register WebP format
)

func main() {
    // image.Decode auto-detects WebP thanks to init() registration
    f, _ := os.Open("photo.webp")
    defer f.Close()

    img, _, _ := image.Decode(f)

    out, _ := os.Create("photo.png")
    defer out.Close()
    png.Encode(out, img)
}
```

Lossy VP8 pixels are returned as `*image.NRGBA`. VP8 stores limited-range
BT.601 YUV, while Go's `image.YCbCr` uses full-range JPEG semantics; converting
during decode prevents black/white range contraction in standard `image`
consumers and repeated WebP transcodes.

### Decode in a loop (zero-allocation)

When decoding many images of similar size (thumbnails, video frames, batch
processing), `DecodeReuse` recycles the pixel buffers of the previous result,
bringing steady-state allocations near zero:

```go
var img image.Image
for _, path := range paths {
    f, _ := os.Open(path)
    img, _ = webp.DecodeReuse(f, img) // reuses img's buffers when compatible
    f.Close()
    // ... process img (do not keep references across iterations)
}
```

### Encode (lossy)

```go
package main

import (
    "image"
    _ "image/jpeg"
    "os"

    "github.com/deepteams/webp"
)

func main() {
    f, _ := os.Open("photo.jpg")
    defer f.Close()
    img, _, _ := image.Decode(f)

    out, _ := os.Create("photo.webp")
    defer out.Close()

    webp.Encode(out, img, &webp.EncoderOptions{
        Quality: 80,
        Method:  4, // 0=fast, 6=best compression
    })
}
```

### Encode (lossless)

```go
webp.Encode(out, img, &webp.EncoderOptions{
    Lossless: true,
    Quality:  75, // controls compression effort
})
```

### Animation

```go
package main

import (
    "image"
    "image/color"
    "os"
    "time"

    "github.com/deepteams/webp/animation"
)

func main() {
    out, _ := os.Create("anim.webp")
    defer out.Close()

    enc := animation.NewEncoder(out, 256, 256, &animation.EncodeOptions{
        Quality:   80,
        LoopCount: 0, // infinite loop
    })

    for i := 0; i < 10; i++ {
        img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
        // ... draw frame ...
        enc.AddFrame(img, 100*time.Millisecond)
    }

    enc.Close()
}
```

### Inspect

```go
f, _ := os.Open("image.webp")
defer f.Close()
feat, _ := webp.GetFeatures(f)

fmt.Printf("Size:      %dx%d\n", feat.Width, feat.Height)
fmt.Printf("Format:    %s\n", feat.Format)    // "lossy", "lossless", "extended"
fmt.Printf("Alpha:     %v\n", feat.HasAlpha)
fmt.Printf("Animated:  %v\n", feat.HasAnimation)
fmt.Printf("Frames:    %d\n", feat.FrameCount)
```

## CLI Tool

```bash
go install github.com/deepteams/webp/cmd/gwebp@latest
```

### Encode

```bash
# JPEG/PNG to WebP (lossy, quality 80)
gwebp enc -q 80 photo.jpg -o photo.webp

# Lossless encoding
gwebp enc -lossless input.png -o output.webp

# Sharp YUV for better chroma edges
gwebp enc -q 90 -sharp_yuv photo.jpg

# Content-specific preset
gwebp enc -preset photo -q 85 landscape.jpg

# GIF to animated WebP
gwebp enc -q 75 animation.gif -o animation.webp
```

### Decode

```bash
# WebP to PNG
gwebp dec input.webp -o output.png

# Animated WebP to GIF
gwebp dec animation.webp -o animation.gif
```

### Info

```bash
gwebp info photo.webp
```

## Encoder Options

| Option | Type | Default | Description |
|---|---|---|---|
| `Lossless` | `bool` | `false` | VP8L lossless encoding |
| `Quality` | `float32` | `75` | Compression quality (0-100) |
| `Method` | `int` | `4` | Effort level (0=fast, 6=slowest/best) |
| `Preset` | `Preset` | `Default` | Content preset (Picture, Photo, Drawing, Icon, Text) |
| `UseSharpYUV` | `bool` | `false` | Sharp RGB-to-YUV conversion |
| `Exact` | `bool` | `false` | Preserve RGB under transparent areas |
| `TargetSize` | `int` | `0` | Target output size in bytes |
| `TargetPSNR` | `float32` | `0` | Target PSNR in dB |
| `SNSStrength` | `int` | `50` | Spatial noise shaping (0-100) |
| `FilterStrength` | `int` | `60` | Loop filter strength (0-100) |
| `FilterSharpness` | `int` | `0` | Loop filter sharpness (0-7) |
| `FilterType` | `int` | `1` | Filter type (0=simple, 1=strong) |
| `Segments` | `int` | `4` | Number of segments (1-4) |
| `Pass` | `int` | `1` | Entropy analysis passes (1-10) |
| `AlphaCompression` | `int` | `1` | Alpha compression (0=none, 1=lossless) |
| `AlphaFiltering` | `int` | `1` | Alpha filter (0=none, 1=fast, 2=best) |
| `AlphaQuality` | `int` | `100` | Alpha quality (0-100) |

## Performance

Benchmarked on Apple M5 Max (macOS, arm64), Go 1.27.0, `GOMAXPROCS=18`, on 2026-09-22 at `v1.2.8` (`56711c6`). Source: `testdata/test_color.png`, 1536x1024. Values are medians of 10 runs using the default 1-second benchmark duration.

### Encode (1536x1024, Quality 75 for lossy)

| Library | Mode | Time | MB/s | B/op | Allocs |
|---------|------|-----:|-----:|-----:|-------:|
| **deepteams/webp** (Pure Go) | Lossy | 48.2 ms | 4.01 | 1.24 MB | 171 |
| gen2brain/webp (WASM) | Lossy | 58.0 ms | 4.36 | 12.9 KB | 12 |
| chai2010/webp (CGo) | Lossy | 77.1 ms | 2.71 | 227.4 KB | 4 |
| **deepteams/webp** (Pure Go) | Lossless | 117.0 ms | 15.62 | 21.04 MB | 1,176 |
| gen2brain/webp (WASM) | Lossless | 191.5 ms | 10.73 | 342.9 KB | 12 |
| nativewebp (Pure Go) | Lossless | 281.2 ms | 7.16 | 89.29 MB | 2,155 |
| chai2010/webp (CGo) | Lossless | 951.4 ms | 1.84 | 2.63 MB | 4 |

### Decode (1536x1024)

| Library | Mode | Time | MB/s | B/op | Allocs |
|---------|------|-----:|-----:|-----:|-------:|
| chai2010/webp (CGo) | Lossy | 9.53 ms | 21.94 | 6.76 MB | 23 |
| **deepteams/webp** (Pure Go) | Lossy | 11.0 ms | 17.62 | 6.51 MB | 7 |
| golang.org/x/image/webp | Lossy | 18.4 ms | 10.49 | 2.59 MB | 13 |
| gen2brain/webp (WASM) | Lossy | 22.9 ms | 11.04 | 622.1 KB | 40 |
| **deepteams/webp** (Pure Go) | Lossless | 17.4 ms | 104.81 | 8.68 MB | 225 |
| chai2010/webp (CGo) | Lossless | 19.1 ms | 91.74 | 10.65 MB | 30 |
| gen2brain/webp (WASM) | Lossless | 34.6 ms | 59.31 | 4.66 MB | 46 |
| nativewebp (Pure Go) | Lossless | 36.3 ms | 55.49 | 6.35 MB | 50 |
| golang.org/x/image/webp | Lossless | 40.0 ms | 45.62 | 7.14 MB | 966 |

These results describe this image and the benchmark's existing settings. `MB/s` uses compressed bytes, not decoded pixels; `B/op` and allocation counts cover Go heap allocations, not total native/WASM memory. Decoder inputs and output color models differ between libraries, so timings are not a comparison of identical work. In particular, deepteams/webp now returns range-correct `*image.NRGBA` for lossy images; older YCbCr decode timings are not equivalent.

[`DecodeReuse`](#decode-in-a-loop-zero-allocation) can reuse compatible output pixel buffers. The tables above measure `Decode` with a fresh output image; see `BenchmarkDecodeLossyReuse` / `BenchmarkDecodeLosslessReuse` in the root package for reuse benchmarks.

Lossy encoding uses row-pipelined parallelism that scales with available cores. Hot DSP kernels are SIMD-accelerated on arm64 (NEON) and amd64 (SSE2/AVX2), with pure Go fallbacks on other architectures. This run measures ARM64 only. See [`benchmark/`](benchmark/) for library versions, methodology, small-image results, and the saved raw measurements.

```bash
cd benchmark && go test -bench=. -benchmem -count=10 -run='^$' -timeout=30m
```

## Compatibility

Output files are compatible with all WebP decoders (Chrome, Firefox, Safari, libwebp `dwebp`, ImageMagick, etc.). The encoder produces bitstream-conformant VP8/VP8L output matching the behavior of Google's C reference implementation ([libwebp](https://chromium.googlesource.com/webm/libwebp)).

## Project Structure

```
webp.go / encode.go       Public API (Decode, Encode, Options)
animation/                 Animation encoder/decoder (ANIM/ANMF)
cmd/gwebp/                 CLI tool
mux/                       WebP mux/demux (RIFF container)
sharpyuv/                  Sharp YUV color space conversion
internal/
  bitio/                   Bit-level I/O (boolean arithmetic, lossless streams)
  container/               RIFF/WEBP container parsing
  dsp/                     DSP (YUV conversion, filters, prediction, cost)
  lossless/                VP8L encoder/decoder
  lossy/                   VP8 encoder/decoder
```

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Run tests (`go test ./...`)
4. Run race detector (`go test -race ./...`)
5. Submit a pull request

### Guidelines

- Keep zero external dependencies
- All codec changes must pass round-trip tests (encode -> decode -> verify)
- Run `go vet ./...` and fix any issues before submitting
- Bitstream code is precision-critical: test thoroughly against reference files

## License

MIT License - see [LICENSE](LICENSE) for details.
