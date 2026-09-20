package nativeadapter

import (
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
)

func assertBaselineSnapshot(t *testing.T, baseline fixanalysis.BaselineSnapshot, target fix.RepoPath, now time.Time) {
	t.Helper()
	contract := baseline.Contract
	if baseline.PreparedAt != now || baseline.Fingerprint == "" || contract.CatalogID != "catalog-v1/report-schema-3" || contract.ProfileSetHash != "profile-v1" || contract.Targets[0].ContentHash == "" {
		t.Fatalf("baseline = %#v", baseline)
	}
	metric := contract.Targets[0].Metrics["cog"]
	if metric.Value != 8 || !metric.Complete {
		t.Fatalf("baseline metrics = %#v", contract.Targets[0].Metrics)
	}
	paths := contract.Targets[0].Evidence[0].Paths
	if len(paths) != 1 || paths[0] != target {
		t.Fatalf("evidence paths = %v, want %v", paths, target)
	}
}

func assertVerifiedSnapshot(t *testing.T, verified fixanalysis.VerificationResult) {
	t.Helper()
	if !verified.Complete || !verified.TargetMet || !verified.Stable() || len(verified.Files) != 1 || verified.Files[0].Score != 70 {
		t.Fatalf("verified = %#v", verified)
	}
}

func assertAnalyzerCalls(t *testing.T, factory *fakeFactory, workspace, candidate string) {
	t.Helper()
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.calls) != 2 {
		t.Fatalf("factory calls = %#v", factory.calls)
	}
	if factory.calls[0].workspace != workspace || factory.calls[1].workspace != candidate {
		t.Fatalf("factory workspaces = %#v", factory.calls)
	}
	if !factory.calls[0].options.ReadCache || factory.calls[1].options.ReadCache {
		t.Fatalf("cache options = %#v", factory.calls)
	}
	for index, analyzer := range factory.analyzers {
		got := analyzer.targets
		if len(got) != 1 || got[0] != "a.go" {
			t.Fatalf("analyzer %d targets = %v", index, got)
		}
	}
}
