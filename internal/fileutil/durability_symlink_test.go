package fileutil

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestWritersSyncFileBeforeRenameAndDirectoryAfter(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fallback bool
		write    func(string, string) error
	}{
		{"WriteFileAtomic", false, func(_, dest string) error { return WriteFileAtomic(dest, []byte("new"), 0644) }},
		{"CopyFile", false, CopyFile},
		{"WriteFileAtomic_EXDEV", true, func(_, dest string) error { return WriteFileAtomic(dest, []byte("new"), 0644) }},
		{"CopyFile_EXDEV", true, CopyFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src, dest := filepath.Join(dir, "src"), filepath.Join(dir, "dest")
			if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
				t.Fatal(err)
			}
			originalSync, originalRename := syncFile, osRename
			t.Cleanup(func() { syncFile, osRename = originalSync, originalRename })
			var events []string
			syncFile = func(f *os.File) error {
				if f.Name() == dir {
					events = append(events, "dir sync")
				} else if f.Name() == dest {
					events = append(events, "dest sync")
				} else {
					events = append(events, "temp sync")
				}
				return originalSync(f)
			}
			osRename = func(oldpath, newpath string) error {
				events = append(events, "rename")
				if tc.fallback {
					return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
				}
				return originalRename(oldpath, newpath)
			}
			if err := tc.write(src, dest); err != nil {
				t.Fatal(err)
			}
			want := []string{"temp sync", "rename", "dir sync"}
			if tc.fallback {
				want = []string{"temp sync", "rename", "dest sync", "dir sync"}
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("sync sequence = %v, want %v", events, want)
			}
		})
	}
}

func TestCopyDirPreservesSymlinks(t *testing.T) {
	src, dest := t.TempDir(), filepath.Join(t.TempDir(), "dest")
	if err := os.WriteFile(filepath.Join(src, "file"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"relative": "file", "dangling": "missing"} {
		if err := os.Symlink(target, filepath.Join(src, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := CopyDir(src, dest); err != nil {
		t.Fatalf("CopyDir: %v", err)
	}
	for name, want := range map[string]string{"relative": "file", "dangling": "missing"} {
		path := filepath.Join(dest, name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s mode = %v, want symlink", name, info.Mode())
		}
		got, err := os.Readlink(path)
		if err != nil || got != want {
			t.Errorf("%s target = %q, %v; want %q", name, got, err, want)
		}
	}
}
