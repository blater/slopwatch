package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type catalogDocument struct {
	Languages  []string              `json:"languages"`
	Analyzers  []analyzerDescriptor  `json:"analyzers"`
	Components []componentDescriptor `json:"components"`
}

type analyzerDescriptor struct {
	Language   string   `json:"language"`
	Backend    string   `json:"backend"`
	Extensions []string `json:"extensions"`
	Entrypoint string   `json:"entrypoint"`
	ReadyPath  string   `json:"ready_path"`
	Source     string   `json:"source"`
}

type componentDescriptor struct {
	ID               string            `json:"component_id"`
	Version          string            `json:"definition_version"`
	Axis             string            `json:"axis"`
	Kind             string            `json:"kind"`
	Aggregator       string            `json:"aggregator"`
	DeduplicationKey []string          `json:"deduplication_key"`
	Support          map[string]string `json:"support"`
	Defaults         componentDefaults `json:"defaults"`
}

type componentDefaults struct {
	Enabled   bool    `json:"enabled"`
	Threshold *string `json:"threshold"`
	Weight    string  `json:"weight"`
	Formula   string  `json:"formula"`
	Cap       *string `json:"cap"`
}

func (defaults componentDefaults) threshold() (float64, bool, error) {
	if defaults.Threshold == nil {
		return 0, false, nil
	}
	value, err := strconv.ParseFloat(*defaults.Threshold, 64)
	return value, true, err
}

func (defaults componentDefaults) weight() (float64, error) {
	return strconv.ParseFloat(defaults.Weight, 64)
}

func loadCatalog(installationRoot string) (catalogDocument, error) {
	path := filepath.Join(installationRoot, "component-catalog.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		return catalogDocument{}, fmt.Errorf("read component catalog: %w", err)
	}
	var document catalogDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return catalogDocument{}, fmt.Errorf("decode component catalog: %w", err)
	}
	return document, nil
}

func (component componentDescriptor) supported(language string) bool {
	switch component.Support[language] {
	case "conformant", "supported", "best_effort":
		return true
	default:
		return false
	}
}
