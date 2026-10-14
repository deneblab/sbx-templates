//go:build unix

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes an exclusive lock on path, creating it if needed, and waits for a holder to let go.
// The lock belongs to the open file, so the OS drops it when the process dies: a crash never leaves a
// stale lock behind, which a lock made of a file's mere existence would.
func lockFile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}
