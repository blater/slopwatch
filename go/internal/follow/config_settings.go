package follow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/blater/slopwatch/internal/style"
)

type configSettingsKind string

const (
	configAgents      configSettingsKind = "agents"
	configFix         configSettingsKind = "fix"
	configConcurrency configSettingsKind = "concurrency"
	configDelivery    configSettingsKind = "delivery"
)

type configSettingsState struct {
	open            bool
	kind            configSettingsKind
	generation      uint64
	loading         bool
	saving          bool
	dirty           bool
	cursor          int
	resolved        appconfig.Resolved
	working         appconfig.Resolved
	probes          map[agent.ProfileID]agent.ProbeResult
	status          string
	input           textinput.Model
	prompt          textarea.Model
	promptOriginal  string
	promptError     string
	editing         bool
	editField       int
	choiceOpen      bool
	choiceCursor    int
	dirtyCursor     int
	closeAfterSave  bool
	returnToFix     bool
	profileEditing  bool
	profileCursor   int
	providerCursor  int
	providerRuntime agent.RuntimeKind
	probing         map[agent.ProfileID]bool
	probeAttempts   map[agent.ProfileID]uint64
	probeSequence   uint64
	pendingActive   agent.ProfileID
	pendingChange   bool
	pendingOriginal *agent.Profile
	pendingFix      *appconfig.FixDefaults
	pendingWasDirty bool
	pendingDefault  bool
	defaultChanged  bool
	connectionTitle string
	connectionError string
}

type agentProviderChoice struct {
	Label       string
	Runtime     agent.RuntimeKind
	Unavailable string
}

var agentProviderChoices = []agentProviderChoice{
	{Label: "Claude CLI", Runtime: "claude-cli", Unavailable: "The Claude CLI adapter is not included in this Slopwatch build."},
	{Label: "Claude API", Runtime: "anthropic-api", Unavailable: "The Claude API adapter is not included in this Slopwatch build."},
	{Label: "Codex", Runtime: "codex-cli"},
	{Label: "Grok", Runtime: "grok-api", Unavailable: "The Grok adapter is not included in this Slopwatch build."},
	{Label: "OpenAI API", Runtime: "openai-responses"},
}

type configResolvedMsg struct {
	generation  uint64
	resolved    appconfig.Resolved
	err         error
	diagnostics []string
}

type configSavedMsg struct {
	generation uint64
	saved      appconfig.Saved
	err        error
}

type configProbeMsg struct {
	generation uint64
	attempt    uint64
	profile    agent.ProfileID
	definition agent.Profile
	result     agent.ProbeResult
}

func (model *Model) openConfigSettings(kind configSettingsKind) tea.Cmd {
	input := textinput.New()
	input.Prompt = ""
	style.ApplyTextInputStyle(&input, false)
	prompt := newMasterPromptTextBox("")
	model.configSettings = configSettingsState{
		open: true, kind: kind, generation: model.configSettings.generation + 1,
		loading: true, probes: map[agent.ProfileID]agent.ProbeResult{}, probing: map[agent.ProfileID]bool{},
		probeAttempts: map[agent.ProfileID]uint64{}, input: input, prompt: prompt,
	}
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
	state := &model.configSettings
	if !state.open || message.generation != state.generation {
		return nil
	}
	state.loading = false
	if message.err != nil {
		state.status = "Load failed: " + message.err.Error()
		return nil
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
		state.cursor = min(state.cursor, max(0, model.configSettingsRows()-1))
	}
	state.status = ""
	if len(message.diagnostics) > 0 {
		state.status = "Needs repair: " + message.diagnostics[0]
	}
	return model.probeProfilesCommand()
}

func (model *Model) probeProfilesCommand() tea.Cmd {
	if model.profileProber == nil || len(model.configSettings.working.Profiles) == 0 {
		return nil
	}
	if model.configSettings.probing == nil {
		model.configSettings.probing = map[agent.ProfileID]bool{}
	}
	generation := model.configSettings.generation
	commands := make([]tea.Cmd, 0, len(model.configSettings.working.Profiles))
	for _, value := range model.configSettings.working.Profiles {
		profile := cloneConfigProfile(value)
		if profileCountForRuntime(model.configSettings.working.Profiles, profile.Runtime) != 1 {
			continue
		}
		attempt := model.nextConfigProbeAttempt(profile.ID)
		model.configSettings.probing[profile.ID] = true
		delete(model.configSettings.probes, profile.ID)
		prober := model.profileProber
		commands = append(commands, func() tea.Msg {
			return configProbeMsg{generation: generation, attempt: attempt, profile: profile.ID, definition: profile, result: prober.Probe(context.Background(), profile)}
		})
	}
	return tea.Batch(commands...)
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

func (model Model) selectedAgentProfileIndex() int {
	state := model.configSettings
	if !state.profileEditing {
		return -1
	}
	return profileIndexForRuntime(state.working.Profiles, state.providerRuntime, state.working.Fix.Profile)
}

func (model *Model) handleConfigSaved(message configSavedMsg) tea.Cmd {
	state := &model.configSettings
	if !state.open || message.generation != state.generation {
		return nil
	}
	state.saving = false
	if message.err != nil {
		if state.kind == configAgents {
			model.rollbackPendingAgentEdit()
			state.connectionTitle = "ACTIVATION FAILED"
			state.connectionError = "Could not save the active connection: " + cleanAgentText(message.err.Error())
		}
		if errors.Is(message.err, appconfig.ErrRevisionConflict) {
			state.status = "Save failed: settings changed elsewhere; Esc and reopen this screen"
		} else {
			state.status = "Save failed: " + message.err.Error()
		}
		return nil
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
	if state.closeAfterSave {
		state.closeAfterSave = false
		if model.hasOverlay(OverlaySettingsDirty) {
			model.overlays.Pop()
		}
		return model.closeConfigSettingsNow()
	}
	if state.kind == configAgents {
		return model.probeProfilesCommand()
	}
	return nil
}

func (model *Model) handleConfigSettingsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	if state.editing {
		switch key.String() {
		case "esc", "escape":
			state.editing = false
			state.input.Blur()
			return model, nil
		case "enter":
			if err := model.commitConfigText(); err != nil {
				state.status = err.Error()
				return model, nil
			}
			state.editing = false
			state.input.Blur()
			if state.kind == configAgents {
				return model, model.testSelectedProfileCommand()
			}
			return model, model.saveConfigSettings()
		default:
			var command tea.Cmd
			state.input, command = state.input.Update(key)
			return model, command
		}
	}
	if state.loading || state.saving {
		if state.loading && (key.String() == "esc" || key.String() == "escape") {
			return model, model.closeConfigSettings()
		}
		return model, nil
	}
	if state.kind == configAgents {
		return model.handleAgentSettingsKey(key)
	}
	name := key.String()
	if state.choiceOpen {
		return model.handleConfigChoiceKey(name)
	}
	if state.kind == configFix && state.cursor >= fixSettingsMetricStart && isToggleKey(name) {
		return model, model.adjustConfigSetting(1)
	}
	switch name {
	case "esc", "escape", "q":
		return model, model.closeConfigSettings()
	case "up", "k":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = max(fixSettingsPromptRow, state.cursor-3)
		} else {
			state.cursor = max(0, state.cursor-1)
		}
	case "down", "j":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = min(max(0, model.configSettingsRows()-1), state.cursor+3)
		} else {
			state.cursor = min(max(0, model.configSettingsRows()-1), state.cursor+1)
		}
	case "left", "h", "-":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = max(fixSettingsMetricStart, state.cursor-1)
		} else if model.configChoiceField(state.cursor) {
			return model, nil
		} else {
			return model, model.adjustConfigSetting(-1)
		}
	case "right", "l", "+", "=":
		if state.kind == configFix && state.cursor >= fixSettingsMetricStart {
			state.cursor = min(max(0, model.configSettingsRows()-1), state.cursor+1)
		} else if model.configChoiceField(state.cursor) {
			return model, nil
		} else {
			return model, model.adjustConfigSetting(1)
		}
	case " ":
		return model, model.adjustConfigSetting(1)
	case "enter":
		if model.configChoiceField(state.cursor) {
			model.openConfigChoice()
		} else if state.kind == configFix && state.cursor == fixSettingsPromptRow {
			state.prompt = newMasterPromptTextBox(state.working.Fix.PromptTemplate)
			state.promptOriginal = state.working.Fix.PromptTemplate
			state.promptError = ""
			state.prompt.Focus()
			model.resizeMasterPromptTextBox()
			model.overlays.Push(OverlayPromptEditor, OverlayCaller{MainView: model.mainView, Overlay: OverlayConfigSettings, Selected: model.mainSelection()})
		} else if state.kind == configAgents && len(state.working.Profiles) > 0 {
			state.profileEditing = true
			state.profileCursor = 0
		} else if model.configTextEditable() {
			model.beginConfigText()
		} else {
			return model, model.adjustConfigSetting(1)
		}
	}
	return model, nil
}

func (model *Model) handleMasterPromptKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	switch key.String() {
	case "ctrl+s":
		value := strings.TrimSpace(cleanEditorText(state.prompt.Value()))
		if value == "" {
			state.promptError = "Agent prompt cannot be empty"
			model.resizeMasterPromptTextBox()
			return model, nil
		}
		state.promptError = ""
		state.working.Fix.PromptTemplate = value
		state.promptOriginal = value
		state.prompt.Blur()
		state.dirty = true
		model.overlays.Pop()
		return model, model.saveConfigSettings()
	case "esc", "escape":
		state.prompt.SetValue(state.promptOriginal)
		state.promptError = ""
		state.prompt.Blur()
		model.overlays.Pop()
		return model, nil
	default:
		updated, command := state.prompt.Update(key)
		state.prompt = updated
		state.promptError = ""
		model.resizeMasterPromptTextBox()
		return model, command
	}
}

func (model Model) configChoices(field int) []fixDialogChoice {
	state := model.configSettings
	switch state.kind {
	case configFix:
		switch field {
		case 1:
			values := []string{"targets-only", "targets-and-tests", "repository"}
			choices := make([]fixDialogChoice, 0, len(values))
			for _, value := range values {
				choices = append(choices, fixDialogChoice{value: value, label: changeScopeLabel(value), selected: value == state.working.Fix.ChangeScope})
			}
			return choices
		case 2:
			choices := make([]fixDialogChoice, 0, len(state.working.Profiles))
			for _, profile := range state.working.Profiles {
				choices = append(choices, fixDialogChoice{value: string(profile.ID), label: agentProfileChoiceLabel(profile), selected: profile.ID == state.working.Fix.Profile})
			}
			return choices
		case 3:
			probe, _ := readyAgentProbe(state.probes, state.working.Fix.Profile)
			choices := []fixDialogChoice{{label: "Runtime default", selected: state.working.Fix.Model == ""}}
			for _, option := range probe.Capabilities.Models {
				choices = append(choices, fixDialogChoice{value: string(option.ID), label: nonemptySetting(option.Label, string(option.ID)), selected: option.ID == state.working.Fix.Model})
			}
			return choices
		case 4:
			probe, _ := readyAgentProbe(state.probes, state.working.Fix.Profile)
			choices := []fixDialogChoice{{label: "Runtime default", selected: state.working.Fix.Effort == ""}}
			for _, option := range probe.Capabilities.Efforts {
				choices = append(choices, fixDialogChoice{value: string(option.ID), label: nonemptySetting(option.Label, string(option.ID)), selected: option.ID == state.working.Fix.Effort})
			}
			return choices
		}
	case configDelivery:
		field = deliverySettingField(state.working.Delivery, field)
		switch field {
		case deliverySettingWorkspace:
			return []fixDialogChoice{
				{value: string(fix.WorkspaceCurrent), label: workspaceModeLabel(fix.WorkspaceCurrent), selected: state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceCurrent},
				{value: string(fix.WorkspaceWorktree), label: workspaceModeLabel(fix.WorkspaceWorktree), selected: state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceWorktree},
			}
		case deliverySettingGit:
			return []fixDialogChoice{
				{value: string(fix.GitLeaveUncommitted), label: gitModeLabel(fix.GitLeaveUncommitted), selected: state.working.Delivery.DefaultPlan.Git == fix.GitLeaveUncommitted},
				{value: string(fix.GitCommitCurrent), label: gitModeLabel(fix.GitCommitCurrent), selected: state.working.Delivery.DefaultPlan.Git == fix.GitCommitCurrent, disabled: state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceWorktree},
				{value: string(fix.GitCommitNewBranch), label: gitModeLabel(fix.GitCommitNewBranch), selected: state.working.Delivery.DefaultPlan.Git == fix.GitCommitNewBranch},
			}
		case deliverySettingPublish:
			return []fixDialogChoice{
				{value: string(fix.PublishLocal), label: publishModeLabel(fix.PublishLocal), selected: state.working.Delivery.DefaultPlan.Publish == fix.PublishLocal},
				{value: string(fix.PublishPush), label: publishModeLabel(fix.PublishPush), selected: state.working.Delivery.DefaultPlan.Publish == fix.PublishPush},
				{value: string(fix.PublishPullRequest), label: publishModeLabel(fix.PublishPullRequest), selected: state.working.Delivery.DefaultPlan.Publish == fix.PublishPullRequest},
			}
		case deliverySettingPRState:
			return []fixDialogChoice{
				{value: "draft", label: "Draft", selected: state.working.Delivery.DraftPullRequests},
				{value: "ready", label: "Ready for review", selected: !state.working.Delivery.DraftPullRequests},
			}
		}
	}
	return nil
}

func (model Model) configChoiceField(field int) bool {
	return len(model.configChoices(field)) > 1
}

func (model *Model) openConfigChoice() {
	state := &model.configSettings
	choices := model.configChoices(state.cursor)
	if len(choices) < 2 {
		return
	}
	state.choiceOpen = true
	state.choiceCursor = 0
	for index, choice := range choices {
		if choice.selected {
			state.choiceCursor = index
			break
		}
	}
}

func (model *Model) handleConfigChoiceKey(name string) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	choices := model.configChoices(state.cursor)
	if len(choices) == 0 {
		state.choiceOpen = false
		return model, nil
	}
	switch name {
	case "esc", "escape", "q":
		state.choiceOpen = false
		return model, nil
	case "up", "k":
		state.choiceCursor = max(0, state.choiceCursor-1)
		return model, nil
	case "down", "j":
		state.choiceCursor = min(len(choices)-1, state.choiceCursor+1)
		return model, nil
	case "enter":
		choice := choices[state.choiceCursor]
		switch state.kind {
		case configFix:
			switch state.cursor {
			case 1:
				state.working.Fix.ChangeScope = choice.value
			case 2:
				state.working.Fix.Profile = agent.ProfileID(choice.value)
				probe, ready := readyAgentProbe(state.probes, state.working.Fix.Profile)
				reconcileFixAgentOptions(&state.working.Fix, probe, ready)
			case 3:
				state.working.Fix.Model = agent.ModelID(choice.value)
			case 4:
				state.working.Fix.Effort = agent.EffortID(choice.value)
			}
		case configDelivery:
			switch deliverySettingField(state.working.Delivery, state.cursor) {
			case deliverySettingWorkspace:
				state.working.Delivery.DefaultPlan.Workspace = fix.WorkspaceMode(choice.value)
				if state.working.Delivery.DefaultPlan.Workspace == fix.WorkspaceWorktree && state.working.Delivery.DefaultPlan.Git == fix.GitCommitCurrent {
					state.working.Delivery.DefaultPlan.Git = fix.GitLeaveUncommitted
					state.working.Delivery.DefaultPlan.Publish = fix.PublishLocal
				}
			case deliverySettingGit:
				state.working.Delivery.DefaultPlan.Git = fix.GitMode(choice.value)
				if state.working.Delivery.DefaultPlan.Git == fix.GitLeaveUncommitted {
					state.working.Delivery.DefaultPlan.Publish = fix.PublishLocal
				}
			case deliverySettingPublish:
				state.working.Delivery.DefaultPlan.Publish = fix.PublishMode(choice.value)
			case deliverySettingPRState:
				state.working.Delivery.DraftPullRequests = choice.value == "draft"
			}
		}
		state.choiceOpen = false
		state.dirty = true
		return model, model.saveConfigSettings()
	}
	return model, nil
}

func newMasterPromptTextBox(value string) textarea.Model {
	editor := textarea.New()
	editor.Prompt = ""
	editor.ShowLineNumbers = false
	editor.Placeholder = "Enter the instructions sent to the agent"
	editor.SetValue(cleanEditorText(value))
	editor.FocusedStyle.Base = lipgloss.NewStyle().Background(style.SurfaceFieldActive).Foreground(style.TextPrimary)
	editor.FocusedStyle.CursorLine = lipgloss.NewStyle().Background(style.SurfaceFieldActive).Foreground(style.TextPrimary)
	editor.FocusedStyle.Text = lipgloss.NewStyle().Background(style.SurfaceFieldActive).Foreground(style.TextPrimary)
	editor.BlurredStyle = editor.FocusedStyle
	return editor
}

func (model *Model) resizeMasterPromptTextBox() {
	errorRows := 0
	if model.configSettings.promptError != "" {
		errorRows = 1
	}
	model.configSettings.prompt.SetWidth(max(1, model.width))
	model.configSettings.prompt.SetHeight(max(1, model.height-2-errorRows))
}

func (model *Model) handleAgentSettingsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	if state.profileEditing {
		if index := model.selectedAgentProfileIndex(); index >= 0 && state.probing[state.working.Profiles[index].ID] && key.String() != "esc" && key.String() != "escape" {
			return model, nil
		}
		switch key.String() {
		case "esc", "escape":
			model.rollbackPendingAgentEdit()
			state.profileEditing = false
			state.pendingActive = ""
			state.cursor = state.providerCursor
			state.status = ""
		case "up", "k":
			state.profileCursor = max(0, state.profileCursor-1)
		case "down", "j":
			state.profileCursor = min(max(0, model.profileFieldCount()-1), state.profileCursor+1)
		case "left", "h", "-":
			if model.adjustProfileChoice(-1) {
				return model, model.testSelectedProfileCommand()
			}
		case "right", "l", "+", "=", " ":
			if model.adjustProfileChoice(1) {
				return model, model.testSelectedProfileCommand()
			}
		case "enter":
			if model.profileFieldCount() > 0 {
				if !model.adjustProfileChoice(1) {
					model.beginConfigText()
					return model, nil
				}
			}
			return model, model.testSelectedProfileCommand()
		}
		return model, nil
	}

	switch key.String() {
	case "esc", "escape", "q":
		return model, model.closeConfigSettings()
	case "up", "k":
		state.cursor = max(0, state.cursor-1)
	case "down", "j":
		state.cursor = min(len(agentProviderChoices)-1, state.cursor+1)
	case "enter":
		return model, model.openSelectedAgentProvider()
	}
	return model, nil
}

func (model *Model) openSelectedAgentProvider() tea.Cmd {
	state := &model.configSettings
	if state.cursor < 0 || state.cursor >= len(agentProviderChoices) {
		return nil
	}
	choice := agentProviderChoices[state.cursor]
	state.providerCursor = state.cursor
	state.providerRuntime = choice.Runtime
	state.profileCursor = 0
	state.profileEditing = true
	state.status = ""
	state.connectionTitle = ""
	state.connectionError = ""
	index := profileIndexForRuntime(state.working.Profiles, choice.Runtime, state.working.Fix.Profile)
	if index < 0 {
		return nil
	}
	profile := state.working.Profiles[index]
	if profile.ID != state.working.Fix.Profile {
		state.pendingActive = profile.ID
	}
	return model.testSelectedProfileCommand()
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
	returnToFix := model.configSettings.returnToFix
	model.configSettings.open = false
	model.configSettings.editing = false
	model.configSettings.returnToFix = false
	if returnToFix {
		if top, ok := model.overlays.Top(); ok && top.Kind == OverlayConfigSettings {
			model.overlays.Pop()
		}
		if !model.hasOverlay(OverlayFixForm) || !model.fixDialog.hasInput && len(model.fixDialog.targetPaths()) == 0 {
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
		return model.loadFixCommand(model.fixDialog.targetPaths(), profile, model.fixDialog.generation)
	}
	model.settings = true
	return nil
}

func (model *Model) handleSettingsDirtyKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	if state.saving {
		return model, nil
	}
	switch key.String() {
	case "up", "k":
		state.dirtyCursor = max(0, state.dirtyCursor-1)
	case "down", "j":
		state.dirtyCursor = min(2, state.dirtyCursor+1)
	case "esc", "q":
		state.closeAfterSave = false
		model.overlays.Pop()
	case "enter":
		switch state.dirtyCursor {
		case 0:
			state.closeAfterSave = true
			return model, model.saveConfigSettings()
		case 1:
			state.dirty = false
			model.overlays.Pop()
			return model, model.closeConfigSettingsNow()
		case 2:
			state.closeAfterSave = false
			model.overlays.Pop()
		}
	}
	return model, nil
}

func (model *Model) saveConfigSettings() tea.Cmd {
	state := &model.configSettings
	if !state.dirty {
		state.status = "No changes to save"
		return nil
	}
	if err := validateConfigSettingsWithCatalog(state.kind, state.working, model.profileCatalog); err != nil {
		state.status = "Cannot save: " + err.Error()
		return nil
	}
	if model.configStore == nil {
		state.status = "Cannot save: no configuration service"
		return nil
	}
	patch := configSettingsPatch(state.kind, state.working)
	if state.kind == configAgents && state.defaultChanged {
		fixDefaults := cloneConfigFix(state.working.Fix)
		patch.Fix = &fixDefaults
	}
	state.saving = true
	state.status = "Saving…"
	generation, revision, kind := state.generation, state.resolved.Revision, state.kind
	store, workspace, fixService := model.configStore, model.configWorkspace, model.fixService
	return func() tea.Msg {
		saved, err := store.Save(context.Background(), workspace, appconfig.ScopeUser, patch, revision)
		if err == nil && kind == configConcurrency && fixService != nil {
			err = fixService.Reconfigure(context.Background(), fixapp.RuntimeLimitsFromConcurrency(saved.Resolved.Concurrency))
		}
		return configSavedMsg{generation: generation, saved: saved, err: err}
	}
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

func validateConfigSettings(kind configSettingsKind, value appconfig.Resolved) error {
	return validateConfigSettingsWithCatalog(kind, value, nil)
}

func validateConfigSettingsWithCatalog(kind configSettingsKind, value appconfig.Resolved, catalog agent.ProfileCatalog) error {
	switch kind {
	case configAgents:
		seen := map[agent.ProfileID]bool{}
		seenRuntime := map[agent.RuntimeKind]bool{}
		for _, profile := range value.Profiles {
			if profile.ID == "" || profile.Runtime == "" {
				return errors.New("every agent profile needs an ID and runtime")
			}
			if seen[profile.ID] {
				return fmt.Errorf("duplicate agent profile %q", profile.ID)
			}
			seen[profile.ID] = true
			if seenRuntime[profile.Runtime] {
				return fmt.Errorf("multiple agent profiles use runtime %q; this release supports one account per provider", profile.Runtime)
			}
			seenRuntime[profile.Runtime] = true
			if catalog == nil {
				if profile.Runtime == "codex-cli" && profile.Executable == "" {
					return fmt.Errorf("agent profile %q needs an executable", profile.ID)
				}
				continue
			}
			descriptor, err := catalog.Descriptor(profile.Runtime)
			if err != nil {
				return fmt.Errorf("agent profile %q: %w", profile.ID, err)
			}
			for _, field := range descriptor.Fields {
				if err := validateProfileFieldValue(field, profileFieldValue(profile, field)); err != nil {
					return fmt.Errorf("agent profile %q: %w", profile.ID, err)
				}
			}
			if err := catalog.ValidateProfile(profile); err != nil {
				return fmt.Errorf("agent profile %q: %w", profile.ID, err)
			}
		}
	case configFix:
		if math.IsNaN(value.Fix.TargetScore) || math.IsInf(value.Fix.TargetScore, 0) || value.Fix.TargetScore < 0 {
			return errors.New("target score must be finite and non-negative")
		}
	case configConcurrency:
		if value.Concurrency.MaxAgents <= 0 || value.Concurrency.MaxVerifiers <= 0 ||
			value.Concurrency.MaxActorsPerJob <= 0 || value.Concurrency.MaxCandidatePreviewBytes <= 0 || value.Concurrency.MaxCandidatePreviewLines <= 0 {
			return errors.New("all concurrency limits must be positive")
		}
	case configDelivery:
		if !value.Delivery.DefaultPlan.Valid() {
			return errors.New("delivery defaults are inconsistent")
		}
		if value.Delivery.DefaultPlan.Publish == fix.PublishPullRequest && strings.TrimSpace(value.Delivery.BaseBranch) == "" {
			return errors.New("pull-request delivery requires an explicit base branch")
		}
		if value.Delivery.CommandOutputBytes <= 0 {
			return errors.New("Git and publisher output bytes must be positive")
		}
		if value.Delivery.Publisher != "github-cli" {
			return errors.New("configured pull-request publisher is unavailable")
		}
		if err := appconfig.ValidateBranchTemplate(value.Delivery.BranchTemplate); err != nil {
			return err
		}
	}
	return nil
}

func (model *Model) adjustConfigSetting(direction int) tea.Cmd {
	state := &model.configSettings
	if state.loading {
		return nil
	}
	changed := false
	switch state.kind {
	case configFix:
		changed = adjustFixSetting(&state.working, state.cursor, direction, state.probes)
	case configConcurrency:
		changed = adjustConcurrencySetting(&state.working.Concurrency, state.cursor, direction)
	case configDelivery:
		changed = adjustDeliverySetting(&state.working.Delivery, state.cursor, direction)
	}
	if changed {
		state.dirty = true
		return model.saveConfigSettings()
	}
	return nil
}

const (
	fixSettingsPromptRow   = 5
	fixSettingsMetricStart = 6
)

func adjustFixSetting(value *appconfig.Resolved, cursor, direction int, probes map[agent.ProfileID]agent.ProbeResult) bool {
	switch cursor {
	case 0:
		value.Fix.TargetScore = math.Max(0, value.Fix.TargetScore+float64(direction*5))
	case 1:
		value.Fix.ChangeScope = cycleString(value.Fix.ChangeScope, []string{"targets-only", "targets-and-tests", "repository"}, direction)
	case 2:
		profiles := []agent.ProfileID{""}
		for _, profile := range value.Profiles {
			profiles = append(profiles, profile.ID)
		}
		value.Fix.Profile = cycleTyped(value.Fix.Profile, profiles, direction)
		probe, ready := readyAgentProbe(probes, value.Fix.Profile)
		reconcileFixAgentOptions(&value.Fix, probe, ready)
	case 3:
		probe, _ := readyAgentProbe(probes, value.Fix.Profile)
		options := modelIDs(probe)
		if len(options) == 0 {
			return false
		}
		value.Fix.Model = cycleTyped(value.Fix.Model, options, direction)
	case 4:
		probe, _ := readyAgentProbe(probes, value.Fix.Profile)
		options := effortIDs(probe)
		if len(options) == 0 {
			return false
		}
		value.Fix.Effort = cycleTyped(value.Fix.Effort, options, direction)
	case fixSettingsPromptRow:
		// Edited in the multiline master-prompt editor.
		return false
	default:
		metrics := fixSettingsMetrics()
		index := cursor - fixSettingsMetricStart
		if index < 0 || index >= len(metrics) {
			return false
		}
		value.Fix.Focus = toggleMetric(value.Fix.Focus, metrics[index])
	}
	return true
}

func adjustConcurrencySetting(value *appconfig.Concurrency, cursor, direction int) bool {
	switch cursor {
	case 0:
		value.MaxAgents = max(1, value.MaxAgents+direction)
	case 1:
		value.MaxVerifiers = max(1, value.MaxVerifiers+direction)
	case 2:
		value.MaxActorsPerJob = max(1, value.MaxActorsPerJob+direction)
	case 3:
		value.MaxCandidatePreviewBytes = maxInt64(1, value.MaxCandidatePreviewBytes+int64(direction)*256*1024)
	case 4:
		value.MaxCandidatePreviewLines = max(1, value.MaxCandidatePreviewLines+direction*100)
	default:
		return false
	}
	return true
}

const (
	deliverySettingWorkspace = iota
	deliverySettingGit
	deliverySettingPublish
	deliverySettingBranch
	deliverySettingRemote
	deliverySettingBase
	deliverySettingPRState
	deliverySettingCommitTitle
	deliverySettingCommitBody
	deliverySettingPRTitle
	deliverySettingPRBody
)

func deliverySettingFields(value appconfig.Delivery) []int {
	fields := []int{deliverySettingWorkspace, deliverySettingGit}
	if value.DefaultPlan.Git == fix.GitLeaveUncommitted {
		return fields
	}
	fields = append(fields, deliverySettingPublish)
	if value.DefaultPlan.Git == fix.GitCommitNewBranch {
		fields = append(fields, deliverySettingBranch)
	}
	if value.DefaultPlan.Publish != fix.PublishLocal {
		fields = append(fields, deliverySettingRemote)
	}
	if value.DefaultPlan.Publish == fix.PublishPullRequest {
		fields = append(fields, deliverySettingBase, deliverySettingPRState)
	}
	fields = append(fields, deliverySettingCommitTitle, deliverySettingCommitBody)
	if value.DefaultPlan.Publish == fix.PublishPullRequest {
		fields = append(fields, deliverySettingPRTitle, deliverySettingPRBody)
	}
	return fields
}

func deliverySettingField(value appconfig.Delivery, cursor int) int {
	fields := deliverySettingFields(value)
	if cursor < 0 || cursor >= len(fields) {
		return -1
	}
	return fields[cursor]
}

func adjustDeliverySetting(value *appconfig.Delivery, cursor, direction int) bool {
	switch deliverySettingField(*value, cursor) {
	case deliverySettingPRState:
		value.DraftPullRequests = !value.DraftPullRequests
	default:
		return false
	}
	return true
}

func (model Model) profileDescriptor(profile agent.Profile) (agent.ProfileDescriptor, error) {
	if model.profileCatalog == nil {
		return agent.ProfileDescriptor{}, errors.New("agent profile schema is unavailable")
	}
	return model.profileCatalog.Descriptor(profile.Runtime)
}

func (model Model) profileFieldCount() int {
	index := model.selectedAgentProfileIndex()
	if index < 0 {
		return 0
	}
	return len(model.profileEditorFields(model.configSettings.working.Profiles[index]))
}

func (model Model) profileEditorFields(profile agent.Profile) []agent.ProfileField {
	descriptor, err := model.profileDescriptor(profile)
	if err != nil {
		return nil
	}
	fields := make([]agent.ProfileField, 0, len(descriptor.Fields))
	for _, field := range descriptor.Fields {
		if !field.PreferencesOnly {
			fields = append(fields, field)
		}
	}
	return fields
}

func profileFieldValue(profile agent.Profile, field agent.ProfileField) string {
	switch field.Key {
	case "executable":
		return profile.Executable
	case "authentication_ref":
		return profile.AuthenticationRef
	case "runtime_profile":
		return profile.RuntimeProfile
	}
	if field.OptionKey != "" {
		return profile.Options[field.OptionKey]
	}
	return ""
}

func setProfileFieldValue(profile *agent.Profile, field agent.ProfileField, value string) {
	switch field.Key {
	case "executable":
		profile.Executable = value
	case "authentication_ref":
		profile.AuthenticationRef = value
	case "runtime_profile":
		profile.RuntimeProfile = value
	default:
		if field.OptionKey != "" {
			if profile.Options == nil {
				profile.Options = map[string]string{}
			}
			if value == "" {
				delete(profile.Options, field.OptionKey)
			} else {
				profile.Options[field.OptionKey] = value
			}
		}
	}
}

func validateProfileFieldValue(field agent.ProfileField, value string) error {
	if field.Required && value == "" {
		return fmt.Errorf("%s is required", field.Label)
	}
	if field.Kind == agent.ProfileFieldChoice && value != "" && !profileChoiceContains(field.Choices, value) {
		return fmt.Errorf("%s must be one of %s", field.Label, strings.Join(field.Choices, ", "))
	}
	if field.Pattern != "" && value != "" {
		pattern, err := regexp.Compile(field.Pattern)
		if err != nil || !pattern.MatchString(value) {
			return fmt.Errorf("%s has an invalid value", field.Label)
		}
	}
	return nil
}

func profileChoiceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (model *Model) adjustProfileChoice(direction int) bool {
	state := &model.configSettings
	index := model.selectedAgentProfileIndex()
	if !state.profileEditing || index < 0 {
		return false
	}
	profile := &state.working.Profiles[index]
	fields := model.profileEditorFields(*profile)
	fieldIndex := state.profileCursor
	if fieldIndex < 0 || fieldIndex >= len(fields) || fields[fieldIndex].Kind != agent.ProfileFieldChoice {
		return false
	}
	field := fields[fieldIndex]
	model.beginPendingAgentEdit(*profile)
	value := cycleString(profileFieldValue(*profile, field), field.Choices, direction)
	setProfileFieldValue(profile, field, value)
	delete(state.probes, profile.ID)
	delete(state.probing, profile.ID)
	state.pendingActive = profile.ID
	state.dirty = true
	state.status = "Modified"
	return true
}

func (model *Model) testSelectedProfileCommand() tea.Cmd {
	state := &model.configSettings
	index := model.selectedAgentProfileIndex()
	if model.profileProber == nil || index < 0 {
		return nil
	}
	profile := cloneConfigProfile(state.working.Profiles[index])
	generation := state.generation
	prober := model.profileProber
	if state.probing == nil {
		state.probing = map[agent.ProfileID]bool{}
	}
	if state.probing[profile.ID] {
		state.status = "Checking connection…"
		return nil
	}
	state.probing[profile.ID] = true
	attempt := model.nextConfigProbeAttempt(profile.ID)
	delete(state.probes, profile.ID)
	state.connectionTitle = ""
	state.connectionError = ""
	if profile.ID != state.working.Fix.Profile {
		state.pendingActive = profile.ID
	}
	state.status = "Checking connection…"
	return func() tea.Msg {
		return configProbeMsg{generation: generation, attempt: attempt, profile: profile.ID, definition: profile, result: prober.Probe(context.Background(), profile)}
	}
}

func (model *Model) nextConfigProbeAttempt(profile agent.ProfileID) uint64 {
	state := &model.configSettings
	if state.probeAttempts == nil {
		state.probeAttempts = map[agent.ProfileID]uint64{}
	}
	state.probeSequence++
	state.probeAttempts[profile] = state.probeSequence
	return state.probeSequence
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
	state := &model.configSettings
	if !state.open || message.generation != state.generation ||
		message.attempt == 0 || state.probeAttempts[message.profile] != message.attempt ||
		!probeDefinitionCurrent(state.working.Profiles, message.definition) {
		return nil
	}
	if state.probes == nil {
		state.probes = map[agent.ProfileID]agent.ProbeResult{}
	}
	delete(state.probing, message.profile)
	state.probes[message.profile] = message.result
	if !state.profileEditing || message.definition.Runtime != state.providerRuntime {
		return nil
	}
	ready := message.result.State == agent.ProbeReady && message.result.Capabilities.Isolation.EligibleForMutation()
	if !ready {
		state.status = "Connection failed"
		state.connectionTitle = "CONNECTION FAILED"
		state.connectionError = nonemptySetting(cleanAgentText(message.result.Diagnostic), agentProbeReadiness(message.result))
		model.rollbackPendingAgentEdit()
		return nil
	}
	state.status = "Connected"
	state.connectionTitle = ""
	state.connectionError = ""
	shouldSave := state.pendingOriginal != nil && state.pendingOriginal.ID == message.profile
	if state.pendingActive == message.profile && state.working.Fix.Profile != message.profile {
		model.beginPendingAgentChange()
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
	if !shouldSave {
		return nil
	}
	state.status = "Connected · saving…"
	return model.saveConfigSettings()
}

func (model *Model) rollbackPendingAgentEdit() {
	state := &model.configSettings
	if !state.pendingChange {
		state.pendingActive = ""
		return
	}
	if state.pendingOriginal != nil {
		original := cloneConfigProfile(*state.pendingOriginal)
		for index := range state.working.Profiles {
			if state.working.Profiles[index].ID == original.ID {
				state.working.Profiles[index] = original
				break
			}
		}
		delete(state.probing, original.ID)
		delete(state.probes, original.ID)
		delete(state.probeAttempts, original.ID)
	}
	if state.pendingFix != nil {
		state.working.Fix = cloneConfigFix(*state.pendingFix)
	}
	state.dirty = state.pendingWasDirty
	state.defaultChanged = state.pendingDefault
	state.pendingChange = false
	state.pendingOriginal = nil
	state.pendingFix = nil
	state.pendingWasDirty = false
	state.pendingDefault = false
	state.pendingActive = ""
}

func (model *Model) beginPendingAgentChange() {
	state := &model.configSettings
	if state.pendingChange {
		return
	}
	state.pendingChange = true
	state.pendingWasDirty = state.dirty
	state.pendingDefault = state.defaultChanged
}

func (model *Model) beginPendingAgentEdit(profile agent.Profile) {
	state := &model.configSettings
	model.beginPendingAgentChange()
	if state.pendingOriginal == nil {
		original := cloneConfigProfile(profile)
		state.pendingOriginal = &original
	}
}

func selectedAgentOption[T ~string](options []agent.Option[T], selected T) T {
	if len(options) == 0 {
		return ""
	}
	for _, option := range options {
		if option.ID == selected {
			return selected
		}
	}
	for _, option := range options {
		if option.Default {
			return option.ID
		}
	}
	return options[0].ID
}

func readyAgentProbe(probes map[agent.ProfileID]agent.ProbeResult, profile agent.ProfileID) (agent.ProbeResult, bool) {
	probe, ok := probes[profile]
	return probe, ok && probe.State == agent.ProbeReady
}

func reconcileFixAgentOptions(value *appconfig.FixDefaults, probe agent.ProbeResult, ready bool) {
	if !ready {
		probe = agent.ProbeResult{}
	}
	value.Model = selectedAgentOption(probe.Capabilities.Models, value.Model)
	value.Effort = selectedAgentOption(probe.Capabilities.Efforts, value.Effort)
}

func (model *Model) configTextEditable() bool {
	cursor := model.configSettings.cursor
	if model.configSettings.profileEditing {
		return true
	}
	if model.configSettings.kind == configDelivery {
		field := deliverySettingField(model.configSettings.working.Delivery, cursor)
		return field == deliverySettingRemote || field == deliverySettingBase || field == deliverySettingBranch ||
			field == deliverySettingCommitTitle || field == deliverySettingCommitBody || field == deliverySettingPRTitle || field == deliverySettingPRBody
	}
	return false
}

func (model *Model) beginConfigText() {
	state := &model.configSettings
	value := ""
	if state.profileEditing {
		index := model.selectedAgentProfileIndex()
		if index < 0 {
			return
		}
		profile := state.working.Profiles[index]
		fields := model.profileEditorFields(profile)
		if state.profileCursor < 0 || state.profileCursor >= len(fields) {
			state.status = "agent connection setting is unavailable"
			return
		}
		value = profileFieldValue(profile, fields[state.profileCursor])
		state.editField = state.profileCursor
	} else {
		field := deliverySettingField(state.working.Delivery, state.cursor)
		switch field {
		case deliverySettingRemote:
			value = state.working.Delivery.Remote
		case deliverySettingBase:
			value = state.working.Delivery.BaseBranch
		case deliverySettingBranch:
			value = state.working.Delivery.BranchTemplate
		case deliverySettingCommitTitle:
			value = state.working.Delivery.CommitTitleTemplate
		case deliverySettingCommitBody:
			value = state.working.Delivery.CommitBodyTemplate
		case deliverySettingPRTitle:
			value = state.working.Delivery.PullRequestTitleTemplate
		case deliverySettingPRBody:
			value = state.working.Delivery.PullRequestBodyTemplate
		}
		state.editField = field
	}
	state.input.SetValue(value)
	state.input.CursorEnd()
	state.input.Focus()
	style.ApplyTextInputStyle(&state.input, true)
	state.editing = true
}

func (model *Model) commitConfigText() error {
	state := &model.configSettings
	value := strings.TrimSpace(state.input.Value())
	if strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("value contains invalid control characters")
	}
	if state.profileEditing {
		index := model.selectedAgentProfileIndex()
		if index < 0 {
			return errors.New("agent connection is unavailable")
		}
		profile := &state.working.Profiles[index]
		fields := model.profileEditorFields(*profile)
		if state.editField < 0 || state.editField >= len(fields) {
			return errors.New("agent connection setting is unavailable")
		}
		field := fields[state.editField]
		if err := validateProfileFieldValue(field, value); err != nil {
			return err
		}
		candidate := cloneConfigProfile(*profile)
		setProfileFieldValue(&candidate, field, value)
		if model.profileCatalog != nil {
			if err := model.profileCatalog.ValidateProfile(candidate); err != nil {
				return err
			}
		}
		model.beginPendingAgentEdit(*profile)
		*profile = candidate
		delete(state.probes, profile.ID)
		delete(state.probing, profile.ID)
		state.pendingActive = profile.ID
	} else {
		if value == "" && state.editField != deliverySettingBase {
			return errors.New("this value cannot be empty")
		}
		switch state.editField {
		case deliverySettingRemote:
			state.working.Delivery.Remote = value
		case deliverySettingBase:
			state.working.Delivery.BaseBranch = value
		case deliverySettingBranch:
			state.working.Delivery.BranchTemplate = value
		case deliverySettingCommitTitle:
			state.working.Delivery.CommitTitleTemplate = value
		case deliverySettingCommitBody:
			state.working.Delivery.CommitBodyTemplate = value
		case deliverySettingPRTitle:
			state.working.Delivery.PullRequestTitleTemplate = value
		case deliverySettingPRBody:
			state.working.Delivery.PullRequestBodyTemplate = value
		}
	}
	state.dirty = true
	state.status = "Modified"
	return nil
}

func (model Model) configSettingsRows() int {
	state := model.configSettings
	switch state.kind {
	case configAgents:
		return len(agentProviderChoices)
	case configFix:
		return fixSettingsMetricStart + len(fixSettingsMetrics())
	case configConcurrency:
		return 7
	case configDelivery:
		return len(deliverySettingFields(state.working.Delivery))
	default:
		return 0
	}
}

func (model Model) configSettingsView() string {
	state := model.configSettings
	width := min(72, max(38, model.width-4))
	title := model.configSettingsTitle()
	if state.loading {
		return style.Popup(style.Heading(title), []string{"Loading configuration…"}, "Esc back", width)
	}
	lines, selectedStart, selectedEnd := model.configSettingsContent(width - 4)
	if len(lines) == 0 {
		lines = []string{"No configured items."}
	}
	bodyWidth := width - 4
	lines, windowStart := scrollModalLineWindow(lines, selectedStart, selectedEnd, max(1, model.modalBodyHeight()-2))
	if state.choiceOpen {
		lines = model.overlayConfigChoiceMenu(lines, bodyWidth, selectedStart-windowStart)
	}
	if state.kind != configAgents {
		lines = append(lines, style.DisabledOption(truncate(model.configSettingsStatusLine(), width), width))
	}
	footer := model.configSettingsFooter()
	if state.editing {
		input := state.input
		style.ApplyTextInputStyle(&input, true)
		lines = append(lines, "Edit: "+style.InputField(input.View(), max(1, width-10)))
	}
	return style.Popup(style.Heading(title), lines, footer, width)
}

func (model Model) configSettingsFullScreen() string {
	state := model.configSettings
	title := model.configSettingsTitle()
	footer := model.configSettingsFooter()
	lines := []string{fixSurfaceLine(title, model.width, style.SurfaceHeader, style.TextPrimary)}
	if state.loading {
		lines = append(lines, fixSurfaceLine("Loading configuration…", model.width, style.SurfaceModal, style.TextMuted))
	} else {
		body, selectedStart, selectedEnd := model.configSettingsContent(model.width)
		reserved := 3 // header, status, footer
		if state.editing {
			reserved++ // active editor row
		}
		body, windowStart := scrollModalLineWindow(body, selectedStart, selectedEnd, max(1, model.height-reserved))
		if state.choiceOpen {
			body = model.overlayConfigChoiceMenu(body, model.width, selectedStart-windowStart)
		}
		for _, line := range body {
			lines = append(lines, fixSurfaceLineANSI(line, model.width, style.SurfaceModal))
		}
		if state.kind != configAgents {
			lines = append(lines, fixSurfaceLine(model.configSettingsStatusLine(), model.width, style.SurfaceModal, style.TextMuted))
		}
	}
	if state.editing {
		input := state.input
		style.ApplyTextInputStyle(&input, true)
		lines = append(lines, fixSurfaceLineANSI("Edit: "+style.InputField(input.View(), max(1, model.width-6)), model.width, style.SurfaceModal))
	}
	for len(lines) < model.height-1 {
		lines = append(lines, fixSurfaceLine("", model.width, style.SurfaceModal, style.TextPrimary))
	}
	lines = append(lines, fixSurfaceLine(footer, model.width, style.SurfaceFooter, style.TextMuted))
	return joinScreenLines(lines[:model.height])
}

func (model Model) configSettingsTitle() string {
	state := model.configSettings
	if state.kind == configAgents && state.profileEditing {
		for _, choice := range agentProviderChoices {
			if choice.Runtime == state.providerRuntime {
				return strings.ToUpper(choice.Label)
			}
		}
	}
	return map[configSettingsKind]string{
		configAgents: "AGENTS", configFix: "FIX DEFAULTS", configConcurrency: "CONCURRENCY & RETENTION",
		configDelivery: "GIT & PULL REQUESTS",
	}[state.kind]
}

func (model Model) configSettingsFooter() string {
	state := model.configSettings
	compact := responsiveTier(model.width, model.height) == ResponsiveCompact
	if state.choiceOpen {
		return "Enter select"
	}
	if state.editing {
		return "Enter apply · Esc cancel"
	}
	if state.kind == configAgents {
		if state.profileEditing {
			if model.profileFieldCount() > 0 {
				return "Enter edit"
			}
			return ""
		}
		return "Enter select"
	}
	readonly := false
	edit := false
	metrics := state.kind == configFix && state.cursor >= fixSettingsMetricStart
	switch state.kind {
	case configFix:
		edit = state.cursor == fixSettingsPromptRow
	case configDelivery:
		edit = model.configTextEditable()
	}
	if compact {
		switch {
		case readonly:
			return "read-only · Esc"
		case edit:
			return "Enter edit · Esc"
		case metrics:
			return "Space select · Esc"
		default:
			return "Esc"
		}
	}
	switch {
	case readonly:
		return "Read-only in this release · Esc back"
	case edit:
		return "Enter edit · Esc back"
	case metrics:
		return "Space select · Esc back"
	default:
		return "Esc back"
	}
}

func (model Model) configSettingsStatusLine() string {
	return model.configSettings.status
}

func firstPromptLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	return value
}

func agentProbeReadiness(result agent.ProbeResult) string {
	if result.State == agent.ProbeReady && result.Capabilities.Isolation.EligibleForMutation() {
		return "RUNNABLE · ready"
	}
	state := string(result.State)
	if result.State == agent.ProbeReady {
		state = "confinement failed"
	}
	if state == "" {
		state = "unknown"
	}
	return "NOT RUNNABLE · " + state
}

func (model Model) configSettingsContent(width int) ([]string, int, int) {
	if model.configSettings.kind == configAgents {
		return model.agentSettingsLines(width)
	}
	lines := model.configSettingsLines(width)
	selected := model.configSettings.cursor
	if model.configSettings.kind == configFix && selected >= fixSettingsMetricStart {
		// Six ordinary rows, one heading, then three metric cells per line.
		selected = 7 + (selected-fixSettingsMetricStart)/3
	}
	selected = min(max(0, selected), max(0, len(lines)-1))
	return lines, selected, selected
}

func scrollModalLineRange(lines []string, selectedStart, selectedEnd, limit int) []string {
	window, _ := scrollModalLineWindow(lines, selectedStart, selectedEnd, limit)
	return window
}

func scrollModalLineWindow(lines []string, selectedStart, selectedEnd, limit int) ([]string, int) {
	if len(lines) <= limit {
		return lines, 0
	}
	limit = max(1, limit)
	selectedStart = min(len(lines)-1, max(0, selectedStart))
	selectedEnd = min(len(lines)-1, max(selectedStart, selectedEnd))
	if selectedEnd-selectedStart+1 >= limit {
		return lines[selectedStart : selectedStart+limit], selectedStart
	}
	start := max(0, selectedEnd-limit+1)
	if start > selectedStart {
		start = selectedStart
	}
	if start+limit > len(lines) {
		start = max(0, len(lines)-limit)
	}
	return lines[start : start+limit], start
}

func (model Model) overlayConfigChoiceMenu(lines []string, width, anchorRow int) []string {
	if len(lines) == 0 || anchorRow < 0 || anchorRow >= len(lines) {
		return lines
	}
	const fieldStart = 24
	if width <= 0 {
		return lines
	}
	menu := formChoiceMenu(model.configChoices(model.configSettings.cursor), model.configSettings.choiceCursor, false, width, len(lines), 1)
	if len(menu) == 0 {
		return lines
	}
	menuTop := min(anchorRow, max(0, len(lines)-len(menu)))
	menuLeft := min(fieldStart, max(0, width-lipgloss.Width(menu[0])))
	return overlayFixLines(lines, menu, menuLeft, menuTop, width, len(lines))
}

func wrappedDisabledConfigLines(value string, width int) []string {
	value = cleanAgentText(value)
	wrapped := wrapText(value, max(1, width), "", "")
	lines := make([]string, 0, len(wrapped))
	for _, logicalLine := range wrapped {
		for _, line := range strings.Split(ansi.Hardwrap(logicalLine, max(1, width), false), "\n") {
			lines = append(lines, style.DisabledOption(line, width))
		}
	}
	return lines
}

func (model Model) agentSettingsLines(width int) ([]string, int, int) {
	state := model.configSettings
	if state.profileEditing {
		return model.agentConnectionLines(width)
	}

	activeRuntime := runtimeForProfile(state.working.Profiles, state.working.Fix.Profile)
	labelWidth := 0
	for _, choice := range agentProviderChoices {
		labelWidth = max(labelWidth, len(choice.Label))
	}
	lines := make([]string, 0, len(agentProviderChoices))
	for index, choice := range agentProviderChoices {
		available, checking := model.agentProviderAvailability(choice)
		status := "available"
		switch {
		case choice.Runtime == activeRuntime && available:
			status = "[ACTIVE]"
		case choice.Runtime == activeRuntime:
			status = "[ACTIVE] · unavailable"
		case checking:
			status = "checking…"
		case !available:
			status = "not available"
		}
		row := fmt.Sprintf("%-*s  %s", labelWidth, choice.Label, status)
		background := style.SurfaceScreen
		if index == state.cursor {
			background = style.SurfaceSelected
		}
		foreground := style.TextPrimary
		if !available {
			foreground = style.TextMuted
		}
		lines = append(lines, lipgloss.NewStyle().Width(width).Background(background).Foreground(foreground).
			Bold(choice.Runtime == activeRuntime).Render(truncate(row, width)))
	}
	selected := min(max(0, state.cursor), len(lines)-1)
	return lines, selected, selected
}

func (model Model) agentProviderAvailability(choice agentProviderChoice) (available, checking bool) {
	if model.profileCatalog == nil {
		return false, false
	}
	if _, err := model.profileCatalog.Descriptor(choice.Runtime); err != nil {
		return false, false
	}
	if profileCountForRuntime(model.configSettings.working.Profiles, choice.Runtime) != 1 {
		return false, false
	}
	index := profileIndexForRuntime(model.configSettings.working.Profiles, choice.Runtime, model.configSettings.working.Fix.Profile)
	profile := model.configSettings.working.Profiles[index]
	if model.configSettings.probing[profile.ID] {
		return true, true
	}
	if probe, ok := model.configSettings.probes[profile.ID]; ok && probe.State == agent.ProbeUnavailable {
		return false, false
	}
	return true, false
}

func (model Model) agentConnectionLines(width int) ([]string, int, int) {
	state := model.configSettings
	choice := agentProviderChoice{Runtime: state.providerRuntime, Label: string(state.providerRuntime)}
	for _, candidate := range agentProviderChoices {
		if candidate.Runtime == state.providerRuntime {
			choice = candidate
			break
		}
	}
	descriptor, descriptorErr := model.profileDescriptor(agent.Profile{Runtime: choice.Runtime})
	if descriptorErr != nil {
		message := nonemptySetting(choice.Unavailable, "This agent adapter is not available in this Slopwatch build.")
		lines := wrappedDisabledConfigLines(message, width)
		return lines, 0, max(0, len(lines)-1)
	}

	count := profileCountForRuntime(state.working.Profiles, choice.Runtime)
	if count == 0 {
		lines := agentConnectionErrorLines("No connection configured. Add exactly one profile in the preferences file, then reopen Settings.", width)
		return lines, 0, max(0, len(lines)-1)
	}
	if count > 1 {
		lines := agentConnectionErrorLines("Multiple connections configured. This release supports one account per provider. In the preferences file, keep one profile and reopen Settings.", width)
		return lines, 0, max(0, len(lines)-1)
	}
	lines := make([]string, 0, 12)
	if descriptor.ConnectionInstructions != "" {
		lines = append(lines, wrappedDisabledConfigLines(descriptor.ConnectionInstructions, width)...)
	}
	if descriptor.DocumentationURL != "" {
		lines = append(lines, wrappedDisabledConfigLines(descriptor.DocumentationURL, width)...)
	}
	index := model.selectedAgentProfileIndex()
	profile := state.working.Profiles[index]
	fields := model.profileEditorFields(profile)
	for fieldIndex, field := range fields {
		value := profileFieldValue(profile, field)
		if value == "" {
			value = field.Default
		}
		lines = append(lines, style.FormFieldRow("  "+field.Label, value, width, 24, fieldIndex == state.profileCursor, field.Kind == agent.ProfileFieldChoice))
		if fieldIndex == state.profileCursor {
			if field.Description != "" {
				lines = append(lines, wrappedDisabledConfigLines(field.Description, width)...)
			}
		}
	}
	statusStart := len(lines)
	lines = append(lines, "")
	if state.saving {
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentInfo).Render("SAVING ACTIVE CONNECTION…"))
		return lines, statusStart, len(lines) - 1
	}
	if state.connectionError != "" {
		title := nonemptySetting(state.connectionTitle, "CONNECTION FAILED")
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentCritical).Render(title))
		lines = append(lines, agentConnectionErrorLines(state.connectionError, width)...)
		return lines, statusStart, len(lines) - 1
	}
	if state.probing[profile.ID] {
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentInfo).Render("CHECKING CONNECTION…"))
		return lines, statusStart, len(lines) - 1
	}
	probe, probed := state.probes[profile.ID]
	if !probed {
		lines = append(lines, lipgloss.NewStyle().Width(width).Foreground(style.TextMuted).Render("Connection has not been checked."))
		return lines, statusStart, len(lines) - 1
	}
	if probe.State == agent.ProbeReady && probe.Capabilities.Isolation.EligibleForMutation() {
		detail := nonemptySetting(cleanAgentText(probe.Authentication.Label), "Connection ready")
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentPositive).Render("CONNECTED"))
		lines = append(lines, wrappedDisabledConfigLines(detail, width)...)
		return lines, statusStart, len(lines) - 1
	}
	lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentCritical).Render("CONNECTION FAILED"))
	diagnostic := nonemptySetting(cleanAgentText(probe.Diagnostic), agentProbeReadiness(probe))
	lines = append(lines, agentConnectionErrorLines(diagnostic, width)...)
	return lines, statusStart, len(lines) - 1
}

func agentConnectionErrorLines(value string, width int) []string {
	wrapped := wrapText(cleanAgentText(value), max(1, width), "", "")
	lines := make([]string, 0, len(wrapped))
	for _, logicalLine := range wrapped {
		for _, line := range strings.Split(ansi.Hardwrap(logicalLine, max(1, width), false), "\n") {
			lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Foreground(style.AccentCritical).Render(line))
		}
	}
	return lines
}

func (model Model) configSettingsLines(width int) []string {
	state := model.configSettings
	option := func(index int, label, value string) string {
		prefix := "  "
		if index == state.cursor {
			prefix = "› "
		}
		return style.FormFieldRow(prefix+label, value, width, 24, index == state.cursor, model.configChoiceField(index))
	}
	switch state.kind {
	case configAgents:
		lines, _, _ := model.agentSettingsLines(width)
		return lines
	case configFix:
		profile := nonemptySetting(string(state.working.Fix.Profile), "not selected")
		for _, configured := range state.working.Profiles {
			if configured.ID == state.working.Fix.Profile {
				profile = agentProfileChoiceLabel(configured) + " [" + string(configured.ID) + "]"
				break
			}
		}
		modelName := nonemptySetting(string(state.working.Fix.Model), "runtime default")
		effort := nonemptySetting(string(state.working.Fix.Effort), "runtime default")
		lines := []string{
			option(0, "Target score", fmt.Sprintf("%.0f", state.working.Fix.TargetScore)),
			option(1, "May edit", changeScopeLabel(state.working.Fix.ChangeScope)), option(2, "Agent profile", profile),
			option(3, "Model", modelName), option(4, "Effort", effort),
			option(fixSettingsPromptRow, "Agent prompt", firstPromptLine(state.working.Fix.PromptTemplate)),
		}
		lines = append(lines, lipgloss.NewStyle().Width(width).Bold(true).Background(style.SurfaceScreen).Foreground(style.TextPrimary).Render("Focus metrics"))
		lines = append(lines, fixSettingsMetricGrid(state, width)...)
		return lines
	case configConcurrency:
		lines := []string{
			option(0, "Running agents", fmt.Sprint(state.working.Concurrency.MaxAgents)),
			option(1, "Running verifiers", fmt.Sprint(state.working.Concurrency.MaxVerifiers)),
			option(2, "Actors per job", fmt.Sprint(state.working.Concurrency.MaxActorsPerJob)),
			option(3, "Candidate preview bytes", fmt.Sprint(state.working.Concurrency.MaxCandidatePreviewBytes)),
			option(4, "Candidate preview lines", fmt.Sprint(state.working.Concurrency.MaxCandidatePreviewLines)),
		}
		return lines
	case configDelivery:
		branchTemplate := strings.TrimSpace(state.working.Delivery.BranchTemplate)
		branchValue := branchTemplate + " → " + appconfig.PreviewBranchTemplate(branchTemplate)
		values := map[int]struct{ label, value string }{
			deliverySettingWorkspace:   {"Work in", workspaceModeLabel(state.working.Delivery.DefaultPlan.Workspace)},
			deliverySettingGit:         {"Git", gitModeLabel(state.working.Delivery.DefaultPlan.Git)},
			deliverySettingPublish:     {"Publish", publishModeLabel(state.working.Delivery.DefaultPlan.Publish)},
			deliverySettingRemote:      {"Remote", state.working.Delivery.Remote},
			deliverySettingBase:        {"Base branch", nonemptySetting(state.working.Delivery.BaseBranch, "not set")},
			deliverySettingBranch:      {"Branch template", branchValue},
			deliverySettingPRState:     {"Initial PR state", map[bool]string{true: "Draft", false: "Ready for review"}[state.working.Delivery.DraftPullRequests]},
			deliverySettingCommitTitle: {"Commit title", nonemptySetting(state.working.Delivery.CommitTitleTemplate, "default")},
			deliverySettingCommitBody:  {"Commit body", nonemptySetting(state.working.Delivery.CommitBodyTemplate, "default")},
			deliverySettingPRTitle:     {"PR title", nonemptySetting(state.working.Delivery.PullRequestTitleTemplate, "commit title")},
			deliverySettingPRBody:      {"PR body", nonemptySetting(state.working.Delivery.PullRequestBodyTemplate, "commit body")},
		}
		fields := deliverySettingFields(state.working.Delivery)
		lines := make([]string, 0, len(fields))
		for row, field := range fields {
			item := values[field]
			lines = append(lines, option(row, item.label, item.value))
		}
		return lines
	}
	return nil
}

func fixSettingsMetricGrid(state configSettingsState, width int) []string {
	type metricChoice struct {
		label    string
		selected bool
		cursor   int
	}
	choices := make([]metricChoice, 0, len(fixSettingsMetrics()))
	for index, metric := range fixSettingsMetrics() {
		choices = append(choices, metricChoice{
			label: strings.ToUpper(string(metric)), selected: hasMetric(state.working.Fix.Focus, metric),
			cursor: fixSettingsMetricStart + index,
		})
	}
	const columns = 3
	cellWidth := max(1, width/columns)
	lines := make([]string, 0, (len(choices)+columns-1)/columns)
	for start := 0; start < len(choices); start += columns {
		line := ""
		for column := 0; column < columns; column++ {
			index := start + column
			currentWidth := cellWidth
			if column == columns-1 {
				currentWidth = max(1, width-cellWidth*(columns-1))
			}
			text := ""
			selected := false
			if index < len(choices) {
				mark := " "
				if choices[index].selected {
					mark = "x"
				}
				text = fmt.Sprintf("[%s]", mark)
				selected = choices[index].cursor == state.cursor
				line += style.ToggleOption(text, choices[index].label, selected, false, currentWidth)
				continue
			}
			line += lipgloss.NewStyle().Width(currentWidth).Background(style.SurfaceScreen).Render(text)
		}
		lines = append(lines, line)
	}
	return lines
}

func fixSettingsMetrics() []fix.MetricID {
	metrics := make([]fix.MetricID, 1, len(scoring.Metrics())+1)
	metrics[0] = fix.MetricScore
	for _, metric := range scoring.Metrics() {
		metrics = append(metrics, fix.MetricID(metric.ID))
	}
	return metrics
}

func agentProfileChoiceLabel(profile agent.Profile) string {
	switch profile.Runtime {
	case "codex-cli":
		return "Codex"
	case "openai-responses":
		return "OpenAI API"
	default:
		return fmt.Sprintf("%s · %s", profile.Label, profile.Runtime)
	}
}

func cloneConfigResolved(value appconfig.Resolved) appconfig.Resolved {
	result := value
	result.Origins = make(map[string]appconfig.Origin, len(value.Origins))
	for key, origin := range value.Origins {
		result.Origins[key] = origin
	}
	result.Fix = cloneConfigFix(value.Fix)
	result.Profiles = cloneConfigProfiles(value.Profiles)
	return result
}

func cloneConfigFix(value appconfig.FixDefaults) appconfig.FixDefaults {
	value.Focus = append([]fix.MetricID(nil), value.Focus...)
	return value
}

func cloneConfigProfiles(values []agent.Profile) []agent.Profile {
	result := make([]agent.Profile, len(values))
	for index, profile := range values {
		result[index] = cloneConfigProfile(profile)
	}
	return result
}

func cloneConfigProfile(value agent.Profile) agent.Profile {
	result := value
	result.Options = make(map[string]string, len(value.Options))
	for key, option := range value.Options {
		result.Options[key] = option
	}
	return result
}

func modelIDs(result agent.ProbeResult) []agent.ModelID {
	values := []agent.ModelID{""}
	for _, option := range result.Capabilities.Models {
		values = append(values, option.ID)
	}
	return values
}

func effortIDs(result agent.ProbeResult) []agent.EffortID {
	values := []agent.EffortID{""}
	for _, option := range result.Capabilities.Efforts {
		values = append(values, option.ID)
	}
	return values
}

func cycleString(current string, values []string, direction int) string {
	return cycleTyped(current, values, direction)
}

func cycleTyped[T comparable](current T, values []T, direction int) T {
	if len(values) == 0 {
		return current
	}
	index := 0
	for candidate, value := range values {
		if value == current {
			index = candidate
			break
		}
	}
	index = (index + direction%len(values) + len(values)) % len(values)
	return values[index]
}

func toggleMetric(values []fix.MetricID, id fix.MetricID) []fix.MetricID {
	if hasMetric(values, id) {
		result := make([]fix.MetricID, 0, len(values)-1)
		for _, value := range values {
			if value != id {
				result = append(result, value)
			}
		}
		return result
	}
	result := append([]fix.MetricID(nil), values...)
	result = append(result, id)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func hasMetric(values []fix.MetricID, id fix.MetricID) bool {
	for _, value := range values {
		if value == id {
			return true
		}
	}
	return false
}

func nonemptySetting(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
