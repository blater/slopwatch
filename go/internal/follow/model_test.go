package follow

import (
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

	"github.com/slopslap/slopslap/internal/report"
)

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

func TestInitialScanRendersCenteredAnimatedStatus(t *testing.T) {
	model := Model{width: 80, height: 20, analyzing: true, animationFrame: 1}
	view := ansi.Strip(model.tableView())
	if !strings.Contains(view, "SCANNING WORKSPACE") {
		t.Fatalf("initial scan status is missing: %q", view)
	}
	if !strings.Contains(view, "⠙") {
		t.Fatalf("initial scan animation frame is missing: %q", view)
	}
	lines := strings.Split(view, "\n")
	statusLine := -1
	for index, line := range lines {
		if strings.Contains(line, "SCANNING WORKSPACE") {
			statusLine = index
			break
		}
	}
	if statusLine != (model.bodyHeight()/2)+2 {
		t.Fatalf("status line = %d, want centered body line %d", statusLine, (model.bodyHeight()/2)+2)
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
	model := Model{
		rows: map[string]rowState{"a.go": {
			scoreChangedAt: time.Now().Add(-2 * time.Minute), movementDelta: 1,
		}},
		options: Options{TrendWindow: time.Minute},
	}
	model.expireTrends(time.Now())
	state := model.rows["a.go"]
	if !state.scoreChangedAt.IsZero() || state.movementDelta != 0 {
		t.Fatalf("expired movement indicator retained state: %#v", state)
	}
}

func TestNewFileMarkerUsesRAGColoursAndExpires(t *testing.T) {
	now := time.Now()
	state := rowState{newFileAt: now}
	if marker, colour, ok := newFileMarker(90, 100, state, now); !ok || marker != "●" || colour != colourGreen {
		t.Fatalf("initial new-file marker = %q, %q, %t; want green dot", marker, colour, ok)
	}
	state.newFileMoved = true
	if _, colour, _ := newFileMarker(50, 100, state, now); colour != colourAmber {
		t.Fatalf("top-half new-file colour = %q, want amber", colour)
	}
	if _, colour, _ := newFileMarker(10, 100, state, now); colour != colourRed {
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
	if marker, colour := model.rowMarker(model.document.Files[0], state, time.Now()); marker != "●" || colour != colourRed {
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
	file := testFile("parent/example.go", 12)
	model := Model{
		width: 80, rows: map[string]rowState{file.Path: {}},
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
	}
	row := model.renderRow(file, true)
	// termenv rounds the final channel of #183b52 down by one while encoding it.
	// Match the common RGB payload whether or not a foreground shares its CSI.
	wantBackground := "48;2;24;59;81m"
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

func TestOverviewUsesReferencePalette(t *testing.T) {
	palette := map[string]lipgloss.Color{
		"text": colourText, "muted": colourMuted, "green": colourGreen,
		"amber": colourAmber, "red": colourRed, "score amber": scoreAmber,
		"score red": scoreRed, "blue": colourBlue, "selection": selectedBackground,
		"screen": screenBackground, "top": topBackground,
		"header": headerBackground, "footer": footerBackground,
	}
	want := map[string]string{
		"text": "#d5e2eb", "muted": "#668298", "green": "#58e7ad",
		"amber": "#f0c765", "red": "#ff8291", "score amber": "#f5c451",
		"score red": "#ff6174", "blue": "#6fb9e8", "selection": "#183b52",
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
	for _, unwanted := range []string{"Enter", "details", "space", "pause"} {
		if strings.Contains(footer, unwanted) {
			t.Errorf("footer still contains %q: %q", unwanted, footer)
		}
	}
	if !strings.Contains(ansi.Strip(footer), "sort") {
		t.Fatalf("footer does not advertise sorting: %q", footer)
	}
}

func TestKeyHintHighlightsHotkeyWhereItOccurs(t *testing.T) {
	ConfigureTerminalColours()
	hint := keyHint("o", "columns", footerBackground)
	if got := ansi.Strip(hint); got != "columns" {
		t.Fatalf("key hint changed label to %q", got)
	}
	if !strings.Contains(hint, "o") {
		t.Fatalf("key hint does not contain highlighted hotkey: %q", hint)
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

func TestApplyingSortPreservesSelectedFile(t *testing.T) {
	first := sortableFile("a.go", 1, 90, 9, 9, 9, 0, 0)
	second := sortableFile("b.go", 2, 10, 1, 1, 1, 0, 0)
	model := Model{
		document: report.Document{Files: []report.File{first, second}},
		selected: "a.go", cursor: 0, sortOpen: true, sortCursor: 0,
		pendingSort: false, options: Options{TrendWindow: time.Minute},
	}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	result := updated.(*Model)
	if result.sortKey != "score" || result.sortReverse {
		t.Fatalf("sort = %s reverse=%t", result.sortKey, result.sortReverse)
	}
	if result.selected != "a.go" || result.cursor != 1 {
		t.Fatalf("selection = %q at %d", result.selected, result.cursor)
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

func TestHeaderUsesRequestedShallowTitleOffsets(t *testing.T) {
	model := Model{
		width: 100, sortKey: "filename",
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
	}
	heading := ansi.Strip(model.header())
	for _, want := range []struct {
		title string
		at    int
	}{
		{"SCORE", 0}, {"COG", 8}, {"NPATH", 14}, {"CYCLO", 23}, {"SHALLOW", 29},
	} {
		if at := strings.Index(heading, want.title); at != want.at {
			t.Errorf("%s starts at %d, want %d in %q", want.title, at, want.at, heading)
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
	if !strings.Contains(heading, "▼SCORE  COG") {
		t.Fatalf("score/COG spacing is wrong: %q", heading)
	}
	if !strings.Contains(heading, "SHALLOW   GOD") {
		t.Fatalf("SHALLOW/GOD/path spacing is wrong: %q", heading)
	}
	row := ansi.Strip(model.renderRow(file, false))
	if !strings.HasPrefix(row, "   12.0  3") {
		t.Fatalf("rank or score/COG spacing is wrong: %q", row)
	}
	if !strings.Contains(row, "100.0  example.go") {
		t.Fatalf("GOD/path spacing is wrong: %q", row)
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
	if !strings.Contains(ansi.Strip(model.sourceViewView()), "func Run()") {
		t.Fatal("source view did not render selected file")
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

func TestTargetedAnalysisNarrowsExplicitLanguages(t *testing.T) {
	got := languagesForPaths([]string{"a.go", "b.java", "c.go"})
	if strings.Join(got, ",") != "go,java" {
		t.Fatalf("languagesForPaths() = %v", got)
	}
}
