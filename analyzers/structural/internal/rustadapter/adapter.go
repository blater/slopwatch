// Package rustadapter bridges the Rust syntax parser into normalized structural facts.
package rustadapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Rust fact output: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("Rust fact adapter failed: %w: %s", err, stderr.String())
	}
	decoder := json.NewDecoder(stdout)
	decoder.DisallowUnknownFields()
	var response factResponse
	decodeErr := decoder.Decode(&response)
	if decodeErr == nil {
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				decodeErr = fmt.Errorf("unexpected trailing JSON value")
			} else {
				decodeErr = fmt.Errorf("invalid trailing output: %w", err)
			}
		}
	}
	if decodeErr != nil {
		// Keep consuming output so a helper with a large malformed response cannot
		// block while Wait waits for its stdout pipe to close.
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf("Rust fact adapter failed: %w: %s", err, stderr.String())
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decode Rust facts: %w", decodeErr)
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
	if err := response.Program.LinkTypeMethods(); err != nil {
		return nil, fmt.Errorf("link Rust method facts: %w", err)
	}
	return response.Program, nil
}
