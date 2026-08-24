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

func TestArrowNavigationMovesImmediatelyWithoutATimer(t *testing.T) {
	model := Model{
		document: report.Document{Files: []report.File{testFile("a.go", 2), testFile("b.go", 1)}},
		rows:     map[string]rowState{}, visible: map[string]bool{}, height: 10,
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if command != nil {
		t.Fatal("cursor movement unexpectedly scheduled deferred work")
	}
	result := updated.(*Model)
	if result.cursor != 1 || result.selected != "b.go" {
		t.Fatalf("cursor = %d, selected = %q", result.cursor, result.selected)
	}
}

func TestScanningStatusRendersAnimatedOnTopBar(t *testing.T) {
	model := Model{width: 80, height: 20, analyzing: true, animationFrame: 1}
	view := ansi.Strip(model.tableView())
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
	if !strings.HasSuffix(strings.TrimRight(lines[0], " "), "⠙") {
		t.Fatalf("scanning status is not right-aligned: %q", lines[0])
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
		width: 100, height: 20, analyzing: true, initialAnalysis: true, animationFrame: 1,
		document: report.Document{Files: []report.File{file}},
		rows:     map[string]rowState{file.Path: {}}, visible: map[string]bool{},
	}
	firstLine := strings.Split(ansi.Strip(model.tableView()), "\n")[0]
	if !strings.Contains(firstLine, "⠙CACHE VERIFYING 1⠙") {
		t.Fatalf("freshness was not displayed as the animated scanning indicator: %q", firstLine)
	}
	if strings.Contains(firstLine, "SCANNING") || strings.Count(firstLine, "CACHE VERIFYING 1") != 1 {
		t.Fatalf("startup rendered a separate or duplicate status: %q", firstLine)
	}
}

func TestInitialScanCentersLogoOverTable(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		width: 20, height: 7, analyzing: true, initialAnalysis: true,
		options: Options{Workspace: "/workspace"},
	}
	view := ansi.Strip(model.startupOverlay(model.tableView(), "XX\nYY"))
	lines := strings.Split(view, "\n")
	if len(lines) != model.height {
		t.Fatalf("startup view has %d lines, want %d: %q", len(lines), model.height, view)
	}
	if strings.TrimSpace(lines[2]) != "XX" || strings.TrimSpace(lines[3]) != "YY" {
		t.Fatalf("logo is not vertically centered: %q", view)
	}
	if strings.Index(lines[2], "XX") != 9 || strings.Index(lines[3], "YY") != 9 {
		t.Fatalf("logo is not horizontally centered: %q", view)
	}
	if !strings.Contains(lines[0], "SCANNING") || !strings.Contains(lines[len(lines)-1], "sort") {
		t.Fatalf("dashboard is not visible behind startup logo: %q", view)
	}
}

func TestInitialViewUsesEmbeddedLogoWithoutReplacingDashboard(t *testing.T) {
	model := Model{
		width: 120, height: 20, analyzing: true, initialAnalysis: true,
		options: Options{Workspace: "/workspace"},
	}
	base := ansi.Strip(model.tableView())
	view := ansi.Strip(model.View())
	if view == base {
		t.Fatal("initial view did not overlay the embedded logo")
	}
	baseLines := strings.Split(base, "\n")
	viewLines := strings.Split(view, "\n")
	if viewLines[0] != baseLines[0] || viewLines[len(viewLines)-1] != baseLines[len(baseLines)-1] {
		t.Fatalf("logo replaced dashboard chrome: %q", view)
	}
	if !strings.Contains(viewLines[0], "SCANNING") {
		t.Fatalf("initial view lost scanning status: %q", viewLines[0])
	}
}

func TestInitialScanCompletionReplacesLogoWithTable(t *testing.T) {
	model := Model{
		width: 80, height: 10, analyzing: true, initialAnalysis: true,
		options: Options{Workspace: "/workspace"},
		rows:    map[string]rowState{}, queued: map[string]bool{}, visible: map[string]bool{},
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
		t.Fatalf("table did not replace startup logo: %q", view)
	}
}

func TestSuccessfulInitialScanEnablesCacheReadsAfterResultIsVisible(t *testing.T) {
	analyzer := &settingsAnalyzer{}
	model := Model{
		analyzer: analyzer, analyzing: true, initialAnalysis: true,
		rows: map[string]rowState{}, queued: map[string]bool{}, visible: map[string]bool{},
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
		analyzer: analyzer, analyzing: true, initialAnalysis: true,
		rows: map[string]rowState{}, queued: map[string]bool{}, visible: map[string]bool{},
	}
	updated, _ := model.Update(analysisResult{full: true, err: fmt.Errorf("scan failed")})
	if analyzer.cacheReads {
		t.Fatal("failed initial scan enabled cache reads")
	}
	if got := updated.(*Model).status; got != "scan failed" {
		t.Fatalf("status = %q, want scan failure", got)
	}
}

func TestStartupLogoRemovesCursorModeControls(t *testing.T) {
	logo := "\x1b[?25lART\x1b[?25h"
	if got := cleanStartupLogo(logo); got != "ART" {
		t.Fatalf("cleaned logo = %q", got)
	}
}

func TestTableTopBarShowsLogoBeforeWorkspace(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 80, height: 10, options: Options{Workspace: "/workspace"}}
	firstLine := strings.Split(ansi.Strip(model.tableView()), "\n")[0]
	logo := "-=[slopwatch]=-"
	if !strings.HasPrefix(firstLine, logo) {
		t.Fatalf("top bar = %q, want logo prefix %q", firstLine, logo)
	}
	if strings.Index(firstLine, "/workspace") <= len(logo) {
		t.Fatalf("workspace path was not moved after logo: %q", firstLine)
	}
}

func TestTableTopBarShowsRepositoryAndBranchBeforeWorkspace(t *testing.T) {
	model := Model{
		width: 100, height: 10,
		options:            Options{Workspace: "/workspace"},
		repositoryIdentity: "river:feature/display",
	}
	firstLine := strings.Split(ansi.Strip(model.tableView()), "\n")[0]
	repository := strings.Index(firstLine, "river:feature/display")
	workspace := strings.Index(firstLine, "/workspace")
	if repository < 0 || workspace <= repository {
		t.Fatalf("top bar does not show repo:branch before path: %q", firstLine)
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
		document: document, cursor: 0, selected: "alpha.go",
		rows: map[string]rowState{}, visible: map[string]bool{}, findInput: textinput.New(),
	}
	model.findQuery = "a"
	model.findNext(1)
	if model.selected != "beta.go" {
		t.Fatalf("first find selected %q, want beta.go", model.selected)
	}
	model.findNext(1)
	if model.selected != "gamma.go" {
		t.Fatalf("next find selected %q, want gamma.go", model.selected)
	}
}

func TestFindUsesFWithSlashAsHiddenSynonym(t *testing.T) {
	for _, key := range []rune{'f', '/'} {
		model := Model{findInput: textinput.New()}
		updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if !updated.(*Model).findOpen {
			t.Fatalf("%q did not open find", key)
		}
	}
}

func TestFindSearchesSourceAndMovesViewport(t *testing.T) {
	model := Model{
		findSource: true, findQuery: "needle", sourceSearchText: "first\nfiller\nsecond needle\nlast",
		sourceViewport: viewport.New(40, 2),
	}
	model.sourceViewport.SetContent(highlightSource("example.go", model.sourceSearchText))
	model.findNext(1)
	if model.sourceViewport.YOffset != 2 {
		t.Fatalf("source find offset = %d, want 2", model.sourceViewport.YOffset)
	}
}

func TestTargetedMergeLeavesUnchangedRowsAlone(t *testing.T) {
	model := Model{
		document: report.Document{Files: []report.File{testFile("a.go", 20), testFile("b.go", 10)}},
		rows:     map[string]rowState{"a.go": {}, "b.go": {}}, visible: map[string]bool{}, height: 10,
		selected: "a.go", options: Options{TrendWindow: 15 * time.Minute},
	}
	model.document.SortAndRank()
	model.merge(analysisResult{
		document: report.Document{Files: []report.File{testFile("b.go", 30)}},
		replace:  []string{"b.go"},
	})
	if len(model.document.Files) != 2 || model.document.Files[0].Path != "b.go" || model.document.Files[1].Path != "a.go" {
		t.Fatalf("unexpected targeted merge: %#v", model.document.Files)
	}
	if model.rows["b.go"].direction != 1 || model.rows["b.go"].movementDelta != 1 || model.rows["b.go"].scoreChangedAt.IsZero() {
		t.Fatalf("changed row movement was not recorded: %#v", model.rows["b.go"])
	}
	if !model.rows["a.go"].editedAt.IsZero() || model.rows["a.go"].movementDelta != 0 || !model.rows["a.go"].scoreChangedAt.IsZero() {
		t.Fatalf("targeted row history was not isolated: %#v", model.rows)
	}
}

func TestInitialFullMergeDoesNotMarkEveryFileAsNew(t *testing.T) {
	model := Model{
		document: report.Document{}, rows: map[string]rowState{},
		visible: map[string]bool{}, options: Options{TrendWindow: 10 * time.Minute},
	}
	model.merge(analysisResult{
		document: report.Document{Files: []report.File{testFile("existing.go", 10)}},
		full:     true,
	})
	if state := model.rows["existing.go"]; !state.newFileAt.IsZero() {
		t.Fatalf("initial full scan marked existing.go as new: %#v", state)
	}
}

func TestUnchangedRowsPassedByChangedRowStayNeutral(t *testing.T) {
	files := []report.File{
		testFile("a.go", 60), testFile("b.go", 50), testFile("c.go", 40),
		testFile("d.go", 30), testFile("e.go", 20), testFile("f.go", 10),
	}
	model := Model{
		document: report.Document{Files: files}, rows: map[string]rowState{},
		visible: map[string]bool{}, selected: "a.go", options: Options{TrendWindow: 10 * time.Minute},
	}
	model.document.SortAndRank()
	model.merge(analysisResult{
		document: report.Document{Files: []report.File{testFile("f.go", 70)}},
		replace:  []string{"f.go"},
	})

	if got := model.rows["f.go"].movementDelta; got != 5 {
		t.Fatalf("changed row movement = %d, want 5", got)
	}
	if got := movementArrow(model.rows["f.go"].movementDelta); got != "⇈" {
		t.Fatalf("changed row arrow = %q, want ⇈", got)
	}
	for _, path := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		state := model.rows[path]
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
		rows: map[string]rowState{"a.go": {
			scoreChangedAt: time.Now().Add(-2 * time.Minute), movementDelta: 1,
		}},
		options: Options{TrendWindow: time.Minute},
	}
	if marker, _ := model.rowMarker(file, model.rows[file.Path], time.Now()); marker != "" {
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
		document: report.Document{Files: []report.File{testFile("a.go", 20)}},
		rows:     map[string]rowState{"a.go": {}}, visible: map[string]bool{},
		options: Options{TrendWindow: 10 * time.Minute}, selected: "a.go",
	}
	model.document.SortAndRank()
	model.merge(analysisResult{
		document: report.Document{Files: []report.File{testFile("b.go", 10)}},
		replace:  []string{"b.go"},
	})
	if state := model.rows["b.go"]; state.newFileAt.IsZero() || state.newFileMoved {
		t.Fatalf("new file did not start in the green state: %#v", state)
	}

	model.merge(analysisResult{
		document: report.Document{Files: []report.File{testFile("b.go", 30)}},
		replace:  []string{"b.go"},
	})
	state := model.rows["b.go"]
	if !state.newFileMoved || state.movementDelta != 1 {
		t.Fatalf("new file rank transition was not recorded: %#v", state)
	}
	if marker, colour := model.rowMarker(model.document.Files[0], state, time.Now()); marker != "●" || colour != style.AccentCritical {
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

func TestTableFillsAvailableHeight(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		width: 100, height: 12, document: report.Document{Files: []report.File{testFile("a.go", 1)}},
		rows: map[string]rowState{"a.go": {}}, visible: map[string]bool{},
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
		width: 80, pathOffset: 7, rows: map[string]rowState{file.Path: {}},
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
	}
	row := model.renderRow(file, true)
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
			width: 80, height: 20, document: report.Document{Files: []report.File{file}},
			rows: map[string]rowState{file.Path: {}}, visible: map[string]bool{},
			options: Options{TrendWindow: time.Minute},
		}
		if row := ansi.Strip(model.renderRow(file, false)); !strings.Contains(row, test.marker) {
			t.Errorf("%s row has no %q marker: %q", test.freshness, test.marker, row)
		}
		detail := ansi.Strip(strings.Join(model.detailContent(file, 70), "\n"))
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
		width: 120, height: 10, document: report.Document{Files: files},
		rows: map[string]rowState{}, visible: map[string]bool{},
		options: Options{Workspace: "/workspace", TrendWindow: time.Minute},
	}
	firstLine := strings.Split(ansi.Strip(model.tableView()), "\n")[0]
	for _, want := range []string{"CACHE", "PROVISIONAL 1", "STALE 1"} {
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
		width: 52, document: report.Document{Files: []report.File{file}},
		rows: map[string]rowState{file.Path: {}}, visible: map[string]bool{"cog": true},
	}
	before := ansi.Strip(model.renderRow(file, true))
	fixedWidth := model.width - model.pathViewportWidth()
	model.movePath(6)
	after := ansi.Strip(model.renderRow(file, true))
	if model.pathOffset != 6 {
		t.Fatalf("path offset = %d, want 6", model.pathOffset)
	}
	if got, want := ansi.Cut(after, 0, fixedWidth), ansi.Cut(before, 0, fixedWidth); got != want {
		t.Fatalf("fixed metric columns moved: before %q, after %q", want, got)
	}
	if ansi.Cut(after, fixedWidth, model.width) == ansi.Cut(before, fixedWidth, model.width) {
		t.Fatalf("filename did not scroll: before %q, after %q", before, after)
	}
	if lipgloss.Width(model.renderRow(file, true)) != model.width {
		t.Fatal("horizontal scrolling changed the row width")
	}
}

func TestHorizontalScrollDoesNotChangeVerticalSelection(t *testing.T) {
	first := testFile("one/very/long/path/that/needs/horizontal/scrolling/first.go", 2)
	second := testFile("two/very/long/path/that/needs/horizontal/scrolling/second.go", 1)
	model := Model{
		width: 30, height: 8, cursor: 0, selected: first.Path,
		document: report.Document{Files: []report.File{first, second}},
		rows:     map[string]rowState{first.Path: {}, second.Path: {}}, visible: map[string]bool{},
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if model.pathOffset != pathScrollStep || model.cursor != 0 || model.offset != 0 || model.selected != first.Path {
		t.Fatalf("horizontal scroll changed vertical state: path=%d cursor=%d offset=%d selected=%q", model.pathOffset, model.cursor, model.offset, model.selected)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if model.pathOffset != pathScrollStep || model.cursor != 1 || model.selected != second.Path {
		t.Fatalf("vertical movement regressed after horizontal scroll: path=%d cursor=%d selected=%q", model.pathOffset, model.cursor, model.selected)
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
	footer := model.footer()
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
	base := Model{document: report.Document{Files: []report.File{file}}}

	enterModel := base
	enterModel.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	infoModel := base
	infoModel.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})

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
	text := ansi.Strip(model.footer())
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
	text := ansi.Strip(model.footer())
	if !strings.Contains(text, "sort") || !strings.Contains(text, "rescan") {
		t.Fatalf("narrow footer dropped left actions: %q", text)
	}
	if !strings.Contains(text, "find") || !strings.Contains(text, "next") {
		t.Fatalf("narrow footer dropped Find/Next: %q", text)
	}
	if strings.Contains(text, "settings") && strings.Index(text, "settings") < strings.Index(text, "rescan") {
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
	model := Model{document: base, baseDocument: base, weights: defaultWeights()}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	result := updated.(*Model)
	if !result.settings || result.weightsOpen {
		t.Fatal("s did not open settings")
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
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
	result.handleWeightsKey("left")
	if result.weights["cognitive_complexity"] != 9.5 {
		t.Fatalf("weight = %v, want 9.5", result.weights["cognitive_complexity"])
	}
	if result.document.Files[0].Score != 9.5 {
		t.Fatalf("score = %v, want 9.5", result.document.Files[0].Score)
	}
}

func TestWeightsResetCurrentAndAll(t *testing.T) {
	model := Model{weights: defaultWeights(), visible: defaultColumnVisibility()}
	model.weightCursor = 0
	model.weights["cognitive_complexity"] = 2
	model.handleWeightsKey("r")
	if got := model.weights["cognitive_complexity"]; got != 10 {
		t.Fatalf("reset current weight = %v, want 10", got)
	}
	model.weights["cognitive_complexity"] = 2
	model.weights["god_class"] = 19
	model.handleWeightsKey("c")
	if !model.weightsResetConfirm {
		t.Fatal("reset all did not ask for confirmation")
	}
	model.handleWeightsKey("n")
	if model.weights["cognitive_complexity"] != 2 || model.weights["god_class"] != 19 {
		t.Fatal("cancelled reset all changed weights")
	}
	model.handleWeightsKey("c")
	model.handleWeightsKey("y")
	if model.weights["cognitive_complexity"] != 10 || model.weights["god_class"] != 1 {
		t.Fatalf("reset all weights = %v, %v", model.weights["cognitive_complexity"], model.weights["god_class"])
	}
}

func TestWeightsAndHelpOpenTheSharedInfoPopup(t *testing.T) {
	model := Model{weights: defaultWeights(), visible: defaultColumnVisibility(), width: 80, height: 20}
	model.weightCursor = 0
	model.handleWeightsKey("i")
	if !model.infoOpen || model.infoKey != "cog" || !strings.Contains(ansi.Strip(model.infoView()), "COG") {
		t.Fatal("weights info did not open the shared COG popup")
	}
	model.handleInfoKey("esc")
	model.handleWeightsKey("enter")
	if !model.infoOpen || model.infoKey != "cog" {
		t.Fatal("Enter did not open weight info")
	}
	model.infoOpen = false
	model.help = true
	model.helpCursor = 1
	model.handleHelpKey("i")
	if !model.infoOpen || model.infoKey != "cog" {
		t.Fatal("help info did not open the shared COG popup")
	}
	model.handleInfoKey("esc")
	if !model.help {
		t.Fatal("closing info unexpectedly closed help")
	}
}

func TestEnterClosesPurelyInformationalDialog(t *testing.T) {
	model := Model{infoOpen: true, infoKey: "cog"}
	model.handleInfoKey("enter")
	if model.infoOpen {
		t.Fatal("Enter did not close the informational dialog")
	}
}

func TestEnterDoesNotCloseHelpDialogWithOptions(t *testing.T) {
	model := Model{help: true, helpCursor: 0}
	model.handleHelpKey("enter")
	if !model.help || !model.infoOpen {
		t.Fatal("Enter did not preserve Help while opening its info option")
	}
}

func TestInfoPopupOverlaysItsParentPopup(t *testing.T) {
	model := Model{width: 80, height: 20, weights: defaultWeights(), visible: defaultColumnVisibility(), weightsOpen: true, infoOpen: true, infoKey: "cog"}
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "WEIGHTS") || !strings.Contains(view, "COG  cognitive complexity") {
		t.Fatalf("info did not overlay the weights popup: %q", view)
	}
	model = Model{width: 80, height: 20, help: true, helpCursor: 1, infoOpen: true, infoKey: "cog"}
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "HELP") || !strings.Contains(view, "COG  cognitive complexity") {
		t.Fatalf("info did not overlay the help popup: %q", view)
	}
}

func TestHelpScrollsAndHasNoCloseHint(t *testing.T) {
	model := Model{help: true, helpCursor: len(metricInformation) - 1, width: 60, height: 10}
	view := ansi.Strip(model.helpView())
	if !strings.Contains(view, "PATH") || strings.Contains(view, "SCORE") {
		t.Fatalf("help did not scroll to the selected entry: %q", view)
	}
	if strings.Contains(view, "Esc") || strings.Contains(view, "close help") {
		t.Fatalf("help still shows a close hint: %q", view)
	}
	if !strings.Contains(view, "info") {
		t.Fatalf("help is missing its info option: %q", view)
	}
}

func TestWeightsViewGroupsIndentedMetricsByCategory(t *testing.T) {
	view := ansi.Strip((Model{weights: defaultWeights()}).weightsView())
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
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "component-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Components []struct {
			ID       string            `json:"component_id"`
			Axis     string            `json:"axis"`
			Support  map[string]string `json:"support"`
			Defaults struct {
				Enabled bool `json:"enabled"`
			} `json:"defaults"`
		} `json:"components"`
	}
	if err := json.Unmarshal(payload, &catalog); err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]int, len(catalog.Components))
	for index, component := range catalog.Components {
		byID[component.ID] = index
	}
	seen := map[string]bool{}
	for _, setting := range componentWeights {
		if seen[setting.id] {
			t.Errorf("duplicate weight setting %s", setting.id)
		}
		seen[setting.id] = true
		index, exists := byID[setting.id]
		if !exists {
			t.Errorf("weight setting %s has no catalog component", setting.id)
			continue
		}
		component := catalog.Components[index]
		if !component.Defaults.Enabled {
			t.Errorf("weight setting %s exposes a disabled catalog component", setting.id)
		}
		if component.Axis != setting.axis {
			t.Errorf("weight setting %s axis = %s, catalog = %s", setting.id, setting.axis, component.Axis)
		}
		supported := false
		for _, level := range component.Support {
			if level == "supported" || level == "conformant" || level == "best_effort" {
				supported = true
			}
		}
		if !supported {
			t.Errorf("weight setting %s is unsupported by every language", setting.id)
		}
	}
}

func TestTypeSafetySettingsEnableAnalysisAndScheduleRefresh(t *testing.T) {
	for _, test := range []struct {
		name          string
		selectSetting func(*Model)
		apply         func(*Model) tea.Cmd
	}{
		{
			name: "column",
			selectSetting: func(model *Model) {
				for index, column := range columnNames() {
					if column.key == "typesafety" {
						model.columnCursor = index
						return
					}
				}
			},
			apply: func(model *Model) tea.Cmd {
				_, command := model.handleColumnKey(" ")
				return command
			},
		},
		{
			name: "individual weight",
			selectSetting: func(model *Model) {
				for index, item := range componentWeights {
					if item.id == "explicit_any" {
						model.weightCursor = index
						return
					}
				}
			},
			apply: func(model *Model) tea.Cmd {
				_, command := model.handleWeightsKey(" ")
				return command
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			analyzer := &settingsAnalyzer{}
			model := &Model{
				analyzer: analyzer, visible: defaultColumnVisibility(),
				weights: defaultWeights(), weightEnabled: defaultWeightEnabled(),
				queued: map[string]bool{},
			}
			test.selectSetting(model)
			command := test.apply(model)
			if !analyzer.typeScriptTypes {
				t.Fatal("setting did not enable compiler-aware TypeScript analysis")
			}
			if !model.analyzing || command == nil {
				t.Fatalf("refresh was not scheduled: analyzing=%t command=%v", model.analyzing, command)
			}
			if _, ok := command().(analysisResult); !ok || analyzer.analyzeCalls != 1 {
				t.Fatalf("refresh command did not run analysis: calls=%d", analyzer.analyzeCalls)
			}
		})
	}
}

func TestTypeSafetyRefreshQueuesBehindAnAnalysisAndDisablingNeedsNoRefresh(t *testing.T) {
	analyzer := &settingsAnalyzer{}
	model := &Model{
		analyzer: analyzer, visible: defaultColumnVisibility(),
		weights: defaultWeights(), weightEnabled: defaultWeightEnabled(),
		queued: map[string]bool{}, analyzing: true,
	}
	for index, column := range columnNames() {
		if column.key == "typesafety" {
			model.columnCursor = index
			break
		}
	}
	_, command := model.handleColumnKey(" ")
	if command != nil || !model.pendingFullAnalysis || !analyzer.typeScriptTypes {
		t.Fatalf("enable during analysis = command %v, pending %t, enabled %t", command, model.pendingFullAnalysis, analyzer.typeScriptTypes)
	}
	_, command = model.Update(analysisResult{document: report.Document{}, full: true})
	if command == nil || model.pendingFullAnalysis || !model.analyzing {
		t.Fatalf("queued refresh = command %v, pending %t, analyzing %t", command, model.pendingFullAnalysis, model.analyzing)
	}
	model.analyzing = false
	_, command = model.handleColumnKey(" ")
	if command != nil || analyzer.typeScriptTypes || model.visible["typesafety"] {
		t.Fatalf("disable = command %v, analyzer enabled %t, visible %t", command, analyzer.typeScriptTypes, model.visible["typesafety"])
	}
}

func TestWeightsEnablementControlsScoreIndependently(t *testing.T) {
	component := report.Component{Contribution: 10}
	base := report.Document{Files: []report.File{{Path: "a.go", Complete: true, Components: map[string]report.Component{
		"cognitive_complexity": component,
	}}}}
	model := Model{document: base, baseDocument: base, weights: defaultWeights(), weightEnabled: defaultWeightEnabled()}
	model.rebuildWeightedDocument()
	if got := model.document.Files[0].Score; got != 10 {
		t.Fatalf("enabled weight score = %v, want 10", got)
	}
	model.handleWeightsKey(" ")
	if model.isWeightEnabled("cognitive_complexity") || model.document.Files[0].Score != 0 {
		t.Fatalf("space did not disable weight: enabled=%t score=%v", model.isWeightEnabled("cognitive_complexity"), model.document.Files[0].Score)
	}
	model.handleWeightsKey(" ")
	if !model.isWeightEnabled("cognitive_complexity") || model.document.Files[0].Score != 10 {
		t.Fatalf("space did not re-enable weight: enabled=%t score=%v", model.isWeightEnabled("cognitive_complexity"), model.document.Files[0].Score)
	}
}

func TestWeightEnablementIncludesAndExcludesEveryApplicableComponent(t *testing.T) {
	for _, test := range []struct {
		name     string
		language string
		include  func(item struct {
			id       string
			label    string
			category string
			parent   string
			axis     string
			value    float64
		}) bool
	}{
		{name: "typescript", language: "typescript", include: func(item struct {
			id       string
			label    string
			category string
			parent   string
			axis     string
			value    float64
		}) bool {
			return true
		}},
		{name: "java", language: "java", include: func(item struct {
			id       string
			label    string
			category string
			parent   string
			axis     string
			value    float64
		}) bool {
			return item.axis != "typescript_type_safety"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			components := make(map[string]report.Component)
			applicable := make([]string, 0, len(componentWeights))
			for _, item := range componentWeights {
				if !test.include(item) {
					continue
				}
				components[item.id] = report.Component{Axis: item.axis, Contribution: 1}
				applicable = append(applicable, item.id)
			}
			base := report.Document{Files: []report.File{{Path: "example." + test.language, Language: test.language, Complete: true, Components: components}}}
			model := Model{document: base, baseDocument: base, weights: defaultWeights(), weightEnabled: map[string]bool{}}
			for _, item := range componentWeights {
				model.weightEnabled[item.id] = test.include(item)
			}
			model.rebuildWeightedDocument()
			if got, want := model.document.Files[0].Score, float64(len(applicable)); got != want {
				t.Fatalf("all applicable weights score = %v, want %v", got, want)
			}

			for _, disabledID := range applicable {
				model.weightEnabled[disabledID] = false
				model.rebuildWeightedDocument()
				if got, want := model.document.Files[0].Score, float64(len(applicable)-1); got != want {
					t.Fatalf("disabled %s score = %v, want %v", disabledID, got, want)
				}
				model.weightEnabled[disabledID] = true
			}
		})
	}
}

func TestTypeSafetyColumnIsOffByDefaultAndUsesItsAxis(t *testing.T) {
	model := Model{visible: map[string]bool{}}
	for _, column := range model.activeColumns() {
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
	model = Model{document: base, baseDocument: base, visible: map[string]bool{}, weights: defaultWeights()}
	model.rebuildWeightedDocument()
	if got := model.document.Files[0].Score; got != 0 {
		t.Fatalf("default type safety score = %v, want 0", got)
	}
	for index, column := range columnNames() {
		if column.key == "typesafety" {
			model.columnCursor = index
			break
		}
	}
	model.handleColumnKey(" ")
	if got := model.document.Files[0].Score; got != 12 {
		t.Fatalf("enabled type safety score = %v, want 12", got)
	}
}

func TestNestingColumnIsOffByDefaultAndControlsItsScore(t *testing.T) {
	model := Model{visible: map[string]bool{}}
	for _, column := range model.activeColumns() {
		if column.key == "nesting" {
			t.Fatal("nesting column is enabled by default")
		}
	}
	component := report.Component{Axis: "structural_core", Contribution: 6}
	base := report.Document{Files: []report.File{{Path: "example.go", Complete: true, Score: 6, Components: map[string]report.Component{
		"deeply_nested_if": component,
	}}}}
	model = Model{document: base, baseDocument: base, visible: map[string]bool{}, weights: defaultWeights()}
	model.rebuildWeightedDocument()
	if got := model.document.Files[0].Score; got != 0 {
		t.Fatalf("default nesting score = %v, want 0", got)
	}
	for index, column := range columnNames() {
		if column.key == "nesting" {
			model.columnCursor = index
			break
		}
	}
	model.handleColumnKey(" ")
	if got := model.document.Files[0].Score; got != 6 {
		t.Fatalf("enabled nesting score = %v, want 6", got)
	}
}

func TestCouplingColumnIsOnByDefaultAndControlsItsScore(t *testing.T) {
	model := Model{visible: map[string]bool{"coupling": true}}
	found := false
	for _, column := range model.activeColumns() {
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
	model = Model{document: base, baseDocument: base, visible: map[string]bool{"coupling": true}, weights: defaultWeights()}
	model.rebuildWeightedDocument()
	if got := model.document.Files[0].Score; got != 10 {
		t.Fatalf("default coupling score = %v, want 10", got)
	}
	for index, column := range columnNames() {
		if column.key == "coupling" {
			model.columnCursor = index
			break
		}
	}
	model.handleColumnKey(" ")
	if got := model.document.Files[0].Score; got != 0 {
		t.Fatalf("disabled coupling score = %v, want 0", got)
	}
}

func TestShortWeightsPopupScrollsToSelectedEntry(t *testing.T) {
	model := Model{height: 10, weightCursor: len(componentWeights) - 1, weights: defaultWeights()}
	view := ansi.Strip(model.weightsView())
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
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if !updated.(*Model).sortOpen {
		t.Fatal("o did not open sorting")
	}
	model = Model{}
	updated, _ = model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if updated.(*Model).sortOpen || !updated.(*Model).settings {
		t.Fatal("s still opens sorting")
	}
}

func TestSettingsContainsColumnsAndReturnsAfterEditing(t *testing.T) {
	model := Model{}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	result := updated.(*Model)
	if !strings.Contains(ansi.Strip(result.settingsView()), "Columns") {
		t.Fatal("settings does not contain Columns")
	}
	result.settingsCursor = 1
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	if !result.columns || !result.columnsFromSettings || result.settings {
		t.Fatal("settings did not open Columns")
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	result = updated.(*Model)
	if result.columns || !result.settings {
		t.Fatal("Columns did not return to Settings")
	}
}

func TestModalSelectionsUseBackgroundInsteadOfTextCursors(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 80, settingsCursor: 0, weightCursor: 0}
	for name, view := range map[string]string{
		"settings": model.settingsView(),
		"weights":  model.weightsView(),
		"columns":  model.columnsView(),
		"sort":     model.sortView(),
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

func TestHelpShortcutShowsCompactWideColumnHelp(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{width: 100, height: 24}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if command != nil || !updated.(*Model).help {
		t.Fatal("h did not open help")
	}
	view := updated.(*Model).helpView()
	if lines := strings.Count(view, "\n") + 1; lines >= 20 {
		t.Fatalf("help popup has %d lines, want fewer than 20", lines)
	}
	if width := lipgloss.Width(view); width < 60 {
		t.Fatalf("help popup width = %d, want a wide popup", width)
	}
	for _, title := range []string{"SCORE", "COG", "NPATH", "CYCLO", "SHALLOW", "GOD", "PATH"} {
		if !strings.Contains(view, title) {
			t.Errorf("help popup does not explain %s", title)
		}
	}
	closed, _ := updated.(*Model).handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
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
	model := Model{document: report.Document{Files: []report.File{first, second, third}}}
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
		model.sortKey, model.sortReverse = test.key, test.reverse
		got := model.displayFiles()
		if got[0].Path != test.wantFirst || got[1].Path != test.wantSecond {
			t.Errorf("sort %s reverse=%t = %s, %s", test.key, test.reverse, got[0].Path, got[1].Path)
		}
	}
}

func TestDisplayFilesCacheRefreshesOnlyWhenOrderingChanges(t *testing.T) {
	model := Model{
		document: report.Document{Files: []report.File{
			testFile("low.go", 1), testFile("high.go", 9),
		}},
		sortKey: "score", sortReverse: true,
		visible: defaultColumnVisibility(),
	}
	model.refreshDisplayFiles()
	first := model.displayFiles()
	second := model.displayFiles()
	if len(first) != 2 || first[0].Path != "high.go" {
		t.Fatalf("cached order = %#v", first)
	}
	if &first[0] != &second[0] {
		t.Fatal("displayFiles copied the cached result")
	}

	model.sortCursor = len(sortFields()) - 1 // filename
	model.activateHighlightedSort(false, true)
	if got := model.displayFiles(); got[0].Path != "high.go" {
		t.Fatalf("filename order = %#v", got)
	}
	model.sortCursor = 0 // score
	model.activateHighlightedSort(false, true)
	if got := model.displayFiles(); got[0].Path != "low.go" {
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
		width: 140, height: 50,
		document: report.Document{Files: files}, rows: rows,
		options: Options{TrendWindow: time.Minute},
		sortKey: "score", sortReverse: true,
		visible: defaultColumnVisibility(),
	}
	model.refreshDisplayFiles()
	model.refreshFreshnessStatus()
	b.ResetTimer()
	for range b.N {
		_ = model.tableView()
	}
}

func TestSpaceAppliesSortWithoutChangingDirectionAndPreservesSelectedFile(t *testing.T) {
	first := sortableFile("a.go", 1, 90, 9, 9, 9, 0, 0)
	second := sortableFile("b.go", 2, 10, 1, 1, 1, 0, 0)
	model := Model{
		document: report.Document{Files: []report.File{first, second}},
		selected: "a.go", cursor: 0, sortOpen: true, sortCursor: 0,
		sortKey: "score", sortReverse: false, sortDirections: map[string]bool{"score": false},
		options: Options{TrendWindow: time.Minute},
	}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	result := updated.(*Model)
	if result.sortKey != "score" || result.sortReverse {
		t.Fatalf("sort = %s reverse=%t", result.sortKey, result.sortReverse)
	}
	if !result.sortOpen {
		t.Fatal("applying sort unexpectedly closed the popup")
	}
	if result.selected != "a.go" || result.cursor != 1 {
		t.Fatalf("selection = %q at %d", result.selected, result.cursor)
	}
}

func TestEnterDoesNotApplyOrCloseSort(t *testing.T) {
	model := Model{sortOpen: true, sortCursor: 1, sortKey: "score", sortReverse: true, visible: defaultColumnVisibility()}
	model.handleSortKey("enter")
	if !model.sortOpen || model.sortKey != "score" || !model.sortReverse {
		t.Fatalf("Enter changed sort popup state: open=%t key=%q reverse=%t", model.sortOpen, model.sortKey, model.sortReverse)
	}
}

func TestActiveSortIsMarkedImmediatelyBeforeItsHeading(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		width:   100,
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
	}
	for key, title := range map[string]string{
		"score": "SCORE", "cog": "COG", "npath": "NPATH",
		"cyclo": "CYCLO", "deep": "SHALLOW", "god": "GOD",
	} {
		model.sortKey, model.sortReverse = key, false
		if heading := model.header(); !strings.Contains(heading, "▲"+title) {
			t.Errorf("ascending %s heading has no adjacent indicator: %q", key, heading)
		}
		model.sortReverse = true
		if heading := model.header(); !strings.Contains(heading, "▼"+title) {
			t.Errorf("descending %s heading has no adjacent indicator: %q", key, heading)
		}
	}
	model.sortKey, model.sortReverse = "filename", false
	if heading := model.header(); !strings.Contains(heading, " ▲") {
		t.Fatalf("filename heading has no ascending indicator: %q", heading)
	}
}

func TestHeaderIncludesEveryEnabledTitle(t *testing.T) {
	model := Model{
		width: 100, sortKey: "filename",
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
	}
	heading := ansi.Strip(model.header())
	for _, title := range []string{"SCORE", "COG", "NPATH", "CYCLO", "SHALLOW", "GOD"} {
		if !strings.Contains(heading, title) {
			t.Errorf("enabled title %s is missing from %q", title, heading)
		}
	}
}

func TestOverviewOmitsRankAndSeparatesScoreFromMetrics(t *testing.T) {
	ConfigureTerminalColours()
	file := sortableFile("example.go", 7, 12, 3, 4, 5, 0, 100)
	model := Model{
		width: 100, sortKey: "score", sortReverse: true,
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
		rows:    map[string]rowState{file.Path: {}},
	}
	heading := ansi.Strip(model.header())
	if strings.Contains(heading, "#") {
		t.Fatalf("rank heading remains: %q", heading)
	}
	for _, title := range []string{"SCORE", "COG", "NPATH", "CYCLO", "SHALLOW", "GOD"} {
		if !strings.Contains(heading, title) {
			t.Errorf("enabled title %s is missing from %q", title, heading)
		}
	}
	row := ansi.Strip(model.renderRow(file, false))
	if !strings.HasPrefix(row, "     12  3") {
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
	model := Model{visible: defaultColumnVisibility(), sortCursor: 1}
	view := ansi.Strip(model.sortView())
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
	for model.sortCursor != len(sortFields())-1 {
		model.handleSortKey("down")
		if model.sortCursor == 0 {
			t.Fatal("sort cursor wrapped while moving down")
		}
	}
	if sortFields()[model.sortCursor].key != "filename" {
		t.Fatalf("sort cursor landed on %q, want filename", sortFields()[model.sortCursor].key)
	}
}

func TestSortDirectionChangesOnlyHighlightedMetricAndActivatesIt(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		visible: defaultColumnVisibility(), sortCursor: 1,
		sortKey: "score", sortReverse: true,
		sortDirections: map[string]bool{"score": true, "cog": false, "npath": false},
	}
	model.handleSortKey("right")
	if model.sortKey != "cog" || !model.sortReverse {
		t.Fatalf("right did not activate descending COG: key=%q reverse=%t", model.sortKey, model.sortReverse)
	}
	if !model.sortDirections["score"] || model.sortDirections["npath"] {
		t.Fatalf("right changed another metric's direction: %#v", model.sortDirections)
	}
	view := ansi.Strip(model.sortView())
	for _, want := range []string{"▼ SCORE", "▼ COG", "▲ NPATH"} {
		if !strings.Contains(view, want) {
			t.Errorf("sort view is missing %q: %q", want, view)
		}
	}
}

func TestSortCursorBackgroundDoesNotRemainOnActiveMetric(t *testing.T) {
	ConfigureTerminalColours()
	model := Model{
		visible: defaultColumnVisibility(), sortCursor: 1,
		sortKey: "score", sortReverse: true, sortDirections: map[string]bool{"score": true, "cog": true},
	}
	lines := strings.Split(model.sortView(), "\n")
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
	model.handleSortKey("down")
	lines = strings.Split(model.sortView(), "\n")
	if strings.Contains(lineWith("COG"), wantBackground) || !strings.Contains(lineWith("NPATH"), wantBackground) {
		t.Fatal("cursor background did not move with the highlighted row")
	}
}

func TestEscapeClosesSortAndColumnsDialogs(t *testing.T) {
	for name, model := range map[string]*Model{
		"sort":    {sortOpen: true},
		"columns": {columns: true},
	} {
		updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
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
	model := Model{
		width: 80, height: 24, detail: true, selected: file.Path,
		document: report.Document{Files: []report.File{file}},
		rows:     map[string]rowState{file.Path: {}}, sortKey: "score", sortReverse: true,
	}
	view := model.detailView()
	if got := lipgloss.Width(view); got != 74 {
		t.Fatalf("detail width = %d, want 74", got)
	}
	if got := lipgloss.Height(view); got != 21 {
		t.Fatalf("detail height = %d, want 21", got)
	}
	if !strings.Contains(ansi.Strip(view), "█") {
		t.Fatal("scrollable detail has no visible scrollbar thumb")
	}
	maximum := model.detailMaxOffset()
	if maximum <= 0 {
		t.Fatal("long detail unexpectedly fits without scrolling")
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyEnd})
	if model.detailOffset != maximum {
		t.Fatalf("End scrolled to %d, want %d", model.detailOffset, maximum)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if model.detailOffset != maximum {
		t.Fatalf("scroll escaped lower bound: %d > %d", model.detailOffset, maximum)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyHome})
	if model.detailOffset != 0 {
		t.Fatalf("Home left detail offset at %d", model.detailOffset)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if model.detailOffset <= 0 || model.detailOffset > maximum {
		t.Fatalf("page scroll produced invalid offset %d of %d", model.detailOffset, maximum)
	}
}

func TestDetailPopupShrinksWithTerminal(t *testing.T) {
	file := testFile("example.go", 1)
	model := Model{
		width: 24, height: 8, selected: file.Path,
		document: report.Document{Files: []report.File{file}},
		rows:     map[string]rowState{file.Path: {}},
	}
	view := model.detailView()
	if lipgloss.Width(view) > model.width || lipgloss.Height(view) > model.height {
		t.Fatalf("detail %dx%d exceeds terminal %dx%d", lipgloss.Width(view), lipgloss.Height(view), model.width, model.height)
	}
}

func TestSourceViewOpensSelectedFileAndUsesViewportScrolling(t *testing.T) {
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
	model := Model{
		width: 80, height: 24, selected: "example.go", options: Options{Workspace: root},
		document: report.Document{Files: []report.File{testFile("example.go", 1)}},
		rows:     map[string]rowState{"example.go": {}}, visible: map[string]bool{},
	}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if command != nil || !updated.(*Model).sourceView {
		t.Fatal("v did not open source view")
	}
	model = *updated.(*Model)
	view := ansi.Strip(model.sourceViewView())
	if !strings.Contains(view, "func Run()") {
		t.Fatal("source view did not render selected file")
	}
	if !strings.Contains(view, "example.go") || !strings.Contains(view, "45 lines") || strings.Contains(view, "SOURCE  example.go") {
		t.Fatalf("source header does not show path and line count: %q", view)
	}
	if !strings.Contains(view, "ctrl-f/b page  g/G jump") || !strings.Contains(view, "find  n/N next  ESC close") {
		t.Fatalf("source footer does not contain the navigation and close groups: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "45 lines") && !strings.HasSuffix(line, "45 lines │") {
			t.Errorf("line count is not inset one column from the right border: %q", line)
		}
		if strings.Contains(line, "ESC close") && !strings.HasSuffix(line, "ESC close │") {
			t.Errorf("footer actions are not inset one column from the right border: %q", line)
		}
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.sourceViewport.YOffset != 1 {
		t.Fatalf("first j moved source viewport by %d lines, want 1", model.sourceViewport.YOffset)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.sourceViewport.YOffset != 2 {
		t.Fatalf("second j moved source viewport by %d lines, want 2 total", model.sourceViewport.YOffset)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.sourceViewport.YOffset != 4 {
		t.Fatalf("established repeat moved source viewport by %d lines, want 4 total", model.sourceViewport.YOffset)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.sourceViewport.YOffset == 0 {
		t.Fatal("Ctrl-F did not advance source viewport")
	}
	longLine := strings.Repeat("x", 200)
	model.sourceViewport.SetContent(longLine + "\n" + contents)
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if model.sourceViewport.HorizontalScrollPercent() == 0 {
		t.Fatal("l did not horizontally scroll source viewport")
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if model.sourceViewport.YOffset != 0 {
		t.Fatalf("g moved source viewport to line %d, want top", model.sourceViewport.YOffset)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if model.sourceViewport.YOffset == 0 {
		t.Fatal("G did not jump to the bottom of the source viewport")
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if model.sourceView || model.sourcePath != "" {
		t.Fatal("Esc did not close source view")
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
	watcher, err := newSourceWatcher(root, []string{"."}, false, nil)
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

func TestFileTargetDoesNotWatchItsSiblings(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "selected.go")
	if err := os.WriteFile(target, []byte("package selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	watcher, err := newSourceWatcher(root, []string{"selected.go"}, false, nil)
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
