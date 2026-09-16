package main

import (
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestHelpIsARecognizedNonFailure(t *testing.T) {
	err := run([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help returned %v, want flag.ErrHelp", err)
	}
}

func TestTextRenderingMarksFailedCoverageAndPrintsDiagnostics(t *testing.T) {
	file := report.File{Path: "broken.go", Score: 0, Complete: false,
		Components: map[string]report.Component{"cognitive_complexity": {}, "cyclomatic_class_complexity": {}},
		Coverage:   map[string]string{"cognitive_complexity": "failed", "cyclomatic_class_complexity": "failed"}}
	document := report.Document{Files: []report.File{file}, Diagnostics: []map[string]any{{
		"severity": "error", "code": "SYNTAX_ERROR", "path": "broken.go", "message": "broken.go:1:14: expected operand",
	}, {
		"severity": "info", "code": "typescript.structural_semantic_exceptions", "path": "valid.ts", "message": "semantic exceptions were observed",
	}}}
	table := renderTable(document, false, false)
	if !strings.Contains(table, "X") || strings.Contains(table, "0/0") || !strings.Contains(table, "X/-") {
		t.Fatalf("failed coverage was rendered as a score: %s", table)
	}
	if compact := renderTable(document, true, false); !strings.Contains(compact, "X  broken.go") {
		t.Fatalf("compact failed coverage omitted X: %s", compact)
	}
	diagnostics := renderDiagnostics(document)
	if !strings.Contains(diagnostics, "SYNTAX_ERROR: broken.go:1:14") || strings.Contains(diagnostics, "broken.go: broken.go:") {
		t.Fatalf("path-specific diagnostic omitted: %s", diagnostics)
	}
	if strings.Contains(diagnostics, "semantic exceptions") {
		t.Fatalf("informational diagnostic leaked into text output: %s", diagnostics)
	}
}

func TestUseCacheIsExplicitOptIn(t *testing.T) {
	flags, defaults := parser()
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if defaults.useCache {
		t.Fatal("slopmark cache reads must be disabled by default")
	}

	flags, optedIn := parser()
	if err := flags.Parse([]string{"--use-cache"}); err != nil {
		t.Fatal(err)
	}
	if !optedIn.useCache {
		t.Fatal("--use-cache did not enable verified cache reads")
	}
}

func TestFollowSymlinksIsExplicitOptIn(t *testing.T) {
	flags, defaults := parser()
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if defaults.followSymlinks {
		t.Fatal("nested symlink traversal must be disabled by default")
	}

	flags, optedIn := parser()
	if err := flags.Parse([]string{"--follow-symlinks"}); err != nil {
		t.Fatal(err)
	}
	if !optedIn.followSymlinks {
		t.Fatal("--follow-symlinks did not enable nested symlink traversal")
	}
}

func TestPreviouslyIgnoredOptionsAreExplicit(t *testing.T) {
	if err := validateOptions(&options{format: "text", backends: stringList{"go=legacy"}}); err == nil {
		t.Fatal("--backend was silently accepted")
	}
	if err := validateOptions(&options{format: "text", config: "preferences.toml"}); err == nil {
		t.Fatal("--config was silently accepted outside follow mode")
	}
	if err := validateOptions(&options{format: "text", follow: true, config: "preferences.toml"}); err != nil {
		t.Fatalf("follow preferences path was rejected: %v", err)
	}
}
