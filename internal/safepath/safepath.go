package safepath

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// MaxImageFileSize is the maximum allowed image file size (100 MB).
const MaxImageFileSize = 100 * 1024 * 1024

// sensitivePrefixes lists directories that should never be used as image sources.
var sensitivePrefixes = []string{"/proc/", "/sys/", "/dev/", "/run/"}

// ValidateImagePath resolves symlinks and rejects paths that could cause
// injection when interpolated into config files or scripts.
func ValidateImagePath(path string) (string, error) {
	// Reject null bytes and newlines — these break config file interpolation.
	if strings.ContainsAny(path, "\x00\n\r") {
		return "", fmt.Errorf("path contains null bytes or newlines")
	}

	// Reject control characters (ASCII 0x01-0x1F except tab).
	for _, c := range path {
		if c < 0x20 && c != '\t' {
			return "", fmt.Errorf("path contains control characters")
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("could not resolve path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("could not resolve symlinks: %w", err)
	}

	// Check resolved path against sensitive prefixes.
	if i := slices.IndexFunc(sensitivePrefixes, func(prefix string) bool {
		return strings.HasPrefix(resolved, prefix)
	}); i >= 0 {
		return "", fmt.Errorf("path %q resolves to restricted directory %s", path, sensitivePrefixes[i])
	}

	// Verify the resolved path doesn't point to a device file.
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("could not stat resolved path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path %q is not a regular file", resolved)
	}

	// Check file size limit.
	if info.Size() > MaxImageFileSize {
		return "", fmt.Errorf("file size %d bytes exceeds maximum allowed size of %d bytes", info.Size(), MaxImageFileSize)
	}

	return resolved, nil
}

// interpolationUnsafe lists characters that are dangerous when a path is
// interpolated into CSS url(), shell expansions, or config-file contexts.
var interpolationUnsafe = []byte{'\'', ')', ';', '{', '}', '`', '$'}

// ValidateForInterpolation rejects paths containing characters that could
// cause injection when interpolated into CSS, shell, or config file contexts.
func ValidateForInterpolation(path string) error {
	for _, c := range interpolationUnsafe {
		if strings.ContainsRune(path, rune(c)) {
			return fmt.Errorf("path contains unsafe character %q for interpolation context", string(c))
		}
	}
	return nil
}
