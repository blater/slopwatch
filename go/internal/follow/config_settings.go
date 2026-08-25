package follow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
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
	profileEditing bool
	profileCursor  int
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
	profile    agent.ProfileID
	result     agent.ProbeResult
}

func (model *Model) openConfigSettings(kind configSettingsKind) tea.Cmd {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 256
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
	if state.closeAfterSave {
		state.closeAfterSave = false
		if model.hasOverlay(OverlaySettingsDirty) {
			model.overlays.Pop()
		}
		model.closeConfigSettingsNow()
		return nil
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
			model.closeConfigSettings()
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
		model.closeConfigSettings()
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
			model.addDefaultCodexProfile()
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

func (model *Model) closeConfigSettings() {
	if model.configSettings.dirty {
		model.configSettings.dirtyCursor = 0
		model.overlays.Push(OverlaySettingsDirty, OverlayCaller{MainView: model.mainView, Overlay: OverlayConfigSettings, Selected: model.mainSelection()})
		return
	}
	model.closeConfigSettingsNow()
}

func (model *Model) closeConfigSettingsNow() {
	model.configSettings.open = false
	model.configSettings.editing = false
	model.settings = true
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
			model.closeConfigSettingsNow()
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
	if err := validateConfigSettings(state.kind, state.working); err != nil {
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
	generation, revision := state.generation, state.resolved.Revision
	store, workspace := model.configStore, model.configWorkspace
	return func() tea.Msg {
		saved, err := store.Save(context.Background(), workspace, appconfig.ScopeUser, patch, revision)
		return configSavedMsg{generation: generation, saved: saved, err: err}
	}
}

func configSettingsPatch(kind configSettingsKind, value appconfig.Resolved) appconfig.Patch {
	switch kind {
	case configAgents:
		profiles := cloneConfigProfiles(value.Profiles)
		return appconfig.Patch{Profiles: &profiles}
	case configFix, configValidation:
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
	switch kind {
	case configAgents:
		seen := map[agent.ProfileID]bool{}
		for _, profile := range value.Profiles {
			if profile.ID == "" || profile.Runtime == "" || profile.Executable == "" {
				return errors.New("every agent profile needs an ID, runtime, and executable")
			}
			if seen[profile.ID] {
				return fmt.Errorf("duplicate agent profile %q", profile.ID)
			}
			seen[profile.ID] = true
		}
	case configFix:
		if math.IsNaN(value.Fix.TargetScore) || math.IsInf(value.Fix.TargetScore, 0) || value.Fix.TargetScore < 0 {
			return errors.New("target score must be finite and non-negative")
		}
		if value.Fix.MaxAttempts <= 0 || value.Fix.AttemptTimeout <= 0 {
			return errors.New("attempt count and timeout must be positive")
		}
	case configConcurrency:
		if value.Concurrency.MaxAgents <= 0 || value.Concurrency.MaxVerifiers <= 0 ||
			value.Concurrency.MaxRetainedJobs <= 0 || value.Concurrency.MaxTranscriptBytes <= 0 {
			return errors.New("all concurrency and retention limits must be positive")
		}
	case configValidation:
		if value.Fix.ValidationPlan != "" && validationPlanIndex(value.Validation, value.Fix.ValidationPlan) < 0 {
			return fmt.Errorf("validation plan %q is unavailable", value.Fix.ValidationPlan)
		}
	case configDelivery:
		if value.Delivery.DefaultMode == "" || value.Delivery.Remote == "" || value.Delivery.BranchTemplate == "" || value.Delivery.Publisher == "" {
			return errors.New("delivery mode, remote, branch template, and publisher are required")
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
		value.Fix.MaxAttempts = max(1, value.Fix.MaxAttempts+direction)
	case 7:
		value.Fix.AttemptTimeout = maxDuration(5*time.Minute, value.Fix.AttemptTimeout+time.Duration(direction)*5*time.Minute)
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
		value.MaxTranscriptBytes = maxInt64(1024, value.MaxTranscriptBytes+int64(direction)*256*1024)
	default:
		return false
	}
	return true
}

func adjustValidationSetting(value *appconfig.Resolved, cursor, direction int) bool {
	if len(value.Validation) == 0 || cursor < 0 || cursor >= len(value.Validation) {
		return false
	}
	selected := value.Validation[cursor].ID
	if value.Fix.ValidationPlan == selected && direction > 0 {
		value.Fix.ValidationPlan = ""
	} else {
		value.Fix.ValidationPlan = selected
	}
	return true
}

func adjustDeliverySetting(value *appconfig.Delivery, cursor, direction int) bool {
	switch cursor {
	case 0:
		value.DefaultMode = cycleTyped(value.DefaultMode, []fix.DeliveryMode{fix.DeliveryModeCandidate, fix.DeliveryModePullRequest}, direction)
	case 4:
		value.Publisher = cycleString(value.Publisher, []string{"github-cli"}, direction)
	case 5:
		value.DraftPullRequests = !value.DraftPullRequests
	default:
		return false
	}
	return true
}

func (model *Model) addDefaultCodexProfile() {
	state := &model.configSettings
	for _, profile := range state.working.Profiles {
		if profile.ID == "codex" {
			state.status = "Codex profile already exists"
			return
		}
	}
	profile := agent.Profile{
		ID: "codex", Label: "Codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned",
	}
	if descriptor, err := model.profileDescriptor(profile); err == nil {
		for _, field := range descriptor.Fields {
			if profileFieldValue(profile, field) == "" && field.Default != "" {
				setProfileFieldValue(&profile, field, field.Default)
			}
		}
	}
	state.working.Profiles = append(state.working.Profiles, profile)
	state.cursor = len(state.working.Profiles) - 1
	state.dirty = true
	state.status = "Added safe Codex CLI profile · s save"
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
	if !state.profileEditing || state.cursor < 0 || state.cursor >= len(state.working.Profiles) || state.profileCursor < 3 {
		return false
	}
	profile := &state.working.Profiles[state.cursor]
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
	state.working.Profiles = append(state.working.Profiles[:state.cursor], state.working.Profiles[state.cursor+1:]...)
	state.cursor = min(state.cursor, max(0, len(state.working.Profiles)-1))
	state.dirty = true
	state.status = fmt.Sprintf("Removed %s from draft · s save", removed)
}

func (model *Model) configTextEditable() bool {
	return model.configSettings.profileEditing || model.configSettings.kind == configDelivery && model.configSettings.cursor >= 1 && model.configSettings.cursor <= 3
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
	} else {
		switch state.cursor {
		case 1:
			value = state.working.Delivery.Remote
		case 2:
			value = state.working.Delivery.BaseBranch
		case 3:
			value = state.working.Delivery.BranchTemplate
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
		return 8 + len(scoring.Metrics())
	case configConcurrency:
		return 4
	case configValidation:
		return len(state.working.Validation)
	case configDelivery:
		return 6
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
	lines = scrollModalLines(lines, state.cursor, max(1, model.modalBodyHeight()-2))
	if state.status != "" {
		lines = append(lines, style.DisabledOption(state.status, width))
	} else if !state.loading {
		lines = append(lines, style.DisabledOption("Source: "+string(configSettingsOrigin(state.kind, state.resolved)), width))
	}
	footer := "↑/↓ fields · ←/→ change · Enter edit · s save · r reload · Esc back"
	if state.kind == configAgents {
		footer = "↑/↓ profiles · Enter edit · t test · a add Codex · d remove · s save · Esc"
		if state.profileEditing {
			footer = "↑/↓ fields · Enter edit · t test · Esc profiles"
		}
	}
	if state.editing {
		lines = append(lines, "Edit: "+state.input.View())
		footer = "Enter apply · Esc cancel"
	} else if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "↑/↓ · ←/→ · s save · Esc back"
		if state.kind == configAgents {
			footer = "↑/↓ · a add · d remove · s save · Esc"
		}
	}
	return style.Popup(style.Heading(title), lines, footer, width)
}

func (model Model) configSettingsFullScreen() string {
	state := model.configSettings
	title := map[configSettingsKind]string{
		configAgents: "AGENTS", configFix: "FIX DEFAULTS", configConcurrency: "CONCURRENCY & RETENTION",
		configValidation: "VALIDATION", configDelivery: "GIT & PULL REQUESTS",
	}[state.kind]
	footer := "↑/↓ fields · ←/→ change · Enter edit · s save · r reload · Esc back"
	lines := []string{fixSurfaceLine(title, model.width, style.SurfaceHeader, style.TextPrimary)}
	if state.loading {
		lines = append(lines, fixSurfaceLine("Loading configuration…", model.width, style.SurfaceModal, style.TextMuted))
	} else {
		body := model.configSettingsLines(model.width)
		body = scrollModalLines(body, state.cursor, max(1, model.height-3))
		for _, line := range body {
			lines = append(lines, fixSurfaceLineANSI(line, model.width, style.SurfaceModal))
		}
		if state.status != "" {
			lines = append(lines, fixSurfaceLine(state.status, model.width, style.SurfaceModal, style.TextMuted))
		}
	}
	if state.kind == configAgents {
		footer = "↑/↓ profiles · a add · Enter edit · t test · d remove · s save · Esc"
	}
	if state.editing {
		lines = append(lines, fixSurfaceLineANSI("Edit: "+state.input.View(), model.width, style.SurfaceModal))
		footer = "Enter apply · Esc cancel"
	} else if responsiveTier(model.width, model.height) == ResponsiveCompact {
		footer = "↑/↓ · ←/→ · s save · Esc back"
		if state.kind == configAgents {
			footer = "↑/↓ · a add · d remove · s save · Esc"
		}
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

func (model Model) configSettingsLines(width int) []string {
	state := model.configSettings
	option := func(index int, label, value string) string {
		if key := model.configFieldOriginKey(index); key != "" {
			if origin := state.working.Origins[key]; origin != "" {
				value += " · " + string(origin)
			}
		}
		return style.ModalOption(fmt.Sprintf("%-22s %s", label, value), index == state.cursor, width)
	}
	switch state.kind {
	case configAgents:
		if state.profileEditing && state.cursor >= 0 && state.cursor < len(state.working.Profiles) {
			profile := state.working.Profiles[state.cursor]
			rows := []struct{ label, value string }{{"Profile ID", string(profile.ID)}, {"Label", profile.Label}, {"Runtime", string(profile.Runtime)}}
			fields := model.profileEditorFields(profile)
			for _, field := range fields {
				rows = append(rows, struct{ label, value string }{field.Label, profileFieldValue(profile, field)})
			}
			lines := make([]string, 0, len(rows)+2)
			for i, row := range rows {
				if origin := state.working.Origins[profileEditorOriginKey(profile, fields, i)]; origin != "" {
					row.value += " · " + string(origin)
				}
				lines = append(lines, style.ModalOption(fmt.Sprintf("%-22s %s", row.label, row.value), i == state.profileCursor, width))
			}
			if state.profileCursor >= 3 && state.profileCursor-3 < len(fields) && fields[state.profileCursor-3].Description != "" {
				lines = append(lines, style.DisabledOption(fields[state.profileCursor-3].Description, width))
			}
			if probe, ok := state.probes[profile.ID]; ok {
				diagnostic := probe.Diagnostic
				if diagnostic == "" {
					diagnostic = "ready"
				}
				lines = append(lines, style.DisabledOption("Test: "+string(probe.State)+" · "+diagnostic, width))
			}
			return lines
		}
		lines := make([]string, 0, len(state.working.Profiles))
		for index, profile := range state.working.Profiles {
			readiness := "not probed"
			if result, ok := state.probes[profile.ID]; ok {
				readiness = string(result.State)
				if result.Version != "" {
					readiness += " " + result.Version
				}
				if result.Diagnostic != "" {
					readiness += " · " + result.Diagnostic
				}
			}
			label := fmt.Sprintf("%s · %s", profile.Label, profile.Runtime)
			value := fmt.Sprintf("%s · auth %s", readiness, nonemptySetting(profile.AuthenticationRef, "provider-owned"))
			lines = append(lines, option(index, label, value))
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
			option(6, "Max attempts", fmt.Sprint(state.working.Fix.MaxAttempts)),
			option(7, "Attempt timeout", state.working.Fix.AttemptTimeout.String()),
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
		return []string{
			option(0, "Running agents", fmt.Sprint(state.working.Concurrency.MaxAgents)),
			option(1, "Running verifiers", fmt.Sprint(state.working.Concurrency.MaxVerifiers)),
			option(2, "Retained jobs", fmt.Sprint(state.working.Concurrency.MaxRetainedJobs)),
			option(3, "Transcript bytes", fmt.Sprint(state.working.Concurrency.MaxTranscriptBytes)),
		}
	case configValidation:
		lines := make([]string, 0, len(state.working.Validation))
		for index, plan := range state.working.Validation {
			mark := " "
			if plan.ID == state.working.Fix.ValidationPlan {
				mark = "x"
			}
			lines = append(lines, option(index, "["+mark+"] "+plan.ID, fmt.Sprintf("%d trusted checks", len(plan.Checks))))
		}
		return lines
	case configDelivery:
		return []string{
			option(0, "Default mode", string(state.working.Delivery.DefaultMode)), option(1, "Remote", state.working.Delivery.Remote),
			option(2, "Base branch", nonemptySetting(state.working.Delivery.BaseBranch, "not set")),
			option(3, "Branch template", state.working.Delivery.BranchTemplate), option(4, "Publisher", state.working.Delivery.Publisher),
			option(5, "Draft pull requests", yesNo(state.working.Delivery.DraftPullRequests)),
		}
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
		keys := []string{"fix.target_score", "fix.change_scope", "fix.profile", "fix.model", "fix.effort", "fix.delegation", "fix.max_attempts", "fix.attempt_timeout"}
		if index < len(keys) {
			return keys[index]
		}
		metrics := scoring.Metrics()
		metricIndex := index - len(keys)
		if metricIndex >= 0 && metricIndex < len(metrics) {
			return "fix.focus." + string(metrics[metricIndex].ID)
		}
	case configConcurrency:
		keys := []string{"concurrency.max_agents", "concurrency.max_verifiers", "concurrency.max_retained_jobs", "concurrency.max_transcript_bytes"}
		if index < len(keys) {
			return keys[index]
		}
	case configDelivery:
		keys := []string{"delivery.default_mode", "delivery.remote", "delivery.base_branch", "delivery.branch_template", "delivery.publisher", "delivery.draft_pull_requests"}
		if index < len(keys) {
			return keys[index]
		}
	case configValidation:
		if index >= 0 && index < len(state.working.Validation) {
			return "validation." + state.working.Validation[index].ID
		}
	}
	return ""
}

func cloneConfigResolved(value appconfig.Resolved) appconfig.Resolved {
	result := value
	result.Origins = make(map[string]appconfig.Origin, len(value.Origins))
	for key, origin := range value.Origins {
		result.Origins[key] = origin
	}
	result.Fix = cloneConfigFix(value.Fix)
	result.Profiles = cloneConfigProfiles(value.Profiles)
	result.Validation = append(result.Validation[:0:0], value.Validation...)
	for index := range result.Validation {
		result.Validation[index].Checks = append(result.Validation[index].Checks[:0:0], value.Validation[index].Checks...)
		for checkIndex := range result.Validation[index].Checks {
			result.Validation[index].Checks[checkIndex].Arguments = append([]string(nil), value.Validation[index].Checks[checkIndex].Arguments...)
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
