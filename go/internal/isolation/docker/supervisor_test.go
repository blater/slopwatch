package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/isolation"
)

func TestCopyCandidateWorkspaceUsesThrowawayTreeAndLeavesSourceUnchanged(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, ".git"), []byte("gitdir: secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("package pkg\n")
	if err := os.WriteFile(filepath.Join(source, "pkg", "main.go"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyCandidateWorkspace(source, destination, testCopyLimits()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "pkg", "main.go"), []byte("mutated validation copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(source, "pkg", "main.go"))
	if err != nil || string(got) != string(original) {
		t.Fatalf("host candidate changed: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destination, ".git")); !os.IsNotExist(err) {
		t.Fatalf("root .git copied: %v", err)
	}
}

func TestCopyCandidateWorkspaceRejectsSparseOversizeNestedGitAndSymlink(t *testing.T) {
	limits := testCopyLimits()
	limits.MaxFileBytes = 8
	for _, test := range []struct {
		name  string
		setup func(string) error
	}{
		{"sparse", func(root string) error {
			file, err := os.Create(filepath.Join(root, "large"))
			if err != nil {
				return err
			}
			defer file.Close()
			return file.Truncate(limits.MaxFileBytes + 1)
		}},
		{"nested git", func(root string) error { return os.MkdirAll(filepath.Join(root, "vendor", ".git"), 0o700) }},
		{"symlink", func(root string) error { return os.Symlink("outside", filepath.Join(root, "link")) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			if err := test.setup(source); err != nil {
				t.Fatal(err)
			}
			if err := copyCandidateWorkspace(source, destination, limits); err == nil {
				t.Fatal("hostile candidate was copied")
			}
		})
	}
}

func TestCopyCandidateWorkspaceRequiresAndEnforcesEveryExplicitLimit(t *testing.T) {
	if err := copyCandidateWorkspace(t.TempDir(), t.TempDir(), isolation.WorkspaceLimits{}); err == nil || !strings.Contains(err.Error(), "workspace limits") {
		t.Fatalf("missing workspace limits error=%v", err)
	}
	for _, test := range []struct {
		name   string
		limits isolation.WorkspaceLimits
		setup  func(string) error
	}{
		{name: "files", limits: isolation.WorkspaceLimits{MaxFiles: 1, MaxDirectories: 2, MaxPathBytes: 100, MaxFileBytes: 100, MaxTotalBytes: 100}, setup: func(root string) error {
			if err := os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0o600); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(root, "b"), []byte("b"), 0o600)
		}},
		{name: "directories", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 1, MaxPathBytes: 100, MaxFileBytes: 100, MaxTotalBytes: 100}, setup: func(root string) error {
			return os.MkdirAll(filepath.Join(root, "a", "b"), 0o700)
		}},
		{name: "paths", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 2, MaxPathBytes: 2, MaxFileBytes: 100, MaxTotalBytes: 100}, setup: func(root string) error {
			return os.WriteFile(filepath.Join(root, "long"), []byte("a"), 0o600)
		}},
		{name: "file bytes", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 2, MaxPathBytes: 100, MaxFileBytes: 1, MaxTotalBytes: 100}, setup: func(root string) error {
			return os.WriteFile(filepath.Join(root, "a"), []byte("ab"), 0o600)
		}},
		{name: "total bytes", limits: isolation.WorkspaceLimits{MaxFiles: 2, MaxDirectories: 2, MaxPathBytes: 100, MaxFileBytes: 100, MaxTotalBytes: 1}, setup: func(root string) error {
			return os.WriteFile(filepath.Join(root, "a"), []byte("ab"), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			if err := test.setup(source); err != nil {
				t.Fatal(err)
			}
			if err := copyCandidateWorkspace(source, destination, test.limits); err == nil {
				t.Fatal("workspace exceeding explicit limit was copied")
			}
		})
	}
}

func testCopyLimits() isolation.WorkspaceLimits {
	return isolation.WorkspaceLimits{MaxFiles: 100, MaxDirectories: 100, MaxPathBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxTotalBytes: 4 << 20}
}
