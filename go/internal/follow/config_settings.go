package follow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

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
	open           bool
	kind           configSettingsKind
	generation     uint64
	loading        bool
	saving         bool
	dirty          bool
	cursor         int
	resolved       appconfig.Resolved
	working        appconfig.Resolved
	probes         map[agent.ProfileID]agent.ProbeResult
	status         string
	input          textinput.Model
	editing        bool
	editField      int
	dirtyCursor    int
	closeAfterSave bool
	returnToFix    bool
	profileEditing bool
	profileCursor  int
	deletePending  agent.ProfileID
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
	profile    agent.ProfileID
	result     agent.ProbeResult
}

func (model *Model) openConfigSettings(kind configSettingsKind) tea.Cmd {
	input := textinput.New()
	input.Prompt = ""
	model.configSettings = configSettingsState{
		open: true, kind: kind, generation: model.configSettings.generation + 1,
		loading: true, probes: map[agent.ProfileID]agent.ProbeResult{}, input: input,
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
	state.cursor = min(state.cursor, max(0, model.configSettingsRows()-1))
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
	generation := model.configSettings.generation
	commands := make([]tea.Cmd, 0, len(model.configSettings.working.Profiles))
	for _, value := range model.configSettings.working.Profiles {
		profile := cloneConfigProfile(value)
		prober := model.profileProber
		commands = append(commands, func() tea.Msg {
			return configProbeMsg{generation: generation, profile: profile.ID, result: prober.Probe(context.Background(), profile)}
		})
	}
	return tea.Batch(commands...)
}

func (model *Model) handleConfigSaved(message configSavedMsg) tea.Cmd {
	state := &model.configSettings
	if !state.open || message.generation != state.generation {
		return nil
	}
	state.saving = false
	if message.err != nil {
		if errors.Is(message.err, appconfig.ErrRevisionConflict) {
			state.status = "Save conflict: settings changed elsewhere; reload with r"
		} else {
			state.status = "Save failed: " + message.err.Error()
		}
		return nil
	}
	state.resolved = cloneConfigResolved(message.saved.Resolved)
	state.working = cloneConfigResolved(message.saved.Resolved)
	state.dirty = false
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
	if state.profileEditing {
		switch key.String() {
		case "esc", "escape":
			state.profileEditing = false
		case "up", "k":
			state.profileCursor = max(0, state.profileCursor-1)
		case "down", "j":
			state.profileCursor = min(max(0, model.profileFieldCount()-1), state.profileCursor+1)
		case "left", "h", "-":
			model.adjustProfileChoice(-1)
		case "right", "l", "+", "=", " ":
			model.adjustProfileChoice(1)
		case "enter":
			if !model.adjustProfileChoice(1) {
				model.beginConfigText()
			}
		case "t":
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
		state.cursor = min(max(0, model.configSettingsRows()-1), state.cursor+1)
	case "left", "h", "-":
		model.adjustConfigSetting(-1)
	case "right", "l", "+", "=", " ":
		model.adjustConfigSetting(1)
	case "enter":
		if state.kind == configAgents && len(state.working.Profiles) > 0 {
			state.profileEditing = true
			state.profileCursor = 0
		} else if model.configTextEditable() {
			model.beginConfigText()
		} else {
			model.adjustConfigSetting(1)
		}
	case "a":
		if state.kind == configAgents {
			model.addDefaultAgentProfile()
		}
	case "d":
		if state.kind == configAgents {
			model.deleteSelectedProfile()
		}
	case "t":
		if state.kind == configAgents {
			return model, model.testSelectedProfileCommand()
		}
	case "r":
		return model, model.reloadConfigSettings()
	case "s", "ctrl+s":
		return model, model.saveConfigSettings()
	}
	return model, nil
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
		for _, profile := range value.Profiles {
			if profile.ID == "" || profile.Runtime == "" {
				return errors.New("every agent profile needs an ID and runtime")
			}
			if seen[profile.ID] {
				return fmt.Errorf("duplicate agent profile %q", profile.ID)
			}
			seen[profile.ID] = true
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
		if value.Delivery.CommitPolicy != "" && value.Delivery.CommitPolicy != "on-publish" {
			return errors.New("unsupported commit policy")
		}
		if value.Delivery.CleanupPolicy != "" && value.Delivery.CleanupPolicy != "retain" {
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
	case 3:
		options := modelIDs(probes[value.Fix.Profile])
		if len(options) == 0 {
			return false
		}
		value.Fix.Model = cycleTyped(value.Fix.Model, options, direction)
	case 4:
		options := effortIDs(probes[value.Fix.Profile])
		if len(options) == 0 {
			return false
		}
		value.Fix.Effort = cycleTyped(value.Fix.Effort, options, direction)
	case 5:
		options := delegationIDs(probes[value.Fix.Profile])
		value.Fix.Delegation = cycleTyped(value.Fix.Delegation, options, direction)
	case 6:
		// The generated prompt compiler and locked envelope are the only v1
		// prompt strategy. Keep the constraint visible without pretending this
		// row can be changed.
		return false
	default:
		metrics := scoring.Metrics()
		index := cursor - 7
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

func validationWorkspaceConsequence(kind validationRowKind) string {
	var consequence string
	switch kind {
	case validationRowWorkspaceFiles, validationRowWorkspaceDirectories, validationRowWorkspacePathBytes, validationRowWorkspaceFileBytes, validationRowWorkspaceTotalBytes:
		consequence = "Shared by candidate copy and before/after fingerprinting; exceeding it fails validation with this effective value."
	case validationRowContainerPIDs:
		consequence = "Maximum processes in each validation container; finite confinement is mandatory and exceeding it fails the check."
	case validationRowContainerMemoryBytes:
		consequence = "Container memory ceiling; workspace and /tmp tmpfs usage is charged to this budget."
	case validationRowContainerCPUMillis:
		consequence = "Validation CPU allocation in thousandths: 1000 is one CPU, 2500 is 2.5 CPUs."
	case validationRowContainerTemporaryBytes:
		consequence = "Maximum /tmp tmpfs size inside validation containers; it also consumes container memory."
	case validationRowContainerWorkspaceBytes:
		consequence = "Writable candidate-copy tmpfs size; it must exceed admitted total bytes and also consumes container memory."
	case validationRowContainerNofileLimit:
		consequence = "Maximum open files per validation process; this finite confinement control cannot be disabled."
	case validationRowContainerGeneratedFileBytes:
		consequence = "Maximum file a validation process may generate; it must cover the admitted per-file limit."
	case validationRowContainerStopTimeout:
		consequence = "Grace allowed for a canceled container to stop before stronger cleanup; it never times active work."
	case validationRowContainerControlTimeout:
		consequence = "Deadline for Docker lifecycle commands such as create, inspect, stop and remove; must exceed stop timeout."
	case validationRowContainerSentinelTimeout:
		consequence = "Deadline for the pre-run filesystem/network safety sentinel; it never times the validation check itself."
	case validationRowContainerCrashProbeTimeout:
		consequence = "Deadline for the startup crash-containment proof; it affects readiness, not an active fix job."
	default:
		consequence = "Validation constraint."
	}
	return "Next start only; restart Slopwatch. " + consequence
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
		value.DefaultMode = cycleTyped(value.DefaultMode, []fix.DeliveryMode{fix.DeliveryModeCandidate, fix.DeliveryModeBranch, fix.DeliveryModePullRequest}, direction)
	case 4:
		return false
	case 5:
		value.DraftPullRequests = !value.DraftPullRequests
	case 6:
		value.RequireValidation = !value.RequireValidation
	default:
		return false
	}
	return true
}

func (model *Model) addDefaultAgentProfile() {
	state := &model.configSettings
	kinds := []agent.RuntimeKind{"codex-cli"}
	if model.profileCatalog != nil && len(model.profileCatalog.Kinds()) > 0 {
		kinds = append([]agent.RuntimeKind(nil), model.profileCatalog.Kinds()...)
		sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	}
	counts := make(map[agent.RuntimeKind]int, len(kinds))
	for _, profile := range state.working.Profiles {
		counts[profile.Runtime]++
	}
	selected := kinds[0]
	for _, kind := range kinds[1:] {
		if counts[kind] < counts[selected] {
			selected = kind
		}
	}
	descriptor := agent.ProfileDescriptor{}
	var descriptorErr error
	if model.profileCatalog != nil {
		descriptor, descriptorErr = model.profileCatalog.Descriptor(selected)
	}
	if model.profileCatalog == nil || descriptorErr != nil {
		descriptor = agent.ProfileDescriptor{Runtime: "codex-cli", Label: "Codex CLI", Fields: []agent.ProfileField{
			{Key: "executable", Default: "codex"}, {Key: "authentication_ref", Default: "provider-owned"},
		}}
		selected = "codex-cli"
	}
	baseID := "agent"
	switch selected {
	case "codex-cli":
		baseID = "codex"
	case "openai-responses":
		baseID = "gpt"
	default:
		baseID = strings.Trim(strings.Map(func(character rune) rune {
			if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
				return character
			}
			return '-'
		}, strings.ToLower(string(selected))), "-")
		if baseID == "" {
			baseID = "agent"
		}
	}
	id := baseID
	for suffix := 2; profileIDExists(state.working.Profiles, agent.ProfileID(id)); suffix++ {
		id = fmt.Sprintf("%s-%d", baseID, suffix)
	}
	profile := agent.Profile{ID: agent.ProfileID(id), Label: descriptor.Label, Runtime: selected, Options: map[string]string{}}
	applyProfileDefaults(&profile, descriptor)
	state.working.Profiles = append(state.working.Profiles, profile)
	state.cursor = len(state.working.Profiles) - 1
	state.dirty = true
	state.status = fmt.Sprintf("Added %s profile · s save", descriptor.Label)
}

func profileIDExists(profiles []agent.Profile, id agent.ProfileID) bool {
	for _, profile := range profiles {
		if profile.ID == id {
			return true
		}
	}
	return false
}

func applyProfileDefaults(profile *agent.Profile, descriptor agent.ProfileDescriptor) {
	for _, field := range descriptor.Fields {
		if profileFieldValue(*profile, field) == "" && field.Default != "" {
			setProfileFieldValue(profile, field, field.Default)
		}
	}
}

func (model Model) profileDescriptor(profile agent.Profile) (agent.ProfileDescriptor, error) {
	if model.profileCatalog == nil {
		return agent.ProfileDescriptor{}, errors.New("agent profile schema is unavailable")
	}
	return model.profileCatalog.Descriptor(profile.Runtime)
}

func (model Model) profileFieldCount() int {
	state := model.configSettings
	if state.cursor < 0 || state.cursor >= len(state.working.Profiles) {
		return 0
	}
	return 3 + len(model.profileEditorFields(state.working.Profiles[state.cursor]))
}

func (model Model) profileEditorFields(profile agent.Profile) []agent.ProfileField {
	descriptor, err := model.profileDescriptor(profile)
	if err != nil {
		return nil
	}
	fields := append([]agent.ProfileField(nil), descriptor.Fields...)
	known := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field.OptionKey != "" {
			known[field.OptionKey] = struct{}{}
		}
	}
	unknown := make([]string, 0)
	for key := range profile.Options {
		if _, exists := known[key]; !exists {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		fields = append(fields, agent.ProfileField{Key: "options." + key, OptionKey: key, Label: "Unsupported option: " + key, Kind: agent.ProfileFieldText, Description: "Clear this value to remove the unsupported option."})
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
	if !state.profileEditing || state.cursor < 0 || state.cursor >= len(state.working.Profiles) {
		return false
	}
	profile := &state.working.Profiles[state.cursor]
	if state.profileCursor == 2 && model.profileCatalog != nil {
		kinds := model.profileCatalog.Kinds()
		if len(kinds) == 0 {
			return false
		}
		sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
		next := cycleTyped(profile.Runtime, kinds, direction)
		if next == profile.Runtime {
			return true
		}
		descriptor, err := model.profileCatalog.Descriptor(next)
		if err != nil {
			state.status = err.Error()
			return true
		}
		profile.Runtime = next
		profile.Executable = ""
		profile.RuntimeProfile = ""
		profile.AuthenticationRef = ""
		profile.Options = map[string]string{}
		applyProfileDefaults(profile, descriptor)
		state.dirty = true
		state.status = "Runtime changed; adapter fields reset · s save"
		return true
	}
	if state.profileCursor < 3 {
		return false
	}
	fields := model.profileEditorFields(*profile)
	index := state.profileCursor - 3
	if index < 0 || index >= len(fields) || fields[index].Kind != agent.ProfileFieldChoice {
		return false
	}
	field := fields[index]
	value := cycleString(profileFieldValue(*profile, field), field.Choices, direction)
	setProfileFieldValue(profile, field, value)
	state.dirty = true
	state.status = "Modified · s save · r reload"
	return true
}

func (model Model) testSelectedProfileCommand() tea.Cmd {
	state := model.configSettings
	if model.profileProber == nil || state.cursor < 0 || state.cursor >= len(state.working.Profiles) {
		return nil
	}
	profile := cloneConfigProfile(state.working.Profiles[state.cursor])
	generation := state.generation
	prober := model.profileProber
	return func() tea.Msg {
		return configProbeMsg{generation: generation, profile: profile.ID, result: prober.Probe(context.Background(), profile)}
	}
}

func (model *Model) deleteSelectedProfile() {
	state := &model.configSettings
	if state.cursor < 0 || state.cursor >= len(state.working.Profiles) {
		return
	}
	removed := state.working.Profiles[state.cursor].ID
	if state.working.Fix.Profile == removed {
		state.status = "Cannot delete the default Fix profile; choose another default first"
		return
	}
	for _, job := range model.agents.Jobs {
		if job.ProfileID == string(removed) && job.Phase != fix.PhaseDiscarded {
			state.status = "Cannot delete a profile referenced by a retained or running job"
			return
		}
	}
	if state.deletePending != removed {
		state.deletePending = removed
		state.status = fmt.Sprintf("Press delete again to remove %s; save persists the deletion", removed)
		return
	}
	state.working.Profiles = append(state.working.Profiles[:state.cursor], state.working.Profiles[state.cursor+1:]...)
	state.deletePending = ""
	state.cursor = min(state.cursor, max(0, len(state.working.Profiles)-1))
	state.dirty = true
	state.status = fmt.Sprintf("Removed %s from draft · s save", removed)
}

func (model *Model) configTextEditable() bool {
	cursor := model.configSettings.cursor
	if model.configSettings.profileEditing || model.configSettings.kind == configDelivery && (cursor >= 1 && cursor <= 3 || cursor >= 7 && cursor <= 11) {
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
		profile := state.working.Profiles[state.cursor]
		switch state.profileCursor {
		case 0:
			value = string(profile.ID)
		case 1:
			value = profile.Label
		case 2:
			value = string(profile.Runtime)
		default:
			fields := model.profileEditorFields(profile)
			if state.profileCursor-3 >= len(fields) {
				state.status = "agent profile schema is unavailable"
				return
			}
			value = profileFieldValue(profile, fields[state.profileCursor-3])
		}
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
			value, _ = configEffectiveBranchTemplate(state.working)
		case 7:
			value = strconv.FormatInt(state.working.Delivery.CommandOutputBytes, 10)
		case 8:
			value = state.working.Delivery.CommitTitleTemplate
		case 9:
			value = state.working.Delivery.CommitBodyTemplate
		case 10:
			value = state.working.Delivery.PullRequestTitleTemplate
		case 11:
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
		profile := &state.working.Profiles[state.cursor]
		oldID := profile.ID
		if state.editField < 3 && value == "" {
			return errors.New("this value cannot be empty")
		}
		switch state.editField {
		case 0:
			profile.ID = agent.ProfileID(value)
		case 1:
			profile.Label = value
		case 2:
			profile.Runtime = agent.RuntimeKind(value)
		default:
			fields := model.profileEditorFields(*profile)
			if state.editField-3 >= len(fields) {
				return errors.New("agent profile schema is unavailable")
			}
			field := fields[state.editField-3]
			if err := validateProfileFieldValue(field, value); err != nil {
				return err
			}
			setProfileFieldValue(profile, field, value)
		}
		if oldID != profile.ID && state.working.Fix.Profile == oldID {
			state.working.Fix.Profile = profile.ID
		}
		if model.profileCatalog != nil {
			if err := model.profileCatalog.ValidateProfile(*profile); err != nil {
				return err
			}
		}
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
		case 7:
			maximum, err := strconv.ParseInt(value, 10, 64)
			if err != nil || maximum <= 0 {
				return errors.New("Git and publisher output bytes must be a positive integer")
			}
			state.working.Delivery.CommandOutputBytes = maximum
		case 8:
			state.working.Delivery.CommitTitleTemplate = value
		case 9:
			state.working.Delivery.CommitBodyTemplate = value
		case 10:
			state.working.Delivery.PullRequestTitleTemplate = value
		case 11:
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
		return len(state.working.Profiles)
	case configFix:
		return 7 + len(scoring.Metrics())
	case configConcurrency:
		return 7
	case configValidation:
		return len(validationSettingsRows(state.working.Validation))
	case configDelivery:
		return 14
	default:
		return 0
	}
}

func (model Model) configSettingsView() string {
	state := model.configSettings
	width := min(72, max(38, model.width-4))
	title := map[configSettingsKind]string{
		configAgents: "AGENTS", configFix: "FIX DEFAULTS", configConcurrency: "CONCURRENCY & RETENTION",
		configValidation: "VALIDATION", configDelivery: "GIT & PULL REQUESTS",
	}[state.kind]
	if state.loading {
		return style.Popup(style.Heading(title), []string{"Loading configuration…"}, "Esc back", width)
	}
	lines := model.configSettingsLines(width - 4)
	if len(lines) == 0 {
		lines = []string{"No configured items."}
	}
	scrollCursor := state.cursor
	if state.profileEditing {
		scrollCursor = state.profileCursor
	}
	lines = scrollModalLines(lines, scrollCursor, max(1, model.modalBodyHeight()-2))
	lines = append(lines, style.DisabledOption(truncate(model.configSettingsStatusLine(), width), width))
	footer := model.configSettingsFooter()
	if state.editing {
		lines = append(lines, "Edit: "+state.input.View())
	}
	return style.Popup(style.Heading(title), lines, footer, width)
}

func (model Model) configSettingsFullScreen() string {
	state := model.configSettings
	title := map[configSettingsKind]string{
		configAgents: "AGENTS", configFix: "FIX DEFAULTS", configConcurrency: "CONCURRENCY & RETENTION",
		configValidation: "VALIDATION", configDelivery: "GIT & PULL REQUESTS",
	}[state.kind]
	footer := model.configSettingsFooter()
	lines := []string{fixSurfaceLine(title, model.width, style.SurfaceHeader, style.TextPrimary)}
	if state.loading {
		lines = append(lines, fixSurfaceLine("Loading configuration…", model.width, style.SurfaceModal, style.TextMuted))
	} else {
		body := model.configSettingsLines(model.width)
		scrollCursor := state.cursor
		if state.profileEditing {
			scrollCursor = state.profileCursor
		}
		reserved := 3 // header, status, footer
		if state.editing {
			reserved++ // active editor row
		}
		body = scrollModalLines(body, scrollCursor, max(1, model.height-reserved))
		for _, line := range body {
			lines = append(lines, fixSurfaceLineANSI(line, model.width, style.SurfaceModal))
		}
		lines = append(lines, fixSurfaceLine(model.configSettingsStatusLine(), model.width, style.SurfaceModal, style.TextMuted))
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

func configSettingsOrigin(kind configSettingsKind, value appconfig.Resolved) appconfig.Origin {
	key := map[configSettingsKind]string{
		configAgents: "agents", configFix: "fix", configConcurrency: "concurrency",
		configValidation: "validation", configDelivery: "delivery",
	}[kind]
	if origin := value.Origins[key]; origin != "" {
		return origin
	}
	return appconfig.OriginBuiltIn
}

func configOriginLabel(origin appconfig.Origin) string {
	switch origin {
	case appconfig.OriginUser:
		return "user preferences"
	case appconfig.OriginRepository:
		return "repository preferences"
	case appconfig.OriginCLI:
		return "command-line override"
	case appconfig.OriginSession:
		return "current session override"
	default:
		return "built-in default"
	}
}

func (model Model) configSettingsFooter() string {
	state := model.configSettings
	compact := responsiveTier(model.width, model.height) == ResponsiveCompact
	if state.editing {
		return "Enter apply · Esc cancel"
	}
	if state.kind == configAgents {
		if compact {
			return "↑/↓ · a add · d remove · s save · Esc"
		}
		if state.profileEditing {
			return "↑/↓ fields · Enter edit · t test · Esc profiles"
		}
		return "↑/↓ profiles · Enter edit · t test · a add agent · d remove · s save · Esc"
	}
	readonly := false
	edit := false
	switch state.kind {
	case configFix:
		readonly = state.cursor == 6
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
		readonly = state.cursor == 4 || state.cursor == 12 || state.cursor == 13
		edit = state.cursor >= 1 && state.cursor <= 3 || state.cursor >= 7 && state.cursor <= 11
	}
	if compact {
		switch {
		case readonly:
			return "read-only · ↑/↓ · s save · Esc"
		case edit:
			return "Enter edit · ↑/↓ · s save · Esc"
		default:
			return "↑/↓ · ←/→ change · s save · Esc"
		}
	}
	switch {
	case readonly:
		return "Read-only in this release · ↑/↓ fields · s save · r reload · Esc back"
	case edit:
		return "Enter edit · ↑/↓ fields · s save · r reload · Esc back"
	default:
		return "↑/↓ fields · ←/→ change · s save · r reload · Esc back"
	}
}

func (model Model) configSettingsStatusLine() string {
	source := model.configFocusedSourceNote()
	if model.configSettings.status == "" {
		return source
	}
	return model.configSettings.status + " · " + source
}

func (model Model) configFocusedSourceNote() string {
	state := model.configSettings
	origin := configSettingsOrigin(state.kind, state.resolved)
	key := model.configFieldOriginKey(state.cursor)
	if state.kind == configAgents && state.profileEditing && state.cursor >= 0 && state.cursor < len(state.working.Profiles) {
		profile := state.working.Profiles[state.cursor]
		key = profileEditorOriginKey(profile, model.profileEditorFields(profile), state.profileCursor)
	}
	if key != "" {
		if exact, ok := configOriginForKey(state.working.Origins, key); ok {
			origin = exact
		}
	}
	switch origin {
	case appconfig.OriginRepository:
		return "Repo override · saves user default only"
	case appconfig.OriginCLI:
		return "CLI override · saves user default only"
	case appconfig.OriginSession:
		return "Session override · saves user default only"
	case appconfig.OriginUser:
		return "Source: user preferences"
	default:
		return "Built-in default · save creates user default"
	}
}

func configOriginForKey(origins map[string]appconfig.Origin, key string) (appconfig.Origin, bool) {
	for candidate := key; candidate != ""; {
		if origin, ok := origins[candidate]; ok {
			return origin, true
		}
		index := strings.LastIndex(candidate, ".")
		if index < 0 {
			break
		}
		candidate = candidate[:index]
	}
	return "", false
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

func (model Model) configSettingsLines(width int) []string {
	state := model.configSettings
	disabled := func(value string) string { return style.DisabledOption(truncate(value, width), width) }
	option := func(index int, label, value string) string {
		if key := model.configFieldOriginKey(index); key != "" {
			if origin := state.working.Origins[key]; origin != "" && origin != appconfig.OriginBuiltIn {
				value += " · " + configOriginLabel(origin)
			}
		}
		return style.ModalOption(truncate(fmt.Sprintf("%-22s %s", label, value), width), index == state.cursor, width)
	}
	switch state.kind {
	case configAgents:
		if state.profileEditing && state.cursor >= 0 && state.cursor < len(state.working.Profiles) {
			profile := state.working.Profiles[state.cursor]
			rows := []struct{ label, value string }{{"Profile ID", string(profile.ID)}, {"Label", profile.Label}, {"Runtime", string(profile.Runtime)}}
			fields := model.profileEditorFields(profile)
			for _, field := range fields {
				value := profileFieldValue(profile, field)
				if value == "" && field.Default != "" {
					value = field.Default + " · adapter default"
				}
				rows = append(rows, struct{ label, value string }{field.Label, value})
			}
			lines := make([]string, 0, len(rows)+2)
			for i, row := range rows {
				if origin := state.working.Origins[profileEditorOriginKey(profile, fields, i)]; origin != "" && origin != appconfig.OriginBuiltIn {
					row.value += " · " + configOriginLabel(origin)
				}
				lines = append(lines, style.ModalOption(truncate(fmt.Sprintf("%-22s %s", row.label, row.value), width), i == state.profileCursor, width))
			}
			if state.profileCursor >= 3 && state.profileCursor-3 < len(fields) && fields[state.profileCursor-3].Description != "" {
				lines = append(lines, disabled(fields[state.profileCursor-3].Description))
			}
			if probe, ok := state.probes[profile.ID]; ok {
				diagnostic := cleanAgentText(probe.Diagnostic)
				if diagnostic == "" {
					diagnostic = "probe completed"
				}
				lines = append(lines, disabled("Test: "+agentProbeReadiness(probe)+" · "+diagnostic))
			}
			return lines
		}
		lines := make([]string, 0, len(state.working.Profiles))
		for index, profile := range state.working.Profiles {
			readiness := "NOT TESTED"
			diagnostic := ""
			if result, ok := state.probes[profile.ID]; ok {
				readiness = agentProbeReadiness(result)
				if result.Version != "" {
					readiness += " " + result.Version
				}
				if result.Diagnostic != "" {
					diagnostic = cleanAgentText(result.Diagnostic)
				}
			}
			label := fmt.Sprintf("%s · %s", profile.Label, profile.Runtime)
			value := fmt.Sprintf("%s · auth %s", readiness, nonemptySetting(profile.AuthenticationRef, "provider-owned"))
			lines = append(lines, option(index, label, value))
			if index == state.cursor && diagnostic != "" {
				lines = append(lines, disabled("Remediation: "+diagnostic))
			}
		}
		return lines
	case configFix:
		profile := nonemptySetting(string(state.working.Fix.Profile), "not selected")
		modelName := nonemptySetting(string(state.working.Fix.Model), "runtime default")
		effort := nonemptySetting(string(state.working.Fix.Effort), "runtime default")
		lines := []string{
			option(0, "Target score", fmt.Sprintf("%.0f", state.working.Fix.TargetScore)),
			option(1, "Change scope", state.working.Fix.ChangeScope), option(2, "Agent profile", profile),
			option(3, "Model", modelName), option(4, "Effort", effort),
			option(5, "Delegation", string(state.working.Fix.Delegation)),
			option(6, "Prompt strategy", nonemptySetting(state.working.Fix.PromptTemplate, "default")+" · fixed v1 · locked envelope"),
		}
		for index, metric := range scoring.Metrics() {
			mark := " "
			if hasMetric(state.working.Fix.Focus, fix.MetricID(metric.ID)) {
				mark = "x"
			}
			lines = append(lines, option(index+7, "Focus metric", fmt.Sprintf("[%s] %s", mark, strings.ToUpper(string(metric.ID)))))
		}
		descriptions := []string{
			"Seeds each new Fix form; the form may override it for one job.",
			"Bounds candidate writes; scope violations remain visible and block publication.",
			"Default adapter profile for newly prepared jobs.",
			"Default model for newly prepared jobs; available values come from the selected adapter.",
			"Default effort for newly prepared jobs; available values come from the selected adapter.",
			"Controls whether the adapter may delegate when it reports that capability.",
			"The v1 compiler is fixed: generated objectives remain inside a non-editable safety envelope.",
		}
		if state.cursor >= 0 {
			if state.cursor < len(descriptions) {
				lines = append(lines, disabled(descriptions[state.cursor]))
			} else {
				lines = append(lines, disabled("Selected metrics become explicit scoring goals for newly prepared jobs."))
			}
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
		descriptions := []string{
			"Maximum fix jobs actively using an agent; lowering it never cancels running jobs and queues later work.",
			"Maximum independent verification runs; lowering it never cancels a running verifier.",
			"Admission cap for retained jobs; lowering it never deletes jobs and blocks admission until below the cap.",
			"Pinned at Prepare for agent output; saved value exactly bounds JSON transcript-entry bytes per job.",
			"Pinned at Prepare; limits distinct primary or delegated actors represented in a job transcript.",
			"Pinned at Prepare; maximum candidate-file bytes loaded for the source preview. Truncation is labelled.",
			"Pinned at Prepare; maximum candidate-file lines rendered in the source preview. Truncation is labelled.",
		}
		if state.cursor >= 0 && state.cursor < len(descriptions) {
			lines = append(lines, disabled(descriptions[state.cursor]))
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
		if state.cursor >= 0 && state.cursor < len(rows) {
			row := rows[state.cursor]
			if validationWorkspaceRow(row.kind) {
				lines = append(lines, disabled(validationWorkspaceConsequence(row.kind)))
			} else if row.kind == validationRowPlan {
				lines = append(lines, disabled("Selects the default plan; PR publication only requires it when Git & PR › PR validation says so."))
			} else if row.kind == validationRowCommand || row.kind == validationRowWorkingDirectory {
				lines = append(lines, disabled("Trusted command identity is read-only here; edit installation preferences, then press r to reload."))
			} else {
				lines = append(lines, disabled("This check policy is user-configurable and applies to newly started validation runs."))
			}
		}
		if len(state.working.Validation) == 0 {
			lines = append([]string{disabled("No trusted validation plans are configured."), disabled("Add one in preferences, then press r to reload.")}, lines...)
		}
		return lines
	case configDelivery:
		branchTemplate, legacyBranchTemplate := configEffectiveBranchTemplate(state.working)
		branchValue := branchTemplate + " → " + appconfig.PreviewBranchTemplate(branchTemplate)
		if legacyBranchTemplate {
			branchValue += " · legacy Fix override"
		}
		lines := []string{
			option(0, "Default mode", string(state.working.Delivery.DefaultMode)), option(1, "Remote", state.working.Delivery.Remote),
			option(2, "Base branch", nonemptySetting(state.working.Delivery.BaseBranch, "not set")),
			option(3, "Branch template", branchValue), option(4, "Publisher", state.working.Delivery.Publisher+" · only supported v1 adapter"),
			option(5, "Initial PR state", map[bool]string{true: "Draft", false: "Ready for review"}[state.working.Delivery.DraftPullRequests]),
			option(6, "PR validation", map[bool]string{true: "Require passing plan", false: "Optional"}[state.working.Delivery.RequireValidation]),
			option(7, "Command output bytes", strconv.FormatInt(state.working.Delivery.CommandOutputBytes, 10)),
			option(8, "Commit title", nonemptySetting(state.working.Delivery.CommitTitleTemplate, "built-in default")),
			option(9, "Commit body", nonemptySetting(state.working.Delivery.CommitBodyTemplate, "built-in default")),
			option(10, "PR title", nonemptySetting(state.working.Delivery.PullRequestTitleTemplate, "commit title")),
			option(11, "PR body", nonemptySetting(state.working.Delivery.PullRequestBodyTemplate, "commit body")),
			option(12, "Cleanup policy", nonemptySetting(state.working.Delivery.CleanupPolicy, "retain")+" · fixed v1"),
			option(13, "Commit policy", nonemptySetting(state.working.Delivery.CommitPolicy, "on-publish")+" · fixed v1"),
		}
		switch state.cursor {
		case 0:
			lines = append(lines, disabled("Seeds each Fix form; changing mode in a form triggers an authoritative delivery recheck."))
		case 1:
			lines = append(lines, disabled("Resolved and identity-checked for branch or pull-request delivery; candidate-only jobs do not publish."))
		case 2:
			lines = append(lines, disabled("Checked on the selected remote when publishing; it does not block the refactor."))
		case 3:
			if legacyBranchTemplate {
				lines = append(lines, disabled("Effective value comes from legacy Fix preferences; edit and save this row to migrate it. Branch collisions are reported."))
			} else {
				lines = append(lines, disabled("{job-short-id} is optional; an existing branch is reported as a collision."))
			}
		case 4:
			lines = append(lines, disabled("GitHub CLI is the only publisher adapter in v1; authorization is checked before admission and publish."))
		case 5:
			lines = append(lines, disabled("Controls whether a new pull request starts as a draft or ready for review."))
		case 6:
			lines = append(lines, disabled("When required, Publish stays unavailable until the selected plan passes."))
		case 7:
			lines = append(lines, disabled("Bounds captured candidate, delivery and publisher command output; commands have no wall-clock timeout and remain cancelable."))
		case 8, 10:
			lines = append(lines, disabled("Single-line title template rendered only during explicit publication; agent runtimes never receive publication authority."))
		case 9, 11:
			lines = append(lines, disabled("Single-line body template in v1; line breaks and control characters are rejected. Rendered only during explicit publication."))
		case 12:
			lines = append(lines, disabled("Fixed v1: worktrees, branches, and transcripts remain until explicit Discard or Cleanup."))
		case 13:
			lines = append(lines, disabled("Fixed v1: commit, push, and PR creation occur only after explicit Publish from Review."))
		}
		return lines
	}
	return nil
}

func profileEditorOriginKey(profile agent.Profile, fields []agent.ProfileField, index int) string {
	prefix := "agents." + string(profile.ID)
	switch index {
	case 0:
		return prefix
	case 1:
		return prefix + ".label"
	case 2:
		return prefix + ".runtime"
	default:
		fieldIndex := index - 3
		if fieldIndex >= 0 && fieldIndex < len(fields) {
			return prefix + "." + fields[fieldIndex].Key
		}
	}
	return ""
}

func (model Model) configFieldOriginKey(index int) string {
	state := model.configSettings
	switch state.kind {
	case configAgents:
		if index >= 0 && index < len(state.working.Profiles) {
			return "agents." + string(state.working.Profiles[index].ID) + ".runtime"
		}
	case configFix:
		keys := []string{"fix.target_score", "fix.change_scope", "fix.profile", "fix.model", "fix.effort", "fix.delegation", "fix.prompt_template"}
		if index < len(keys) {
			return keys[index]
		}
		metrics := scoring.Metrics()
		metricIndex := index - len(keys)
		if metricIndex >= 0 && metricIndex < len(metrics) {
			return "fix.focus." + string(metrics[metricIndex].ID)
		}
	case configConcurrency:
		keys := []string{"concurrency.max_agents", "concurrency.max_verifiers", "concurrency.max_retained_jobs", "concurrency.max_transcript_bytes", "concurrency.max_actors_per_job", "concurrency.max_candidate_preview_bytes", "concurrency.max_candidate_preview_lines"}
		if index < len(keys) {
			return keys[index]
		}
	case configDelivery:
		if index == 3 {
			if _, legacy := configEffectiveBranchTemplate(state.working); legacy {
				return "fix.branch_template"
			}
		}
		keys := []string{"delivery.default_mode", "delivery.remote", "delivery.base_branch", "delivery.branch_template", "delivery.publisher", "delivery.draft_pull_requests", "delivery.require_validation", "delivery.command_output_bytes", "delivery.commit_title_template", "delivery.commit_body_template", "delivery.pull_request_title_template", "delivery.pull_request_body_template", "delivery.cleanup_policy", "delivery.commit_policy"}
		if index < len(keys) {
			return keys[index]
		}
	case configValidation:
		rows := validationSettingsRows(state.working.Validation)
		if index >= 0 && index < len(rows) {
			row := rows[index]
			if validationWorkspaceRow(row.kind) {
				keys := map[validationRowKind]string{
					validationRowWorkspaceFiles:              "validation_workspace.max_files",
					validationRowWorkspaceDirectories:        "validation_workspace.max_directories",
					validationRowWorkspacePathBytes:          "validation_workspace.max_path_bytes",
					validationRowWorkspaceFileBytes:          "validation_workspace.max_file_bytes",
					validationRowWorkspaceTotalBytes:         "validation_workspace.max_total_bytes",
					validationRowContainerPIDs:               "validation_workspace.container_pids",
					validationRowContainerMemoryBytes:        "validation_workspace.container_memory_bytes",
					validationRowContainerCPUMillis:          "validation_workspace.container_cpu_millis",
					validationRowContainerTemporaryBytes:     "validation_workspace.container_temporary_bytes",
					validationRowContainerWorkspaceBytes:     "validation_workspace.container_workspace_bytes",
					validationRowContainerNofileLimit:        "validation_workspace.container_nofile_limit",
					validationRowContainerGeneratedFileBytes: "validation_workspace.container_generated_file_bytes",
					validationRowContainerStopTimeout:        "validation_workspace.container_stop_timeout",
					validationRowContainerControlTimeout:     "validation_workspace.container_control_timeout",
					validationRowContainerSentinelTimeout:    "validation_workspace.container_sentinel_timeout",
					validationRowContainerCrashProbeTimeout:  "validation_workspace.container_crash_probe_timeout",
				}
				return keys[row.kind]
			}
			if row.kind == validationRowPlan {
				return "fix.validation_plan"
			}
			key := "validation." + state.working.Validation[row.plan].ID
			if row.check >= 0 {
				key += ".checks." + string(state.working.Validation[row.plan].Checks[row.check].ID)
			}
			return key
		}
	}
	return ""
}

func configEffectiveBranchTemplate(value appconfig.Resolved) (string, bool) {
	deliveryTemplate := strings.TrimSpace(value.Delivery.BranchTemplate)
	legacyTemplate := strings.TrimSpace(value.Fix.BranchTemplate)
	if deliveryTemplate == "" {
		return legacyTemplate, legacyTemplate != ""
	}
	legacyOrigin := value.Origins["fix.branch_template"]
	if value.Origins["delivery.branch_template"] == appconfig.OriginBuiltIn &&
		legacyOrigin != "" && legacyOrigin != appconfig.OriginBuiltIn && legacyTemplate != "" {
		return legacyTemplate, true
	}
	return deliveryTemplate, false
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
