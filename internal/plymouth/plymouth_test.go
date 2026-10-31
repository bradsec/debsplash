package plymouth

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
)

func TestHexToFloats_Black(t *testing.T) {
	r, g, b, err := hexToFloats("#000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("expected 0,0,0 got %g,%g,%g", r, g, b)
	}
}

func TestHexToFloats_White(t *testing.T) {
	r, g, b, err := hexToFloats("#FFFFFF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != 1 || g != 1 || b != 1 {
		t.Errorf("expected 1,1,1 got %g,%g,%g", r, g, b)
	}
}

func TestHexToFloats_ShortHex(t *testing.T) {
	r, g, b, err := hexToFloats("#F00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != 1 || g != 0 || b != 0 {
		t.Errorf("expected 1,0,0 got %g,%g,%g", r, g, b)
	}
}

func TestHexToFloats_InvalidReturnsError(t *testing.T) {
	_, _, _, err := hexToFloats("invalid")
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestHexToFloats_NoHash(t *testing.T) {
	r, g, b, err := hexToFloats("FF8800")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still parse correctly since we strip #
	if r < 0.99 || g < 0.53 || g > 0.54 || b != 0 {
		t.Errorf("expected ~1,~0.533,0 got %g,%g,%g", r, g, b)
	}
}

func TestHexToFloats_Valid(t *testing.T) {
	r, g, b, err := hexToFloats("#FF8800")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != 1.0 || g < 0.53 || g > 0.534 || b != 0 {
		t.Errorf("unexpected values: r=%v g=%v b=%v", r, g, b)
	}
}

func TestHexToFloats_Short(t *testing.T) {
	r, g, b, err := hexToFloats("#FFF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r != 1.0 || g != 1.0 || b != 1.0 {
		t.Errorf("expected 1,1,1 got r=%v g=%v b=%v", r, g, b)
	}
}

func TestHexToFloats_Invalid(t *testing.T) {
	_, _, _, err := hexToFloats("X")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestHexToFloats_Empty(t *testing.T) {
	_, _, _, err := hexToFloats("")
	if err == nil {
		t.Error("expected error for empty string")
	}
}

func TestWriteThemeScript_Boxed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.script")
	esc := config.DefaultEncryptScreenConfig()
	esc.Style = "boxed"

	err := writeThemeScript(path, "bg.png", esc)
	if err != nil {
		t.Fatalf("writeThemeScript failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, `Image("bg.png")`) {
		t.Error("missing background image reference")
	}
	if !strings.Contains(content, "Boxed style") {
		t.Error("missing boxed style comment")
	}
	if !strings.Contains(content, "display_password_callback") {
		t.Error("missing password callback")
	}
}

func TestWriteThemeScript_Minimal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.script")
	esc := config.DefaultEncryptScreenConfig()
	esc.Style = "minimal"

	err := writeThemeScript(path, "bg.png", esc)
	if err != nil {
		t.Fatalf("writeThemeScript failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "Minimal style") {
		t.Error("missing minimal style comment")
	}
}

func TestWriteThemeScript_Floating(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.script")
	esc := config.DefaultEncryptScreenConfig()
	esc.Style = "floating"

	err := writeThemeScript(path, "bg.png", esc)
	if err != nil {
		t.Fatalf("writeThemeScript failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.Contains(content, "Floating style") {
		t.Error("missing floating style comment")
	}
	if !strings.Contains(content, `Image("background_box.png")`) {
		t.Error("missing background_box.png reference")
	}
	if !strings.Contains(content, "bg_box_scaled") {
		t.Error("missing bg_box_scaled reference")
	}
}

func TestGenerateBoxImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "box.png")
	esc := config.DefaultEncryptScreenConfig()
	esc.BoxColor = "#FF0000"
	esc.BoxOpacity = 0.5

	err := generateBoxImage(path, esc)
	if err != nil {
		t.Fatalf("generateBoxImage failed: %v", err)
	}

	// Verify file exists and is valid PNG
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("could not open generated image: %v", err)
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("could not decode PNG: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() != 400 || bounds.Dy() != 100 {
		t.Errorf("expected 400x100, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}
