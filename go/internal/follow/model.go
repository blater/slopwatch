package follow

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"

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
	analyzer            Analyzer
	watcher             *sourceWatcher
	options             Options
	mainView            MainView
	files               FilesState
	agents              AgentsState
	overlays            OverlayStack
	fixService          FixService
	fixWorkspace        fix.WorkspaceIdentity
	fixSubscription     fixapp.Subscription
	fixGeneration       uint64
	fixDialog           fixDialogState
	jobCommand          jobCommandState
	cancelConfirmation  cancelConfirmation
	jobMonitor          jobMonitorState
	jobReader           jobReaderState
	shutdown            shutdownState
	fixNotice           string
	fixUpdatesStale     bool
	fixRetryGeneration  uint64
	fixTargetDesired    float64
	fixTargetSaving     bool
	configStore         ConfigStore
	configWorkspace     fix.WorkspaceIdentity
	profileProber       ProfileProber
	profileCatalog      agent.ProfileCatalog
	configSettings      configSettingsState
	repositoryIdentity  string
	width               int
	height              int
	analyzing           bool
	queued              map[string]bool
	status              string
	initialAnalysis     bool
	startupLogoExpired  bool
	animationFrame      int
	detail              bool
	detailOffset        int
	help                bool
	helpCursor          int
	helpTopic           string
	infoOpen            bool
	infoKey             string
	columns             bool
	columnCursor        int
	sortOpen            bool
	settings            bool
	settingsCursor      int
	appearance          bool
	appearanceCursor    int
	theme               style.Theme
	weightsOpen         bool
	weightCursor        int
	weightsResetConfirm bool
	weights             map[string]float64
	weightEnabled       map[string]bool
	weightStep          float64
	maximumWeight       float64
	preferencesPath     string
	preferences         preferences.Document
	columnsFromSettings bool
	pendingFullAnalysis bool
	source              sourceState
}
