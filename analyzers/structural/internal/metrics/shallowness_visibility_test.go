package metrics

import (
	"fmt"
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

func BenchmarkModuleShallownessLargeProgram(b *testing.B) {
	const files = 10_000
	program := &facts.Program{
		Files:            make([]string, 0, files),
		Types:            make([]*facts.Type, 0, files),
		PublicOperations: make([]*facts.PublicOperation, 0, files),
		Representation:   make([]*facts.RepresentationExposure, 0, files),
	}
	for index := range files {
		path := fmt.Sprintf("pkg/file-%05d.go", index)
		location := facts.Location{Path: path, Line: 1, Column: 1}
		program.Files = append(program.Files, path)
		program.Types = append(program.Types, &facts.Type{Name: "Service", Location: location})
		program.PublicOperations = append(program.PublicOperations, &facts.PublicOperation{Name: "Run", Location: location})
		program.Representation = append(program.Representation, &facts.RepresentationExposure{Location: location})
	}
	b.ResetTimer()
	for range b.N {
		moduleShallownessMeasurements(program)
	}
}
