package nativeadapter

import (
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
)

func TestVerifyReportsScoresWithoutInventoryOrEstimateVeto(t *testing.T) {
	file := report.File{Path: "service.go", Complete: true, Score: 35, Components: map[string]report.Component{
		"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "measured", DepthBoundaryIDs: []string{"boundary"}},
	}}
	for _, depths := range []map[string]report.DepthBoundary{
		nil,
		{"boundary": {ID: "boundary", State: "measured", InventoryFingerprint: "changed"}},
		{"boundary": {ID: "boundary", State: "partial", Estimated: true}},
	} {
		result, err := verifyFile(fix.TargetSnapshot{Path: "service.go"}, file, depths, fix.ScoringGoal{MaximumScore: 40}, false)
		if err != nil || !result.TargetMet || result.Score != 35 || strings.Contains(result.Diagnostic, "rebaseline") {
			t.Fatalf("score reporting rejected: result=%#v err=%v", result, err)
		}
		if depths["boundary"].Estimated && (result.Complete || !strings.Contains(result.Diagnostic, "estimated")) {
			t.Fatalf("estimate metadata lost: %#v", result)
		}
	}
}
