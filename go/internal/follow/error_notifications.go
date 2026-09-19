package follow

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Background events belong to the screen underneath the error. In particular,
// a completed form must not accidentally pop the error instead of its own frame.
func suspendErrorOverlay(model *Model, message tea.Msg) {
	if _, key := message.(tea.KeyMsg); key {
		return
	}
	if frame, ok := model.overlays.Top(); ok && frame.Kind == OverlayRuntimeError {
		model.overlays.Pop()
	}
}

func (model Model) surfaceErrors() []string {
	return []string{model.fixDialog.errorText, model.fixDialog.scoreError,
		model.jobMonitor.errorText, model.runtime.jobReader.errorText,
		model.runtime.jobActions.confirmation.errorText, model.runtime.shutdown.errorText,
		model.configSettings.connectionError, configErrorStatus(model.configSettings.status)}
}

func configErrorStatus(value string) string {
	for _, prefix := range []string{"Invalid setting:", "Load failed:", "Save failed:", "Cannot save:", "Needs repair:", "Feature settings unavailable:"} {
		if strings.HasPrefix(value, prefix) {
			return value
		}
	}
	return ""
}

func showChangedSurfaceErrors(model *Model, before []string) {
	for i, value := range model.surfaceErrors() {
		if value != "" && value != before[i] {
			showRuntimeError(model, errors.New(value))
		}
	}
}
