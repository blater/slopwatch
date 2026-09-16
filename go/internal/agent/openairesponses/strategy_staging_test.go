package openairesponses

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrashLeftoverStagingCannotPoisonCandidateOrDeleteLookalikes(t *testing.T) {
	root := canonicalTempDir(t)
	request := testRequest(t, root)
	lookalike := filepath.Join(root, ".slopwatch-agent-unrelated.tmp")
	if err := os.WriteFile(lookalike, []byte("tracked lookalike"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.Workspace.StagingRoot, ".slopwatch-agent-crash.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools, err := newCandidateTools(request.Workspace, request.Write, resolvedConfig{maxWriteBytes: 1 << 20, maxReadBytes: 1 << 20}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	if _, err := tools.write(t.Context(), "main.go", "package main\n"); err != nil {
		t.Fatal(err)
	}
	assertLookalikePreserved(t, lookalike)
	assertStagingArtifactsContained(t, root, lookalike)
}

func assertLookalikePreserved(t *testing.T, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "tracked lookalike" {
		t.Fatalf("lookalike changed: %q %v", contents, err)
	}
}

func assertStagingArtifactsContained(t *testing.T, root, lookalike string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".slopwatch-agent-") && entry.Name() != filepath.Base(lookalike) {
			t.Fatalf("temporary artifact entered candidate: %s", entry.Name())
		}
	}
}
