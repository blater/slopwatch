package follow

import (
	"context"

	"github.com/blater/slopwatch/internal/report"
	tea "github.com/charmbracelet/bubbletea"
)

func requestRescan(model *Model) tea.Cmd {
	if model.runtime.rescanRunning {
		return nil
	}
	model.runtime.rescanPending = true
	if model.analyzing || model.runtime.startupWatcherPending {
		return nil
	}
	return beginRescan(model)
}

func beginRescan(model *Model) tea.Cmd {
	model.runtime.rescanPending, model.runtime.rescanRunning = false, true
	model.runtime.rescanLossGeneration = model.runtime.notificationLoss.generation
	if model.watcher != nil {
		model.runtime.rescanMonitorLossVersion = model.watcher.monitor.LossVersion()
	}
	model.analyzing = true
	emit := beginAnalysisProgress(model, report.FreshnessVerifying, "checking workspace")
	analyzer, watcher := model.analyzer, model.watcher
	disabled := model.options.DisableGitignore
	targets := append([]string(nil), model.options.Targets...)
	return func() tea.Msg {
		if controller, ok := analyzer.(gitignoreController); ok {
			controller.SetDisableGitignore(disabled)
		}
		var result analysisResult
		run := func() error {
			watcher.matcher.Freeze()
			result = startupAnalysisCommandWithProgress(analyzer, targets, emit, watcher.startupMatcher())().(analysisResult)
			return result.err
		}
		if watcher == nil {
			result = startupAnalysisCommandWithProgress(analyzer, targets, emit)().(analysisResult)
			return result
		}
		err := watcher.monitor.Rescan(context.Background(), run, func() error { return watcher.matcher.Reload(disabled) })
		watcher.matcher.Freeze()
		result.full, result.err = true, err
		return result
	}
}
