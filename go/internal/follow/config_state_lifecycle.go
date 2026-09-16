package follow

import (
	"errors"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/style"
)

func newConfigSettingsState(kind configSettingsKind, generation uint64) configSettingsState {
	input := textinput.New()
	input.Prompt = ""
	style.ApplyTextInputStyle(&input, false)
	return configSettingsState{
		open: true, kind: kind, generation: generation, loading: true,
		probes: map[agent.ProfileID]agent.ProbeResult{}, probing: map[agent.ProfileID]bool{},
		probeAttempts: map[agent.ProfileID]uint64{}, input: input, prompt: newMasterPromptTextBox(""),
	}
}

type configResolvedOutcome struct {
	accepted bool
	probe    bool
}

func (state *configSettingsState) applyResolved(message configResolvedMsg) configResolvedOutcome {
	if !state.open || message.generation != state.generation {
		return configResolvedOutcome{}
	}
	state.loading = false
	if message.err != nil {
		state.status = "Load failed: " + message.err.Error()
		return configResolvedOutcome{accepted: true}
	}
	state.resolved = cloneConfigResolved(message.resolved)
	state.working = cloneConfigResolved(message.resolved)
	state.prompt = newMasterPromptTextBox(state.working.Fix.PromptTemplate)
	state.promptOriginal = state.working.Fix.PromptTemplate
	state.probes = map[agent.ProfileID]agent.ProbeResult{}
	state.probing = map[agent.ProfileID]bool{}
	state.probeAttempts = map[agent.ProfileID]uint64{}
	state.defaultChanged = false
	if state.kind == configAgents {
		state.cursor = agentProviderIndex(runtimeForProfile(state.working.Profiles, state.working.Fix.Profile))
	} else {
		state.cursor = min(state.cursor, max(0, configSettingsRowsForState(*state)-1))
	}
	state.status = ""
	if len(message.diagnostics) > 0 {
		state.status = "Needs repair: " + message.diagnostics[0]
	}
	return configResolvedOutcome{accepted: true, probe: true}
}

type configSavedOutcome struct {
	accepted bool
	close    bool
	probe    bool
}

func (state *configSettingsState) applySaved(message configSavedMsg) configSavedOutcome {
	if !state.open || message.generation != state.generation {
		return configSavedOutcome{}
	}
	state.saving = false
	if message.err != nil {
		if state.kind == configAgents {
			state.rollbackPendingAgentEdit()
			state.connectionTitle = "ACTIVATION FAILED"
			state.connectionError = "Could not save the active connection: " + cleanAgentText(message.err.Error())
		}
		if errors.Is(message.err, appconfig.ErrRevisionConflict) {
			state.status = "Save failed: settings changed elsewhere; Esc and reopen this screen"
		} else {
			state.status = "Save failed: " + message.err.Error()
		}
		return configSavedOutcome{accepted: true}
	}
	state.pendingChange = false
	state.pendingOriginal = nil
	state.pendingFix = nil
	state.pendingWasDirty = false
	state.pendingDefault = false
	state.connectionTitle = ""
	state.connectionError = ""
	state.resolved = cloneConfigResolved(message.saved.Resolved)
	state.working = cloneConfigResolved(message.saved.Resolved)
	state.dirty = false
	state.defaultChanged = false
	state.status = "Saved"
	outcome := configSavedOutcome{accepted: true, probe: state.kind == configAgents}
	if state.closeAfterSave {
		state.closeAfterSave = false
		outcome.close = true
		outcome.probe = false
	}
	return outcome
}

func (state *configSettingsState) handleDirtyKey(key tea.KeyMsg) configDirtyAction {
	if state.saving {
		return configDirtyNone
	}
	switch key.String() {
	case "up", "k":
		state.dirtyCursor = max(0, state.dirtyCursor-1)
	case "down", "j":
		state.dirtyCursor = min(2, state.dirtyCursor+1)
	case "esc", "q":
		state.closeAfterSave = false
		return configDirtyCancel
	case "enter":
		switch state.dirtyCursor {
		case 0:
			state.closeAfterSave = true
			return configDirtySave
		case 1:
			state.dirty = false
			return configDirtyDiscard
		case 2:
			state.closeAfterSave = false
			return configDirtyCancel
		}
	}
	return configDirtyNone
}
