// Package rustadapter bridges the Rust syntax parser into normalized structural facts.
package rustadapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"slopslap.dev/structural/internal/facts"
)

const helperName = "slopslap-structural-rust"

type factRequest struct {
	SchemaVersion int      `json:"schema_version"`
	Workspace     string   `json:"workspace"`
	SourcePaths   []string `json:"source_paths"`
	IncludeTests  bool     `json:"include_tests"`
}

type factResponse struct {
	SchemaVersion int            `json:"schema_version"`
	Program       *facts.Program `json:"program"`
	Error         *string        `json:"error"`
}

// Adapter invokes the bundled Rust parser without running Cargo or project code.
type Adapter struct {
	Executable string
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return helperName + ".exe"
	}
	return helperName
}

func defaultExecutable() string {
	host, err := os.Executable()
	if err != nil {
		return filepath.Join("adapters", "rust", "target", "release", executableName())
	}
	directory := filepath.Dir(host)
	for _, candidate := range []string{
		filepath.Join(directory, executableName()),
		filepath.Join(directory, "adapters", "rust", "target", "release", executableName()),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return filepath.Join(directory, executableName())
}

func (Adapter) Language() string       { return "rust" }
func (Adapter) FactSchemaVersion() int { return facts.SchemaVersion }
func (Adapter) ParserModes() []string  { return []string{"syn-full-no-macro-expansion"} }

// Analyze requests facts for the exact canonical inventory from the Rust helper.
func (adapter Adapter) Analyze(workspace string, paths []string, options map[string]any) (*facts.Program, error) {
	executable := adapter.Executable
	if executable == "" {
		executable = defaultExecutable()
	}
	includeTests, _ := options["include_tests"].(bool)
	payload, err := json.Marshal(factRequest{facts.SchemaVersion, workspace, paths, includeTests})
	if err != nil {
		return nil, fmt.Errorf("encode Rust fact request: %w", err)
	}
	command := exec.Command(executable)
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("Rust fact adapter failed: %w: %s", err, stderr.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var response factResponse
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Rust facts: %w", err)
	}
	if response.SchemaVersion != facts.SchemaVersion {
		return nil, fmt.Errorf("Rust fact schema %d is unsupported", response.SchemaVersion)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("Rust syntax analysis failed: %s", *response.Error)
	}
	if response.Program == nil {
		return nil, fmt.Errorf("Rust fact adapter returned no program")
	}
	return response.Program, nil
}
