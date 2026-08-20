//go:build unix

package core

import (
	"os"
	"syscall"
)

// flock is per open file description, so it serializes concurrent
// UpdateConfig callers both within one process and across processes.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func flockUnlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
