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
	model.configSettings.working.Profiles = nil
	model.addDefaultCodexProfile()
	profiles := model.configSettings.working.Profiles
	if len(profiles) != 1 || profiles[0].Runtime != "codex-cli" || profiles[0].Executable != "codex" || profiles[0].AuthenticationRef != "provider-owned" {
		t.Fatalf("Codex default = %#v", profiles)
	}
	model.configSettings.probes["codex"] = agent.ProbeResult{State: agent.ProbeReady, Version: "1.2.3"}
	text := ansi.Strip(model.configSettingsView())
	for _, fragment := range []string{"Codex", "ready 1.2.3", "auth provider-owned"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("agent settings missing %q: %q", fragment, text)
		}
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
	if len(fields) != 3 || fields[2].OptionKey != "obsolete" {
		t.Fatalf("repair fields = %#v", fields)
	}
	model.configSettings.editField = 5
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
	return agent.ProfileDescriptor{Runtime: kind, Label: "Codex CLI", Fields: []agent.ProfileField{
		{Key: "executable", Label: "Executable", Kind: agent.ProfileFieldExecutable, Required: true},
		{Key: "authentication_ref", Label: "Authentication", Kind: agent.ProfileFieldAuthReference, Required: true, Description: "Run `codex login` to authorize."},
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
	if patch.Concurrency != nil {
		resolved.Concurrency = *patch.Concurrency
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
			MaxAttempts: 2, AttemptTimeout: 30 * time.Minute,
		},
		Concurrency: appconfig.Concurrency{MaxAgents: 2, MaxVerifiers: 1, MaxRetainedJobs: 100, MaxTranscriptBytes: 1024 * 1024},
		Profiles:    []agent.Profile{{ID: "codex", Label: "Codex", Runtime: "codex", Executable: "codex", AuthenticationRef: "provider-owned"}},
		Validation:  []validation.Plan{{ID: "go-test", Checks: []validation.Check{{ID: "test", Executable: "go"}}}},
		Delivery:    appconfig.Delivery{DefaultMode: "candidate", Remote: "origin", BranchTemplate: "slopwatch/fix-{job}", Publisher: "github-cli", DraftPullRequests: true},
		TrendWindow: 10 * time.Minute,
	}
}
