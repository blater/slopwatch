package candidate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

func verifySeedManifest(destination string, manifest seedManifest) error {
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, entry := range manifest.Entries {
		info, err := root.Lstat(entry.Path.String())
		if entry.Deleted {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err == nil {
				return fmt.Errorf("deleted path %s is still present", entry.Path)
			}
			return err
		}
		if err != nil || !info.Mode().IsRegular() || uint32(info.Mode().Perm()) != entry.Mode || info.Size() != entry.Size {
			return fmt.Errorf("workspace snapshot metadata differs for %s", entry.Path)
		}
		file, err := root.Open(entry.Path.String())
		if err != nil {
			return err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(hash, io.LimitReader(file, entry.Size+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || written != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.Hash {
			return fmt.Errorf("workspace snapshot content differs for %s", entry.Path)
		}
	}
	return nil
}
