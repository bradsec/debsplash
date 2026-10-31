package imgutil

import (
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Resolution thresholds for warnings.
const (
	// MinRecommendedWidth/Height: below this the image will look blurry on a modern display.
	// 1024x768 is the GRUB default and is acceptable; warn only for genuinely small images.
	MinRecommendedWidth  = 1024
	MinRecommendedHeight = 768
	MaxRecommendedWidth  = 3840
	MaxRecommendedHeight = 2160
)

// Hard limits checked from the image header before any pixel data is decoded.
// They sit above 8K UHD (7680x4320) so legitimate wallpapers pass, and cap a
// decoded 32-bit buffer at 256 MiB.
const (
	MaxImageWidth  = 16384
	MaxImageHeight = 16384
	MaxImagePixels = 8192 * 8192
)

// checkDimensions rejects header dimensions that would make decoding or
// resizing allocate unbounded memory.
func checkDimensions(w, h int) error {
	if w > MaxImageWidth || h > MaxImageHeight || w*h > MaxImagePixels {
		return fmt.Errorf("image dimensions %dx%d exceed the maximum of %dx%d (%d pixels)",
			w, h, MaxImageWidth, MaxImageHeight, MaxImagePixels)
	}
	return nil
}

// ImageInfo holds validated image metadata.
type ImageInfo struct {
	Path   string
	Format string // "png" or "jpeg"
	Width  int
	Height int
}

// Validate checks that the file at path is a valid PNG or JPEG image.
// It returns image metadata and any warnings (e.g. resolution issues).
// A non-nil error means the image is unusable.
func Validate(path string) (*ImageInfo, []string, error) {
	if err := checkExt(path); err != nil {
		return nil, nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("could not open image: %w", err)
	}
	defer f.Close()

	return validateContent(f, path)
}

func checkExt(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return fmt.Errorf("unsupported image format %q — only PNG and JPEG are supported", ext)
	}
	return nil
}

// validateContent checks the image bytes in f against the extension of path.
func validateContent(f io.ReadSeeker, path string) (*ImageInfo, []string, error) {
	ext := strings.ToLower(filepath.Ext(path))

	// Check magic bytes before full decode.
	header := make([]byte, 8)
	n, err := f.Read(header)
	if err != nil || n < 4 {
		return nil, nil, fmt.Errorf("could not read image header: file too small or unreadable")
	}

	isPNG := n >= 8 && header[0] == 0x89 && header[1] == 'P' && header[2] == 'N' && header[3] == 'G'
	isJPEG := header[0] == 0xFF && header[1] == 0xD8 && header[2] == 0xFF

	if !isPNG && !isJPEG {
		return nil, nil, fmt.Errorf("file is not a valid PNG or JPEG image (bad magic bytes)")
	}

	if isPNG && ext != ".png" {
		return nil, nil, fmt.Errorf("file appears to be PNG but has extension %q — rename to .png", ext)
	}
	if isJPEG && ext == ".png" {
		return nil, nil, fmt.Errorf("file appears to be JPEG but has extension .png — rename to .jpg")
	}

	// Seek back to start for full decode.
	if _, err := f.Seek(0, 0); err != nil {
		return nil, nil, fmt.Errorf("could not seek image file: %w", err)
	}

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, nil, fmt.Errorf("could not decode image: %w — file may be corrupted", err)
	}
	if err := checkDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, nil, err
	}

	info := &ImageInfo{
		Path:   path,
		Format: format,
		Width:  cfg.Width,
		Height: cfg.Height,
	}

	var warnings []string

	if cfg.Width < MinRecommendedWidth || cfg.Height < MinRecommendedHeight {
		warnings = append(warnings, fmt.Sprintf(
			"image resolution %dx%d is below recommended %dx%d — may appear blurry or pixelated",
			cfg.Width, cfg.Height, MinRecommendedWidth, MinRecommendedHeight))
	}

	if cfg.Width > MaxRecommendedWidth || cfg.Height > MaxRecommendedHeight {
		warnings = append(warnings, fmt.Sprintf(
			"image resolution %dx%d exceeds %dx%d — some boot firmware may not display this correctly",
			cfg.Width, cfg.Height, MaxRecommendedWidth, MaxRecommendedHeight))
	}

	// GRUB-specific format checks.
	if _, err := f.Seek(0, 0); err == nil {
		if isJPEG {
			if isProgressiveJPEG(f) {
				warnings = append(warnings, "JPEG is progressive — GRUB requires baseline (non-progressive) JPEG; it will be converted automatically when applied")
			}
		}
		if isPNG {
			if _, err := f.Seek(0, 0); err == nil {
				if isInterlacedPNG(f) {
					warnings = append(warnings, "PNG uses Adam7 interlacing — GRUB does not support interlaced PNG and will silently skip the image; it will be converted automatically when applied")
				}
			}
			if _, err := f.Seek(0, 0); err == nil {
				if isIndexedPNG(f) {
					warnings = append(warnings, "PNG uses indexed/paletted color — GRUB requires RGB or RGBA PNG; it will be converted automatically when applied")
				}
			}
		}
	}

	return info, warnings, nil
}

// isProgressiveJPEG scans JPEG markers to detect progressive encoding.
// Progressive JPEGs use SOF2 (0xFFC2) instead of SOF0 (0xFFC0) for baseline.
func isProgressiveJPEG(r io.ReadSeeker) bool {
	buf := make([]byte, 2)
	// Skip SOI marker (0xFFD8).
	if _, err := r.Read(buf); err != nil {
		return false
	}

	for {
		// Read marker.
		if _, err := io.ReadFull(r, buf); err != nil {
			return false
		}
		if buf[0] != 0xFF {
			return false
		}

		marker := buf[1]

		// SOF0 = baseline, SOF2 = progressive.
		if marker == 0xC0 {
			return false // baseline
		}
		if marker == 0xC2 {
			return true // progressive
		}

		// SOS (start of scan) — stop searching.
		if marker == 0xDA {
			return false
		}

		// Skip markers without length (RST, TEM, etc.).
		if marker == 0x00 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}

		// Read segment length and skip.
		var segLen uint16
		if err := binary.Read(r, binary.BigEndian, &segLen); err != nil {
			return false
		}
		if segLen < 2 {
			return false
		}
		if _, err := r.Seek(int64(segLen-2), io.SeekCurrent); err != nil {
			return false
		}
	}
}

// isInterlacedPNG checks if a PNG file uses Adam7 interlacing, which GRUB
// does not support (grub-core/video/readers/png.c rejects interlace method != 0).
func isInterlacedPNG(r io.ReadSeeker) bool {
	// PNG IHDR layout (from byte 0 of file):
	//   8  sig | 4 len | 4 "IHDR" | 4 width | 4 height | 1 depth | 1 color | 1 compress | 1 filter | 1 interlace
	// Interlace byte is at offset 28.
	buf := make([]byte, 29)
	if _, err := io.ReadFull(r, buf); err != nil {
		return false
	}
	return buf[28] != 0 // 0 = None, 1 = Adam7
}

// isIndexedPNG checks if a PNG file uses indexed (paletted) color type.
// PNG color type 3 = indexed.
func isIndexedPNG(r io.ReadSeeker) bool {
	// PNG header: 8 bytes signature, then IHDR chunk.
	// IHDR: 4 bytes length, 4 bytes "IHDR", 4 bytes width, 4 bytes height, 1 byte bit depth, 1 byte color type.
	buf := make([]byte, 26)
	if _, err := io.ReadFull(r, buf); err != nil {
		return false
	}
	// Color type is at offset 25 (8 sig + 4 len + 4 type + 4 width + 4 height + 1 bit depth).
	colorType := buf[25]
	return colorType == 3 // 3 = indexed/paletted
}
