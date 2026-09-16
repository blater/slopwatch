package follow

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
)

func (model *Model) openConfigSettings(kind configSettingsKind) tea.Cmd {
	model.configSettings = newConfigSettingsState(kind, model.configSettings.generation+1)
	if model.configStore == nil {
		model.configSettings.loading = false
		model.configSettings.status = "Feature settings unavailable: no configuration service"
		return nil
	}
	generation := model.configSettings.generation
	store, workspace := model.configStore, model.configWorkspace
	return func() tea.Msg {
		if editor, ok := store.(appconfig.Editor); ok {
			editable, err := editor.LoadEditable(context.Background(), workspace)
			return configResolvedMsg{generation: generation, resolved: editable.Resolved, diagnostics: editable.Diagnostics, err: err}
		}
		resolved, err := store.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
		return configResolvedMsg{generation: generation, resolved: resolved, err: err}
	}
}

func (model *Model) handleConfigResolved(message configResolvedMsg) tea.Cmd {
	outcome := model.configSettings.applyResolved(message)
	if !outcome.accepted || !outcome.probe {
		return nil
	}
	return model.configSettings.probeProfilesCommand(model.profileProber)
}

func agentProviderIndex(runtime agent.RuntimeKind) int {
	for index, choice := range agentProviderChoices {
		if choice.Runtime == runtime {
			return index
		}
	}
	return 0
}

func runtimeForProfile(profiles []agent.Profile, id agent.ProfileID) agent.RuntimeKind {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile.Runtime
		}
	}
	return ""
}

func profileIndexForRuntime(profiles []agent.Profile, runtime agent.RuntimeKind, _ agent.ProfileID) int {
	match := -1
	for index, profile := range profiles {
		if profile.Runtime != runtime {
			continue
		}
		if match >= 0 {
			return -1
		}
		match = index
	}
	return match
}

func profileCountForRuntime(profiles []agent.Profile, runtime agent.RuntimeKind) int {
	count := 0
	for _, profile := range profiles {
		if profile.Runtime == runtime {
			count++
		}
	}
	return count
}

func (model *Model) handleConfigSaved(message configSavedMsg) tea.Cmd {
	outcome := model.configSettings.applySaved(message)
	if !outcome.accepted {
		return nil
	}
	if outcome.close {
		if overlayPresent(model.overlays, OverlaySettingsDirty) {
			model.overlays.Pop()
		}
		return model.closeConfigSettingsNow()
	}
	if outcome.probe {
		return model.configSettings.probeProfilesCommand(model.profileProber)
	}
	return nil
}
