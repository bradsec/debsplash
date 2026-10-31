package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
)

func TestMigrateStatePreservesFirstOriginal(t *testing.T) {
	root := t.TempDir()
	oldNew, oldLegacy, oldOriginal := stateDir, legacyStateDir, originalBackupDir
	stateDir, legacyStateDir = filepath.Join(root, "debsplash"), filepath.Join(root, "splashchanger")
	originalBackupDir = filepath.Join(stateDir, "original")
	t.Cleanup(func() { stateDir, legacyStateDir, originalBackupDir = oldNew, oldLegacy, oldOriginal })
	original := filepath.Join(legacyStateDir, "original")
	if err := os.MkdirAll(original, 0700); err != nil {
		t.Fatal(err)
	}
	want := []byte("first original\x00unchanged")
	if err := os.WriteFile(filepath.Join(original, "manifest.txt"), want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateState(false); err != nil {
		t.Fatal(err)
	}
	if err := SaveOriginal(&config.Config{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyStateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy dir remains: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(stateDir, "original", "manifest.txt"))
	if err != nil || string(got) != string(want) {
		t.Fatalf("original changed: %q, %v", got, err)
	}
	if err := MigrateState(false); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(stateDir, "original", "manifest.txt"))
	if err != nil || string(got) != string(want) {
		t.Fatalf("second run changed original: %q, %v", got, err)
	}
}

func TestMigrateStateKeepsBothDirectories(t *testing.T) {
	root := t.TempDir()
	oldNew, oldLegacy := stateDir, legacyStateDir
	stateDir, legacyStateDir = filepath.Join(root, "debsplash"), filepath.Join(root, "splashchanger")
	t.Cleanup(func() { stateDir, legacyStateDir = oldNew, oldLegacy })
	for _, path := range []string{stateDir, legacyStateDir} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(legacyStateDir, "keep"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(legacyStateDir, "original", "manifest.txt"), "first original")
	output := captureStdout(t, func() {
		if err := MigrateState(false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "Warning") {
		t.Fatalf("warning missing: %q", output)
	}
	if got, err := os.ReadFile(filepath.Join(legacyStateDir, "keep")); err != nil || string(got) != "legacy" {
		t.Fatalf("legacy changed: %q, %v", got, err)
	}
	oldOriginal := originalBackupDir
	originalBackupDir = filepath.Join(stateDir, "original")
	t.Cleanup(func() { originalBackupDir = oldOriginal })
	if err := SaveOriginal(&config.Config{}, testEnv()); err == nil || !strings.Contains(err.Error(), "refusing to create") {
		t.Fatalf("would resnapshot over legacy original: %v", err)
	}
}

func TestMigrateStateDryRun(t *testing.T) {
	root := t.TempDir()
	oldNew, oldLegacy := stateDir, legacyStateDir
	stateDir, legacyStateDir = filepath.Join(root, "debsplash"), filepath.Join(root, "splashchanger")
	t.Cleanup(func() { stateDir, legacyStateDir = oldNew, oldLegacy })
	if err := os.Mkdir(legacyStateDir, 0700); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := MigrateState(true); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "Would migrate") {
		t.Fatalf("dry-run report missing: %q", output)
	}
	if _, err := os.Stat(legacyStateDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run made new dir: %v", err)
	}
}

func TestMigrateStateIgnoresLegacyOriginalStaging(t *testing.T) {
	p := isolateScope(t)
	oldNew, oldLegacy, oldOriginal := stateDir, legacyStateDir, originalBackupDir
	stateDir, legacyStateDir = filepath.Join(p.root, "debsplash"), filepath.Join(p.root, "splashchanger")
	originalBackupDir = filepath.Join(stateDir, "original")
	t.Cleanup(func() { stateDir, legacyStateDir, originalBackupDir = oldNew, oldLegacy, oldOriginal })
	writeTestFile(t, filepath.Join(legacyStateDir, ".original-staging-old", "manifest.txt"), "partial")
	if err := MigrateState(false); err != nil {
		t.Fatal(err)
	}
	if err := SaveOriginal(&config.Config{}, testEnv()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(originalBackupDir, "manifest.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, ".original-staging-old")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging remains: %v", err)
	}
}
