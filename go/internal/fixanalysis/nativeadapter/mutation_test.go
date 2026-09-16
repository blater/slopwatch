package nativeadapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/report"
)

func TestVerifyDetectsMutationDuringAnalysis(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	candidate := makeCandidate(t, root, "before")
	factory := &fakeFactory{documents: []report.Document{testDocument("a.go", 70, 7, 5)}}
	factory.onAnalyze = func() {
		if err := os.WriteFile(filepath.Join(candidate.RepositoryRoot, "go", "a.go"), []byte("after"), 0o600); err != nil {
			t.Error(err)
		}
	}
	result, err := mustService(t, factory).Verify(context.Background(), fixanalysis.VerificationRequest{
		Candidate: candidate, Contract: testContract("go/a.go"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stable() || result.Complete || result.TargetMet || !strings.Contains(result.Diagnostic, "changed during analysis") {
		t.Fatalf("Verify() = %#v", result)
	}
}
