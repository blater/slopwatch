package adapters

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

type fakeAdapter string

func (item fakeAdapter) Language() string  { return string(item) }
func (fakeAdapter) FactSchemaVersion() int { return facts.SchemaVersion }
func (fakeAdapter) ParserModes() []string  { return []string{"fixture"} }
func (fakeAdapter) Analyze(string, []string, map[string]any) (*facts.Program, error) {
	return &facts.Program{}, nil
}

type staleAdapter struct{ fakeAdapter }

func (staleAdapter) FactSchemaVersion() int { return facts.SchemaVersion - 1 }

func TestRegistrySelectsAdaptersWithoutLanguageBranches(t *testing.T) {
	registry, err := NewRegistry(fakeAdapter("go"), fakeAdapter("rust"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.Adapter("rust")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Language() != "rust" {
		t.Fatalf("selected %q", selected.Language())
	}
	if _, err := NewRegistry(fakeAdapter("go"), fakeAdapter("go")); err == nil {
		t.Fatal("expected duplicate language rejection")
	}
	if _, err := NewRegistry(staleAdapter{fakeAdapter("old")}); err == nil {
		t.Fatal("expected fact-schema rejection")
	}
}
