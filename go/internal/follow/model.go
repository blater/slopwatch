package follow

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

type Options struct {
	DisableGitignore     bool
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

// changeAnalyzer can reuse the analyzer's dependency graph for a source
// change. It returns every file whose result may have changed, including
// deleted paths, so the dashboard can replace exactly that affected set.
type changeAnalyzer interface {
	AnalyzeChanges(context.Context, []string) (report.Document, []string, error)
}

type gitignoreController interface{ SetDisableGitignore(bool) }

type typeScriptTypesController interface {
	SetTypeScriptTypes(bool)
}

type cacheReadController interface {
	SetCacheReads(bool)
}

type analysisResult struct {
	document report.Document
	replace  []string
	// paths are the original requested inputs. They remain available for a
	// retry when an incremental analysis fails before it can report rows.
	paths []string
	full  bool
	err   error
}

type analysisRetry struct{}

type watcherReady struct {
	err     error
	watcher *sourceWatcher
}

type animationTick time.Time
type startupLogoExpired struct{}

type sourceLoaded struct {
	err        error
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

// sourceState contains source preview and find interaction state. Keeping the
// viewport and its search cursor together prevents the root model from
// owning details of the source reader lifecycle.
type sourceState struct {
	view           bool
	path           string
	viewport       viewport.Model
	loadGeneration uint64
	loading        bool
	lastKey        string
	lastAt         time.Time
	rapid          int
	searchText     string
	findInput      textinput.Model
	findOpen       bool
	findQuery      string
	findSource     bool
}

type Model struct {
	analyzer                Analyzer
	watcher                 *sourceWatcher
	watchGeneration         uint64
	watchReconfigurePending bool
	watchReconfigureCancel  context.CancelFunc
	watchNeedsWait          bool
	watchRetryCount         int
	startupWatcherPending   bool
	options                 Options
	mainView                MainView
	files                   FilesState
	agents                  AgentsState
	overlays                OverlayStack
	fixService              FixService
	fixWorkspace            fix.WorkspaceIdentity
	fixUpdates              fixSubscriptionState
	fixGeneration           uint64
	fixDialog               fixDialogState
	jobActions              jobActionState
	jobMonitor              jobMonitorState
	jobReader               jobReaderState
	shutdown                shutdownState
	fixNotice               string
	targetScorePreference   targetScorePreferenceState
	configStore             ConfigStore
	configWorkspace         fix.WorkspaceIdentity
	profileProber           ProfileProber
	profileCatalog          agent.ProfileCatalog
	configSettings          configSettingsState
	repositoryIdentity      string
	width                   int
	height                  int
	analyzing               bool
	queued                  map[string]bool
	status                  string
	runtimeError            string
	runtimeErrorMessages    []string
	fixErrorSummary         string
	runtimeErrorOffset      int
	initialAnalysis         bool
	startupLogoExpired      bool
	animationFrame          int
	cursorActivity          time.Time
	detail                  bool
	detailOffset            int
	help                    bool
	helpCursor              int
	helpTopic               string
	infoOpen                bool
	infoKey                 string
	columns                 bool
	columnCursor            int
	sortOpen                bool
	settings                bool
	settingsCursor          int
	settingsGroup           string
	settingsRootCursor      int
	filesSettings           bool
	configParent            *configSettingsState
	appearance              bool
	appearanceCursor        int
	theme                   style.Theme
	weightsOpen             bool
	weightCursor            int
	weightsResetConfirm     bool
	weights                 map[string]float64
	weightEnabled           map[string]bool
	weightStep              float64
	maximumWeight           float64
	preferencesPath         string
	preferences             preferences.Document
	columnsFromSettings     bool
	pendingFullAnalysis     bool
	discardAnalysis         bool
	analysisRetryPending    bool
	source                  sourceState
}
