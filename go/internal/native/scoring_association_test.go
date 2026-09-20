package native

import (
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestScoringRoutineAssociationPreservesOverloadsAndRawEvidence(t *testing.T) {
	method := func(start, end int) observation {
		return observation{path: "Example.java", scope: "function", subject: protocolSubject{
			Symbol: "Example.run", Routine: "Example.run", Line: start, Column: 1, EndLine: end, EndColumn: 1,
		}}
	}
	nested := func(line int) observation {
		return observation{path: "Example.java", scope: "expression", subject: protocolSubject{
			Symbol: "if", Routine: "Example.run", Line: line, Column: 2, EndLine: line, EndColumn: 10,
		}}
	}
	raw := map[string][]observation{
		"cognitive_complexity":         {method(1, 10), method(12, 20)},
		"cyclomatic_method_complexity": {method(1, 10), method(12, 20)},
		"npath_complexity":             {method(1, 10), method(12, 20)},
		"deeply_nested_if":             {nested(3), nested(5), nested(14), nested(25)},
	}
	associated := associateScoringRoutines(raw)
	first := associated["cognitive_complexity"][0].scoringRoutine
	second := associated["cognitive_complexity"][1].scoringRoutine
	if first == "" || second == "" || first == second {
		t.Fatalf("overloads merged: %q / %q", first, second)
	}
	for _, id := range []string{"cognitive_complexity", "cyclomatic_method_complexity", "npath_complexity"} {
		if associated[id][0].scoringRoutine != first || associated[id][1].scoringRoutine != second {
			t.Fatalf("%s does not share routine identities", id)
		}
		if raw[id][0].scoringRoutine != "" || associated[id][0].subject.Routine != "Example.run" {
			t.Fatal("association changed raw analyzer evidence")
		}
	}
	for i, want := range []string{first, first, second, ""} {
		if got := associated["deeply_nested_if"][i].scoringRoutine; got != want {
			t.Errorf("nesting %d owner = %q, want %q", i, got, want)
		}
	}
	component, err := scoreComponent(componentDescriptor{ID: "deeply_nested_if", Kind: "count", Defaults: componentDefaults{Weight: "6", Formula: "count"}}, "complete", associated["deeply_nested_if"])
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]float64{}
	for _, subject := range component.Subjects {
		routine := subject.Routine
		if routine == "" {
			routine = subject.Subject
		}
		counts[routine] += subject.Value
	}
	if counts[first] != 2 || counts[second] != 1 || counts["unassociated:Example.java:25:2"] != 1 || component.Contribution != 24 || len(component.Evidence) != 4 {
		t.Fatalf("nesting aggregation lost routine counts or raw evidence: %#v", component)
	}
}

func TestScoringRangeIndexFindsSmallestEnclosingDeclaration(t *testing.T) {
	span := func(start, end int) report.SourceRange {
		return report.SourceRange{Path: "nested.ts", Start: report.SourcePosition{Line: start, Column: 1}, End: report.SourcePosition{Line: end, Column: 1}}
	}
	index := newScoringRangeIndex([]scoringRange{
		{key: "outer", location: span(1, 100)},
		{key: "inner", location: span(10, 50)},
		{key: "deep", location: span(20, 30)},
		{key: "sibling", location: span(60, 70)},
	})
	for _, test := range []struct {
		start, end int
		want       string
	}{{22, 23, "deep"}, {35, 36, "inner"}, {55, 56, "outer"}, {65, 66, "sibling"}, {75, 76, "outer"}, {110, 111, ""}} {
		if got := index.enclosing(span(test.start, test.end)); got != test.want {
			t.Errorf("%d:%d = %q, want %q", test.start, test.end, got, test.want)
		}
	}
	ambiguous := newScoringRangeIndex([]scoringRange{{key: "one", location: span(1, 10)}, {key: "two", location: span(5, 20)}})
	if got := ambiguous.enclosing(span(6, 7)); got != "" {
		t.Fatalf("ambiguous crossing declarations assigned to %q", got)
	}
}

func TestScoringOwnerUsesExactReceiverOutsideTypeDeclaration(t *testing.T) {
	span := func(line int) report.SourceRange {
		return report.SourceRange{Path: "source", Start: report.SourcePosition{Line: line, Column: 1}, End: report.SourcePosition{Line: line + 2, Column: 1}}
	}
	for _, language := range []string{"go", "rust"} {
		file := report.File{Language: language, Components: map[string]report.Component{
			"cyclomatic_class_complexity": {
				Subjects: []report.SubjectContribution{{Subject: "Counter@1"}},
				Evidence: []report.MeasurementEvidence{{Symbol: "Counter", Location: span(1)}},
			},
			"cyclomatic_method_complexity": {
				Subjects: []report.SubjectContribution{{Subject: "run"}, {Subject: "other"}},
				Evidence: []report.MeasurementEvidence{{Routine: "Counter.Run", Location: span(10)}, {Routine: "Other.Counter.Run", Location: span(20)}},
			},
		}}
		associateTypeOwners(&file)
		methods := file.Components["cyclomatic_method_complexity"].Subjects
		if methods[0].Owner != "Counter@1" || methods[1].Owner != "" {
			t.Errorf("%s receiver association = %#v", language, methods)
		}
	}
}
