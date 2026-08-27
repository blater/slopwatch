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

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

type Options struct {
	Workspace            string
	Targets              []string
	Languages            []string
	IncludeTests         bool
	FollowSymlinks       bool
	Limit                int
	TrendWindow          time.Duration
	Compact              bool
	TypeScriptTypes      bool
	PreferencesPath      string
	FixService           FixService
	FixWorkspace         fix.WorkspaceIdentity
	FixUnavailableReason string
	ConfigStore          ConfigStore
	ConfigWorkspace      fix.WorkspaceIdentity
	ProfileProber        ProfileProber
	ProfileCatalog       agent.ProfileCatalog
}

type ConfigStore interface {
	appconfig.Resolver
	appconfig.Store
}

type ProfileProber interface {
	Probe(context.Context, agent.Profile) agent.ProbeResult
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
	mainView             MainView
	files                FilesState
	agents               AgentsState
	overlays             OverlayStack
	fixService           FixService
	fixWorkspace         fix.WorkspaceIdentity
	fixSubscription      fixapp.Subscription
	fixGeneration        uint64
	fixDialog            fixDialogState
	jobCommand           jobCommandState
	cancelConfirmation   cancelConfirmation
	jobMonitor           jobMonitorState
	jobReader            jobReaderState
	shutdown             shutdownState
	fixNotice            string
	fixUpdatesStale      bool
	fixRetryGeneration   uint64
	fixTargetDesired     float64
	fixTargetSaving      bool
	configStore          ConfigStore
	configWorkspace      fix.WorkspaceIdentity
	profileProber        ProfileProber
	profileCatalog       agent.ProfileCatalog
	configSettings       configSettingsState
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
	marking              bool
	shiftMarking         bool
	marked               map[string]bool
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
	weightStep           float64
	maximumWeight        float64
	preferencesPath      string
	preferences          preferences.Document
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
	agentFindInput       textinput.Model
	visible              map[string]bool
}

func New(document report.Document, analyzer Analyzer, options Options) (*Model, error) {
	userPreferences, preferenceTrendWindow, err := loadUserPreferences(options.PreferencesPath)
	if err != nil {
		return nil, err
	}
	if options.TrendWindow <= 0 {
		options.TrendWindow = preferenceTrendWindow
	}
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
	findInput := textinput.New()
	findInput.Prompt = "/ "
	findInput.Placeholder = "find"
	agentFindInput := textinput.New()
	agentFindInput.Prompt = "/ "
	agentFindInput.Placeholder = "find jobs"
	configWorkspace := options.ConfigWorkspace
	if configWorkspace.RepositoryRoot == "" {
		configWorkspace = options.FixWorkspace
	}
	model := &Model{
		analyzer: analyzer, watcher: watcher, options: options, document: document,
		mainView: MainViewFiles, agents: AgentsState{ShowAll: true, Expanded: map[fix.JobID]bool{}},
		marked:     map[string]bool{},
		fixService: options.FixService, fixWorkspace: options.FixWorkspace,
		configStore: options.ConfigStore, configWorkspace: configWorkspace, profileProber: options.ProfileProber, profileCatalog: options.ProfileCatalog,
		repositoryIdentity: repositoryIdentity(options.Workspace),
		baseDocument:       document,
		weights:            preferenceWeights(userPreferences), weightEnabled: preferenceWeightEnabled(userPreferences),
		weightStep: userPreferences.Scoring.WeightStep, maximumWeight: userPreferences.Scoring.MaximumWeight,
		preferencesPath: options.PreferencesPath, preferences: userPreferences,
		rows: rows, queued: map[string]bool{},
		findInput: findInput, agentFindInput: agentFindInput,
		sortKey: userPreferences.Table.SortBy, sortReverse: userPreferences.Table.SortDescending,
		theme: style.Theme(userPreferences.Appearance.Theme), visible: preferenceColumns(userPreferences),
	}
	ConfigureTheme(model.theme)
	if controller, ok := analyzer.(typeScriptTypesController); ok {
		controller.SetTypeScriptTypes(model.typeScriptTypesWanted())
	}
	model.rebuildWeightedDocument()
	if len(document.Files) > 0 {
		model.selected = document.Files[0].Path
	}
	model.captureFilesState()
	if model.fixService != nil {
		model.fixSubscription = model.fixService.Subscribe()
	}
	return model, nil
}

func (model *Model) Close() {
	model.watcher.close()
	if model.fixSubscription != nil {
		_ = model.fixSubscription.Close()
	}
}

// StartInitialAnalysis makes the first scan run after Bubble Tea has entered
// the alternate screen, allowing the empty dashboard to render immediately.
func (model *Model) StartInitialAnalysis() {
	model.analyzing = true
	model.initialAnalysis = true
	model.startupLogoExpired = false
}

func initModel(model Model) tea.Cmd {
	commands := []tea.Cmd{tickAnimation(model.analyzing)}
	if model.fixService != nil {
		commands = append(commands, initialFixJobsCommand(model.fixService))
	}
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

func (model Model) bodyHeight() int {
	reserved := 3
	if model.mainView == MainViewFiles {
		reserved = 4
	}
	return max(1, model.height-reserved)
}

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
	if frame, ok := model.overlays.Top(); ok && frame.Kind == OverlayShutdown && model.width >= 24 && model.height >= 2 {
		return model.featureOverlayView(resizeView(model.width, model.height), frame)
	}
	if responsiveTier(model.width, model.height) == ResponsiveResize {
		return resizeView(model.width, model.height)
	}
	base := model.mainViewContent()
	if frame, ok := model.overlays.Top(); ok && !frame.compatibility {
		return model.featureOverlayView(base, frame)
	}
	if model.initialAnalysis && model.analyzing && !model.startupLogoExpired {
		if model.mainView == MainViewFiles {
			return model.startupView(base)
		}
	}
	if model.detail {
		return model.overlay(base, model.detailView())
	}
	if model.sourceView {
		return model.overlay(base, model.sourceViewView())
	}
	return modalView(model, base)
}

func (model Model) mainViewContent() string {
	if model.mainView == MainViewAgents {
		return agentsTableView(model)
	}
	return model.tableView()
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
	if model.configSettings.open {
		if fullScreenSurface(model.width, model.height) {
			return model.configSettingsFullScreen()
		}
		return model.overlay(base, model.configSettingsView())
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
		body = append(body, style.ToggleOption(fmt.Sprintf("[%s]", mark), column.title, index == model.columnCursor, false, 34))
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
