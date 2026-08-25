//go:build windows

package jobstore

import (
	"fmt"
	"os"
)

func openJournal(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("open job journal: symbolic link rejected")
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open job journal: %w", err)
	}
	return file, nil
}
