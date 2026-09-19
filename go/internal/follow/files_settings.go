package follow

import (
	"context"
	"path/filepath"
	"time"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceignore"
	tea "github.com/charmbracelet/bubbletea"
)

func (model *Model) toggleGitignore() tea.Cmd {
	model.runtime.watchRetryCount = 0
	model.options.DisableGitignore = !model.options.DisableGitignore
	persistUserPreferences(model)
	return model.refreshIgnorePolicy()
}

type watcherReconfigureRetry struct{ generation uint64 }

type watcherReconfigured struct {
	watcher     *sourceWatcher
	generation  uint64
	err         error
	stopCleanup func() bool
}

func (model *Model) refreshIgnorePolicy() tea.Cmd {
	if model.runtime.watchReconfigureCancel != nil {
		model.runtime.watchReconfigureCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	model.runtime.watchReconfigureCancel = cancel
	model.runtime.watchGeneration++
	generation, options := model.runtime.watchGeneration, model.options
	model.runtime.watchReconfigurePending = true
	model.runtime.pendingFullAnalysis = true
	model.runtime.discardAnalysis = model.analyzing && !model.runtime.analysisRetryPending && !model.runtime.startupWatcherPending
	if model.runtime.analysisRetryPending {
		model.runtime.analysisRetryPending = false
		model.analyzing = false
	}
	if model.runtime.startupWatcherPending {
		model.analyzing = false
	}
	if controller, ok := model.analyzer.(gitignoreController); ok {
		controller.SetDisableGitignore(options.DisableGitignore)
	}
	model.pruneIgnoredRows()
	return func() tea.Msg {
		watcher, err := newSourceWatcher(options.Workspace, options.Targets, options.IncludeTests, options.FollowSymlinks, options.Languages, options.DisableGitignore)
		if err == nil {
			err = watcher.start()
		}
		var stopCleanup func() bool
		if watcher != nil {
			stopCleanup = context.AfterFunc(ctx, watcher.close)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil && watcher != nil {
			watcher.close()
			watcher = nil
		}
		return watcherReconfigured{watcher: watcher, generation: generation, err: err, stopCleanup: stopCleanup}
	}
}

func (model *Model) handleWatcherReconfigured(message watcherReconfigured) tea.Cmd {
	if message.stopCleanup != nil {
		message.stopCleanup()
	}
	if message.generation != model.runtime.watchGeneration {
		if message.watcher != nil {
			message.watcher.close()
		}
		return nil
	}
	model.runtime.watchReconfigurePending = false
	if message.err != nil {
		commands := []tea.Cmd{}
		if model.runtime.watchNeedsWait {
			commands = append(commands, model.resumeWatcherWait())
		}
		if model.runtime.watchRetryCount < 3 {
			model.runtime.watchRetryCount++
			generation := model.runtime.watchGeneration
			commands = append(commands, tea.Tick(time.Duration(model.runtime.watchRetryCount)*250*time.Millisecond, func(time.Time) tea.Msg { return watcherReconfigureRetry{generation: generation} }))
		} else {
			showRuntimeError(model, message.err)
		}

		if !model.analyzing && !model.runtime.startupWatcherPending {
			_, command := continueQueuedAnalysis(model)
			commands = append(commands, command)
		}
		return tea.Batch(commands...)
	}
	model.runtime.watchRetryCount = 0
	model.runtime.startupWatcherPending = false
	previous := model.watcher
	model.watcher = message.watcher
	if previous != nil {
		previous.close()
	}
	commands := []tea.Cmd{model.resumeWatcherWait()}
	if !model.analyzing {
		_, command := continueQueuedAnalysis(model)
		commands = append(commands, command)
	}
	return tea.Batch(commands...)
}

func (model *Model) pruneIgnoredRows() {
	matcher := sourceignore.New(model.options.Workspace, model.options.DisableGitignore)
	keep := make([]report.File, 0, len(model.files.BaseDocument.Files))
	for _, file := range model.files.BaseDocument.Files {
		if !matcher.Ignored(filepath.Join(model.options.Workspace, file.Path), false) {
			keep = append(keep, file)
		} else {
			delete(model.files.Rows, file.Path)
			delete(model.files.Marked, file.Path)
		}
	}
	model.files.BaseDocument.Files = keep
	// Prevent the empty-base fallback from restoring the previous projection.
	model.files.Document.Files = append([]report.File(nil), keep...)
	rebuildWeightedDocument(model)
	restoreSelection(model)
}

func (model *Model) resumeWatcherWait() tea.Cmd {
	model.runtime.watchNeedsWait = false
	return waitForChange(model.watcher)
}
