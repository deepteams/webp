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

Benchmarked on Apple M2 Pro (arm64, 10 cores), 1536x1024 RGB image, Go 1.24.2. Median of 3 runs.

### Encode (1536x1024, Quality 75)

| Library | Mode | Time | B/op | Allocs | Output |
|---------|------|-----:|-----:|-------:|-------:|
| **deepteams/webp** (Pure Go) | Lossy | **78 ms** | 1.5 MB | 126 | **193 KB** |
| gen2brain/webp (WASM) | Lossy | 81 ms | 18 KB | 12 | 253 KB |
| chai2010/webp (CGo) | Lossy | 106 ms | 234 KB | 4 | 209 KB |
| **deepteams/webp** (Pure Go) | Lossless | **224 ms** | 84 MB | 1,290 | 1,833 KB |
| gen2brain/webp (WASM) | Lossless | 271 ms | 514 KB | 12 | 2,054 KB |
| nativewebp (Pure Go) | Lossless | 419 ms | 89 MB | 2,156 | 2,012 KB |
| chai2010/webp (CGo) | Lossless | 1,317 ms | 3.5 MB | 5 | **1,751 KB** |

### Decode (1536x1024)

| Library | Mode | Time | B/op | Allocs |
|---------|------|-----:|-----:|-------:|
| chai2010/webp (CGo) | Lossy | **12.6 ms** | 7.2 MB | 24 |
| **deepteams/webp** (Pure Go) | Lossy | **13.4 ms** | 2.6 MB | 7 |
| golang.org/x/image/webp | Lossy | 24.7 ms | 2.6 MB | 13 |
| gen2brain/webp (WASM) | Lossy | 31.7 ms | 1.2 MB | 41 |
| chai2010/webp (CGo) | Lossless | **33.8 ms** | 14.7 MB | 33 |
| **deepteams/webp** (Pure Go) | Lossless | 40.9 ms | 8.8 MB | 257 |
| nativewebp (Pure Go) | Lossless | 52.3 ms | 6.4 MB | 50 |
| golang.org/x/image/webp | Lossless | 55.5 ms | 7.3 MB | 1,126 |
| gen2brain/webp (WASM) | Lossless | 56.9 ms | 10.6 MB | 50 |

Lossy encoding uses row-pipelined parallelism that scales with available cores. See [`benchmark/`](benchmark/) for full methodology and small-image results.

```bash
cd benchmark && go test -bench=. -benchmem -count=3 -run='^$' -timeout=30m
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
  pool/                    Object pool utilities
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
