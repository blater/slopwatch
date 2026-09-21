package follow

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func unavailableScoreFile(path string, score float64) report.File {
	file := testFile(path, score)
	file.Complete = false
	file.Coverage = map[string]string{"score": "failed"}
	return file
}

func TestScoreDistributionUsesExactBoundariesAndOverflow(t *testing.T) {
	files := []report.File{
		testFile("zero.go", 0), testFile("lower-edge.go", 4.999), testFile("five.go", 5),
		testFile("ninety-nine.go", 99.999), testFile("hundred.go", 100), testFile("overflow.go", 790),
		unavailableScoreFile("failed.go", 12), testFile("nan.go", math.NaN()), testFile("inf.go", math.Inf(1)),
	}
	distribution := buildScoreDistribution(files)
	if distribution.bins[0] != 2 || distribution.bins[1] != 1 || distribution.bins[19] != 1 || distribution.bins[20] != 2 {
		t.Fatalf("score bins = %#v", distribution.bins)
	}
	if distribution.unavailable != 3 || distribution.total != len(files) {
		t.Fatalf("unavailable/total = %d/%d", distribution.unavailable, distribution.total)
	}
}

func TestPartialAggregateSortsAndJoinsScoreDistribution(t *testing.T) {
	partial := testFile("partial.ts", 10)
	partial.Complete = false
	partial.Components["cognitive_complexity"] = report.Component{Contribution: 10}
	partial.Components["module_shallowness"] = report.Component{DepthVersion: "responsibility-burden-v4", DepthState: "partial"}
	partial.Coverage = map[string]string{"cognitive_complexity": "complete", "module_shallowness": "partial"}
	failed := unavailableScoreFile("failed.ts", 0)
	model := Model{files: FilesState{SortKey: "score"}}
	if !filesLess(model.files.SortKey, false, partial, failed) {
		t.Fatal("partial aggregate did not sort ahead of unavailable score")
	}
	distribution := buildScoreDistribution([]report.File{partial, failed})
	if distribution.bins[2] != 1 || distribution.unavailable != 1 {
		t.Fatalf("partial aggregate distribution = bins %#v unavailable %d", distribution.bins, distribution.unavailable)
	}
}

func TestIncrementalScoreDistributionReplacesAndDeletesAffectedFiles(t *testing.T) {
	model := Model{files: FilesState{
		Document:     report.Document{Files: []report.File{testFile("low.go", 4), testFile("high.go", 95), testFile("gone.go", 100)}},
		BaseDocument: report.Document{Files: []report.File{testFile("low.go", 4), testFile("high.go", 95), testFile("gone.go", 100)}},
		Rows:         map[string]rowState{},
	}}
	model.files.rebuildScoreDistribution()
	merge(&model, analysisResult{
		document: report.Document{Files: []report.File{testFile("low.go", 55)}},
		replace:  []string{"low.go", "gone.go"},
	})
	distribution := model.files.ScoreDistribution
	if distribution.total != 2 || distribution.unavailable != 0 || distribution.bins[11] != 1 || distribution.bins[19] != 1 {
		t.Fatalf("incremental distribution = %#v total=%d unavailable=%d", distribution.bins, distribution.total, distribution.unavailable)
	}
}

func TestScoreDistributionTracksFreshnessWithoutChangingTotals(t *testing.T) {
	model := Model{files: FilesState{
		Document:     report.Document{Files: []report.File{testFile("a.go", 12), testFile("b.go", 12)}},
		BaseDocument: report.Document{Files: []report.File{testFile("a.go", 12), testFile("b.go", 12)}},
	}}
	model.files.rebuildScoreDistribution()
	markFreshness(&model, []string{"a.go"}, report.FreshnessRefreshing, "source changed")
	if model.files.ScoreDistribution.bins[2] != 2 || model.files.ScoreDistribution.currentBins[2] != 1 {
		t.Fatalf("refreshing freshness counts = total %d current %d", model.files.ScoreDistribution.bins[2], model.files.ScoreDistribution.currentBins[2])
	}
	markFreshness(&model, []string{"a.go"}, report.FreshnessCurrent, "verified")
	if model.files.ScoreDistribution.currentBins[2] != 2 {
		t.Fatalf("verified freshness count = %d, want 2", model.files.ScoreDistribution.currentBins[2])
	}
}

func TestReweightRebuildsScoreDistribution(t *testing.T) {
	file := testFile("weighted.go", 10)
	file.Components["cognitive_complexity"] = report.Component{Contribution: 10}
	model := Model{files: FilesState{BaseDocument: report.Document{Files: []report.File{file}}}, weights: map[string]float64{"cognitive_complexity": 10}, weightEnabled: map[string]bool{"cognitive_complexity": true}}
	rebuildWeightedDocument(&model)
	if model.files.ScoreDistribution.bins[2] != 1 {
		t.Fatalf("initial weighted bin = %#v", model.files.ScoreDistribution.bins)
	}
	model.weights["cognitive_complexity"] = 20
	rebuildWeightedDocument(&model)
	if model.files.ScoreDistribution.bins[4] != 1 || model.files.ScoreDistribution.bins[2] != 0 {
		t.Fatalf("reweighted bins = %#v", model.files.ScoreDistribution.bins)
	}
}

func TestScoreDistributionHeaderKeepsGraphStartStableAcrossStatusChanges(t *testing.T) {
	model := Model{width: 160, files: FilesState{Document: report.Document{Files: []report.File{testFile("a.go", 2), testFile("b.go", 76), testFile("c.go", 101)}}}, repositoryIdentity: "repo", options: Options{Workspace: "/workspace"}}
	model.files.rebuildScoreDistribution()
	model.status = "OK"
	first, _, firstBottom, _ := tableTopParts(model)
	model.status = "CACHE REFRESHING 12"
	second, _, _, _ := tableTopParts(model)
	firstPlain, secondPlain := ansi.Strip(first), ansi.Strip(second)
	if strings.Contains(firstPlain, "SCORE") || !strings.Contains(ansi.Strip(firstBottom), "⁰") || !strings.Contains(ansi.Strip(firstBottom), "¹⁰⁰⁺") {
		t.Fatalf("graph labels missing: first=%q bottom=%q", firstPlain, ansi.Strip(firstBottom))
	}
	graphStart := lipgloss.Width(tableLogo) + 3
	if got := strings.Index(ansi.Strip(firstBottom), "⁰"); got != graphStart {
		t.Fatalf("graph start = %d, want %d: %q", got, graphStart, ansi.Strip(firstBottom))
	}
	if !strings.Contains(secondPlain, "CACHE REFRESHIN") {
		t.Fatalf("status was not preserved: %q", secondPlain)
	}
}

func TestScoreDistributionHeaderPreview(t *testing.T) {
	files := []report.File{
		testFile("green.go", 2), testFile("yellow.go", 50), testFile("orange.go", 75),
		testFile("red.go", 100), testFile("overflow.go", 790), unavailableScoreFile("pending.go", 0),
	}
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		for _, width := range []int{80, 120, 160} {
			model := Model{width: width, repositoryIdentity: "repo", options: Options{Workspace: "/workspace"}, files: FilesState{Document: report.Document{Files: files}}}
			model.files.rebuildScoreDistribution()
			top, _, bottom, _ := tableTopParts(model)
			topPlain, bottomPlain := ansi.Strip(top), ansi.Strip(bottom)
			if width == 80 && (strings.Contains(topPlain, "SCORE") || !strings.Contains(bottomPlain, "⁰") || !strings.Contains(bottomPlain, "¹⁰⁰⁺")) {
				t.Fatalf("width-80 graph axis missing: %q / %q", topPlain, bottomPlain)
			}
			t.Logf("%s width=%d\n%s\n%s", theme, width, topPlain, bottomPlain)
		}
	}
	ConfigureTheme("dark")
}

func TestHeaderStatusCellsUseFixedPaintedCapacityAndPriority(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previousProfile)
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		model := Model{
			analyzing:      true,
			animationFrame: 0,
			files: FilesState{
				FreshnessStatusReady: true,
				FreshnessStatusText:  "CACHE REFRESHING 80000 · STALE 2",
			},
			fixUpdates: fixSubscriptionState{stale: true},
		}
		top, bottom := headerStatusCells(model, 16)
		if top != "⠋ STALE 2" || bottom != "AGENTS STALE" {
			t.Fatalf("%s status priority = %q / %q", theme, top, bottom)
		}
		for _, value := range []string{top, bottom, ""} {
			cell := renderHeaderStatusCell(value, 16)
			if lipgloss.Width(ansi.Strip(cell)) != 16 || !strings.Contains(cell, ";48;2") {
				t.Fatalf("%s status cell was not painted across its padding: width=%d ansi=%q", theme, lipgloss.Width(ansi.Strip(cell)), cell)
			}
			reverseSequences := strings.Count(cell, "\x1b[7;") + strings.Count(cell, "\x1b[7m")
			wantReverseSequences := 0
			if value != "" {
				wantReverseSequences = 1
			}
			if reverseSequences != wantReverseSequences {
				t.Fatalf("%s status reverse video sequences = %d, want %d for value %q, ansi=%q", theme, reverseSequences, wantReverseSequences, value, cell)
			}
		}
	}
	ConfigureTheme(style.ThemeDark)
}

func TestHeaderStatusCellsHideIdleAndKeepLongNoticeInModel(t *testing.T) {
	model := Model{
		status: "ERROR: a deliberately long notice that must remain in model state",
		files:  FilesState{ScoreDistribution: scoreDistribution{ready: true, total: 80_000}},
		agents: AgentsState{Jobs: []fix.JobPresentation{{Phase: fix.PhaseCompleted}}},
	}
	top, bottom := headerStatusCells(model, 16)
	if model.status == "" || top == "" || lipgloss.Width(top) > 16 || !strings.HasSuffix(top, "…") {
		t.Fatalf("long notice display/model preservation = %q / %q", top, model.status)
	}
	if bottom != "" {
		t.Fatalf("completed agent history leaked into status: %q", bottom)
	}
	model.status = ""
	top, bottom = headerStatusCells(model, 16)
	if top != "" || bottom != "" {
		t.Fatalf("normal ready idle status = %q / %q", top, bottom)
	}
}

func TestScoreDistributionAxisAlignsWithScoreBars(t *testing.T) {
	distribution := scoreDistribution{ready: true}
	distribution.bins[0], distribution.currentBins[0] = 1, 1
	model := Model{files: FilesState{ScoreDistribution: distribution}}
	top, bottom := renderScoreDistribution(model, 60)
	topPlain, bottomPlain := ansi.Strip(top), ansi.Strip(bottom)
	if got, want := strings.Index(bottomPlain, "⁰"), 0; got != want {
		t.Fatalf("axis starts at %d, want %d: %q / %q", got, want, topPlain, bottomPlain)
	}
	if !strings.Contains(bottomPlain, "¹⁰⁰⁺") {
		t.Fatalf("axis did not reserve overflow padding: %q", bottomPlain)
	}
}

func TestScoreDistributionResponsiveMinimumWidth(t *testing.T) {
	distribution := scoreDistribution{ready: true}
	model := Model{files: FilesState{ScoreDistribution: distribution}}
	top, bottom := renderScoreDistribution(model, 6)
	if strings.Contains(ansi.Strip(top), "SCORE") || !strings.Contains(ansi.Strip(bottom), "⁰") || !strings.Contains(ansi.Strip(bottom), "¹⁰⁰⁺") {
		t.Fatalf("minimum-width graph = %q / %q", ansi.Strip(top), ansi.Strip(bottom))
	}
	if top, bottom := renderScoreDistribution(model, 5); top != "" || bottom != "" {
		t.Fatalf("graph rendered below six cells: %q / %q", top, bottom)
	}
}

func TestScoreDistributionSparseGraphKeepsEmptyBinsBlank(t *testing.T) {
	distribution := scoreDistribution{ready: true}
	distribution.bins[20], distribution.currentBins[20] = 1, 1
	model := Model{files: FilesState{ScoreDistribution: distribution}}
	top, bottom := renderScoreDistribution(model, 60)
	if strings.Count(ansi.Strip(top), "█") == 0 || strings.Contains(ansi.Strip(bottom), "█") || !strings.Contains(ansi.Strip(bottom), "¹⁰⁰⁺") {
		t.Fatalf("sparse graph blocks = %q / %q", ansi.Strip(top), ansi.Strip(bottom))
	}
}

func TestScoreDistributionRepaintsWhitespaceForEachTheme(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(previousProfile)
	lipgloss.SetColorProfile(termenv.TrueColor)
	model := Model{files: FilesState{ScoreDistribution: scoreDistribution{ready: true}}}
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		top, bottom := renderScoreDistribution(model, 30)
		for _, line := range []string{top, bottom} {
			if lipgloss.Width(ansi.Strip(line)) != 30 || (!strings.Contains(line, ";48;2") && !strings.Contains(line, "\x1b[48;2")) {
				t.Fatalf("%s graph line was not fully repainted: width=%d line=%q", theme, lipgloss.Width(ansi.Strip(line)), line)
			}
		}
	}
	ConfigureTheme(style.ThemeDark)
}

func TestScoreDistributionEmptyColumnsUseMutedColour(t *testing.T) {
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		ConfigureTheme(theme)
		if got := scoreDistributionColour(0, true, 0); got != style.TextMuted {
			t.Fatalf("%s empty column colour = %v, want muted %v", theme, got, style.TextMuted)
		}
		if got := scoreDistributionColour(0, true, 1); got == style.TextMuted {
			t.Fatalf("%s populated current column lost palette colour", theme)
		}
		if got := scoreDistributionColour(0, false, 1); got == style.TextMuted {
			t.Fatalf("%s populated provisional column lost grey colour", theme)
		}
	}
	ConfigureTheme(style.ThemeDark)
}

func TestResponsiveHeaderSplitsRemainingSpaceEvenly(t *testing.T) {
	gap, right, graph, _ := headerColumnWidths(200, 11, 8)
	if gap != 3 || right < 20 || absInt(right-graph) > 1 {
		t.Fatalf("header split = gap %d right %d graph %d", gap, right, graph)
	}
}

func TestResponsiveHeaderKeepsLongRightLabelsWithinTwentyCells(t *testing.T) {
	long := "branch/with/a/very/long/repository/path/slopwatch"
	for _, width := range []int{60, 80, 120, 200, 300} {
		_, right, _, _ := headerColumnWidths(width, 11, 0)
		if right < 20 {
			continue
		}
		for _, value := range []string{long, "/Users/example/workspaces/slopwatch"} {
			label := responsiveHeaderLabel(value, right)
			if lipgloss.Width(label) > right || !strings.HasSuffix(label, "slopwatch") {
				t.Fatalf("width %d right label = %q at budget %d", width, label, right)
			}
		}
	}
}

func TestResponsiveGraphWidthsRemainPaintedAndTicksDoNotTouch(t *testing.T) {
	distribution := scoreDistribution{ready: true}
	distribution.bins[0], distribution.currentBins[0] = 1, 1
	distribution.bins[20], distribution.currentBins[20] = 1, 1
	model := Model{files: FilesState{ScoreDistribution: distribution}}
	for width := 6; width <= 100; width++ {
		top, bottom := renderScoreDistribution(model, width)
		if lipgloss.Width(ansi.Strip(top)) != width || lipgloss.Width(ansi.Strip(bottom)) != width {
			t.Fatalf("width %d rendered %d/%d cells", width, lipgloss.Width(ansi.Strip(top)), lipgloss.Width(ansi.Strip(bottom)))
		}
		axis := ansi.Strip(bottom)
		if strings.Contains(axis, "⁰²⁵") || strings.Contains(axis, "²⁵⁵⁰") || strings.Contains(axis, "⁵⁰⁷⁵") || strings.Contains(axis, "⁷⁵¹⁰⁰⁺") {
			t.Fatalf("width %d touching axis ticks: %q", width, axis)
		}
	}
}

func TestNarrowGraphGroupsNormalBucketsAndKeepsOverflowSeparate(t *testing.T) {
	distribution := scoreDistribution{ready: true}
	for bucket := 0; bucket < scoreDistributionBuckets-1; bucket++ {
		distribution.bins[bucket] = 1
		distribution.currentBins[bucket] = 1
	}
	distribution.bins[20] = 3
	columns := scoreDistributionColumns(distribution, 6)
	total := 0
	for _, column := range columns {
		total += column.count
	}
	if total != 23 || columns[5].count != 3 {
		t.Fatalf("narrow columns = %#v, total %d", columns, total)
	}
	distribution.currentBins[0] = 0
	if scoreDistributionColumns(distribution, 6)[0].current {
		t.Fatal("group containing provisional sample was colored current")
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
