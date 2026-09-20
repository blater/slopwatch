package scoring

import (
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestContinuousSeverityProjectionUsesCanonicalUnitInput(t *testing.T) {
	baseSeverity, err := ScalarSeverity("continuous-log", 10, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	original := report.File{Complete: true, Components: map[string]report.Component{
		"cyclomatic_method_complexity": {
			ScoringDefinition: &report.ScoringDefinition{
				Aggregation: "sum",
			},
			Subjects: []report.SubjectContribution{{Subject: "routine", Value: 10, BaseSeverity: baseSeverity}},
		},
	}}
	projected := ProjectFile(original, NewPolicy(map[string]float64{"cyclomatic_method_complexity": 5}, map[string]bool{"cyclomatic_method_complexity": true}))
	if got := projected.Components["cyclomatic_method_complexity"].Contribution; got != 5 {
		t.Fatalf("reference contribution = %v, want 5", got)
	}
	reweighted := ProjectFile(projected, NewPolicy(map[string]float64{"cyclomatic_method_complexity": 2}, map[string]bool{"cyclomatic_method_complexity": true}))
	if got := reweighted.Components["cyclomatic_method_complexity"].Contribution; got != 2 {
		t.Fatalf("reweighted contribution = %v, want 2", got)
	}
	if got := original.Components["cyclomatic_method_complexity"].Subjects[0].Contribution; got != 0 {
		t.Fatalf("projection mutated original subject contribution = %v", got)
	}
}
