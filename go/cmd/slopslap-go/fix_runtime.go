package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/agent/codexcli"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/appconfig/preferencesadapter"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/delivery"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis/nativeadapter"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/isolation"
	"github.com/blater/slopwatch/internal/jobstore"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/publisher/ghcli"
	"github.com/blater/slopwatch/internal/validation"
)

type fixFeature struct {
	service    *fixapp.Manager
	config     *preferencesadapter.Adapter
	workspace  fix.WorkspaceIdentity
	prober     agent.Prober
	catalog    agent.ProfileCatalog
	candidates candidate.Service
}

func (feature *fixFeature) Close(ctx context.Context) error {
	if feature == nil || feature.service == nil {
		if feature != nil && feature.candidates != nil {
			return feature.candidates.Close()
		}
		return nil
	}
	return feature.service.Shutdown(ctx)
}

func buildFixFeature(ctx context.Context, workspace, installationRoot, preferencesPath string, parsed *options, languages []string) (*fixFeature, error) {
	runner := isolation.Runner{}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("locate fix job state: %w", err)
	}
	stateRoot := filepath.Join(cacheRoot, "slopwatch", "fix")
	analysisRoot, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return nil, err
	}
	// Configuration and provider metadata are composed before the runnable job
	// manager. This provisional identity is enough to repair user preferences
	// even when Git/candidate initialization fails; it is replaced with the
	// discovered repository identity before any job can be admitted.
	identity := fix.WorkspaceIdentity{RepositoryRoot: analysisRoot, AnalysisRoot: analysisRoot}

	registry := agent.NewRegistry()
	// The executable checker measures the exact generated Codex sandbox. Crash
	// containment remains false because process groups cannot prevent a tool
	// from escaping into a new session; therefore this build stays fail-closed
	// even when the filesystem/network sentinel gates pass.
	strategy := codexcli.New(runner, codexcli.ExecutableChecker{Runner: runner, CrashContained: false})
	if err := registry.Register(codexcli.RuntimeKind, strategy); err != nil {
		return nil, err
	}
	defaults := codexDefaults(preferences.DefaultDocument(), identity, stateRoot, preferencesPath)
	config, err := preferencesadapter.New(preferencesadapter.Options{UserPath: preferencesPath, Defaults: defaults, RuntimeKinds: registry.Kinds(), ProfileCatalog: registry})
	if err != nil {
		return nil, err
	}
	feature := &fixFeature{config: config, workspace: identity, prober: registry, catalog: registry}
	candidates, err := candidate.NewGitWorktreeService(filepath.Join(stateRoot, "candidates"), runner)
	if err != nil {
		return feature, err
	}
	feature.candidates = candidates
	discovered, err := candidates.DiscoverWorkspace(ctx, workspace)
	if err != nil {
		return feature, err
	}
	discovered.AnalysisRoot = analysisRoot
	identity = discovered
	feature.workspace = identity
	resolved, err := config.Resolve(ctx, identity, appconfig.SessionOverrides{})
	if err != nil {
		return feature, err
	}
	analysis, err := nativeadapter.New(nativeadapter.Config{InstallationRoot: installationRoot, Languages: languages,
		IncludeTests: parsed.includeTests, TypeScriptTypes: parsed.typescriptTypes, FollowSymlinks: parsed.followSymlinks, BaselineReadCache: true})
	if err != nil {
		return feature, err
	}
	validator, err := validation.NewRunner(validation.DenyAllExecutor{Diagnostic: "validation commands are disabled until this platform proves candidate-only confinement"})
	if err != nil {
		return feature, err
	}
	journal, err := jobstore.OpenFile(filepath.Join(stateRoot, string(identity.Repository), "jobs"))
	if err != nil {
		return feature, err
	}
	deliveryService, err := delivery.NewGitService(runner)
	if err != nil {
		_ = journal.Close()
		return feature, err
	}
	pullRequests, publisherErr := ghcli.New(runner)
	if publisherErr != nil {
		pullRequests = nil
	}
	manager, err := fixapp.New(fixapp.Dependencies{Config: config, Analysis: analysis, Validation: validator, Candidates: candidates, ScopePlanner: candidate.UnitScopePlanner{}, DeliveryPreflight: deliveryService,
		Agents: registry, Store: journal, Delivery: deliveryService, Publisher: pullRequests}, fixapp.Options{
		MaxAgents: resolved.Concurrency.MaxAgents, MaxVerifiers: resolved.Concurrency.MaxVerifiers,
		MaxTranscriptItems: transcriptItemLimit(resolved.Concurrency.MaxTranscriptBytes),
	})
	if err != nil {
		_ = journal.Close()
		return feature, err
	}
	feature.service = manager
	feature.candidates = nil // manager owns candidate shutdown after successful composition.
	return feature, nil
}

func codexDefaults(value preferences.Document, workspace fix.WorkspaceIdentity, stateRoot, preferencesPath string) preferences.Document {
	value = preferences.Clone(value)
	denied := []string{workspace.RepositoryRoot, stateRoot}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		denied = append(denied, home)
	}
	if preferencesPath != "" {
		denied = append(denied, filepath.Dir(preferencesPath))
	}
	value.Agents.Profiles = []preferences.AgentProfile{{ID: "codex-default", Label: "Codex CLI", Runtime: string(codexcli.RuntimeKind),
		Executable: "codex", RuntimeProfile: "slopwatch", AuthenticationRef: "provider-owned",
		Options: map[string]string{"denied_read_roots": strings.Join(denied, string(os.PathListSeparator))}}}
	value.Fix.Profile = "codex-default"
	value.Fix.Model = "gpt-5.6-sol"
	value.Fix.Effort = "high"
	value.Fix.Delegation = string(agent.DelegationSingle)
	return value
}

func transcriptItemLimit(bytes int64) int {
	if bytes <= 0 {
		return 2_000
	}
	items := bytes / 256
	if items < 100 {
		items = 100
	}
	if items > 20_000 {
		items = 20_000
	}
	return int(items)
}

func closeFixFeature(feature *fixFeature) error {
	if feature == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return feature.Close(ctx)
}
