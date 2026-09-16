package follow

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/style"
)

type configSettingsKind string

const (
	configAgents      configSettingsKind = "agents"
	configFix         configSettingsKind = "fix"
	configConcurrency configSettingsKind = "concurrency"
	configDelivery    configSettingsKind = "delivery"
)

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

func resizeMasterPromptTextBox(state *configSettingsState, width, height int) {
	errorRows := 0
	if state.promptError != "" {
		errorRows = 1
	}
	state.prompt.SetWidth(max(1, width))
	state.prompt.SetHeight(max(1, height-2-errorRows))
}

func profileDescriptor(catalog agent.ProfileCatalog, profile agent.Profile) (agent.ProfileDescriptor, error) {
	if catalog == nil {
		return agent.ProfileDescriptor{}, errors.New("agent profile schema is unavailable")
	}
	return catalog.Descriptor(profile.Runtime)
}

func (state configSettingsState) profileFieldCount(catalog agent.ProfileCatalog) int {
	index := state.selectedProfileIndex()
	if index < 0 {
		return 0
	}
	return len(state.profileEditorFields(catalog, state.working.Profiles[index]))
}

func (state configSettingsState) profileEditorFields(catalog agent.ProfileCatalog, profile agent.Profile) []agent.ProfileField {
	descriptor, err := profileDescriptor(catalog, profile)
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

func (state configSettingsState) selectedProfileIndex() int {
	if !state.profileEditing {
		return -1
	}
	return profileIndexForRuntime(state.working.Profiles, state.providerRuntime, state.working.Fix.Profile)
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

func (state *configSettingsState) rollbackPendingAgentEdit() {
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

func (state *configSettingsState) beginPendingAgentChange() {
	if state.pendingChange {
		return
	}
	state.pendingChange = true
	state.pendingWasDirty = state.dirty
	state.pendingDefault = state.defaultChanged
}

func (state *configSettingsState) beginPendingAgentEdit(profile agent.Profile) {
	state.beginPendingAgentChange()
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

func (state configSettingsState) textEditable() bool {
	cursor := state.cursor
	if state.profileEditing {
		return true
	}
	if state.kind == configDelivery {
		field := deliverySettingField(state.working.Delivery, cursor)
		return field == deliverySettingRemote || field == deliverySettingBase || field == deliverySettingBranch ||
			field == deliverySettingCommitTitle || field == deliverySettingCommitBody || field == deliverySettingPRTitle || field == deliverySettingPRBody
	}
	return false
}

func (state *configSettingsState) beginText(catalog agent.ProfileCatalog) {
	value := ""
	if state.profileEditing {
		index := state.selectedProfileIndex()
		if index < 0 {
			return
		}
		profile := state.working.Profiles[index]
		fields := state.profileEditorFields(catalog, profile)
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

func (state *configSettingsState) commitText(catalog agent.ProfileCatalog) error {
	value := strings.TrimSpace(state.input.Value())
	if strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("value contains invalid control characters")
	}
	if state.profileEditing {
		index := state.selectedProfileIndex()
		if index < 0 {
			return errors.New("agent connection is unavailable")
		}
		profile := &state.working.Profiles[index]
		fields := state.profileEditorFields(catalog, *profile)
		if state.editField < 0 || state.editField >= len(fields) {
			return errors.New("agent connection setting is unavailable")
		}
		field := fields[state.editField]
		if err := validateProfileFieldValue(field, value); err != nil {
			return err
		}
		candidate := cloneConfigProfile(*profile)
		setProfileFieldValue(&candidate, field, value)
		if catalog != nil {
			if err := catalog.ValidateProfile(candidate); err != nil {
				return err
			}
		}
		state.beginPendingAgentEdit(*profile)
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
