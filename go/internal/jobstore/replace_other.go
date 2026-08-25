//go:build !windows

package jobstore

import "os"

func replaceJournal(source, destination string) error {
	return os.Rename(source, destination)
}
