package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/spf13/cobra"
)

func TestLyIsNotAnApplyTarget(t *testing.T) {
	env := &detect.Environment{LoginManager: detect.LMLY, HasGrub: true}
	targets, err := resolveTargets(env, "", "")
	if err != nil || len(targets) != 1 || targets[0] != config.TargetGrub {
		t.Fatalf("targets = %v, error = %v; want only grub", targets, err)
	}
}

func TestFontSizeBounds(t *testing.T) {
	old := flags.encrypt
	t.Cleanup(func() { flags.encrypt = old })
	flags.encrypt.boxColor, flags.encrypt.textColor, flags.encrypt.style = "#000000", "#FFFFFF", "boxed"
	for _, size := range []int{-1, 0, 33} {
		flags.encrypt.fontSize = size
		if err := validateEncryptFlags(); err == nil {
			t.Errorf("font size %d accepted", size)
		}
	}
	for _, size := range []int{1, 32} {
		flags.encrypt.fontSize = size
		if err := validateEncryptFlags(); err != nil {
			t.Errorf("font size %d rejected: %v", size, err)
		}
	}
}

func TestCompletionNeedsNoRootOrLock(t *testing.T) {
	oldPath, oldDryRun := lockPath, flags.dryRun
	lockPath, flags.dryRun = filepath.Join(t.TempDir(), "lock"), false
	t.Cleanup(func() { lockPath, flags.dryRun = oldPath, oldDryRun })
	for _, name := range []string{"completion", "__complete", "__completeNoDesc"} {
		cmd := &cobra.Command{Use: name}
		if err := rootCmd.PersistentPreRunE(cmd, nil); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("completion created lock: %v", err)
	}
	if os.Geteuid() != 0 {
		err := rootCmd.PersistentPreRunE(grubCmd, nil)
		if err == nil || !strings.Contains(err.Error(), "must be run as root") {
			t.Errorf("root gate: %v", err)
		}
	}
}

func TestClamp01(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tt := range tests {
		if got := clamp01(tt.in); got != tt.want {
			t.Errorf("clamp01(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestBuildConfig(t *testing.T) {
	flags.encrypt.halign = 0.3
	flags.encrypt.valign = 0.8
	flags.encrypt.boxColor = "#FF0000"
	flags.encrypt.boxOpacity = 0.5
	flags.encrypt.textColor = "#00FF00"
	flags.encrypt.fontSize = 18
	flags.encrypt.style = "floating"
	flags.dryRun = true

	cfg := buildConfig()

	if cfg.EncryptScreen.HAlign != 0.3 {
		t.Errorf("HAlign = %v, want 0.3", cfg.EncryptScreen.HAlign)
	}
	if cfg.EncryptScreen.VAlign != 0.8 {
		t.Errorf("VAlign = %v, want 0.8", cfg.EncryptScreen.VAlign)
	}
	if cfg.EncryptScreen.BoxColor != "#FF0000" {
		t.Errorf("BoxColor = %v, want #FF0000", cfg.EncryptScreen.BoxColor)
	}
	if cfg.EncryptScreen.BoxOpacity != 0.5 {
		t.Errorf("BoxOpacity = %v, want 0.5", cfg.EncryptScreen.BoxOpacity)
	}
	if cfg.EncryptScreen.TextColor != "#00FF00" {
		t.Errorf("TextColor = %v, want #00FF00", cfg.EncryptScreen.TextColor)
	}
	if cfg.EncryptScreen.FontSize != 18 {
		t.Errorf("FontSize = %v, want 18", cfg.EncryptScreen.FontSize)
	}
	if cfg.EncryptScreen.Style != "floating" {
		t.Errorf("Style = %v, want floating", cfg.EncryptScreen.Style)
	}
	if !cfg.DryRun {
		t.Error("DryRun = false, want true")
	}

	// Reset for other tests
	flags.dryRun = false
}

func TestBuildConfigClampsValues(t *testing.T) {
	flags.encrypt.halign = 1.5
	flags.encrypt.valign = -0.3
	flags.encrypt.boxOpacity = 2.0

	cfg := buildConfig()

	if cfg.EncryptScreen.HAlign != 1.0 {
		t.Errorf("HAlign = %v, want 1.0 (clamped)", cfg.EncryptScreen.HAlign)
	}
	if cfg.EncryptScreen.VAlign != 0.0 {
		t.Errorf("VAlign = %v, want 0.0 (clamped)", cfg.EncryptScreen.VAlign)
	}
	if cfg.EncryptScreen.BoxOpacity != 1.0 {
		t.Errorf("BoxOpacity = %v, want 1.0 (clamped)", cfg.EncryptScreen.BoxOpacity)
	}
}

func TestValidateEncryptFlags_ValidColors(t *testing.T) {
	flags.encrypt.boxColor = "#000000"
	flags.encrypt.textColor = "#FFFFFF"
	flags.encrypt.style = "boxed"

	if err := validateEncryptFlags(); err != nil {
		t.Errorf("unexpected error with valid flags: %v", err)
	}
}

func TestValidateEncryptFlags_InvalidBoxColor(t *testing.T) {
	flags.encrypt.boxColor = "not-a-color"
	flags.encrypt.textColor = "#FFFFFF"
	flags.encrypt.style = "boxed"

	err := validateEncryptFlags()
	if err == nil {
		t.Error("expected error for invalid box-color, got nil")
	}
}

func TestValidateEncryptFlags_InvalidTextColor(t *testing.T) {
	flags.encrypt.boxColor = "#000000"
	flags.encrypt.textColor = "bad"
	flags.encrypt.style = "boxed"

	err := validateEncryptFlags()
	if err == nil {
		t.Error("expected error for invalid text-color, got nil")
	}
}

func TestValidateEncryptFlags_InvalidStyle(t *testing.T) {
	flags.encrypt.boxColor = "#000000"
	flags.encrypt.textColor = "#FFFFFF"
	flags.encrypt.style = "nope"

	err := validateEncryptFlags()
	if err == nil {
		t.Error("expected error for invalid style, got nil")
	}
}

func TestValidateEncryptFlags_ValidStyles(t *testing.T) {
	flags.encrypt.boxColor = "#000000"
	flags.encrypt.textColor = "#FFFFFF"

	for _, s := range []string{"minimal", "boxed", "floating"} {
		flags.encrypt.style = s
		if err := validateEncryptFlags(); err != nil {
			t.Errorf("unexpected error for style %q: %v", s, err)
		}
	}
}

func TestRootHasSubcommands(t *testing.T) {
	expected := map[string]bool{
		"apply":   false,
		"grub":    false,
		"encrypt": false,
		"login":   false,
		"backup":  false,
		"restore": false,
		"status":  false,
	}

	for _, cmd := range rootCmd.Commands() {
		if _, ok := expected[cmd.Name()]; ok {
			expected[cmd.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected subcommand %q not found on rootCmd", name)
		}
	}
}

func TestDryRunIsPersistentFlag(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("dry-run")
	if f == nil {
		t.Fatal("--dry-run persistent flag not found on rootCmd")
	}
	if f.DefValue != "false" {
		t.Errorf("--dry-run default = %q, want \"false\"", f.DefValue)
	}
}

func TestRestoreHasForceFlag(t *testing.T) {
	f := restoreCmd.Flags().Lookup("force")
	if f == nil {
		t.Fatal("--force flag not found on restoreCmd")
	}
	if f.DefValue != "false" {
		t.Errorf("--force default = %q, want \"false\"", f.DefValue)
	}
	if f.Shorthand != "f" {
		t.Errorf("--force shorthand = %q, want \"f\"", f.Shorthand)
	}
}

func TestValidateAndResolveImage_UnsafePath(t *testing.T) {
	_, _, err := validateAndResolveImage("/tmp/test;evil.png")
	if err == nil {
		t.Error("expected error for unsafe path, got nil")
	}
}

func TestResolveTargets_DefaultAll(t *testing.T) {
	env := &detect.Environment{
		HasGrub:      true,
		HasPlymouth:  true,
		LoginManager: detect.LMGDM,
	}
	targets, err := resolveTargets(env, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("got %d targets, want 3", len(targets))
	}
	if targets[0] != config.TargetGrub || targets[1] != config.TargetPlymouth || targets[2] != config.TargetLogin {
		t.Errorf("targets = %v, want [grub plymouth login]", targets)
	}
}

func TestResolveTargets_TargetsFlag(t *testing.T) {
	env := &detect.Environment{
		HasGrub:      true,
		HasPlymouth:  true,
		LoginManager: detect.LMGDM,
	}
	targets, err := resolveTargets(env, "grub,login", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	if targets[0] != config.TargetGrub || targets[1] != config.TargetLogin {
		t.Errorf("targets = %v, want [grub login]", targets)
	}
}

func TestResolveTargets_SkipFlag(t *testing.T) {
	env := &detect.Environment{
		HasGrub:      true,
		HasPlymouth:  true,
		LoginManager: detect.LMGDM,
	}
	targets, err := resolveTargets(env, "", "plymouth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("got %d targets, want 2", len(targets))
	}
	if targets[0] != config.TargetGrub || targets[1] != config.TargetLogin {
		t.Errorf("targets = %v, want [grub login]", targets)
	}
}

func TestResolveTargets_MutuallyExclusive(t *testing.T) {
	env := &detect.Environment{HasGrub: true}
	_, err := resolveTargets(env, "grub", "login")
	if err == nil {
		t.Error("expected error for mutually exclusive flags, got nil")
	}
}

func TestResolveTargets_UnavailableTarget(t *testing.T) {
	env := &detect.Environment{
		HasGrub:      true,
		HasPlymouth:  false,
		LoginManager: detect.LMUnknown,
	}
	targets, err := resolveTargets(env, "grub", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 1 || targets[0] != config.TargetGrub {
		t.Errorf("targets = %v, want [grub]", targets)
	}
}

func TestResolveTargets_AllRequestedUnavailable(t *testing.T) {
	env := &detect.Environment{
		HasGrub:      false,
		HasPlymouth:  false,
		LoginManager: detect.LMUnknown,
	}
	_, err := resolveTargets(env, "grub", "")
	if err == nil {
		t.Error("expected error when no targets available, got nil")
	}
}

func TestResizeFlagRegistered(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("resize")
	if f == nil {
		t.Fatal("--resize persistent flag not found on rootCmd")
	}
	if f.DefValue != "fill" {
		t.Errorf("--resize default = %q, want \"fill\"", f.DefValue)
	}
}

func TestApplyTargetsFlagRegistered(t *testing.T) {
	f := applyCmd.Flags().Lookup("targets")
	if f == nil {
		t.Fatal("--targets flag not found on applyCmd")
	}
}

func TestApplySkipFlagRegistered(t *testing.T) {
	f := applyCmd.Flags().Lookup("skip")
	if f == nil {
		t.Fatal("--skip flag not found on applyCmd")
	}
}

func TestValidateGlobalFlags_InvalidResizeMode(t *testing.T) {
	origResize := flags.resize
	defer func() { flags.resize = origResize }()

	flags.resize = "stretch"
	err := validateGlobalFlags()
	if err == nil {
		t.Error("expected error for invalid resize mode 'stretch', got nil")
	}
}

func TestValidateGlobalFlags_ValidResizeModes(t *testing.T) {
	origResize := flags.resize
	origRes := flags.resolution
	defer func() {
		flags.resize = origResize
		flags.resolution = origRes
	}()

	flags.resolution = ""
	for _, mode := range []string{"none", "fit", "fill", "crop"} {
		flags.resize = mode
		if err := validateGlobalFlags(); err != nil {
			t.Errorf("unexpected error for resize mode %q: %v", mode, err)
		}
	}
}

func TestValidateGlobalFlags_InvalidResolutionFormat(t *testing.T) {
	origResize := flags.resize
	origRes := flags.resolution
	defer func() {
		flags.resize = origResize
		flags.resolution = origRes
	}()

	flags.resize = "none"

	for _, res := range []string{"bad", "100", "axb"} {
		flags.resolution = res
		if err := validateGlobalFlags(); err == nil {
			t.Errorf("expected error for resolution %q, got nil", res)
		}
	}
}

func TestValidateGlobalFlags_ResolutionBelowMinimum(t *testing.T) {
	origResize := flags.resize
	origRes := flags.resolution
	defer func() {
		flags.resize = origResize
		flags.resolution = origRes
	}()

	flags.resize = "none"
	flags.resolution = "100x100"
	if err := validateGlobalFlags(); err == nil {
		t.Error("expected error for resolution below minimum")
	}
}

func TestValidateGlobalFlags_ResolutionAboveMaximum(t *testing.T) {
	origResize := flags.resize
	origRes := flags.resolution
	defer func() {
		flags.resize = origResize
		flags.resolution = origRes
	}()

	flags.resize = "none"
	flags.resolution = "99999x99999"
	if err := validateGlobalFlags(); err == nil {
		t.Error("expected error for resolution above maximum")
	}
}

func TestValidateGlobalFlags_ValidResolution(t *testing.T) {
	origResize := flags.resize
	origRes := flags.resolution
	defer func() {
		flags.resize = origResize
		flags.resolution = origRes
	}()

	flags.resize = "none"
	flags.resolution = "1920x1080"
	if err := validateGlobalFlags(); err != nil {
		t.Errorf("unexpected error for valid resolution: %v", err)
	}
}

func TestResolveTargets_EmptyEnvironment(t *testing.T) {
	env := &detect.Environment{
		HasGrub:      false,
		HasPlymouth:  false,
		LoginManager: detect.LMUnknown,
	}
	targets, err := resolveTargets(env, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("expected no targets for empty environment, got %v", targets)
	}
}

func TestResolveTargets_AllSkipped(t *testing.T) {
	env := &detect.Environment{
		HasGrub:      true,
		HasPlymouth:  true,
		LoginManager: detect.LMGDM,
	}
	_, err := resolveTargets(env, "", "grub,plymouth,login")
	if err == nil {
		t.Error("expected error when all targets skipped, got nil")
	}
}

func TestResolveTargets_UnknownTarget(t *testing.T) {
	env := &detect.Environment{HasGrub: true}
	_, err := resolveTargets(env, "nonexistent", "")
	if err == nil {
		t.Error("expected error for unknown target, got nil")
	}
}

func TestParseResolution_Valid(t *testing.T) {
	w, h, err := parseResolution("1920x1080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 1920 || h != 1080 {
		t.Errorf("got %dx%d, want 1920x1080", w, h)
	}
}

func TestParseResolution_RejectsTrailingGarbage(t *testing.T) {
	_, _, err := parseResolution("1920x1080garbage")
	if err == nil {
		t.Error("expected error for trailing garbage")
	}
}

func TestParseResolution_RejectsExtraDimension(t *testing.T) {
	_, _, err := parseResolution("1920x1080x999")
	if err == nil {
		t.Error("expected error for extra dimension")
	}
}

func TestParseResolution_RejectsEmpty(t *testing.T) {
	_, _, err := parseResolution("")
	if err == nil {
		t.Error("expected error for empty string")
	}
}

func TestParseResolution_RejectsNonNumeric(t *testing.T) {
	_, _, err := parseResolution("widexhigh")
	if err == nil {
		t.Error("expected error for non-numeric")
	}
}
