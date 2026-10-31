package fileutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dest := filepath.Join(dir, "dest.txt")

	content := []byte("hello world")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dest); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCopyFile_PermissionsPreserved(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.sh")
	dest := filepath.Join(dir, "dest.sh")

	if err := os.WriteFile(src, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dest); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("perm = %o, want 0755", info.Mode().Perm())
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.txt")
	content := []byte("atomic content")

	if err := WriteFileAtomic(path, content, 0600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestCopyFile_EXDEVFallback(t *testing.T) {
	// Save original and restore after test.
	origRename := osRename
	t.Cleanup(func() { osRename = origRename })

	// Inject a mock that always returns EXDEV.
	osRename = func(oldpath, newpath string) error {
		return &os.LinkError{
			Op:  "rename",
			Old: oldpath,
			New: newpath,
			Err: syscall.EXDEV,
		}
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dest := filepath.Join(dir, "sub", "dest.txt")

	content := []byte("cross-device content")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dest); err != nil {
		t.Fatalf("CopyFile with EXDEV: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	// Temp file should have been cleaned up.
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".dest.txt.tmp-*")); len(leftovers) != 0 {
		t.Errorf("tmp file should have been removed after EXDEV fallback: %v", leftovers)
	}
}

func TestWriteFileAtomic_EXDEVFallback(t *testing.T) {
	origRename := osRename
	t.Cleanup(func() { osRename = origRename })

	osRename = func(oldpath, newpath string) error {
		return &os.LinkError{
			Op:  "rename",
			Old: oldpath,
			New: newpath,
			Err: syscall.EXDEV,
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "atomic-exdev.txt")
	content := []byte("exdev atomic")

	if err := WriteFileAtomic(path, content, 0644); err != nil {
		t.Fatalf("WriteFileAtomic with EXDEV: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dest := filepath.Join(t.TempDir(), "dest")

	// Create a small directory tree.
	subdir := filepath.Join(src, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "b.txt"), []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(src, dest); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}

	// Verify files exist with correct content.
	for _, tc := range []struct {
		rel     string
		content string
	}{
		{"a.txt", "aaa"},
		{"sub/b.txt", "bbb"},
	} {
		got, err := os.ReadFile(filepath.Join(dest, tc.rel))
		if err != nil {
			t.Errorf("reading %s: %v", tc.rel, err)
			continue
		}
		if string(got) != tc.content {
			t.Errorf("%s: content = %q, want %q", tc.rel, got, tc.content)
		}
	}
}
