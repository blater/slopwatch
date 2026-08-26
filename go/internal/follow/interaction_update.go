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
	case fixPreparedMsg:
		model.handleFixPrepared(message)
		return model, nil
	case fixSubmittedMsg:
		model.handleFixSubmitted(message)
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
		model.handleJobReader(message)
		return model, nil
	case shutdownCompleteMsg:
		return model, model.handleShutdownComplete(message)
	case tea.KeyMsg:
		return model.handleKey(message)
	default:
		return model, nil
	}
}

func handleWatcherReady(model *Model, message watcherReady) (tea.Model, tea.Cmd) {
	if message.err != nil {
		model.analyzing = false
		model.initialAnalysis = false
		model.status = message.err.Error()
		model.markFreshness(nil, report.FreshnessStaleError, "workspace verification could not start")
		return model, nil
	}
	model.markFreshness(nil, report.FreshnessVerifying, "validating current workspace")
	return model, tea.Batch(model.waitForChange(), model.analyze(nil, true))
}

func handleWindowSize(model *Model, message tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	model.width, model.height = message.Width, message.Height
	model.ensureVisible()
	model.clampPathOffset()
	model.ensureAgentVisible()
	model.clampAgentHorizontalOffset()
	if model.hasOverlay(OverlayPromptEditor) {
		model.configSettings.prompt.SetWidth(max(1, model.width))
		model.configSettings.prompt.SetHeight(max(1, model.height-2))
	}
	model.clampDetailOffset()
	if model.sourceView {
		model.resizeSourceViewport()
	}
	return model, nil
}

func handleSourceChange(model *Model, message sourceChange) (tea.Model, tea.Cmd) {
	command := model.waitForChange()
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
	model.markFreshness(nil, report.FreshnessRefreshing, "workspace inputs changed")
	if model.analyzing {
		model.pendingFullAnalysis = true
		return model, command
	}
	model.analyzing = true
	return model, tea.Batch(command, model.analyze(nil, true))
}

func handlePartialSourceChange(model *Model, message sourceChange, command tea.Cmd) (tea.Model, tea.Cmd) {
	model.markFreshness(message.Paths, report.FreshnessRefreshing, "source changed")
	queueChangedPaths(model, message.Paths)
	if model.analyzing {
		return model, command
	}
	paths := model.takeQueue()
	model.analyzing = true
	return model, tea.Batch(command, model.analyzeExisting(paths))
}

func queueChangedPaths(model *Model, paths []string) {
	for _, path := range paths {
		model.queued[path] = true
		if state, exists := model.rows[path]; exists {
			state.editedAt = time.Now()
			state.direction = 0
			model.rows[path] = state
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
	model.markFreshness(message.replace, report.FreshnessStaleError, "verification failed: "+message.err.Error())
}

func handleAnalysisSuccess(model *Model, message analysisResult, wasInitial bool) {
	model.status = ""
	model.merge(message)
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
	model.sourceViewport = message.viewport
	model.resizeSourceViewport()
	model.sourceSearchText = message.contents
	model.sourceLoading = false
	if !message.highlight {
		return model, nil
	}
	width, height := model.sourceDimensions()
	return model, highlightSourceCommand(message.generation, message.path, message.contents, width, height, model.theme)
}

func handleSourceHighlighted(model *Model, message sourceHighlighted) (tea.Model, tea.Cmd) {
	if !currentSourceMessage(model, message.generation, message.path) {
		return model, nil
	}
	message.viewport.SetYOffset(model.sourceViewport.YOffset)
	model.sourceViewport = message.viewport
	model.resizeSourceViewport()
	return model, nil
}

func currentSourceMessage(model *Model, generation uint64, path string) bool {
	return model.sourceView && generation == model.sourceLoadGeneration && path == model.sourcePath
}

func continueQueuedAnalysis(model *Model) (tea.Model, tea.Cmd) {
	if model.pendingFullAnalysis {
		model.pendingFullAnalysis = false
		model.queued = map[string]bool{}
		model.analyzing = true
		return model, model.analyze(nil, true)
	}
	if len(model.queued) > 0 {
		paths := model.takeQueue()
		model.analyzing = true
		return model, model.analyzeExisting(paths)
	}
	return model, nil
}
