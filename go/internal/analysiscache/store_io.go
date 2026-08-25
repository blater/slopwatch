package analysiscache

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func writeImmutable(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("immutable cache path contains different data")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomicNoReplace(path, data)
}

func writeAtomic(path string, data []byte) error {
	return writeAtomicMode(path, data, false)
}

func writeAtomicNoReplace(path string, data []byte) error {
	return writeAtomicMode(path, data, true)
}

func writeAtomicMode(path string, data []byte, noReplace bool) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".tmp-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if noReplace {
		if existing, err := os.ReadFile(path); err == nil {
			if !bytes.Equal(existing, data) {
				return fmt.Errorf("immutable cache path contains different data")
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(temporaryName, path); err != nil {
		if noReplace {
			if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
				return nil
			}
		}
		return err
	}
	return nil
}
