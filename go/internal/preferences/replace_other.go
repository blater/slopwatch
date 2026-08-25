//go:build !windows

package preferences

import "os"

func replaceFile(from, to string) error {
	return os.Rename(from, to)
}
