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
