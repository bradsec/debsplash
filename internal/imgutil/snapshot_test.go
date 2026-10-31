package imgutil

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTakeSnapshot_KeepsValidatedBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	writePNG(t, path, 4, 4)
	want, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}

	snap, err := TakeSnapshot(path)
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	defer snap.Remove()

	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(snap.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("snapshot bytes differ from the validated image")
	}
	if filepath.Ext(snap.Path) != ".png" {
		t.Errorf("snapshot ext = %q, want .png", filepath.Ext(snap.Path))
	}
	if snap.Info.Width != 4 || snap.Info.Height != 4 {
		t.Errorf("info = %+v", snap.Info)
	}

	fi, _ := os.Stat(snap.Path)
	if fi.Mode() != snapshotMode {
		t.Errorf("snapshot mode = %v, want %v", fi.Mode(), os.FileMode(snapshotMode))
	}
	di, _ := os.Stat(filepath.Dir(snap.Path))
	if di.Mode().Perm() != 0700 {
		t.Errorf("snapshot dir perm = %o, want 0700", di.Mode().Perm())
	}

	if err := snap.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(snap.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("snapshot dir still present after Remove: %v", err)
	}
}

func TestTakeSnapshot_RejectsFinalSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.png")
	writePNG(t, real, 4, 4)
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if snap, err := TakeSnapshot(link); err == nil {
		snap.Remove()
		t.Fatal("TakeSnapshot followed a symlink")
	}
}

func TestTakeSnapshot_RejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.png")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := TakeSnapshot(path)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("TakeSnapshot accepted a FIFO")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TakeSnapshot blocked on a FIFO")
	}
}

func TestTakeSnapshot_RejectsNonImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "img.png")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nid\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if snap, err := TakeSnapshot(path); err == nil {
		snap.Remove()
		t.Fatal("TakeSnapshot accepted a non-image")
	}
}

// N-01: a symlink anywhere in the path, not only the final component, must
// be rejected, so swapping a parent directory after resolution cannot
// redirect the open.
func TestTakeSnapshot_RejectsSymlinkedParentDir(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(realDir, "img.png"), 4, 4)
	linkDir := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(linkDir, "img.png")
	snap, err := TakeSnapshot(path)
	if err == nil {
		snap.Remove()
		t.Fatal("TakeSnapshot followed a symlinked parent directory")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the path %q", err, path)
	}
}
