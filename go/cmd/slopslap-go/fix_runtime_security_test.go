package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalInstallationExecutableRejectsAmbientAndRepositoryPaths(t *testing.T) {
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	repository := filepath.Join(root, "repo")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "trusted-cli")
	if err := os.WriteFile(executable, []byte("test"), 0o555); err != nil {
		t.Fatal(err)
	}
	if got, err := canonicalInstallationExecutable(repository, executable, "test CLI"); err != nil || got != executable {
		t.Fatalf("canonical executable = %q, %v", got, err)
	}
	if _, err := canonicalInstallationExecutable(repository, "trusted-cli", "test CLI"); err == nil || !strings.Contains(err.Error(), "absolute canonical") {
		t.Fatalf("relative executable error = %v", err)
	}

	repositoryExecutable := filepath.Join(repository, "malicious-cli")
	if err := os.WriteFile(repositoryExecutable, []byte("test"), 0o555); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalInstallationExecutable(repository, repositoryExecutable, "test CLI"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("repository executable error = %v", err)
	}

	writable := filepath.Join(root, "writable-cli")
	if err := os.WriteFile(writable, []byte("test"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writable, 0o775); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalInstallationExecutable(repository, writable, "test CLI"); err == nil || !strings.Contains(err.Error(), "non-writable") {
		t.Fatalf("writable executable error = %v", err)
	}
}
