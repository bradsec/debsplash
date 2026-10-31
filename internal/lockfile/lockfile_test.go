package lockfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	lock, err := AcquirePath(path)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	defer lock.Release()

	// Verify lock file exists and contains PID.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read lock file: %v", err)
	}
	if len(data) == 0 {
		t.Error("lock file is empty, expected PID")
	}
}

func TestAcquireTwiceFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	lock1, err := AcquirePath(path)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer lock1.Release()

	_, err = AcquirePath(path)
	if err == nil {
		t.Error("expected error for second acquire, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' error, got: %v", err)
	}
}

func TestReleaseThenReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	lock1, err := AcquirePath(path)
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	lock1.Release()

	lock2, err := AcquirePath(path)
	if err != nil {
		t.Fatalf("reacquire after release failed: %v", err)
	}
	lock2.Release()
}

func TestReleaseNil(t *testing.T) {
	var l *Lock
	if err := l.Release(); err != nil {
		t.Errorf("Release on nil lock should not error: %v", err)
	}
}

func TestReleasePreservesLockInode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	first, err := AcquirePath(path)
	if err != nil {
		t.Fatal(err)
	}
	firstInfo, err := first.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	pathInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("lock path disappeared after Release: %v", err)
	}
	if !os.SameFile(firstInfo, pathInfo) {
		t.Fatal("lock path points to a different inode after Release")
	}
	second, err := AcquirePath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	secondInfo, err := second.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(firstInfo, secondInfo) {
		t.Fatal("re-Acquire locked a different inode")
	}
}

func TestQueuedOpenerCannotOverlapNewHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	first, err := AcquirePath(path)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer queued.Close()
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(queued.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(queued.Fd()), syscall.LOCK_UN)
	third, err := AcquirePath(path)
	if err == nil {
		third.Release()
		t.Fatal("two holders overlapped on different lock inodes")
	}
}

func TestConcurrentAcquireReleaseNeverOverlaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")
	var active atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for range 1000 {
				lock, err := AcquirePath(path)
				if err != nil {
					runtime.Gosched()
					continue
				}
				if active.Add(1) != 1 {
					t.Error("two lock holders overlapped")
				}
				runtime.Gosched()
				active.Add(-1)
				lock.Release()
			}
		})
	}
	wg.Wait()
}
