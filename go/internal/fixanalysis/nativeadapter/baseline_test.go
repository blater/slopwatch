package nativeadapter

import (
	"context"
	"fmt"
	"path/filepath"
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

func TestPrepareBaselineAllowsIncompleteAndMissingRequiredMetrics(t *testing.T) {
	workspace := makeWorkspace(t, filepath.Join(t.TempDir(), "live"), "baseline")
	service := mustService(t, &fakeFactory{documents: []report.Document{incompleteDocument("a.go")}})
	baseline, err := service.PrepareBaseline(context.Background(), fixanalysis.BaselineRequest{
		Workspace: workspace, Targets: []fix.RepoPath{"go/a.go"},
		Goal:            fix.ScoringGoal{MaximumScore: 0, Focus: []fix.MetricGoal{{Metric: "typesafety", Maximum: 0}}, AllowedRegression: map[fix.MetricID]float64{"npath": 0}},
		RequiredMetrics: []fix.MetricID{"npath", "typesafety"}, FreshBy: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	target := baseline.Contract.Targets[0]
	if target.Complete || target.Metrics["npath"].Complete || target.Metrics["npath"].Value != 5 || baseline.Contract.RequireComplete {
		t.Fatalf("evidence metadata changed: %#v", baseline.Contract)
	}
	if _, exists := target.Metrics["typesafety"]; exists {
		t.Fatal("missing metric was fabricated")
	}
}

func TestPrepareBaselineAllowsMissingAndEstimatedDepthInventory(t *testing.T) {
	for _, estimated := range []bool{false, true} {
		t.Run(fmt.Sprint(estimated), func(t *testing.T) {
			workspace := makeWorkspace(t, filepath.Join(t.TempDir(), "live"), "baseline")
			document := testDocument("a.go", 80, 8, 5)
			document.Files[0].Components["module_shallowness"] = report.Component{
				DepthVersion: "responsibility-burden-v4", DepthState: "partial", DepthEstimated: estimated,
				DepthBoundaryIDs: []string{"missing"},
			}
			service := mustService(t, &fakeFactory{documents: []report.Document{document}})
			baseline, err := service.PrepareBaseline(context.Background(), fixanalysis.BaselineRequest{
				Workspace: workspace, Targets: []fix.RepoPath{"go/a.go"}, Goal: fix.ScoringGoal{MaximumScore: 10},
			})
			if err != nil {
				t.Fatal(err)
			}
			if estimated && baseline.Contract.Targets[0].Complete {
				t.Fatal("estimated evidence labeled complete")
			}
			if baseline.Contract.Targets[0].Score != 80 {
				t.Fatal("baseline score changed")
			}
		})
	}
}

func TestPrepareBaselineToleratesReportMetadataFailures(t *testing.T) {
	for _, scenario := range []string{"identity", "schema", "profile", "calibration", "truncated", "omitted", "invalid-path", "evidence-path", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			workspace := makeWorkspace(t, filepath.Join(t.TempDir(), "live"), "baseline")
			document := testDocument("a.go", 80, 8, 5)
			identity := "catalog-v1"
			switch scenario {
			case "identity":
				identity = ""
			case "schema":
				document.SchemaVersion = 0
			case "profile":
				document.ProfileSetHash = ""
			case "calibration":
				document.Calibrated = false
			case "truncated":
				document.Truncated = true
			case "omitted":
				document.Files = nil
			case "invalid-path":
				document.Files[0].Path = "../outside.go"
			case "duplicate":
				document.Files = append(document.Files, document.Files[0])
			case "evidence-path":
				component := document.Files[0].Components["cognitive_complexity"]
				component.Evidence[0].Location.Path = "../outside.go"
				document.Files[0].Components["cognitive_complexity"] = component
			}
			factory := &fakeFactory{documents: []report.Document{document}, catalogs: []string{identity}}
			baseline, err := mustService(t, factory).PrepareBaseline(context.Background(), fixanalysis.BaselineRequest{
				Workspace: workspace, Targets: []fix.RepoPath{"go/a.go"}, Goal: fix.ScoringGoal{MaximumScore: 10},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(baseline.Contract.Targets) != 1 || baseline.Contract.Targets[0].Path != "go/a.go" {
				t.Fatalf("targets = %#v", baseline.Contract.Targets)
			}
			target := baseline.Contract.Targets[0]
			if scenario != "identity" && target.Complete {
				t.Fatalf("incomplete evidence marked complete: %#v", target)
			}
			if scenario == "omitted" || scenario == "invalid-path" || scenario == "duplicate" {
				if len(target.Metrics) != 0 {
					t.Fatalf("fabricated measurements: %#v", target.Metrics)
				}
			}
			if factory.calls[0].options.ReadCache || len(factory.analyzers[0].targets) != 1 || factory.analyzers[0].targets[0] != "a.go" {
				t.Fatal("analysis was not fresh and scoped")
			}
		})
	}
}
