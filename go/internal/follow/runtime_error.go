package follow

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/style"
)

// showRuntimeError keeps the diagnostic in the model and puts it above any
// normal screen overlay. A shutdown confirmation always remains the topmost
// interaction, so an error received during shutdown is shown afterward.
func showRuntimeError(model *Model, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	for _, previous := range model.runtime.runtimeErrorMessages {
		if previous == message {
			showStoredRuntimeError(model)
			return
		}
	}
	if model.runtimeError == "" {
		model.runtime.runtimeErrorMessages = nil
		model.runtime.runtimeErrorOffset = 0
	}
	model.runtime.runtimeErrorMessages = append(model.runtime.runtimeErrorMessages, message)
	model.runtimeError = strings.Join(model.runtime.runtimeErrorMessages, "\n\n")
	showStoredRuntimeError(model)
}

func showStoredRuntimeError(model *Model) {
	if model.runtimeError == "" {
		return
	}
	if overlay, ok := model.overlays.Top(); ok && overlay.Kind == OverlayShutdown {
		return
	}
	if overlayPresent(model.overlays, OverlayRuntimeError) {
		return
	}
	model.reconcileLegacyOverlayStack()
	if overlay, ok := model.overlays.Top(); ok && overlay.Kind == OverlayShutdown {
		return
	}
	model.overlays.Push(OverlayRuntimeError, OverlayCaller{MainView: model.mainView, Selected: model.mainSelection()})
}

func dismissRuntimeError(model *Model) {
	if model.status == model.runtimeError {
		model.status = ""
	}
	model.runtimeError = ""
	model.runtime.runtimeErrorMessages = nil
	model.runtime.runtimeErrorOffset = 0
	if overlay, ok := model.overlays.Top(); ok && overlay.Kind == OverlayRuntimeError {
		model.overlays.Pop()
	}
}

func handleRuntimeErrorKey(model *Model, name string) (tea.Model, tea.Cmd) {
	if name == "ctrl+c" {
		return model.requestQuit()
	}
	page := runtimeErrorBodyHeight(model.height)
	limit := max(0, len(runtimeErrorLines(model.runtimeError, runtimeErrorWidth(model.width)-4))-page)
	model.runtime.runtimeErrorOffset = min(limit, max(0, model.runtime.runtimeErrorOffset))
	switch name {
	case "esc", "enter":
		dismissRuntimeError(model)
	case "up", "k":
		model.runtime.runtimeErrorOffset--
	case "down", "j":
		model.runtime.runtimeErrorOffset++
	case "pgup", "ctrl+u":
		model.runtime.runtimeErrorOffset -= page
	case "pgdown", "ctrl+d", " ":
		model.runtime.runtimeErrorOffset += page
	case "home":
		model.runtime.runtimeErrorOffset = 0
	case "end":
		model.runtime.runtimeErrorOffset = limit
	}
	model.runtime.runtimeErrorOffset = min(limit, max(0, model.runtime.runtimeErrorOffset))
	return model, nil
}

func runtimeErrorWidth(width int) int { return min(80, max(6, width-4)) }

func runtimeErrorPopup(model Model) string {
	outerWidth := runtimeErrorWidth(model.width)
	popupWidth := outerWidth - 2
	bodyWidth := max(1, popupWidth-2)
	lines := runtimeErrorLines(model.runtimeError, bodyWidth)
	bodyHeight := min(runtimeErrorBodyHeight(model.height), max(1, len(lines)))
	start := min(max(0, model.runtime.runtimeErrorOffset), max(0, len(lines)-bodyHeight))
	end := min(len(lines), start+bodyHeight)
	visible := lines[start:end]
	for len(visible) < bodyHeight {
		visible = append(visible, "")
	}
	footer := "Enter/Esc dismiss"
	if len(lines) > bodyHeight {
		footer = "↑↓ scroll · Enter/Esc close"
	}
	content := make([]string, 0, len(visible))
	for _, line := range visible {
		content = append(content, fixSurfaceLine(line, bodyWidth, style.SurfaceModal, style.TextPrimary))
	}
	rows := []string{"ERROR", ""}
	rows = append(rows, content...)
	if model.height < 7 {
		// Popup's blank separator rows need seven terminal rows at minimum. The
		// tight form fits the supported 36x6 surface while retaining the full
		// message and footer controls.
		rows = []string{"ERROR"}
		rows = append(rows, content...)
		rows = append(rows, footer)
	} else {
		rows = append(rows, "", footer)
	}
	return lipgloss.NewStyle().Width(popupWidth).Padding(0, 1).
		Border(lipgloss.RoundedBorder()).BorderForeground(style.AccentCritical).
		BorderBackground(style.SurfaceModal).Background(style.SurfaceModal).
		Foreground(style.TextPrimary).Render(strings.Join(rows, "\n"))
}

func runtimeErrorBodyHeight(height int) int {
	// Spacious popup rows are title + two separators + footer + two-cell border.
	// At six rows the tight form is used (title + body + footer + border).
	if height < 7 {
		return max(1, height-4)
	}
	return max(1, min(18, height-6))
}

func runtimeErrorLines(message string, width int) []string {
	message = cleanEditorText(message)
	message = strings.ReplaceAll(message, "\t", "    ")
	logical := strings.Split(message, "\n")
	lines := make([]string, 0, len(logical))
	for _, line := range logical {
		wrapped := ansi.Hardwrap(line, max(1, width), false)
		if wrapped == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, strings.Split(wrapped, "\n")...)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
