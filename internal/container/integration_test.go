package container

import (
	"os"
	"testing"
)

func TestParseRealVP8File(t *testing.T) {
	path := "../../libwebp/examples/test.webp"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	p, err := NewParser(data)
	if err != nil {
		t.Fatalf("NewParser: %v", err)
	}

	f := p.Features()
	if f.Format != FormatVP8 {
		t.Fatalf("format = %v, want VP8", f.Format)
	}
	// test.webp is 128x128 lossy
	if f.Width != 128 || f.Height != 128 {
		t.Fatalf("dimensions = %dx%d, want 128x128", f.Width, f.Height)
	}
	if f.HasAlpha {
		t.Fatal("unexpected alpha in test.webp")
	}
	if f.HasAnim {
		t.Fatal("unexpected animation in test.webp")
	}
	if len(p.Frames()) != 1 {
		t.Fatalf("got %d frames, want 1", len(p.Frames()))
	}
}
