//go:build unix

package openairesponses

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func renameRootedFile(source *os.File, sourceName string, destination *os.File, destinationName string) error {
	if source == nil || destination == nil || sourceName == "" || destinationName == "" ||
		strings.Contains(sourceName, "/") || strings.Contains(destinationName, "/") || sourceName == "." || sourceName == ".." || destinationName == "." || destinationName == ".." {
		return errors.New("rooted rename requires simple source and destination names")
	}
	return unix.Renameat(int(source.Fd()), sourceName, int(destination.Fd()), destinationName)
}
