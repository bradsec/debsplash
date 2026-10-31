package cmd

import (
	"fmt"
	"regexp"

	"github.com/bradsec/debsplash/internal/backup"
	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/bradsec/debsplash/internal/imgutil"
	"github.com/bradsec/debsplash/internal/safepath"
)

var saveOriginal = backup.SaveOriginal

func validateEncryptFlags() error {
	if err := config.ValidateHexColor(flags.encrypt.boxColor); err != nil {
		return fmt.Errorf("invalid --box-color: %w", err)
	}
	if err := config.ValidateHexColor(flags.encrypt.textColor); err != nil {
		return fmt.Errorf("invalid --text-color: %w", err)
	}
	switch flags.encrypt.style {
	case "minimal", "boxed", "floating":
	default:
		return fmt.Errorf("invalid --style %q: must be minimal, boxed, or floating", flags.encrypt.style)
	}
	if flags.encrypt.fontSize < 1 || flags.encrypt.fontSize > 32 {
		return fmt.Errorf("invalid --font-size %d: must be between 1 and 32", flags.encrypt.fontSize)
	}
	return nil
}

func buildConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.DryRun = flags.dryRun
	cfg.EncryptScreen.HAlign = clamp01(flags.encrypt.halign)
	cfg.EncryptScreen.VAlign = clamp01(flags.encrypt.valign)
	cfg.EncryptScreen.BoxColor = flags.encrypt.boxColor
	cfg.EncryptScreen.BoxOpacity = clamp01(flags.encrypt.boxOpacity)
	cfg.EncryptScreen.TextColor = flags.encrypt.textColor
	cfg.EncryptScreen.FontSize = flags.encrypt.fontSize
	cfg.EncryptScreen.Style = flags.encrypt.style

	cfg.Resize = config.ResizeMode(flags.resize)

	// Apply resolution override to all targets.
	if flags.resWidth > 0 && flags.resHeight > 0 {
		cfg.TargetDimensions[config.TargetGrub] = config.Dimensions{Width: flags.resWidth, Height: flags.resHeight}
		cfg.TargetDimensions[config.TargetPlymouth] = config.Dimensions{Width: flags.resWidth, Height: flags.resHeight}
		cfg.TargetDimensions[config.TargetLogin] = config.Dimensions{Width: flags.resWidth, Height: flags.resHeight}
	}

	return cfg
}

// Resolution bounds for --resolution flag.
const (
	minResWidth  = 320
	minResHeight = 240
	maxResWidth  = 7680
	maxResHeight = 4320
)

// resolutionRe matches strict WIDTHxHEIGHT format.
var resolutionRe = regexp.MustCompile(`^\d{1,5}x\d{1,5}$`)

// parseResolution parses a "WIDTHxHEIGHT" string strictly.
func parseResolution(s string) (int, int, error) {
	if !resolutionRe.MatchString(s) {
		return 0, 0, fmt.Errorf("invalid resolution %q: must be WIDTHxHEIGHT (e.g. 1920x1080)", s)
	}
	var w, h int
	fmt.Sscanf(s, "%dx%d", &w, &h)
	return w, h, nil
}

// validateGlobalFlags validates global CLI flags before command execution.
func validateGlobalFlags() error {
	// Validate resize mode.
	switch config.ResizeMode(flags.resize) {
	case config.ResizeNone, config.ResizeFit, config.ResizeFill, config.ResizeCrop:
	default:
		return fmt.Errorf("invalid --resize mode %q: must be none, fit, fill, or crop", flags.resize)
	}

	// Validate resolution format and bounds.
	if flags.resolution != "" {
		w, h, err := parseResolution(flags.resolution)
		if err != nil {
			return err
		}
		if w < minResWidth || h < minResHeight {
			return fmt.Errorf("--resolution %dx%d is below minimum %dx%d", w, h, minResWidth, minResHeight)
		}
		if w > maxResWidth || h > maxResHeight {
			return fmt.Errorf("--resolution %dx%d exceeds maximum %dx%d", w, h, maxResWidth, maxResHeight)
		}
		flags.resWidth = w
		flags.resHeight = h
	}

	return nil
}

// validateAndResolveImage sanitizes the path, validates the image, and prints
// info. For a real apply it returns a private snapshot of the validated bytes;
// callers must read only that path and call cleanup when done, so replacing
// the original file after validation cannot change what gets installed.
// Dry-run returns the original path after validation and never installs it.
func validateAndResolveImage(rawPath string) (string, func(), error) {
	imgPath, err := safepath.ValidateImagePath(rawPath)
	if err != nil {
		return "", nil, fmt.Errorf("unsafe image path: %w", err)
	}

	if err := safepath.ValidateForInterpolation(imgPath); err != nil {
		return "", nil, fmt.Errorf("unsafe characters in image path: %w", err)
	}

	var info *imgutil.ImageInfo
	var warnings []string
	var resolvedPath string
	cleanup := func() {}
	if flags.dryRun {
		info, warnings, err = imgutil.ValidateOpened(imgPath)
		resolvedPath = imgPath
	} else {
		var snap *imgutil.Snapshot
		snap, err = imgutil.TakeSnapshot(imgPath)
		if err == nil {
			info, warnings = snap.Info, snap.Warnings
			resolvedPath = snap.Path
			cleanup = func() {
				if err := snap.Remove(); err != nil {
					fmt.Printf("  [warning] Could not remove image snapshot: %v\n", err)
				}
			}
		}
	}
	if err != nil {
		return "", nil, fmt.Errorf("invalid image: %w", err)
	}
	fmt.Printf("  [validate] Image: %s (%s, %dx%d)\n", imgPath, info.Format, info.Width, info.Height)
	for _, w := range warnings {
		fmt.Printf("  [validate] Warning: %s\n", w)
	}
	if flags.resize != "" && flags.resize != "none" {
		fmt.Printf("  [validate] Resize mode: %s\n", flags.resize)
	}

	return resolvedPath, cleanup, nil
}

// detectAndBackup runs environment detection and backup with the given error policy.
// Returns the detected environment.
func detectAndBackup(cfg *config.Config, policy backupPolicy) (*detect.Environment, error) {
	fmt.Println("[debsplash] Detecting environment...")
	env, err := detect.DetectEnvironment()
	if err != nil {
		return nil, fmt.Errorf("environment detection failed: %w", err)
	}

	// Save original system files on first run (never overwrites).
	if err := saveOriginal(cfg, env); err != nil {
		return env, fmt.Errorf("could not save original files: %w", err)
	}

	if cfg.DryRun {
		fmt.Println("[debsplash] Checking automatic backup plan...")
	} else {
		fmt.Println("[debsplash] Taking automatic backup...")
	}
	if err := backup.TakeBackup(cfg, env); err != nil {
		if policy.requireBackup {
			return env, fmt.Errorf("backup failed: %w", err)
		}
		fmt.Printf("  [warning] Backup failed: %v (continuing without backup)\n", err)
	}

	return env, nil
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
