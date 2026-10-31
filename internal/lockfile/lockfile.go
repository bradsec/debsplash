package lockfile

import (
	"fmt"
	"os"
	"syscall"
)

const defaultPath = "/var/run/debsplash.lock"

// Lock represents an acquired filesystem lock.
type Lock struct {
	file *os.File
}

// Acquire attempts to acquire an exclusive lock on the lockfile.
// Returns an error if another instance is already running.
func Acquire() (*Lock, error) {
	return AcquirePath(defaultPath)
}

// AcquirePath attempts to acquire an exclusive lock on the given path.
func AcquirePath(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("could not open lock file %s: %w", path, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another instance of debsplash is already running (lock: %s)", path)
	}

	// Write PID for debugging.
	f.Truncate(0)
	fmt.Fprintf(f, "%d\n", os.Getpid())

	return &Lock{file: f}, nil
}

// Release releases the lock. The lock file stays in place so all openers use the same inode.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	return nil
}
