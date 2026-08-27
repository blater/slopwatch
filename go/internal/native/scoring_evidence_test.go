package native

import (
	"reflect"
	"testing"

	"github.com/blater/slopmochi/internal/report"
)

func TestCountScoringPreservesEveryFindingAsStructuredEvidence(t *testing.T) {
	descriptor := componentDescriptor{
		ID: "deeply_nested_if", Kind: "count",
		Defaults: componentDefaults{Weight: "2", Formula: "count"},
	}
	flatAttributes := map[string]any{"problem_depth": 3}
	nestedAttributes := map[string]any{"normalized_symbol": "value"}
	provenance := map[string]any{"analyzer": "slopslap-typescript", "rule": "unsafe_type_use/typescript-local-sink-v1"}
	raw := []observation{
		{
			path: "main.go", scope: "expression", value: 1,
			subject: protocolSubject{
				Name: "if", Symbol: "Service.Run.if", Line: 11, Column: 3,
				EndLine: 14, EndColumn: 4,
			},
			attributes: flatAttributes,
		},
		{
			path: "main.ts", scope: "expression", value: 1,
			subject: protocolSubject{
				Name: "value", Symbol: "Service.run.value", Routine: "Service.run",
				Start: protocolPosition{Line: 21, Column: 7, Offset: 410},
				End:   protocolPosition{Line: 21, Column: 12, Offset: 415},
			},
			attributes: nestedAttributes,
			provenance: provenance,
		},
	}

	component, err := scoreComponent(descriptor, "complete", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertCountComponent(t, component, flatAttributes, nestedAttributes, provenance)
}

func assertCountComponent(t *testing.T, component report.Component, flatAttributes, nestedAttributes, provenance map[string]any) {
	t.Helper()
	assertCountSummary(t, component)
	assertEvidenceLocations(t, component)
	assertEvidenceMetadata(t, component, flatAttributes, nestedAttributes, provenance)
}

func assertCountSummary(t *testing.T, component report.Component) {
	t.Helper()
	if component.Contribution != 4 || len(component.Subjects) != 1 || component.Subjects[0].Subject != "deduplicated_count" {
		t.Fatalf("count score changed: %#v", component)
	}
	if len(component.Evidence) != 2 {
		t.Fatalf("evidence = %#v", component.Evidence)
	}
}

func assertEvidenceLocations(t *testing.T, component report.Component) {
	t.Helper()
	flat, nested := component.Evidence[0], component.Evidence[1]
	if flat.Symbol != "Service.Run.if" || flat.Scope != "expression" || flat.Location.Start.Line != 11 || flat.Location.End.Column != 4 {
		t.Fatalf("flat evidence = %#v", flat)
	}
	if nested.Symbol != "Service.run.value" || nested.Routine != "Service.run" || nested.Location.Start.Offset != 410 || nested.Location.End.Offset != 415 {
		t.Fatalf("nested evidence = %#v", nested)
	}
}

func assertEvidenceMetadata(t *testing.T, component report.Component, flatAttributes, nestedAttributes, provenance map[string]any) {
	t.Helper()
	flat, nested := component.Evidence[0], component.Evidence[1]
	if !reflect.DeepEqual(flat.Attributes, flatAttributes) || !reflect.DeepEqual(nested.Attributes, nestedAttributes) {
		t.Fatalf("attributes were not preserved: %#v", component.Evidence)
	}
	if !reflect.DeepEqual(nested.Provenance, provenance) {
		t.Fatalf("provenance was not preserved: %#v", nested.Provenance)
	}
}

func TestSubjectKeyAcceptsNestedAnalyzerCoordinates(t *testing.T) {
	subject := protocolSubject{
		Name: "run", Symbol: "Service.run",
		Start: protocolPosition{Line: 5, Column: 3, Offset: 20},
		End:   protocolPosition{Line: 9, Column: 4, Offset: 80},
	}
	if got := subjectKey(subject); got != "Service.run@5:3-9:4" {
		t.Fatalf("subject key = %q", got)
	}
}
