package main

import (
	"errors"
	"flag"
	"testing"
)

func TestHelpIsARecognizedNonFailure(t *testing.T) {
	err := run([]string{"--help"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help returned %v, want flag.ErrHelp", err)
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
