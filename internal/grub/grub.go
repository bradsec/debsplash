package grub

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/deps"
	"github.com/bradsec/debsplash/internal/fileutil"
	"github.com/bradsec/debsplash/internal/imgutil"
)

const (
	grubConfig        = "/etc/default/grub"
	grubBgDest        = "/boot/grub/debsplash-background"
	grubBgKey         = "GRUB_BACKGROUND"
	grubTerminalKey   = "GRUB_TERMINAL"
	grubGfxmodeKey    = "GRUB_GFXMODE"
	grubGfxpayloadKey = "GRUB_GFXPAYLOAD_LINUX"
	// grubGfxmode: "auto" lets GRUB select the best mode the firmware supports.
	// A hardcoded mode (e.g. 1024x768x32) causes GRUB to fall back to text mode
	// (plain blue screen) on any system that doesn't support that exact mode+depth.
	grubGfxmode = "auto"
)

// BackgroundPNG is the image Apply installs; backup includes it in its scope.
const BackgroundPNG = grubBgDest + ".png"

// Apply sets the GRUB background image and regenerates grub.cfg.
//
// We do NOT set GRUB_BACKGROUND in /etc/default/grub. 05_debian_theme reads
// that key and calls set_background_image without colour arguments, which is
// identical to what happens when it auto-discovers a *.png in /boot/grub/ —
// so setting the key adds no value but would prevent the desktop-base colour
// fallback from ever being tried for other targets. Relying on auto-discovery
// is simpler and equally correct.
//
// We do NOT set GRUB_TERMINAL. Setting it adds "terminal_input gfxterm" to
// grub.cfg, which was absent in the original working Debian config.
func Apply(imgPath string) error {
	// Always write as PNG regardless of input format. This normalises the image
	// to 8-bit non-interlaced RGBA, which is the only format GRUB's PNG reader
	// reliably accepts. Interlaced PNGs and indexed-colour PNGs cause GRUB to
	// silently skip the background image with no on-screen error.
	destPath := BackgroundPNG

	// Remove other-extension leftovers from previous runs. 05_debian_theme
	// scans /boot/grub/ for *.jpg before *.png, so a stale JPEG would be
	// picked up ahead of our normalised PNG.
	for _, ext := range []string{".jpg", ".jpeg", ".tga"} {
		old := grubBgDest + ext
		if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Printf("  [grub] Warning: could not remove old image %s: %v\n", old, err)
		}
	}
	fmt.Printf("  [grub] Normalising image to %s\n", destPath)
	if err := imgutil.NormalizeForGrub(imgPath, destPath); err != nil {
		return fmt.Errorf("failed to normalise image: %w", err)
	}
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".tga"} {
		if err := os.Remove(config.LegacyGrubBackground + ext); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	fmt.Printf("  [grub] Updating %s\n", grubConfig)

	// Remove any GRUB_BACKGROUND we may have set in a previous run — Debian's
	// 05_debian_theme auto-discovers *.png files in /boot/grub/ when this key
	// is absent, which is the correct single-command path.
	if err := removeGrubKey(grubBgKey); err != nil {
		return fmt.Errorf("failed to remove %s: %w", grubBgKey, err)
	}

	// Remove any GRUB_TERMINAL we may have set in a previous run.
	if err := removeGrubKey(grubTerminalKey); err != nil {
		return fmt.Errorf("failed to remove %s: %w", grubTerminalKey, err)
	}

	if err := ensureGfxmode(); err != nil {
		return fmt.Errorf("failed to set gfxmode: %w", err)
	}

	if err := ensureGfxpayload(); err != nil {
		return fmt.Errorf("failed to set gfxpayload: %w", err)
	}

	if err := ensureBootSplash(); err != nil {
		return fmt.Errorf("failed to enable boot splash: %w", err)
	}

	fmt.Println("  [grub] Running update-grub...")
	if err := deps.EnsureCommand("update-grub"); err != nil {
		return err
	}
	out, err := exec.Command("update-grub").CombinedOutput()
	if err != nil {
		return fmt.Errorf("update-grub failed: %w\n%s", err, string(out))
	}

	fmt.Println("  [grub] Done.")
	return nil
}

// ensureBootSplash checks GRUB_CMDLINE_LINUX_DEFAULT for the "splash" keyword
// and adds it if missing, so Plymouth boot splash is displayed.
func ensureBootSplash() error {
	lines, err := readLines(grubConfig)
	if err != nil {
		return err
	}

	updated, err := ensureSplashInLines(lines)
	if err != nil {
		return err
	}
	if updated == nil {
		return nil // already has splash
	}

	fmt.Println("  [grub] Enabling boot splash (adding 'splash' to GRUB_CMDLINE_LINUX_DEFAULT)...")
	return writeLines(grubConfig, updated)
}

// ensureGfxmode sets GRUB_GFXMODE=auto so GRUB picks the best mode the
// firmware supports. A hardcoded mode+depth (e.g. 1024x768x32) causes GRUB
// to fall back to text mode on any system that doesn't support it exactly.
func ensureGfxmode() error {
	lines, err := readLines(grubConfig)
	if err != nil {
		return err
	}
	updated := setOrReplaceKey(lines, grubGfxmodeKey, grubGfxmode)
	if updated == nil {
		return nil
	}
	fmt.Printf("  [grub] Setting GRUB_GFXMODE=%s...\n", grubGfxmode)
	return writeLines(grubConfig, updated)
}

// ensureGfxpayload sets GRUB_GFXPAYLOAD_LINUX=keep so the framebuffer is
// preserved when handing off to the kernel/Plymouth.
func ensureGfxpayload() error {
	lines, err := readLines(grubConfig)
	if err != nil {
		return err
	}
	updated := setOrReplaceKey(lines, grubGfxpayloadKey, "keep")
	if updated == nil {
		return nil
	}
	fmt.Println("  [grub] Setting GRUB_GFXPAYLOAD_LINUX=keep...")
	return writeLines(grubConfig, updated)
}

// removeGrubKey removes settings previously written by debsplash.
func removeGrubKey(key string) error {
	lines, err := readLines(grubConfig)
	if err != nil {
		return err
	}
	result := removeGrubKeyLines(lines, key)
	if key == grubTerminalKey {
		for _, line := range result {
			if strings.HasPrefix(strings.TrimSpace(line), key+"=") {
				fmt.Printf("  [grub] Warning: preserving user %s; it may prevent the background from showing\n", key)
				break
			}
		}
	}
	if len(result) == len(lines) {
		return nil
	}
	fmt.Printf("  [grub] Removing %s from %s\n", key, grubConfig)
	return writeLines(grubConfig, result)
}

func removeGrubKeyLines(lines []string, key string) []string {
	result := make([]string, 0, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		value, active := strings.CutPrefix(trimmed, key+"=")
		if active && i > 0 && (strings.TrimSpace(lines[i-1]) == "# debsplash: "+key || strings.TrimSpace(lines[i-1]) == config.LegacyGrubMarker+key) {
			result = result[:len(result)-1]
			continue
		}
		if active && ((key == grubTerminalKey && value == `"gfxterm"`) ||
			(key == grubBgKey && (value == `"`+config.LegacyGrubBackground+`.png"` || value == `"`+config.LegacyGrubBackground+`.jpg"` || value == `"`+config.LegacyGrubBackground+`.jpeg"` || value == `"`+config.LegacyGrubBackground+`.tga"` || value == `"`+grubBgDest+`.png"` ||
				value == `"`+grubBgDest+`.jpg"` || value == `"`+grubBgDest+`.jpeg"` ||
				value == `"`+grubBgDest+`.tga"`))) {
			continue
		}
		result = append(result, line)
	}
	return result
}

// setOrReplaceKey only replaces settings marked as owned by debsplash.
// Returns nil when no change is needed.
func setOrReplaceKey(lines []string, key, value string) []string {
	newLine := fmt.Sprintf(`%s="%s"`, key, value)
	marker := "# debsplash: " + key
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") {
			if i == 0 || (strings.TrimSpace(lines[i-1]) != marker && strings.TrimSpace(lines[i-1]) != config.LegacyGrubMarker+key) {
				return nil
			}
			if trimmed == newLine && strings.TrimSpace(lines[i-1]) == marker {
				return nil
			}
			result := append([]string(nil), lines...)
			result[i-1] = marker
			result[i] = newLine
			return result
		}
	}
	return append(append(append([]string(nil), lines...), marker), newLine)
}

// ensureSplashInLines adds "splash" to GRUB_CMDLINE_LINUX_DEFAULT if not present.
// Returns nil if no change is needed.
func ensureSplashInLines(lines []string) ([]string, error) {
	const key = "GRUB_CMDLINE_LINUX_DEFAULT"

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		after, ok := strings.CutPrefix(trimmed, key+"=")
		if !ok {
			continue
		}

		quote := byte(0)
		value := after
		suffix := ""
		if len(after) > 0 && (after[0] == '\'' || after[0] == '"') {
			quote = after[0]
			end := strings.IndexByte(after[1:], quote)
			if end < 0 {
				return nil, fmt.Errorf("%s: unmatched quote", key)
			}
			value = after[1 : end+1]
			suffix = after[end+2:]
		} else if pos := strings.IndexAny(after, " \t"); pos >= 0 {
			value, suffix = after[:pos], after[pos:]
		}
		unsupportedValue := strings.ContainsAny(value, `$\\`) ||
			(quote != 0 && strings.ContainsRune(value, rune(quote))) ||
			(quote == 0 && strings.ContainsAny(value, `'"#`))
		if unsupportedValue ||
			(suffix != "" && (suffix[0] != ' ' && suffix[0] != '\t' ||
				!strings.HasPrefix(strings.TrimSpace(suffix), "#") && strings.TrimSpace(suffix) != "")) {
			return nil, fmt.Errorf("%s: unsupported shell value %q", key, after)
		}

		// Check if "splash" is already present as a word.
		for w := range strings.FieldsSeq(value) {
			if w == "splash" {
				return nil, nil
			}
		}

		// Add splash.
		if value == "" {
			value = "splash"
		} else {
			value += " splash"
		}
		result := make([]string, len(lines))
		copy(result, lines)
		prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if quote == 0 {
			quote = '"'
		}
		quotedValue := string(quote) + value + string(quote)
		result[i] = prefix + key + "=" + quotedValue + suffix
		return result, nil
	}

	// No GRUB_CMDLINE_LINUX_DEFAULT line found — add one.
	result := make([]string, len(lines))
	copy(result, lines)
	return append(result, fmt.Sprintf(`%s="splash"`, key)), nil
}

// readLines reads a file into a slice of strings.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// writeLines writes a slice of strings to a file atomically.
func writeLines(path string, lines []string) error {
	var buf strings.Builder
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return fileutil.WriteFileAtomic(path, []byte(buf.String()), 0644)
}
