package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	isolationdocker "github.com/blater/slopwatch/internal/isolation/docker"
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
	sensitiveRoots := fixSensitiveRoots(identity, stateRoot, preferencesPath)
	confinement := isolation.SelectCandidateConfinement(runner, isolation.ConfinementOptions{})

	registry := agent.NewRegistry()
	// The exact provider probe and trusted validation consume the same platform
	// confinement capability. Unsupported hosts remain fail-closed through that
	// shared selector rather than independent composition booleans.
	strategy := codexcli.New(runner, codexcli.ExecutableChecker{Runner: runner, Confinement: confinement})
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
	defaults := agentDefaults(preferences.DefaultDocument(), sensitiveRoots)
	config, err := preferencesadapter.New(preferencesadapter.Options{UserPath: preferencesPath, Defaults: defaults, RuntimeKinds: registry.Kinds(), ProfileCatalog: registry})
	if err != nil {
		return nil, err
	}
	feature := &fixFeature{config: config, workspace: identity, prober: registry, catalog: registry}
	userResolved, err := config.Resolve(ctx, fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		return feature, err
	}
	candidates, err := candidate.NewGitWorktreeService(filepath.Join(stateRoot, "candidates"), runner, candidate.GitWorktreeConfig{
		DiscoveryCommandOutputBytes: userResolved.Delivery.CommandOutputBytes,
	})
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
	journal, err := jobstore.OpenFile(filepath.Join(stateRoot, string(identity.Repository), "jobs"))
	if err != nil {
		return feature, err
	}
	// Opening the journal acquires the repository installation lease. Container
	// orphan reconciliation must occur only while that lease is held, so a
	// second Slopwatch process cannot race cleanup or claim the same containers.
	validationProperties := loadValidationInstallationProperties(identity, resolved.Validation)
	validationConfinement := configuredValidationConfinement(ctx, filepath.Join(stateRoot, string(identity.Repository), "containers"), validationProperties, resolved.ValidationWorkspace)
	validator, err := validation.NewRunner(validation.ConfiningExecutor{Confinement: validationConfinement, SensitiveRoots: sensitiveRoots},
		validation.RunnerConfig{Environment: validationProperties.ContainerEnvironment, WorkspaceLimits: isolation.WorkspaceLimits{
			MaxFiles: resolved.ValidationWorkspace.MaxFiles, MaxDirectories: resolved.ValidationWorkspace.MaxDirectories,
			MaxPathBytes: resolved.ValidationWorkspace.MaxPathBytes, MaxFileBytes: resolved.ValidationWorkspace.MaxFileBytes,
			MaxTotalBytes: resolved.ValidationWorkspace.MaxTotalBytes,
		}})
	if err != nil {
		_ = journal.Close()
		return feature, err
	}
	deliveryService, err := delivery.NewGitService(runner)
	if err != nil {
		_ = journal.Close()
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
	manager, err := fixapp.New(fixapp.Dependencies{Config: config, Analysis: analysis, Validation: validator, Candidates: candidates, ScopePlanner: candidate.UnitScopePlanner{}, DeliveryPreflight: deliveryService,
		Agents: registry, Store: journal, Delivery: deliveryService, Publisher: pullRequests, SecretAdmission: openairesponses.SecretAdmission{Resolver: responsesSecrets}}, fixapp.Options{
		MaxAgents: fixapp.RuntimeLimitsFromConcurrency(resolved.Concurrency).MaxAgents, MaxVerifiers: fixapp.RuntimeLimitsFromConcurrency(resolved.Concurrency).MaxVerifiers,
		MaxRetainedJobs:            fixapp.RuntimeLimitsFromConcurrency(resolved.Concurrency).MaxRetainedJobs,
		MaxTranscriptBytes:         fixapp.RuntimeLimitsFromConcurrency(resolved.Concurrency).MaxTranscriptBytes,
		StartupValidationWorkspace: resolved.ValidationWorkspace,
	})
	if err != nil {
		_ = journal.Close()
		return feature, err
	}
	feature.service = manager
	feature.candidates = nil // manager owns candidate shutdown after successful composition.
	return feature, nil
}

func fixSensitiveRoots(workspace fix.WorkspaceIdentity, stateRoot, preferencesPath string) []string {
	denied := []string{workspace.RepositoryRoot, stateRoot}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		denied = append(denied, home)
	}
	if preferencesPath != "" {
		denied = append(denied, filepath.Dir(preferencesPath))
	}
	return denied
}

func agentDefaults(value preferences.Document, denied []string) preferences.Document {
	value = preferences.Clone(value)
	value.Agents.Profiles = []preferences.AgentProfile{
		{ID: "codex-default", Label: "Codex — managed sign-in (ChatGPT recommended)", Runtime: string(codexcli.RuntimeKind),
			Executable: "codex", RuntimeProfile: "slopwatch", AuthenticationRef: "provider-owned",
			Options: map[string]string{"denied_read_roots": strings.Join(denied, string(os.PathListSeparator))}},
		{ID: "gpt-default", Label: "OpenAI Responses API — API key", Runtime: string(openairesponses.RuntimeKind),
			AuthenticationRef: "env:OPENAI_API_KEY", Options: map[string]string{}},
	}
	value.Fix.Profile = "codex-default"
	value.Fix.Model = ""
	value.Fix.Effort = "high"
	value.Fix.Delegation = string(agent.DelegationSingle)
	return value
}

const (
	validationImageEnvironment            = "SLOPWATCH_FIX_CONTAINER_IMAGE"
	validationHostEnvironment             = "SLOPWATCH_FIX_DOCKER_HOST"
	validationDockerExecutableEnvironment = "SLOPWATCH_FIX_DOCKER_EXECUTABLE"
	validationExecutableMapEnvironment    = "SLOPWATCH_FIX_EXECUTABLE_MAP"
	containerBinaryPath                   = "/usr/local/bin/slopmark"
	publisherExecutableEnvironment        = "SLOPWATCH_FIX_GH_EXECUTABLE"
)

type validationInstallationProperties struct {
	Image, DockerHost, DockerExecutable string
	SupervisorPath, ProbeExecutable     string
	ExecutableMap                       map[string]string
	ContainerEnvironment                []string
	Diagnostic                          string
}

// loadValidationInstallationProperties is the temporary installation boundary
// for the properties-file PR. Environment access stops here; neither the
// validation service nor Docker adapter reads ambient configuration.
func loadValidationInstallationProperties(identity fix.WorkspaceIdentity, plans []validation.Plan) validationInstallationProperties {
	properties := validationInstallationProperties{Image: strings.TrimSpace(os.Getenv(validationImageEnvironment)), DockerHost: strings.TrimSpace(os.Getenv(validationHostEnvironment)), SupervisorPath: containerBinaryPath, ProbeExecutable: containerBinaryPath,
		ContainerEnvironment: []string{"LANG=C.UTF-8", "LC_ALL=C", "PATH=/usr/local/bin:/usr/bin:/bin", "CI=1", "GIT_TERMINAL_PROMPT=0"}}
	if properties.Image == "" {
		properties.Diagnostic = "set " + validationImageEnvironment + " to an installation-owned immutable image digest"
		return properties
	}
	if properties.DockerHost == "" {
		properties.Diagnostic = validationHostEnvironment + " must name an explicit local unix:// Docker socket"
		return properties
	}
	dockerExecutable := strings.TrimSpace(os.Getenv(validationDockerExecutableEnvironment))
	if dockerExecutable == "" {
		properties.Diagnostic = validationDockerExecutableEnvironment + " must name the canonical installation-owned Docker CLI"
		return properties
	}
	dockerExecutable, err := canonicalInstallationExecutable(identity.RepositoryRoot, dockerExecutable, "Docker CLI")
	if err != nil {
		properties.Diagnostic = err.Error()
		return properties
	}
	properties.DockerExecutable = dockerExecutable
	rawMap := strings.TrimSpace(os.Getenv(validationExecutableMapEnvironment))
	if rawMap == "" {
		for _, plan := range plans {
			if len(plan.Checks) > 0 {
				properties.Diagnostic = validationExecutableMapEnvironment + " must be an explicit JSON host-to-container executable map"
				return properties
			}
		}
		properties.ExecutableMap = map[string]string{}
		return properties
	}
	if len(rawMap) > 64<<10 || json.Unmarshal([]byte(rawMap), &properties.ExecutableMap) != nil || len(properties.ExecutableMap) == 0 {
		properties.Diagnostic = validationExecutableMapEnvironment + " is not a valid bounded JSON executable map"
		return properties
	}
	for _, plan := range plans {
		for _, check := range plan.Checks {
			mapped, ok := properties.ExecutableMap[check.Executable]
			if !ok || !filepath.IsAbs(check.Executable) || !strings.HasPrefix(mapped, "/") {
				properties.Diagnostic = "installation executable map does not cover validation check " + string(check.ID)
				return properties
			}
		}
	}
	return properties
}

// configuredValidationConfinement translates installation-owned environment
// properties into the deep confinement port. Repository preferences can only
// select a trusted validation plan; they never choose an image, daemon, mount,
// executable mapping, or container security option.
func configuredValidationConfinement(ctx context.Context, containerState string, properties validationInstallationProperties, policy appconfig.ValidationWorkspace) isolation.CandidateConfinement {
	if properties.Diagnostic != "" {
		return isolation.UnsupportedConfinement{Reason: "validation confinement is not configured: " + properties.Diagnostic}
	}
	if err := os.MkdirAll(containerState, 0o700); err != nil {
		return isolation.UnsupportedConfinement{Reason: "validation confinement state could not be created"}
	}
	info, err := os.Lstat(containerState)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return isolation.UnsupportedConfinement{Reason: "validation confinement state is not a private directory"}
	}
	if err := os.Chmod(containerState, 0o700); err != nil {
		return isolation.UnsupportedConfinement{Reason: "validation confinement state permissions could not be secured"}
	}
	installationID, err := durableConfinementOwner(containerState)
	if err != nil {
		return isolation.UnsupportedConfinement{Reason: "validation confinement owner identity is unavailable: " + err.Error()}
	}
	client, err := isolationdocker.NewCommandClient(properties.DockerExecutable, properties.DockerHost)
	if err != nil {
		return isolation.UnsupportedConfinement{Reason: "validation Docker client is unavailable: " + err.Error()}
	}
	backend, err := isolationdocker.New(isolationdocker.Config{
		Client: client, Image: properties.Image, InstallationID: installationID, StateRoot: containerState,
		SupervisorPath: properties.SupervisorPath, ProbeExecutable: properties.ProbeExecutable, ExecutableMap: properties.ExecutableMap,
		PIDsLimit: policy.ContainerPIDs, MemoryBytes: policy.ContainerMemoryBytes, CPUMillis: policy.ContainerCPUMillis,
		TemporaryBytes: policy.ContainerTemporaryBytes, WorkspaceBytes: policy.ContainerWorkspaceBytes,
		NofileLimit: policy.ContainerNofileLimit, GeneratedFileBytes: policy.ContainerGeneratedFileBytes,
		StopTimeout: policy.ContainerStopTimeout, ControlTimeout: policy.ContainerControlTimeout,
		SentinelWallTime: policy.ContainerSentinelTimeout, CrashProbeWallTime: policy.ContainerCrashProbeTimeout,
	})
	if err != nil {
		return isolation.UnsupportedConfinement{Reason: "validation confinement configuration was rejected: " + err.Error()}
	}
	if err := backend.Reconcile(ctx); err != nil {
		return isolation.UnsupportedConfinement{Reason: "validation confinement readiness failed: " + err.Error()}
	}
	return backend
}

func durableConfinementOwner(stateRoot string) (string, error) {
	root, err := os.OpenRoot(stateRoot)
	if err != nil {
		return "", errors.New("open owner state root")
	}
	defer root.Close()
	return durableConfinementOwnerRoot(root)
}

func durableConfinementOwnerRoot(root *os.Root) (string, error) {
	const name = "owner-id"
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", errors.New("generate owner identity")
	}
	value := hex.EncodeToString(data)
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, writeErr := file.WriteString(value + "\n"); writeErr != nil {
			_ = file.Close()
			return "", errors.New("write owner identity")
		}
		if syncErr := file.Sync(); syncErr != nil {
			_ = file.Close()
			return "", errors.New("sync owner identity")
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", errors.New("close owner identity")
		}
		directory, openErr := root.Open(".")
		if openErr != nil {
			return "", errors.New("open owner directory for sync")
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil || closeErr != nil {
			return "", errors.New("sync owner directory")
		}
		return value, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return "", errors.New("create owner identity")
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() <= 0 || info.Size() > 128 {
		return "", errors.New("owner identity is not a private bounded regular file")
	}
	existing, err := root.ReadFile(name)
	if err != nil {
		return "", errors.New("read owner identity")
	}
	value = strings.TrimSpace(string(existing))
	decoded, decodeErr := hex.DecodeString(value)
	if decodeErr != nil || len(decoded) != 16 {
		return "", errors.New("owner identity is invalid")
	}
	return value, nil
}

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
