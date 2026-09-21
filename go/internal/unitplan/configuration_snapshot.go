package unitplan

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// ConfigurationSnapshot captures metadata membership and bytes once. Sources
// and their imports remain live when planning subsequent incremental updates.
type ConfigurationSnapshot struct {
	files    []string
	data     map[string][]byte
	errors   map[string]error
	captured bool
}

func NewConfigurationSnapshot() *ConfigurationSnapshot {
	return &ConfigurationSnapshot{data: map[string][]byte{}, errors: map[string]error{}}
}

func liveSource(path string) bool {
	if filepath.Base(path) == "build.rs" {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".java", ".rs", ".ts", ".tsx", ".mts", ".cts":
		return true
	}
	return false
}

func (s *ConfigurationSnapshot) inventory(root string, files []string) ([]string, error) {
	if !s.captured {
		for _, path := range files {
			if !liveSource(path) {
				s.files = append(s.files, path)
			}
			// Capture known metadata even before a newly created source needs it.
			// Referenced TS JSON is captured by planner reads.
			if configurationInput(path) {
				_, _ = s.read(root, path)
			}
		}
		return files, nil
	}
	result := append([]string(nil), s.files...)
	for _, path := range files {
		if liveSource(path) {
			result = append(result, path)
		}
	}
	return uniqueStrings(result), nil
}

func (s *ConfigurationSnapshot) read(root, path string) ([]byte, error) {
	if data, ok := s.data[path]; ok {
		return data, nil
	}
	if err, ok := s.errors[path]; ok {
		return nil, err
	}
	if s.captured {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		s.errors[path] = err
	} else {
		s.data[path] = data
	}
	return data, err
}

// Capture completes the startup snapshot after the first plan has identified
// backend inputs; deleted configuration remains available for later updates.
func (s *ConfigurationSnapshot) Capture(root string, plan Plan) {
	if s == nil || s.captured {
		return
	}
	for _, unit := range plan.Units {
		for _, path := range unit.ConfigInputs {
			_, _ = s.read(root, path)
		}
	}
	s.captured = true
}

// Bytes returns immutable startup bytes. Callers must not modify the map.
func (s *ConfigurationSnapshot) Bytes() map[string][]byte {
	if s == nil {
		return nil
	}
	return s.data
}

// Unchanged preserves startup/one-shot validation before the snapshot becomes
// the active session policy. Incremental analysis deliberately does not call it.
func (s *ConfigurationSnapshot) Unchanged(root string) bool {
	if s == nil {
		return true
	}
	for path, expected := range s.data {
		actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(actual, expected) {
			return false
		}
	}
	return true
}

func configurationInput(path string) bool {
	switch filepath.Base(path) {
	case "go.mod", "go.sum", "go.work", "go.work.sum", "Cargo.toml", "Cargo.lock", "build.rs", "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "gradle.properties", "gradle.lockfile", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return true
	}
	return (strings.HasPrefix(filepath.Base(path), "tsconfig") && strings.HasSuffix(path, ".json")) || strings.HasPrefix(path, ".cargo/") || strings.Contains(path, "/.cargo/") || strings.HasPrefix(path, ".mvn/") || strings.Contains(path, "/.mvn/") || strings.Contains("/"+path, "/gradle/wrapper/")
}
