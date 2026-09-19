package native

import (
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
)

func TestDepthV4ArtifactRoundTripPreservesLedgerAndNullableState(t *testing.T) {
	a, b, c := "a.go", "b.go", "c.go"
	artifact := analysiscache.UnitArtifact{Report: report.Document{
		Depth: map[string]report.DepthBoundary{
			"measured": {ID: "measured", State: "measured", PolicyRevision: "r7", InventoryFingerprint: "inventory-v1", Shallow: depthFloat(30), Files: []string{a}, Raw: map[string]any{
				"state": "measured", "shallow": 30.0, "obligations": []any{"kept"}, "description": "old prose",
			}},
			"na":      {ID: "na", State: "not_applicable", Files: []string{a}},
			"partial": {ID: "partial", State: "partial", Evidence: []any{map[string]any{"kind": "bounded-static-v1", "summary": "old prose", "details": map[string]any{"count": 1}}}, Files: []string{b}},
		},
		Files: []report.File{
			{Path: a, Language: "go", Coverage: map[string]string{"module_shallowness": "complete"}, Components: map[string]report.Component{"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "measured", Evidence: []report.MeasurementEvidence{{Value: 30}}}}},
			{Path: b, Language: "go", Coverage: map[string]string{"module_shallowness": "complete"}, Components: map[string]report.Component{"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "partial"}}},
			{Path: c, Language: "go", Coverage: map[string]string{"module_shallowness": "complete"}, Components: map[string]report.Component{"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "unavailable"}}},
		},
	}}
	inputs, reusable := scoreInputsFromArtifact(artifact, []string{a, b, c}, true)
	if !reusable || len(inputs.depth) != 3 || inputs.depth["measured"].PolicyRevision != "r7" || inputs.depth["measured"].InventoryFingerprint != "inventory-v1" || inputs.depth["measured"].Raw["obligations"] == nil {
		t.Fatalf("depth ledger was not restored: reusable=%v depth=%#v", reusable, inputs.depth)
	}
	if len(inputs.depth["measured"].Raw) != 1 || inputs.depth["measured"].Raw["state"] != nil || inputs.depth["measured"].Raw["description"] != nil {
		t.Fatalf("legacy depth payload was not compacted: %#v", inputs.depth["measured"].Raw)
	}
	if got := inputs.depth["partial"].Evidence[0].(map[string]any); got["summary"] != nil || got["kind"] != "bounded-static-v1" {
		t.Fatalf("legacy depth evidence was not compacted: %#v", got)
	}
	if len(inputs.observations[a]["module_shallowness"]) != 0 || inputs.depthStates[b] != "partial" || inputs.depthStates[c] != "unavailable" {
		t.Fatalf("v4 artifact state leaked into legacy observations: %#v states=%#v", inputs.observations, inputs.depthStates)
	}
	filtered := filterScoreInputs(inputs, pathSet([]string{a}))
	if len(filtered.depth) != 2 || len(filtered.depthByPath[a]) != 2 || filtered.depth["partial"].Files != nil {
		t.Fatalf("owned depth filter lost boundary membership: %#v index=%#v", filtered.depth, filtered.depthByPath)
	}
}

func depthFloat(value float64) *float64 { return &value }
