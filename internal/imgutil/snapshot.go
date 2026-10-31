package imgutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// snapshotMode is the mode of the private copy. Targets that copy the image
// (Plymouth theme, login staging) inherit it, so it is fixed rather than taken
// from the caller's file; 0644 lets login greeters running as their own user
// read the staged image. The 0700 snapshot directory keeps the copy private.
const snapshotMode = 0644

// Snapshot is a private copy of a validated image. Every later step reads
// only Path, so replacing or relinking the original after validation cannot
// change what is installed.
type Snapshot struct {
	Path     string
	Info     *ImageInfo
	Warnings []string
	dir      string
}

// TakeSnapshot opens path once, refusing a symlink in any path component
// (final component only on kernels without openat2), requires a
// regular file within the decode size cap, validates the bytes read from that
// descriptor, and writes exactly those bytes into a new private directory.
// The snapshot keeps the original extension. Call Remove when done.
func TakeSnapshot(path string) (*Snapshot, error) {
	data, info, warnings, err := readValidated(path)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "debsplash-")
	if err != nil {
		return nil, fmt.Errorf("could not create snapshot directory: %w", err)
	}
	snapPath, err := writeSnapshot(dir, filepath.Ext(path), data)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	return &Snapshot{Path: snapPath, Info: info, Warnings: warnings, dir: dir}, nil
}

// ValidateOpened applies the snapshot's hardened open and validation without writing a copy.
func ValidateOpened(path string) (*ImageInfo, []string, error) {
	_, info, warnings, err := readValidated(path)
	return info, warnings, err
}

func readValidated(path string) ([]byte, *ImageInfo, []string, error) {
	if err := checkExt(path); err != nil {
		return nil, nil, nil, err
	}

	f, err := openNoSymlinks(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not stat image: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, nil, fmt.Errorf("image %q is not a regular file", path)
	}
	if fi.Size() > maxDecodeSize {
		return nil, nil, nil, fmt.Errorf("image size %d bytes exceeds maximum of %d bytes", fi.Size(), maxDecodeSize)
	}

	data, err := io.ReadAll(io.LimitReader(f, maxDecodeSize+1))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not read image: %w", err)
	}
	if len(data) > maxDecodeSize {
		return nil, nil, nil, fmt.Errorf("image grew beyond maximum of %d bytes while reading", maxDecodeSize)
	}

	info, warnings, err := validateContent(bytes.NewReader(data), path)
	if err != nil {
		return nil, nil, nil, err
	}
	return data, info, warnings, nil
}

// openNoSymlinks opens path read-only, refusing a symlink in any component.
// Callers resolve symlinks first (safepath.ValidateImagePath), so a symlink
// seen here means a component was swapped after resolution. O_NONBLOCK stops
// a FIFO swapped in from blocking the open; the caller's fstat rejects it.
func openNoSymlinks(path string) (*os.File, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err == nil {
		return os.NewFile(uintptr(fd), path), nil
	}
	if !errors.Is(err, unix.ENOSYS) {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EXDEV) {
			return nil, fmt.Errorf("could not open image %q: path contains a symlink or changed after it was resolved: %w", path, err)
		}
		return nil, fmt.Errorf("could not open image %q: %w", path, err)
	}

	// Kernel older than 5.6 has no openat2: only the final component is
	// protected from symlinks on this path.
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("could not open image %q: %w", path, err)
	}
	return f, nil
}

func writeSnapshot(dir, ext string, data []byte) (string, error) {
	out, err := os.CreateTemp(dir, "image-*"+ext)
	if err != nil {
		return "", fmt.Errorf("could not create snapshot: %w", err)
	}
	if _, err := out.Write(data); err != nil {
		out.Close()
		return "", fmt.Errorf("could not write snapshot: %w", err)
	}
	if err := out.Chmod(snapshotMode); err != nil {
		out.Close()
		return "", fmt.Errorf("could not set snapshot mode: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("could not write snapshot: %w", err)
	}
	return out.Name(), nil
}

// Remove deletes the snapshot and its private directory.
func (s *Snapshot) Remove() error {
	return os.RemoveAll(s.dir)
}
