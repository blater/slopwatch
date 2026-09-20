package scoring

import (
	"encoding/json"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func subject(name, routine string, value float64) report.SubjectContribution {
	return report.SubjectContribution{Subject: name, Routine: routine, Value: value, BaseSeverity: value}
}
func component(subjects ...report.SubjectContribution) report.Component {
	return report.Component{ScoringDefinition: &report.ScoringDefinition{Aggregation: "max"}, Subjects: subjects}
}

func TestPolicyCopiesInputsAndDistinguishesMissingFromExplicitZero(t *testing.T) {
	w := map[string]float64{"cognitive_complexity": 5, "god_class": 0}
	e := map[string]bool{"cognitive_complexity": true, "god_class": true}
	p := NewPolicy(w, e)
	w["cognitive_complexity"] = 20
	e["cognitive_complexity"] = false
	if p.WeightFactor("cognitive_complexity") != .5 || p.WeightFactor("god_class") != 0 || p.Weight("npath_complexity") != 8 || !p.Enabled("npath_complexity") {
		t.Fatal("policy copy/default failure")
	}
}

func TestProjectFilePreservesLegacyReweightingSemantics(t *testing.T) {
	original := report.File{Path: "example.go", Complete: true, Score: 24, Axes: map[string]float64{"old": 24}, Components: map[string]report.Component{
		"cognitive_complexity":        {Contribution: 10, ObservedContribution: 10, Subjects: []report.SubjectContribution{{Subject: "routine", Value: 20, Contribution: 10}}},
		"cyclomatic_class_complexity": {Contribution: 4, ObservedContribution: 4},
		"explicit_any":                {Contribution: 3, ObservedContribution: 3},
		"future_component":            {Contribution: 7, ObservedContribution: 7},
	}}
	projected := ProjectFile(original, NewPolicy(map[string]float64{"cognitive_complexity": 5}, map[string]bool{"cognitive_complexity": true}))
	cognitive := projected.Components["cognitive_complexity"]
	if projected.Score != 9 || projected.Axes["structural_core"] != 5 || projected.Axes["structural_language"] != 4 || cognitive.Contribution != 5 || cognitive.ObservedContribution != 10 || cognitive.Subjects[0].Contribution != 5 {
		t.Fatalf("legacy projection = %#v", projected)
	}
	originalCognitive := original.Components["cognitive_complexity"]
	if len(projected.ScoringLimitations) == 0 || original.Score != 24 || original.Axes["old"] != 24 || originalCognitive.Contribution != 10 || originalCognitive.Subjects[0].Contribution != 10 {
		t.Fatal("legacy projection mutated input or omitted limitation")
	}
}

func TestProjectFileGroupsSignalsAndRepeatsDeterministically(t *testing.T) {
	f := report.File{Path: "x.go", Complete: true, Components: map[string]report.Component{
		"cognitive_complexity": component(subject("cog", "pkg.run", 4)), "cyclomatic_method_complexity": component(subject("cyclo", "pkg.run", 6)), "npath_complexity": component(subject("npath", "pkg.run", 3)),
	}}
	p := NewPolicy(map[string]float64{"cognitive_complexity": 1, "cyclomatic_method_complexity": 1, "npath_complexity": 1}, map[string]bool{"cognitive_complexity": true, "cyclomatic_method_complexity": true, "npath_complexity": true})
	got := ProjectFile(f, p)
	if got.Score != 6 || got.Components["cyclomatic_method_complexity"].Contribution != 6 || len(got.ScoringAttributions) != 1 || got.Components["cognitive_complexity"].Subjects[0].Contribution != 0 {
		t.Fatalf("grouped=%#v", got)
	}
	if signals := got.ScoringAttributions[0].Signals; len(signals) != 2 || signals[0].Component != "cognitive_complexity" || signals[1].Component != "npath_complexity" {
		t.Fatalf("supporting signals must retain losers without duplicating the winner: %#v", signals)
	}
	repeated := ProjectFile(got, NewPolicy(map[string]float64{"cognitive_complexity": 2, "cyclomatic_method_complexity": 2, "npath_complexity": 2}, map[string]bool{"cognitive_complexity": true, "cyclomatic_method_complexity": true, "npath_complexity": true}))
	if repeated.Score != 12 || repeated.Components["cyclomatic_method_complexity"].Contribution != 12 {
		t.Fatalf("repeated=%#v", repeated)
	}
}

func TestProjectFileDistinctAndUnassociatedGroups(t *testing.T) {
	f := report.File{Path: "x.go", Complete: true, Components: map[string]report.Component{
		"cognitive_complexity": component(subject("one", "one", 5), subject("two", "two", 2)), "deeply_nested_if": component(subject("nested", "one", 3), subject("orphan", "unassociated:orphan", 4)),
	}}
	got := ProjectFile(f, NewPolicy(map[string]float64{"cognitive_complexity": 1, "deeply_nested_if": 1}, map[string]bool{"cognitive_complexity": true, "deeply_nested_if": true}))
	if got.Score != 11 || len(got.ScoringAttributions) != 3 || len(got.ScoringLimitations) == 0 {
		t.Fatalf("groups=%#v", got)
	}
}

func TestProjectFileTypeResidualAndZeroBaseWeight(t *testing.T) {
	typeSubject := subject("Widget", "", 10)
	method := subject("run", "Widget.run", 4)
	method.Owner = "Widget"
	f := report.File{Path: "x.go", Complete: true, Components: map[string]report.Component{
		"cyclomatic_class_complexity": {ScoringDefinition: &report.ScoringDefinition{Aggregation: "sum"}, Subjects: []report.SubjectContribution{typeSubject}}, "cyclomatic_method_complexity": component(method),
	}}
	p := NewPolicy(map[string]float64{"cyclomatic_class_complexity": 1, "cyclomatic_method_complexity": 1}, map[string]bool{"cyclomatic_class_complexity": true, "cyclomatic_method_complexity": true})
	if got := ProjectFile(f, p).Components["cyclomatic_class_complexity"].Contribution; got != 6 {
		t.Fatalf("residual=%v", got)
	}
	if got := ProjectFile(f, NewPolicy(map[string]float64{"cyclomatic_class_complexity": 1, "cyclomatic_method_complexity": 1}, map[string]bool{"cyclomatic_class_complexity": true, "cyclomatic_method_complexity": false})).Components["cyclomatic_class_complexity"].Contribution; got != 10 {
		t.Fatalf("disabled residual=%v", got)
	}
	z := report.File{Path: "z.go", Complete: true, Components: map[string]report.Component{"cognitive_complexity": {ScoringDefinition: &report.ScoringDefinition{Aggregation: "sum"}, Subjects: []report.SubjectContribution{subject("run", "run", 5)}}}}
	if got := ProjectFile(z, NewPolicy(map[string]float64{"cognitive_complexity": 2}, map[string]bool{"cognitive_complexity": true})).Score; got != 10 {
		t.Fatalf("zero base=%v", got)
	}
}

func TestProjectFileTypeResidualHonorsMaxAggregation(t *testing.T) {
	first := subject("Widget", "", 10)
	second := subject("Gadget", "", 4)
	first.Owner = "Widget"
	second.Owner = "Gadget"
	methodOne := subject("run", "Widget.run", 2)
	methodOne.Owner = "Widget"
	methodTwo := subject("run", "Gadget.run", 2)
	methodTwo.Owner = "Gadget"
	f := report.File{Path: "x.go", Complete: true, Components: map[string]report.Component{
		"cyclomatic_class_complexity": {
			ScoringDefinition: &report.ScoringDefinition{Aggregation: "max"},
			Subjects:          []report.SubjectContribution{first, second},
		},
		"cyclomatic_method_complexity": component(methodOne, methodTwo),
	}}
	projected := ProjectFile(f, NewPolicy(
		map[string]float64{"cyclomatic_class_complexity": 1, "cyclomatic_method_complexity": 1},
		map[string]bool{"cyclomatic_class_complexity": true, "cyclomatic_method_complexity": false},
	))
	if got := projected.Components["cyclomatic_class_complexity"].Contribution; got != 10 {
		t.Fatalf("max type residual = %v, want 10", got)
	}
}

func TestProjectFileClearsStalePassedWithoutThreshold(t *testing.T) {
	passed := true
	f := report.File{Path: "x.go", Complete: true, Passed: &passed, Components: map[string]report.Component{"cognitive_complexity": component(subject("run", "run", 2))}}
	if got := ProjectFile(f, NewPolicy(map[string]float64{"cognitive_complexity": 3}, map[string]bool{"cognitive_complexity": true})); got.Passed != nil {
		t.Fatalf("stale Passed=%v", got.Passed)
	}
}

func TestProjectFileMarksCompleteZeroAsValid(t *testing.T) {
	got := ProjectFile(report.File{Path: "x.ts", Complete: true, Components: map[string]report.Component{"explicit_any": {Contribution: 3}}}, NewPolicy(nil, nil))
	if got.Score != 0 || !got.ValidZero {
		t.Fatalf("zero=%#v", got)
	}
}

func TestProjectDocumentReranksProjectedScores(t *testing.T) {
	d := report.Document{Files: []report.File{{Path: "low.go", Complete: true, Components: map[string]report.Component{"god_class": {Contribution: 1}}}, {Path: "high.go", Complete: true, Components: map[string]report.Component{"cognitive_complexity": {Contribution: 10}}}}}
	d.Summary = map[string]any{"passed": true, "failed_files": 0, "pass_score": 5.0}
	got := ProjectDocument(d, NewPolicy(nil, nil))
	if got.Files[0].Path != "high.go" || got.Files[0].Rank != 1 {
		t.Fatalf("rank=%#v", got.Files)
	}
	if _, exists := got.Summary["passed"]; exists {
		t.Fatal("projected document retained a stale pass outcome")
	}
	if _, exists := got.Summary["failed_files"]; exists || d.Summary["passed"] != true || len(d.Summary) != 3 {
		t.Fatal("projection retained stale failure counts or mutated the original summary")
	}
}

func TestProjectFileHasDeterministicFloatingPointAccumulation(t *testing.T) {
	f := report.File{Components: map[string]report.Component{}}
	weights := map[string]float64{}
	enabled := map[string]bool{}
	for index, id := range []string{"cognitive_complexity", "cyclomatic_method_complexity", "npath_complexity", "god_class", "coupling_between_objects", "module_shallowness"} {
		f.Components[id] = component(subject(id, id, 100.0/float64(index+3)))
		weights[id], enabled[id] = 1, true
	}
	policy := NewPolicy(weights, enabled)
	want, err := json.Marshal(ProjectFile(f, policy))
	if err != nil {
		t.Fatal(err)
	}
	for iteration := 0; iteration < 100; iteration++ {
		got, err := json.Marshal(ProjectFile(f, policy))
		if err != nil || string(got) != string(want) {
			t.Fatalf("projection varied with map iteration: error=%v\nwant=%s\ngot=%s", err, want, got)
		}
	}
}
