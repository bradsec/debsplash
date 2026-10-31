package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Target represents a splash screen target.
type Target string

const (
	TargetGrub     Target = "grub"
	TargetPlymouth Target = "plymouth"
	TargetLogin    Target = "login"
)

// AllTargets is the ordered list of all known targets.
var AllTargets = []Target{TargetGrub, TargetPlymouth, TargetLogin}

// ParseTargets validates and parses a comma-separated list of target names.
func ParseTargets(csv string) ([]Target, error) {
	if csv == "" {
		return nil, nil
	}
	valid := map[string]Target{
		"grub":     TargetGrub,
		"plymouth": TargetPlymouth,
		"login":    TargetLogin,
	}
	var targets []Target
	for s := range strings.SplitSeq(csv, ",") {
		s = strings.TrimSpace(strings.ToLower(s))
		t, ok := valid[s]
		if !ok {
			return nil, fmt.Errorf("unknown target %q (valid: grub, plymouth, login)", s)
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// Dimensions holds width and height for image processing.
type Dimensions struct {
	Width  int
	Height int
}

// ResizeMode controls how images are resized for a target.
type ResizeMode string

const (
	ResizeNone ResizeMode = "none"
	ResizeFit  ResizeMode = "fit"  // scale to fit, may letterbox
	ResizeFill ResizeMode = "fill" // scale to fill, center-crop excess
	ResizeCrop ResizeMode = "crop" // crop to aspect ratio, then scale
)

// DefaultTargetDimensions returns the default dimensions for each target.
func DefaultTargetDimensions() map[Target]Dimensions {
	return map[Target]Dimensions{
		TargetGrub:     {Width: 1920, Height: 1080},
		TargetPlymouth: {Width: 1920, Height: 1080},
		TargetLogin:    {Width: 1920, Height: 1080},
	}
}

// hexColorRe matches valid 3- or 6-digit hex color strings like #FFF or #ff8800.
var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$`)

// ValidateHexColor validates that s is a well-formed hex color string.
func ValidateHexColor(s string) error {
	if !hexColorRe.MatchString(s) {
		return fmt.Errorf("invalid hex color %q: must match #RGB or #RRGGBB", s)
	}
	return nil
}

const (
	// BackupBaseDir is where all backups are stored.
	BackupBaseDir = "/var/lib/debsplash/backups"

	// OriginalBackupDir is where the very first (factory) backup is stored.
	// This is written once on first run and never overwritten.
	OriginalBackupDir = "/var/lib/debsplash/original"
	StateDir          = "/var/lib/debsplash"
	// Legacy names stay until installations made by splashchanger no longer need migration.
	LegacyStateDir              = "/var/lib/splashchanger"
	LegacyLockPath              = "/var/run/splashchanger.lock"
	LegacyTheme                 = "splashchanger"
	LegacyGrubMarker            = "# splashchanger: "
	LegacyCSSStart              = "/* splashchanger-start */"
	LegacyCSSEnd                = "/* splashchanger-end */"
	LegacyGresourceBackupSuffix = ".splashchanger-backup"
	LegacyGresourceHashSuffix   = ".splashchanger-sha256"
	LegacyStagingDir            = "/usr/share/splashchanger"
	LegacyGrubBackground        = "/boot/grub/splashchanger-background"
	LegacyGDMDconf              = "/etc/dconf/db/gdm.d/00-splashchanger"
	LegacyGDMOverride           = "/usr/share/gdm/dconf/91-splashchanger"

	// GrubConfigFile is the GRUB defaults file.
	GrubConfigFile = "/etc/default/grub"

	// GrubBackgroundDir is where we copy the GRUB background image.
	GrubBackgroundDir = "/boot/grub"

	// PlymouthThemeDir is the Plymouth themes directory.
	PlymouthThemeDir = "/usr/share/plymouth/themes"

	// PlymouthDefaultTheme is the Plymouth default theme config.
	PlymouthDefaultTheme = "/etc/plymouth/plymouthd.conf"
)

// EncryptScreenConfig holds customization options for the Plymouth
// encryption password prompt (position, color, style).
type EncryptScreenConfig struct {
	// HAlign controls horizontal position of the password box (0.0=left, 0.5=center, 1.0=right).
	HAlign float64
	// VAlign controls vertical position of the password box (0.0=top, 0.5=center, 1.0=bottom).
	VAlign float64
	// BoxColor is the background color of the password box in hex (e.g. "#000000").
	BoxColor string
	// BoxOpacity is the opacity of the password box background (0.0=transparent, 1.0=opaque).
	BoxOpacity float64
	// TextColor is the color of the password prompt text in hex (e.g. "#FFFFFF").
	TextColor string
	// FontSize is the font size for the password prompt text.
	FontSize int
	// Style controls the visual style of the password box: "minimal", "boxed", or "floating".
	Style string
}

// Config holds runtime configuration for debsplash.
type Config struct {
	BackupDir        string
	EncryptScreen    EncryptScreenConfig
	DryRun           bool
	Resize           ResizeMode
	TargetDimensions map[Target]Dimensions
}

// DefaultEncryptScreenConfig returns sensible defaults for the encryption screen.
func DefaultEncryptScreenConfig() EncryptScreenConfig {
	return EncryptScreenConfig{
		HAlign:     0.5,
		VAlign:     0.7,
		BoxColor:   "#000000",
		BoxOpacity: 0.7,
		TextColor:  "#FFFFFF",
		FontSize:   14,
		Style:      "boxed",
	}
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		BackupDir:        BackupBaseDir,
		EncryptScreen:    DefaultEncryptScreenConfig(),
		Resize:           ResizeNone,
		TargetDimensions: DefaultTargetDimensions(),
	}
}
