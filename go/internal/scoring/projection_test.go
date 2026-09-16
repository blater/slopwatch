package scoring

import (
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestPolicyCopiesInputsAndDistinguishesMissingFromExplicitZero(t *testing.T) {
	weights := map[string]float64{"cognitive_complexity": 5, "god_class": 0}
	enabled := map[string]bool{"cognitive_complexity": true, "god_class": true}
	policy := NewPolicy(weights, enabled)
	weights["cognitive_complexity"] = 20
	enabled["cognitive_complexity"] = false

	if policy.WeightFactor("cognitive_complexity") != 0.5 {
		t.Fatalf("copied factor = %v, want 0.5", policy.WeightFactor("cognitive_complexity"))
	}
	if policy.WeightFactor("god_class") != 0 {
		t.Fatalf("explicit zero factor = %v, want 0", policy.WeightFactor("god_class"))
	}
	if policy.Weight("npath_complexity") != 8 || !policy.Enabled("npath_complexity") {
		t.Fatal("missing policy values did not resolve to catalog defaults")
	}
}

func TestProjectFilePreservesLegacyReweightingSemantics(t *testing.T) {
	original := report.File{
		Path: "example.go", Complete: true, Score: 24,
		Axes: map[string]float64{"old": 24},
		Components: map[string]report.Component{
			"cognitive_complexity": {
				Contribution: 10, ObservedContribution: 10,
				Subjects: []report.SubjectContribution{{Subject: "routine", Value: 20, Contribution: 10}},
			},
			"cyclomatic_class_complexity": {Contribution: 4, ObservedContribution: 4},
			"explicit_any":                {Contribution: 3, ObservedContribution: 3},
			"future_component":            {Contribution: 7, ObservedContribution: 7},
		},
	}
	policy := NewPolicy(
		map[string]float64{"cognitive_complexity": 5},
		map[string]bool{"cognitive_complexity": true},
	)
	projected := ProjectFile(original, policy)

	if projected.Score != 9 {
		t.Fatalf("score = %v, want 9", projected.Score)
	}
	if projected.Axes["structural_core"] != 5 || projected.Axes["structural_language"] != 4 {
		t.Fatalf("axes = %#v", projected.Axes)
	}
	if projected.Axes["typescript_type_safety"] != 0 || projected.Axes["unknown"] != 0 {
		t.Fatalf("disabled axes = %#v", projected.Axes)
	}
	cognitive := projected.Components["cognitive_complexity"]
	if cognitive.Contribution != 5 || cognitive.ObservedContribution != 10 || cognitive.Subjects[0].Contribution != 5 {
		t.Fatalf("cognitive projection = %#v", cognitive)
	}

	if original.Score != 24 || original.Axes["old"] != 24 {
		t.Fatal("projection mutated original file")
	}
	originalCognitive := original.Components["cognitive_complexity"]
	if originalCognitive.Contribution != 10 || originalCognitive.Subjects[0].Contribution != 10 {
		t.Fatal("projection mutated original component")
	}
}

func TestProjectFileMarksCompleteZeroAsValid(t *testing.T) {
	original := report.File{
		Path: "example.ts", Complete: true,
		Components: map[string]report.Component{
			"explicit_any": {Contribution: 3},
		},
	}
	projected := ProjectFile(original, NewPolicy(nil, nil))
	if projected.Score != 0 || !projected.ValidZero {
		t.Fatalf("zero projection = score %v valid %t", projected.Score, projected.ValidZero)
	}
}

func TestProjectDocumentReranksProjectedScores(t *testing.T) {
	document := report.Document{Files: []report.File{
		{Path: "low.go", Complete: true, Components: map[string]report.Component{"god_class": {Contribution: 1}}},
		{Path: "high.go", Complete: true, Components: map[string]report.Component{"cognitive_complexity": {Contribution: 10}}},
	}}
	projected := ProjectDocument(document, NewPolicy(nil, nil))
	if projected.Files[0].Path != "high.go" || projected.Files[0].Rank != 1 || projected.Files[1].Rank != 2 {
		t.Fatalf("ranked files = %#v", projected.Files)
	}
}
