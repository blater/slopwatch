package preferences

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type ProjectDocument struct {
	Files ProjectFiles `toml:"files"`
}

type ProjectFiles struct {
	Exclude string `toml:"exclude,multiline"`
}

func LoadProject(root string) (ProjectDocument, error) {
	var document ProjectDocument
	path := filepath.Join(root, ".slopwatch.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return document, err
	}
	if err := toml.Unmarshal(data, &document); err != nil {
		return document, fmt.Errorf("decode project preferences %s: %w", path, err)
	}
	return document, nil
}

func SaveProject(root string, document ProjectDocument) error {
	data, err := toml.Marshal(document)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(root, ".slopwatch.toml"), data)
}
