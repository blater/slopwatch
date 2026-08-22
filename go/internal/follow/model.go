package follow

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/report"
)

type Options struct {
	Workspace    string
	Targets      []string
	Languages    []string
	IncludeTests bool
	Limit        int
	TrendWindow  time.Duration
	Compact      bool
}

type Analyzer interface {
	Analyze(context.Context, []string, []string) (report.Document, error)
}

type analysisResult struct {
	document report.Document
	replace  []string
	full     bool
	err      error
}

type animationTick time.Time

type rowState struct {
	editedAt       time.Time
	direction      int
	scoreChangedAt time.Time
	movementDelta  int
	newFileAt      time.Time
	newFileRank    int
	newFileMoved   bool
	ranks          []rankPoint
}

type rankPoint struct {
	at   time.Time
	rank int
}

type Model struct {
	analyzer         Analyzer
	watcher          *sourceWatcher
	options          Options
	document         report.Document
	rows             map[string]rowState
	width            int
	height           int
	cursor           int
	offset           int
	selected         string
	analyzing        bool
	queued           map[string]bool
	status           string
	initialAnalysis  bool
	animationFrame   int
	detail           bool
	detailOffset     int
	help             bool
	columns          bool
	columnCursor     int
	sortOpen         bool
	sortCursor       int
	sortKey          string
	sortReverse      bool
	pendingSort      bool
	sourceView       bool
	sourcePath       string
	sourceViewport   viewport.Model
	sourceLastKey    string
	sourceLastAt     time.Time
	sourceRapid      int
	sourceSearchText string
	findInput        textinput.Model
	findOpen         bool
	findQuery        string
	findSource       bool
	visible          map[string]bool
}

func New(document report.Document, analyzer Analyzer, options Options) (*Model, error) {
	watcher, err := newSourceWatcher(
		options.Workspace, options.Targets, options.IncludeTests, options.Languages,
	)
	if err != nil {
		return nil, err
	}
	document.SortAndRank()
	rows := make(map[string]rowState, len(document.Files))
	now := time.Now()
	for _, file := range document.Files {
		rows[file.Path] = rowState{ranks: []rankPoint{{at: now, rank: file.Rank}}}
	}
	if options.TrendWindow <= 0 {
		options.TrendWindow = 10 * time.Minute
	}
	findInput := textinput.New()
	findInput.Prompt = "/ "
	findInput.Placeholder = "find"
	findInput.CharLimit = 256
	model := &Model{
		analyzer: analyzer, watcher: watcher, options: options, document: document,
		rows: rows, queued: map[string]bool{},
		findInput: findInput,
		sortKey:   "score", sortReverse: true,
		visible: map[string]bool{"cog": true, "npath": true, "cyclo": true, "deep": true, "god": true},
	}
	if len(document.Files) > 0 {
		model.selected = document.Files[0].Path
	}
	return model, nil
}

func (model *Model) Close() { model.watcher.close() }

// StartInitialAnalysis makes the first scan run after Bubble Tea has entered
// the alternate screen, allowing the empty dashboard to render immediately.
func (model *Model) StartInitialAnalysis() {
	model.analyzing = true
	model.initialAnalysis = true
}

func (model Model) Init() tea.Cmd {
	commands := []tea.Cmd{model.waitForChange(), tickAnimation()}
	if model.initialAnalysis {
		commands = append(commands, model.analyze(nil, true))
	}
	return tea.Batch(commands...)
}

func tickAnimation() tea.Cmd {
	return tea.Tick(125*time.Millisecond, func(at time.Time) tea.Msg { return animationTick(at) })
}

func (model Model) waitForChange() tea.Cmd {
	return func() tea.Msg { return <-model.watcher.changes }
}

func (model Model) analyze(paths []string, full bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		targets := paths
		if full {
			targets = model.options.Targets
		}
		languages := languagesForPaths(paths)
		if full {
			languages = nil
		}
		document, err := model.analyzer.Analyze(ctx, targets, languages)
		return analysisResult{document: document, replace: paths, full: full, err: err}
	}
}

func languagesForPaths(paths []string) []string {
	seen := map[string]bool{}
	for _, path := range paths {
		language := ""
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go":
			language = "go"
		case ".java":
			language = "java"
		case ".rs":
			language = "rust"
		case ".ts", ".tsx", ".mts", ".cts":
			language = "typescript"
		}
		if language != "" {
			seen[language] = true
		}
	}
	result := make([]string, 0, len(seen))
	for language := range seen {
		result = append(result, language)
	}
	sort.Strings(result)
	return result
}

func (model Model) bodyHeight() int { return max(1, model.height-3) }

func (model *Model) ensureVisible() {
	page := model.bodyHeight()
	if model.cursor < model.offset {
		model.offset = model.cursor
	}
	if model.cursor >= model.offset+page {
		model.offset = model.cursor - page + 1
	}
	maxOffset := max(0, len(model.displayFiles())-page)
	model.offset = min(model.offset, maxOffset)
}

func (model Model) View() string {
	if model.width <= 0 || model.height <= 0 {
		return ""
	}
	base := model.tableView()
	if model.detail {
		return model.overlay(base, model.detailView())
	}
	if model.sourceView {
		return model.overlay(base, model.sourceViewView())
	}
	if model.help {
		return model.overlayBelowTitle(base, model.helpView())
	}
	if model.columns {
		return model.overlay(base, model.columnsView())
	}
	if model.sortOpen {
		return model.overlay(base, model.sortView())
	}
	return base
}

var (
	colourMuted        = lipgloss.Color("#668298")
	colourText         = lipgloss.Color("#d5e2eb")
	colourGreen        = lipgloss.Color("#58e7ad")
	colourAmber        = lipgloss.Color("#f0c765")
	colourRed          = lipgloss.Color("#ff8291")
	scoreAmber         = lipgloss.Color("#f5c451")
	scoreRed           = lipgloss.Color("#ff6174")
	colourBlue         = lipgloss.Color("#6fb9e8")
	selectedBackground = lipgloss.Color("#183b52")
	screenBackground   = lipgloss.Color("#071019")
	topBackground      = lipgloss.Color("#0a1622")
	headerBackground   = lipgloss.Color("#0b1e2d")
	footerBackground   = lipgloss.Color("#061019")
)

func (model Model) columnsView() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(colourBlue).Render("Columns"), ""}
	for index, column := range columnNames() {
		cursor := "  "
		if index == model.columnCursor {
			cursor = "› "
		}
		mark := " "
		if model.visible[column.key] {
			mark = "✓"
		}
		lines = append(lines, fmt.Sprintf("%s[%s] %s", cursor, mark, column.title))
	}
	lines = append(lines, "", "space toggle   Enter/Esc close")
	return lipgloss.NewStyle().Width(38).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(colourBlue).Background(lipgloss.Color("#0d1d29")).Foreground(colourText).Render(strings.Join(lines, "\n"))
}

func (model Model) sortView() string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(colourBlue).Render("SORT RESULTS"), ""}
	for index, item := range sortFields() {
		cursor := "  "
		if index == model.sortCursor {
			cursor = "› "
		}
		direction := "ascending"
		if model.pendingSort {
			direction = "descending"
		}
		line := fmt.Sprintf("%s%-12s", cursor, item.label)
		if index == model.sortCursor {
			line += lipgloss.NewStyle().Foreground(colourGreen).Render(direction)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "←/→ direction   Enter apply   Esc cancel")
	return lipgloss.NewStyle().Width(46).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(colourBlue).Background(lipgloss.Color("#0d1d29")).Foreground(colourText).Render(strings.Join(lines, "\n"))
}
