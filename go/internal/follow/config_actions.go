package follow

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fixapp"
)

func (model *Model) handleAgentSettingsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := model.configSettings.handleAgentKey(key, model.profileCatalog)
	return model.applyConfigKeyAction(action)
}

func (model *Model) openSelectedAgentProvider() tea.Cmd {
	state := &model.configSettings
	if state.cursor == len(agentProviderChoices) {
		parent := *state
		model.configParent = &parent
		*state = newConfigSettingsState(configConcurrency, parent.generation+1)
		state.loading = false
		state.resolved = cloneConfigResolved(parent.resolved)
		state.working = cloneConfigResolved(parent.working)
		return nil
	}
	if state.cursor < 0 || state.cursor >= len(agentProviderChoices) {
		return nil
	}
	choice := agentProviderChoices[state.cursor]
	if !state.selectProvider(choice) {
		return nil
	}
	return state.testSelectedProfileCommand(model.profileProber)
}

func (model *Model) closeConfigSettings() tea.Cmd {
	if model.configSettings.dirty {
		if model.configSettings.kind != configAgents {
			model.configSettings.closeAfterSave = true
			return model.saveConfigSettings()
		}
		model.configSettings.dirtyCursor = 0
		model.overlays.Push(OverlaySettingsDirty, OverlayCaller{MainView: model.mainView, Overlay: OverlayConfigSettings, Selected: model.mainSelection()})
		return nil
	}
	return model.closeConfigSettingsNow()
}

func (model *Model) closeConfigSettingsNow() tea.Cmd {
	if model.configParent != nil {
		parent := *model.configParent
		parent.resolved.Revision = model.configSettings.resolved.Revision
		parent.working.Revision = model.configSettings.working.Revision
		parent.working.Concurrency = model.configSettings.working.Concurrency
		parent.resolved.Concurrency = model.configSettings.resolved.Concurrency
		parent.generation = model.configSettings.generation + 1
		model.configSettings = parent
		model.configParent = nil
		return model.configSettings.probeProfilesCommand(model.profileProber)
	}
	returnToFix := model.configSettings.returnToFix
	model.configSettings.open = false
	model.configSettings.editing = false
	model.configSettings.returnToFix = false
	if returnToFix {
		if top, ok := model.overlays.Top(); ok && top.Kind == OverlayConfigSettings {
			model.overlays.Pop()
		}
		if !overlayPresent(model.overlays, OverlayFixForm) || !model.fixDialog.hasInput && len(model.fixDialog.targetPaths()) == 0 {
			return nil
		}
		model.fixGeneration++
		model.fixDialog.generation = model.fixGeneration
		model.fixDialog.loading = true
		model.fixDialog.errorText = ""
		model.fixDialog.statusText = "Rechecking settings, runtime, and workspace readiness…"
		var profile *agent.ProfileID
		if model.fixDialog.hasInput && model.fixDialog.input.Profile.ID != "" {
			selected := model.fixDialog.input.Profile.ID
			profile = &selected
		}
		var delivery *fixapp.LoadDelivery
		if model.fixDialog.hasInput {
			delivery = &fixapp.LoadDelivery{Plan: model.fixDialog.input.DeliveryPlan, Branch: model.fixDialog.input.BranchName}
		}
		workspace := fixLoadWorkspace(model.fixWorkspace, model.options.Workspace)
		return loadFixCommand(model.fixService, workspace, model.fixDialog.targetPaths(), profile, delivery, model.fixDialog.generation)
	}
	model.settings = true
	return nil
}

func (model *Model) handleSettingsDirtyKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	switch state.handleDirtyKey(key) {
	case configDirtySave:
		return model, model.saveConfigSettings()
	case configDirtyDiscard:
		model.overlays.Pop()
		return model, model.closeConfigSettingsNow()
	case configDirtyCancel:
		model.overlays.Pop()
	}
	return model, nil
}

func (model *Model) saveConfigSettings() tea.Cmd {
	state := &model.configSettings
	request, ok := state.prepareSave(model.profileCatalog, model.configStore, model.configWorkspace, model.fixService)
	if !ok {
		return nil
	}
	return request.command()
}

func configSettingsPatch(kind configSettingsKind, value appconfig.Resolved) appconfig.Patch {
	switch kind {
	case configAgents:
		profiles := cloneConfigProfiles(value.Profiles)
		return appconfig.Patch{Profiles: &profiles}
	case configFix:
		fixDefaults := cloneConfigFix(value.Fix)
		return appconfig.Patch{Fix: &fixDefaults}
	case configConcurrency:
		concurrency := value.Concurrency
		return appconfig.Patch{Concurrency: &concurrency}
	case configDelivery:
		delivery := value.Delivery
		return appconfig.Patch{Delivery: &delivery}
	default:
		return appconfig.Patch{}
	}
}
