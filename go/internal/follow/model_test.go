package follow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

type settingsAnalyzer struct {
	typeScriptTypes bool
	cacheReads      bool
	analyzeCalls    int
}

type catalogTestComponent struct {
	ID       string            `json:"component_id"`
	Axis     string            `json:"axis"`
	Support  map[string]string `json:"support"`
	Defaults struct {
		Enabled bool `json:"enabled"`
	} `json:"defaults"`
}

func (analyzer *settingsAnalyzer) SetTypeScriptTypes(enabled bool) {
	analyzer.typeScriptTypes = enabled
}

func (analyzer *settingsAnalyzer) SetCacheReads(enabled bool) {
	analyzer.cacheReads = enabled
}

func (analyzer *settingsAnalyzer) Analyze(context.Context, []string, []string) (report.Document, error) {
	analyzer.analyzeCalls++
	return report.Document{}, nil
}

func testFile(path string, score float64) report.File {
	return report.File{
		Path: path, Language: "go", Score: score, Complete: true,
		Components: map[string]report.Component{},
	}
}

func TestSearchInputsDoNotSilentlyClipLongQueries(t *testing.T) {
	model, err := New(report.Document{}, &settingsAnalyzer{}, Options{Workspace: t.TempDir(), Targets: []string{"."}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.Close)
	query := strings.Repeat("organisation/platform/component/", 16) + "target.go"
	model.source.findInput.SetValue(query)
	model.agents.FindInput.SetValue(query)
	if model.source.findInput.Value() != query || model.agents.FindInput.Value() != query {
		t.Fatalf("search query was clipped: files=%d jobs=%d want=%d", len(model.source.findInput.Value()), len(model.agents.FindInput.Value()), len(query))
	}
}

func TestJobHistoryIsVisibleByDefault(t *testing.T) {
	model, err := New(report.Document{}, &settingsAnalyzer{}, Options{Workspace: t.TempDir(), Targets: []string{"."}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.Close)
	if !model.agents.ShowAll {
		t.Fatal("finished jobs would disappear from the default Agents view")
	}
}

func TestArrowNavigationMovesImmediatelyWithoutATimer(t *testing.T) {
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{testFile("a.go", 2), testFile("b.go", 1)}},
			Rows:     map[string]rowState{},
			Visible:  map[string]bool{},
		},
		height: 10,
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if command != nil {
		t.Fatal("cursor movement unexpectedly scheduled deferred work")
	}
	result := updated.(*Model)
	if result.files.Cursor != 1 || result.files.Selected != "b.go" {
		t.Fatalf("cursor = %d, selected = %q", result.files.Cursor, result.files.Selected)
	}
}

func TestScanningStatusRendersAnimatedOnTopBar(t *testing.T) {
	model := Model{width: 80, height: 20, analyzing: true, animationFrame: 1}
	view := ansi.Strip(tableView(model))
	if !strings.Contains(view, "SCANNING") {
		t.Fatalf("initial scan status is missing: %q", view)
	}
	if !strings.Contains(view, "⠙") {
		t.Fatalf("initial scan animation frame is missing: %q", view)
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "SCANNING") {
		t.Fatalf("scanning status is not on the top line: %q", lines[0])
	}
	for _, line := range lines[1:] {
		if strings.Contains(line, "SCANNING") {
			t.Fatalf("scanning status still appears in the body: %q", line)
		}
	}
}

func TestStartupScanningIndicatorUsesFreshnessStatus(t *testing.T) {
	file := testFile("cached.go", 12)
	file.Freshness = report.FreshnessVerifying
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{file}},
			Rows:     map[string]rowState{file.Path: {}},
			Visible:  map[string]bool{},
		},
		width: 100, height: 20, analyzing: true, initialAnalysis: true, animationFrame: 1,
	}
	model.files.refreshFreshnessStatus()
	firstLine := strings.Split(ansi.Strip(tableView(model)), "\n")[0]
	if !strings.Contains(firstLine, "⠙ VERIFY 1") {
		t.Fatalf("freshness was not displayed as the animated scanning indicator: %q", firstLine)
	}
	if strings.Contains(firstLine, "SCANNING") || strings.Count(firstLine, "VERIFY 1") != 1 {
		t.Fatalf("startup rendered a separate or duplicate status: %q", firstLine)
	}
}

func TestInitialScanCompletionShowsTable(t *testing.T) {
	model := Model{
		files: FilesState{
			Rows:    map[string]rowState{},
			Visible: map[string]bool{},
		},
		width:           80,
		height:          10,
		analyzing:       true,
		initialAnalysis: true,
		options:         Options{Workspace: "/workspace"},
		queued:          map[string]bool{},
	}
	updated, _ := model.Update(analysisResult{
		full:     true,
		document: report.Document{Files: []report.File{testFile("main.go", 1)}},
	})
	result := updated.(*Model)
	view := ansi.Strip(result.View())
	if result.initialAnalysis || result.analyzing {
		t.Fatalf("initial scan state was not cleared: initial=%t analyzing=%t", result.initialAnalysis, result.analyzing)
	}
	if !strings.Contains(view, "main.go") {
		t.Fatalf("completed scan did not show its file: %q", view)
	}
}

func TestSuccessfulInitialScanEnablesCacheReadsAfterResultIsVisible(t *testing.T) {
	analyzer := &settingsAnalyzer{}
	model := Model{
		files: FilesState{
			Rows:    map[string]rowState{},
			Visible: map[string]bool{},
		},
		analyzer: analyzer, analyzing: true, initialAnalysis: true, queued: map[string]bool{},
	}
	updated, _ := model.Update(analysisResult{full: true, document: report.Document{}})
	result := updated.(*Model)
	if !analyzer.cacheReads {
		t.Fatal("successful initial scan did not enable subsequent cache reads")
	}
	if result.initialAnalysis || result.analyzing {
		t.Fatalf("initial scan state was not cleared: initial=%t analyzing=%t", result.initialAnalysis, result.analyzing)
	}
}

func TestFailedInitialScanDoesNotEnableCacheReads(t *testing.T) {
	analyzer := &settingsAnalyzer{}
	model := Model{
		files: FilesState{
			Rows:    map[string]rowState{},
			Visible: map[string]bool{},
		},
		analyzer: analyzer, analyzing: true, initialAnalysis: true, queued: map[string]bool{},
	}
	updated, _ := model.Update(analysisResult{full: true, err: fmt.Errorf("scan failed")})
	if analyzer.cacheReads {
		t.Fatal("failed initial scan enabled cache reads")
	}
	if got := updated.(*Model).runtimeError; got != "scan failed" {
		t.Fatalf("status = %q, want scan failure", got)
	}
}

func TestTableTopBarRightAlignsWorkspaceOneCharacterFromMargin(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 80, height: 10, options: Options{Workspace: "/workspace"}}
	lines := strings.Split(ansi.Strip(tableView(model)), "\n")
	if !strings.HasSuffix(lines[1], "/workspace ") {
		t.Fatalf("workspace is not right-aligned on the second title line: %q", lines[1])
	}
}

func TestTableTopBarSplitsBrandBranchAndWorkspaceAcrossTwoLines(t *testing.T) {
	model := Model{
		width: 100, height: 10,
		options:            Options{Workspace: "/workspace"},
		repositoryIdentity: "river:feature/display",
	}
	lines := strings.Split(ansi.Strip(tableView(model)), "\n")
	if !strings.HasPrefix(lines[0], tableLogo) || !strings.HasSuffix(lines[0], "river:feature/display ") {
		t.Fatalf("first title line does not show logo and branch: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "/workspace ") {
		t.Fatalf("second title line does not show the workspace: %q", lines[1])
	}
	brandStart := strings.Index(lines[1], tableWordmark)
	wantStart := (lipgloss.Width(tableLogo) - lipgloss.Width(tableWordmark)) / 2
	if brandStart < 0 || lipgloss.Width(lines[1][:brandStart]) != wantStart {
		t.Fatalf("%s is not centered below the logo: %q", tableWordmark, lines[1])
	}
}

func TestTableTopBarPaintsPaddingInsteadOfUsingTerminalBackground(t *testing.T) {
	ConfigureTerminalColours()
	left := lipgloss.NewStyle().Foreground(style.TextPrimary).Background(style.SurfaceTop).Render("LEFT")
	line := renderTableTitleLine(left, "RIGHT", 20)
	paintedGap := lipgloss.NewStyle().Foreground(style.TextMuted).Background(style.SurfaceTop).Render(strings.Repeat(" ", 10))
	if !strings.Contains(line, paintedGap) {
		t.Fatalf("title gap does not carry the themed background: %q", line)
	}
	if got := lipgloss.Width(line); got != 20 {
		t.Fatalf("painted title width = %d, want 20", got)
	}
}

func TestRepositoryIdentityFindsContainingRepository(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feature/display\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "src", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Base(root) + ":feature/display"
	if got := repositoryIdentity(nested); got != want {
		t.Fatalf("repository identity = %q, want %q", got, want)
	}
}

func TestFindSearchesMainTableAndAdvancesWithNext(t *testing.T) {
	files := []report.File{testFile("alpha.go", 3), testFile("beta.go", 2), testFile("gamma.go", 1)}
	document := report.Document{Files: files}
	document.SortAndRank()
	model := Model{
		files: FilesState{
			Document: document,
			Cursor:   0,
			Selected: "alpha.go",
			Rows:     map[string]rowState{},
			Visible:  map[string]bool{},
		},
		source: sourceState{findInput: textinput.New()},
	}
	model.source.findQuery = "a"
	findNext(&model, 1)
	if model.files.Selected != "beta.go" {
		t.Fatalf("first find selected %q, want beta.go", model.files.Selected)
	}
	findNext(&model, 1)
	if model.files.Selected != "gamma.go" {
		t.Fatalf("next find selected %q, want gamma.go", model.files.Selected)
	}
}

func TestFindUsesFWithSlashAsHiddenSynonym(t *testing.T) {
	for _, key := range []rune{'f', '/'} {
		model := Model{source: sourceState{findInput: textinput.New()}}
		updated, _ := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if !updated.(*Model).source.findOpen {
			t.Fatalf("%q did not open find", key)
		}
	}
}

func TestFindSearchesSourceAndMovesViewport(t *testing.T) {
	model := Model{
		source: sourceState{findSource: true, findQuery: "needle", searchText: "first\nfiller\nsecond needle\nlast", viewport: viewport.New(40, 2)},
	}
	model.source.viewport.SetContent(highlightSource("example.go", model.source.searchText, style.ThemeDark))
	findNext(&model, 1)
	if model.source.viewport.YOffset != 2 {
		t.Fatalf("source find offset = %d, want 2", model.source.viewport.YOffset)
	}
}

func TestTargetedMergeLeavesUnchangedRowsAlone(t *testing.T) {
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{testFile("a.go", 20), testFile("b.go", 10)}},
			Rows:     map[string]rowState{"a.go": {}, "b.go": {}},
			Visible:  map[string]bool{},
			Selected: "a.go",
		},
		height:  10,
		options: Options{TrendWindow: 15 * time.Minute},
	}
	model.files.Document.SortAndRank()
	merge(&model, analysisResult{
		document: report.Document{Files: []report.File{testFile("b.go", 30)}},
		replace:  []string{"b.go"},
	})
	if len(model.files.Document.Files) != 2 || model.files.Document.Files[0].Path != "b.go" || model.files.Document.Files[1].Path != "a.go" {
		t.Fatalf("unexpected targeted merge: %#v", model.files.Document.Files)
	}
	if model.files.Rows["b.go"].direction != 1 || model.files.Rows["b.go"].movementDelta != 1 || model.files.Rows["b.go"].scoreChangedAt.IsZero() {
		t.Fatalf("changed row movement was not recorded: %#v", model.files.Rows["b.go"])
	}
	if !model.files.Rows["a.go"].editedAt.IsZero() || model.files.Rows["a.go"].movementDelta != 0 || !model.files.Rows["a.go"].scoreChangedAt.IsZero() {
		t.Fatalf("targeted row history was not isolated: %#v", model.files.Rows)
	}
}

func TestTargetedMergeRefreshesDisplayedScoresForEveryChangedFile(t *testing.T) {
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{
				testFile("Repository.java", 125),
				testFile("Store.java", 125),
				testFile("Writer.java", 121),
			}},
			Rows: map[string]rowState{
				"Repository.java": {}, "Store.java": {}, "Writer.java": {},
			},
			Visible:     map[string]bool{},
			Selected:    "Store.java",
			SortKey:     "score",
			SortReverse: true,
		},
		options: Options{TrendWindow: 15 * time.Minute},
	}
	model.files.Document.SortAndRank()
	model.files.refreshDisplayFiles(model.options.Limit)

	merge(&model, analysisResult{
		document: report.Document{Files: []report.File{
			testFile("Store.java", 0),
			testFile("Writer.java", 0),
		}},
		replace: []string{"Store.java", "Writer.java"},
	})

	scores := map[string]float64{}
	for _, file := range model.files.displayFiles(model.options.Limit) {
		scores[file.Path] = file.Score
	}
	if scores["Repository.java"] != 125 || scores["Store.java"] != 0 || scores["Writer.java"] != 0 {
		t.Fatalf("displayed scores after merge = %#v", scores)
	}
}

func TestInitialFullMergeDoesNotMarkEveryFileAsNew(t *testing.T) {
	model := Model{
		files: FilesState{
			Document: report.Document{},
			Rows:     map[string]rowState{},
			Visible:  map[string]bool{},
		},
		options: Options{TrendWindow: 10 * time.Minute},
	}
	merge(&model, analysisResult{
		document: report.Document{Files: []report.File{testFile("existing.go", 10)}},
		full:     true,
	})
	if state := model.files.Rows["existing.go"]; !state.newFileAt.IsZero() {
		t.Fatalf("initial full scan marked existing.go as new: %#v", state)
	}
}

func TestUnchangedRowsPassedByChangedRowStayNeutral(t *testing.T) {
	files := []report.File{
		testFile("a.go", 60), testFile("b.go", 50), testFile("c.go", 40),
		testFile("d.go", 30), testFile("e.go", 20), testFile("f.go", 10),
	}
	model := Model{
		files: FilesState{
			Document: report.Document{Files: files},
			Rows:     map[string]rowState{},
			Visible:  map[string]bool{},
			Selected: "a.go",
		},
		options: Options{TrendWindow: 10 * time.Minute},
	}
	model.files.Document.SortAndRank()
	merge(&model, analysisResult{
		document: report.Document{Files: []report.File{testFile("f.go", 70)}},
		replace:  []string{"f.go"},
	})

	if got := model.files.Rows["f.go"].movementDelta; got != 5 {
		t.Fatalf("changed row movement = %d, want 5", got)
	}
	if got := movementArrow(model.files.Rows["f.go"].movementDelta); got != "⇈" {
		t.Fatalf("changed row arrow = %q, want ⇈", got)
	}
	for _, path := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		state := model.files.Rows[path]
		if state.movementDelta != 0 || !state.scoreChangedAt.IsZero() {
			t.Fatalf("unchanged passer %s received movement state: %#v", path, state)
		}
	}
}

func TestMovementArrowThresholds(t *testing.T) {
	for _, test := range []struct {
		delta int
		want  string
	}{
		{1, "↑"}, {4, "↑"}, {5, "⇈"}, {-1, "↓"}, {-4, "↓"}, {-5, "⇊"}, {0, ""},
	} {
		if got := movementArrow(test.delta); got != test.want {
			t.Errorf("movementArrow(%d) = %q, want %q", test.delta, got, test.want)
		}
	}
}

func TestMovementIndicatorExpiresWithTrendWindow(t *testing.T) {
	file := testFile("a.go", 1)
	model := Model{
		files: FilesState{
			Rows: map[string]rowState{"a.go": {
				scoreChangedAt: time.Now().Add(-2 * time.Minute), movementDelta: 1,
			}},
		},
		options: Options{TrendWindow: time.Minute},
	}
	if marker, _ := rowMarker(model, file, model.files.Rows[file.Path], time.Now()); marker != "" {
		t.Fatalf("expired movement indicator remains visible: %q", marker)
	}
}

func TestNewFileMarkerUsesRAGColoursAndExpires(t *testing.T) {
	now := time.Now()
	state := rowState{newFileAt: now}
	if marker, colour, ok := newFileMarker(90, 100, state, now); !ok || marker != "●" || colour != style.AccentPositive {
		t.Fatalf("initial new-file marker = %q, %q, %t; want green dot", marker, colour, ok)
	}
	state.newFileMoved = true
	if _, colour, _ := newFileMarker(50, 100, state, now); colour != style.AccentWarning {
		t.Fatalf("top-half new-file colour = %q, want amber", colour)
	}
	if _, colour, _ := newFileMarker(10, 100, state, now); colour != style.AccentCritical {
		t.Fatalf("top-ten-percent new-file colour = %q, want red", colour)
	}
	if _, _, ok := newFileMarker(10, 100, state, now.Add(newFileIndicatorWindow)); ok {
		t.Fatal("new-file marker remained active after its ten-minute window")
	}
}

func TestNewFileStateTransitionsFromGreenWhenItsRankChanges(t *testing.T) {
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{testFile("a.go", 20)}},
			Rows:     map[string]rowState{"a.go": {}},
			Visible:  map[string]bool{},
			Selected: "a.go",
		},
		options: Options{TrendWindow: 10 * time.Minute},
	}
	model.files.Document.SortAndRank()
	merge(&model, analysisResult{
		document: report.Document{Files: []report.File{testFile("b.go", 10)}},
		replace:  []string{"b.go"},
	})
	if state := model.files.Rows["b.go"]; state.newFileAt.IsZero() || state.newFileMoved {
		t.Fatalf("new file did not start in the green state: %#v", state)
	}

	merge(&model, analysisResult{
		document: report.Document{Files: []report.File{testFile("b.go", 30)}},
		replace:  []string{"b.go"},
	})
	state := model.files.Rows["b.go"]
	if !state.newFileMoved || state.movementDelta != 1 {
		t.Fatalf("new file rank transition was not recorded: %#v", state)
	}
	if marker, colour := rowMarker(model, model.files.Document.Files[0], state, time.Now()); marker != "●" || colour != style.AccentCritical {
		t.Fatalf("top-ranked new file marker = %q, %q; want red dot", marker, colour)
	}
}

func TestOverviewUsesMaximumRoutineMetric(t *testing.T) {
	file := testFile("a.go", 1)
	file.Components["cognitive_complexity"] = report.Component{
		Contribution: 4,
		Subjects:     []report.SubjectContribution{{Value: 8}, {Value: 21}, {Value: 13}},
	}
	value, exists, contribution := metric(file, "cog")
	if !exists || value != 21 || contribution != 4 {
		t.Fatalf("metric = %v, %t, %v; want 21, true, 4", value, exists, contribution)
	}
}

func TestOverviewShowsRawCouplingRatherThanThresholdedContribution(t *testing.T) {
	file := testFile("a.go", 0)
	file.Components["coupling_between_objects"] = report.Component{
		Contribution: 0,
		Subjects:     []report.SubjectContribution{{Value: 2}, {Value: 7}, {Value: 3}},
	}
	value, exists, contribution := metric(file, "coupling")
	if !exists || value != 7 || contribution != 0 {
		t.Fatalf("coupling metric = %v, %t, %v; want 7, true, 0", value, exists, contribution)
	}
}

func TestFailedCoverageRendersXAndSortsAfterMeasuredFiles(t *testing.T) {
	failed := failedCoverageFile()
	assertFailedCoverageCells(t, failed)
	assertFailedCoverageSort(t, failed)
}

func failedCoverageFile() report.File {
	failed := testFile("broken.ts", 0)
	failed.Complete = false
	failed.ValidZero = false
	failed.Coverage = map[string]string{}
	for _, id := range []string{"cognitive_complexity", "npath_complexity", "cyclomatic_method_complexity", "module_shallowness", "god_class", "coupling_between_objects"} {
		failed.Coverage[id] = "failed"
		failed.Components[id] = report.Component{Subjects: []report.SubjectContribution{{Value: 0}}}
	}
	return failed
}

func assertFailedCoverageCells(t *testing.T, failed report.File) {
	t.Helper()
	if _, available, _ := metric(failed, "cog"); available {
		t.Fatal("failed coverage was treated as a measured metric")
	}
	if rendered := ansi.Strip(renderMetricCell(failed, columnDefinitions[1], style.SurfaceScreen)); !strings.Contains(rendered, "X") {
		t.Fatalf("failed metric cell = %q, want X", rendered)
	}
	if rendered := ansi.Strip(modelRenderFixedColumns(failed)); !strings.Contains(rendered, "X") {
		t.Fatalf("failed score cell = %q, want X", rendered)
	}
	if got := strings.Count(ansi.Strip(modelRenderFixedColumns(failed)), "X"); got < 7 {
		t.Fatalf("failed displayed columns = %d X values, want score plus metrics", got)
	}
}

func assertFailedCoverageSort(t *testing.T, failed report.File) {
	t.Helper()
	measured := testFile("measured.go", 4)
	measured.Components["cognitive_complexity"] = report.Component{Subjects: []report.SubjectContribution{{Value: 4}}}
	model := Model{
		files: FilesState{
			SortKey: "cog",
		}}
	if !filesLess(model.files.SortKey, model.files.SortReverse, measured, failed) || filesLess(model.files.SortKey, model.files.SortReverse, failed, measured) {
		t.Fatal("failed metric did not sort after measured values")
	}
	model.files.SortReverse = true
	if !filesLess(model.files.SortKey, model.files.SortReverse, measured, failed) || filesLess(model.files.SortKey, model.files.SortReverse, failed, measured) {
		t.Fatal("failed metric did not remain last in reverse sort")
	}
}

func modelRenderFixedColumns(file report.File) string {
	return renderFixedColumns(Model{
		files: FilesState{
			Visible: defaultColumnVisibility(),
		}}, file, rowState{}, style.SurfaceScreen)
}

func TestSortingKeepsFilenameTieBreaksDeterministicInBothDirections(t *testing.T) {
	left := testFile("a.go", 5)
	right := testFile("b.go", 5)
	model := Model{
		files: FilesState{
			SortKey: "score",
		}}
	if !filesLess(model.files.SortKey, model.files.SortReverse, left, right) || filesLess(model.files.SortKey, model.files.SortReverse, right, left) {
		t.Fatal("ascending numeric tie did not use filename")
	}
	model.files.SortReverse = true
	if !filesLess(model.files.SortKey, model.files.SortReverse, left, right) || filesLess(model.files.SortKey, model.files.SortReverse, right, left) {
		t.Fatal("descending numeric tie lost ascending filename order")
	}
	model.files.SortKey = "filename"
	model.files.SortReverse = false
	if !filesLess(model.files.SortKey, model.files.SortReverse, left, right) || filesLess(model.files.SortKey, model.files.SortReverse, right, left) {
		t.Fatal("ascending filename sort is not deterministic")
	}
	model.files.SortReverse = true
	if !filesLess(model.files.SortKey, model.files.SortReverse, right, left) || filesLess(model.files.SortKey, model.files.SortReverse, left, right) {
		t.Fatal("descending filename sort is not deterministic")
	}
}

func TestInfoShowsMatchingAnalysisDiagnostic(t *testing.T) {
	file := testFile("broken.ts", 0)
	file.Complete = false
	file.Coverage = map[string]string{"cognitive_complexity": "failed"}
	file.Components = map[string]report.Component{"cognitive_complexity": {}}
	model := Model{
		files: FilesState{
			Selected: file.Path,
			Document: report.Document{Files: []report.File{file}, Diagnostics: []map[string]any{{
				"path": file.Path, "code": "typescript.syntax.12", "message": "Unexpected token", "line": float64(12), "column": float64(5),
			}}},
		},
		width:   80,
		infoKey: "cog",
	}
	text := ansi.Strip(infoView(model))
	detail := ansi.Strip(strings.Join(detailContent(model, file, 60), "\n"))
	if strings.Contains(text, "Unexpected token") {
		t.Fatalf("metric help unexpectedly absorbed file diagnostics: %q", text)
	}
	if !strings.Contains(detail, "typescript.syntax.12") || !strings.Contains(detail, "Unexpected token (12:5)") || !strings.Contains(detail, "score X") {
		t.Fatalf("file detail omitted diagnostic or failed score: %q", detail)
	}
}

func TestLongDiagnosticWrapsInsideScrollableFileDetail(t *testing.T) {
	file := testFile("broken.ts", 0)
	file.Complete = false
	file.Coverage = map[string]string{"cognitive_complexity": "failed"}
	model := Model{
		files: FilesState{
			Selected: file.Path,
			Document: report.Document{Files: []report.File{file}, Diagnostics: []map[string]any{{
				"path": file.Path, "code": "typescript.syntax.12", "message": strings.Repeat("long parser detail ", 12) + "tail", "line": float64(12),
			}}},
		},
		width:  40,
		height: 8,
	}
	lines := detailContent(model, file, 20)
	if len(lines) < 4 || !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "tail") {
		t.Fatalf("long diagnostic was not wrapped in detail: %q", ansi.Strip(strings.Join(lines, "\n")))
	}
	if detailMaxOffset(model) == 0 {
		t.Fatal("wrapped diagnostic did not contribute to detail scrolling")
	}
}

func TestTableFillsAvailableHeight(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{testFile("a.go", 1)}},
			Rows:     map[string]rowState{"a.go": {}},
			Visible:  map[string]bool{},
		},
		width:   100,
		height:  12,
		options: Options{Workspace: "/workspace"},
	}
	if got := strings.Count(model.View(), "\n") + 1; got != model.height {
		t.Fatalf("rendered %d lines, want terminal height %d", got, model.height)
	}
}

func TestSelectedRowCarriesReferenceBackgroundAcrossEveryCell(t *testing.T) {
	ConfigureTerminalColours()
	file := testFile("parent/with/a/long/path/to/example.go", 12)
	model := Model{
		files: FilesState{
			HorizontalOffset: 7,
			Rows:             map[string]rowState{file.Path: {}},
			Visible:          map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
		},
		width: 80,
	}
	row := renderRow(model, file, true)
	// termenv rounds the green channel of #245a78 down by one while encoding it.
	// Match the common RGB payload whether or not a foreground shares its CSI.
	wantBackground := "48;2;36;89;120m"
	if !strings.Contains(row, wantBackground) {
		t.Fatalf("selected row does not use the reference background: %q", row)
	}
	plainSegments := strings.Split(row, "\x1b[0m")
	for _, segment := range plainSegments {
		if segment == "" {
			continue
		}
		if !strings.Contains(segment, wantBackground) {
			t.Fatalf("selected-row segment lost its background: %q", segment)
		}
	}
	if got := lipgloss.Width(row); got != model.width {
		t.Fatalf("selected row width = %d, want %d", got, model.width)
	}
}

func TestCachedFreshnessIsVisibleInRowsAndDetails(t *testing.T) {
	ConfigureTerminalColours()
	for _, test := range []struct {
		freshness report.Freshness
		marker    string
		label     string
	}{
		{report.FreshnessProvisional, "◌", "PROVISIONAL"},
		{report.FreshnessVerifying, "◌", "VERIFYING"},
		{report.FreshnessRefreshing, "↻", "REFRESHING"},
		{report.FreshnessStaleError, "!", "STALE ERROR"},
	} {
		file := testFile("cached.go", 12)
		file.Freshness = test.freshness
		file.FreshnessNote = "background reconciliation"
		model := Model{
			files: FilesState{
				Document: report.Document{Files: []report.File{file}},
				Rows:     map[string]rowState{file.Path: {}},
				Visible:  map[string]bool{},
			},
			width:   80,
			height:  20,
			options: Options{TrendWindow: time.Minute},
		}
		if row := ansi.Strip(renderRow(model, file, false)); !strings.Contains(row, test.marker) {
			t.Errorf("%s row has no %q marker: %q", test.freshness, test.marker, row)
		}
		detail := ansi.Strip(strings.Join(detailContent(model, file, 70), "\n"))
		if !strings.Contains(detail, test.label) || !strings.Contains(detail, file.FreshnessNote) {
			t.Errorf("%s detail does not disclose freshness: %q", test.freshness, detail)
		}
	}
}

func TestMainScreenSummarizesCachedFreshness(t *testing.T) {
	files := []report.File{
		testFile("current.go", 3), testFile("provisional.go", 2), testFile("stale.go", 1),
	}
	files[0].Freshness = report.FreshnessCurrent
	files[1].Freshness = report.FreshnessProvisional
	files[2].Freshness = report.FreshnessStaleError
	model := Model{
		files: FilesState{
			Document: report.Document{Files: files},
			Rows:     map[string]rowState{},
			Visible:  map[string]bool{},
		},
		width:   120,
		height:  10,
		options: Options{Workspace: "/workspace", TrendWindow: time.Minute},
	}
	model.files.refreshFreshnessStatus()
	firstLine := strings.Split(ansi.Strip(tableView(model)), "\n")[0]
	for _, want := range []string{"STALE 1"} {
		if !strings.Contains(firstLine, want) {
			t.Fatalf("top status missing %q: %q", want, firstLine)
		}
	}
}

func TestHorizontalScrollMovesOnlyFileNames(t *testing.T) {
	ConfigureTerminalColours()
	file := testFile("a/very/long/source/path/that/exceeds/the/available/filename/viewport/example.go", 12)
	file.Components["cognitive_complexity"] = report.Component{Contribution: 3, Subjects: []report.SubjectContribution{{Value: 7}}}
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{file}},
			Rows:     map[string]rowState{file.Path: {}},
			Visible:  map[string]bool{"cog": true},
		},
		width: 52,
	}
	before := ansi.Strip(renderRow(model, file, true))
	fixedWidth := model.width - model.pathViewportWidth()
	model.movePath(6)
	after := ansi.Strip(renderRow(model, file, true))
	if model.files.HorizontalOffset != 6 {
		t.Fatalf("path offset = %d, want 6", model.files.HorizontalOffset)
	}
	if got, want := ansi.Cut(after, 0, fixedWidth), ansi.Cut(before, 0, fixedWidth); got != want {
		t.Fatalf("fixed metric columns moved: before %q, after %q", want, got)
	}
	if ansi.Cut(after, fixedWidth, model.width) == ansi.Cut(before, fixedWidth, model.width) {
		t.Fatalf("filename did not scroll: before %q, after %q", before, after)
	}
	if lipgloss.Width(renderRow(model, file, true)) != model.width {
		t.Fatal("horizontal scrolling changed the row width")
	}
}

func TestHorizontalScrollDoesNotChangeVerticalSelection(t *testing.T) {
	first := testFile("one/very/long/path/that/needs/horizontal/scrolling/first.go", 2)
	second := testFile("two/very/long/path/that/needs/horizontal/scrolling/second.go", 1)
	model := Model{
		files: FilesState{
			Cursor:   0,
			Selected: first.Path,
			Document: report.Document{Files: []report.File{first, second}},
			Rows:     map[string]rowState{first.Path: {}, second.Path: {}},
			Visible:  map[string]bool{},
		},
		width:  36,
		height: 8,
	}
	handleKey(&model, tea.KeyMsg{Type: tea.KeyRight})
	if model.files.HorizontalOffset != pathScrollStep || model.files.Cursor != 0 || model.files.Offset != 0 || model.files.Selected != first.Path {
		t.Fatalf("horizontal scroll changed vertical state: path=%d cursor=%d offset=%d selected=%q", model.files.HorizontalOffset, model.files.Cursor, model.files.Offset, model.files.Selected)
	}
	handleKey(&model, tea.KeyMsg{Type: tea.KeyDown})
	if model.files.HorizontalOffset != pathScrollStep || model.files.Cursor != 1 || model.files.Selected != second.Path {
		t.Fatalf("vertical movement regressed after horizontal scroll: path=%d cursor=%d selected=%q", model.files.HorizontalOffset, model.files.Cursor, model.files.Selected)
	}
}

func TestOverviewUsesReferencePalette(t *testing.T) {
	palette := map[string]lipgloss.Color{
		"text": style.TextPrimary, "muted": style.TextMuted, "green": style.AccentPositive,
		"amber": style.AccentWarning, "red": style.AccentCritical, "score amber": style.ScoreWarning,
		"score red": style.ScoreCritical, "blue": style.AccentInfo, "selection": style.SurfaceSelected,
		"screen": style.SurfaceScreen, "top": style.SurfaceTop,
		"header": style.SurfaceHeader, "footer": style.SurfaceFooter,
	}
	want := map[string]string{
		"text": "#d5e2eb", "muted": "#668298", "green": "#58e7ad",
		"amber": "#f0c765", "red": "#ff8291", "score amber": "#f5c451",
		"score red": "#ff6174", "blue": "#6fb9e8", "selection": "#245a78",
		"screen": "#071019", "top": "#0a1622",
		"header": "#0b1e2d", "footer": "#061019",
	}
	for role, colour := range palette {
		if string(colour) != want[role] {
			t.Errorf("%s colour = %s, want %s", role, colour, want[role])
		}
	}
}

func TestFooterOnlyAdvertisesUsefulActions(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 80}
	footer := footer(model)
	for _, unwanted := range []string{"Enter", "details", "space", "pause", "columns"} {
		if strings.Contains(footer, unwanted) {
			t.Errorf("footer still contains %q: %q", unwanted, footer)
		}
	}
	if !strings.Contains(ansi.Strip(footer), "sort") {
		t.Fatalf("footer does not advertise sorting: %q", footer)
	}
	if !strings.Contains(ansi.Strip(footer), "settings") {
		t.Fatalf("footer does not advertise settings: %q", footer)
	}
	if !strings.Contains(ansi.Strip(footer), "info") {
		t.Fatalf("footer does not advertise file info: %q", footer)
	}
	if !strings.Contains(ansi.Strip(footer), "o") {
		t.Fatalf("footer does not advertise the o sort shortcut: %q", footer)
	}
}

func TestMainInfoKeyOpensTheSamePageAsEnter(t *testing.T) {
	file := report.File{Path: "main.go", Complete: true, Components: map[string]report.Component{}}
	base := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{file}},
		}}

	enterModel := base
	handleKey(&enterModel, tea.KeyMsg{Type: tea.KeyEnter})
	infoModel := base
	handleKey(&infoModel, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

	if !enterModel.detail || !infoModel.detail {
		t.Fatalf("detail state differs: enter=%t info=%t", enterModel.detail, infoModel.detail)
	}
	if enterModel.View() != infoModel.View() {
		t.Fatal("i and Enter opened different file information pages")
	}
}

func TestFooterPlacesGenericActionsOnTheRightWithoutOverlap(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 100}
	text := ansi.Strip(footer(model))
	left := strings.Index(text, "sort")
	right := strings.Index(text, "settings")
	if left < 0 || right < 0 || left >= right {
		t.Fatalf("footer groups are not ordered left-to-right: %q", text)
	}
	if !strings.Contains(text, "help") || !strings.Contains(text, "quit") {
		t.Fatalf("footer omitted generic actions: %q", text)
	}
	if strings.Index(text, "find") > strings.Index(text, "next") {
		t.Fatalf("find and next are out of order: %q", text)
	}
}

func TestFooterDropsGenericActionsBeforeLeftActionsOnNarrowScreens(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 30}
	text := ansi.Strip(footer(model))
	if !strings.Contains(text, "mark") || !strings.Contains(text, "clear") {
		t.Fatalf("narrow footer dropped permanent marking actions: %q", text)
	}
	if !strings.Contains(text, "find") || !strings.Contains(text, "next") {
		t.Fatalf("narrow footer dropped Find/Next: %q", text)
	}
	if strings.Contains(text, "settings") && strings.Index(text, "settings") < strings.Index(text, "clear") {
		t.Fatalf("generic actions overlap the left group: %q", text)
	}
}

func TestSettingsOpensWeightsAndAdjustsScore(t *testing.T) {
	component := report.Component{
		Contribution: 10,
		Subjects:     []report.SubjectContribution{{Subject: "x", Value: 20, Contribution: 10}},
	}
	base := report.Document{Files: []report.File{{
		Path: "a.go", Complete: true, Score: 10,
		Components: map[string]report.Component{"cognitive_complexity": component},
	}}}
	model := Model{
		files: FilesState{
			Document:     base,
			BaseDocument: base,
		}, weights: defaultWeights()}
	updated, _ := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	result := updated.(*Model)
	if !result.settings || result.weightsOpen {
		t.Fatal("s did not open settings")
	}
	openSetting(result, "analysis")
	result.settingsCursor = 1
	updated, _ = handleKey(result, tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	if !result.weightsOpen || result.settings {
		t.Fatal("Enter did not open weights")
	}
	for index, item := range componentWeights {
		if item.id == "cognitive_complexity" {
			result.weightCursor = index
			break
		}
	}
	handleWeightsKey(result, "left")
	if result.weights["cognitive_complexity"] != 9.5 {
		t.Fatalf("weight = %v, want 9.5", result.weights["cognitive_complexity"])
	}
	if result.files.Document.Files[0].Score != 9.5 {
		t.Fatalf("score = %v, want 9.5", result.files.Document.Files[0].Score)
	}
}

func TestWeightsResetCurrentAndAll(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible: defaultColumnVisibility(),
		}, weights: defaultWeights()}
	model.weightCursor = 0
	model.weights["cognitive_complexity"] = 2
	handleWeightsKey(&model, "r")
	if got := model.weights["cognitive_complexity"]; got != 10 {
		t.Fatalf("reset current weight = %v, want 10", got)
	}
	model.weights["cognitive_complexity"] = 2
	model.weights["god_class"] = 19
	handleWeightsKey(&model, "c")
	if !model.weightsResetConfirm {
		t.Fatal("reset all did not ask for confirmation")
	}
	handleWeightsKey(&model, "n")
	if model.weights["cognitive_complexity"] != 2 || model.weights["god_class"] != 19 {
		t.Fatal("cancelled reset all changed weights")
	}
	handleWeightsKey(&model, "c")
	handleWeightsKey(&model, "y")
	if model.weights["cognitive_complexity"] != 10 || model.weights["god_class"] != 1 {
		t.Fatalf("reset all weights = %v, %v", model.weights["cognitive_complexity"], model.weights["god_class"])
	}
}

func TestWeightsAndHelpOpenTheSharedInfoPopup(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible: defaultColumnVisibility(),
		}, weights: defaultWeights(), width: 80, height: 20}
	model.weightCursor = 0
	handleWeightsKey(&model, "i")
	if !model.infoOpen || model.infoKey != "cog" || !strings.Contains(ansi.Strip(infoView(model)), "COG") {
		t.Fatal("weights info did not open the shared COG popup")
	}
	handleInfoKey(&model, "esc")
	before := isWeightEnabled(model, componentWeights[model.weightCursor].id)
	handleWeightsKey(&model, "enter")
	if model.infoOpen || isWeightEnabled(model, componentWeights[model.weightCursor].id) == before {
		t.Fatal("Enter did not toggle the weight checkbox")
	}
	model.infoOpen = false
	model.help = true
	model.helpTopic = helpScoring
	model.helpCursor = 1
	handleHelpKey(&model, "i")
	if !model.infoOpen || model.infoKey != "cog" {
		t.Fatal("help info did not open the shared COG popup")
	}
	handleInfoKey(&model, "esc")
	if !model.help {
		t.Fatal("closing info unexpectedly closed help")
	}
}

func TestCheckboxListsAcceptEnterAndSpace(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible: defaultColumnVisibility(),
		}, weights: defaultWeights(), weightEnabled: defaultWeightEnabled()}
	model.columnCursor = columnIndex("cog")
	columnBefore := model.files.Visible["cog"]
	handleColumnKey(&model, "enter")
	if model.files.Visible["cog"] == columnBefore {
		t.Fatal("Enter did not toggle a column checkbox")
	}
	handleColumnKey(&model, " ")
	if model.files.Visible["cog"] != columnBefore {
		t.Fatal("Space did not toggle the column checkbox")
	}

	model.weightCursor = componentIndex("cognitive_complexity")
	weightBefore := isWeightEnabled(model, "cognitive_complexity")
	handleWeightsKey(&model, "enter")
	if isWeightEnabled(model, "cognitive_complexity") == weightBefore {
		t.Fatal("Enter did not toggle a weight checkbox")
	}
	handleWeightsKey(&model, " ")
	if isWeightEnabled(model, "cognitive_complexity") != weightBefore {
		t.Fatal("Space did not toggle the weight checkbox")
	}
}

func TestEnterClosesPurelyInformationalDialog(t *testing.T) {
	model := Model{infoOpen: true, infoKey: "cog"}
	handleInfoKey(&model, "enter")
	if model.infoOpen {
		t.Fatal("Enter did not close the informational dialog")
	}
}

func TestEnterDoesNotCloseHelpDialogWithOptions(t *testing.T) {
	model := Model{help: true, helpTopic: helpScoring, helpCursor: 0}
	handleHelpKey(&model, "enter")
	if !model.help || !model.infoOpen {
		t.Fatal("Enter did not preserve Help while opening its info option")
	}
}

func TestInfoPopupOverlaysItsParentPopup(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible: defaultColumnVisibility(),
		}, width: 80, height: 20, weights: defaultWeights(), weightsOpen: true, infoOpen: true, infoKey: "cog"}
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "WEIGHTS") || !strings.Contains(view, "COG  cognitive complexity") {
		t.Fatalf("info did not overlay the weights popup: %q", view)
	}
	model = Model{width: 80, height: 20, help: true, helpTopic: helpScoring, helpCursor: 1, infoOpen: true, infoKey: "cog"}
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "COG  cognitive complexity") {
		t.Fatalf("info did not overlay the help popup: %q", view)
	}
}

func TestHelpScrollsAndHasTopicHint(t *testing.T) {
	model := Model{help: true, helpTopic: helpScoring, helpCursor: len(metricInformation) - 1, width: 60, height: 10}
	view := ansi.Strip(helpView(model))
	if !strings.Contains(view, "PATH") || strings.Contains(view, "SCORE") {
		t.Fatalf("help did not scroll to the selected entry: %q", view)
	}
	if !strings.Contains(view, "topics") {
		t.Fatalf("help is missing its return-to-topics hint: %q", view)
	}
	if !strings.Contains(view, "info") {
		t.Fatalf("help is missing its info option: %q", view)
	}
}

func TestWeightsViewGroupsIndentedMetricsByCategory(t *testing.T) {
	view := ansi.Strip(weightsView(Model{weights: defaultWeights()}))
	for _, text := range []string{"Structural", "  COG", "  CYCLO", "  NPATH", "  SHALLOW", "Type safety", "Ambiguous boolean"} {
		if !strings.Contains(view, text) {
			t.Errorf("weights view does not contain %q", text)
		}
	}
	lines := strings.Split(view, "\n")
	lineWith := func(text string) string {
		for _, line := range lines {
			if strings.Contains(line, text) {
				return line
			}
		}
		return ""
	}
	categoryLine := strings.TrimPrefix(lineWith("COG"), "│")
	metricLine := strings.TrimPrefix(lineWith("Cognitive complexity"), "│")
	if !strings.HasPrefix(categoryLine, "   COG") || !strings.HasPrefix(metricLine, "     [✓]") {
		t.Fatalf("measure hierarchy is not indented: %q / %q", lineWith("COG"), lineWith("Cognitive complexity"))
	}
}

func TestEveryWeightSettingMapsToAnEnabledCatalogComponent(t *testing.T) {
	components := readTestCatalog(t)
	byID := make(map[string]catalogTestComponent, len(components))
	for _, component := range components {
		byID[component.ID] = component
	}
	assertWeightSettingsMatchCatalog(t, byID)
}

func readTestCatalog(t *testing.T) []catalogTestComponent {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "component-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Components []catalogTestComponent `json:"components"`
	}
	if err := json.Unmarshal(payload, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog.Components
}

func assertWeightSettingsMatchCatalog(t *testing.T, byID map[string]catalogTestComponent) {
	t.Helper()
	seen := map[string]bool{}
	for _, setting := range componentWeights {
		if seen[setting.id] {
			t.Errorf("duplicate weight setting %s", setting.id)
		}
		seen[setting.id] = true
		component, exists := byID[setting.id]
		if !exists {
			t.Errorf("weight setting %s has no catalog component", setting.id)
			continue
		}
		assertWeightSetting(t, setting.id, setting.axis, component)
	}
}

func assertWeightSetting(t *testing.T, id, axis string, component catalogTestComponent) {
	t.Helper()
	if !component.Defaults.Enabled {
		t.Errorf("weight setting %s exposes a disabled catalog component", id)
	}
	if component.Axis != axis {
		t.Errorf("weight setting %s axis = %s, catalog = %s", id, axis, component.Axis)
	}
	for _, level := range component.Support {
		if level == "supported" || level == "conformant" || level == "best_effort" {
			return
		}
	}
	t.Errorf("weight setting %s is unsupported by every language", id)
}

func TestTypeSafetySettingsEnableAnalysisAndScheduleRefresh(t *testing.T) {
	for _, test := range []struct {
		name          string
		selectSetting func(*Model)
		apply         func(*Model) tea.Cmd
	}{
		{
			name:          "column",
			selectSetting: selectTypeSafetyColumn,
			apply: func(model *Model) tea.Cmd {
				_, command := handleColumnKey(model, " ")
				return command
			},
		},
		{
			name:          "individual weight",
			selectSetting: selectExplicitAnyWeight,
			apply: func(model *Model) tea.Cmd {
				_, command := handleWeightsKey(model, " ")
				return command
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertTypeSafetyToggle(t, test.selectSetting, test.apply)
		})
	}
}

func selectTypeSafetyColumn(model *Model) {
	for index, column := range columnNames() {
		if column.key == "typesafety" {
			model.columnCursor = index
			return
		}
	}
}

func selectExplicitAnyWeight(model *Model) {
	for index, item := range componentWeights {
		if item.id == "explicit_any" {
			model.weightCursor = index
			return
		}
	}
}

func assertTypeSafetyToggle(t *testing.T, selectSetting func(*Model), apply func(*Model) tea.Cmd) {
	t.Helper()
	analyzer := &settingsAnalyzer{}
	model := &Model{
		files: FilesState{
			Visible: defaultColumnVisibility(),
		},
		analyzer:      analyzer,
		weights:       defaultWeights(),
		weightEnabled: defaultWeightEnabled(),
		queued:        map[string]bool{},
	}
	selectSetting(model)
	command := apply(model)
	if !analyzer.typeScriptTypes {
		t.Fatal("setting did not enable compiler-aware TypeScript analysis")
	}
	if !model.analyzing || command == nil {
		t.Fatalf("refresh was not scheduled: analyzing=%t command=%v", model.analyzing, command)
	}
	if _, ok := command().(analysisResult); !ok || analyzer.analyzeCalls != 1 {
		t.Fatalf("refresh command did not run analysis: calls=%d", analyzer.analyzeCalls)
	}
}

func TestTypeSafetyRefreshQueuesBehindAnAnalysisAndDisablingNeedsNoRefresh(t *testing.T) {
	analyzer := &settingsAnalyzer{}
	model := &Model{
		files: FilesState{
			Visible: defaultColumnVisibility(),
		},
		analyzer:      analyzer,
		weights:       defaultWeights(),
		weightEnabled: defaultWeightEnabled(),
		queued:        map[string]bool{},
		analyzing:     true,
	}
	selectTypeSafetyColumn(model)
	_, command := handleColumnKey(model, " ")
	assertQueuedTypeSafetyEnable(t, model, analyzer, command)
	_, command = model.Update(analysisResult{document: report.Document{}, full: true})
	assertQueuedTypeSafetyRefresh(t, model, command)
	model.analyzing = false
	_, command = handleColumnKey(model, " ")
	assertTypeSafetyDisabled(t, model, analyzer, command)
}

func assertQueuedTypeSafetyEnable(t *testing.T, model *Model, analyzer *settingsAnalyzer, command tea.Cmd) {
	t.Helper()
	if command != nil || !model.pendingFullAnalysis || !analyzer.typeScriptTypes {
		t.Fatalf("enable during analysis = command %v, pending %t, enabled %t", command, model.pendingFullAnalysis, analyzer.typeScriptTypes)
	}
}

func assertQueuedTypeSafetyRefresh(t *testing.T, model *Model, command tea.Cmd) {
	t.Helper()
	if command == nil || model.pendingFullAnalysis || !model.analyzing {
		t.Fatalf("queued refresh = command %v, pending %t, analyzing %t", command, model.pendingFullAnalysis, model.analyzing)
	}
}

func assertTypeSafetyDisabled(t *testing.T, model *Model, analyzer *settingsAnalyzer, command tea.Cmd) {
	t.Helper()
	if command != nil || analyzer.typeScriptTypes || model.files.Visible["typesafety"] {
		t.Fatalf("disable = command %v, analyzer enabled %t, visible %t", command, analyzer.typeScriptTypes, model.files.Visible["typesafety"])
	}
}

func TestWeightsEnablementControlsScoreIndependently(t *testing.T) {
	component := report.Component{Contribution: 10}
	base := report.Document{Files: []report.File{{Path: "a.go", Complete: true, Components: map[string]report.Component{
		"cognitive_complexity": component,
	}}}}
	model := Model{
		files: FilesState{
			Document:     base,
			BaseDocument: base,
		}, weights: defaultWeights(), weightEnabled: defaultWeightEnabled()}
	rebuildWeightedDocument(&model)
	if got := model.files.Document.Files[0].Score; got != 10 {
		t.Fatalf("enabled weight score = %v, want 10", got)
	}
	handleWeightsKey(&model, " ")
	if isWeightEnabled(model, "cognitive_complexity") || model.files.Document.Files[0].Score != 0 {
		t.Fatalf("space did not disable weight: enabled=%t score=%v", isWeightEnabled(model, "cognitive_complexity"), model.files.Document.Files[0].Score)
	}
	handleWeightsKey(&model, " ")
	if !isWeightEnabled(model, "cognitive_complexity") || model.files.Document.Files[0].Score != 10 {
		t.Fatalf("space did not re-enable weight: enabled=%t score=%v", isWeightEnabled(model, "cognitive_complexity"), model.files.Document.Files[0].Score)
	}
}

func TestWeightEnablementIncludesAndExcludesEveryApplicableComponent(t *testing.T) {
	t.Run("typescript", func(t *testing.T) {
		assertWeightScenario(t, "typescript", func(string) bool { return true })
	})
	t.Run("java", func(t *testing.T) {
		assertWeightScenario(t, "java", func(axis string) bool { return axis != "typescript_type_safety" })
	})
}

func assertWeightScenario(t *testing.T, language string, include func(string) bool) {
	t.Helper()
	components := make(map[string]report.Component)
	applicable := make([]string, 0, len(componentWeights))
	model := Model{weights: defaultWeights(), weightEnabled: map[string]bool{}}
	for _, item := range componentWeights {
		model.weightEnabled[item.id] = include(item.axis)
		if include(item.axis) {
			components[item.id] = report.Component{Axis: item.axis, Contribution: 1}
			applicable = append(applicable, item.id)
		}
	}
	base := report.Document{Files: []report.File{{Path: "example." + language, Language: language, Complete: true, Components: components}}}
	model.files.Document, model.files.BaseDocument = base, base
	rebuildWeightedDocument(&model)
	if got, want := model.files.Document.Files[0].Score, float64(len(applicable)); got != want {
		t.Fatalf("all applicable weights score = %v, want %v", got, want)
	}
	assertEachWeightCanBeDisabled(t, &model, applicable)
}

func assertEachWeightCanBeDisabled(t *testing.T, model *Model, applicable []string) {
	t.Helper()
	for _, disabledID := range applicable {
		model.weightEnabled[disabledID] = false
		rebuildWeightedDocument(model)
		if got, want := model.files.Document.Files[0].Score, float64(len(applicable)-1); got != want {
			t.Fatalf("disabled %s score = %v, want %v", disabledID, got, want)
		}
		model.weightEnabled[disabledID] = true
	}
}

func TestTypeSafetyColumnIsOffByDefaultAndUsesItsAxis(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible: map[string]bool{},
		}}
	for _, column := range activeColumns(model) {
		if column.key == "typesafety" {
			t.Fatal("type safety column is enabled by default")
		}
	}
	file := testFile("example.ts", 0)
	file.Axes = map[string]float64{"typescript_type_safety": 12}
	value, exists, _ := metric(file, "typesafety")
	if !exists || value != 12 {
		t.Fatalf("type safety metric = %v, %t; want 12, true", value, exists)
	}
	component := report.Component{Axis: "typescript_type_safety", Contribution: 12}
	base := report.Document{Files: []report.File{{Path: "example.ts", Complete: true, Score: 12, Components: map[string]report.Component{
		"explicit_any": component,
	}}}}
	model = Model{
		files: FilesState{
			Document:     base,
			BaseDocument: base,
			Visible:      map[string]bool{},
		}, weights: defaultWeights()}
	rebuildWeightedDocument(&model)
	if got := model.files.Document.Files[0].Score; got != 0 {
		t.Fatalf("default type safety score = %v, want 0", got)
	}
	for index, column := range columnNames() {
		if column.key == "typesafety" {
			model.columnCursor = index
			break
		}
	}
	handleColumnKey(&model, " ")
	if got := model.files.Document.Files[0].Score; got != 12 {
		t.Fatalf("enabled type safety score = %v, want 12", got)
	}
}

func TestNestingColumnIsOffByDefaultAndControlsItsScore(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible: map[string]bool{},
		}}
	for _, column := range activeColumns(model) {
		if column.key == "nesting" {
			t.Fatal("nesting column is enabled by default")
		}
	}
	component := report.Component{Axis: "structural_core", Contribution: 6}
	base := report.Document{Files: []report.File{{Path: "example.go", Complete: true, Score: 6, Components: map[string]report.Component{
		"deeply_nested_if": component,
	}}}}
	model = Model{
		files: FilesState{
			Document:     base,
			BaseDocument: base,
			Visible:      map[string]bool{},
		}, weights: defaultWeights()}
	rebuildWeightedDocument(&model)
	if got := model.files.Document.Files[0].Score; got != 0 {
		t.Fatalf("default nesting score = %v, want 0", got)
	}
	for index, column := range columnNames() {
		if column.key == "nesting" {
			model.columnCursor = index
			break
		}
	}
	handleColumnKey(&model, " ")
	if got := model.files.Document.Files[0].Score; got != 6 {
		t.Fatalf("enabled nesting score = %v, want 6", got)
	}
}

func TestCouplingColumnIsOnByDefaultAndControlsItsScore(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible: map[string]bool{"coupling": true},
		}}
	found := false
	for _, column := range activeColumns(model) {
		if column.key == "coupling" && column.title == "CPL" {
			found = true
		}
	}
	if !found {
		t.Fatal("CPL column is not enabled by default")
	}
	component := report.Component{Axis: "structural_language", Contribution: 10}
	base := report.Document{Files: []report.File{{Path: "example.go", Complete: true, Score: 10, Components: map[string]report.Component{
		"coupling_between_objects": component,
	}}}}
	model = Model{
		files: FilesState{
			Document:     base,
			BaseDocument: base,
			Visible:      map[string]bool{"coupling": true},
		}, weights: defaultWeights()}
	rebuildWeightedDocument(&model)
	if got := model.files.Document.Files[0].Score; got != 10 {
		t.Fatalf("default coupling score = %v, want 10", got)
	}
	for index, column := range columnNames() {
		if column.key == "coupling" {
			model.columnCursor = index
			break
		}
	}
	handleColumnKey(&model, " ")
	if got := model.files.Document.Files[0].Score; got != 0 {
		t.Fatalf("disabled coupling score = %v, want 0", got)
	}
}

func TestShortWeightsPopupScrollsToSelectedEntry(t *testing.T) {
	model := Model{height: 10, weightCursor: len(componentWeights) - 1, weights: defaultWeights()}
	view := ansi.Strip(weightsView(model))
	if !strings.Contains(view, "space on/off") || !strings.Contains(view, "←/→ weights") || !strings.Contains(view, "clear") || !strings.Contains(view, "info") {
		t.Fatalf("weights popup is missing its adjustment hint: %q", view)
	}
	if !strings.Contains(view, "Unsafe type use") {
		t.Fatalf("short weights popup did not scroll to selected entry: %q", view)
	}
	if strings.Contains(view, "COG (cognitive complexity)") {
		t.Fatalf("short weights popup did not scroll its body: %q", view)
	}
}

func TestOOpensSortAndSDoesNot(t *testing.T) {
	model := Model{}
	updated, _ := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if !updated.(*Model).sortOpen {
		t.Fatal("o did not open sorting")
	}
	model = Model{}
	updated, _ = handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if updated.(*Model).sortOpen || !updated.(*Model).settings {
		t.Fatal("s still opens sorting")
	}
}

func TestSettingsContainsColumnsAndReturnsAfterEditing(t *testing.T) {
	model := Model{}
	updated, _ := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	result := updated.(*Model)
	openSetting(result, "appearance")
	if !strings.Contains(ansi.Strip(settingsView(*result)), "Columns") {
		t.Fatal("settings does not contain Columns")
	}
	result.settingsCursor = 1
	updated, _ = handleKey(result, tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	if !result.columns || !result.columnsFromSettings || result.settings {
		t.Fatal("settings did not open Columns")
	}
	updated, _ = handleKey(result, tea.KeyMsg{Type: tea.KeyEsc})
	result = updated.(*Model)
	if result.columns || !result.settings {
		t.Fatal("Columns did not return to Settings")
	}
}

func TestSettingsOptionsAreAlphabetical(t *testing.T) {
	want := []string{"Agents", "Appearance", "Static Analysis"}
	if len(settingsItems) != len(want) {
		t.Fatalf("settings count = %d, want %d", len(settingsItems), len(want))
	}
	for index, label := range want {
		if settingsItems[index].label != label {
			t.Fatalf("settings[%d] = %q, want %q", index, settingsItems[index].label, label)
		}
	}
}

func TestModalSelectionsUseBackgroundInsteadOfTextCursors(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 80, settingsCursor: 0, weightCursor: 0}
	for name, view := range map[string]string{
		"settings":   settingsView(model),
		"appearance": appearanceView(model),
		"weights":    weightsView(model),
		"columns":    columnsView(model),
		"sort":       sortView(model),
	} {
		for _, line := range strings.Split(ansi.Strip(view), "\n") {
			if strings.Contains(line, "←/→ weights") {
				continue
			}
			if strings.Contains(line, "›") || strings.Contains(line, ">") {
				t.Errorf("%s view still uses a text cursor", name)
			}
		}
	}
}

func TestKeyHintHighlightsHotkeyWhereItOccurs(t *testing.T) {
	ConfigureTerminalColours()
	hint := keyHint("o", "columns", style.SurfaceFooter)
	if got := ansi.Strip(hint); got != "columns" {
		t.Fatalf("key hint changed label to %q", got)
	}
	if !strings.Contains(hint, "o") {
		t.Fatalf("key hint does not contain highlighted hotkey: %q", hint)
	}
}

func TestHintRowUsesSharedSpacingAndHyphenation(t *testing.T) {
	ConfigureTerminalColours()
	got := ansi.Strip(hintRow(style.SurfaceFooter,
		hintItem{"r", "reset"},
		hintItem{"a", "are you sure?"},
		hintItem{"i", "info"},
	))
	if got != "reset  are-you-sure?  info" {
		t.Fatalf("hint row = %q", got)
	}
}

func TestHelpShortcutShowsTopicChooser(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 100, height: 24}
	updated, command := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if command != nil || !updated.(*Model).help {
		t.Fatal("h did not open help")
	}
	view := helpView(*updated.(*Model))
	if lines := strings.Count(view, "\n") + 1; lines >= 20 {
		t.Fatalf("help popup has %d lines, want fewer than 20", lines)
	}
	if width := lipgloss.Width(view); width < 60 {
		t.Fatalf("help popup width = %d, want a wide popup", width)
	}
	for _, title := range []string{"Command-line options", "Main-screen controls", "Scoring system"} {
		if !strings.Contains(view, title) {
			t.Errorf("help popup does not offer %s", title)
		}
	}
	closed, _ := handleKey(updated.(*Model), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if closed.(*Model).help {
		t.Fatal("q did not close help")
	}
}

func TestHelpOverlayLeavesTitleRowVisible(t *testing.T) {
	model := Model{width: 80, height: 20}
	baseLines := []string{"TITLE"}
	for len(baseLines) < model.height {
		baseLines = append(baseLines, "background")
	}
	base := strings.Join(baseLines, "\n")
	view := model.overlayBelowTitle(base, strings.Repeat("help\n", 14))
	if !strings.Contains(strings.Split(view, "\n")[0], "TITLE") {
		t.Fatal("help overlay covered the title row")
	}
}

func TestHelpWrappedLinesAlignUnderDescriptions(t *testing.T) {
	lines := wrapText("Weighted sum of all enabled metrics and rules. Lower is better", 30, "SCORE   ", "        ")
	if len(lines) < 2 {
		t.Fatal("help entry did not wrap")
	}
	if !strings.HasPrefix(lines[0], "SCORE   ") {
		t.Fatalf("first help line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "        ") {
		t.Fatalf("continuation line is not indented: %q", lines[1])
	}
}

func TestDisplayFilesSortsEveryOverviewColumnInBothDirections(t *testing.T) {
	first := sortableFile("zeta.go", 1, 80, 8, 18, 28, 2, 38)
	second := sortableFile("alpha.go", 2, 20, 2, 12, 14, 1, 24)
	third := sortableFile("middle.go", 3, 50, 5, 15, 21, 0, 31)
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{first, second, third}},
		}}
	tests := []struct {
		key        string
		reverse    bool
		wantFirst  string
		wantSecond string
	}{
		{"score", false, "alpha.go", "middle.go"},
		{"score", true, "zeta.go", "middle.go"},
		{"cog", false, "alpha.go", "middle.go"},
		{"cog", true, "zeta.go", "middle.go"},
		{"npath", false, "alpha.go", "middle.go"},
		{"npath", true, "zeta.go", "middle.go"},
		{"cyclo", false, "alpha.go", "middle.go"},
		{"cyclo", true, "zeta.go", "middle.go"},
		{"deep", false, "middle.go", "alpha.go"},
		{"deep", true, "zeta.go", "alpha.go"},
		{"god", false, "alpha.go", "middle.go"},
		{"god", true, "zeta.go", "middle.go"},
		{"filename", false, "alpha.go", "middle.go"},
		{"filename", true, "zeta.go", "middle.go"},
	}
	for _, test := range tests {
		model.files.SortKey, model.files.SortReverse = test.key, test.reverse
		got := model.files.displayFiles(model.options.Limit)
		if got[0].Path != test.wantFirst || got[1].Path != test.wantSecond {
			t.Errorf("sort %s reverse=%t = %s, %s", test.key, test.reverse, got[0].Path, got[1].Path)
		}
	}
}

func TestDisplayFilesSortsPathByTheCompletePath(t *testing.T) {
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{
				testFile("zeta/alpha.go", 1),
				testFile("alpha/zeta.go", 1),
				testFile("middle/beta.go", 1),
			}},
			SortKey: "filename",
		},
	}

	got := model.files.displayFiles(model.options.Limit)
	want := []string{"alpha/zeta.go", "middle/beta.go", "zeta/alpha.go"}
	for index := range want {
		if got[index].Path != want[index] {
			t.Fatalf("ascending path order = %#v, want %#v", []string{got[0].Path, got[1].Path, got[2].Path}, want)
		}
	}

	model.files.SortReverse = true
	got = model.files.displayFiles(model.options.Limit)
	want = []string{"zeta/alpha.go", "middle/beta.go", "alpha/zeta.go"}
	for index := range want {
		if got[index].Path != want[index] {
			t.Fatalf("descending path order = %#v, want %#v", []string{got[0].Path, got[1].Path, got[2].Path}, want)
		}
	}
}

func TestDisplayFilesCacheRefreshesOnlyWhenOrderingChanges(t *testing.T) {
	model := Model{
		files: FilesState{
			Document: report.Document{Files: []report.File{
				testFile("low.go", 1), testFile("high.go", 9),
			}},
			SortKey:     "score",
			SortReverse: true,
			Visible:     defaultColumnVisibility(),
		},
	}
	model.files.refreshDisplayFiles(model.options.Limit)
	first := model.files.displayFiles(model.options.Limit)
	second := model.files.displayFiles(model.options.Limit)
	if len(first) != 2 || first[0].Path != "high.go" {
		t.Fatalf("cached order = %#v", first)
	}
	if &first[0] != &second[0] {
		t.Fatal("displayFiles copied the cached result")
	}

	model.files.SortCursor = len(sortFields()) - 1 // filename
	activateHighlightedSort(&model, false, true)
	if got := model.files.displayFiles(model.options.Limit); got[0].Path != "high.go" {
		t.Fatalf("filename order = %#v", got)
	}
	model.files.SortCursor = 0 // score
	activateHighlightedSort(&model, false, true)
	if got := model.files.displayFiles(model.options.Limit); got[0].Path != "low.go" {
		t.Fatalf("refreshed score order = %#v", got)
	}
}

func BenchmarkTableViewTwentyFiveThousandFiles(b *testing.B) {
	ConfigureTerminalColours()
	files := make([]report.File, 25_000)
	rows := make(map[string]rowState, len(files))
	for index := range files {
		path := fmt.Sprintf("module/package/file_%05d.go", index)
		files[index] = testFile(path, float64(index%100))
		rows[path] = rowState{}
	}
	model := Model{
		files: FilesState{
			Document:    report.Document{Files: files},
			Rows:        rows,
			SortKey:     "score",
			SortReverse: true,
			Visible:     defaultColumnVisibility(),
		},
		width:   140,
		height:  50,
		options: Options{TrendWindow: time.Minute},
	}
	model.files.refreshDisplayFiles(model.options.Limit)
	model.files.refreshFreshnessStatus()
	b.ResetTimer()
	for range b.N {
		_ = tableView(model)
	}
}

func TestDisplayFilesDoesNotCapFortyFiveThousandRows(t *testing.T) {
	files := make([]report.File, 45_000)
	for index := range files {
		files[index] = testFile(fmt.Sprintf("src/main/java/example/Class%05d.java", index), 0)
	}
	model := Model{
		files: FilesState{
			Document:    report.Document{Files: files},
			SortKey:     "score",
			SortReverse: true,
			Visible:     defaultColumnVisibility(),
		},
	}
	model.files.refreshDisplayFiles(model.options.Limit)
	if got := len(model.files.displayFiles(model.options.Limit)); got != len(files) {
		t.Fatalf("display rows = %d, want %d", got, len(files))
	}
}

func TestSpaceAppliesSortWithoutChangingDirectionAndPreservesSelectedFile(t *testing.T) {
	first := sortableFile("a.go", 1, 90, 9, 9, 9, 0, 0)
	second := sortableFile("b.go", 2, 10, 1, 1, 1, 0, 0)
	model := Model{
		files: FilesState{
			Document:       report.Document{Files: []report.File{first, second}},
			Selected:       "a.go",
			Cursor:         0,
			SortKey:        "score",
			SortReverse:    false,
			SortDirections: map[string]bool{"score": false},
			SortCursor:     0,
		},
		sortOpen: true,
		options:  Options{TrendWindow: time.Minute},
	}
	updated, _ := handleKey(&model, tea.KeyMsg{Type: tea.KeySpace})
	result := updated.(*Model)
	if result.files.SortKey != "score" || result.files.SortReverse {
		t.Fatalf("sort = %s reverse=%t", result.files.SortKey, result.files.SortReverse)
	}
	if !result.sortOpen {
		t.Fatal("applying sort unexpectedly closed the popup")
	}
	if result.files.Selected != "a.go" || result.files.Cursor != 1 {
		t.Fatalf("selection = %q at %d", result.files.Selected, result.files.Cursor)
	}
}

func TestEnterDoesNotApplyOrCloseSort(t *testing.T) {
	model := Model{
		files: FilesState{
			SortKey:     "score",
			SortReverse: true,
			Visible:     defaultColumnVisibility(),
			SortCursor:  1,
		},
		sortOpen: true,
	}
	handleSortKey(&model, "enter")
	if !model.sortOpen || model.files.SortKey != "score" || !model.files.SortReverse {
		t.Fatalf("Enter changed sort popup state: open=%t key=%q reverse=%t", model.sortOpen, model.files.SortKey, model.files.SortReverse)
	}
}

func TestActiveSortIsMarkedImmediatelyBeforeItsHeading(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		files: FilesState{
			Visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
		},
		width: 100,
	}
	for key, title := range map[string]string{
		"score": "SCORE", "cog": "COG", "npath": "NPATH",
		"cyclo": "CYCLO", "deep": "SHALLOW", "god": "GOD",
	} {
		model.files.SortKey, model.files.SortReverse = key, false
		if heading := header(model); !strings.Contains(heading, "▲"+title) {
			t.Errorf("ascending %s heading has no adjacent indicator: %q", key, heading)
		}
		model.files.SortReverse = true
		if heading := header(model); !strings.Contains(heading, "▼"+title) {
			t.Errorf("descending %s heading has no adjacent indicator: %q", key, heading)
		}
	}
	model.files.SortKey, model.files.SortReverse = "filename", false
	if heading := header(model); !strings.Contains(heading, " ▲") {
		t.Fatalf("filename heading has no ascending indicator: %q", heading)
	}
}

func TestHeaderIncludesEveryEnabledTitle(t *testing.T) {
	model := Model{
		files: FilesState{
			SortKey: "filename",
			Visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
		},
		width: 100,
	}
	heading := ansi.Strip(header(model))
	for _, title := range []string{"SCORE", "COG", "NPATH", "CYCLO", "SHALLOW", "GOD"} {
		if !strings.Contains(heading, title) {
			t.Errorf("enabled title %s is missing from %q", title, heading)
		}
	}
}

func TestHeaderRightAlignsCommaFormattedFileCountInHeaderStyle(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		files: FilesState{
			SortKey:  "score",
			Document: report.Document{Files: make([]report.File, 12_345)},
			Visible:  map[string]bool{"cog": true, "npath": true},
		},
		width: 100,
	}
	header := header(model)
	plain := ansi.Strip(header)
	if !strings.HasSuffix(plain, "FILES: 12,345 ") {
		t.Fatalf("file count is not right-aligned one character from the margin: %q", plain)
	}
	if !strings.Contains(header, "FILES: 12,345") {
		t.Fatalf("styled header is missing the file count: %q", header)
	}
	if got := formatIntegerWithCommas(1_234_560); got != "1,234,560" {
		t.Fatalf("formatted file count = %q, want 1,234,560", got)
	}
}

func TestOverviewOmitsRankAndSeparatesScoreFromMetrics(t *testing.T) {
	ConfigureTerminalColours()
	file := sortableFile("example.go", 7, 12, 3, 4, 5, 0, 100)
	model := Model{
		files: FilesState{
			SortKey:     "score",
			SortReverse: true,
			Visible:     map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
			Rows:        map[string]rowState{file.Path: {}},
		},
		width: 100,
	}
	heading := ansi.Strip(header(model))
	if strings.Contains(heading, "#") {
		t.Fatalf("rank heading remains: %q", heading)
	}
	for _, title := range []string{"SCORE", "COG", "NPATH", "CYCLO", "SHALLOW", "GOD"} {
		if !strings.Contains(heading, title) {
			t.Errorf("enabled title %s is missing from %q", title, heading)
		}
	}
	row := ansi.Strip(renderRow(model, file, false))
	if !strings.HasPrefix(row, "      12 3") {
		t.Fatalf("rank or score/COG spacing is wrong: %q", row)
	}
	if !strings.Contains(row, "100  example.go") {
		t.Fatalf("GOD/path spacing is wrong: %q", row)
	}
}

func TestGodAndCouplingDisplayAsRoundedIntegers(t *testing.T) {
	for key, want := range map[string]string{"god": "11", "coupling": "11"} {
		if got := metricText(key, 10.6); got != want {
			t.Errorf("%s display = %q, want %q", key, got, want)
		}
	}
	if got := decimalWithin(10.6, 8); got != "11" {
		t.Fatalf("score display = %q, want 11", got)
	}
}

func TestEveryDisplayedColumnIsSortable(t *testing.T) {
	columns := columnNames()
	fields := sortFields()
	for _, column := range columns {
		found := false
		for _, field := range fields {
			if field.key == column.key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("displayed column %q has no sort field", column.key)
		}
	}
}

func TestSortUsesSharedColumnDescriptionsAndSkipsHiddenColumns(t *testing.T) {
	model := Model{
		files: FilesState{
			Visible:    defaultColumnVisibility(),
			SortCursor: 1,
		}}
	view := ansi.Strip(sortView(model))
	for _, text := range []string{
		"COG         (cognitive)",
		"NPATH       (execution path complexity)",
		"SHALLOW     (module depth)",
		"GOD         (responsibility concentration)",
		"CPL         (dependency entanglement)",
		"NEST        (deep nesting)",
		"TYPE        (type safety)",
	} {
		if !strings.Contains(view, text) {
			t.Errorf("sort description missing %q: %q", text, view)
		}
	}
	for model.files.SortCursor != len(sortFields())-1 {
		handleSortKey(&model, "down")
		if model.files.SortCursor == 0 {
			t.Fatal("sort cursor wrapped while moving down")
		}
	}
	if sortFields()[model.files.SortCursor].key != "filename" {
		t.Fatalf("sort cursor landed on %q, want filename", sortFields()[model.files.SortCursor].key)
	}
}

func TestSortDirectionChangesOnlyHighlightedMetricAndActivatesIt(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		files: FilesState{
			Visible:        defaultColumnVisibility(),
			SortKey:        "score",
			SortReverse:    true,
			SortDirections: map[string]bool{"score": true, "cog": false, "npath": false},
			SortCursor:     1,
		},
	}
	handleSortKey(&model, "right")
	if model.files.SortKey != "cog" || !model.files.SortReverse {
		t.Fatalf("right did not activate descending COG: key=%q reverse=%t", model.files.SortKey, model.files.SortReverse)
	}
	if !model.files.SortDirections["score"] || model.files.SortDirections["npath"] {
		t.Fatalf("right changed another metric's direction: %#v", model.files.SortDirections)
	}
	view := ansi.Strip(sortView(model))
	for _, want := range []string{"▼ SCORE", "▼ COG", "▲ NPATH"} {
		if !strings.Contains(view, want) {
			t.Errorf("sort view is missing %q: %q", want, view)
		}
	}
}

func TestSortCursorBackgroundDoesNotRemainOnActiveMetric(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		files: FilesState{
			Visible:        defaultColumnVisibility(),
			SortKey:        "score",
			SortReverse:    true,
			SortDirections: map[string]bool{"score": true, "cog": true},
			SortCursor:     1,
		},
	}
	lines := strings.Split(sortView(model), "\n")
	lineWith := func(title string) string {
		for _, line := range lines {
			if strings.Contains(ansi.Strip(line), title) {
				return line
			}
		}
		return ""
	}
	wantBackground := "48;2;36;89;120m"
	if strings.Contains(lineWith("SCORE"), wantBackground) {
		t.Fatal("active sort metric retained the cursor background")
	}
	if !strings.Contains(lineWith("COG"), wantBackground) {
		t.Fatal("highlighted sort metric has no cursor background")
	}
	handleSortKey(&model, "down")
	lines = strings.Split(sortView(model), "\n")
	if strings.Contains(lineWith("COG"), wantBackground) || !strings.Contains(lineWith("NPATH"), wantBackground) {
		t.Fatal("cursor background did not move with the highlighted row")
	}
}

func TestEscapeClosesSortAndColumnsDialogs(t *testing.T) {
	for name, model := range map[string]*Model{
		"sort":    {sortOpen: true},
		"columns": {columns: true},
	} {
		updated, _ := handleKey(model, tea.KeyMsg{Type: tea.KeyEsc})
		result := updated.(*Model)
		if result.sortOpen || result.columns {
			t.Errorf("Escape did not close %s dialog", name)
		}
	}
}

func TestModalIsCompositedOverCurrentTable(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 80, height: 24}
	base := strings.Repeat("underlying table row"+strings.Repeat(" ", 60)+"\n", 23) +
		"underlying table row" + strings.Repeat(" ", 60)
	modal := lipgloss.NewStyle().Width(20).Border(lipgloss.RoundedBorder()).Render("SORT RESULTS")
	result := model.overlay(base, modal)
	if !strings.Contains(result, "SORT RESULTS") {
		t.Fatal("composite lost modal contents")
	}
	if !strings.Contains(result, "underlying table row") {
		t.Fatal("composite cleared the underlying table")
	}
	if got := strings.Count(result, "\n") + 1; got != model.height {
		t.Fatalf("composite height = %d, want %d", got, model.height)
	}
}

func TestDetailPopupFitsTerminalAndScrollsWithinContent(t *testing.T) {
	ConfigureTerminalColours()
	model := longDetailPopupModel()
	assertDetailPopupSize(t, &model)
	assertDetailPopupScrolling(t, &model)
}

func longDetailPopupModel() Model {
	file := sortableFile("long/path/example.go", 1, 80, 30, 300, 40, 2, 100)
	component := file.Components["cognitive_complexity"]
	for index := 0; index < 30; index++ {
		component.Subjects = append(component.Subjects, report.SubjectContribution{
			Subject: fmt.Sprintf("routine_%02d_with_a_long_descriptive_name", index),
			Value:   float64(index), Contribution: float64(index) / 10,
		})
	}
	component.Observations = len(component.Subjects)
	file.Components["cognitive_complexity"] = component
	return Model{
		files: FilesState{
			Selected:    file.Path,
			Document:    report.Document{Files: []report.File{file}},
			Rows:        map[string]rowState{file.Path: {}},
			SortKey:     "score",
			SortReverse: true,
		},
		width:  80,
		height: 24,
		detail: true,
	}
}

func assertDetailPopupSize(t *testing.T, model *Model) {
	t.Helper()
	view := detailView(*model)
	if got := lipgloss.Width(view); got != 74 {
		t.Fatalf("detail width = %d, want 74", got)
	}
	if got := lipgloss.Height(view); got != 21 {
		t.Fatalf("detail height = %d, want 21", got)
	}
	if !strings.Contains(ansi.Strip(view), "█") {
		t.Fatal("scrollable detail has no visible scrollbar thumb")
	}
}

func assertDetailPopupScrolling(t *testing.T, model *Model) {
	t.Helper()
	maximum := detailMaxOffset(*model)
	if maximum <= 0 {
		t.Fatal("long detail unexpectedly fits without scrolling")
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyEnd})
	if model.detailOffset != maximum {
		t.Fatalf("End scrolled to %d, want %d", model.detailOffset, maximum)
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyDown})
	if model.detailOffset != maximum {
		t.Fatalf("scroll escaped lower bound: %d > %d", model.detailOffset, maximum)
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyHome})
	if model.detailOffset != 0 {
		t.Fatalf("Home left detail offset at %d", model.detailOffset)
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyPgDown})
	if model.detailOffset <= 0 || model.detailOffset > maximum {
		t.Fatalf("page scroll produced invalid offset %d of %d", model.detailOffset, maximum)
	}
}

func TestDetailPopupShrinksWithTerminal(t *testing.T) {
	file := testFile("example.go", 1)
	model := Model{
		files: FilesState{
			Selected: file.Path,
			Document: report.Document{Files: []report.File{file}},
			Rows:     map[string]rowState{file.Path: {}},
		},
		width:  24,
		height: 8,
	}
	view := detailView(model)
	if lipgloss.Width(view) > model.width || lipgloss.Height(view) > model.height {
		t.Fatalf("detail %dx%d exceeds terminal %dx%d", lipgloss.Width(view), lipgloss.Height(view), model.width, model.height)
	}
}

func TestSourceViewOpensSelectedFileAndUsesViewportScrolling(t *testing.T) {
	model := sourceViewTestModel(t)
	model = openAndLoadSourceView(t, model)
	assertSourceViewContents(t, &model)
	assertSourceViewScrolling(t, &model)
}

func sourceViewTestModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "example.go")
	lines := []string{"package example", "", "func Run() {", "\treturn", "}"}
	for index := 0; index < 40; index++ {
		lines = append(lines, fmt.Sprintf("// line %02d", index))
	}
	contents := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return Model{
		files: FilesState{
			Selected: "example.go",
			Document: report.Document{Files: []report.File{testFile("example.go", 1)}},
			Rows:     map[string]rowState{"example.go": {}},
			Visible:  map[string]bool{},
		},
		width:   80,
		height:  24,
		options: Options{Workspace: root},
	}
}

func openAndLoadSourceView(t *testing.T, model Model) Model {
	t.Helper()
	var command tea.Cmd
	model, command = openSourceViewTest(t, model)
	model, command = loadRawSourceView(t, model, command)
	return finishSourceHighlight(t, model, command)
}

func openSourceViewTest(t *testing.T, model Model) (Model, tea.Cmd) {
	t.Helper()
	updated, command := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if command == nil || !updated.(*Model).source.view {
		t.Fatal("v did not open source view")
	}
	model = *updated.(*Model)
	if loadingView := ansi.Strip(sourceViewView(model)); !strings.Contains(loadingView, "Loading source…") || !strings.Contains(loadingView, "loading") {
		t.Fatalf("source popup did not render before loading completed: %q", loadingView)
	}
	return model, command
}

func loadRawSourceView(t *testing.T, model Model, command tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	updated, next := model.Update(command())
	if next == nil {
		t.Fatal("source load did not defer syntax highlighting")
	}
	model = *updated.(*Model)
	if rawView := ansi.Strip(sourceViewView(model)); !strings.Contains(rawView, "func Run()") || model.source.loading {
		t.Fatalf("raw source was not usable before highlighting completed: %q", rawView)
	}
	return model, next
}

func finishSourceHighlight(t *testing.T, model Model, command tea.Cmd) Model {
	t.Helper()
	updated, next := model.Update(command())
	if next != nil {
		t.Fatal("source highlighting unexpectedly returned a follow-up command")
	}
	return *updated.(*Model)
}

func assertSourceViewContents(t *testing.T, model *Model) {
	t.Helper()
	view := ansi.Strip(sourceViewView(*model))
	if !strings.Contains(view, "func Run()") {
		t.Fatal("source view did not render selected file")
	}
	if !strings.Contains(view, "example.go") || !strings.Contains(view, "45 lines") || strings.Contains(view, "SOURCE  example.go") {
		t.Fatalf("source header does not show path and line count: %q", view)
	}
	if !strings.Contains(view, "ctrl-f/b page  g/G jump") || !strings.Contains(view, "find  n/N next  ESC close") {
		t.Fatalf("source footer does not contain the navigation and close groups: %q", view)
	}
	assertSourceViewInsets(t, view)
}

func assertSourceViewInsets(t *testing.T, view string) {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "45 lines") && !strings.HasSuffix(line, "45 lines │") {
			t.Errorf("line count is not inset one column from the right border: %q", line)
		}
		if strings.Contains(line, "ESC close") && !strings.HasSuffix(line, "ESC close │") {
			t.Errorf("footer actions are not inset one column from the right border: %q", line)
		}
	}
}

func assertSourceViewScrolling(t *testing.T, model *Model) {
	t.Helper()
	assertSourceVerticalScrolling(t, model)
	assertSourceHorizontalScrolling(t, model)
	assertSourceJumpAndClose(t, model)
}

func assertSourceVerticalScrolling(t *testing.T, model *Model) {
	t.Helper()
	handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.source.viewport.YOffset != 1 {
		t.Fatalf("first j moved source viewport by %d lines, want 1", model.source.viewport.YOffset)
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.source.viewport.YOffset != 2 {
		t.Fatalf("second j moved source viewport by %d lines, want 2 total", model.source.viewport.YOffset)
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.source.viewport.YOffset != 4 {
		t.Fatalf("established repeat moved source viewport by %d lines, want 4 total", model.source.viewport.YOffset)
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.source.viewport.YOffset == 0 {
		t.Fatal("Ctrl-F did not advance source viewport")
	}
}

func assertSourceHorizontalScrolling(t *testing.T, model *Model) {
	t.Helper()
	longLine := strings.Repeat("x", 200)
	model.source.viewport.SetContent(longLine + "\n" + model.source.searchText)
	handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if model.source.viewport.HorizontalScrollPercent() == 0 {
		t.Fatal("l did not horizontally scroll source viewport")
	}
}

func assertSourceJumpAndClose(t *testing.T, model *Model) {
	t.Helper()
	handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if model.source.viewport.YOffset != 0 {
		t.Fatalf("g moved source viewport to line %d, want top", model.source.viewport.YOffset)
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if model.source.viewport.YOffset == 0 {
		t.Fatal("G did not jump to the bottom of the source viewport")
	}
	handleKey(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.source.view || model.source.path != "" {
		t.Fatal("Esc did not close source view")
	}
}

func TestSourceViewIgnoresACompletedLoadAfterItCloses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "example.go"), []byte("package example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := testFile("example.go", 1)
	model := Model{
		files: FilesState{
			Selected: file.Path,
			Document: report.Document{Files: []report.File{file}},
			Rows:     map[string]rowState{file.Path: {}},
			Visible:  map[string]bool{},
		},
		width:   80,
		height:  24,
		options: Options{Workspace: root},
	}
	_, load := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	handleKey(&model, tea.KeyMsg{Type: tea.KeyEsc})
	model.Update(load())
	if model.source.view || model.source.path != "" || model.source.searchText != "" {
		t.Fatal("completed background load reopened or populated a closed source view")
	}
}

func BenchmarkOpenSourceViewTwentyFiveThousandFiles(b *testing.B) {
	files := make([]report.File, 25_000)
	for index := range files {
		files[index] = testFile(fmt.Sprintf("module/package/file_%05d.go", index), float64(index%100))
	}
	model := Model{
		files: FilesState{
			Selected:    files[0].Path,
			Document:    report.Document{Files: files},
			SortKey:     "score",
			SortReverse: true,
			Visible:     defaultColumnVisibility(),
		},
		width:   140,
		height:  50,
		options: Options{Workspace: b.TempDir()},
	}
	model.files.refreshDisplayFiles(model.options.Limit)
	model.files.Selected = model.files.displayFiles(model.options.Limit)[0].Path
	b.ResetTimer()
	for range b.N {
		model.source.view = false
		if command := openSourceView(&model); command == nil {
			b.Fatal("openSourceView returned no load command")
		}
	}
}

func TestSourceLineCountHandlesEmptyAndTrailingNewlineFiles(t *testing.T) {
	for name, test := range map[string]struct {
		contents string
		want     int
	}{
		"empty":            {contents: "", want: 0},
		"one line":         {contents: "only", want: 1},
		"without trailing": {contents: "first\nsecond", want: 2},
		"with trailing":    {contents: "first\nsecond\n", want: 2},
	} {
		t.Run(name, func(t *testing.T) {
			if got := sourceLineCount(test.contents); got != test.want {
				t.Fatalf("sourceLineCount(%q) = %d, want %d", test.contents, got, test.want)
			}
		})
	}
}

func sortableFile(path string, rank int, score, cog, npath, cyclo, deep, god float64) report.File {
	file := testFile(path, score)
	file.Rank = rank
	file.Components = map[string]report.Component{
		"cognitive_complexity":         {Subjects: []report.SubjectContribution{{Value: cog}}},
		"npath_complexity":             {Subjects: []report.SubjectContribution{{Value: npath}}},
		"cyclomatic_method_complexity": {Subjects: []report.SubjectContribution{{Value: cyclo}}},
		"module_shallowness":           {Subjects: []report.SubjectContribution{{Value: deep}}},
		"god_class":                    {Contribution: god},
	}
	return file
}

func TestWatcherExcludesTestsByConvention(t *testing.T) {
	root := t.TempDir()
	watcher, err := newSourceWatcher(root, []string{"."}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	for _, path := range []string{"x_test.go", "tests/x.java", "x.spec.ts"} {
		if _, _, ok := watcher.eligible(filepath.Join(root, filepath.FromSlash(path))); ok {
			t.Fatalf("test source %q was eligible", path)
		}
	}
	if path, language, ok := watcher.eligible(filepath.Join(root, "x.go")); !ok || path != "x.go" || language != "go" {
		t.Fatalf("ordinary source = %q, %q, %t", path, language, ok)
	}
}

func TestWatcherStartsWhenLanguageConfigurationDirectoriesAreAbsent(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "erBuilder")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "Main.java"), []byte("class Main {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	watcher, err := newSourceWatcher(root, []string{"erBuilder"}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if err := watcher.start(); err != nil {
		t.Fatalf("watching a Java target with absent optional language configuration directories: %v", err)
	}
}

func TestWatcherStartsWithExplicitSymlinkDirectoryTarget(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "Main.java"), []byte("class Main {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, filepath.Join(root, "erBuilder")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	watcher, err := newSourceWatcher(root, []string{"erBuilder"}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if err := watcher.start(); err != nil {
		t.Fatalf("watching an explicitly selected symlink directory: %v", err)
	}
}

func TestFileTargetDoesNotWatchItsSiblings(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "selected.go")
	if err := os.WriteFile(target, []byte("package selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	watcher, err := newSourceWatcher(root, []string{"selected.go"}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if _, _, ok := watcher.eligible(filepath.Join(root, "sibling.go")); ok {
		t.Fatal("a sibling escaped the exact-file watch scope")
	}
	if _, _, ok := watcher.eligible(target); !ok {
		t.Fatal("the exact file target was not eligible")
	}
}

func TestConfigurationLanguageCoversPlannerInputs(t *testing.T) {
	tests := map[string]string{
		"go.mod":             "go",
		"nested/go.work.sum": "go",
		"Cargo.lock":         "rust",
		"crate/build.rs":     "rust",
		"pom.xml":            "java",
		"gradle/wrapper/gradle-wrapper.properties": "",
		"tsconfig.build.json":                      "typescript",
		"web/package-lock.json":                    "typescript",
		"nested/.cargo/config.toml":                "",
		"nested/.mvn/maven.config":                 "java",
	}
	for path, wantLanguage := range tests {
		language, ok := configurationLanguage(path)
		if !ok || language != wantLanguage {
			t.Errorf("configurationLanguage(%q) = %q, %t; want %q, true", path, language, ok, wantLanguage)
		}
	}
	for _, path := range []string{"main.go", "README.md", "src/main.ts"} {
		if language, ok := configurationLanguage(path); ok {
			t.Errorf("configurationLanguage(%q) = %q, true; want miss", path, language)
		}
	}
}

func TestTargetedAnalysisNarrowsExplicitLanguages(t *testing.T) {
	got := languagesForPaths([]string{"a.go", "b.java", "c.go"})
	if strings.Join(got, ",") != "go,java" {
		t.Fatalf("languagesForPaths() = %v", got)
	}
}
