package follow

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/report"
)

func handleMessage(model *Model, message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case configProbeMsg:
		return model, model.handleConfigProbe(message)
	case configResolvedMsg:
		return model, model.handleConfigResolved(message)
	case configSavedMsg:
		return model, model.handleConfigSaved(message)
	case watcherReady:
		return handleWatcherReady(model, message)
	case tea.WindowSizeMsg:
		return handleWindowSize(model, message)
	case sourceChange:
		return handleSourceChange(model, message)
	case analysisResult:
		return handleAnalysisResult(model, message)
	case animationTick:
		model.animationFrame++
		return model, tickAnimation(model.analyzing)
	case startupLogoExpired:
		model.startupLogoExpired = true
		return model, nil
	case sourceLoaded:
		return handleSourceLoaded(model, message)
	case sourceHighlighted:
		return handleSourceHighlighted(model, message)
	case fixLoadedMsg:
		model.handleFixLoaded(message)
		return model, nil
	case fixStartedMsg:
		model.handleFixStarted(message)
		return model, nil
	case fixTargetPreferenceSavedMsg:
		return model, model.handleFixTargetPreferenceSaved(message)
	case fixJobsMsg:
		return model, model.handleFixJobs(message)
	case fixCommandMsg:
		model.handleFixCommand(message)
		return model, nil
	case fixRetrySubscriptionMsg:
		return model, model.retryFixSubscription(message)
	case jobMonitorMsg:
		return model, model.handleJobMonitor(message)
	case jobReaderMsg:
		return model, model.handleJobReader(message)
	case shutdownCompleteMsg:
		return model, model.shutdown.complete(message)
	case tea.KeyMsg:
		return handleKey(model, message)
	default:
		return model, nil
	}
}

func handleWatcherReady(model *Model, message watcherReady) (tea.Model, tea.Cmd) {
	if message.err != nil {
		model.analyzing = false
		model.initialAnalysis = false
		model.status = message.err.Error()
		markFreshness(model, nil, report.FreshnessStaleError, "workspace verification could not start")
		return model, nil
	}
	markFreshness(model, nil, report.FreshnessVerifying, "validating current workspace")
	return model, tea.Batch(waitForChange(model.watcher), model.analyze(nil, true))
}

func handleWindowSize(model *Model, message tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	model.width, model.height = message.Width, message.Height
	model.ensureVisible()
	model.clampPathOffset()
	model.agents.ensureVisible(makeAgentLayout(model.width, model.height, model.bodyHeight()))
	policy := model.agentMetricPolicy()
	model.agents.clampHorizontal(maximumAgentHorizontalOffset(model.agents.rows(), responsiveTier(model.width, model.height), model.width, policy.visible))
	if overlayPresent(model.overlays, OverlayPromptEditor) {
		resizeMasterPromptTextBox(&model.configSettings, model.width, model.height)
	}
	model.clampDetailOffset()
	if model.source.view {
		width, height := sourceDimensions(model.width, model.height)
		model.source.resize(width, height)
	}
	return model, nil
}

func handleSourceChange(model *Model, message sourceChange) (tea.Model, tea.Cmd) {
	command := waitForChange(model.watcher)
	if message.Err != nil {
		model.status = message.Err.Error()
		return model, command
	}
	if message.Full {
		return handleFullSourceChange(model, message, command)
	}
	return handlePartialSourceChange(model, message, command)
}

func handleFullSourceChange(model *Model, message sourceChange, command tea.Cmd) (tea.Model, tea.Cmd) {
	markFreshness(model, nil, report.FreshnessRefreshing, "workspace inputs changed")
	if model.analyzing {
		model.pendingFullAnalysis = true
		return model, command
	}
	model.analyzing = true
	return model, tea.Batch(command, model.analyze(nil, true))
}

func handlePartialSourceChange(model *Model, message sourceChange, command tea.Cmd) (tea.Model, tea.Cmd) {
	markFreshness(model, message.Paths, report.FreshnessRefreshing, "source changed")
	queueChangedPaths(model, message.Paths)
	if model.analyzing {
		return model, command
	}
	paths := takeQueue(model)
	model.analyzing = true
	return model, tea.Batch(command, analyzeExisting(*model, paths))
}

func queueChangedPaths(model *Model, paths []string) {
	for _, path := range paths {
		model.queued[path] = true
		if state, exists := model.files.Rows[path]; exists {
			state.editedAt = time.Now()
			state.direction = 0
			model.files.Rows[path] = state
		}
	}
}

func handleAnalysisResult(model *Model, message analysisResult) (tea.Model, tea.Cmd) {
	wasInitial := model.initialAnalysis
	model.analyzing = false
	if message.full {
		model.initialAnalysis = false
	}
	if message.err != nil {
		handleAnalysisError(model, message)
	} else {
		handleAnalysisSuccess(model, message, wasInitial)
	}
	return continueQueuedAnalysis(model)
}

func handleAnalysisError(model *Model, message analysisResult) {
	model.status = message.err.Error()
	markFreshness(model, message.replace, report.FreshnessStaleError, "verification failed: "+message.err.Error())
}

func handleAnalysisSuccess(model *Model, message analysisResult, wasInitial bool) {
	model.status = ""
	merge(model, message)
	model.clampPathOffset()
	if wasInitial {
		if controller, ok := model.analyzer.(cacheReadController); ok {
			controller.SetCacheReads(true)
		}
	}
}

func handleSourceLoaded(model *Model, message sourceLoaded) (tea.Model, tea.Cmd) {
	if !currentSourceMessage(model, message.generation, message.path) {
		return model, nil
	}
	model.source.viewport = message.viewport
	width, height := sourceDimensions(model.width, model.height)
	model.source.resize(width, height)
	model.source.searchText = message.contents
	model.source.loading = false
	if !message.highlight {
		return model, nil
	}
	width, height = sourceDimensions(model.width, model.height)
	return model, highlightSourceCommand(message.generation, message.path, message.contents, width, height, model.theme)
}

func handleSourceHighlighted(model *Model, message sourceHighlighted) (tea.Model, tea.Cmd) {
	if !currentSourceMessage(model, message.generation, message.path) {
		return model, nil
	}
	message.viewport.SetYOffset(model.source.viewport.YOffset)
	model.source.viewport = message.viewport
	width, height := sourceDimensions(model.width, model.height)
	model.source.resize(width, height)
	return model, nil
}

func currentSourceMessage(model *Model, generation uint64, path string) bool {
	return model.source.view && generation == model.source.loadGeneration && path == model.source.path
}

func continueQueuedAnalysis(model *Model) (tea.Model, tea.Cmd) {
	if model.pendingFullAnalysis {
		model.pendingFullAnalysis = false
		model.queued = map[string]bool{}
		model.analyzing = true
		return model, model.analyze(nil, true)
	}
	if len(model.queued) > 0 {
		paths := takeQueue(model)
		model.analyzing = true
		return model, analyzeExisting(*model, paths)
	}
	return model, nil
}
