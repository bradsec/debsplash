package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
)

func TestDetectAndBackupAbortsOnOriginalFailure(t *testing.T) {
	old := saveOriginal
	saveOriginal = func(*config.Config, *detect.Environment) error { return errors.New("snapshot failed") }
	t.Cleanup(func() { saveOriginal = old })
	backupDir := filepath.Join(t.TempDir(), "backups")
	_, err := detectAndBackup(&config.Config{BackupDir: backupDir}, backupPolicy{})
	if err == nil || !strings.Contains(err.Error(), "snapshot failed") {
		t.Fatalf("original failure ignored: %v", err)
	}
	if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup started after original failure: %v", err)
	}
}
