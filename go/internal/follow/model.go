package follow

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type Options struct {
	Workspace      string
	Targets        []string
	Languages      []string
	IncludeTests   bool
	FollowSymlinks bool
	Limit          int
	TrendWindow    time.Duration
	Compact        bool
}

type Analyzer interface {
	Analyze(context.Context, []string, []string) (report.Document, error)
}

type typeScriptTypesController interface {
	SetTypeScriptTypes(bool)
}

type cacheReadController interface {
	SetCacheReads(bool)
}

type analysisResult struct {
	document report.Document
	replace  []string
	full     bool
	err      error
}

type watcherReady struct{ err error }

type animationTick time.Time
type startupLogoExpired struct{}

type sourceLoaded struct {
	generation uint64
	path       string
	contents   string
	viewport   viewport.Model
	highlight  bool
}

type sourceHighlighted struct {
	generation uint64
	path       string
	viewport   viewport.Model
}

const startupLogoDuration = 2 * time.Second

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
	analyzer             Analyzer
	watcher              *sourceWatcher
	options              Options
	repositoryIdentity   string
	document             report.Document
	displayFilesCache    []report.File
	displayFilesReady    bool
	longestDisplayPath   int
	freshnessStatusText  string
	freshnessStatusReady bool
	rows                 map[string]rowState
	width                int
	height               int
	cursor               int
	offset               int
	pathOffset           int
	selected             string
	analyzing            bool
	queued               map[string]bool
	status               string
	initialAnalysis      bool
	startupLogoExpired   bool
	animationFrame       int
	detail               bool
	detailOffset         int
	help                 bool
	helpCursor           int
	helpTopic            string
	infoOpen             bool
	infoKey              string
	columns              bool
	columnCursor         int
	sortOpen             bool
	sortCursor           int
	sortKey              string
	sortReverse          bool
	sortDirections       map[string]bool
	settings             bool
	settingsCursor       int
	appearance           bool
	appearanceCursor     int
	theme                style.Theme
	weightsOpen          bool
	weightCursor         int
	weightsResetConfirm  bool
	weights              map[string]float64
	weightEnabled        map[string]bool
	baseDocument         report.Document
	columnsFromSettings  bool
	pendingFullAnalysis  bool
	sourceView           bool
	sourcePath           string
	sourceViewport       viewport.Model
	sourceLoadGeneration uint64
	sourceLoading        bool
	sourceLastKey        string
	sourceLastAt         time.Time
	sourceRapid          int
	sourceSearchText     string
	findInput            textinput.Model
	findOpen             bool
	findQuery            string
	findSource           bool
	visible              map[string]bool
}

func New(document report.Document, analyzer Analyzer, options Options) (*Model, error) {
	watcher, err := newSourceWatcher(
		options.Workspace, options.Targets, options.IncludeTests, options.FollowSymlinks, options.Languages,
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
		repositoryIdentity: repositoryIdentity(options.Workspace),
		baseDocument:       document, weights: defaultWeights(), weightEnabled: defaultWeightEnabled(),
		rows: rows, queued: map[string]bool{},
		findInput: findInput,
		sortKey:   "score", sortReverse: true,
		theme:   style.ThemeDark,
		visible: defaultColumnVisibility(),
	}
	model.rebuildWeightedDocument()
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
	model.startupLogoExpired = false
}

func initModel(model Model) tea.Cmd {
	commands := []tea.Cmd{tickAnimation(model.analyzing)}
	if model.initialAnalysis {
		// Establish the mutation barrier before the verifier reads any live
		// input. This still runs after Bubble Tea renders the cached projection.
		commands = append(commands, model.startWatcher(), hideStartupLogo())
	} else {
		commands = append(commands, model.waitForChange())
	}
	return tea.Batch(commands...)
}

func hideStartupLogo() tea.Cmd {
	return tea.Tick(startupLogoDuration, func(time.Time) tea.Msg { return startupLogoExpired{} })
}

func tickAnimation(analyzing bool) tea.Cmd {
	interval := time.Second
	if analyzing {
		interval = 125 * time.Millisecond
	}
	return tea.Tick(interval, func(at time.Time) tea.Msg { return animationTick(at) })
}

func (model Model) waitForChange() tea.Cmd {
	return func() tea.Msg { return model.watcher.wait() }
}

func (model Model) startWatcher() tea.Cmd {
	return func() tea.Msg { return watcherReady{err: model.watcher.start()} }
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

func view(model Model) string {
	if model.width <= 0 || model.height <= 0 {
		return ""
	}
	base := model.tableView()
	if model.initialAnalysis && model.analyzing && !model.startupLogoExpired {
		return model.startupView(base)
	}
	if model.detail {
		return model.overlay(base, model.detailView())
	}
	if model.sourceView {
		return model.overlay(base, model.sourceViewView())
	}
	return modalView(model, base)
}

func modalView(model Model, base string) string {
	if model.infoOpen {
		return model.overlay(infoUnderlay(model, base), model.infoView())
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
	if model.weightsOpen {
		return model.overlay(base, model.weightsView())
	}
	if model.appearance {
		return model.overlay(base, model.appearanceView())
	}
	if model.settings {
		return model.overlay(base, model.settingsView())
	}
	return base
}

func infoUnderlay(model Model, base string) string {
	if model.weightsOpen {
		return model.overlay(base, model.weightsView())
	}
	if model.help {
		return model.overlayBelowTitle(base, model.helpView())
	}
	return base
}

func columnsView(model Model) string {
	body := make([]string, 0, len(columnNames()))
	for index, column := range columnNames() {
		mark := " "
		if model.visible[column.key] {
			mark = "✓"
		}
		body = append(body, style.ModalOption(fmt.Sprintf("[%s] %s", mark, column.title), index == model.columnCursor, 34))
	}
	return style.Popup(style.Heading("Columns"), scrollModalLines(body, model.columnCursor, model.modalBodyHeight()), "", 38)
}

func sortView(model Model) string {
	const sortOptionWidth = 52
	body := make([]string, 0, len(sortFields()))
	for index, item := range sortFields() {
		active := item.key == model.sortKey
		arrow := "▲"
		if model.sortDirection(item.key) {
			arrow = "▼"
		}
		label := fmt.Sprintf("%-11s (%s)", item.title, item.shortDescription)
		lineText := arrow + " " + label
		if !model.sortOptionEnabled(index) {
			body = append(body, style.DisabledOption(lineText, sortOptionWidth))
			continue
		}
		body = append(body, style.SortOption(arrow, label, active, index == model.sortCursor, sortOptionWidth))
	}
	return style.Popup(style.Heading("SORT RESULTS"), body, "", 56)
}
