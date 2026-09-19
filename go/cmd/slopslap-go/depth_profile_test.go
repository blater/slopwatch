package main

import (
	"testing"

	"github.com/blater/slopwatch/internal/native"
)

func TestShallowProfileIsExplicitAndValidated(t *testing.T) {
	flags, defaults := parser()
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if profile, err := selectedShallowProfile(defaults); err != nil || profile != native.ShallowProfileResponsibilityV4 {
		t.Fatalf("default shallow profile = %q, error=%v", profile, err)
	}
	flags, optedIn := parser()
	if err := flags.Parse([]string{"--shallow-profile=" + native.ShallowProfileResponsibilityV4}); err != nil {
		t.Fatal(err)
	}
	if optedIn.shallowProfile != native.ShallowProfileResponsibilityV4 {
		t.Fatalf("opted-in shallow profile = %q", optedIn.shallowProfile)
	}
	if err := validateOptions(optedIn); err != nil {
		t.Fatalf("valid shallow profile rejected: %v", err)
	}
	flags, canonical := parser()
	if err := flags.Parse([]string{"--score-profile=" + native.ShallowProfileResponsibilityV4}); err != nil {
		t.Fatal(err)
	}
	if profile, err := selectedShallowProfile(canonical); err != nil || profile != native.ShallowProfileResponsibilityV4 {
		t.Fatalf("canonical v4 profile = %q, error=%v", profile, err)
	}
	legacy := &options{scoreProfile: shallowProfileLegacy}
	if profile, err := selectedShallowProfile(legacy); err != nil || profile != shallowProfileLegacy {
		t.Fatalf("explicit legacy profile = %q, error=%v", profile, err)
	}
	if err := validateOptions(&options{format: "text", shallowProfile: native.ShallowProfileResponsibilityV4, scoreProfile: shallowProfileLegacy}); err == nil {
		t.Fatal("conflicting profile flags were accepted")
	}
	if err := validateOptions(&options{format: "text", shallowProfile: "unknown"}); err == nil {
		t.Fatal("unknown shallow profile was accepted")
	}
}
