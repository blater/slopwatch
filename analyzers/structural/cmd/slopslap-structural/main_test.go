package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolEmitsMeasurementsCoveragePlanAndTerminal(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package sample\nfunc Run(ok bool) { if ok {} }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := request{
		Type: "request", Version: 1, Invocation: "00000000-0000-0000-0000-000000000001", Workspace: root,
		Units:      []unit{{ID: "go-unit", Language: "go", Paths: []string{"main.go"}, Metadata: map[string]any{}}},
		Components: []component{{ID: "cognitive_complexity", Version: "pmd-sonar-v1"}, {ID: "npath_complexity", Version: "pmd-v1"}},
		Options:    map[string]any{}, Limits: map[string]int{},
	}
	var output bytes.Buffer
	if code := run(input, &output); code != 0 {
		t.Fatalf("run returned %d", code)
	}
	decoder := json.NewDecoder(&output)
	types := make([]string, 0)
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		types = append(types, record["type"].(string))
	}
	want := []string{"measurement", "measurement", "coverage", "coverage", "execution_plan", "terminal"}
	if len(types) != len(want) {
		t.Fatalf("record types = %#v", types)
	}
	for index := range want {
		if types[index] != want[index] {
			t.Fatalf("record types = %#v", types)
		}
	}
}

func TestDecodeRequestAcceptsLargeInventories(t *testing.T) {
	input := request{
		Type: "request", Version: 1, Invocation: "00000000-0000-0000-0000-000000000004", Workspace: t.TempDir(),
		Units: []unit{{ID: "go-unit", Language: "go", Paths: []string{strings.Repeat("x", 2*1024*1024)}}},
	}
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(input); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRequest(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Units[0].Paths[0]; got != input.Units[0].Paths[0] {
		t.Fatal("large source inventory was not preserved")
	}
}

func TestProtocolUsesDeterministicTypeMetricFallback(t *testing.T) {
	root := t.TempDir()
	source := "package sample\nimport \"example.invalid/missing\"\ntype Service struct { peer missing.Peer }\nfunc (s Service) Run(other missing.Peer) { _ = s.peer; _ = other.Value }\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	input := request{
		Type: "request", Version: 1, Invocation: "00000000-0000-0000-0000-000000000002", Workspace: root,
		Units:      []unit{{ID: "go-unit", Language: "go", Paths: []string{"main.go"}, Metadata: map[string]any{}}},
		Components: []component{{ID: "cognitive_complexity", Version: "pmd-sonar-v1"}, {ID: "coupling_between_objects", Version: "pmd-v1"}},
		Options:    map[string]any{}, Limits: map[string]int{},
	}
	var first, second bytes.Buffer
	run(input, &first)
	run(input, &second)
	if first.String() != second.String() {
		t.Fatal("identical analysis requests produced different protocol output")
	}
	decoder := json.NewDecoder(&first)
	seenComplete := false
	seenCBO := false
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["type"] == "measurement" && record["component_id"] == "coupling_between_objects" {
			seenCBO = true
		}
		if record["type"] == "coverage" && record["component_id"] == "coupling_between_objects" {
			seenComplete = record["state"] == "complete" && record["reason"] == ""
		}
	}
	if !seenCBO || !seenComplete {
		t.Fatalf("CBO measurement=%v complete coverage=%v", seenCBO, seenComplete)
	}
}

func TestValidationRejectsDuplicateSourceOwnership(t *testing.T) {
	input := request{
		Type: "request", Version: 1, Invocation: "00000000-0000-0000-0000-000000000003", Workspace: t.TempDir(),
		Units: []unit{
			{ID: "first", Language: "go", Paths: []string{"main.go"}},
			{ID: "second", Language: "go", Paths: []string{"main.go"}},
		},
		Components: []component{{ID: "npath_complexity", Version: "pmd-v1"}},
	}
	if err := validate(input); err == nil {
		t.Fatal("expected duplicate source ownership rejection")
	}
}
