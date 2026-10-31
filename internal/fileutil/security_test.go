package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

// F-01 / H-01: setuid, setgid and sticky bits on a source must never reach
// the destination, which root creates.
func TestCopyFile_StripsSpecialModeBits(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "dest")
	if err := os.WriteFile(src, []byte("payload"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0755|os.ModeSetuid|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(src); info.Mode()&os.ModeSetuid == 0 {
		t.Skip("filesystem does not keep setuid on test file")
	}

	if err := CopyFile(src, dest); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if special := info.Mode() & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky); special != 0 {
		t.Errorf("dest mode = %v, special bits %v propagated", info.Mode(), special)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("dest perm = %o, want 0755", info.Mode().Perm())
	}
}

// S-02: a pre-planted "<path>.tmp" symlink must not redirect the write.
func TestWriteFileAtomic_DoesNotFollowPlantedTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.png")
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Fatal(err)
	}

	if err := WriteFileAtomic(path, []byte("new data"), 0644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	if got, _ := os.ReadFile(victim); string(got) != "original" {
		t.Errorf("victim overwritten through planted temp symlink: %q", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "new data" {
		t.Errorf("dest = %q, want %q", got, "new data")
	}
}

// Same planted-temp check for CopyFile, which used the same fixed name.
func TestCopyFile_DoesNotFollowPlantedTemp(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "dest")
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(src, []byte("new data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, dest+".tmp"); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dest); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "original" {
		t.Errorf("victim overwritten through planted temp symlink: %q", got)
	}
}
