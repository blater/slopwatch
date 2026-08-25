package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestHelpIsARecognizedNonFailure(t *testing.T) {
	err := run([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help returned %v, want flag.ErrHelp", err)
	}
}

func TestDurableConfinementOwnerIsStableAndRejectsUnsafeState(t *testing.T) {
	root := t.TempDir()
	first, err := durableConfinementOwner(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := durableConfinementOwner(root)
	if err != nil || first != second || len(first) != 32 {
		t.Fatalf("owners %q %q err=%v", first, second, err)
	}
	info, err := os.Lstat(filepath.Join(root, "owner-id"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("owner mode=%v err=%v", info.Mode(), err)
	}

	for _, test := range []struct {
		name  string
		setup func(string) error
	}{
		{"symlink", func(path string) error { return os.Symlink("target", path) }},
		{"wrong mode", func(path string) error {
			return os.WriteFile(path, []byte("00112233445566778899aabbccddeeff\n"), 0o644)
		}},
		{"oversize", func(path string) error { return os.WriteFile(path, make([]byte, 129), 0o600) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			if err := test.setup(filepath.Join(state, "owner-id")); err != nil {
				t.Fatal(err)
			}
			if _, err := durableConfinementOwner(state); err == nil {
				t.Fatal("unsafe owner state accepted")
			}
		})
	}
}

func TestDurableConfinementOwnerUsesOpenedRootAfterPathSubstitution(t *testing.T) {
	parent := t.TempDir()
	original := filepath.Join(parent, "state")
	moved := filepath.Join(parent, "state-original")
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(original)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	owner, err := durableConfinementOwnerRoot(root)
	if err != nil || owner == "" {
		t.Fatalf("rooted owner creation failed: owner=%q err=%v", owner, err)
	}
	if _, err := os.Stat(filepath.Join(moved, "owner-id")); err != nil {
		t.Fatalf("owner was not persisted under opened root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(original, "owner-id")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement path was touched: %v", err)
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
