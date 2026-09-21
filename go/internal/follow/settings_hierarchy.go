package follow

import (
	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

func (model Model) settingsUnderlay(base string) string {
	if model.settingsGroup != "" {
		parent := model
		parent.settingsGroup = ""
		parent.settingsCursor = model.settingsRootCursor
		base = model.settingsParentLayer(base, settingsView(parent), 2)
		if !model.settings || model.runtime.filesSettings {
			base = model.settingsParentLayer(base, settingsView(model), 1)
		}
	}
	if model.configSettings.returnToFix || model.runtime.configParent != nil && model.runtime.configParent.returnToFix {
		base = model.overlay(base, fixDialogPopup(model.fixDialog, model.profileCatalog, model.width, model.height))
	}
	if model.runtime.configParent != nil {
		base = model.overlay(base, configSettingsPopup(*model.runtime.configParent, model.profileCatalog, model.width, model.height))
	}
	if top, ok := model.overlays.Top(); ok && top.Kind == OverlaySettingsDirty {
		base = model.overlay(base, configSettingsPopup(model.configSettings, model.profileCatalog, model.width, model.height))
	}
	return base
}

func filesSettingsView(model Model) string {
	if model.runtime.filesEditing {
		content := []string{model.runtime.filesExclusions.View()}
		if model.height > 0 && model.height < 10 {
			return style.TightPopup(style.Heading("SOURCE EXCLUSIONS"), content, "Ctrl+S save · Esc cancel", model.runtime.filesExclusions.Width()+4)
		}
		return style.Popup(style.Heading("SOURCE EXCLUSIONS"), content, "Ctrl+S save · Esc cancel", model.runtime.filesExclusions.Width()+4)
	}
	mark := " "
	if !model.options.DisableGitignore {
		mark = "✓"
	}
	return style.Popup(style.Heading("FILES"), []string{
		style.ToggleOption("["+mark+"]", "Honor gitignore", model.runtime.filesCursor == 0, false, 34),
		style.ToggleOption("›", "Project source exclusions", model.runtime.filesCursor == 1, false, 34),
	}, "↑/↓ select · Enter edit · Esc back", 42)
}

func (model Model) settingsParentLayer(base, popup string, depth int) string {
	if model.width <= 0 || model.height <= 0 {
		return base
	}
	lines := strings.Split(popup, "\n")
	left := max(0, (model.width-lipgloss.Width(popup))/2-depth*2)
	top := max(0, (model.height-len(lines))/2-depth*2)
	return strings.Join(overlayFixLines(strings.Split(base, "\n"), lines, left, top, model.width, model.height), "\n")
}
