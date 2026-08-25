//go:build !unix

package candidate

import (
	"errors"
	"os"
)

func readPrivateRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("candidate ownership marker is not a regular file")
	}
	if info.Size() > 1<<20 {
		return nil, errors.New("candidate ownership marker exceeds its size limit")
	}
	return os.ReadFile(path)
}
