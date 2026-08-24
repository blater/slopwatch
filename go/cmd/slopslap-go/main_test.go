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
