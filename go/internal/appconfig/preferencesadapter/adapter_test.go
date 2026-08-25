package preferencesadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/validation"
)

func TestResolveDefaultsAndCreatesUserPreferences(t *testing.T) {
	t.Parallel()
	adapter, userPath, workspace := newTestAdapter(t)
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fix.TargetScore != 100 || resolved.Concurrency.MaxAgents != 2 || resolved.Concurrency.MaxVerifiers != 1 {
		t.Fatalf("resolved defaults = %#v", resolved)
	}
	if resolved.Origins["fix"] != appconfig.OriginBuiltIn || resolved.Revision == 0 {
		t.Fatalf("origins/revision = %#v / %d", resolved.Origins, resolved.Revision)
	}
	if _, err := os.Stat(userPath); err != nil {
		t.Fatalf("user preferences were not created: %v", err)
	}
}

func TestUserRoundTripReturnsDeepCopies(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{
		ID: "primary", Label: "Codex", Runtime: "codex", Executable: "/usr/bin/codex",
		AuthenticationRef: "env:CODEX_API_TOKEN", Options: map[string]string{"sandbox": "strict"},
	}}
	fixDefaults := initial.Fix
	fixDefaults.Profile = "primary"
	fixDefaults.Focus = []fix.MetricID{"cog", "coupling"}
	fixDefaults.ValidationPlan = "go"
	plans := []validation.Plan{{ID: "go", Checks: []validation.Check{{
		ID: "test", Executable: "go", Arguments: []string{"test", "./..."},
		Required: true, Timeout: time.Minute, MaxOutputBytes: 4096,
	}}}}
	saved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{
		Profiles: &profiles, Fix: &fixDefaults, Validation: &plans,
	}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Resolved.Fix.Profile != "primary" || saved.Resolved.Fix.ValidationPlan != "go" || saved.Resolved.Origins["agents"] != appconfig.OriginUser {
		t.Fatalf("saved = %#v", saved.Resolved)
	}
	saved.Resolved.Profiles[0].Options["sandbox"] = "mutated"
	saved.Resolved.Validation[0].Checks[0].Arguments[0] = "mutated"
	saved.Resolved.Fix.Focus[0] = "npath"
	again, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Profiles[0].Options["sandbox"] != "strict" || again.Validation[0].Checks[0].Arguments[0] != "test" || again.Fix.Focus[0] != "cog" {
		t.Fatalf("resolved values alias a prior snapshot: %#v", again)
	}
}

func TestUnknownRuntimeIsRejected(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "other", Runtime: "unknown", Executable: "other"}}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "unknown runtime") {
		t.Fatalf("Save() error = %v", err)
	}
}

func TestSaveRejectsStaleRevisionIncludingExternalEdit(t *testing.T) {
	t.Parallel()
	adapter, userPath, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	concurrency := initial.Concurrency
	concurrency.MaxAgents = 3
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Concurrency: &concurrency}, initial.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Concurrency: &concurrency}, initial.Revision); !errors.Is(err, appconfig.ErrRevisionConflict) {
		t.Fatalf("stale Save() error = %v", err)
	}
	current, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n# external editor\n")...)
	if err := os.WriteFile(userPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Concurrency: &concurrency}, current.Revision)
	if !errors.Is(err, appconfig.ErrRevisionConflict) {
		t.Fatalf("externally stale Save() error = %v", err)
	}
}

func TestRepositoryScopeCannotPersistCommandsOrSecrets(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "bad", Runtime: "codex", Executable: "codex", AuthenticationRef: "literal-secret"}}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{Profiles: &profiles}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "cannot register agent profiles") {
		t.Fatalf("repository profile Save() error = %v", err)
	}
	plans := []validation.Plan{{ID: "bad", Checks: []validation.Check{{ID: "run", Executable: "sh"}}}}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{Validation: &plans}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "cannot define executable checks") {
		t.Fatalf("repository validation Save() error = %v", err)
	}
	repositoryPath := filepath.Join(workspace.RepositoryRoot, ".slopwatch", "preferences.toml")
	if err := os.MkdirAll(filepath.Dir(repositoryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := "version = 1\n[[agents.profiles]]\nid='bad'\nruntime='codex'\nexecutable='sh'\nauthentication_ref='literal-secret'\n"
	if err := os.WriteFile(repositoryPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{}); err == nil || !strings.Contains(err.Error(), "cannot register agent profiles") {
		t.Fatalf("repository literal command/secret Resolve() error = %v", err)
	}
}

func TestRepositorySelectsTrustedUserValidationPlanByID(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	plans := []validation.Plan{{ID: "go", Checks: []validation.Check{{
		ID: "test", Executable: "go", Arguments: []string{"test", "./..."},
		Timeout: time.Minute, MaxOutputBytes: 1024,
	}}}}
	userSaved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Validation: &plans}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	selection := []validation.Plan{{ID: "go"}}
	repositorySaved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{Validation: &selection}, userSaved.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if repositorySaved.Resolved.Origins["validation"] != appconfig.OriginRepository ||
		len(repositorySaved.Resolved.Validation) != 1 || len(repositorySaved.Resolved.Validation[0].Checks) != 1 ||
		repositorySaved.Resolved.Validation[0].Checks[0].Executable != "go" {
		t.Fatalf("repository selection = %#v", repositorySaved.Resolved)
	}
}

func TestInvalidRepositoryPatchIsNotPersisted(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	invalid := initial.Concurrency
	invalid.MaxAgents = 0
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeRepository, appconfig.Patch{Concurrency: &invalid}, initial.Revision); err == nil {
		t.Fatal("invalid repository patch was accepted")
	}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatalf("invalid repository patch was persisted: %v", err)
	}
	if resolved.Concurrency.MaxAgents != initial.Concurrency.MaxAgents {
		t.Fatalf("MaxAgents = %d, want %d", resolved.Concurrency.MaxAgents, initial.Concurrency.MaxAgents)
	}
}

func TestUserAuthenticationReferenceCannotContainLiteralSecret(t *testing.T) {
	t.Parallel()
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "codex", Runtime: "codex", Executable: "codex", AuthenticationRef: "sk-literal"}}
	_, err = adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, initial.Revision)
	if err == nil || !strings.Contains(err.Error(), "literal credentials") {
		t.Fatalf("literal credential Save() error = %v", err)
	}
}

func TestInvalidProviderProfileCanBeLoadedForRepairAndSaved(t *testing.T) {
	root := t.TempDir()
	defaults := preferences.DefaultDocument()
	defaults.Agents.Profiles = []preferences.AgentProfile{{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned", Options: map[string]string{"unknown": "bad"}}}
	defaults.Fix.Profile = "codex"
	catalog := &strictProfileCatalog{}
	adapter, err := New(Options{UserPath: filepath.Join(root, "preferences.toml"), Defaults: defaults, RuntimeKinds: catalog.Kinds(), ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	workspace := fix.WorkspaceIdentity{RepositoryRoot: filepath.Join(root, "repo")}
	if _, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{}); err == nil {
		t.Fatal("invalid provider option resolved")
	}
	editable, err := adapter.LoadEditable(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(editable.Diagnostics) == 0 {
		t.Fatal("repair snapshot omitted diagnostics")
	}
	profiles := editable.Resolved.Profiles
	delete(profiles[0].Options, "unknown")
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, editable.Resolved.Revision); err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil || len(resolved.Profiles) != 1 {
		t.Fatalf("repaired resolve=%+v err=%v", resolved, err)
	}
}

func TestProviderValidationRunsForProfileWithoutOptions(t *testing.T) {
	root := t.TempDir()
	catalog := &rejectingProfileCatalog{}
	adapter, err := New(Options{UserPath: filepath.Join(root, "preferences.toml"), RuntimeKinds: catalog.Kinds(), ProfileCatalog: catalog})
	if err != nil {
		t.Fatal(err)
	}
	workspace := fix.WorkspaceIdentity{RepositoryRoot: filepath.Join(root, "repo")}
	resolved, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "codex", Runtime: "codex-cli", Executable: "codex", AuthenticationRef: "provider-owned"}}
	if _, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles}, resolved.Revision); err == nil || !strings.Contains(err.Error(), "provider rejected") {
		t.Fatalf("Save() error = %v", err)
	}
}

func TestOriginsIdentifyProfileOptionsAndListEntries(t *testing.T) {
	adapter, _, workspace := newTestAdapter(t)
	initial, err := adapter.Resolve(context.Background(), workspace, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []agent.Profile{{ID: "primary", Label: "Codex", Runtime: "codex", Executable: "codex", AuthenticationRef: "provider-owned", Options: map[string]string{"sandbox": "strict"}}}
	fixDefaults := initial.Fix
	fixDefaults.Focus = []fix.MetricID{"cog"}
	plans := []validation.Plan{{ID: "go", Checks: []validation.Check{{ID: "test", Executable: "go", Arguments: []string{"test", "./..."}, Timeout: time.Minute, MaxOutputBytes: 1024}}}}
	saved, err := adapter.Save(context.Background(), workspace, appconfig.ScopeUser, appconfig.Patch{Profiles: &profiles, Fix: &fixDefaults, Validation: &plans}, initial.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"agents.primary", "agents.primary.executable", "agents.primary.options.sandbox", "fix.focus.cog", "validation.go", "validation.go.checks.test"} {
		if got := saved.Resolved.Origins[key]; got != appconfig.OriginUser {
			t.Errorf("origin[%q] = %q, want user", key, got)
		}
	}
}

type rejectingProfileCatalog struct{}

func (*rejectingProfileCatalog) Kinds() []agent.RuntimeKind { return []agent.RuntimeKind{"codex-cli"} }
func (*rejectingProfileCatalog) Descriptor(agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	return agent.ProfileDescriptor{Runtime: "codex-cli", Label: "Codex"}, nil
}
func (*rejectingProfileCatalog) ValidateProfile(agent.Profile) error {
	return errors.New("provider rejected profile")
}

type strictProfileCatalog struct{}

func (*strictProfileCatalog) Kinds() []agent.RuntimeKind { return []agent.RuntimeKind{"codex-cli"} }
func (*strictProfileCatalog) Descriptor(agent.RuntimeKind) (agent.ProfileDescriptor, error) {
	return agent.ProfileDescriptor{Runtime: "codex-cli", Label: "Codex"}, nil
}
func (*strictProfileCatalog) ValidateProfile(profile agent.Profile) error {
	for key := range profile.Options {
		if key != "denied_read_roots" {
			return fmt.Errorf("unsupported option %s", key)
		}
	}
	return nil
}

func newTestAdapter(t *testing.T) (*Adapter, string, fix.WorkspaceIdentity) {
	t.Helper()
	root := t.TempDir()
	userPath := filepath.Join(root, "user", "preferences.toml")
	adapter, err := New(Options{UserPath: userPath, RuntimeKinds: []agent.RuntimeKind{"codex"}})
	if err != nil {
		t.Fatal(err)
	}
	workspace := fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: filepath.Join(root, "repository")}
	return adapter, userPath, workspace
}
