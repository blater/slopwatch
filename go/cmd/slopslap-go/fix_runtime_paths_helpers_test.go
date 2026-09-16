package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func securityExecutableFixture(t *testing.T) (string, string, string) {
	t.Helper()
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
	return root, repository, executable
}

func assertTrustedExecutable(t *testing.T, repository, executable string) {
	t.Helper()
	if got, err := canonicalInstallationExecutable(repository, executable, "test CLI"); err != nil || got != executable {
		t.Fatalf("canonical executable = %q, %v", got, err)
	}
}

func assertRejectsRelativeExecutable(t *testing.T, repository string) {
	t.Helper()
	if _, err := canonicalInstallationExecutable(repository, "trusted-cli", "test CLI"); err == nil || !strings.Contains(err.Error(), "absolute canonical") {
		t.Fatalf("relative executable error = %v", err)
	}
}

func assertRejectsRepositoryExecutable(t *testing.T, repository string) {
	t.Helper()
	path := filepath.Join(repository, "malicious-cli")
	if err := os.WriteFile(path, []byte("test"), 0o555); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalInstallationExecutable(repository, path, "test CLI"); err == nil || !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("repository executable error = %v", err)
	}
}

func assertRejectsWritableExecutable(t *testing.T, root, repository string) {
	t.Helper()
	path := filepath.Join(root, "writable-cli")
	if err := os.WriteFile(path, []byte("test"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o775); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalInstallationExecutable(repository, path, "test CLI"); err == nil || !strings.Contains(err.Error(), "non-writable") {
		t.Fatalf("writable executable error = %v", err)
	}
}
