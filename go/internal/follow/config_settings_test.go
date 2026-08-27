package follow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixprompt"
	userprefs "github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/style"
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
	result.configSettings.working.Delivery.DefaultPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitCommitNewBranch, Publish: fix.PublishPush}
	result.configSettings.cursor = 4 // Remote in the expanded delivery form.
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

func TestDashboardDefaultsCarryValidFeatureDefaults(t *testing.T) {
	t.Parallel()
	value := defaultUserPreferences()
	if value.Fix.TargetScore != 100 || value.Fix.ChangeScope != "repository" ||
		value.Concurrency.MaxAgents != 2 || value.Delivery.Publisher != "github-cli" {
		t.Fatalf("dashboard defaults lost feature properties: %#v", value)
	}
}

func TestDashboardSavePreservesFeatureGroups(t *testing.T) {
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
		t.Fatalf("dashboard save overwrote feature groups: %#v", loaded)
	}
}

func TestFeatureSettingsValidateBeforeAsyncSave(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Concurrency.MaxAgents = 0
	model := settingsModel(configConcurrency, resolved, &settingsConfigStore{resolved: settingsResolved()})
	model.configSettings.dirty = true
	command := model.saveConfigSettings()
	if command != nil || !strings.Contains(model.configSettings.status, "Cannot save") {
		t.Fatalf("invalid save status=%q command=%v", model.configSettings.status, command)
	}
}

func TestFeatureSettingsSaveReportsConflictAndServiceErrors(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		err  error
		want string
	}{
		"conflict": {fmt.Errorf("wrapped: %w", appconfig.ErrRevisionConflict), "Save failed: settings changed elsewhere"},
		"service":  {errors.New("disk unavailable"), "Save failed: disk unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &settingsConfigStore{resolved: settingsResolved(), saveErr: test.err}
			model := settingsModel(configConcurrency, settingsResolved(), store)
			model.configSettings.working.Concurrency.MaxAgents++
			model.configSettings.dirty = true
			command := model.saveConfigSettings()
			if command == nil || store.saveCalls != 0 || !model.configSettings.saving {
				t.Fatal("save did not remain asynchronous")
			}
			updated, _ := model.Update(command())
			result := updated.(*Model)
			if result.configSettings.saving || !strings.Contains(result.configSettings.status, test.want) || store.saveCalls != 1 {
				t.Fatalf("save result status=%q calls=%d", result.configSettings.status, store.saveCalls)
			}
			if store.expected != 7 || store.workspace.Repository != "repo" || store.scope != appconfig.ScopeUser {
				t.Fatalf("save contract = revision %d workspace %+v scope %q", store.expected, store.workspace, store.scope)
			}
		})
	}
}

func TestFixDefaultsSaveEachChangeAndDoNotOfferReload(t *testing.T) {
	resolved := settingsResolved()
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configFix, resolved, store)
	model.configSettings.cursor = 0

	updated, save := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(*Model)
	if save == nil || !model.configSettings.saving || model.configSettings.working.Fix.TargetScore != 105 {
		t.Fatalf("target score change was not saved automatically: %+v", model.configSettings)
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if store.saveCalls != 1 || model.configSettings.dirty || model.configSettings.working.Fix.TargetScore != 105 {
		t.Fatalf("automatic save did not settle: calls=%d state=%+v", store.saveCalls, model.configSettings)
	}

	before := model.configSettings.working.Fix.TargetScore
	_, command := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if command != nil || model.configSettings.loading || model.configSettings.working.Fix.TargetScore != before {
		t.Fatal("Fix Defaults retained a reload key")
	}
	footer := model.configSettingsFooter()
	if strings.Contains(footer, "save") || strings.Contains(footer, "reload") {
		t.Fatalf("Fix Defaults footer still exposes manual persistence: %q", footer)
	}
}

func TestAgentSettingsOfferSafeCodexDefaultAndShowReadiness(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
	}
	resolved.Fix.Profile = "codex"
	model := settingsModel(configAgents, resolved, &settingsConfigStore{})
	model.profileCatalog = &multiRuntimeProfileServices{}
	model.width = 120
	model.configSettings.probes["codex"] = agent.ProbeResult{State: agent.ProbeReady, Version: "1.2.3", Authentication: agent.Authentication{Method: "chatgpt", Label: "Signed in with ChatGPT"}, Capabilities: agent.Capabilities{Isolation: agent.RuntimeIsolation{
		Writes: agent.CandidateTreeAndGitMetadataProtected, SensitiveReadsDenied: true, TransportAuthIsolated: true, CrashContainment: true,
	}}}
	text := ansi.Strip(model.configSettingsView())
	for _, fragment := range []string{"Claude CLI  not available", "Claude API  not available", "Codex       [ACTIVE]", "Grok        not available", "OpenAI API  available", "Enter select"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("agent settings missing %q: %q", fragment, text)
		}
	}
	for _, forbidden := range []string{"add agent", "remove", "t test", "↑/↓", "Label", "Profile ID"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("agent chooser exposed %q: %q", forbidden, text)
		}
	}
}

func TestAgentConnectionCheckHasImmediateAndReadableFeedback(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"}}
	resolved.Fix.Profile = "codex"
	model := settingsModel(configAgents, resolved, &settingsConfigStore{})
	services := &settingsProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.width, model.height = 54, 16
	model.configSettings.cursor = agentProviderIndex("codex-cli")
	command := model.openSelectedAgentProvider()
	if command == nil {
		t.Fatal("opening Codex did not start its connection check")
	}
	if text := ansi.Strip(model.configSettingsView()); !strings.Contains(text, "CODEX") || !strings.Contains(text, "CHECKING CONNECTION") {
		t.Fatalf("connection check had no immediate feedback: %q", text)
	}
	_, _ = handleMessage(model, command())
	text := ansi.Strip(model.configSettingsView())
	for _, fragment := range []string{"CODEX", "CONNECTION FAILED", "codex login", "https://developers.openai.com/codex/auth"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("connection result hid %q: %q", fragment, text)
		}
	}
}

func TestMissingCodexExecutableIsMarkedUnavailable(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "codex", Runtime: "codex-cli", Executable: "missing-codex", AuthenticationRef: "provider-owned"}}
	resolved.Fix.Profile = "codex"
	model := settingsModel(configAgents, resolved, &settingsConfigStore{})
	model.profileCatalog = &multiRuntimeProfileServices{}
	model.configSettings.probes["codex"] = agent.ProbeResult{State: agent.ProbeUnavailable, Diagnostic: "codex executable was not found on PATH"}
	text := ansi.Strip(model.configSettingsView())
	if !strings.Contains(text, "Codex       [ACTIVE] · unavailable") {
		t.Fatalf("missing CLI harness was not marked unavailable: %q", text)
	}
}

func TestAgentConnectionDialogShowsOnlyEssentialAdapterFields(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"}}
	resolved.Fix.Profile = "api"
	model := settingsModel(configAgents, resolved, &settingsConfigStore{})
	model.profileCatalog = &multiRuntimeProfileServices{}
	model.configSettings.providerRuntime = "openai-responses"
	model.configSettings.profileEditing = true
	text := ansi.Strip(strings.Join(model.configSettingsLines(120), "\n"))
	for _, hidden := range []string{"Runtime", "Executable", "Probe timeout", "Cancellation grace", "Profile ID", "Label"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("Agents popup exposed preferences-only field %q: %q", hidden, text)
		}
	}
	if !strings.Contains(text, "Authentication") {
		t.Fatalf("Agents popup hid the user-owned authentication choice: %q", text)
	}
}

func TestSuccessfulAutomaticConnectionCheckSelectsAndSavesActiveProvider(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Label: "Codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Label: "OpenAI", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
	}
	resolved.Fix.Profile = "codex"
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	services := &readyMultiRuntimeProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.cursor = agentProviderIndex("openai-responses")
	probe := model.openSelectedAgentProvider()
	if probe == nil || !model.configSettings.probing["api"] {
		t.Fatal("selecting OpenAI API did not begin an automatic connection check")
	}
	_, save := handleMessage(model, probe())
	if save == nil {
		t.Fatal("successful connection did not schedule automatic activation save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if store.resolved.Fix.Profile != "api" || store.resolved.Fix.Model != "api-model" {
		t.Fatalf("saved Fix defaults=%#v", store.resolved.Fix)
	}
}

func TestEditedConnectionRejectsStaleABAProbeAndRestoresLastKnownGoodProfile(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OLD_OPENAI_KEY"},
	}
	resolved.Fix.Profile = "codex"
	model := settingsModel(configAgents, resolved, &settingsConfigStore{resolved: resolved})
	services := &multiRuntimeProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.cursor = agentProviderIndex("openai-responses")
	stale := model.openSelectedAgentProvider()
	if stale == nil {
		t.Fatal("initial connection check was not started")
	}
	model.configSettings.editField = 0
	model.configSettings.input.SetValue("env:NEW_OPENAI_KEY")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	superseded := model.testSelectedProfileCommand()
	if superseded == nil {
		t.Fatal("edited connection remained blocked behind the stale check")
	}
	model.configSettings.editField = 0
	model.configSettings.input.SetValue("env:OLD_OPENAI_KEY")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	current := model.testSelectedProfileCommand()
	if current == nil {
		t.Fatal("second connection edit did not supersede the first edit")
	}
	_, _ = handleMessage(model, stale())
	if _, exists := model.configSettings.probes["api"]; exists || !model.configSettings.probing["api"] {
		t.Fatal("ABA-stale connection result replaced the newer in-flight check")
	}
	_, _ = handleMessage(model, superseded())
	if _, exists := model.configSettings.probes["api"]; exists || !model.configSettings.probing["api"] {
		t.Fatal("superseded middle connection result replaced the current check")
	}
	_, _ = handleMessage(model, current())
	if model.configSettings.probing["api"] {
		t.Fatal("current connection result did not finish its check")
	}
	if got := model.configSettings.working.Profiles[1].AuthenticationRef; got != "env:OLD_OPENAI_KEY" || model.configSettings.dirty || model.configSettings.pendingOriginal != nil {
		t.Fatalf("failed second edit did not restore the last-known-good profile: %+v", model.configSettings)
	}
}

func TestSuccessfulEditToActiveConnectionIsAutomaticallySaved(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OLD_OPENAI_KEY"}}
	resolved.Fix.Profile = "api"
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	services := &readyMultiRuntimeProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.profileEditing = true
	model.configSettings.providerRuntime = "openai-responses"
	model.configSettings.editField = 0
	model.configSettings.input.SetValue("env:NEW_OPENAI_KEY")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	probe := model.testSelectedProfileCommand()
	if probe == nil {
		t.Fatal("active connection edit did not begin an automatic check")
	}
	_, save := handleMessage(model, probe())
	if save == nil {
		t.Fatal("successful active connection edit was not automatically saved")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if got := store.resolved.Profiles[0].AuthenticationRef; got != "env:NEW_OPENAI_KEY" {
		t.Fatalf("saved authentication reference = %q", got)
	}
}

func TestFailedConnectionEditIsRolledBackAndCannotBeSaved(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:WORKING_OPENAI_KEY"}}
	resolved.Fix.Profile = "api"
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	services := &multiRuntimeProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.profileEditing = true
	model.configSettings.providerRuntime = "openai-responses"
	model.configSettings.editField = 0
	model.configSettings.input.SetValue("env:BROKEN_OPENAI_KEY")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	probe := model.testSelectedProfileCommand()
	_, save := handleMessage(model, probe())
	if save != nil {
		t.Fatal("failed connection check attempted to save")
	}
	state := model.configSettings
	if got := state.working.Profiles[0].AuthenticationRef; got != "env:WORKING_OPENAI_KEY" || state.dirty || state.pendingOriginal != nil {
		t.Fatalf("failed connection edit was not rolled back: %+v", state)
	}
	model.handleAgentSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	if model.hasOverlay(OverlaySettingsDirty) || store.saveCalls != 0 {
		t.Fatal("failed connection edit remained reachable through Save/Discard")
	}
}

func TestEscapingAnInFlightConnectionEditRestoresItAndRejectsItsResult(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:WORKING_OPENAI_KEY"}}
	resolved.Fix.Profile = "api"
	model := settingsModel(configAgents, resolved, &settingsConfigStore{resolved: resolved})
	services := &readyMultiRuntimeProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.profileEditing = true
	model.configSettings.providerRuntime = "openai-responses"
	model.configSettings.editField = 0
	model.configSettings.input.SetValue("env:UNVERIFIED_OPENAI_KEY")
	if err := model.commitConfigText(); err != nil {
		t.Fatal(err)
	}
	probe := model.testSelectedProfileCommand()
	if probe == nil {
		t.Fatal("connection edit did not start a check")
	}
	model.handleAgentSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	_, save := handleMessage(model, probe())
	if save != nil || model.configSettings.working.Profiles[0].AuthenticationRef != "env:WORKING_OPENAI_KEY" || model.configSettings.dirty {
		t.Fatalf("escaped connection candidate remained saveable: %+v", model.configSettings)
	}
}

func TestAutomaticActivationSaveFailureRestoresActiveProviderAndIsVisible(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = append(resolved.Profiles, agent.Profile{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"})
	store := &settingsConfigStore{resolved: resolved, saveErr: errors.New("preferences are read-only")}
	model := settingsModel(configAgents, resolved, store)
	services := &readyMultiRuntimeProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.cursor = agentProviderIndex("openai-responses")
	probe := model.openSelectedAgentProvider()
	_, save := handleMessage(model, probe())
	if save == nil {
		t.Fatal("ready provider did not attempt automatic activation")
	}
	_, _ = handleMessage(model, save())
	if model.configSettings.working.Fix.Profile != "codex" || model.configSettings.saving {
		t.Fatalf("failed save left an unsaved provider active: %+v", model.configSettings)
	}
	text := ansi.Strip(model.configSettingsView())
	if !strings.Contains(text, "ACTIVATION FAILED") || !strings.Contains(text, "preferences are read-only") {
		t.Fatalf("activation save failure was hidden: %q", text)
	}
}

func TestDuplicateProviderProfilesAreUnavailableAndNeverProbed(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = append(resolved.Profiles,
		agent.Profile{ID: "api-work", Runtime: "openai-responses", AuthenticationRef: "env:WORK_KEY"},
		agent.Profile{ID: "api-personal", Runtime: "openai-responses", AuthenticationRef: "env:PERSONAL_KEY"},
	)
	model := settingsModel(configAgents, resolved, &settingsConfigStore{resolved: resolved})
	services := &readyMultiRuntimeProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.cursor = agentProviderIndex("openai-responses")
	if available, _ := model.agentProviderAvailability(agentProviderChoices[model.configSettings.cursor]); available {
		t.Fatal("ambiguous provider profiles were reported available")
	}
	if command := model.openSelectedAgentProvider(); command != nil {
		t.Fatal("ambiguous provider profiles started a connection check")
	}
	text := ansi.Strip(model.configSettingsView())
	if !strings.Contains(text, "Multiple connections") || !strings.Contains(text, "one account") {
		t.Fatalf("duplicate-profile recovery was unclear: %q", text)
	}
}

func TestSupportedProviderWithoutProfileIsUnavailableWithRecovery(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	model := settingsModel(configAgents, resolved, &settingsConfigStore{resolved: resolved})
	model.profileCatalog = &multiRuntimeProfileServices{}
	model.configSettings.cursor = agentProviderIndex("openai-responses")
	choice := agentProviderChoices[model.configSettings.cursor]
	if available, _ := model.agentProviderAvailability(choice); available {
		t.Fatal("provider without a configuration was reported available")
	}
	if command := model.openSelectedAgentProvider(); command != nil {
		t.Fatal("provider without a profile started a connection check")
	}
	text := ansi.Strip(model.configSettingsView())
	if !strings.Contains(text, "No connection configured") || !strings.Contains(text, "preferences") {
		t.Fatalf("missing-profile recovery was unclear: %q", text)
	}
}

func TestCompactMissingAndDuplicateProfileRecoveryIsVisible(t *testing.T) {
	t.Parallel()
	for name, profiles := range map[string][]agent.Profile{
		"missing": settingsResolved().Profiles,
		"duplicate": append(settingsResolved().Profiles,
			agent.Profile{ID: "api-work", Runtime: "openai-responses", AuthenticationRef: "env:WORK_KEY"},
			agent.Profile{ID: "api-personal", Runtime: "openai-responses", AuthenticationRef: "env:PERSONAL_KEY"},
		),
	} {
		t.Run(name, func(t *testing.T) {
			resolved := settingsResolved()
			resolved.Profiles = profiles
			model := settingsModel(configAgents, resolved, &settingsConfigStore{resolved: resolved})
			model.profileCatalog = &multiRuntimeProfileServices{}
			model.width, model.height = 40, 10
			model.configSettings.cursor = agentProviderIndex("openai-responses")
			model.openSelectedAgentProvider()
			view := ansi.Strip(model.configSettingsFullScreen())
			assertScreenSize(t, view, 40, 10)
			if !strings.Contains(view, "preferences file") || !strings.Contains(view, "reopen Settings") {
				t.Fatalf("compact %s recovery was clipped: %q", name, view)
			}
		})
	}
}

func TestChoiceConnectionEditRollsBackAfterFailedAutomaticCheck(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{{ID: "choice", Runtime: "choice-api", AuthenticationRef: "provider-owned", Options: map[string]string{"region": "us"}}}
	resolved.Fix.Profile = "choice"
	model := settingsModel(configAgents, resolved, &settingsConfigStore{resolved: resolved})
	services := &choiceProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.profileEditing = true
	model.configSettings.providerRuntime = "choice-api"
	if !model.adjustProfileChoice(1) {
		t.Fatal("choice field was not changed")
	}
	probe := model.testSelectedProfileCommand()
	_, save := handleMessage(model, probe())
	if save != nil || model.configSettings.working.Profiles[0].Options["region"] != "us" || model.configSettings.dirty {
		t.Fatalf("failed choice check was not rolled back: %+v", model.configSettings)
	}
}

func TestChoiceRepairOfInactiveProviderActivatesAndSavesIt(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = append(resolved.Profiles, agent.Profile{ID: "choice", Runtime: "choice-api", AuthenticationRef: "provider-owned", Options: map[string]string{"region": "us"}})
	store := &settingsConfigStore{resolved: resolved}
	model := settingsModel(configAgents, resolved, store)
	services := &repairableChoiceProfileServices{}
	model.profileCatalog, model.profileProber = services, services
	model.configSettings.profileEditing = true
	model.configSettings.providerRuntime = "choice-api"
	if !model.adjustProfileChoice(1) {
		t.Fatal("choice repair did not change the field")
	}
	probe := model.testSelectedProfileCommand()
	_, save := handleMessage(model, probe())
	if save == nil {
		t.Fatal("ready choice repair did not schedule activation save")
	}
	_, _ = handleMessage(model, save())
	if store.resolved.Fix.Profile != "choice" || store.resolved.Profiles[1].Options["region"] != "eu" {
		t.Fatalf("choice repair was saved without activation: %#v %#v", store.resolved.Fix, store.resolved.Profiles)
	}
}

func TestFixSettingsProfileCycleClearsForeignRuntimeChoices(t *testing.T) {
	t.Parallel()
	resolved := settingsResolved()
	resolved.Profiles = []agent.Profile{
		{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"},
		{ID: "api", Runtime: "openai-responses", AuthenticationRef: "env:OPENAI_API_KEY"},
	}
	resolved.Fix.Profile, resolved.Fix.Model, resolved.Fix.Effort = "codex", "codex-model", "high"

	if !adjustFixSetting(&resolved, 2, 1, map[agent.ProfileID]agent.ProbeResult{}) {
		t.Fatal("Fix Settings profile row did not change")
	}
	if resolved.Fix.Profile != "api" || resolved.Fix.Model != "" || resolved.Fix.Effort != "" {
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

func TestAgentProfileChoiceLabelsStayShort(t *testing.T) {
	t.Parallel()
	builtIn := agentProfileChoiceLabel(agent.Profile{ID: "codex-default", Runtime: "codex-cli"})
	custom := agentProfileChoiceLabel(agent.Profile{ID: "work", Runtime: "codex-cli"})
	if builtIn != "Codex" || custom != "Codex" {
		t.Fatalf("Codex choice labels: built-in=%q custom=%q", builtIn, custom)
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

	reopened := &Model{configStore: store, configWorkspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo"}}
	reload := reopened.openConfigSettings(configAgents)
	if reload == nil || !reopened.configSettings.loading {
		t.Fatal("reopening settings did not load fresh configuration")
	}
	reopened.handleConfigResolved(reload().(configResolvedMsg))
	got := reopened.configSettings.working.Profiles
	if len(got) != 1 || got[0].ID != "gpt" || got[0].AuthenticationRef != "env:OPENAI_API_KEY" || got[0].Executable != "" {
		t.Fatalf("reloaded API profile=%#v", got)
	}
}

type settingsProfileServices struct{}

func (*settingsProfileServices) Kinds() []agent.RuntimeKind { return []agent.RuntimeKind{"codex-cli"} }
func (*settingsProfileServices) Descriptor(kind agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	if kind != "codex-cli" {
		return agent.ProfileDescriptor{}, fmt.Errorf("unknown runtime %s", kind)
	}
	return agent.ProfileDescriptor{Runtime: kind, Label: "Codex", ConnectionInstructions: "Run `codex login` to authorize.", DocumentationURL: "https://developers.openai.com/codex/auth", Fields: []agent.ProfileField{
		{Key: "executable", Label: "Executable", Kind: agent.ProfileFieldExecutable, Required: true, PreferencesOnly: true},
		{Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference, Required: true, Description: "Run `codex login` to authorize.", PreferencesOnly: true},
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
		return agent.ProfileDescriptor{Runtime: kind, Label: "OpenAI API", ConnectionInstructions: "Set the API key environment variable.", Fields: []agent.ProfileField{{
			Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference,
			Required: true, Default: "env:OPENAI_API_KEY",
		}}}, nil
	}
	return (&settingsProfileServices{}).Descriptor(kind)
}

type readyMultiRuntimeProfileServices struct{ multiRuntimeProfileServices }

func (*readyMultiRuntimeProfileServices) Probe(_ context.Context, profile agent.Profile) agent.ProbeResult {
	model := agent.ModelID("codex-model")
	if profile.Runtime == "openai-responses" {
		model = "api-model"
	}
	return agent.ProbeResult{
		Runtime: profile.Runtime, State: agent.ProbeReady,
		Authentication: agent.Authentication{Label: "Authenticated"},
		Capabilities: agent.Capabilities{
			Models:  []agent.Option[agent.ModelID]{{ID: model, Default: true}},
			Efforts: []agent.Option[agent.EffortID]{{ID: "high", Default: true}},
			Isolation: agent.RuntimeIsolation{
				Writes: agent.CandidateTreeEnforced, ProviderManagedCancellation: true,
			},
		},
	}
}

type choiceProfileServices struct{}

func (*choiceProfileServices) Kinds() []agent.RuntimeKind {
	return []agent.RuntimeKind{"codex-cli", "choice-api"}
}
func (*choiceProfileServices) Descriptor(kind agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	if kind == "codex-cli" {
		return (&settingsProfileServices{}).Descriptor(kind)
	}
	if kind == "choice-api" {
		return agent.ProfileDescriptor{Runtime: kind, Label: "Choice API", Fields: []agent.ProfileField{{
			Key: "options.region", OptionKey: "region", Label: "Region", Kind: agent.ProfileFieldChoice,
			Required: true, Choices: []string{"us", "eu"},
		}}}, nil
	}
	return agent.ProfileDescriptor{}, fmt.Errorf("unknown runtime %s", kind)
}
func (*choiceProfileServices) ValidateProfile(agent.Profile) error { return nil }
func (*choiceProfileServices) Probe(_ context.Context, profile agent.Profile) agent.ProbeResult {
	return agent.ProbeResult{Runtime: profile.Runtime, State: agent.ProbeUnauthenticated, Diagnostic: "connection rejected"}
}

type repairableChoiceProfileServices struct{ choiceProfileServices }

func (*repairableChoiceProfileServices) Probe(_ context.Context, profile agent.Profile) agent.ProbeResult {
	if profile.Runtime != "choice-api" || profile.Options["region"] != "eu" {
		return agent.ProbeResult{Runtime: profile.Runtime, State: agent.ProbeUnauthenticated, Diagnostic: "connection rejected"}
	}
	return agent.ProbeResult{
		Runtime: profile.Runtime, State: agent.ProbeReady,
		Capabilities: agent.Capabilities{
			Models:    []agent.Option[agent.ModelID]{{ID: "choice-model", Default: true}},
			Efforts:   []agent.Option[agent.EffortID]{{ID: "high", Default: true}},
			Isolation: agent.RuntimeIsolation{Writes: agent.CandidateTreeEnforced, ProviderManagedCancellation: true},
		},
	}
}

func TestFeatureSettingsRenderAllSectionsAtResponsiveSizes(t *testing.T) {
	t.Parallel()
	sections := map[configSettingsKind]string{
		configAgents: "AGENTS", configFix: "FIX DEFAULTS", configConcurrency: "CONCURRENCY & RETENTION",
		configDelivery: "GIT & PULL REQUESTS",
	}
	for _, size := range [][2]int{{40, 10}, {72, 16}, {120, 30}} {
		for kind, heading := range sections {
			model := settingsModel(kind, settingsResolved(), &settingsConfigStore{})
			model.width, model.height = size[0], size[1]
			text := ansi.Strip(model.configSettingsView())
			if !strings.Contains(text, heading) || kind == configAgents && !strings.Contains(text, "Enter select") {
				t.Fatalf("%s at %dx%d = %q", kind, size[0], size[1], text)
			}
			if kind == configFix && (strings.Contains(text, "s save") || strings.Contains(text, "r reload")) {
				t.Fatalf("Fix Defaults still advertises manual persistence at %dx%d: %q", size[0], size[1], text)
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

func TestSettingsKeepDeliveryAndMasterPromptClear(t *testing.T) {
	resolved := settingsResolved()
	resolved.Fix.PromptTemplate = fixprompt.DefaultTemplate

	deliveryModel := settingsModel(configDelivery, resolved, &settingsConfigStore{})
	deliveryText := ansi.Strip(strings.Join(deliveryModel.configSettingsLines(100), "\n"))
	for _, unwanted := range []string{"Publisher", "only supported v1 adapter", "fixed v1", "explicit Discard", "Built-in default", "save creates user default"} {
		if strings.Contains(deliveryText, unwanted) {
			t.Fatalf("delivery settings contain obsolete hint %q: %q", unwanted, deliveryText)
		}
	}

	fixModel := settingsModel(configFix, resolved, &settingsConfigStore{})
	fixModel.configSettings.cursor = fixSettingsPromptRow
	fixText := ansi.Strip(strings.Join(fixModel.configSettingsLines(75), "\n"))
	if !strings.Contains(fixText, "Agent prompt") || !strings.Contains(fixText, "[x] SCORE") {
		t.Fatalf("master prompt or score metric missing: %q", fixText)
	}
	if strings.Count(fixText, "Focus metrics") != 1 || strings.Contains(fixText, "Focus metric ") || strings.Contains(fixText, "Change scope") || !strings.Contains(fixText, "May edit") {
		t.Fatalf("Fix settings grouping or labels are wrong: %q", fixText)
	}
	gridRows := 0
	for _, line := range strings.Split(fixText, "\n") {
		if strings.Count(line, "[") == 3 {
			gridRows++
		}
	}
	if gridRows != 3 {
		t.Fatalf("focus metrics are not an aligned 3x3 grid: %q", fixText)
	}
	if text := ansi.Strip(strings.Join(fixModel.configSettingsLines(100), "\n")); strings.Contains(text, "Master template used") || strings.Contains(text, "Built-in default") {
		t.Fatalf("Fix settings contain an explanatory hint: %q", text)
	}
}

func TestSavingTargetScoreKeepsModelAndEffortChoicesStable(t *testing.T) {
	resolved := settingsResolved()
	model := settingsModel(configFix, resolved, &settingsConfigStore{})
	model.configSettings.probes["codex"] = agent.ProbeResult{State: agent.ProbeReady, Capabilities: agent.Capabilities{
		Models:  []agent.Option[agent.ModelID]{{ID: "gpt", Label: "GPT"}, {ID: "mini", Label: "Mini"}},
		Efforts: []agent.Option[agent.EffortID]{{ID: "high", Label: "High"}, {ID: "medium", Label: "Medium"}},
	}}
	if !model.configChoiceField(3) || !model.configChoiceField(4) {
		t.Fatal("model and effort did not start as dropdowns")
	}
	command := model.handleConfigSaved(configSavedMsg{generation: model.configSettings.generation, saved: appconfig.Saved{Revision: resolved.Revision + 1, Resolved: resolved}})
	if command != nil || !model.configChoiceField(3) || !model.configChoiceField(4) {
		t.Fatal("saving an unrelated FIX DEFAULT caused agent choices to disappear or re-probe")
	}
}

func TestFixDefaultsEditTheMasterPrompt(t *testing.T) {
	model := settingsModel(configFix, settingsResolved(), &settingsConfigStore{})
	model.configSettings.cursor = fixSettingsPromptRow
	model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.hasOverlay(OverlayPromptEditor) || !strings.Contains(ansi.Strip(model.View()), "MASTER AGENT PROMPT") {
		t.Fatalf("master prompt editor did not open: %q", ansi.Strip(model.View()))
	}
	if model.configSettings.prompt.ShowLineNumbers || model.configSettings.prompt.Prompt != "" || strings.Contains(ansi.Strip(model.View()), "┃") {
		t.Fatalf("master prompt did not use a plain text box: %q", ansi.Strip(model.View()))
	}
	const prompt = "Refactor {targets} until SCORE is no more than {target_score}."
	model.configSettings.prompt.SetValue(prompt)
	model.handleMasterPromptKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.hasOverlay(OverlayPromptEditor) || model.configSettings.working.Fix.PromptTemplate != prompt || !model.configSettings.dirty {
		t.Fatalf("master prompt was not applied to Fix Defaults: %+v", model.configSettings)
	}
}

func TestFixDefaultsChoicesOpenAsAnchoredMenusWithoutReflow(t *testing.T) {
	model := settingsModel(configFix, settingsResolved(), &settingsConfigStore{resolved: settingsResolved()})
	model.configSettings.cursor = 1 // May edit.
	closed := model.configSettingsLines(68)
	model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.configSettings.choiceOpen {
		t.Fatal("Enter did not open the May edit dropdown")
	}
	open := model.configSettingsLines(68)
	if strings.Join(closed, "\n") != strings.Join(open, "\n") {
		t.Fatalf("opening a settings dropdown reflowed its form rows: closed=%q open=%q", ansi.Strip(strings.Join(closed, "\n")), ansi.Strip(strings.Join(open, "\n")))
	}
	view := ansi.Strip(model.configSettingsView())
	for _, choice := range []string{"Selected files", "Selected files + related tests", "Any file in the project"} {
		if !strings.Contains(view, choice) {
			t.Fatalf("settings dropdown omitted %q despite available dialog space: %q", choice, view)
		}
	}
	model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	_, save := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if save == nil || model.configSettings.choiceOpen || model.configSettings.working.Fix.ChangeScope != "repository" {
		t.Fatalf("settings dropdown selection was not applied and saved: %+v", model.configSettings)
	}
}

func TestMasterPromptEmptyErrorIsVisible(t *testing.T) {
	for _, size := range []struct{ width, height int }{{36, 6}, {80, 24}} {
		model := settingsModel(configFix, settingsResolved(), &settingsConfigStore{})
		model.width, model.height = size.width, size.height
		model.configSettings.cursor = fixSettingsPromptRow
		model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
		model.configSettings.prompt.SetValue("   ")
		model.handleMasterPromptKey(tea.KeyMsg{Type: tea.KeyCtrlS})
		plain := ansi.Strip(model.View())
		if !model.hasOverlay(OverlayPromptEditor) || !strings.Contains(plain, "Agent prompt cannot be empty") {
			t.Fatalf("%dx%d prompt error was not visible: %q", size.width, size.height, plain)
		}
		assertScreenSize(t, model.View(), size.width, size.height)
	}
}

func TestDeliverySettingsDoNotSilentlyClipOrganisationBranchTemplate(t *testing.T) {
	resolved := settingsResolved()
	resolved.Delivery.DefaultPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitCommitNewBranch, Publish: fix.PublishPush}
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

func TestSettingsDoNotAppendFocusedHints(t *testing.T) {
	resolved := settingsResolved()
	resolved.Origins["concurrency.max_agents"] = appconfig.OriginCLI
	model := settingsModel(configConcurrency, resolved, &settingsConfigStore{})
	model.configSettings.cursor = 0
	text := ansi.Strip(strings.Join(model.configSettingsLines(100), "\n"))
	for _, unwanted := range []string{"lowering it", "never deletes", "Pinned at Prepare", "maximum candidate-file"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("settings contain focused hint %q: %q", unwanted, text)
		}
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
	resolved.Revision++
	store.resolved = cloneConfigResolved(resolved)
	return appconfig.Saved{Revision: resolved.Revision, Resolved: resolved}, nil
}

func settingsModel(kind configSettingsKind, resolved appconfig.Resolved, store ConfigStore) *Model {
	input := textinput.New()
	prompt := textarea.New()
	return &Model{
		width: 80, height: 24, configStore: store,
		configWorkspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo"},
		configSettings: configSettingsState{
			open: true, kind: kind, generation: 1, resolved: cloneConfigResolved(resolved),
			working: cloneConfigResolved(resolved), probes: map[agent.ProfileID]agent.ProbeResult{}, probing: map[agent.ProfileID]bool{}, input: input, prompt: prompt,
		},
	}
}

func settingsResolved() appconfig.Resolved {
	return appconfig.Resolved{
		SchemaVersion: 1, Revision: 7,
		Origins: map[string]appconfig.Origin{
			"agents": appconfig.OriginUser, "fix": appconfig.OriginBuiltIn, "concurrency": appconfig.OriginUser,
			"delivery": appconfig.OriginBuiltIn,
		},
		Fix: appconfig.FixDefaults{
			TargetScore: 100, Focus: []fix.MetricID{fix.MetricScore}, ChangeScope: "targets-and-tests", Profile: "codex", PromptTemplate: fixprompt.DefaultTemplate,
		},
		Concurrency: appconfig.Concurrency{MaxAgents: 2, MaxVerifiers: 1, MaxActorsPerJob: 32, MaxCandidatePreviewBytes: 4 << 20, MaxCandidatePreviewLines: 5000},
		Profiles:    []agent.Profile{{ID: "codex", Label: "Codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"}},
		Delivery: appconfig.Delivery{DefaultPlan: fix.DeliveryPlan{Workspace: fix.WorkspaceCurrent, Git: fix.GitLeaveUncommitted, Publish: fix.PublishLocal}, Remote: "origin", BaseBranch: "main",
			BranchTemplate: "slopwatch/fix-{job}", Publisher: "github-cli", DraftPullRequests: true, CommandOutputBytes: 4 << 20,
			CommitTitleTemplate: "Refactor {targets}", CommitBodyTemplate: "Fix {goal}", PullRequestTitleTemplate: "Refactor {targets}", PullRequestBodyTemplate: "Fix {goal}"},
		TrendWindow: 10 * time.Minute,
	}
}
