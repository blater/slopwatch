package follow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	userprefs "github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/style"
	"github.com/blater/slopwatch/internal/validation"
)

func TestFeatureSettingsLoadAsynchronouslyAndOwnKeyboard(t *testing.T) {
	t.Parallel()
	store := &settingsConfigStore{resolved: settingsResolved()}
	model := &Model{
		width: 80, height: 24, mainView: MainViewFiles, settings: true,
		settingsCursor: settingsIndex("delivery"), configStore: store,
		configWorkspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo"},
	}
	updated, command := model.handleSettingsKey("enter")
	result := updated.(*Model)
	if command == nil || !result.configSettings.open || !result.configSettings.loading || result.settings {
		t.Fatalf("opening feature settings = %+v, command nil=%t", result.configSettings, command == nil)
	}
	if store.resolveCalls != 0 {
		t.Fatal("configuration resolved synchronously on the Bubble Tea update path")
	}
	message := command()
	updated, _ = result.Update(message)
	result = updated.(*Model)
	if result.configSettings.loading || result.configSettings.working.Revision != 7 || store.resolveCalls != 1 {
		t.Fatalf("resolved state = %+v, calls=%d", result.configSettings, store.resolveCalls)
	}

	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	result = updated.(*Model)
	if result.mainView != MainViewFiles || !result.configSettings.open {
		t.Fatal("Tab escaped the feature settings overlay")
	}
	result.configSettings.cursor = 1
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	if !result.configSettings.editing {
		t.Fatal("Enter did not transfer ownership to the text input")
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	result = updated.(*Model)
	if !result.configSettings.open || !result.configSettings.editing || !strings.Contains(result.configSettings.input.Value(), "q") {
		t.Fatal("printable key escaped an active settings text input")
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	result = updated.(*Model)
	if result.configSettings.editing || !result.configSettings.open {
		t.Fatal("first Escape did not leave inline editing only")
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	result = updated.(*Model)
	if result.configSettings.open || !result.settings {
		t.Fatal("second Escape did not restore Settings")
	}
}

func TestLegacyDashboardDefaultsCarryValidFeatureDefaults(t *testing.T) {
	t.Parallel()
	value := defaultUserPreferences()
	if value.Fix.TargetScore != 100 || value.Fix.ChangeScope != "targets-and-tests" ||
		value.Concurrency.MaxAgents != 2 || value.Delivery.Publisher != "github-cli" {
		t.Fatalf("dashboard defaults lost feature properties: %#v", value)
	}
}

func TestLegacyDashboardSavePreservesFeatureGroups(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "preferences.toml")
	value := defaultUserPreferences()
	value.Agents.Profiles = []userprefs.AgentProfile{{ID: "codex", Runtime: "codex", Executable: "codex", Options: map[string]string{"sandbox": "strict"}}}
	value.Fix.Profile = "codex"
	if err := userprefs.Save(path, value); err != nil {
		t.Fatal(err)
	}
	model := &Model{
		preferencesPath: path, preferences: defaultUserPreferences(), theme: style.ThemeLight,
		visible: preferenceColumns(value), sortKey: value.Table.SortBy, sortReverse: value.Table.SortDescending,
		weights: preferenceWeights(value), weightEnabled: preferenceWeightEnabled(value),
		weightStep: value.Scoring.WeightStep, maximumWeight: value.Scoring.MaximumWeight,
	}
	model.persistUserPreferences()
	loaded, err := userprefs.LoadOrCreate(path, defaultUserPreferences())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Agents.Profiles) != 1 || loaded.Agents.Profiles[0].Options["sandbox"] != "strict" || loaded.Fix.Profile != "codex" {
		t.Fatalf("legacy save overwrote feature groups: %#v", loaded)
	}
}

func TestFeatureSettingsValidateBeforeAsyncSave(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Concurrency.MaxAgents = 0
	model := settingsModel(configConcurrency, resolved, &settingsConfigStore{resolved: settingsResolved()})
	model.configSettings.dirty = true
	updated, command := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	result := updated.(*Model)
	if command != nil || !strings.Contains(result.configSettings.status, "Cannot save") {
		t.Fatalf("invalid save status=%q command=%v", result.configSettings.status, command)
	}
}

func TestFeatureSettingsSaveReportsConflictAndServiceErrors(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		err  error
		want string
	}{
		"conflict": {fmt.Errorf("wrapped: %w", appconfig.ErrRevisionConflict), "Save conflict"},
		"service":  {errors.New("disk unavailable"), "Save failed: disk unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &settingsConfigStore{resolved: settingsResolved(), saveErr: test.err}
			model := settingsModel(configConcurrency, settingsResolved(), store)
			model.configSettings.working.Concurrency.MaxAgents++
			model.configSettings.dirty = true
			updated, command := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
			result := updated.(*Model)
			if command == nil || store.saveCalls != 0 || !result.configSettings.saving {
				t.Fatal("save did not remain asynchronous")
			}
			updated, _ = result.Update(command())
			result = updated.(*Model)
			if result.configSettings.saving || !strings.Contains(result.configSettings.status, test.want) || store.saveCalls != 1 {
				t.Fatalf("save result status=%q calls=%d", result.configSettings.status, store.saveCalls)
			}
			if store.expected != 7 || store.workspace.Repository != "repo" || store.scope != appconfig.ScopeUser {
				t.Fatalf("save contract = revision %d workspace %+v scope %q", store.expected, store.workspace, store.scope)
			}
		})
	}
}

func TestAgentSettingsOfferSafeCodexDefaultAndShowReadiness(t *testing.T) {
	t.Parallel()
	model := settingsModel(configAgents, settingsResolved(), &settingsConfigStore{})
	model.width = 120
	model.configSettings.working.Profiles = nil
	model.addDefaultAgentProfile()
	profiles := model.configSettings.working.Profiles
	if len(profiles) != 1 || profiles[0].Runtime != "codex-cli" || profiles[0].Executable != "codex" || profiles[0].AuthenticationRef != "provider-owned" {
		t.Fatalf("Codex default = %#v", profiles)
	}
	model.configSettings.probes["codex"] = agent.ProbeResult{State: agent.ProbeReady, Version: "1.2.3", Authentication: agent.Authentication{Method: "chatgpt", Label: "Signed in with ChatGPT"}, Capabilities: agent.Capabilities{Isolation: agent.RuntimeIsolation{
		Writes: agent.CandidateTreeAndGitMetadataProtected, SensitiveReadsDenied: true, TransportAuthIsolated: true, CrashContainment: true,
	}}}
	text := ansi.Strip(model.configSettingsView())
	for _, fragment := range []string{"Codex", "managed sign-in", "DEFAULT", "ready 1.", "D set default"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("agent settings missing %q: %q", fragment, text)
		}
	}
	model.configSettings.profileEditing = true
	if text := ansi.Strip(strings.Join(model.configSettingsLines(160), "\n")); !strings.Contains(text, "Signed in with ChatGPT") {
		t.Fatalf("agent settings omitted sanitized account method: %q", text)
	}
}

func TestAgentTestStatusWrapsInsidePopupAndRemainsVisible(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = resolved.Profiles[:1]
	model := settingsModel(configAgents, resolved, &settingsConfigStore{})
	model.width, model.height = 54, 16
	model.configSettings.probes[resolved.Profiles[0].ID] = agent.ProbeResult{
		State:      agent.ProbeDegraded,
		Diagnostic: "Codex confinement gates failed: crash containment unavailable at /a/very/long/unbroken/provider/protocol/path; run Test again after correcting the installation",
	}

	text := ansi.Strip(model.configSettingsView())
	for _, fragment := range []string{"Test: NOT RUNNABLE", "confinement gates failed", "/a/very/long/unbroken/provider/protocol/path", "run Test again", "installation"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("wrapped Test status hid %q: %q", fragment, text)
		}
	}
}

func TestAgentSettingsAddAdapterDefinedProfilesWithoutImplementationNoise(t *testing.T) {
	t.Parallel()
	model := settingsModel(configAgents, settingsResolved(), &settingsConfigStore{})
	model.profileCatalog = &multiRuntimeProfileServices{}
	model.configSettings.working.Profiles = nil
	model.addDefaultAgentProfile()
	model.addDefaultAgentProfile()
	profiles := model.configSettings.working.Profiles
	if len(profiles) != 2 || profiles[0].Runtime != "codex-cli" || profiles[1].Runtime != "openai-responses" ||
		profiles[1].Executable != "" || profiles[1].AuthenticationRef != "env:OPENAI_API_KEY" {
		t.Fatalf("adapter defaults = %#v", profiles)
	}
	model.configSettings.cursor = 0
	model.configSettings.profileEditing = true
	text := ansi.Strip(strings.Join(model.configSettingsLines(120), "\n"))
	for _, hidden := range []string{"Runtime", "Executable", "Probe timeout", "Cancellation grace", "Additional denied roots"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("Agents popup exposed preferences-only field %q: %q", hidden, text)
		}
	}
	if !strings.Contains(text, "Authentication") {
		t.Fatalf("Agents popup hid the user-owned authentication choice: %q", text)
	}
}

func TestAgentSettingsCanExplicitlySetAndSaveDefaultProfile(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Label: "Codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Label: "OpenAI", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
	}
	resolved.Fix.Profile = "api"
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	model.configSettings.cursor = 0
	model.configSettings.probes["codex"] = agent.ProbeResult{State: agent.ProbeReady, Capabilities: agent.Capabilities{
		Models:     []agent.Option[agent.ModelID]{{ID: "codex-model", Default: true}},
		Efforts:    []agent.Option[agent.EffortID]{{ID: "high", Default: true}},
		Delegation: []agent.Option[agent.DelegationMode]{{ID: agent.DelegationSingle, Default: true}},
	}}
	updated, command := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	result := updated.(*Model)
	if command != nil || result.configSettings.working.Fix.Profile != "codex" || result.configSettings.working.Fix.Model != "codex-model" || !result.configSettings.dirty {
		t.Fatalf("set default state=%+v command=%v", result.configSettings, command)
	}
	save := result.saveConfigSettings()
	if save == nil {
		t.Fatal("setting default did not produce a save command")
	}
	result.handleConfigSaved(save().(configSavedMsg))
	if store.resolved.Fix.Profile != "codex" || store.resolved.Fix.Model != "codex-model" {
		t.Fatalf("saved Fix defaults=%#v", store.resolved.Fix)
	}
}

func TestUnprobedDefaultSwitchPersistsRuntimeDefaults(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
	}
	resolved.Fix.Profile, resolved.Fix.Model, resolved.Fix.Effort, resolved.Fix.Delegation = "codex", "codex-model", "high", agent.DelegationSingle
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	model.configSettings.cursor = 1
	model.setSelectedAgentDefault()
	if model.configSettings.working.Fix.Model != "" || model.configSettings.working.Fix.Effort != "" || model.configSettings.working.Fix.Delegation != agent.DelegationSingle {
		t.Fatalf("untested adapter retained foreign selections: %#v", model.configSettings.working.Fix)
	}
	save := model.saveConfigSettings()
	if save == nil {
		t.Fatal("unprobed default switch did not save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if store.resolved.Fix.Profile != "api" || store.resolved.Fix.Model != "" || store.resolved.Fix.Effort != "" {
		t.Fatalf("saved runtime defaults=%#v", store.resolved.Fix)
	}
}

func TestFailedProbeDefaultSwitchDoesNotReuseItsCapabilities(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
	}
	resolved.Fix.Profile, resolved.Fix.Model, resolved.Fix.Effort, resolved.Fix.Delegation = "codex", "codex-model", "high", agent.DelegationSingle
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	model.configSettings.cursor = 1
	model.configSettings.probes["api"] = agent.ProbeResult{
		State: agent.ProbeUnauthenticated,
		Capabilities: agent.Capabilities{
			Models:  []agent.Option[agent.ModelID]{{ID: "api-model", Default: true}},
			Efforts: []agent.Option[agent.EffortID]{{ID: "medium", Default: true}},
		},
	}

	model.setSelectedAgentDefault()
	if got := model.configSettings.working.Fix; got.Profile != "api" || got.Model != "" || got.Effort != "" || got.Delegation != agent.DelegationSingle {
		t.Fatalf("failed probe was treated as a usable capability catalog: %#v", got)
	}
	save := model.saveConfigSettings()
	if save == nil {
		t.Fatal("failed-probe default switch did not save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if got := store.resolved.Fix; got.Profile != "api" || got.Model != "" || got.Effort != "" || got.Delegation != agent.DelegationSingle {
		t.Fatalf("failed-probe selections were persisted: %#v", got)
	}
}

func TestFixSettingsProfileCycleClearsForeignRuntimeChoices(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
	}
	resolved.Fix.Profile, resolved.Fix.Model, resolved.Fix.Effort, resolved.Fix.Delegation = "codex", "codex-model", "high", agent.DelegationMode("team")

	if !adjustFixSetting(&resolved, 2, 1, map[agent.ProfileID]agent.ProbeResult{}) {
		t.Fatal("Fix Settings profile row did not change")
	}
	if resolved.Fix.Profile != "api" || resolved.Fix.Model != "" || resolved.Fix.Effort != "" || resolved.Fix.Delegation != agent.DelegationSingle {
		t.Fatalf("Fix Settings retained choices belonging to the previous runtime: %#v", resolved.Fix)
	}
}

func TestPreferencesReloadClearsAgentProbeCatalog(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	model := settingsModel(configAgents, resolved, &settingsConfigStore{})
	model.configSettings.probes["codex"] = agent.ProbeResult{State: agent.ProbeReady}
	resolved.Profiles[0].Executable = "/configured/in/preferences"
	model.handleConfigResolved(configResolvedMsg{generation: model.configSettings.generation, resolved: resolved})
	if len(model.configSettings.probes) != 0 {
		t.Fatalf("preferences reload retained probes for an old execution definition: %#v", model.configSettings.probes)
	}
}

func TestAuthenticationEditInvalidatesProbeBeforeProfileBecomesDefault(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "primary", Label: "Primary API", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
		{ID: "candidate", Label: "Candidate", Runtime: "openai-responses", AuthenticationRef: "env:OLD_OPENAI_KEY"},
	}
	resolved.Fix.Profile, resolved.Fix.Model, resolved.Fix.Effort, resolved.Fix.Delegation = "primary", "primary-model", "high", agent.DelegationSingle
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	services := &multiRuntimeProfileServices{}
	model.profileCatalog = services
	model.profileProber = services
	model.configSettings.probes["candidate"] = agent.ProbeResult{State: agent.ProbeReady, Capabilities: agent.Capabilities{
		Models:     []agent.Option[agent.ModelID]{{ID: "stale-api-model", Default: true}},
		Efforts:    []agent.Option[agent.EffortID]{{ID: "high", Default: true}},
		Delegation: []agent.Option[agent.DelegationMode]{{ID: agent.DelegationSingle, Default: true}},
	}}
	model.configSettings.profileEditing = true
	model.configSettings.cursor = 1
	model.configSettings.profileCursor = 2
	staleProbe := model.testSelectedProfileCommand()
	if staleProbe == nil {
		t.Fatal("old runtime did not produce a probe command")
	}

	model.configSettings.editField = 2
	model.configSettings.input.SetValue("env:NEW_OPENAI_KEY")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	if _, exists := model.configSettings.probes["candidate"]; exists {
		t.Fatal("authentication change retained the old adapter probe")
	}
	_, _ = handleMessage(model, staleProbe())
	if _, exists := model.configSettings.probes["candidate"]; exists {
		t.Fatal("late probe for the old authentication definition was accepted after the edit")
	}
	model.configSettings.profileEditing = false
	model.setSelectedAgentDefault()
	if got := model.configSettings.working.Fix; got.Profile != "candidate" || got.Model != "" || got.Effort != "" || got.Delegation != agent.DelegationSingle {
		t.Fatalf("new default reused the old runtime's capability choices: %#v", got)
	}
	save := model.saveConfigSettings()
	if save == nil {
		t.Fatal("authentication/default change did not produce a save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	reloaded := settingsModel(configAgents, store.resolved, store)
	if got := reloaded.configSettings.working.Fix; got.Profile != "candidate" || got.Model != "" || got.Effort != "" || got.Delegation != agent.DelegationSingle {
		t.Fatalf("reloaded settings retained a stale adapter selection: %#v", got)
	}
}

func TestOnlyBuiltInCodexProfileCarriesRecommendation(t *testing.T) {
	t.Parallel()
	builtIn := agentProfileChoiceLabel(agent.Profile{ID: "codex-default", Runtime: "codex-cli"})
	custom := agentProfileChoiceLabel(agent.Profile{ID: "work", Runtime: "codex-cli"})
	if !strings.Contains(builtIn, "recommended") || strings.Contains(custom, "recommended") {
		t.Fatalf("Codex choice labels: built-in=%q custom=%q", builtIn, custom)
	}
}

func TestRenamingDefaultAgentPersistsUpdatedFixReference(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	model.configSettings.profileEditing = true
	model.configSettings.cursor = 0
	model.configSettings.editField = 0
	model.configSettings.input.SetValue("renamed-codex")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	if model.configSettings.working.Fix.Profile != "renamed-codex" || !model.configSettings.defaultChanged {
		t.Fatalf("renamed default state=%+v", model.configSettings)
	}
	save := model.saveConfigSettings()
	if save == nil {
		t.Fatal("renamed default did not save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if store.resolved.Fix.Profile != "renamed-codex" {
		t.Fatalf("saved default reference=%q", store.resolved.Fix.Profile)
	}
}

func TestAPIBackedAgentProfileSavesAndReloadsWithoutExecutable(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	model.profileCatalog = &multiRuntimeProfileServices{}
	model.configSettings.working.Profiles = []agent.Profile{{
		ID: "gpt", Label: "OpenAI Responses API", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY",
	}}
	model.configSettings.dirty = true

	save := model.saveConfigSettings()
	if save == nil || !model.configSettings.saving {
		t.Fatal("API-backed profile was rejected before asynchronous save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if model.configSettings.saving || model.configSettings.dirty || store.saveCalls != 1 {
		t.Fatalf("save state=%+v calls=%d", model.configSettings, store.saveCalls)
	}
	if got := store.resolved.Profiles; len(got) != 1 || got[0].Runtime != "openai-responses" || got[0].Executable != "" {
		t.Fatalf("saved API profile=%#v", got)
	}

	reload := model.reloadConfigSettings()
	if reload == nil {
		t.Fatal("reload did not remain asynchronous")
	}
	model.handleConfigResolved(reload().(configResolvedMsg))
	got := model.configSettings.working.Profiles
	if len(got) != 1 || got[0].ID != "gpt" || got[0].AuthenticationRef != "env:OPENAI_API_KEY" || got[0].Executable != "" {
		t.Fatalf("reloaded API profile=%#v", got)
	}
}

func TestAgentSettingsExposeTestDiagnosticsAndCanRemoveUnknownOption(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "codex", Label: "Codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned", Options: map[string]string{"obsolete": "bad"}}}
	model := settingsModel(configAgents, resolved, &settingsConfigStore{})
	services := &settingsProfileServices{}
	model.profileCatalog = services
	model.profileProber = services

	command := model.testSelectedProfileCommand()
	if command == nil {
		t.Fatal("explicit profile test returned no command")
	}
	message := command().(configProbeMsg)
	if message.result.State != agent.ProbeUnauthenticated || !strings.Contains(message.result.Diagnostic, "codex login") {
		t.Fatalf("profile test = %#v", message.result)
	}
	model.configSettings.probes[message.profile] = message.result
	if text := ansi.Strip(model.configSettingsView()); !strings.Contains(text, "codex login") {
		t.Fatalf("profile diagnostic not rendered: %q", text)
	}

	model.configSettings.profileEditing = true
	fields := model.profileEditorFields(model.configSettings.working.Profiles[0])
	if len(fields) != 2 || fields[1].OptionKey != "obsolete" {
		t.Fatalf("repair fields = %#v", fields)
	}
	model.configSettings.editField = 3
	model.configSettings.input.SetValue("")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	if _, exists := model.configSettings.working.Profiles[0].Options["obsolete"]; exists {
		t.Fatal("unsupported option was not removed")
	}
}

type settingsProfileServices struct{}

func (*settingsProfileServices) Kinds() []agent.RuntimeKind { return []agent.RuntimeKind{"codex-cli"} }
func (*settingsProfileServices) Descriptor(kind agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	if kind != "codex-cli" {
		return agent.ProfileDescriptor{}, fmt.Errorf("unknown runtime %s", kind)
	}
	return agent.ProfileDescriptor{Runtime: kind, Label: "Codex", Fields: []agent.ProfileField{
		{Key: "executable", Label: "Executable", Kind: agent.ProfileFieldExecutable, Required: true, PreferencesOnly: true},
		{Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference, Required: true, Description: "Run `codex login` to authorize."},
		{Key: "options.probe_timeout", OptionKey: "probe_timeout", Label: "Probe timeout", Kind: agent.ProfileFieldText, PreferencesOnly: true},
		{Key: "options.termination_grace", OptionKey: "termination_grace", Label: "Cancellation grace", Kind: agent.ProfileFieldText, PreferencesOnly: true},
	}}, nil
}
func (*settingsProfileServices) ValidateProfile(profile agent.Profile) error {
	for key := range profile.Options {
		return fmt.Errorf("unsupported option %s", key)
	}
	return nil
}
func (*settingsProfileServices) Probe(_ context.Context, profile agent.Profile) agent.ProbeResult {
	return agent.ProbeResult{Runtime: profile.Runtime, State: agent.ProbeUnauthenticated, Diagnostic: "Run codex login to authorize"}
}

type multiRuntimeProfileServices struct{ settingsProfileServices }

func (*multiRuntimeProfileServices) Kinds() []agent.RuntimeKind {
	return []agent.RuntimeKind{"codex-cli", "openai-responses"}
}
func (*multiRuntimeProfileServices) Descriptor(kind agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	if kind == "openai-responses" {
		return agent.ProfileDescriptor{Runtime: kind, Label: "OpenAI Responses API", Fields: []agent.ProfileField{{
			Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference,
			Required: true, Default: "env:OPENAI_API_KEY",
		}}}, nil
	}
	return (&settingsProfileServices{}).Descriptor(kind)
}

func TestFeatureSettingsRenderAllSectionsAtResponsiveSizes(t *testing.T) {
	t.Parallel()
	sections := map[configSettingsKind]string{
		configAgents: "AGENTS", configFix: "FIX DEFAULTS", configConcurrency: "CONCURRENCY & RETENTION",
		configValidation: "VALIDATION", configDelivery: "GIT & PULL REQUESTS",
	}
	for _, size := range [][2]int{{40, 10}, {72, 16}, {120, 30}} {
		for kind, heading := range sections {
			model := settingsModel(kind, settingsResolved(), &settingsConfigStore{})
			model.width, model.height = size[0], size[1]
			text := ansi.Strip(model.configSettingsView())
			if !strings.Contains(text, heading) || !strings.Contains(text, "save") {
				t.Fatalf("%s at %dx%d = %q", kind, size[0], size[1], text)
			}
		}
	}
}

func TestCompactSettingsEditorKeepsApplyAndCancelFooterVisible(t *testing.T) {
	model := settingsModel(configDelivery, settingsResolved(), &settingsConfigStore{})
	model.width, model.height = 36, 6
	model.configSettings.cursor = 3
	model.beginConfigText()
	view := model.configSettingsFullScreen()
	assertScreenSize(t, view, 36, 6)
	plain := ansi.Strip(view)
	for _, wanted := range []string{"Edit:", "Enter apply", "Esc cancel"} {
		if !strings.Contains(plain, wanted) {
			t.Fatalf("compact active editor omitted %q: %q", wanted, plain)
		}
	}
}

func TestSettingsExposeFixedPoliciesTrustedCommandsAndFocusedSources(t *testing.T) {
	resolved := settingsResolved()
	resolved.Fix.PromptTemplate = "default"
	resolved.Delivery.CleanupPolicy = "retain"
	resolved.Delivery.CommitPolicy = "on-publish"
	resolved.Validation = []validation.Plan{{ID: "release", Checks: []validation.Check{{
		ID: "test", Label: "Go test", Executable: "/usr/bin/go", Arguments: []string{"test", "./..."},
		WorkingDirectory: "module", Required: true, Timeout: 10 * time.Minute, MaxOutputBytes: 4096,
	}}}}
	resolved.Origins["validation.release.checks.test"] = appconfig.OriginRepository

	validationModel := settingsModel(configValidation, resolved, &settingsConfigStore{})
	validationModel.configSettings.cursor = 18 // trusted command row
	validationText := ansi.Strip(strings.Join(validationModel.configSettingsLines(100), "\n"))
	for _, wanted := range []string{"Go test command", "/usr/bin/go", `"./..."`, "installation-owned", "working dir", "module"} {
		if !strings.Contains(validationText, wanted) {
			t.Fatalf("validation settings hid %q: %q", wanted, validationText)
		}
	}
	if footer := validationModel.configSettingsFooter(); !strings.Contains(footer, "Read-only in this release") {
		t.Fatalf("trusted command footer implied editing: %q", footer)
	}
	if source := validationModel.configFocusedSourceNote(); !strings.Contains(source, "Repo override") || !strings.Contains(source, "saves user default only") {
		t.Fatalf("repository precedence consequence=%q", source)
	}
	validationModel.configSettings.cursor = 20 // timeout row
	if footer := validationModel.configSettingsFooter(); !strings.Contains(footer, "Enter edit") {
		t.Fatalf("editable validation limit footer=%q", footer)
	}

	deliveryModel := settingsModel(configDelivery, resolved, &settingsConfigStore{})
	for _, cursor := range []int{4, 12, 13} {
		deliveryModel.configSettings.cursor = cursor
		before := deliveryModel.configSettings.working.Delivery
		deliveryModel.adjustConfigSetting(1)
		if deliveryModel.configSettings.working.Delivery != before || deliveryModel.configSettings.dirty {
			t.Fatalf("fixed delivery row %d behaved as editable", cursor)
		}
		if footer := deliveryModel.configSettingsFooter(); !strings.Contains(footer, "Read-only in this release") {
			t.Fatalf("fixed delivery row %d footer=%q", cursor, footer)
		}
	}
	deliveryModel.configSettings.cursor = 12
	deliveryText := ansi.Strip(strings.Join(deliveryModel.configSettingsLines(100), "\n"))
	for _, wanted := range []string{"only supported v1 adapter", "retain · fixed v1", "on-publish · fixed v1", "until explicit Discard or Cleanup"} {
		if !strings.Contains(deliveryText, wanted) {
			t.Fatalf("delivery settings hid %q: %q", wanted, deliveryText)
		}
	}
	deliveryModel.configSettings.cursor = 9
	if text := ansi.Strip(strings.Join(deliveryModel.configSettingsLines(100), "\n")); !strings.Contains(text, "Single-line body template") || !strings.Contains(text, "line breaks") {
		t.Fatalf("delivery body-template constraint was hidden: %q", text)
	}

	fixModel := settingsModel(configFix, resolved, &settingsConfigStore{})
	fixModel.configSettings.cursor = 6
	if text := ansi.Strip(strings.Join(fixModel.configSettingsLines(100), "\n")); !strings.Contains(text, "Prompt strategy") || !strings.Contains(text, "fixed v1") || !strings.Contains(text, "non-editable safety envelope") {
		t.Fatalf("fixed prompt strategy was unclear: %q", text)
	}
	if footer := fixModel.configSettingsFooter(); !strings.Contains(footer, "Read-only in this release") {
		t.Fatalf("fixed prompt footer=%q", footer)
	}
}

func TestDeliverySettingsDoNotSilentlyClipOrganisationBranchTemplate(t *testing.T) {
	resolved := settingsResolved()
	store := &settingsConfigStore{resolved: resolved}
	model := &Model{width: 80, height: 24, configStore: store, configWorkspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo"}}
	load := model.openConfigSettings(configDelivery)
	model.handleConfigResolved(load().(configResolvedMsg))
	model.configSettings.cursor = 3
	model.beginConfigText()

	template := strings.Repeat("organisation/platform/", 16) + "{target-stem}-{job-short-id}"
	model.configSettings.input.SetValue(template)
	if got := model.configSettings.input.Value(); got != template {
		t.Fatalf("branch template was clipped to %d of %d bytes", len(got), len(template))
	}
	if err := model.commitConfigText(); err != nil {
		t.Fatalf("long organisation branch template was rejected: %v", err)
	}
	if got := model.configSettings.working.Delivery.BranchTemplate; got != template {
		t.Fatalf("committed branch template = %q, want %q", got, template)
	}
}

func TestSettingsShowAndMigrateEffectiveLegacyBranchTemplate(t *testing.T) {
	resolved := settingsResolved()
	resolved.Delivery.BranchTemplate = "slopwatch/fix-{job-short-id}"
	resolved.Fix.BranchTemplate = "organisation/legacy/{target-stem}-{job-short-id}"
	resolved.Origins["delivery.branch_template"] = appconfig.OriginBuiltIn
	resolved.Origins["fix.branch_template"] = appconfig.OriginUser
	model := settingsModel(configDelivery, resolved, &settingsConfigStore{})
	model.configSettings.cursor = 3

	text := ansi.Strip(strings.Join(model.configSettingsLines(120), "\n"))
	for _, wanted := range []string{"organisation/legacy", "legacy Fix preferences", "edit and save this row to migrate"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("legacy effective branch template omitted %q: %q", wanted, text)
		}
	}
	if source := model.configFocusedSourceNote(); source != "Source: user preferences" {
		t.Fatalf("legacy effective branch source=%q", source)
	}
	model.beginConfigText()
	if got := model.configSettings.input.Value(); got != resolved.Fix.BranchTemplate {
		t.Fatalf("migration editor started with %q, want effective %q", got, resolved.Fix.BranchTemplate)
	}
	if err := model.commitConfigText(); err != nil || model.configSettings.working.Delivery.BranchTemplate != resolved.Fix.BranchTemplate {
		t.Fatalf("legacy template migration value=%q err=%v", model.configSettings.working.Delivery.BranchTemplate, err)
	}
}

func TestValidationPlanSelectorReportsDefaultSelectionSource(t *testing.T) {
	resolved := settingsResolved()
	resolved.Origins["fix.validation_plan"] = appconfig.OriginCLI
	resolved.Origins["validation.go-test"] = appconfig.OriginRepository
	model := settingsModel(configValidation, resolved, &settingsConfigStore{})
	model.configSettings.cursor = 16 // first plan, after startup workspace/container constraints.
	if source := model.configFocusedSourceNote(); !strings.Contains(source, "CLI override") || strings.Contains(source, "Repo override") {
		t.Fatalf("validation default selector source=%q", source)
	}
}

func TestValidationWorkspaceConstraintsAreVisibleEditableAndExplained(t *testing.T) {
	resolved := settingsResolved()
	resolved.Origins["validation_workspace.max_files"] = appconfig.OriginUser
	model := settingsModel(configValidation, resolved, &settingsConfigStore{})
	text := ansi.Strip(strings.Join(model.configSettingsLines(100), "\n"))
	for _, wanted := range []string{"Workspace files", "100000", "Workspace directories", "Workspace path bytes", "Largest file bytes", "Workspace total bytes", "candidate copy", "fingerprinting", "Container processes", "Container memory bytes", "Container CPU millis", "Container /tmp bytes", "Container workspace bytes", "Container open files", "Generated file bytes", "Container stop timeout", "Docker control timeout", "Safety sentinel timeout", "Crash probe timeout"} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("validation workspace settings hid %q: %q", wanted, text)
		}
	}
	if footer := model.configSettingsFooter(); !strings.Contains(footer, "Enter edit") {
		t.Fatalf("workspace constraint was not editable: %q", footer)
	}
	model.beginConfigText()
	model.configSettings.input.SetValue("120000")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	if model.configSettings.working.ValidationWorkspace.MaxFiles != 120000 {
		t.Fatalf("workspace files=%d", model.configSettings.working.ValidationWorkspace.MaxFiles)
	}
	patch := configSettingsPatch(configValidation, model.configSettings.working)
	if patch.ValidationWorkspace == nil || patch.ValidationWorkspace.MaxFiles != 120000 {
		t.Fatalf("validation workspace patch=%#v", patch.ValidationWorkspace)
	}
	model.configSettings.cursor = 13 // Docker control timeout.
	model.beginConfigText()
	model.configSettings.input.SetValue("45s")
	if err := model.commitConfigText(); err != nil || model.configSettings.working.ValidationWorkspace.ContainerControlTimeout != 45*time.Second {
		t.Fatalf("control timeout=%s err=%v", model.configSettings.working.ValidationWorkspace.ContainerControlTimeout, err)
	}
	if consequence := validationWorkspaceConsequence(validationRowContainerControlTimeout); !strings.Contains(consequence, "lifecycle commands") || !strings.Contains(consequence, "must exceed stop") || !strings.Contains(consequence, "Next start only") || !strings.Contains(consequence, "restart") {
		t.Fatalf("control timeout consequence=%q", consequence)
	}
}

func TestValidationSettingsReportRestartOnlyForStartupPolicyChanges(t *testing.T) {
	t.Parallel()

	t.Run("workspace and container policy", func(t *testing.T) {
		t.Parallel()
		resolved := settingsResolved()
		store := &settingsConfigStore{resolved: resolved}
		model := settingsModel(configValidation, resolved, store)
		model.configSettings.working.ValidationWorkspace.MaxFiles++
		model.configSettings.dirty = true

		command := model.saveConfigSettings()
		if command == nil {
			t.Fatal("workspace policy save did not remain asynchronous")
		}
		message := command().(configSavedMsg)
		if !message.restartRequired {
			t.Fatal("workspace policy save did not report restart requirement")
		}
		model.handleConfigSaved(message)
		if !strings.Contains(model.configSettings.status, "Saved for next start") || !strings.Contains(model.configSettings.status, "restart Slopwatch") {
			t.Fatalf("workspace policy save status=%q", model.configSettings.status)
		}
	})

	t.Run("validation plan policy", func(t *testing.T) {
		t.Parallel()
		resolved := settingsResolved()
		store := &settingsConfigStore{resolved: resolved}
		model := settingsModel(configValidation, resolved, store)
		model.configSettings.working.Validation[0].Checks[0].Timeout = time.Minute
		model.configSettings.dirty = true

		command := model.saveConfigSettings()
		if command == nil {
			t.Fatal("validation plan save did not remain asynchronous")
		}
		message := command().(configSavedMsg)
		if message.restartRequired {
			t.Fatal("live validation plan save incorrectly reported restart requirement")
		}
		model.handleConfigSaved(message)
		if model.configSettings.status != "Saved" {
			t.Fatalf("validation plan save status=%q, want live Saved status", model.configSettings.status)
		}
	})
}

func TestValidationPolicySaveAndReturnLeavesFixBlockedUntilRestart(t *testing.T) {
	resolved := settingsResolved()
	store := &settingsConfigStore{resolved: resolved}
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 80, 24)
	model.configStore = store
	model.configWorkspace = fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo"}
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	load := model.openConfigSettings(configValidation)
	model.handleConfigResolved(load().(configResolvedMsg))
	model.configSettings.returnToFix = true
	model.overlays.Push(OverlayConfigSettings, OverlayCaller{MainView: MainViewFiles, Overlay: OverlayFixForm})
	model.configSettings.working.ValidationWorkspace.MaxFiles++
	model.configSettings.dirty = true
	model.configSettings.closeAfterSave = true
	service.prepareErr = errors.New("prepare fix: validation workspace or container settings changed after Slopwatch started; restart Slopwatch before preparing another Fix so the saved policy is enforced")

	save := model.saveConfigSettings()
	message := save().(configSavedMsg)
	reprepare := model.handleConfigSaved(message)
	if reprepare == nil || model.configSettings.open {
		t.Fatalf("save-and-return did not close Settings and recheck Fix: open=%t command=%v", model.configSettings.open, reprepare)
	}
	model.handleFixPrepared(reprepare().(fixPreparedMsg))
	if !model.hasOverlay(OverlayFixForm) || model.fixDialog.loading || model.fixDialog.statusText != "Preparation failed" ||
		!strings.Contains(model.fixDialog.errorText, "restart Slopwatch") || !strings.Contains(model.fixDialog.errorText, "saved policy is enforced") {
		t.Fatalf("Fix was not persistently restart-blocked after policy save: %+v", model.fixDialog)
	}
}

func TestSettingsExplainLiveLimitConsequencesAndFriendlyOverrideSources(t *testing.T) {
	resolved := settingsResolved()
	resolved.Origins["concurrency.max_agents"] = appconfig.OriginCLI
	model := settingsModel(configConcurrency, resolved, &settingsConfigStore{})
	model.configSettings.cursor = 0
	text := ansi.Strip(strings.Join(model.configSettingsLines(100), "\n"))
	if !strings.Contains(text, "lowering it never cancels running jobs") {
		t.Fatalf("agent limit consequence=%q", text)
	}
	if source := model.configFocusedSourceNote(); !strings.Contains(source, "CLI override") || !strings.Contains(source, "saves user default only") || strings.Contains(source, "built_in") {
		t.Fatalf("friendly CLI source=%q", source)
	}
	model.configSettings.cursor = 2
	if text := ansi.Strip(strings.Join(model.configSettingsLines(100), "\n")); !strings.Contains(text, "never deletes jobs") || !strings.Contains(text, "blocks admission") {
		t.Fatalf("retention consequence=%q", text)
	}
	model.configSettings.cursor = 3
	if text := ansi.Strip(strings.Join(model.configSettingsLines(100), "\n")); !strings.Contains(text, "Pinned at Prepare") || !strings.Contains(text, "exactly bounds JSON transcript-entry bytes per job") {
		t.Fatalf("transcript consequence=%q", text)
	}
	model.configSettings.cursor = 5
	if text := ansi.Strip(strings.Join(model.configSettingsLines(100), "\n")); !strings.Contains(text, "maximum candidate-file bytes") || !strings.Contains(text, "Truncation is") {
		t.Fatalf("candidate byte preview consequence=%q", text)
	}
	model.configSettings.cursor = 6
	if text := ansi.Strip(strings.Join(model.configSettingsLines(100), "\n")); !strings.Contains(text, "maximum candidate-file lines") || !strings.Contains(text, "Truncation is") {
		t.Fatalf("candidate line preview consequence=%q", text)
	}
}

type settingsConfigStore struct {
	resolved     appconfig.Resolved
	resolveErr   error
	saveErr      error
	resolveCalls int
	saveCalls    int
	workspace    fix.WorkspaceIdentity
	scope        appconfig.Scope
	expected     appconfig.Revision
}

func (store *settingsConfigStore) Resolve(_ context.Context, _ fix.WorkspaceIdentity, _ appconfig.SessionOverrides) (appconfig.Resolved, error) {
	store.resolveCalls++
	return cloneConfigResolved(store.resolved), store.resolveErr
}

func (store *settingsConfigStore) Save(_ context.Context, workspace fix.WorkspaceIdentity, scope appconfig.Scope, patch appconfig.Patch, expected appconfig.Revision) (appconfig.Saved, error) {
	store.saveCalls++
	store.workspace, store.scope, store.expected = workspace, scope, expected
	if store.saveErr != nil {
		return appconfig.Saved{}, store.saveErr
	}
	resolved := cloneConfigResolved(store.resolved)
	if patch.Profiles != nil {
		resolved.Profiles = cloneConfigProfiles(*patch.Profiles)
	}
	if patch.Fix != nil {
		resolved.Fix = cloneConfigFix(*patch.Fix)
	}
	if patch.Concurrency != nil {
		resolved.Concurrency = *patch.Concurrency
	}
	if patch.Validation != nil {
		resolved.Validation = cloneConfigValidation(*patch.Validation)
	}
	if patch.ValidationWorkspace != nil {
		resolved.ValidationWorkspace = *patch.ValidationWorkspace
	}
	resolved.Revision++
	store.resolved = cloneConfigResolved(resolved)
	return appconfig.Saved{Revision: resolved.Revision, Resolved: resolved}, nil
}

func settingsModel(kind configSettingsKind, resolved appconfig.Resolved, store ConfigStore) *Model {
	input := textinput.New()
	return &Model{
		width: 80, height: 24, configStore: store,
		configWorkspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo"},
		configSettings: configSettingsState{
			open: true, kind: kind, generation: 1, resolved: cloneConfigResolved(resolved),
			working: cloneConfigResolved(resolved), probes: map[agent.ProfileID]agent.ProbeResult{}, input: input,
		},
	}
}

func settingsResolved() appconfig.Resolved {
	return appconfig.Resolved{
		SchemaVersion: 1, Revision: 7,
		Origins: map[string]appconfig.Origin{
			"agents": appconfig.OriginUser, "fix": appconfig.OriginBuiltIn, "concurrency": appconfig.OriginUser,
			"validation": appconfig.OriginUser, "delivery": appconfig.OriginBuiltIn,
		},
		Fix: appconfig.FixDefaults{
			TargetScore: 100, ChangeScope: "targets-and-tests", Profile: "codex", Delegation: "single",
		},
		Concurrency: appconfig.Concurrency{MaxAgents: 2, MaxVerifiers: 1, MaxRetainedJobs: 100, MaxTranscriptBytes: 1024 * 1024, MaxActorsPerJob: 32, MaxCandidatePreviewBytes: 4 << 20, MaxCandidatePreviewLines: 5000},
		ValidationWorkspace: appconfig.ValidationWorkspace{MaxFiles: 100000, MaxDirectories: 20000, MaxPathBytes: 16 << 20, MaxFileBytes: 64 << 20, MaxTotalBytes: 512 << 20,
			ContainerPIDs: 256, ContainerMemoryBytes: 4 << 30, ContainerCPUMillis: 2000, ContainerTemporaryBytes: 1 << 30, ContainerWorkspaceBytes: 1 << 30,
			ContainerNofileLimit: 1024, ContainerGeneratedFileBytes: 64 << 20, ContainerStopTimeout: 3 * time.Second, ContainerControlTimeout: 30 * time.Second,
			ContainerSentinelTimeout: 10 * time.Second, ContainerCrashProbeTimeout: 15 * time.Second},
		Profiles:    []agent.Profile{{ID: "codex", Label: "Codex", Runtime: "codex", Executable: "codex", AuthenticationRef: "provider-owned"}},
		Validation:  []validation.Plan{{ID: "go-test", Checks: []validation.Check{{ID: "test", Executable: "go"}}}},
		Delivery:    appconfig.Delivery{DefaultMode: "candidate", Remote: "origin", BranchTemplate: "slopwatch/fix-{job}", Publisher: "github-cli", DraftPullRequests: true},
		TrendWindow: 10 * time.Minute,
	}
}
