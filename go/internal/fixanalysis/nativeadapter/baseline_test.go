package nativeadapter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/report"
)

func TestPrepareBaselineAndVerifyUseFreshCandidateAnalyzer(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workspace := makeWorkspace(t, filepath.Join(root, "live"), "baseline")
	candidate := makeCandidate(t, filepath.Join(root, "candidate"), "candidate")
	factory := &fakeFactory{documents: []report.Document{
		testDocument("a.go", 80, 8, 5),
		testDocument("a.go", 70, 7, 5),
	}}
	now := time.Unix(100, 0)
	service, err := NewWithFactory(Config{
		InstallationRoot: "/installation", BaselineReadCache: true, Clock: func() time.Time { return now },
	}, factory)
	if err != nil {
		t.Fatal(err)
	}
	target := fix.RepoPath("go/a.go")
	baseline, err := service.PrepareBaseline(context.Background(), fixanalysis.BaselineRequest{
		Workspace: workspace, Targets: []fix.RepoPath{target},
		Goal: fix.ScoringGoal{MaximumScore: 100, Focus: []fix.MetricGoal{{Metric: "cog", Maximum: 10}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBaselineSnapshot(t, baseline, target, now)

	verified, err := service.Verify(context.Background(), fixanalysis.VerificationRequest{Candidate: candidate, Contract: baseline.Contract})
	if err != nil {
		t.Fatal(err)
	}
	assertVerifiedSnapshot(t, verified)
	assertAnalyzerCalls(t, factory, workspace.AnalysisRoot, candidate.AnalysisRoot)
}

func TestPrepareBaselineRejectsIncompleteExplicitRequiredMetric(t *testing.T) {
	root := t.TempDir()
	workspace := makeWorkspace(t, filepath.Join(root, "live"), "baseline")
	service := mustService(t, &fakeFactory{documents: []report.Document{incompleteDocument("a.go")}})
	_, err := service.PrepareBaseline(context.Background(), fixanalysis.BaselineRequest{Workspace: workspace, Targets: []fix.RepoPath{"go/a.go"}, Goal: fix.ScoringGoal{MaximumScore: 100}, RequiredMetrics: []fix.MetricID{"npath"}})
	if err == nil || !strings.Contains(err.Error(), "npath") {
		t.Fatalf("required metric error=%v", err)
	}
}
