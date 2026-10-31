package cmd

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeTestPNG(t *testing.T, path string, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// F-01: once validated, the path handed to targets must keep the validated
// bytes even if the caller-controlled source is replaced afterwards.
func TestValidateAndResolveImage_SwapAfterValidation(t *testing.T) {
	for _, swap := range []string{"rename", "symlink"} {
		t.Run(swap, func(t *testing.T) {
			dir := t.TempDir()
			img := filepath.Join(dir, "img.png")
			want := writeTestPNG(t, img, 4, 4)

			got, cleanup, err := validateAndResolveImage(img)
			if err != nil {
				t.Fatalf("validateAndResolveImage: %v", err)
			}
			defer cleanup()

			switch swap {
			case "rename":
				evil := filepath.Join(dir, "evil")
				if err := os.WriteFile(evil, []byte("#!/bin/sh\nid\n"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(evil, img); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				secret := filepath.Join(dir, "secret")
				if err := os.WriteFile(secret, []byte("DUMMY-SECRET\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(img); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(secret, img); err != nil {
					t.Fatal(err)
				}
			}

			data, err := os.ReadFile(got)
			if err != nil {
				t.Fatalf("read returned path: %v", err)
			}
			if !bytes.Equal(data, want) {
				t.Errorf("returned path %s no longer holds the validated image (got %d bytes, %q...)", got, len(data), data[:min(len(data), 12)])
			}
		})
	}
}
