package follow

import (
	"errors"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/report"
)

func progressModel(files ...report.File) Model {
	document := report.Document{Files: files}
	document.SortAndRank()
	rows := make(map[string]rowState, len(files))
	for _, file := range files {
		rows[file.Path] = rowState{}
	}
	return Model{
		files: FilesState{
			Document: document, BaseDocument: document, Rows: rows,
			Visible: map[string]bool{}, SortKey: "score", SortReverse: true,
		},
		options: Options{TrendWindow: time.Minute},
		queued:  map[string]bool{},
	}
}

func TestAnalysisProgressDrainResortsAndPreservesSelection(t *testing.T) {
	model := progressModel(testFile("selected.go", 10), testFile("other.go", 5))
	model.files.Selected = "selected.go"
	model.files.refreshDisplayFiles(model.options.Limit)
	emit := beginAnalysisProgress(&model, report.FreshnessRefreshing, "analysis in progress")
	emit(report.Document{Files: []report.File{testFile("other.go", 20)}, Depth: map[string]report.DepthBoundary{"other-boundary": {ID: "other-boundary", Scope: "file"}}})
	model.runtime.analysisProgress.lastFlush = time.Now().Add(-analysisProgressBatchInterval)
	flushAnalysisProgress(&model, false)

	if model.files.Selected != "selected.go" {
		t.Fatalf("selection moved during progress resort: %q", model.files.Selected)
	}
	if files := model.files.displayFiles(model.options.Limit); len(files) != 2 || files[0].Path != "other.go" {
		t.Fatalf("progress sort = %#v", files)
	}
	if file := model.files.Document.Files[0]; file.Freshness != report.FreshnessRefreshing {
		t.Fatalf("preview freshness = %q, want refreshing", file.Freshness)
	}
	if _, ok := model.files.BaseDocument.Depth["other-boundary"]; !ok {
		t.Fatalf("progress depth boundary was not retained: %#v", model.files.BaseDocument.Depth)
	}
	if model.files.ScoreDistribution.total != 2 || model.files.ScoreDistribution.bins[4] != 1 {
		t.Fatalf("progress score distribution = %#v", model.files.ScoreDistribution)
	}
	emit(report.Document{Files: []report.File{testFile("other.go", 15)}})
	flushAnalysisProgress(&model, false)
	if model.files.Document.Files[0].Score != 20 {
		t.Fatalf("dirty progress flushed before cadence: %#v", model.files.Document.Files)
	}
	model.runtime.analysisProgress.lastFlush = time.Now().Add(-analysisProgressBatchInterval)
	flushAnalysisProgress(&model, false)
	if model.files.Document.Files[0].Score != 15 || model.files.ScoreDistribution.bins[3] != 1 {
		t.Fatalf("second progress drain = files:%#v distribution:%#v", model.files.Document.Files, model.files.ScoreDistribution)
	}
}

func TestSupersededAnalysisProgressIsIgnored(t *testing.T) {
	model := progressModel(testFile("old.go", 1))
	oldEmit := beginAnalysisProgress(&model, report.FreshnessVerifying, "analysis in progress")
	beginAnalysisProgress(&model, report.FreshnessRefreshing, "analysis in progress")
	oldEmit(report.Document{Files: []report.File{testFile("stale.go", 99)}})
	model.runtime.analysisProgress.lastFlush = time.Now().Add(-analysisProgressBatchInterval)
	flushAnalysisProgress(&model, false)
	if len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Path != "old.go" {
		t.Fatalf("superseded progress changed rows: %#v", model.files.Document.Files)
	}
	emit := beginAnalysisProgress(&model, report.FreshnessRefreshing, "analysis in progress")
	emit(report.Document{Files: []report.File{testFile("discarded.go", 99)}})
	model.runtime.discardAnalysis = true
	flushAnalysisProgress(&model, true)
	if len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Path != "old.go" {
		t.Fatalf("discarded progress changed rows: %#v", model.files.Document.Files)
	}
	model.runtime.discardAnalysis = false
	emit = beginAnalysisProgress(&model, report.FreshnessRefreshing, "analysis in progress")
	emit(report.Document{Files: []report.File{testFile("reconfigure.go", 99)}})
	model.runtime.watchReconfigurePending = true
	flushAnalysisProgress(&model, true)
	if len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Path != "old.go" {
		t.Fatalf("reconfigure progress changed rows: %#v", model.files.Document.Files)
	}
}

func TestAnalysisProgressFinalSuccessAndFailureHaveAuthoritativeLifecycle(t *testing.T) {
	model := progressModel(testFile("old.go", 1))
	emit := beginAnalysisProgress(&model, report.FreshnessRefreshing, "analysis in progress")
	emit(report.Document{Files: []report.File{testFile("preview.go", 7)}})
	updated, _ := handleAnalysisResult(&model, analysisResult{full: true, document: report.Document{Files: []report.File{testFile("final.go", 3)}}})
	if updated != &model || model.runtime.analysisProgress != nil || len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Path != "final.go" {
		t.Fatalf("final result did not replace preview: %#v", model.files.Document.Files)
	}

	model = progressModel()
	emit = beginAnalysisProgress(&model, report.FreshnessRefreshing, "analysis in progress")
	emit(report.Document{Files: []report.File{testFile("preview.go", 7)}})
	_, _ = handleAnalysisResult(&model, analysisResult{full: true, err: errors.New("scan failed")})
	if model.runtime.analysisProgress != nil || len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Path != "preview.go" {
		t.Fatalf("failed result discarded completed preview: %#v", model.files.Document.Files)
	}
	if model.files.Document.Files[0].Freshness != report.FreshnessStaleError {
		t.Fatalf("failed preview freshness = %q", model.files.Document.Files[0].Freshness)
	}
}

func TestPendingRatingsStayOutOfCompletedGraph(t *testing.T) {
	model := progressModel()
	emit := beginAnalysisProgress(&model, report.FreshnessVerifying, "analysis in progress")
	file := testFile("service.go", 25)
	file.PendingComponents = []string{"module_shallowness"}
	emit(report.Document{Files: []report.File{file}, Progress: map[string]report.ScanProgress{"go": {Stage: "attribution", Completed: 1, Total: 2, Files: 2}}})
	flushAnalysisProgress(&model, true)
	if model.runtime.scanFinished != 0 || model.runtime.scanTotal != 2 || model.files.ScoreDistribution.bins[5] != 0 || !metricPending(model.files.Document.Files[0], "score") {
		t.Fatal("pending analysis was presented as a completed rating")
	}
	file.PendingComponents = nil
	file.Complete = false // Estimated evidence may still be a finished calculation.
	value := 25.0
	file.Components["module_shallowness"] = report.Component{DepthVersion: "responsibility-burden-v4", DepthState: "partial", DepthEstimated: true, RawMaximum: &value, Contribution: 25}
	emit(report.Document{Files: []report.File{file}})
	flushAnalysisProgress(&model, true)
	if model.runtime.scanFinished != 1 || model.files.ScoreDistribution.bins[5] != 1 || metricPending(model.files.Document.Files[0], "score") {
		t.Fatal("finished estimated rating did not update the master document and graph")
	}
}

func TestScanProgressDoesNotCreateEditHighlights(t *testing.T) {
	model := progressModel(testFile("existing.go", 10))
	model.initialAnalysis = true
	emit := beginAnalysisProgress(&model, report.FreshnessVerifying, "scanning")
	emit(report.Document{Files: []report.File{testFile("existing.go", 30), testFile("new.go", 20)}})
	flushAnalysisProgress(&model, true)
	assertNeutral := func() {
		t.Helper()
		for path, row := range model.files.Rows {
			if !row.editedAt.IsZero() || !row.scoreChangedAt.IsZero() || !row.newFileAt.IsZero() || row.movementDelta != 0 {
				t.Fatalf("scan delivery highlighted %s: %+v", path, row)
			}
		}
	}
	assertNeutral()
	handleAnalysisResult(&model, analysisResult{full: true, document: report.Document{Files: []report.File{testFile("existing.go", 35), testFile("new.go", 20)}}})
	assertNeutral()
	emit = beginAnalysisProgress(&model, report.FreshnessRefreshing, "source changed")
	queueChangedPaths(&model, []string{"existing.go"})
	emit(report.Document{Files: []report.File{testFile("existing.go", 40)}})
	flushAnalysisProgress(&model, true)
	handleAnalysisResult(&model, analysisResult{replace: []string{"existing.go"}, document: report.Document{Files: []report.File{testFile("existing.go", 40)}}})
	row := model.files.Rows["existing.go"]
	if row.editedAt.IsZero() || row.scoreChangedAt.IsZero() || row.direction != 1 {
		t.Fatal("actual source change lost its highlighting")
	}
}
