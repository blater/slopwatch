package metrics

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestModuleShallownessDoesNotInferVisibilityFromCapitalization(t *testing.T) {
	measurements := moduleShallownessMeasurements(&facts.Program{
		Files:     []string{"participant.java"},
		Functions: []*facts.Function{{Name: "commit", Location: facts.Location{Path: "participant.java"}}},
		Types:     []*facts.Type{{Name: "Participant", Kind: "interface", Location: facts.Location{Path: "participant.java"}}},
	})
	if len(measurements) != 1 {
		t.Fatalf("measurements = %d, want 1", len(measurements))
	}
	if measurements[0].Value != 0 || measurements[0].Attributes["available"] != false {
		t.Fatalf("missing public-operation evidence became a score: %#v", measurements[0])
	}
}
