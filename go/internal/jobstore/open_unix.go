//go:build !windows

package jobstore

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openJournal(path string) (*os.File, error) {
	descriptor, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open job journal: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), path)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("secure job journal: %w", err)
	}
	return file, nil
}
