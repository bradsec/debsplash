package fileutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// osRename is an overridable package-level var so tests can inject EXDEV failures.
var osRename = os.Rename
var syncFile = (*os.File).Sync

// renameOrCopy tries osRename first; if it fails with EXDEV (cross-device link),
// it falls back to copying the file contents directly and removing the source.
func renameOrCopy(tmp, dest string) error {
	err := osRename(tmp, dest)
	if err == nil {
		return nil
	}

	if linkErr, ok := errors.AsType[*os.LinkError](err); ok && errors.Is(linkErr.Err, syscall.EXDEV) {
		// Cross-filesystem: copy contents then remove tmp.
		return copyAndRemove(tmp, dest)
	}
	return err
}

// copyAndRemove copies src to dest (overwriting) and removes src.
func copyAndRemove(src, dest string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := syncFile(out); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	return os.Remove(src)
}

func syncParent(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return syncFile(dir)
}

// CopyFile copies src to dest using streaming I/O, creating parent directories
// as needed. It writes to a temp file first, then renames for atomicity.
func CopyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("could not create parent directory: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	// Take the mode from the opened file, not a separate path lookup, and
	// keep only permission bits: setuid, setgid and sticky never propagate.
	srcInfo, err := in.Stat()
	if err != nil {
		return err
	}

	// Write to a fresh temp file in the same directory, then rename
	// atomically. CreateTemp uses O_EXCL with a random name, so a
	// pre-planted file or symlink cannot redirect the write.
	out, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if err := out.Chmod(srcInfo.Mode().Perm()); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := syncFile(out); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	if err := renameOrCopy(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncParent(dest)
}

// WriteFileAtomic writes data to path atomically by writing to a temp file
// first, then renaming. This prevents partial writes from corrupting config files.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("could not create parent directory: %w", err)
	}

	// CreateTemp uses O_EXCL with a random name, so a pre-planted file or
	// symlink at a predictable temp path cannot redirect the write.
	out, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if _, err := out.Write(data); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Chmod(perm); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := syncFile(out); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := renameOrCopy(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncParent(path)
}

// CopyDir recursively copies the directory tree rooted at src to dest,
// creating directories and copying files as needed.
func CopyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Compute the destination path relative to src.
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}

		return CopyFile(path, target)
	})
}
