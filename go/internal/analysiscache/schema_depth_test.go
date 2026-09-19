package analysiscache

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestDepthBoundaryIDsSurviveDisplayProjection(t *testing.T) {
	measured := 30.0
	document := report.Document{
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
