package imgutil

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
)

// makeSolidImage creates a solid-color image of the given size.
func makeSolidImage(t *testing.T, dir string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	path := filepath.Join(dir, "test.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test image: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test image: %v", err)
	}
	return path
}

func TestProcessImage_None(t *testing.T) {
	dir := t.TempDir()
	src := makeSolidImage(t, dir, 200, 100)
	dst := filepath.Join(dir, "out.png")

	if err := ProcessImage(src, dst, config.Dimensions{Width: 50, Height: 50}, config.ResizeNone); err != nil {
		t.Fatalf("ProcessImage(none) error: %v", err)
	}

	// Should be a copy — same size.
	img := loadTestImage(t, dst)
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
		t.Errorf("none mode: got %dx%d, want 200x100", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestProcessImage_Fit(t *testing.T) {
	dir := t.TempDir()
	src := makeSolidImage(t, dir, 200, 100)
	dst := filepath.Join(dir, "out.png")

	dims := config.Dimensions{Width: 100, Height: 100}
	if err := ProcessImage(src, dst, dims, config.ResizeFit); err != nil {
		t.Fatalf("ProcessImage(fit) error: %v", err)
	}

	img := loadTestImage(t, dst)
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 100 {
		t.Errorf("fit mode: got %dx%d, want 100x100", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestProcessImage_Fill(t *testing.T) {
	dir := t.TempDir()
	src := makeSolidImage(t, dir, 200, 100)
	dst := filepath.Join(dir, "out.png")

	dims := config.Dimensions{Width: 100, Height: 100}
	if err := ProcessImage(src, dst, dims, config.ResizeFill); err != nil {
		t.Fatalf("ProcessImage(fill) error: %v", err)
	}

	img := loadTestImage(t, dst)
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 100 {
		t.Errorf("fill mode: got %dx%d, want 100x100", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestProcessImage_Crop(t *testing.T) {
	dir := t.TempDir()
	src := makeSolidImage(t, dir, 200, 100)
	dst := filepath.Join(dir, "out.png")

	dims := config.Dimensions{Width: 80, Height: 60}
	if err := ProcessImage(src, dst, dims, config.ResizeCrop); err != nil {
		t.Fatalf("ProcessImage(crop) error: %v", err)
	}

	img := loadTestImage(t, dst)
	if img.Bounds().Dx() != 80 || img.Bounds().Dy() != 60 {
		t.Errorf("crop mode: got %dx%d, want 80x60", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestResizeFit_Letterbox(t *testing.T) {
	// Wide image into square — should have black bars top/bottom.
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := range 100 {
		for x := range 200 {
			img.Set(x, y, color.White)
		}
	}

	result := resizeFit(img, config.Dimensions{Width: 100, Height: 100})
	bounds := result.Bounds()
	if bounds.Dx() != 100 || bounds.Dy() != 100 {
		t.Fatalf("got %dx%d, want 100x100", bounds.Dx(), bounds.Dy())
	}

	// Top-left corner should be black (letterbox area).
	r, g, b, _ := result.At(0, 0).RGBA()
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("corner pixel should be black (letterbox), got r=%d g=%d b=%d", r, g, b)
	}
}

func TestProcessImage_InvalidDimensions(t *testing.T) {
	dir := t.TempDir()
	src := makeSolidImage(t, dir, 200, 100)
	dst := filepath.Join(dir, "out.png")

	err := ProcessImage(src, dst, config.Dimensions{Width: 0, Height: 100}, config.ResizeFit)
	if err == nil {
		t.Error("expected error for zero-width dimensions")
	}

	err = ProcessImage(src, dst, config.Dimensions{Width: 100, Height: -1}, config.ResizeFit)
	if err == nil {
		t.Error("expected error for negative-height dimensions")
	}
}

func TestProcessImage_UnknownMode(t *testing.T) {
	dir := t.TempDir()
	src := makeSolidImage(t, dir, 200, 100)
	dst := filepath.Join(dir, "out.png")

	err := ProcessImage(src, dst, config.Dimensions{Width: 100, Height: 100}, "badmode")
	if err == nil {
		t.Error("expected error for unknown resize mode")
	}
}

func TestSaveImage_Atomic(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	path := filepath.Join(dir, "atomic.png")

	if err := saveImage(img, path); err != nil {
		t.Fatalf("saveImage error: %v", err)
	}

	// Verify file exists and is valid PNG.
	loadTestImage(t, path)
}

func loadTestImage(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open result image: %v", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode result image: %v", err)
	}
	return img
}
