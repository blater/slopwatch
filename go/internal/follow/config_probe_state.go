package follow

import (
	"context"
	"maps"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
)

func (state *configSettingsState) nextProbeAttempt(profile agent.ProfileID) uint64 {
	if state.probeAttempts == nil {
		state.probeAttempts = map[agent.ProfileID]uint64{}
	}
	state.probeSequence++
	state.probeAttempts[profile] = state.probeSequence
	return state.probeSequence
}

func (state *configSettingsState) testSelectedProfileCommand(prober ProfileProber) tea.Cmd {
	index := state.selectedProfileIndex()
	if prober == nil || index < 0 {
		return nil
	}
	profile := cloneConfigProfile(state.working.Profiles[index])
	if state.probing == nil {
		state.probing = map[agent.ProfileID]bool{}
	}
	if state.probing[profile.ID] {
		state.status = "Checking connection…"
		return nil
	}
	state.probing[profile.ID] = true
	attempt := state.nextProbeAttempt(profile.ID)
	delete(state.probes, profile.ID)
	state.connectionTitle = ""
	state.connectionError = ""
	if profile.ID != state.working.Fix.Profile {
		state.pendingActive = profile.ID
	}
	state.status = "Checking connection…"
	generation := state.generation
	return func() tea.Msg {
		return configProbeMsg{generation: generation, attempt: attempt, profile: profile.ID, definition: profile, result: prober.Probe(context.Background(), profile)}
	}
}

func (state *configSettingsState) probeProfilesCommand(prober ProfileProber) tea.Cmd {
	if prober == nil || len(state.working.Profiles) == 0 {
		return nil
	}
	if state.probing == nil {
		state.probing = map[agent.ProfileID]bool{}
	}
	commands := make([]tea.Cmd, 0, len(state.working.Profiles))
	for _, value := range state.working.Profiles {
		profile := cloneConfigProfile(value)
		if profileCountForRuntime(state.working.Profiles, profile.Runtime) != 1 {
			continue
		}
		attempt := state.nextProbeAttempt(profile.ID)
		state.probing[profile.ID] = true
		delete(state.probes, profile.ID)
		generation := state.generation
		commands = append(commands, func() tea.Msg {
			return configProbeMsg{generation: generation, attempt: attempt, profile: profile.ID, definition: profile, result: prober.Probe(context.Background(), profile)}
		})
	}
	return tea.Batch(commands...)
}

func probeDefinitionCurrent(profiles []agent.Profile, definition agent.Profile) bool {
	for _, profile := range profiles {
		if profile.ID == definition.ID {
			return profile.Runtime == definition.Runtime && profile.Executable == definition.Executable &&
				profile.RuntimeProfile == definition.RuntimeProfile && profile.AuthenticationRef == definition.AuthenticationRef &&
				maps.Equal(profile.Options, definition.Options)
		}
	}
	return false
}

func (model *Model) handleConfigProbe(message configProbeMsg) tea.Cmd {
	if !model.configSettings.applyProbeAndPrepareSave(message) {
		return nil
	}
	return model.saveConfigSettings()
}

func (state *configSettingsState) applyProbe(message configProbeMsg) bool {
	if !state.open || message.generation != state.generation ||
		message.attempt == 0 || state.probeAttempts[message.profile] != message.attempt ||
		!probeDefinitionCurrent(state.working.Profiles, message.definition) {
		return false
	}
	if state.probes == nil {
		state.probes = map[agent.ProfileID]agent.ProbeResult{}
	}
	delete(state.probing, message.profile)
	state.probes[message.profile] = message.result
	if !state.profileEditing || message.definition.Runtime != state.providerRuntime {
		return false
	}
	ready := message.result.State == agent.ProbeReady && message.result.Capabilities.Isolation.EligibleForMutation()
	if !ready {
		state.status = "Connection failed"
		state.connectionTitle = "CONNECTION FAILED"
		state.connectionError = nonemptySetting(cleanAgentText(message.result.Diagnostic), agentProbeReadiness(message.result))
		state.rollbackPendingAgentEdit()
		return false
	}
	state.status = "Connected"
	state.connectionTitle = ""
	state.connectionError = ""
	shouldSave := state.pendingOriginal != nil && state.pendingOriginal.ID == message.profile
	if state.pendingActive == message.profile && state.working.Fix.Profile != message.profile {
		state.beginPendingAgentChange()
		if state.pendingFix == nil {
			original := cloneConfigFix(state.working.Fix)
			state.pendingFix = &original
		}
		state.working.Fix.Profile = message.profile
		reconcileFixAgentOptions(&state.working.Fix, message.result, true)
		state.defaultChanged = true
		state.dirty = true
		shouldSave = true
	}
	state.pendingActive = ""
	return shouldSave
}

func (state *configSettingsState) applyProbeAndPrepareSave(message configProbeMsg) bool {
	if !state.applyProbe(message) {
		return false
	}
	state.status = "Connected · saving…"
	return true
}
