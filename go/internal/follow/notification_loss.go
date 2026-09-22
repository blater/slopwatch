package follow

import (
	"fmt"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/style"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const notificationLossExplanation = "Filesystem changes arrived faster than notifications could be processed, or the filesystem reported incomplete change details. Some displayed results may be outdated. Monitoring is continuing. Close this message and press r to repeat the startup checks and refresh the workspace."

type notificationLossState struct {
	count      uint64
	generation uint64
	latest     time.Time
	deadline   time.Time
	visible    bool
	offset     int
}

func recordNotificationLoss(model *Model, count uint64, now time.Time) {
	state := &model.runtime.notificationLoss
	state.count += count
	state.generation += count
	state.latest, state.deadline, state.visible = now, now.Add(time.Minute), true
}

func expireNotificationLoss(model *Model, now time.Time) {
	state := &model.runtime.notificationLoss
	if state.visible && !now.Before(state.deadline) {
		state.visible = false
	}
}

func notificationFooter(model Model, normal string) string {
	if !model.runtime.notificationLoss.visible {
		return normal
	}
	width := model.width
	label := "! Some file changes may be missed"
	if width < 64 {
		label = "! Changes may be missed"
	}
	if width < 40 {
		label = "! warning"
	}
	foreground, background := lipgloss.Color("#ffff00"), lipgloss.Color("#600000")
	if model.theme == style.ThemeLight {
		foreground, background = lipgloss.Color("#000000"), lipgloss.Color("#ffcccc")
	}
	notice := lipgloss.NewStyle().Foreground(foreground).Background(background).Render(label)
	leftWidth := max(0, width-lipgloss.Width(notice)-1)
	left := truncateANSI(normal, leftWidth)
	gap := max(0, width-lipgloss.Width(left)-lipgloss.Width(notice))
	return left + lipgloss.NewStyle().Background(style.SurfaceFooter).Render(strings.Repeat(" ", gap)) + notice
}

func openNotificationLoss(model *Model) {
	if model.runtime.notificationLoss.count == 0 {
		return
	}
	model.overlays.Push(OverlayNotificationLoss, OverlayCaller{MainView: model.mainView, Selected: model.mainSelection()})
}

func notificationLossModel(model Model) Model {
	state := model.runtime.notificationLoss
	model.runtimeError = fmt.Sprintf("%s\n\nOccurrences: %d\nLatest: %s", notificationLossExplanation, state.count, state.latest.Format(time.RFC3339))
	model.runtime.runtimeErrorOffset = state.offset
	return model
}

func notificationLossPopup(model Model) string {
	return diagnosticPopup(notificationLossModel(model), "WARNING")
}

func handleNotificationLossKey(model *Model, name string) (tea.Model, tea.Cmd) {
	if name == "esc" || name == "enter" {
		model.overlays.Pop()
		return model, nil
	}
	if name == "ctrl+c" {
		return model.requestQuit()
	}
	proxy := notificationLossModel(*model)
	_, command := handleRuntimeErrorKey(&proxy, name)
	model.runtime.notificationLoss.offset = proxy.runtime.runtimeErrorOffset
	return model, command
}
