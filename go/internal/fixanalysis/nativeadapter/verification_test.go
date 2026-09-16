package nativeadapter

import (
	"context"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/report"
)

func TestVerifyEnforcesScoreFocusRegressionCompletenessAndExactInventory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		document   report.Document
		wantDetail string
	}{
		{name: "score", document: testDocument("a.go", 101, 8, 5), wantDetail: "score"},
		{name: "focus", document: testDocument("a.go", 90, 11, 5), wantDetail: "COG"},
		{name: "regression", document: testDocument("a.go", 90, 8, 7), wantDetail: "NPATH regressed"},
		{name: "incomplete", document: incompleteDocument("a.go"), wantDetail: "incomplete"},
		{name: "missing", document: report.Document{Calibrated: true, ProfileSetHash: "profile-v1", SchemaVersion: 3}, wantDetail: "returned 0 files"},
		{name: "extra", document: report.Document{Calibrated: true, ProfileSetHash: "profile-v1", SchemaVersion: 3, Files: []report.File{
			testFile("a.go", 80, 8, 5), testFile("b.go", 80, 8, 5),
		}}, wantDetail: "returned 2 files"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			candidate := makeCandidate(t, root, "candidate")
			factory := &fakeFactory{documents: []report.Document{test.document}}
			service := mustService(t, factory)
			contract := testContract("go/a.go")
			result, err := service.Verify(context.Background(), fixanalysis.VerificationRequest{Candidate: candidate, Contract: contract})
			if err != nil {
				t.Fatal(err)
			}
			if result.TargetMet {
				t.Fatalf("Verify() = %#v, want target not met", result)
			}
			detail := result.Diagnostic
			if len(result.Files) > 0 {
				detail += " " + result.Files[0].Diagnostic
			}
			if !strings.Contains(detail, test.wantDetail) {
				t.Fatalf("diagnostic %q does not contain %q", detail, test.wantDetail)
			}
		})
	}
}
