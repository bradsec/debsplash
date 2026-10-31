//go:build integration

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bradsec/debsplash/internal/backup"
	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/bradsec/debsplash/internal/fileutil"
)

// setupTestFiles creates a temporary directory structure with a backup dir,
// a default config pointing at it, and a mock environment.
func setupTestFiles(t *testing.T) (string, *config.Config, *detect.Environment) {
	t.Helper()
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.BackupDir = backupDir
	env := &detect.Environment{
		Desktop:      detect.DEGNOME,
		LoginManager: detect.LMGDM,
		HasPlymouth:  true,
		HasGrub:      true,
	}
	return tmpDir, cfg, env
}

// TestDryRunApplyFlow verifies that DryRun mode prevents file modifications.
// In dry-run mode, TakeBackup should not create timestamped backup subdirectories.
func TestDryRunApplyFlow(t *testing.T) {
	_, cfg, env := setupTestFiles(t)
	cfg.DryRun = true

	// Remember the backup base dir before the call.
	backupDir := cfg.BackupDir

	if err := backup.TakeBackup(cfg, env); err != nil {
		t.Fatalf("TakeBackup in dry-run mode failed: %v", err)
	}

	// The base backup dir should exist (we created it), but no timestamped
	// subdirectories should have been created.
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("failed to read backup dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("dry-run created unexpected backup subdirectory: %s", e.Name())
		}
	}
}

// TestBackupRestoreRoundTrip creates temp files, backs them up using fileutil,
// modifies the originals, restores from backup, and verifies the originals
// match the backed-up versions. This exercises the same copy mechanisms used
// by the backup and restore code paths.
func TestBackupRestoreRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	// Create "original" files with known content.
	origDir := filepath.Join(tmpDir, "originals")
	backupDir := filepath.Join(tmpDir, "backup")
	files := map[string]string{
		"etc/default/grub":            "GRUB_TIMEOUT=5\nGRUB_BACKGROUND=/boot/grub/bg.png\n",
		"etc/plymouth/plymouthd.conf": "[Daemon]\nTheme=splashchanger\n",
		"etc/lightdm/lightdm.conf":    "[Seat:*]\ngreeter-session=lightdm-gtk-greeter\n",
	}

	// Write originals.
	for relPath, content := range files {
		fullPath := filepath.Join(origDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("failed to create dir for %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", relPath, err)
		}
	}

	// Back up each file using fileutil.CopyFile (same function backup.TakeBackup uses).
	for relPath := range files {
		src := filepath.Join(origDir, relPath)
		dest := filepath.Join(backupDir, relPath)
		if err := fileutil.CopyFile(src, dest); err != nil {
			t.Fatalf("failed to back up %s: %v", relPath, err)
		}
	}

	// Verify backup files exist and match originals.
	for relPath, content := range files {
		backedUp := filepath.Join(backupDir, relPath)
		got, err := os.ReadFile(backedUp)
		if err != nil {
			t.Fatalf("backup file %s not found: %v", relPath, err)
		}
		if string(got) != content {
			t.Errorf("backup content mismatch for %s:\n  want: %q\n  got:  %q", relPath, content, string(got))
		}
	}

	// Modify the originals.
	modifiedContent := map[string]string{
		"etc/default/grub":            "GRUB_TIMEOUT=10\nGRUB_BACKGROUND=/boot/grub/new.png\n",
		"etc/plymouth/plymouthd.conf": "[Daemon]\nTheme=default\n",
		"etc/lightdm/lightdm.conf":    "[Seat:*]\ngreeter-session=slick-greeter\n",
	}
	for relPath, content := range modifiedContent {
		fullPath := filepath.Join(origDir, relPath)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to modify %s: %v", relPath, err)
		}
	}

	// Verify originals are actually modified.
	for relPath, expected := range modifiedContent {
		fullPath := filepath.Join(origDir, relPath)
		got, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("failed to read modified %s: %v", relPath, err)
		}
		if string(got) != expected {
			t.Errorf("modified file mismatch for %s", relPath)
		}
	}

	// Restore: copy backup files back to originals.
	for relPath := range files {
		src := filepath.Join(backupDir, relPath)
		dest := filepath.Join(origDir, relPath)
		if err := fileutil.CopyFile(src, dest); err != nil {
			t.Fatalf("failed to restore %s: %v", relPath, err)
		}
	}

	// Verify restored files match the original content (before modification).
	for relPath, expected := range files {
		fullPath := filepath.Join(origDir, relPath)
		got, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("restored file %s not found: %v", relPath, err)
		}
		if string(got) != expected {
			t.Errorf("restored content mismatch for %s:\n  want: %q\n  got:  %q", relPath, expected, string(got))
		}
	}
}
