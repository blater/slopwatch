package native

import (
	"fmt"
	"testing"

	"github.com/blater/slopwatch/internal/report"
)

func TestSharedDepthBoundaryDoesNotRepeatEvidencePerFile(t *testing.T) {
	inputs := newScoreInputs()
	for index := 0; index < 100; index++ {
		inputs.mergeDepth("shared", report.DepthBoundary{
			ID: "shared", State: "partial", Files: []string{fmt.Sprintf("file%d.java", index)},
			Reasons:  []any{map[string]any{"code": "depth_payload_limit", "message": "bounded analysis"}},
			Evidence: []any{map[string]any{"rule": "shared_boundary"}},
		})
	}
	boundary := inputs.depth["shared"]
	if len(boundary.Files) != 100 || len(boundary.Reasons) != 1 || len(boundary.Evidence) != 1 {
		t.Fatalf("shared ledger: files=%d reasons=%d evidence=%d", len(boundary.Files), len(boundary.Reasons), len(boundary.Evidence))
	}
	inputs.mergeDepth("shared", report.DepthBoundary{ID: "shared", State: "partial",
		Reasons: []any{map[string]any{"code": "depth_payload_limit", "message": "different boundary detail"}},
	})
	if len(inputs.depth["shared"].Reasons) != 2 {
		t.Fatal("distinct diagnostic detail was lost")
	}
}
