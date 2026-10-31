package backup

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
)

// captureStdout captures stdout output during fn execution.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestTakeBackup_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")

	cfg := &config.Config{
		BackupDir: backupDir,
		DryRun:    true,
	}
	env := &detect.Environment{
		Desktop:      detect.DEGNOME,
		LoginManager: detect.LMGDM,
		HasPlymouth:  true,
		HasGrub:      true,
	}

	output := captureStdout(t, func() {
		err := TakeBackup(cfg, env)
		if err != nil {
			t.Fatalf("TakeBackup dry-run failed: %v", err)
		}
	})

	// Backup directory should NOT have been created (dry-run).
	if _, err := os.Stat(backupDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected backup dir not to exist in dry-run mode, but it does")
	}

	// Output should contain "Would back up" for any files that exist on the system.
	// In a test environment, most files won't exist, but we verify no actual copies happen.
	if strings.Contains(output, "Warning: could not create") {
		t.Errorf("dry-run should not attempt to create directories, got: %s", output)
	}
}

func TestRestoreFilesKeepsNestedManifest(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "tmp", "manifest.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("nested"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.txt"), []byte("top"), 0600); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if _, _, _, err := (&backupSet{dir: root, label: "restore", dryRun: true}).restoreFiles(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, nested) || strings.Contains(output, filepath.Join(root, "manifest.txt")+" to") {
		t.Fatalf("manifest restore output = %q", output)
	}
}

func TestTakeBackupUsesSeparateDirectories(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	oldFiles, oldGrub, oldSlim, oldStage, oldHash, oldSDDM, oldNow := filesToBackup, grubBackgroundPNG, slimConfPath, stagingDir, gresourceHashPath, sddmThemeConfUserFn, backupNow
	filesToBackup, grubBackgroundPNG, slimConfPath, stagingDir, gresourceHashPath = []string{source}, "", "", "", ""
	sddmThemeConfUserFn = func() string { return "" }
	backupNow = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() {
		filesToBackup, grubBackgroundPNG, slimConfPath, stagingDir, gresourceHashPath, sddmThemeConfUserFn = oldFiles, oldGrub, oldSlim, oldStage, oldHash, oldSDDM
		backupNow = oldNow
	})
	cfg := &config.Config{BackupDir: filepath.Join(root, "backups")}
	for range 2 {
		if err := TakeBackup(cfg, &detect.Environment{}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(cfg.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() != "2026-10-31_12-00-00.000000000" || entries[1].Name() != "2026-10-31_12-00-00.000000000-1" {
		t.Fatalf("backup directories = %v", entries)
	}
}

func TestSaveOriginalPreservesFirstSnapshot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	oldDir, oldFiles := originalBackupDir, filesToBackup
	originalBackupDir, filesToBackup = filepath.Join(root, "original"), []string{source}
	t.Cleanup(func() { originalBackupDir = oldDir; filesToBackup = oldFiles })
	_ = os.WriteFile(source, []byte("first"), 0600)
	env := &detect.Environment{}
	if err := SaveOriginal(&config.Config{}, env); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(originalBackupDir, source))
	if err != nil {
		t.Fatal(err)
	}
	firstManifest, err := os.ReadFile(filepath.Join(originalBackupDir, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(source, []byte("second"), 0600)
	if err := SaveOriginal(&config.Config{}, env); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(originalBackupDir, source))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("original changed: %q to %q", first, second)
	}
	secondManifest, err := os.ReadFile(filepath.Join(originalBackupDir, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstManifest, secondManifest) {
		t.Fatal("original manifest changed")
	}
}

func TestSaveOriginalInterruptedCopy(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good")
	bad := filepath.Join(root, "bad")
	oldDir, oldFiles := originalBackupDir, filesToBackup
	originalBackupDir, filesToBackup = filepath.Join(root, "original"), []string{good, bad}
	t.Cleanup(func() { originalBackupDir = oldDir; filesToBackup = oldFiles })
	if err := os.WriteFile(good, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", bad)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	env := &detect.Environment{}
	if err := SaveOriginal(&config.Config{}, env); err == nil {
		t.Fatal("copy failure was ignored")
	}
	stale := filepath.Join(root, ".original-staging-interrupted")
	if err := os.Mkdir(stale, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(originalBackupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial original published: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("complete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveOriginal(&config.Config{}, env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale staging directory remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(originalBackupDir, bad)); err != nil {
		t.Fatal(err)
	}
}

func TestSaveOriginalDryRunCreatesNothing(t *testing.T) {
	root := t.TempDir()
	old := originalBackupDir
	originalBackupDir = filepath.Join(root, "parent", "original")
	t.Cleanup(func() { originalBackupDir = old })
	if err := SaveOriginal(&config.Config{DryRun: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(originalBackupDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created original parent: %v", err)
	}
}

func TestSaveOriginalRejectsLegacyPartial(t *testing.T) {
	root := t.TempDir()
	old := originalBackupDir
	originalBackupDir = filepath.Join(root, "original")
	t.Cleanup(func() { originalBackupDir = old })
	if err := os.Mkdir(originalBackupDir, 0700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(originalBackupDir, "partial")
	if err := os.WriteFile(partial, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveOriginal(&config.Config{}, &detect.Environment{}); err == nil {
		t.Fatal("partial original accepted")
	}
	data, err := os.ReadFile(partial)
	if err != nil || string(data) != "keep" {
		t.Fatalf("partial original changed: %q, %v", data, err)
	}
}

func TestTakeBackupDryRunSummary(t *testing.T) {
	oldFiles := filesToBackup
	file := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	filesToBackup = []string{file}
	t.Cleanup(func() { filesToBackup = oldFiles })
	output := captureStdout(t, func() {
		if err := TakeBackup(&config.Config{BackupDir: filepath.Join(t.TempDir(), "backups"), DryRun: true}, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "[backup] Would back up 1 item(s)") || strings.Contains(output, "[backup] Backed up") {
		t.Fatalf("misleading dry-run summary: %q", output)
	}
}

func TestTakeBackupReturnsCopyFailures(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good")
	bad := filepath.Join(root, "bad")
	badToo := filepath.Join(root, "bad-too")
	missing := filepath.Join(root, "missing")
	if err := os.WriteFile(good, []byte("saved"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", bad)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	secondListener, err := net.Listen("unix", badToo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { secondListener.Close() })
	oldFiles := filesToBackup
	filesToBackup = []string{good, bad, badToo, missing}
	t.Cleanup(func() { filesToBackup = oldFiles })
	backupDir := filepath.Join(root, "backups")
	err = TakeBackup(&config.Config{BackupDir: backupDir}, &detect.Environment{})
	if err == nil || !strings.HasPrefix(err.Error(), "copy "+bad+":") || !strings.Contains(err.Error(), "\ncopy "+badToo+":") || strings.Contains(err.Error(), missing) {
		t.Fatalf("expected both failed existing copies only, got %v", err)
	}
	entries, readErr := os.ReadDir(backupDir)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("backup directory: %v, %v", entries, readErr)
	}
	if data, readErr := os.ReadFile(filepath.Join(backupDir, entries[0].Name(), good)); readErr != nil || string(data) != "saved" {
		t.Fatalf("good file copy: %q, %v", data, readErr)
	}
}

func TestPruneOldBackups(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 12 mock timestamped backup directories.
	for i := range 12 {
		dir := filepath.Join(tmpDir, fmt.Sprintf("2025-01-%02d_12-00-00", i+1))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}

	output := captureStdout(t, func() {
		pruneOldBackups(tmpDir, 10)
	})

	// Should have pruned 2 oldest.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	dirCount := 0
	for _, e := range entries {
		if e.IsDir() {
			dirCount++
		}
	}
	if dirCount != 10 {
		t.Errorf("expected 10 backups after prune, got %d", dirCount)
	}

	// The two oldest should be gone.
	if _, err := os.Stat(filepath.Join(tmpDir, "2025-01-01_12-00-00")); !errors.Is(err, os.ErrNotExist) {
		t.Error("expected oldest backup to be pruned")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "2025-01-02_12-00-00")); !errors.Is(err, os.ErrNotExist) {
		t.Error("expected second-oldest backup to be pruned")
	}

	// The third should still exist.
	if _, err := os.Stat(filepath.Join(tmpDir, "2025-01-03_12-00-00")); err != nil {
		t.Error("expected third-oldest backup to still exist")
	}

	if !strings.Contains(output, "Pruned old backup") {
		t.Error("expected prune output message")
	}
}

func TestLatestBackup(t *testing.T) {
	tmpDir := t.TempDir()

	dirs := []string{
		"2025-01-01_12-00-00",
		"2025-01-15_08-30-00",
		"2025-02-01_09-00-00",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, d), 0700); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := latestBackup(tmpDir)
	if err != nil {
		t.Fatalf("latestBackup failed: %v", err)
	}

	expected := filepath.Join(tmpDir, "2025-02-01_09-00-00")
	if latest != expected {
		t.Errorf("expected latest=%s, got %s", expected, latest)
	}
}

func TestLatestBackup_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := latestBackup(tmpDir)
	if err == nil {
		t.Error("expected error for empty backup dir")
	}
}

func TestFilesToBackup_NoGrubCfg(t *testing.T) {
	for _, f := range filesToBackup {
		if f == "/boot/grub/grub.cfg" {
			t.Error("/boot/grub/grub.cfg should NOT be in filesToBackup")
		}
	}
}

func TestFilesToBackup_HasGDMDconfPaths(t *testing.T) {
	found := map[string]bool{
		"/etc/dconf/profile/gdm":               false,
		"/etc/dconf/db/gdm.d/00-splashchanger": false,
	}
	for _, f := range filesToBackup {
		if _, ok := found[f]; ok {
			found[f] = true
		}
	}
	for path, present := range found {
		if !present {
			t.Errorf("expected %s to be in filesToBackup", path)
		}
	}
}

func TestFilesToBackup_HasPlymouthThemeDir(t *testing.T) {
	found := slices.Contains(filesToBackup, "/usr/share/plymouth/themes/splashchanger")
	if !found {
		t.Error("expected /usr/share/plymouth/themes/splashchanger in filesToBackup")
	}
}

func TestCleanupSplashchangerFilesPreservesOriginalSnapshot(t *testing.T) {
	root := t.TempDir()
	originalDir := filepath.Join(root, "original")
	preserved := filepath.Join(root, "profile", "gdm")
	created := filepath.Join(root, "created")
	for _, path := range []string{preserved, created} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("admin config"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := filepath.Join(originalDir, preserved)
	if err := os.MkdirAll(filepath.Dir(snapshot), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, []byte("admin config"), 0600); err != nil {
		t.Fatal(err)
	}

	oldGDM, oldCreated := gdmProfilePath, createdFiles
	gdmProfilePath, createdFiles = preserved, []string{created}
	t.Cleanup(func() { gdmProfilePath, createdFiles = oldGDM, oldCreated })

	captureStdout(t, func() {
		cleanupSplashchangerFiles(originalDir, "restore-original", false, true)
	})
	if _, err := os.Stat(preserved); err != nil {
		t.Fatalf("original file removed despite snapshot: %v", err)
	}
	if _, err := os.Stat(created); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("splashchanger-created file remains: %v", err)
	}
}

func TestRestore_ManifestMismatchWarning(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a backup directory with a manifest showing "gdm".
	backupName := "2025-03-01_12-00-00"
	backupPath := filepath.Join(tmpDir, backupName)
	if err := os.MkdirAll(backupPath, 0700); err != nil {
		t.Fatal(err)
	}

	manifest := filepath.Join(backupPath, "manifest.txt")
	content := "splashchanger backup\nTimestamp:   2025-03-01_12-00-00\nDesktop:     gnome\nLoginMgr:    gdm\nPlymouth:    true\nGRUB:        true\n"
	if err := os.WriteFile(manifest, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a dummy file so the walk has something.
	dummyDir := filepath.Join(backupPath, "etc", "lightdm")
	if err := os.MkdirAll(dummyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dummyDir, "lightdm.conf"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		BackupDir: tmpDir,
		DryRun:    true, // dry-run so we don't actually write to /etc
	}
	env := &detect.Environment{
		Desktop:      detect.DEGNOME,
		LoginManager: detect.LMLightDM, // different from manifest "gdm"
		HasPlymouth:  true,
		HasGrub:      true,
	}

	output := captureStdout(t, func() {
		err := Restore(cfg, env)
		if err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
	})

	if !strings.Contains(output, "backup was taken with gdm but current login manager is lightdm") {
		t.Errorf("expected manifest mismatch warning, got:\n%s", output)
	}
}

func TestRunPostRestoreCommands(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "plymouthd.conf")
	if err := os.WriteFile(conf, []byte("[Daemon]\nTheme=breeze\n"), 0644); err != nil {
		t.Fatal(err)
	}
	oldConfPath := plymouthConfPath
	plymouthConfPath = conf
	defer func() { plymouthConfPath = oldConfPath }()

	// Track which commands were called.
	var calledCommands []string
	origExecCommand := execCommand
	execCommand = func(name string, args ...string) error {
		full := name
		if len(args) > 0 {
			full += " " + strings.Join(args, " ")
		}
		calledCommands = append(calledCommands, full)
		return nil
	}
	defer func() { execCommand = origExecCommand }()

	// Test with all three types restored.
	output := captureStdout(t, func() {
		runPostRestoreCommands(false, true, true, true)
	})

	expectedCmds := []string{
		"update-grub",
		"update-initramfs -u",
		"plymouth-set-default-theme breeze",
		"dconf update",
	}
	for _, exp := range expectedCmds {
		found := slices.Contains(calledCommands, exp)
		if !found {
			t.Errorf("expected post-restore command %q to be called, called: %v", exp, calledCommands)
		}
	}

	for _, exp := range expectedCmds {
		if !strings.Contains(output, "Running: "+exp) {
			t.Errorf("expected output to contain 'Running: %s', got:\n%s", exp, output)
		}
	}
}

func TestRunPostRestoreCommands_OnlyGrub(t *testing.T) {
	var calledCommands []string
	origExecCommand := execCommand
	execCommand = func(name string, args ...string) error {
		full := name
		if len(args) > 0 {
			full += " " + strings.Join(args, " ")
		}
		calledCommands = append(calledCommands, full)
		return nil
	}
	defer func() { execCommand = origExecCommand }()

	captureStdout(t, func() {
		runPostRestoreCommands(false, true, false, false)
	})

	if len(calledCommands) != 1 || calledCommands[0] != "update-grub" {
		t.Errorf("expected only update-grub, got: %v", calledCommands)
	}
}

func TestRunPostRestoreCommands_DryRun(t *testing.T) {
	origExecCommand := execCommand
	execCommand = func(name string, args ...string) error {
		t.Errorf("execCommand should not be called in dry-run mode, got: %s %v", name, args)
		return nil
	}
	defer func() { execCommand = origExecCommand }()

	output := captureStdout(t, func() {
		runPostRestoreCommands(true, true, true, true)
	})

	if !strings.Contains(output, "Would run: update-grub") {
		t.Errorf("expected 'Would run: update-grub' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Would run: dconf update") {
		t.Errorf("expected 'Would run: dconf update' in output, got:\n%s", output)
	}
}

func TestRestore_DryRun_PostRestoreCommands(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a backup with a grub file.
	backupName := "2025-03-01_12-00-00"
	backupPath := filepath.Join(tmpDir, backupName)
	grubDir := filepath.Join(backupPath, "etc", "default")
	if err := os.MkdirAll(grubDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grubDir, "grub"), []byte("GRUB_TIMEOUT=5"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "manifest.txt"), []byte("splashchanger backup\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Ensure execCommand is NOT called in dry-run.
	origExecCommand := execCommand
	execCommand = func(name string, args ...string) error {
		t.Errorf("execCommand should not be called in dry-run mode, got: %s %v", name, args)
		return nil
	}
	defer func() { execCommand = origExecCommand }()

	cfg := &config.Config{
		BackupDir: tmpDir,
		DryRun:    true,
	}
	env := &detect.Environment{
		Desktop:      detect.DEGNOME,
		LoginManager: detect.LMGDM,
	}

	output := captureStdout(t, func() {
		err := Restore(cfg, env)
		if err != nil {
			t.Fatalf("Restore dry-run failed: %v", err)
		}
	})

	if !strings.Contains(output, "Would run: update-grub") {
		t.Errorf("expected dry-run output to contain 'Would run: update-grub', got:\n%s", output)
	}
	if !strings.Contains(output, "Would restore") {
		t.Errorf("expected dry-run output to contain 'Would restore', got:\n%s", output)
	}
	if !strings.Contains(output, "Dry run complete. No files restored.") || strings.Contains(output, "Restore complete.") {
		t.Errorf("misleading dry-run restore summary: %q", output)
	}
}

func TestRestore_DryRun_RestoredPlymouthTheme(t *testing.T) {
	backupDir := t.TempDir()
	conf := filepath.Join(backupDir, "2025-03-01_12-00-00", "etc", "plymouth", "plymouthd.conf")
	if err := os.MkdirAll(filepath.Dir(conf), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte("[Daemon]\nTheme=breeze\n"), 0644); err != nil {
		t.Fatal(err)
	}
	oldConfPath := plymouthConfPath
	plymouthConfPath = conf
	defer func() { plymouthConfPath = oldConfPath }()

	output := captureStdout(t, func() {
		if err := Restore(&config.Config{BackupDir: backupDir, DryRun: true}, &detect.Environment{}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "Would run: plymouth-set-default-theme breeze\n  Would run: update-initramfs -u") {
		t.Fatalf("restore did not select backed-up theme before initramfs update:\n%s", output)
	}
}
