package nativeadapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if baseline.PreparedAt != now || baseline.Fingerprint == "" || baseline.Contract.CatalogID != "catalog-v1/report-schema-3" ||
		baseline.Contract.ProfileSetHash != "profile-v1" || baseline.Contract.Targets[0].ContentHash == "" {
		t.Fatalf("baseline = %#v", baseline)
	}
	if baseline.Contract.Targets[0].Metrics["cog"].Value != 8 || !baseline.Contract.Targets[0].Metrics["cog"].Complete {
		t.Fatalf("baseline metrics = %#v", baseline.Contract.Targets[0].Metrics)
	}
	if paths := baseline.Contract.Targets[0].Evidence[0].Paths; len(paths) != 1 || paths[0] != target {
		t.Fatalf("evidence paths = %v, want %v", paths, target)
	}

	verified, err := service.Verify(context.Background(), fixanalysis.VerificationRequest{Candidate: candidate, Contract: baseline.Contract})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Complete || !verified.Compliant || !verified.Stable() || len(verified.Files) != 1 || verified.Files[0].Score != 70 {
		t.Fatalf("verified = %#v", verified)
	}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if len(factory.calls) != 2 || factory.calls[0].workspace != workspace.AnalysisRoot || factory.calls[1].workspace != candidate.AnalysisRoot {
		t.Fatalf("factory calls = %#v", factory.calls)
	}
	if !factory.calls[0].options.ReadCache || factory.calls[1].options.ReadCache {
		t.Fatalf("cache options = %#v", factory.calls)
	}
	if got := factory.analyzers[1].targets; len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("candidate analysis targets = %v", got)
	}
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
			if result.Compliant {
				t.Fatalf("Verify() = %#v, want noncompliant", result)
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
	if result.Stable() || result.Complete || result.Compliant || !strings.Contains(result.Diagnostic, "changed during analysis") {
		t.Fatalf("Verify() = %#v", result)
	}
}

func TestVerifyRejectsCatalogOrProfileDriftWithoutAcceptingReport(t *testing.T) {
	t.Parallel()
	for name, values := range map[string][3]string{
		"catalog": {"catalog-v2", "profile-v1", "catalog changed"},
		"profile": {"catalog-v1", "profile-v2", "profile changed"},
	} {
		catalog, profile, detail := values[0], values[1], values[2]
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			candidate := makeCandidate(t, root, "candidate")
			document := testDocument("a.go", 70, 7, 5)
			document.ProfileSetHash = profile
			factory := &fakeFactory{catalogs: []string{catalog}, documents: []report.Document{document}}
			result, err := mustService(t, factory).Verify(context.Background(), fixanalysis.VerificationRequest{
				Candidate: candidate, Contract: testContract("go/a.go"),
			})
			if err != nil || result.Compliant || !strings.Contains(result.Diagnostic, detail) {
				t.Fatalf("Verify() = %#v, %v", result, err)
			}
		})
	}
}

type factoryCall struct {
	workspace string
	options   AnalyzerOptions
}

type fakeFactory struct {
	mu        sync.Mutex
	documents []report.Document
	catalogs  []string
	calls     []factoryCall
	analyzers []*fakeAnalyzer
	onAnalyze func()
}

func (factory *fakeFactory) New(workspace, _ string, options AnalyzerOptions) (Analyzer, error) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	index := len(factory.calls)
	catalog := "catalog-v1"
	if index < len(factory.catalogs) {
		catalog = factory.catalogs[index]
	}
	analyzer := &fakeAnalyzer{document: factory.documents[index], catalog: catalog, onAnalyze: factory.onAnalyze}
	factory.calls = append(factory.calls, factoryCall{workspace: workspace, options: options})
	factory.analyzers = append(factory.analyzers, analyzer)
	return analyzer, nil
}

type fakeAnalyzer struct {
	document  report.Document
	catalog   string
	targets   []string
	onAnalyze func()
}

func (analyzer *fakeAnalyzer) Analyze(_ context.Context, targets, _ []string) (report.Document, error) {
	analyzer.targets = append([]string(nil), targets...)
	if analyzer.onAnalyze != nil {
		analyzer.onAnalyze()
	}
	return analyzer.document, nil
}

func (analyzer *fakeAnalyzer) ScoringIdentity() (string, error) { return analyzer.catalog, nil }

func mustService(t *testing.T, factory Factory) *Service {
	t.Helper()
	service, err := NewWithFactory(Config{InstallationRoot: "/installation"}, factory)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func makeWorkspace(t *testing.T, root, contents string) fix.WorkspaceIdentity {
	t.Helper()
	writeTarget(t, root, contents)
	return fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: root, AnalysisRoot: filepath.Join(root, "go"), BaseCommit: "base"}
}

func makeCandidate(t *testing.T, root, contents string) fix.CandidateIdentity {
	t.Helper()
	writeTarget(t, root, contents)
	return fix.CandidateIdentity{Job: "job", Repository: "repo", RepositoryRoot: root, AnalysisRoot: filepath.Join(root, "go"), BaseCommit: "base"}
}

func writeTarget(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "go"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go", "a.go"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testContract(path fix.RepoPath) fix.ScoringContract {
	file := testFile("a.go", 80, 8, 5)
	return fix.ScoringContract{
		CatalogID: "catalog-v1/report-schema-3", ProfileSetHash: "profile-v1", RequireComplete: true,
		Goal: fix.ScoringGoal{MaximumScore: 100, Focus: []fix.MetricGoal{{Metric: "cog", Maximum: 10}}},
		Targets: []fix.TargetSnapshot{{
			Path: path, Score: file.Score, Complete: true,
			Metrics: metricValues(file),
		}},
	}
}

func testDocument(path string, score, cognitive, npath float64) report.Document {
	return report.Document{Calibrated: true, ProfileSetHash: "profile-v1", SchemaVersion: 3, Files: []report.File{testFile(path, score, cognitive, npath)}}
}

func incompleteDocument(path string) report.Document {
	file := testFile(path, 80, 8, 5)
	file.Complete = false
	return report.Document{Calibrated: true, ProfileSetHash: "profile-v1", SchemaVersion: 3, Files: []report.File{file}}
}

func testFile(path string, score, cognitive, npath float64) report.File {
	return report.File{
		Path: path, Language: "go", Complete: true, Score: score,
		Components: map[string]report.Component{
			"cognitive_complexity": {
				Contribution: cognitive, Subjects: []report.SubjectContribution{{Value: cognitive}},
				Evidence: []report.MeasurementEvidence{{Value: cognitive, Location: report.SourceRange{Path: path}}},
			},
			"npath_complexity": {Contribution: npath, Subjects: []report.SubjectContribution{{Value: npath}}},
		},
	}
}
