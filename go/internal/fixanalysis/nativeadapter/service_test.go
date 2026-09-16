package nativeadapter

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
)

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
