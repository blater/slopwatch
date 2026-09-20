package analysiscache

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/scoring"
)

func TestDepthBoundaryIDsSurviveDisplayProjection(t *testing.T) {
	measured := 30.0
	document := report.Document{
		ScorePolicyRevision: "test-grouping-policy",
		Depth: map[string]report.DepthBoundary{
			"na": {ID: "na", State: "not_applicable"},
			"partial": {ID: "partial", State: "partial", Estimated: true,
				Evidence: []any{map[string]any{"kind": "bounded-static-v1"}},
				Raw:      map[string]any{"H": 2}},
		},
		Files: []report.File{{
			Path: "service.go",
			Components: map[string]report.Component{"module_shallowness": {
				DepthVersion: "responsibility-burden-v4", DepthState: "partial", RawMaximum: &measured,
				DepthBoundaryIDs: []string{"partial", "na"},
			}},
		}},
	}
	document.Files[0].Components["cognitive_complexity"] = report.Component{
		ScoringDefinition: &report.ScoringDefinition{Aggregation: "sum"},
		Subjects:          []report.SubjectContribution{{Subject: "run", Routine: "run", Value: 3, BaseSeverity: 3}},
		Evidence:          []report.MeasurementEvidence{{Name: "run"}},
	}
	for _, id := range []string{"cyclomatic_method_complexity", "npath_complexity"} {
		document.Files[0].Components[id] = report.Component{
			ScoringDefinition: &report.ScoringDefinition{Aggregation: "sum"},
			Subjects:          []report.SubjectContribution{{Subject: "run", Value: 1, BaseSeverity: 0.2}},
		}
	}
	document.Files[0].ScoringLimitations = []string{"test association limitation"}
	document = scoring.ProjectDocument(document, scoring.NewPolicy(map[string]float64{"cognitive_complexity": 2}, nil))
	projection := ProjectionFromReport("view", document, FreshnessCurrent)
	data, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	var restored DisplayProjection
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	files := restored.ReportFiles()
	if !reflect.DeepEqual(files[0].ScoringAttributions, document.Files[0].ScoringAttributions) ||
		!reflect.DeepEqual(files[0].ScoringLimitations, document.Files[0].ScoringLimitations) {
		t.Fatal("compact projection lost winner, supporting signals, or limitations")
	}
	if restored.ScorePolicyRevision != document.ScorePolicyRevision || len(files[0].Components["cognitive_complexity"].Evidence) != 0 {
		t.Fatal("compact projection lost policy identity or duplicated evidence")
	}
	reweighted := scoring.ProjectFile(files[0], scoring.NewPolicy(map[string]float64{"cognitive_complexity": 4}, nil))
	if reweighted.Score != 12 || len(reweighted.ScoringAttributions) != 1 || reweighted.ScoringAttributions[0].Contribution != 12 {
		t.Fatalf("cached unit severity or group identity was lost: %#v", reweighted)
	}
	got := files[0].Components["module_shallowness"].DepthBoundaryIDs
	if !reflect.DeepEqual(got, document.Files[0].Components["module_shallowness"].DepthBoundaryIDs) {
		t.Fatalf("depth boundary IDs = %#v, want %#v", got, document.Files[0].Components["module_shallowness"].DepthBoundaryIDs)
	}
	if restored.Depth["na"].State != "not_applicable" || restored.Depth["partial"].State != "partial" {
		t.Fatalf("nullable depth ledger lost: %#v", restored.Depth)
	}
	if boundary := restored.Depth["partial"]; !boundary.Estimated || boundary.Raw != nil || len(boundary.Evidence) != 0 {
		t.Fatalf("display projection duplicated detail or lost estimation state: %#v", boundary)
	}
	if len(document.Depth["partial"].Evidence) == 0 {
		t.Fatal("display projection mutated the detailed report")
	}
}
