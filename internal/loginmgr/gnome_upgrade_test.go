package loginmgr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGDMShellTheme_RefreshesBackupAfterPackageUpgrade(t *testing.T) {
	if _, err := exec.LookPath("glib-compile-resources"); err != nil {
		t.Skip("glib-compile-resources not installed; needed to build the gresource fixture")
	}
	dir := t.TempDir()
	oldPath := gnomeShellGresourcePath
	oldTempDir := gdmTempDir
	gnomeShellGresourcePath = filepath.Join(dir, "gnome-shell-theme.gresource")
	gdmTempDir = dir
	t.Cleanup(func() {
		gnomeShellGresourcePath = oldPath
		gdmTempDir = oldTempDir
	})

	compile := func(version string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "gnome-shell.css"), []byte("/* "+version+" */\n"), 0644); err != nil {
			t.Fatal(err)
		}
		xml := `<gresources><gresource prefix="/org/gnome/shell/theme"><file>gnome-shell.css</file></gresource></gresources>`
		xmlPath := filepath.Join(dir, "theme.xml")
		if err := os.WriteFile(xmlPath, []byte(xml), 0644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("glib-compile-resources", "--sourcedir", dir, "--target", gnomeShellGresourcePath, xmlPath).CombinedOutput(); err != nil {
			t.Fatalf("compile fixture: %v: %s", err, out)
		}
	}

	compile("version-one")
	if err := applyGDMShellTheme("/tmp/background.png"); err != nil {
		t.Fatal(err)
	}
	compile("version-two") // Simulate the package replacing the installed file.
	if err := applyGDMShellTheme("/tmp/background.png"); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("gresource", "extract", gnomeShellGresourcePath, "/org/gnome/shell/theme/gnome-shell.css").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "version-two") {
		t.Fatalf("installed theme kept old package CSS: %q", out)
	}
}
