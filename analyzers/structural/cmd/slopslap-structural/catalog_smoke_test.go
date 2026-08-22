package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBundledCatalogMatchesStructuralAnalyzer(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "component-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Components []struct {
			ID      string `json:"component_id"`
			Version string `json:"definition_version"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	versions := make(map[string]string, len(catalog.Components))
	for _, component := range catalog.Components {
		versions[component.ID] = component.Version
	}
	for id, version := range componentVersions {
		if versions[id] != version {
			t.Fatalf("catalog mismatch for %s: bundled=%q structural=%q", id, versions[id], version)
		}
	}
}
