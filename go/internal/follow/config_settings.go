package follow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
	"github.com/blater/slopwatch/internal/validation"
)

type configSettingsKind string

const (
	configAgents      configSettingsKind = "agents"
	configFix         configSettingsKind = "fix"
	configConcurrency configSettingsKind = "concurrency"
	configValidation  configSettingsKind = "validation"
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
	generation      uint64
	saved           appconfig.Saved
	restartRequired bool
	err             error
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
	prompt := textarea.New()
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
	state.prompt = textarea.New()
	state.prompt.SetValue(state.working.Fix.PromptTemplate)
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
			state.status = "Save conflict: settings changed elsewhere; reload with r"
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
	if message.restartRequired {
		state.status = "Saved for next start · restart Slopwatch before preparing another Fix"
	}
	if state.closeAfterSave {
		state.closeAfterSave = false
		if model.hasOverlay(OverlaySettingsDirty) {
			model.overlays.Pop()
		}
		return model.closeConfigSettingsNow()
	}
	return model.probeProfilesCommand()
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
			return model, nil
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
	switch key.String() {
	case "esc", "escape", "q":
		return model, model.closeConfigSettings()
	case "up", "k":
		state.cursor = max(0, state.cursor-1)
	case "down", "j":
		state.cursor = min(max(0, model.configSettingsRows()-1), state.cursor+1)
	case "left", "h", "-":
		model.adjustConfigSetting(-1)
	case "right", "l", "+", "=", " ":
		model.adjustConfigSetting(1)
	case "enter":
		if state.kind == configFix && state.cursor == 6 {
			state.prompt.SetValue(state.working.Fix.PromptTemplate)
			state.promptOriginal = state.working.Fix.PromptTemplate
			state.promptError = ""
			state.prompt.Focus()
			model.overlays.Push(OverlayPromptEditor, OverlayCaller{MainView: model.mainView, Overlay: OverlayConfigSettings, Selected: model.mainSelection()})
		} else if state.kind == configAgents && len(state.working.Profiles) > 0 {
			state.profileEditing = true
			state.profileCursor = 0
		} else if model.configTextEditable() {
			model.beginConfigText()
		} else {
			model.adjustConfigSetting(1)
		}
	case "r":
		return model, model.reloadConfigSettings()
	case "s", "ctrl+s":
		return model, model.saveConfigSettings()
	}
	return model, nil
}

func (model *Model) handleMasterPromptKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := &model.configSettings
	switch key.String() {
	case "ctrl+s":
		value := strings.TrimSpace(state.prompt.Value())
		if value == "" {
			state.promptError = "Agent prompt cannot be empty"
			return model, nil
		}
		state.promptError = ""
		state.working.Fix.PromptTemplate = value
		state.promptOriginal = value
		state.prompt.Blur()
		state.dirty = true
		state.status = "Modified"
		model.overlays.Pop()
		return model, nil
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
		return model, command
	}
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
		if !model.hasOverlay(OverlayFixForm) || !model.fixDialog.hasDraft && model.fixDialog.target == "" {
			return nil
		}
		model.fixGeneration++
		model.fixDialog.generation = model.fixGeneration
		model.fixDialog.loading = true
		model.fixDialog.submitBlocked = false
		model.fixDialog.errorText = ""
		model.fixDialog.statusText = "Rechecking settings, runtime, validation, and workspace readiness…"
		var profile *agent.ProfileID
		if model.fixDialog.hasDraft && model.fixDialog.draft.Profile.ID != "" {
			selected := model.fixDialog.draft.Profile.ID
			profile = &selected
		}
		return model.prepareFixCommand(model.fixDialog.target, profile, model.fixDialog.generation)
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

func (model *Model) reloadConfigSettings() tea.Cmd {
	state := &model.configSettings
	state.generation++
	state.loading = true
	state.saving = false
	state.status = ""
	if model.configStore == nil {
		state.loading = false
		state.status = "Feature settings unavailable: no configuration service"
		return nil
	}
	generation, store, workspace := state.generation, model.configStore, model.configWorkspace
	return func() tea.Msg {
		if editor, ok := store.(appconfig.Editor); ok {
			editable, err := editor.LoadEditable(context.Background(), workspace)
			return configResolvedMsg{generation: generation, resolved: editable.Resolved, diagnostics: editable.Diagnostics, err: err}
		}
		resolved, err := store.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
		return configResolvedMsg{generation: generation, resolved: resolved, err: err}
	}
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
	restartRequired := kind == configValidation && state.working.ValidationWorkspace != state.resolved.ValidationWorkspace
	store, workspace, fixService := model.configStore, model.configWorkspace, model.fixService
	return func() tea.Msg {
		saved, err := store.Save(context.Background(), workspace, appconfig.ScopeUser, patch, revision)
		if err == nil && kind == configConcurrency && fixService != nil {
			err = fixService.Reconfigure(context.Background(), fixapp.RuntimeLimitsFromConcurrency(saved.Resolved.Concurrency))
		}
		return configSavedMsg{generation: generation, saved: saved, restartRequired: restartRequired, err: err}
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
	case configValidation:
		fixDefaults := cloneConfigFix(value.Fix)
		plans := cloneConfigValidation(value.Validation)
		workspace := value.ValidationWorkspace
		return appconfig.Patch{Fix: &fixDefaults, Validation: &plans, ValidationWorkspace: &workspace}
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
			value.Concurrency.MaxRetainedJobs <= 0 || value.Concurrency.MaxTranscriptBytes <= 0 || value.Concurrency.MaxActorsPerJob <= 0 || value.Concurrency.MaxCandidatePreviewBytes <= 0 || value.Concurrency.MaxCandidatePreviewLines <= 0 {
			return errors.New("all concurrency and retention limits must be positive")
		}
	case configValidation:
		workspace := value.ValidationWorkspace
		if workspace.MaxFiles <= 0 || workspace.MaxDirectories <= 0 || workspace.MaxPathBytes <= 0 || workspace.MaxFileBytes <= 0 || workspace.MaxTotalBytes <= 0 {
			return errors.New("all validation workspace limits must be positive")
		}
		if workspace.MaxFileBytes > workspace.MaxTotalBytes {
			return errors.New("validation file bytes cannot exceed total workspace bytes")
		}
		if workspace.ContainerPIDs <= 0 || workspace.ContainerMemoryBytes <= 0 || workspace.ContainerCPUMillis <= 0 || workspace.ContainerTemporaryBytes <= 0 || workspace.ContainerWorkspaceBytes <= 0 || workspace.ContainerNofileLimit <= 0 || workspace.ContainerGeneratedFileBytes <= 0 || workspace.ContainerStopTimeout <= 0 || workspace.ContainerControlTimeout <= 0 || workspace.ContainerSentinelTimeout <= 0 || workspace.ContainerCrashProbeTimeout <= 0 {
			return errors.New("all validation container limits must be positive")
		}
		if workspace.ContainerWorkspaceBytes <= workspace.MaxTotalBytes {
			return errors.New("container workspace bytes must exceed admitted total bytes")
		}
		if workspace.ContainerGeneratedFileBytes < workspace.MaxFileBytes {
			return errors.New("generated file bytes must cover admitted max file bytes")
		}
		if workspace.ContainerControlTimeout <= workspace.ContainerStopTimeout {
			return errors.New("Docker control timeout must exceed container stop timeout")
		}
		if value.Fix.ValidationPlan != "" {
			index := validationPlanIndex(value.Validation, value.Fix.ValidationPlan)
			if index < 0 {
				return fmt.Errorf("validation plan %q is unavailable", value.Fix.ValidationPlan)
			}
			if len(value.Validation[index].Checks) == 0 {
				return fmt.Errorf("validation plan %q has no configured checks", value.Fix.ValidationPlan)
			}
		}
	case configDelivery:
		if value.Delivery.DefaultMode == "" || value.Delivery.Remote == "" || value.Delivery.BranchTemplate == "" || value.Delivery.Publisher == "" {
			return errors.New("delivery mode, remote, branch template, and publisher are required")
		}
		if value.Delivery.CommitPolicy != "" && value.Delivery.CommitPolicy != "automatic" {
			return errors.New("unsupported commit policy")
		}
		if value.Delivery.CleanupPolicy != "" && value.Delivery.CleanupPolicy != "remove-worktree" {
			return errors.New("unsupported cleanup policy")
		}
		if value.Delivery.DefaultMode == fix.DeliveryModePullRequest && strings.TrimSpace(value.Delivery.BaseBranch) == "" {
			return errors.New("pull-request delivery requires an explicit base branch")
		}
		if value.Delivery.DefaultMode == fix.DeliveryModePullRequest && value.Delivery.RequireValidation && strings.TrimSpace(value.Fix.ValidationPlan) == "" {
			return errors.New("PR validation is required, but no validation plan is selected")
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

func (model *Model) adjustConfigSetting(direction int) {
	state := &model.configSettings
	if state.loading {
		return
	}
	changed := false
	switch state.kind {
	case configFix:
		changed = adjustFixSetting(&state.working, state.cursor, direction, state.probes)
	case configConcurrency:
		changed = adjustConcurrencySetting(&state.working.Concurrency, state.cursor, direction)
	case configValidation:
		changed = adjustValidationSetting(&state.working, state.cursor, direction)
	case configDelivery:
		changed = adjustDeliverySetting(&state.working.Delivery, state.cursor, direction)
	}
	if changed {
		state.dirty = true
		state.status = "Modified · s save · r reload"
	}
}

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
	case 5:
		probe, _ := readyAgentProbe(probes, value.Fix.Profile)
		options := delegationIDs(probe)
		value.Fix.Delegation = cycleTyped(value.Fix.Delegation, options, direction)
	case 6:
		// Edited in the multiline master-prompt editor.
		return false
	case 7:
		// SCORE is the goal for every fix; component metrics add emphasis.
		return false
	default:
		metrics := scoring.Metrics()
		index := cursor - 8
		if index < 0 || index >= len(metrics) {
			return false
		}
		id := fix.MetricID(metrics[index].ID)
		value.Fix.Focus = toggleMetric(value.Fix.Focus, id)
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
		value.MaxRetainedJobs = max(1, value.MaxRetainedJobs+direction*10)
	case 3:
		value.MaxTranscriptBytes = maxInt64(1, value.MaxTranscriptBytes+int64(direction)*256*1024)
	case 4:
		value.MaxActorsPerJob = max(1, value.MaxActorsPerJob+direction)
	case 5:
		value.MaxCandidatePreviewBytes = maxInt64(1, value.MaxCandidatePreviewBytes+int64(direction)*256*1024)
	case 6:
		value.MaxCandidatePreviewLines = max(1, value.MaxCandidatePreviewLines+direction*100)
	default:
		return false
	}
	return true
}

func adjustValidationSetting(value *appconfig.Resolved, cursor, direction int) bool {
	rows := validationSettingsRows(value.Validation)
	if cursor < 0 || cursor >= len(rows) {
		return false
	}
	row := rows[cursor]
	switch row.kind {
	case validationRowPlan:
		selected := value.Validation[row.plan].ID
		if value.Fix.ValidationPlan == selected && direction > 0 {
			value.Fix.ValidationPlan = ""
		} else {
			value.Fix.ValidationPlan = selected
		}
	case validationRowRequired:
		value.Validation[row.plan].Checks[row.check].Required = !value.Validation[row.plan].Checks[row.check].Required
	default:
		return false
	}
	return true
}

type validationRowKind uint8

const (
	validationRowWorkspaceFiles validationRowKind = iota
	validationRowWorkspaceDirectories
	validationRowWorkspacePathBytes
	validationRowWorkspaceFileBytes
	validationRowWorkspaceTotalBytes
	validationRowContainerPIDs
	validationRowContainerMemoryBytes
	validationRowContainerCPUMillis
	validationRowContainerTemporaryBytes
	validationRowContainerWorkspaceBytes
	validationRowContainerNofileLimit
	validationRowContainerGeneratedFileBytes
	validationRowContainerStopTimeout
	validationRowContainerControlTimeout
	validationRowContainerSentinelTimeout
	validationRowContainerCrashProbeTimeout
	validationRowPlan
	validationRowRequired
	validationRowCommand
	validationRowWorkingDirectory
	validationRowTimeout
	validationRowOutput
)

type validationSettingsRow struct {
	plan  int
	check int
	kind  validationRowKind
}

func validationWorkspaceRow(kind validationRowKind) bool {
	return kind >= validationRowWorkspaceFiles && kind <= validationRowContainerCrashProbeTimeout
}

func validationWorkspaceValue(value appconfig.ValidationWorkspace, kind validationRowKind) int64 {
	switch kind {
	case validationRowWorkspaceFiles:
		return value.MaxFiles
	case validationRowWorkspaceDirectories:
		return value.MaxDirectories
	case validationRowWorkspacePathBytes:
		return value.MaxPathBytes
	case validationRowWorkspaceFileBytes:
		return value.MaxFileBytes
	case validationRowWorkspaceTotalBytes:
		return value.MaxTotalBytes
	case validationRowContainerPIDs:
		return int64(value.ContainerPIDs)
	case validationRowContainerMemoryBytes:
		return value.ContainerMemoryBytes
	case validationRowContainerCPUMillis:
		return value.ContainerCPUMillis
	case validationRowContainerTemporaryBytes:
		return value.ContainerTemporaryBytes
	case validationRowContainerWorkspaceBytes:
		return value.ContainerWorkspaceBytes
	case validationRowContainerNofileLimit:
		return value.ContainerNofileLimit
	case validationRowContainerGeneratedFileBytes:
		return value.ContainerGeneratedFileBytes
	default:
		return 0
	}
}

func validationWorkspaceDurationRow(kind validationRowKind) bool {
	return kind >= validationRowContainerStopTimeout && kind <= validationRowContainerCrashProbeTimeout
}

func validationWorkspaceText(value appconfig.ValidationWorkspace, kind validationRowKind) string {
	switch kind {
	case validationRowContainerStopTimeout:
		return value.ContainerStopTimeout.String()
	case validationRowContainerControlTimeout:
		return value.ContainerControlTimeout.String()
	case validationRowContainerSentinelTimeout:
		return value.ContainerSentinelTimeout.String()
	case validationRowContainerCrashProbeTimeout:
		return value.ContainerCrashProbeTimeout.String()
	default:
		return strconv.FormatInt(validationWorkspaceValue(value, kind), 10)
	}
}

func setValidationWorkspaceDuration(value *appconfig.ValidationWorkspace, kind validationRowKind, duration time.Duration) {
	switch kind {
	case validationRowContainerStopTimeout:
		value.ContainerStopTimeout = duration
	case validationRowContainerControlTimeout:
		value.ContainerControlTimeout = duration
	case validationRowContainerSentinelTimeout:
		value.ContainerSentinelTimeout = duration
	case validationRowContainerCrashProbeTimeout:
		value.ContainerCrashProbeTimeout = duration
	}
}

func setValidationWorkspaceValue(value *appconfig.ValidationWorkspace, kind validationRowKind, maximum int64) {
	switch kind {
	case validationRowWorkspaceFiles:
		value.MaxFiles = maximum
	case validationRowWorkspaceDirectories:
		value.MaxDirectories = maximum
	case validationRowWorkspacePathBytes:
		value.MaxPathBytes = maximum
	case validationRowWorkspaceFileBytes:
		value.MaxFileBytes = maximum
	case validationRowWorkspaceTotalBytes:
		value.MaxTotalBytes = maximum
	case validationRowContainerPIDs:
		value.ContainerPIDs = int(maximum)
	case validationRowContainerMemoryBytes:
		value.ContainerMemoryBytes = maximum
	case validationRowContainerCPUMillis:
		value.ContainerCPUMillis = maximum
	case validationRowContainerTemporaryBytes:
		value.ContainerTemporaryBytes = maximum
	case validationRowContainerWorkspaceBytes:
		value.ContainerWorkspaceBytes = maximum
	case validationRowContainerNofileLimit:
		value.ContainerNofileLimit = maximum
	case validationRowContainerGeneratedFileBytes:
		value.ContainerGeneratedFileBytes = maximum
	}
}

func validationSettingsRows(plans []validation.Plan) []validationSettingsRow {
	rows := []validationSettingsRow{
		{plan: -1, check: -1, kind: validationRowWorkspaceFiles},
		{plan: -1, check: -1, kind: validationRowWorkspaceDirectories},
		{plan: -1, check: -1, kind: validationRowWorkspacePathBytes},
		{plan: -1, check: -1, kind: validationRowWorkspaceFileBytes},
		{plan: -1, check: -1, kind: validationRowWorkspaceTotalBytes},
		{plan: -1, check: -1, kind: validationRowContainerPIDs},
		{plan: -1, check: -1, kind: validationRowContainerMemoryBytes},
		{plan: -1, check: -1, kind: validationRowContainerCPUMillis},
		{plan: -1, check: -1, kind: validationRowContainerTemporaryBytes},
		{plan: -1, check: -1, kind: validationRowContainerWorkspaceBytes},
		{plan: -1, check: -1, kind: validationRowContainerNofileLimit},
		{plan: -1, check: -1, kind: validationRowContainerGeneratedFileBytes},
		{plan: -1, check: -1, kind: validationRowContainerStopTimeout},
		{plan: -1, check: -1, kind: validationRowContainerControlTimeout},
		{plan: -1, check: -1, kind: validationRowContainerSentinelTimeout},
		{plan: -1, check: -1, kind: validationRowContainerCrashProbeTimeout},
	}
	for planIndex, plan := range plans {
		rows = append(rows, validationSettingsRow{plan: planIndex, check: -1, kind: validationRowPlan})
		for checkIndex := range plan.Checks {
			rows = append(rows,
				validationSettingsRow{plan: planIndex, check: checkIndex, kind: validationRowRequired},
				validationSettingsRow{plan: planIndex, check: checkIndex, kind: validationRowCommand},
				validationSettingsRow{plan: planIndex, check: checkIndex, kind: validationRowWorkingDirectory},
				validationSettingsRow{plan: planIndex, check: checkIndex, kind: validationRowTimeout},
				validationSettingsRow{plan: planIndex, check: checkIndex, kind: validationRowOutput},
			)
		}
	}
	return rows
}

func validationInvocation(check validation.Check) string {
	parts := []string{cleanAgentText(check.Executable)}
	for _, argument := range check.Arguments {
		parts = append(parts, strconv.Quote(cleanAgentText(argument)))
	}
	return nonemptySetting(strings.Join(parts, " "), "not configured")
}

func adjustDeliverySetting(value *appconfig.Delivery, cursor, direction int) bool {
	switch cursor {
	case 0:
		value.DefaultMode = cycleTyped(value.DefaultMode, []fix.DeliveryMode{fix.DeliveryModeBranch, fix.DeliveryModePullRequest}, direction)
	case 4:
		value.DraftPullRequests = !value.DraftPullRequests
	case 5:
		value.RequireValidation = !value.RequireValidation
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
	state.status = "Modified · s save · r reload"
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
	value.Delegation = selectedAgentOption(probe.Capabilities.Delegation, value.Delegation)
	if value.Delegation == "" {
		value.Delegation = agent.DelegationSingle
	}
}

func (model *Model) configTextEditable() bool {
	cursor := model.configSettings.cursor
	if model.configSettings.profileEditing || model.configSettings.kind == configDelivery && (cursor >= 1 && cursor <= 3 || cursor >= 6 && cursor <= 10) {
		return true
	}
	if model.configSettings.kind == configValidation {
		rows := validationSettingsRows(model.configSettings.working.Validation)
		return cursor >= 0 && cursor < len(rows) && (validationWorkspaceRow(rows[cursor].kind) || rows[cursor].kind == validationRowTimeout || rows[cursor].kind == validationRowOutput)
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
	} else if state.kind == configValidation {
		rows := validationSettingsRows(state.working.Validation)
		if state.cursor < 0 || state.cursor >= len(rows) {
			return
		}
		row := rows[state.cursor]
		if validationWorkspaceRow(row.kind) {
			value = validationWorkspaceText(state.working.ValidationWorkspace, row.kind)
		} else if row.kind == validationRowTimeout {
			check := state.working.Validation[row.plan].Checks[row.check]
			value = check.Timeout.String()
		} else if row.kind == validationRowOutput {
			check := state.working.Validation[row.plan].Checks[row.check]
			value = strconv.FormatInt(check.MaxOutputBytes, 10)
		}
		state.editField = state.cursor
	} else {
		switch state.cursor {
		case 1:
			value = state.working.Delivery.Remote
		case 2:
			value = state.working.Delivery.BaseBranch
		case 3:
			value = state.working.Delivery.BranchTemplate
		case 6:
			value = strconv.FormatInt(state.working.Delivery.CommandOutputBytes, 10)
		case 7:
			value = state.working.Delivery.CommitTitleTemplate
		case 8:
			value = state.working.Delivery.CommitBodyTemplate
		case 9:
			value = state.working.Delivery.PullRequestTitleTemplate
		case 10:
			value = state.working.Delivery.PullRequestBodyTemplate
		}
		state.editField = state.cursor
	}
	state.input.SetValue(value)
	state.input.CursorEnd()
	state.input.Focus()
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
	} else if state.kind == configValidation {
		rows := validationSettingsRows(state.working.Validation)
		if state.editField < 0 || state.editField >= len(rows) {
			return errors.New("validation setting is unavailable")
		}
		row := rows[state.editField]
		if validationWorkspaceRow(row.kind) {
			if validationWorkspaceDurationRow(row.kind) {
				duration, err := time.ParseDuration(value)
				if err != nil || duration <= 0 {
					return errors.New("validation container timeout must be a positive duration such as 30s")
				}
				setValidationWorkspaceDuration(&state.working.ValidationWorkspace, row.kind, duration)
			} else {
				maximum, err := strconv.ParseInt(value, 10, 64)
				if err != nil || maximum <= 0 || row.kind == validationRowContainerPIDs && int64(int(maximum)) != maximum {
					return errors.New("validation workspace/container limit must be a positive integer")
				}
				setValidationWorkspaceValue(&state.working.ValidationWorkspace, row.kind, maximum)
			}
		} else {
			check := &state.working.Validation[row.plan].Checks[row.check]
			switch row.kind {
			case validationRowTimeout:
				duration, err := time.ParseDuration(value)
				if err != nil || duration <= 0 {
					return errors.New("check timeout must be a positive duration such as 10m")
				}
				check.Timeout = duration
			case validationRowOutput:
				maximum, err := strconv.ParseInt(value, 10, 64)
				if err != nil || maximum <= 0 {
					return errors.New("check output bytes must be a positive integer")
				}
				check.MaxOutputBytes = maximum
			default:
				return errors.New("this validation setting is changed with left or right")
			}
		}
	} else {
		if value == "" && state.editField != 2 {
			return errors.New("this value cannot be empty")
		}
		switch state.editField {
		case 1:
			state.working.Delivery.Remote = value
		case 2:
			state.working.Delivery.BaseBranch = value
		case 3:
			state.working.Delivery.BranchTemplate = value
		case 6:
			maximum, err := strconv.ParseInt(value, 10, 64)
			if err != nil || maximum <= 0 {
				return errors.New("Git and publisher output bytes must be a positive integer")
			}
			state.working.Delivery.CommandOutputBytes = maximum
		case 7:
			state.working.Delivery.CommitTitleTemplate = value
		case 8:
			state.working.Delivery.CommitBodyTemplate = value
		case 9:
			state.working.Delivery.PullRequestTitleTemplate = value
		case 10:
			state.working.Delivery.PullRequestBodyTemplate = value
		}
	}
	state.dirty = true
	state.status = "Modified · s save · r reload"
	return nil
}

func (model Model) configSettingsRows() int {
	state := model.configSettings
	switch state.kind {
	case configAgents:
		return len(agentProviderChoices)
	case configFix:
		return 8 + len(scoring.Metrics())
	case configConcurrency:
		return 7
	case configValidation:
		return len(validationSettingsRows(state.working.Validation))
	case configDelivery:
		return 11
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
	lines = scrollModalLineRange(lines, selectedStart, selectedEnd, max(1, model.modalBodyHeight()-2))
	if state.kind != configAgents {
		lines = append(lines, style.DisabledOption(truncate(model.configSettingsStatusLine(), width), width))
	}
	footer := model.configSettingsFooter()
	if state.editing {
		lines = append(lines, "Edit: "+state.input.View())
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
		body = scrollModalLineRange(body, selectedStart, selectedEnd, max(1, model.height-reserved))
		for _, line := range body {
			lines = append(lines, fixSurfaceLineANSI(line, model.width, style.SurfaceModal))
		}
		if state.kind != configAgents {
			lines = append(lines, fixSurfaceLine(model.configSettingsStatusLine(), model.width, style.SurfaceModal, style.TextMuted))
		}
	}
	if state.editing {
		lines = append(lines, fixSurfaceLineANSI("Edit: "+state.input.View(), model.width, style.SurfaceModal))
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
		configValidation: "VALIDATION", configDelivery: "GIT & PULL REQUESTS",
	}[state.kind]
}

func (model Model) configSettingsFooter() string {
	state := model.configSettings
	compact := responsiveTier(model.width, model.height) == ResponsiveCompact
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
	switch state.kind {
	case configFix:
		edit = state.cursor == 6
	case configValidation:
		rows := validationSettingsRows(state.working.Validation)
		if state.cursor >= 0 && state.cursor < len(rows) {
			kind := rows[state.cursor].kind
			if validationWorkspaceRow(kind) {
				edit = true
			} else {
				switch kind {
				case validationRowCommand, validationRowWorkingDirectory:
					readonly = true
				case validationRowTimeout, validationRowOutput:
					edit = true
				}
			}
		}
	case configDelivery:
		edit = state.cursor >= 1 && state.cursor <= 3 || state.cursor >= 6 && state.cursor <= 10
	}
	if compact {
		switch {
		case readonly:
			return "read-only · s save · Esc"
		case edit:
			return "Enter edit · s save · Esc"
		default:
			return "←/→ change · s save · Esc"
		}
	}
	switch {
	case readonly:
		return "Read-only in this release · s save · r reload · Esc back"
	case edit:
		return "Enter edit · s save · r reload · Esc back"
	default:
		return "←/→ change · s save · r reload · Esc back"
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
	selected := min(max(0, model.configSettings.cursor), max(0, len(lines)-1))
	return lines, selected, selected
}

func scrollModalLineRange(lines []string, selectedStart, selectedEnd, limit int) []string {
	if len(lines) <= limit {
		return lines
	}
	limit = max(1, limit)
	selectedStart = min(len(lines)-1, max(0, selectedStart))
	selectedEnd = min(len(lines)-1, max(selectedStart, selectedEnd))
	if selectedEnd-selectedStart+1 >= limit {
		return lines[selectedStart : selectedStart+limit]
	}
	start := max(0, selectedEnd-limit+1)
	if start > selectedStart {
		start = selectedStart
	}
	if start+limit > len(lines) {
		start = max(0, len(lines)-limit)
	}
	return lines[start : start+limit]
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
		lines = append(lines, style.ModalOption(truncate(fmt.Sprintf("%-22s %s", field.Label, value), width), fieldIndex == state.profileCursor, width))
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
		return style.ModalOption(truncate(fmt.Sprintf("%-22s %s", label, value), width), index == state.cursor, width)
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
			option(1, "Change scope", state.working.Fix.ChangeScope), option(2, "Agent profile", profile),
			option(3, "Model", modelName), option(4, "Effort", effort),
			option(5, "Delegation", string(state.working.Fix.Delegation)),
			option(6, "Agent prompt", firstPromptLine(state.working.Fix.PromptTemplate)),
			option(7, "Focus metric", "[x] SCORE"),
		}
		for index, metric := range scoring.Metrics() {
			mark := " "
			if hasMetric(state.working.Fix.Focus, fix.MetricID(metric.ID)) {
				mark = "x"
			}
			lines = append(lines, option(index+8, "Focus metric", fmt.Sprintf("[%s] %s", mark, strings.ToUpper(string(metric.ID)))))
		}
		return lines
	case configConcurrency:
		lines := []string{
			option(0, "Running agents", fmt.Sprint(state.working.Concurrency.MaxAgents)),
			option(1, "Running verifiers", fmt.Sprint(state.working.Concurrency.MaxVerifiers)),
			option(2, "Retained jobs", fmt.Sprint(state.working.Concurrency.MaxRetainedJobs)),
			option(3, "Transcript bytes/job", fmt.Sprint(state.working.Concurrency.MaxTranscriptBytes)),
			option(4, "Actors per job", fmt.Sprint(state.working.Concurrency.MaxActorsPerJob)),
			option(5, "Candidate preview bytes", fmt.Sprint(state.working.Concurrency.MaxCandidatePreviewBytes)),
			option(6, "Candidate preview lines", fmt.Sprint(state.working.Concurrency.MaxCandidatePreviewLines)),
		}
		return lines
	case configValidation:
		rows := validationSettingsRows(state.working.Validation)
		lines := make([]string, 0, len(rows)+1)
		for index, row := range rows {
			if validationWorkspaceRow(row.kind) {
				labels := map[validationRowKind]string{
					validationRowWorkspaceFiles: "Workspace files", validationRowWorkspaceDirectories: "Workspace directories",
					validationRowWorkspacePathBytes: "Workspace path bytes", validationRowWorkspaceFileBytes: "Largest file bytes",
					validationRowWorkspaceTotalBytes: "Workspace total bytes",
					validationRowContainerPIDs:       "Container processes", validationRowContainerMemoryBytes: "Container memory bytes",
					validationRowContainerCPUMillis: "Container CPU millis", validationRowContainerTemporaryBytes: "Container /tmp bytes",
					validationRowContainerWorkspaceBytes: "Container workspace bytes", validationRowContainerNofileLimit: "Container open files",
					validationRowContainerGeneratedFileBytes: "Generated file bytes", validationRowContainerStopTimeout: "Container stop timeout",
					validationRowContainerControlTimeout: "Docker control timeout", validationRowContainerSentinelTimeout: "Safety sentinel timeout",
					validationRowContainerCrashProbeTimeout: "Crash probe timeout",
				}
				lines = append(lines, option(index, labels[row.kind], validationWorkspaceText(state.working.ValidationWorkspace, row.kind)))
				continue
			}
			plan := state.working.Validation[row.plan]
			if row.kind == validationRowPlan {
				mark := " "
				if plan.ID == state.working.Fix.ValidationPlan {
					mark = "x"
				}
				status := fmt.Sprintf("%d trusted checks", len(plan.Checks))
				if len(plan.Checks) == 0 {
					status = "UNUSABLE · no configured checks"
				}
				lines = append(lines, option(index, "["+mark+"] Plan "+plan.ID, status))
				continue
			}
			check := plan.Checks[row.check]
			prefix := "  " + nonemptySetting(check.Label, string(check.ID))
			switch row.kind {
			case validationRowRequired:
				lines = append(lines, option(index, prefix+" required", yesNo(check.Required)))
			case validationRowCommand:
				lines = append(lines, option(index, prefix+" command", validationInvocation(check)+" · installation-owned"))
			case validationRowWorkingDirectory:
				lines = append(lines, option(index, prefix+" working dir", nonemptySetting(check.WorkingDirectory.String(), "candidate root")+" · installation-owned"))
			case validationRowTimeout:
				lines = append(lines, option(index, prefix+" timeout", check.Timeout.String()))
			case validationRowOutput:
				lines = append(lines, option(index, prefix+" output bytes", strconv.FormatInt(check.MaxOutputBytes, 10)))
			}
		}
		if len(state.working.Validation) == 0 {
			lines = append([]string{style.DisabledOption("No validation plans configured", width)}, lines...)
		}
		return lines
	case configDelivery:
		branchTemplate := strings.TrimSpace(state.working.Delivery.BranchTemplate)
		branchValue := branchTemplate + " → " + appconfig.PreviewBranchTemplate(branchTemplate)
		lines := []string{
			option(0, "Default mode", string(state.working.Delivery.DefaultMode)), option(1, "Remote", state.working.Delivery.Remote),
			option(2, "Base branch", nonemptySetting(state.working.Delivery.BaseBranch, "not set")),
			option(3, "Branch template", branchValue),
			option(4, "Initial PR state", map[bool]string{true: "Draft", false: "Ready for review"}[state.working.Delivery.DraftPullRequests]),
			option(5, "PR validation", map[bool]string{true: "Require passing plan", false: "Optional"}[state.working.Delivery.RequireValidation]),
			option(6, "Command output bytes", strconv.FormatInt(state.working.Delivery.CommandOutputBytes, 10)),
			option(7, "Commit title", nonemptySetting(state.working.Delivery.CommitTitleTemplate, "default")),
			option(8, "Commit body", nonemptySetting(state.working.Delivery.CommitBodyTemplate, "default")),
			option(9, "PR title", nonemptySetting(state.working.Delivery.PullRequestTitleTemplate, "commit title")),
			option(10, "PR body", nonemptySetting(state.working.Delivery.PullRequestBodyTemplate, "commit body")),
		}
		return lines
	}
	return nil
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
	result.Validation = cloneConfigValidation(value.Validation)
	return result
}

func cloneConfigValidation(value []validation.Plan) []validation.Plan {
	result := append(value[:0:0], value...)
	for index := range result {
		result[index].Checks = append(result[index].Checks[:0:0], value[index].Checks...)
		for checkIndex := range result[index].Checks {
			result[index].Checks[checkIndex].Arguments = append([]string(nil), value[index].Checks[checkIndex].Arguments...)
		}
	}
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

func delegationIDs(result agent.ProbeResult) []agent.DelegationMode {
	values := []agent.DelegationMode{agent.DelegationSingle}
	for _, option := range result.Capabilities.Delegation {
		if option.ID != agent.DelegationSingle {
			values = append(values, option.ID)
		}
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

func validationPlanIndex(values []validation.Plan, id string) int {
	for index, value := range values {
		if value.ID == id {
			return index
		}
	}
	return -1
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

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
