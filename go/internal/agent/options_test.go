package agent

import "testing"

func TestResolveOptionUsesProviderDefault(t *testing.T) {
	options := []Option[ModelID]{{ID: "first"}, {ID: "provider-default", Default: true}}
	if got, ok := ResolveOption(options, ""); !ok || got != "provider-default" {
		t.Fatalf("ResolveOption() = %q, %t", got, ok)
	}
}
