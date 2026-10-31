package imgutil

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

// F-02: images over the dimension bound are rejected from the header alone.
func TestValidate_RejectsOversizedDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tall.png")
	writePNG(t, path, 1, 20000)
	_, _, err := Validate(path)
	if err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("Validate(1x20000) err = %v, want dimension limit error", err)
	}
}

func TestProcessImage_RejectsOversizedDimensions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tall.png")
	writePNG(t, path, 1, 20000)
	err := ProcessImage(path, filepath.Join(dir, "out.png"), config.Dimensions{Width: 64, Height: 36}, config.ResizeFill)
	if err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("ProcessImage(1x20000) err = %v, want dimension limit error", err)
	}
}

func TestValidate_Accepts8K(t *testing.T) {
	path := filepath.Join(t.TempDir(), "8k.png")
	writePNG(t, path, 7680, 4320)
	if _, _, err := Validate(path); err != nil {
		t.Fatalf("Validate(7680x4320): %v", err)
	}
}

// F-02: resizeFill must not allocate an intermediate proportional to the
// source aspect ratio. A 1x1000 source filled to 100x10 used to allocate a
// 100x100000 RGBA intermediate (40 MB).
func TestResizeFill_BoundedAllocation(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 1, 1000))
	dims := config.Dimensions{Width: 100, Height: 10}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	out := resizeFill(src, dims)
	runtime.ReadMemStats(&after)

	if b := out.Bounds(); b.Dx() != 100 || b.Dy() != 10 {
		t.Fatalf("resizeFill bounds = %v, want 100x10", b)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<20 {
		t.Errorf("resizeFill allocated %d bytes, want under 1 MiB", alloc)
	}
}
