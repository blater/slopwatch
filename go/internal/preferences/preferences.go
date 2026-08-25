// Package preferences owns the versioned, user-editable Slopwatch preferences
// document and its durable storage lifecycle.
package preferences

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

const CurrentVersion = 1

type Document struct {
	Version     int         `toml:"version"`
	Appearance  Appearance  `toml:"appearance"`
	Table       Table       `toml:"table"`
	Interaction Interaction `toml:"interaction"`
	Scoring     Scoring     `toml:"scoring"`
}

type Appearance struct {
	Theme string `toml:"theme"`
}

type Table struct {
	VisibleColumns []string `toml:"visible_columns"`
	SortBy         string   `toml:"sort_by"`
	SortDescending bool     `toml:"sort_descending"`
}

type Interaction struct {
	TrendWindow string `toml:"trend_window"`
}

type Scoring struct {
	WeightStep    float64                        `toml:"weight_step"`
	MaximumWeight float64                        `toml:"maximum_weight"`
	Components    map[string]ComponentPreference `toml:"components"`
}

type ComponentPreference struct {
	Enabled bool    `toml:"enabled"`
	Weight  float64 `toml:"weight"`
}

func DefaultPath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(root, "slopwatch", "preferences.toml"), nil
}

func LoadOrCreate(path string, defaults Document) (Document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := Save(path, defaults); err != nil {
			return Document{}, err
		}
		return defaults, nil
	}
	if err != nil {
		return Document{}, fmt.Errorf("read preferences %s: %w", path, err)
	}
	value := clone(defaults)
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Document{}, fmt.Errorf("decode preferences %s: %w", path, err)
	}
	if value.Version != CurrentVersion {
		return Document{}, fmt.Errorf("preferences %s use schema version %d; supported version is %d", path, value.Version, CurrentVersion)
	}
	return value, nil
}

func Save(path string, value Document) error {
	data, err := toml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	header := []byte("# Slopwatch preferences. Managed by the dashboard; manual edits are read at launch.\n# CLI options override matching values for that run.\n")
	data = append(header, data...)
	if err := writeAtomic(path, data); err != nil {
		return fmt.Errorf("write preferences %s: %w", path, err)
	}
	return nil
}

func clone(value Document) Document {
	result := value
	result.Table.VisibleColumns = append([]string(nil), value.Table.VisibleColumns...)
	result.Scoring.Components = make(map[string]ComponentPreference, len(value.Scoring.Components))
	for id, component := range value.Scoring.Components {
		result.Scoring.Components[id] = component
	}
	return result
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".preferences-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, path)
}
