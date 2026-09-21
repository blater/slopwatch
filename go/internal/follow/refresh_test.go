package follow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
	tea "github.com/charmbracelet/bubbletea"
)

type refreshAnalyzer struct {
	document    report.Document
	targets     [][]string
	languages   [][]string
	changed     [][]string
	changesErr  error
	changesErrs []error
}

func (analyzer *refreshAnalyzer) Analyze(_ context.Context, targets, languages []string) (report.Document, error) {
	analyzer.targets = append(analyzer.targets, append([]string(nil), targets...))
	analyzer.languages = append(analyzer.languages, append([]string(nil), languages...))
	return analyzer.document, nil
}

func (analyzer *refreshAnalyzer) AnalyzeChanges(_ context.Context, paths []string) (report.Document, []string, error) {
	analyzer.changed = append(analyzer.changed, append([]string(nil), paths...))
	if len(analyzer.changesErrs) > 0 {
		err := analyzer.changesErrs[0]
		analyzer.changesErrs = analyzer.changesErrs[1:]
		if err != nil {
			return report.Document{}, nil, err
		}
	}
	if analyzer.changesErr != nil {
		return report.Document{}, nil, analyzer.changesErr
	}
	return analyzer.document, []string{"methods.go", "owner.go"}, nil
}

func TestIncrementalSourceRefreshReplacesAffectedPeersAndDiagnostics(t *testing.T) {
	old := report.Document{
		Files: []report.File{testFile("owner.go", 7), testFile("methods.go", 4), testFile("unrelated.go", 2)},
		Diagnostics: []map[string]any{
			{"path": "owner.go", "message": "old owner"},
			{"path": "unrelated.go", "message": "keep unrelated"},
		},
	}
	fresh := report.Document{
		Files: []report.File{testFile("owner.go", 0)},
		Diagnostics: []map[string]any{
			{"path": "owner.go", "message": "fresh owner"},
		},
	}
	analyzer := &refreshAnalyzer{document: fresh}
	model := refreshModel(t.TempDir(), old, analyzer)
	message := runSourceChangeAnalysis(t, &model)
	if message.full {
		t.Fatal("incremental source refresh became a full document replacement")
	}
	handleAnalysisResult(&model, message)
	assertIncrementalRefresh(t, model.files.Document)
}

func TestIncrementalDiagnosticsIgnoreInvocationIDs(t *testing.T) {
	previous := []map[string]any{{"message": "workspace warning", "invocation_id": "old"}}
	updated := []map[string]any{{"message": "workspace warning", "invocation_id": "new"}}
	merged := mergeDiagnostics(previous, updated, nil, nil)
	if len(merged) != 1 || merged[0]["message"] != "workspace warning" {
		t.Fatalf("diagnostics duplicated across invocations: %#v", merged)
	}
}

func TestIncrementalDiagnosticsReplaceUnitMetadata(t *testing.T) {
	previous := []map[string]any{{"unit_id": "go:pkg", "message": "old unit warning"}}
	updated := []map[string]any{{"unit_id": "go:pkg", "message": "new unit warning"}}
	plans := []map[string]any{{"unit_id": "go:pkg"}}
	merged := mergeDiagnostics(previous, updated, nil, plans)
	if len(merged) != 1 || merged[0]["message"] != "new unit warning" {
		t.Fatalf("unit diagnostics were not replaced: %#v", merged)
	}
}

func TestUnavailableIncrementalPlanRetainsResultsAndSurfacesError(t *testing.T) {
	analyzer := &refreshAnalyzer{changesErr: native.ErrIncrementalPlanUnavailable}
	model := refreshModel(t.TempDir(), report.Document{Files: []report.File{testFile("owner.go", 3)}}, analyzer)
	message := runSourceChangeAnalysis(t, &model)
	if message.full || !errors.Is(message.err, native.ErrIncrementalPlanUnavailable) || len(analyzer.targets) != 0 {
		t.Fatalf("unexpected fallback: %+v", message)
	}
	handleAnalysisResult(&model, message)
	if len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Score != 3 || model.runtimeError == "" {
		t.Fatal("failed update lost published result or error")
	}
}

func TestWorkspaceChurnRetriesOriginalIncrementalRequestWithoutError(t *testing.T) {
	analyzer := &refreshAnalyzer{
		document:    report.Document{Files: []report.File{testFile("owner.go", 3)}},
		changesErrs: []error{native.ErrWorkspaceChanged, native.ErrWorkspaceChanged},
	}
	model := refreshModel(t.TempDir(), report.Document{Files: []report.File{testFile("owner.go", 2)}}, analyzer)
	model.analyzing = true

	first, ok := analyzeExisting(model, []string{"methods.go"})().(analysisResult)
	if !ok || !errors.Is(first.err, native.ErrWorkspaceChanged) {
		t.Fatalf("initial churn result = %#v", first)
	}
	_, retry := model.Update(first)
	assertChurnWaiting(t, model, retry)

	_, command := model.Update(analysisRetry{})
	assertRetryStarted(t, model, command)
	_, stale := model.Update(analysisRetry{})
	if stale != nil || !model.analyzing {
		t.Fatalf("stale retry altered active analysis: command=%v analyzing=%t", stale, model.analyzing)
	}
	second, ok := command().(analysisResult)
	if !ok {
		t.Fatalf("retry command returned %T", second)
	}
	assertChurnResult(t, second, analyzer)
	_, retry = model.Update(second)
	assertChurnWaiting(t, model, retry)
	_, command = model.Update(analysisRetry{})
	assertRetryStarted(t, model, command)
	third, ok := command().(analysisResult)
	if !ok {
		t.Fatalf("successful retry result type = %T", third)
	}
	if third.err != nil {
		t.Fatalf("successful retry result = %#v", third)
	}
	_, command = model.Update(third)
	assertRecoveredAnalysis(t, model, command)
}

func assertChurnWaiting(t *testing.T, model Model, retry tea.Cmd) {
	t.Helper()
	if retry == nil {
		t.Fatal("workspace churn did not schedule a bounded retry")
	}
	if !model.analyzing {
		t.Fatal("workspace churn cleared analyzing while waiting for retry")
	}
	if model.runtimeError != "" {
		t.Fatalf("workspace churn surfaced runtime error: %q", model.runtimeError)
	}
	if len(model.files.Document.Files) != 1 {
		t.Fatalf("workspace churn replaced previous rows: %#v", model.files.Document.Files)
	}
	if model.files.Document.Files[0].Score != 2 {
		t.Fatalf("workspace churn changed previous score: %#v", model.files.Document.Files)
	}
}

func assertRetryStarted(t *testing.T, model Model, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("retry timer did not continue queued analysis")
	}
	if !model.analyzing {
		t.Fatal("retry timer cleared analyzing before queued analysis started")
	}
}

func assertChurnResult(t *testing.T, result analysisResult, analyzer *refreshAnalyzer) {
	t.Helper()
	if !errors.Is(result.err, native.ErrWorkspaceChanged) {
		t.Fatalf("retry error = %v", result.err)
	}
	if len(analyzer.changed) != 2 {
		t.Fatalf("retry analyzer calls = %d, want 2", len(analyzer.changed))
	}
	for index, paths := range analyzer.changed {
		if len(paths) != 1 || paths[0] != "methods.go" {
			t.Fatalf("retry %d paths = %v", index, paths)
		}
	}
}

func assertRecoveredAnalysis(t *testing.T, model Model, command tea.Cmd) {
	t.Helper()
	if command != nil {
		t.Fatal("successful retry scheduled unexpected follow-up")
	}
	if model.analyzing {
		t.Fatal("successful retry left analyzing set")
	}
	if model.runtimeError != "" {
		t.Fatalf("successful retry retained runtime error: %q", model.runtimeError)
	}
	if len(model.files.Document.Files) != 1 || model.files.Document.Files[0].Score != 3 {
		t.Fatalf("successful retry rows = %#v", model.files.Document.Files)
	}
}

func assertQueuedEdit(t *testing.T, model Model, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("edit during retry did not keep watcher active")
	}
	if !model.analyzing {
		t.Fatal("edit during retry cleared analyzing")
	}
	if len(model.queued) != 1 || !model.queued["later.go"] {
		t.Fatalf("edit during retry queue = %v", model.queued)
	}
}

func refreshModel(workspace string, document report.Document, analyzer Analyzer) Model {
	return Model{
		analyzer: analyzer,
		options:  Options{Workspace: workspace, Targets: []string{"."}},
		files: FilesState{
			Document: document, BaseDocument: document, Rows: map[string]rowState{}, Visible: defaultColumnVisibility(),
		},
		queued: map[string]bool{},
	}
}

func runSourceChangeAnalysis(t *testing.T, model *Model) analysisResult {
	t.Helper()
	_, command := handleSourceChange(model, sourceChange{Paths: []string{"methods.go"}})
	if command == nil {
		t.Fatal("source change did not schedule analysis")
	}
	result := command()
	if direct, ok := result.(analysisResult); ok {
		return direct
	}
	batch, ok := result.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("source change command = %T, want batch", result)
	}
	message, ok := batch[len(batch)-1]().(analysisResult)
	if !ok {
		t.Fatalf("source analysis returned %T", message)
	}
	return message
}

func assertIncrementalRefresh(t *testing.T, document report.Document) {
	t.Helper()
	owner := nativeRefreshFile(t, document, "owner.go")
	if owner.Score != 0 {
		t.Fatalf("affected peer score = %v, want 0", owner.Score)
	}
	if _, err := findReportFile(document, "methods.go"); err == nil {
		t.Fatal("deleted affected file remained in the document")
	}
	unrelated := nativeRefreshFile(t, document, "unrelated.go")
	if unrelated.Score != 2 {
		t.Fatalf("unrelated score = %v, want 2", unrelated.Score)
	}
	if len(document.Diagnostics) != 2 || document.Diagnostics[0]["message"] != "keep unrelated" || document.Diagnostics[1]["message"] != "fresh owner" {
		t.Fatalf("diagnostics after incremental refresh = %#v", document.Diagnostics)
	}
}

func findReportFile(document report.Document, path string) (report.File, error) {
	for _, file := range document.Files {
		if file.Path == path {
			return file, nil
		}
	}
	return report.File{}, fmt.Errorf("file %s not found", path)
}

func TestNativeSourceRefreshUpdatesCrossFileScores(t *testing.T) {
	workspace := nativeRefreshWorkspace(t)
	installationRoot := nativeRefreshInstallationRoot(t)
	analyzer := newNativeRefreshAnalyzer(t, workspace, installationRoot, "cache")
	initial := nativeRefreshAnalyze(t, analyzer)
	assertInitialCrossFileScore(t, initial)

	if err := os.WriteFile(filepath.Join(workspace, "methods.go"), []byte(nativeRefreshShortMethods()), 0o600); err != nil {
		t.Fatal(err)
	}
	model := Model{
		analyzer: analyzer,
		options:  Options{Workspace: workspace, Targets: []string{"."}},
		files: FilesState{
			Document: initial, BaseDocument: initial, Rows: map[string]rowState{}, Visible: defaultColumnVisibility(),
		},
		queued: map[string]bool{},
	}
	assertNativeMethodsRefresh(t, &model, workspace, installationRoot)
	assertNativeDeletionRefresh(t, &model, workspace, installationRoot)
}

func nativeRefreshInstallationRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func assertInitialCrossFileScore(t *testing.T, document report.Document) {
	t.Helper()
	if nativeRefreshFile(t, document, "owner.go").Score <= 0 {
		t.Fatalf("fixture did not establish a cross-file score: %v", nativeRefreshFile(t, document, "owner.go").Score)
	}
}

func assertNativeMethodsRefresh(t *testing.T, model *Model, workspace, installationRoot string) {
	t.Helper()
	want := nativeRefreshReference(t, workspace, installationRoot, "fresh-cache")
	message := runSourceChangeAnalysis(t, model)
	handleAnalysisResult(model, message)
	gotOwner := nativeRefreshFile(t, model.files.Document, "owner.go")
	if gotOwner.Score != nativeRefreshFile(t, want, "owner.go").Score {
		t.Fatalf("peer score after source refresh = %v, fresh full analysis = %v", gotOwner.Score, nativeRefreshFile(t, want, "owner.go").Score)
	}
}

func assertNativeDeletionRefresh(t *testing.T, model *Model, workspace, installationRoot string) {
	t.Helper()
	if err := os.Remove(filepath.Join(workspace, "methods.go")); err != nil {
		t.Fatal(err)
	}
	want := nativeRefreshReference(t, workspace, installationRoot, "fresh-delete-cache")
	message := runSourceChangeAnalysis(t, model)
	handleAnalysisResult(model, message)
	if _, err := findReportFile(model.files.Document, "methods.go"); err == nil {
		t.Fatal("deleted source file remained after incremental refresh")
	}
	if nativeRefreshFile(t, model.files.Document, "owner.go").Score != nativeRefreshFile(t, want, "owner.go").Score {
		t.Fatal("deletion refresh left the peer score stale")
	}
}

func newNativeRefreshAnalyzer(t *testing.T, workspace, installationRoot, cacheName string) *native.Analyzer {
	t.Helper()
	analyzer, err := native.New(workspace, installationRoot, native.Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true})
	if err != nil {
		t.Fatal(err)
	}
	analyzer.EnableCache(filepath.Join(t.TempDir(), cacheName))
	return analyzer
}

func nativeRefreshReference(t *testing.T, workspace, installationRoot, cacheName string) report.Document {
	t.Helper()
	return nativeRefreshAnalyze(t, newNativeRefreshAnalyzer(t, workspace, installationRoot, cacheName))
}

func nativeRefreshAnalyze(t *testing.T, analyzer *native.Analyzer) report.Document {
	t.Helper()
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func nativeRefreshWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module refreshrepro\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "owner.go"), []byte("package example\ntype Owner struct { value int }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "methods.go"), []byte(nativeRefreshLongMethods()), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func nativeRefreshLongMethods() string {
	const prefix = "package example\n"
	methods := ""
	for index := 0; index < 60; index++ {
		methods += fmt.Sprintf("func (o *Owner) Method%d(x int) int { if x > 0 { return x }; return o.value }\n", index)
	}
	return prefix + methods
}

func nativeRefreshShortMethods() string {
	return "package example\nfunc (o *Owner) Method0(x int) int { return x }\n"
}

func nativeRefreshFile(t *testing.T, document report.Document, path string) report.File {
	t.Helper()
	for _, file := range document.Files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("analysis omitted %s: %#v", path, document.Files)
	return report.File{}
}

func TestSuccessfulStartupIsOnlyFullCallAcrossSettingsErrorsAndQueuedChanges(t *testing.T) {
	analyzer := &refreshAnalyzer{document: report.Document{Files: []report.File{testFile("owner.go", 3), testFile("unrelated.go", 8)}}}
	model := refreshModel(t.TempDir(), report.Document{}, analyzer)
	model.StartInitialAnalysis()
	_, initial := handleWatcherReady(&model, watcherReady{})
	handleAnalysisResult(&model, initial().(analysisResult))
	if len(analyzer.targets) != 1 {
		t.Fatal("startup did not run exactly once")
	}
	if _, cmd := handleSourceChange(&model, sourceChange{IgnoreRules: true}); cmd != nil {
		t.Fatal("rule notice scheduled analysis")
	}
	model.preferencesPath = filepath.Join(t.TempDir(), "preferences.toml")
	model.toggleGitignore()
	model.syncTypeScriptTypes()
	failure := errors.New("watcher overflow")
	_, first := handleSourceChange(&model, sourceChange{Paths: []string{"methods.go"}, IgnoreRules: true, Err: failure})
	if first == nil || model.runtimeError == "" {
		t.Fatal("combined error/source batch was lost")
	}
	_, queued := handleSourceChange(&model, sourceChange{Paths: []string{"later.go"}})
	if queued != nil || !model.queued["later.go"] {
		t.Fatal("in-flight source batch was not queued")
	}
	_, next := handleAnalysisResult(&model, first().(analysisResult))
	if next == nil {
		t.Fatal("queued source changes were discarded")
	}
	handleAnalysisResult(&model, next().(analysisResult))
	if len(analyzer.targets) != 1 || len(analyzer.changed) != 2 || analyzer.changed[1][0] != "later.go" {
		t.Fatalf("full=%v incremental=%v", analyzer.targets, analyzer.changed)
	}
	if _, err := findReportFile(model.files.Document, "unrelated.go"); err != nil {
		t.Fatal("unrelated published result removed")
	}
	_, stopped := handleSourceChange(&model, sourceChange{Closed: true, Err: errors.New("watcher closed")})
	if stopped != nil || !model.runtime.watchStopped || len(analyzer.targets) != 1 {
		t.Fatal("closed watcher started another wait or scan")
	}
}
