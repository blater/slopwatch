//go:build !unix

package openairesponses

import (
	"errors"
	"os"
)

func renameRootedFile(*os.File, string, *os.File, string) error {
	return errors.New("rooted cross-directory replacement is unsupported on this platform")
}
