package backup

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
)

// TestMain points every system path seam at a missing temporary location so
// no test in this package reads or writes /boot, /etc, /usr or /var.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "splashchanger-backup-test-")
	if err != nil {
		panic(err)
	}
	missing := filepath.Join(root, "missing")
	legacyBackupScope = nil
	createdFiles = []string{filepath.Join(missing, "created")}
	gdmProfilePath = filepath.Join(missing, "gdm")
	grubBackgroundPNG = filepath.Join(missing, "splashchanger-background.png")
	slimConfPath = filepath.Join(missing, "slim.conf")
	stagingDir = filepath.Join(missing, "staging")
	gresourceBackupPath = filepath.Join(missing, "gresource.splashchanger-backup")
	gresourceHashPath = filepath.Join(missing, "gresource.splashchanger-sha256")
	legacyGrubBackgroundPNG = filepath.Join(missing, "legacy-background.png")
	legacyStagingDir = filepath.Join(missing, "legacy-staging")
	legacyGresourceBackupPath = filepath.Join(missing, "legacy-gresource.backup")
	legacyGresourceHashPath = filepath.Join(missing, "legacy-gresource.sha256")
	sddmThemeConfUserFn = func() string { return "" }
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

type scopePaths struct {
	root, grubConf, grubPNG, slim, staging, sddmConf, gresource, gresourceHash, plymouthTheme string
}

// isolateScope redirects every backup and cleanup path into a fresh temp dir.
func isolateScope(t *testing.T) scopePaths {
	t.Helper()
	root := t.TempDir()
	p := scopePaths{
		root:          root,
		grubConf:      filepath.Join(root, "etc/default/grub"),
		grubPNG:       filepath.Join(root, "boot/grub/splashchanger-background.png"),
		slim:          filepath.Join(root, "etc/slim.conf"),
		staging:       filepath.Join(root, "usr/share/splashchanger"),
		sddmConf:      filepath.Join(root, "usr/share/sddm/themes/breeze/theme.conf.user"),
		gresource:     filepath.Join(root, "usr/share/gnome-shell/gnome-shell-theme.gresource"),
		gresourceHash: filepath.Join(root, "usr/share/gnome-shell/gnome-shell-theme.gresource.splashchanger-sha256"),
		plymouthTheme: filepath.Join(root, "usr/share/plymouth/themes/splashchanger"),
	}
	oldFiles, oldLegacy, oldCreated, oldGDM := filesToBackup, legacyBackupScope, createdFiles, gdmProfilePath
	oldPNG, oldSlim, oldStaging := grubBackgroundPNG, slimConfPath, stagingDir
	oldGresBackup, oldGresHash, oldSDDM := gresourceBackupPath, gresourceHashPath, sddmThemeConfUserFn
	oldOriginal, oldPlymouth, oldExec := originalBackupDir, plymouthConfPath, execCommand
	t.Cleanup(func() {
		filesToBackup, legacyBackupScope, createdFiles, gdmProfilePath = oldFiles, oldLegacy, oldCreated, oldGDM
		grubBackgroundPNG, slimConfPath, stagingDir = oldPNG, oldSlim, oldStaging
		gresourceBackupPath, gresourceHashPath, sddmThemeConfUserFn = oldGresBackup, oldGresHash, oldSDDM
		originalBackupDir, plymouthConfPath, execCommand = oldOriginal, oldPlymouth, oldExec
	})
	filesToBackup = []string{p.grubConf, p.gresource}
	legacyBackupScope = slices.Clone(filesToBackup)
	createdFiles = []string{p.plymouthTheme}
	gdmProfilePath = filepath.Join(root, "etc/dconf/profile/gdm")
	grubBackgroundPNG, slimConfPath, stagingDir = p.grubPNG, p.slim, p.staging
	gresourceBackupPath = p.gresource + ".splashchanger-backup"
	gresourceHashPath = p.gresourceHash
	sddmThemeConfUserFn = func() string { return p.sddmConf }
	originalBackupDir = filepath.Join(root, "var/lib/splashchanger/original")
	plymouthConfPath = filepath.Join(root, "etc/plymouth/plymouthd.conf")
	execCommand = func(string, ...string) error { return nil }
	return p
}

func TestCleanupLegacyAndNewArtifactsKeepsSnapshotPaths(t *testing.T) {
	p := isolateScope(t)
	legacy := filepath.Join(p.root, "legacy-artifact")
	current := filepath.Join(p.root, "new-artifact")
	kept := filepath.Join(p.root, "kept-artifact")
	createdFiles = []string{legacy, current, kept}
	snapshot := filepath.Join(p.root, "original")
	for _, path := range []string{legacy, current, kept, filepath.Join(snapshot, kept)} {
		writeTestFile(t, path, "keep original")
	}
	writeTestFile(t, filepath.Join(snapshot, "manifest.txt"), "debsplash backup\n")
	cleanupSplashchangerFiles(snapshot, "restore-original", false, true)
	for _, path := range []string{legacy, current} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("created artifact remains: %s, %v", path, err)
		}
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("snapshot-present artifact removed: %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Errorf("%s = %q, %v; want %q", path, got, err, want)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s still exists (err=%v)", path, err)
	}
}

func testEnv() *detect.Environment {
	return &detect.Environment{Desktop: detect.DEKDE, LoginManager: detect.LMSDDM, HasGrub: true}
}

func TestRestoreRevertsGrubSlimAndSDDM(t *testing.T) {
	p := isolateScope(t)
	writeTestFile(t, p.grubConf, "GRUB_TIMEOUT=5\n")
	writeTestFile(t, p.grubPNG, "old-png")
	writeTestFile(t, p.slim, "background /usr/share/images/old.png\n")
	cfg := &config.Config{BackupDir: filepath.Join(p.root, "backups")}
	captureStdout(t, func() {
		if err := TakeBackup(cfg, testEnv()); err != nil {
			t.Fatal(err)
		}
	})

	// Simulate apply: overwrite the GRUB image and SLiM config, create the
	// staging dir and an SDDM theme.conf.user that did not exist before.
	staged := filepath.Join(p.staging, "login-background.png")
	writeTestFile(t, p.grubPNG, "new-png")
	writeTestFile(t, staged, "staged")
	writeTestFile(t, p.slim, "background "+staged+"\n")
	writeTestFile(t, p.sddmConf, "[General]\nbackground="+staged+"\n")

	captureStdout(t, func() {
		if err := Restore(&config.Config{BackupDir: cfg.BackupDir, DryRun: true}, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	assertContent(t, p.sddmConf, "[General]\nbackground="+staged+"\n")
	assertContent(t, staged, "staged")

	captureStdout(t, func() {
		if err := Restore(cfg, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	assertContent(t, p.grubPNG, "old-png")
	assertContent(t, p.slim, "background /usr/share/images/old.png\n")
	assertAbsent(t, p.sddmConf)
	assertAbsent(t, p.staging)
}

func TestRestoreOriginalLegacyKeepsUserSlimConf(t *testing.T) {
	p := isolateScope(t)
	// Legacy original: written before scope records, holding only grub.
	writeTestFile(t, filepath.Join(originalBackupDir, p.grubConf), "GRUB_TIMEOUT=5\n")
	writeTestFile(t, filepath.Join(originalBackupDir, "manifest.txt"),
		"splashchanger backup\nTimestamp:   2025-01-01_00-00-00\n\nFiles backed up:\n  "+p.grubConf+"\n")

	staged := filepath.Join(p.staging, "login-background.png")
	userSlim := "# admin config\nbackground " + staged + "\n"
	userSDDM := "[General]\nbackground=/home/admin/wallpaper.png\n"
	writeTestFile(t, p.slim, userSlim)
	writeTestFile(t, p.sddmConf, userSDDM)
	writeTestFile(t, staged, "staged")
	writeTestFile(t, p.grubPNG, "tool-png")

	out := captureStdout(t, func() {
		if err := RestoreOriginal(&config.Config{}, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	assertContent(t, p.slim, userSlim)
	assertContent(t, p.sddmConf, userSDDM)
	// slim.conf still points into the staging dir, so it must survive.
	assertContent(t, staged, "staged")
	if !strings.Contains(out, "still referenced") {
		t.Errorf("expected a warning about the kept staging dir, got:\n%s", out)
	}
	assertAbsent(t, p.grubPNG)
}

func TestRestoreOriginalLegacyRemovesMarkedThemeConf(t *testing.T) {
	p := isolateScope(t)
	writeTestFile(t, filepath.Join(originalBackupDir, "manifest.txt"), "splashchanger backup\n")
	// The marker check matches only the exact file applySDDM writes for the
	// real staging dir; it reads the content, never that directory.
	marked := "[General]\nbackground=/usr/share/splashchanger/login-background.png\n"
	writeTestFile(t, p.sddmConf, marked)
	captureStdout(t, func() {
		if err := RestoreOriginal(&config.Config{}, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	assertAbsent(t, p.sddmConf)
}

func TestSaveOriginalRecordsScopeAndRestoreRemovesCreatedThemeConf(t *testing.T) {
	p := isolateScope(t)
	writeTestFile(t, p.grubConf, "GRUB_TIMEOUT=5\n")
	captureStdout(t, func() {
		if err := SaveOriginal(&config.Config{}, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	manifest, err := os.ReadFile(filepath.Join(originalBackupDir, "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{p.grubConf, p.grubPNG, p.slim, p.staging, p.sddmConf, p.gresourceHash} {
		if !strings.Contains(string(manifest), "Backup scope:") || !strings.Contains(string(manifest), "  "+want+"\n") {
			t.Fatalf("original manifest does not record scope path %s:\n%s", want, manifest)
		}
	}

	// Tool writes a theme.conf.user that the scoped original shows was absent.
	// Content deliberately lacks the marker: scope alone must decide.
	writeTestFile(t, p.sddmConf, "[General]\nbackground=/some/other.png\n")
	captureStdout(t, func() {
		if err := RestoreOriginal(&config.Config{}, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	assertAbsent(t, p.sddmConf)
}

func TestRestoreKeepsGresourceChecksumConsistent(t *testing.T) {
	p := isolateScope(t)
	writeTestFile(t, p.gresource, "patched-v1")
	writeTestFile(t, p.gresourceHash, "hash-of-patched-v1")
	cfg := &config.Config{BackupDir: filepath.Join(p.root, "backups")}
	captureStdout(t, func() {
		if err := TakeBackup(cfg, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	writeTestFile(t, p.gresource, "patched-v2")
	writeTestFile(t, p.gresourceHash, "hash-of-patched-v2")
	captureStdout(t, func() {
		if err := Restore(cfg, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	assertContent(t, p.gresource, "patched-v1")
	assertContent(t, p.gresourceHash, "hash-of-patched-v1")
}

func TestRestoreLegacyBackupDropsStaleGresourceChecksum(t *testing.T) {
	p := isolateScope(t)
	backupDir := filepath.Join(p.root, "backups")
	snapshot := filepath.Join(backupDir, "2025-01-01_00-00-00")
	writeTestFile(t, filepath.Join(snapshot, p.gresource), "patched-v1")
	writeTestFile(t, filepath.Join(snapshot, "manifest.txt"), "splashchanger backup\n")
	writeTestFile(t, p.gresource, "patched-v2")
	writeTestFile(t, p.gresourceHash, "hash-of-patched-v2")
	captureStdout(t, func() {
		if err := Restore(&config.Config{BackupDir: backupDir}, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	assertContent(t, p.gresource, "patched-v1")
	assertAbsent(t, p.gresourceHash)
}

// A path whose copy failed must not be recorded in the backup scope, or a
// later restore reads it as "absent when backed up" and deletes the live file.
func TestRestoreKeepsLiveFileWhenBackupCopyFailed(t *testing.T) {
	p := isolateScope(t)
	gdm := gdmProfilePath
	filesToBackup = append(filesToBackup, gdm)
	writeTestFile(t, p.grubConf, "GRUB_TIMEOUT=5\n")
	writeTestFile(t, gdm, "unreadable")
	if err := os.Chmod(gdm, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(gdm, 0600) })
	if _, err := os.ReadFile(gdm); err == nil {
		t.Skip("0000 file is readable (running as root)")
	}
	cfg := &config.Config{BackupDir: filepath.Join(p.root, "backups")}
	captureStdout(t, func() {
		if err := TakeBackup(cfg, testEnv()); err == nil {
			t.Fatal("expected copy failure")
		}
	})
	captureStdout(t, func() {
		if err := Restore(cfg, testEnv()); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Lstat(gdm); err != nil {
		t.Fatalf("live file removed by restore: %v", err)
	}
}
