package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDryRunPreRunCreatesNoLock(t *testing.T) {
	oldPath, oldDryRun := lockPath, flags.dryRun
	lockPath = filepath.Join(t.TempDir(), "splashchanger.lock")
	flags.dryRun = true
	t.Cleanup(func() {
		if activeLock != nil {
			activeLock.Release()
			activeLock = nil
		}
		lockPath, flags.dryRun = oldPath, oldDryRun
	})

	_ = rootCmd.PersistentPreRunE(grubCmd, nil)
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created lock file: %v", err)
	}
}

func TestDryRunValidationCreatesNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	imageDir := t.TempDir()
	image := filepath.Join(imageDir, "image.png")
	writeTestPNG(t, image, 4, 4)
	oldDryRun := flags.dryRun
	flags.dryRun = true
	t.Cleanup(func() { flags.dryRun = oldDryRun })

	_, cleanup, err := validateAndResolveImage(image)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry-run created %d item(s) in TMPDIR", len(entries))
	}
}
