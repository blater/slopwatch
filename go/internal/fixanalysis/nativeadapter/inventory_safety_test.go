package nativeadapter

import (
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
)

func TestV4InventoryChangeBlocksFixVerification(t *testing.T) {
	file := report.File{Path: "service.go", Complete: true, Score: 0, Components: map[string]report.Component{
		"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "measured", DepthBoundaryIDs: []string{"boundary"}},
	}}
	baseline := fix.TargetSnapshot{Path: "service.go", DepthInventory: map[string]string{"boundary": "before"}}
	goal := fix.ScoringGoal{MaximumScore: 100}
	result, err := verifyFile(baseline, file, map[string]report.DepthBoundary{"boundary": {ID: "boundary", State: "measured", InventoryFingerprint: "before"}}, goal, false)
	if err != nil || !result.TargetMet {
		t.Fatalf("unchanged inventory rejected: result=%#v err=%v", result, err)
	}
	result, err = verifyFile(baseline, file, map[string]report.DepthBoundary{"boundary": {ID: "boundary", State: "measured", InventoryFingerprint: "after"}}, goal, false)
	if err != nil || result.TargetMet || result.Complete || !containsDiagnostic(result.Diagnostic, "rebaseline required") {
		t.Fatalf("changed inventory accepted: result=%#v err=%v", result, err)
	}
	result, err = verifyFile(baseline, file, nil, goal, false)
	if err != nil || result.TargetMet || !containsDiagnostic(result.Diagnostic, "missing") {
		t.Fatalf("missing inventory accepted: result=%#v err=%v", result, err)
	}
	result, err = verifyFile(baseline, file, map[string]report.DepthBoundary{"boundary": {ID: "boundary", State: "partial", Estimated: true, InventoryFingerprint: "before"}}, goal, false)
	if err != nil || result.TargetMet || !containsDiagnostic(result.Diagnostic, "estimate") {
		t.Fatalf("estimated inventory accepted: result=%#v err=%v", result, err)
	}
}

func containsDiagnostic(value, want string) bool {
	for index := 0; index+len(want) <= len(value); index++ {
		if value[index:index+len(want)] == want {
			return true
		}
	}
	return false
}
