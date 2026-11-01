package state

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tortoise10h/buddy/internal/config"
)

type FileLock struct {
	file *os.File
}

// AcquireStateLock acquires an exclusive advisory lock on ~/.buddy/.state.lock with retry.
func AcquireStateLock(timeout time.Duration) (*FileLock, error) {
	buddyDir := config.GetBuddyDir()
	if err := os.MkdirAll(buddyDir, 0755); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(buddyDir, ".state.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open state lock file: %w", err)
	}

	start := time.Now()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &FileLock{file: f}, nil
		}
		if time.Since(start) >= timeout {
			f.Close()
			return nil, fmt.Errorf("timed out waiting for state lock (%v): %w", timeout, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Release unlocks and closes the lock file.
func (l *FileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer l.file.Close()
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}
