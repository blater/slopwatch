package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/agent/codexcli"
	"github.com/blater/slopwatch/internal/agent/openairesponses"
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

func buildFixFeature(ctx context.Context, workspace, installationRoot, preferencesPath, userDataRoot string, parsed *options, languages []string) (*fixFeature, error) {
	runner := isolation.Runner{}
	stateRoot := filepath.Join(userDataRoot, "fix")
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
	// Codex follows the same deep-client integration as t3code: Slopwatch owns
	// an App Server child per attempt and the adapter translates its lifecycle,
	// events and turn-scoped cancellation behind agent.Strategy.
	strategy := codexcli.New()
	if err := registry.Register(codexcli.RuntimeKind, strategy); err != nil {
		return nil, err
	}
	responsesSecrets := openairesponses.DefaultEnvironmentSecretResolver()
	responses, err := openairesponses.New(openairesponses.Config{}, responsesSecrets)
	if err != nil {
		return nil, fmt.Errorf("configure OpenAI Responses agent: %w", err)
	}
	if err := registry.Register(openairesponses.RuntimeKind, responses); err != nil {
		return nil, err
	}
	defaults := agentDefaults(preferences.DefaultDocument())
	config, err := preferencesadapter.New(preferencesadapter.Options{UserPath: preferencesPath, Defaults: defaults, RuntimeKinds: registry.Kinds(), ProfileCatalog: registry})
	if err != nil {
		return nil, err
	}
	feature := &fixFeature{config: config, workspace: identity, prober: registry, catalog: registry}
	userResolved, err := config.Resolve(ctx, fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		return feature, err
	}
	worktrees, err := candidate.NewGitWorktreeService(filepath.Join(stateRoot, "candidates", "worktrees"), runner, candidate.GitWorktreeConfig{
		DiscoveryCommandOutputBytes: userResolved.Delivery.CommandOutputBytes,
	})
	if err != nil {
		return feature, err
	}
	direct, err := candidate.NewDirectService(filepath.Join(stateRoot, "candidates", "current"))
	if err != nil {
		_ = worktrees.Close()
		return feature, err
	}
	candidates, err := candidate.NewStrategyService(direct, worktrees)
	if err != nil {
		_ = direct.Close()
		_ = worktrees.Close()
		return feature, err
	}
	feature.candidates = candidates
	discovered, err := worktrees.DiscoverWorkspace(ctx, workspace)
	if err != nil {
		hash := sha256.Sum256([]byte(analysisRoot))
		discovered = fix.WorkspaceIdentity{Repository: fix.RepositoryID(hex.EncodeToString(hash[:16])), RepositoryRoot: analysisRoot, AnalysisRoot: analysisRoot}
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
	jobStates, err := jobstore.Open(filepath.Join(stateRoot, string(identity.Repository), "jobs"))
	if err != nil {
		return feature, err
	}
	deliveryService, err := delivery.NewGitService(runner)
	if err != nil {
		_ = jobStates.Close()
		return feature, err
	}
	var pullRequests *ghcli.Service
	if ghExecutable := strings.TrimSpace(os.Getenv(publisherExecutableEnvironment)); ghExecutable != "" {
		canonical, canonicalErr := canonicalInstallationExecutable(identity.RepositoryRoot, ghExecutable, "GitHub publisher")
		publisherRoot := filepath.Join(stateRoot, string(identity.Repository), "publisher")
		if canonicalErr == nil {
			canonicalErr = os.MkdirAll(publisherRoot, 0o700)
		}
		if canonicalErr == nil {
			canonicalErr = os.Chmod(publisherRoot, 0o700)
		}
		if canonicalErr == nil {
			pullRequests, canonicalErr = ghcli.New(ghcli.Config{Executable: canonical, WorkingDirectory: publisherRoot}, runner)
		}
		if canonicalErr != nil {
			pullRequests = nil
		}
	}
	manager, err := fixapp.New(fixapp.Dependencies{Config: config, Analysis: analysis, Candidates: candidates, ScopePlanner: candidate.UnitScopePlanner{}, DeliveryPreflight: deliveryService,
		Agents: registry, Store: jobStates, Delivery: deliveryService, Publisher: pullRequests}, fixapp.Options{
		MaxAgents: fixapp.RuntimeLimitsFromConcurrency(resolved.Concurrency).MaxAgents, MaxVerifiers: fixapp.RuntimeLimitsFromConcurrency(resolved.Concurrency).MaxVerifiers,
		JobIndexPath: filepath.Join(userDataRoot, "fix-jobs.jsonl"),
	})
	if err != nil {
		_ = jobStates.Close()
		return feature, err
	}
	feature.service = manager
	feature.candidates = nil // manager owns candidate shutdown after successful composition.
	return feature, nil
}

func agentDefaults(value preferences.Document) preferences.Document {
	value = preferences.Clone(value)
	value.Agents.Profiles = []preferences.AgentProfile{
		{ID: "codex-default", Label: "Codex", Runtime: string(codexcli.RuntimeKind),
			Executable: "codex", RuntimeProfile: "slopwatch", AuthenticationRef: "provider-owned", Options: map[string]string{}},
		{ID: "gpt-default", Label: "OpenAI API", Runtime: string(openairesponses.RuntimeKind),
			AuthenticationRef: "env:OPENAI_API_KEY", Options: map[string]string{}},
	}
	value.Fix.Profile = "codex-default"
	value.Fix.Model = ""
	value.Fix.Effort = "high"
	return value
}

const (
	publisherExecutableEnvironment = "SLOPWATCH_FIX_GH_EXECUTABLE"
)

func pathInside(root, value string) bool {
	if root == "" || value == "" {
		return false
	}
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func canonicalInstallationExecutable(repositoryRoot, value, label string) (string, error) {
	clean := filepath.Clean(value)
	if !filepath.IsAbs(value) || clean != value {
		return "", fmt.Errorf("%s must be an absolute canonical path", label)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil || resolved != clean {
		return "", fmt.Errorf("%s must resolve to its canonical path", label)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return "", fmt.Errorf("%s must be a non-writable executable regular file", label)
	}
	if pathInside(repositoryRoot, resolved) {
		return "", fmt.Errorf("%s must be installation-owned and outside the repository", label)
	}
	return resolved, nil
}

func closeFixFeature(feature *fixFeature) error {
	if feature == nil {
		return nil
	}
	return feature.Close(context.Background())
}
