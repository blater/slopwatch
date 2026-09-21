package follow

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/lipgloss"
)

func initModel(model Model) tea.Cmd {
	commands := []tea.Cmd{tickAnimationFor(model)}
	if model.fixService != nil {
		commands = append(commands, initialFixJobsCommand(model.fixService))
	}
	if model.initialAnalysis {
		// Establish the mutation barrier before the verifier reads any live
		// input. This still runs after Bubble Tea renders the cached projection.
		commands = append(commands, startWatcher(model.watcher), hideStartupLogo())
	} else {
		commands = append(commands, waitForChange(model.watcher))
	}
	return tea.Batch(commands...)
}

func hideStartupLogo() tea.Cmd {
	return tea.Tick(startupLogoDuration, func(time.Time) tea.Msg { return startupLogoExpired{} })
}

func tickAnimationFor(model Model) tea.Cmd {
	return tea.Tick(cursorHighlightTick(model, time.Now()), func(at time.Time) tea.Msg { return animationTick(at) })
}

func view(model Model) string {
	if model.width <= 0 || model.height <= 0 {
		return ""
	}
	if surface, ok := resizeSurface(model); ok {
		return paintScreen(surface, model.width, model.height)
	}
	base := model.mainViewContent()
	return paintScreen(overlaySurfaceView(model, base), model.width, model.height)
}

// Paint every cell, including blank rows and padding, independently of the
// terminal's default colors. Preserve the colors of nested panels and text.
func paintScreen(content string, width, height int) string {
	return paintSurface(content, width, height, style.TextPrimary, style.SurfaceScreen)
}

func paintSurface(content string, width, height int, foreground, background lipgloss.Color) string {
	lines := strings.Split(content, "\n")
	painted := make([]string, height)
	for row := range painted {
		line := ""
		if row < len(lines) {
			line = lines[row]
		}
		painted[row] = paintSourceLine(line, width, foreground, background)
	}
	return strings.Join(painted, "\n")
}

func resizeSurface(model Model) (string, bool) {
	if frame, ok := model.overlays.Top(); ok && frame.Kind == OverlayShutdown && model.width >= 24 && model.height >= 2 {
		return model.featureOverlayView(resizeView(model.width, model.height), frame), true
	}
	if responsiveTier(model.width, model.height) == ResponsiveResize {
		return resizeView(model.width, model.height), true
	}
	return "", false
}

func overlaySurfaceView(model Model, base string) string {
	base = model.settingsUnderlay(base)
	if frame, ok := model.overlays.Top(); ok && !frame.compatibility {
		return model.featureOverlayView(base, frame)
	}
	if model.initialAnalysis && model.analyzing && !model.startupLogoExpired {
		if model.mainView == MainViewFiles {
			return startupView(model, base)
		}
	}
	if model.detail {
		return model.overlay(base, detailView(model))
	}
	if model.source.view {
		return model.overlay(base, sourceViewView(model))
	}
	return modalView(model, base)
}

func modalView(model Model, base string) string {
	if model.infoOpen {
		return model.overlay(infoUnderlay(model, base), infoView(model))
	}
	if model.help {
		return model.overlayBelowTitle(base, helpView(model))
	}
	if model.columns {
		return model.overlay(base, columnsView(model))
	}
	if model.sortOpen {
		return model.overlay(base, sortView(model))
	}
	if model.weightsOpen {
		return model.overlay(base, weightsView(model))
	}
	if model.appearance {
		return model.overlay(base, appearanceView(model))
	}
	if model.configSettings.open {
		if fullScreenSurface(model.width, model.height) {
			return configSettingsFullScreen(model.configSettings, model.profileCatalog, model.width, model.height)
		}
		return model.overlay(base, configSettingsPopup(model.configSettings, model.profileCatalog, model.width, model.height))
	}
	if model.settings {
		if model.runtime.filesSettings {
			return model.overlay(base, filesSettingsView(model))
		}
		return model.overlay(base, settingsView(model))
	}
	return base
}

func infoUnderlay(model Model, base string) string {
	if model.weightsOpen {
		return model.overlay(base, weightsView(model))
	}
	if model.help {
		return model.overlayBelowTitle(base, helpView(model))
	}
	return base
}

func columnsView(model Model) string {
	body := make([]string, 0, len(columnNames()))
	for index, column := range columnNames() {
		mark := " "
		if model.files.Visible[column.key] {
			mark = "✓"
		}
		body = append(body, style.ToggleOption(fmt.Sprintf("[%s]", mark), column.title, index == model.columnCursor, false, 34))
	}
	return style.Popup(style.Heading("Columns"), scrollModalLines(body, model.columnCursor, model.modalBodyHeight()), "", 38)
}

func sortView(model Model) string {
	const sortOptionWidth = 52
	body := make([]string, 0, len(sortFields()))
	for index, item := range sortFields() {
		active := item.key == model.files.SortKey
		arrow := "▲"
		if model.files.sortDirection(item.key) {
			arrow = "▼"
		}
		label := fmt.Sprintf("%-11s (%s)", item.title, item.shortDescription)
		lineText := arrow + " " + label
		if !model.files.sortOptionEnabled(index) {
			body = append(body, style.DisabledOption(lineText, sortOptionWidth))
			continue
		}
		body = append(body, style.SortOption(arrow, label, active, index == model.files.SortCursor, sortOptionWidth))
	}
	return style.Popup(style.Heading("SORT RESULTS"), body, "", 56)
}
