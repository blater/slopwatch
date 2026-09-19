package follow

import (
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/bubbles/textinput"
)

func New(document report.Document, analyzer Analyzer, options Options) (*Model, error) {
	userPreferences, preferenceTrendWindow, err := loadUserPreferences(options.PreferencesPath)
	if err != nil {
		return nil, err
	}
	if options.TrendWindow <= 0 {
		options.TrendWindow = preferenceTrendWindow
	}
	options.DisableGitignore = !userPreferences.Files.HonorGitignore
	watcher, err := newSourceWatcher(
		options.Workspace, options.Targets, options.IncludeTests, options.FollowSymlinks, options.Languages, options.DisableGitignore,
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
		analyzer: analyzer, watcher: watcher, options: options,
		cursorActivity: now,
		mainView:       MainViewFiles,
		files: FilesState{
			Document: document, BaseDocument: document, Rows: rows, Marked: map[string]bool{},
			SortKey: userPreferences.Table.SortBy, SortReverse: userPreferences.Table.SortDescending,
			Visible: preferenceColumns(userPreferences),
		},
		fixService: options.FixService, fixWorkspace: options.FixWorkspace,
		configStore: options.ConfigStore, configWorkspace: configWorkspace, profileProber: options.ProfileProber, profileCatalog: options.ProfileCatalog,
		repositoryIdentity: repositoryIdentity(options.Workspace),
		weights:            preferenceWeights(userPreferences), weightEnabled: preferenceWeightEnabled(userPreferences),
		weightStep: userPreferences.Scoring.WeightStep, maximumWeight: userPreferences.Scoring.MaximumWeight,
		preferencesPath: options.PreferencesPath, preferences: userPreferences,
		queued: map[string]bool{},
		source: sourceState{findInput: findInput}, agents: AgentsState{ShowAll: true, Expanded: map[fix.JobID]bool{}, FindInput: agentFindInput},
		theme: style.Theme(userPreferences.Appearance.Theme),
	}
	ConfigureTheme(model.theme)
	if controller, ok := analyzer.(gitignoreController); ok {
		controller.SetDisableGitignore(options.DisableGitignore)
	}
	model.pruneIgnoredRows()
	if controller, ok := analyzer.(typeScriptTypesController); ok {
		controller.SetTypeScriptTypes(typeScriptTypesWanted(*model))
	}
	rebuildWeightedDocument(model)
	if len(model.files.Document.Files) > 0 {
		model.files.Selected = model.files.Document.Files[0].Path
	}
	model.fixUpdates = newFixSubscriptionState(model.fixService)
	return model, nil
}

func (model *Model) Close() {
	if model.watchReconfigureCancel != nil {
		model.watchReconfigureCancel()
	}
	model.watcher.close()
	model.fixUpdates.close()
}

// StartInitialAnalysis makes the first scan run after Bubble Tea has entered
// the alternate screen, allowing the empty dashboard to render immediately.

func (model *Model) StartInitialAnalysis() {
	model.analyzing = true
	model.initialAnalysis = true
	model.startupWatcherPending = true
	model.startupLogoExpired = false
}
