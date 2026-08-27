// Package preferencesadapter translates durable preferences documents into
// the narrow application configuration API. Domain and UI packages therefore
// do not depend on TOML, preference paths, or persistence details.
package preferencesadapter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/preferences"
)

type Options struct {
	UserPath                  string
	RepositoryPreferencesRoot string
	Defaults                  preferences.Document
	RuntimeKinds              []agent.RuntimeKind
	ProfileCatalog            agent.ProfileCatalog
}

type Adapter struct {
	mu             sync.Mutex
	userPath       string
	repositoryRoot string
	defaults       preferences.Document
	runtimeKinds   map[agent.RuntimeKind]struct{}
	profileCatalog agent.ProfileCatalog
}

var _ appconfig.Resolver = (*Adapter)(nil)
var _ appconfig.Store = (*Adapter)(nil)
var _ appconfig.Editor = (*Adapter)(nil)

func (adapter *Adapter) LoadEditable(ctx context.Context, workspace fix.WorkspaceIdentity) (appconfig.Editable, error) {
	resolved, err := adapter.Resolve(ctx, workspace, appconfig.SessionOverrides{})
	if err == nil {
		return appconfig.Editable{Resolved: resolved}, nil
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	user, loadErr := preferences.LoadOrCreate(adapter.userPath, adapter.defaults)
	if loadErr != nil {
		return appconfig.Editable{}, loadErr
	}
	value, convertErr := documentToResolved(user)
	if convertErr != nil {
		return appconfig.Editable{}, convertErr
	}
	_, raw, _, loadErr := preferences.LoadPartial(adapter.userPath)
	if loadErr != nil {
		return appconfig.Editable{}, loadErr
	}
	var repositoryRaw []byte
	_, _, repositoryRaw, _, loadErr = adapter.loadRepository(workspace)
	if loadErr != nil {
		return appconfig.Editable{}, loadErr
	}
	value.Origins = builtInOrigins()
	markFixListOrigins(value.Origins, preferences.Fix{}, adapter.defaults.Fix, appconfig.OriginBuiltIn)
	markProfileEntryOrigins(value.Origins, nil, adapter.defaults.Agents.Profiles, appconfig.OriginBuiltIn)
	markUserOrigins(&value, preferences.PartialDocument{Fix: &user.Fix, Concurrency: &user.Concurrency, Agents: &user.Agents, Delivery: &user.Delivery, Interaction: &user.Interaction}, adapter.defaults, user)
	value.Revision = revision(raw, repositoryRaw)
	return appconfig.Editable{Resolved: value, Diagnostics: []string{err.Error()}}, nil
}

func New(options Options) (*Adapter, error) {
	if options.UserPath == "" {
		return nil, errors.New("preferences adapter requires a user preferences path")
	}
	repositoryRoot := options.RepositoryPreferencesRoot
	if repositoryRoot == "" {
		repositoryRoot = filepath.Join(filepath.Dir(options.UserPath), "repositories")
	}
	absoluteRepositoryRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repository preferences root: %w", err)
	}
	defaults := options.Defaults
	if defaults.Version == 0 {
		defaults = preferences.DefaultDocument()
	}
	if defaults.Version != preferences.CurrentVersion {
		return nil, fmt.Errorf("default preferences use schema version %d; supported version is %d", defaults.Version, preferences.CurrentVersion)
	}
	kinds := make(map[agent.RuntimeKind]struct{}, len(options.RuntimeKinds))
	for _, kind := range options.RuntimeKinds {
		if kind == "" {
			return nil, errors.New("runtime kind cannot be empty")
		}
		kinds[kind] = struct{}{}
	}
	return &Adapter{
		userPath:       options.UserPath,
		repositoryRoot: absoluteRepositoryRoot,
		defaults:       preferences.Clone(defaults), profileCatalog: options.ProfileCatalog,
		runtimeKinds: kinds,
	}, nil
}

func (adapter *Adapter) Resolve(ctx context.Context, workspace fix.WorkspaceIdentity, overrides appconfig.SessionOverrides) (appconfig.Resolved, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return adapter.resolve(ctx, workspace, overrides)
}

func (adapter *Adapter) Save(ctx context.Context, workspace fix.WorkspaceIdentity, scope appconfig.Scope, patch appconfig.Patch, expected appconfig.Revision) (appconfig.Saved, error) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return appconfig.Saved{}, err
	}
	currentRevision, err := adapter.revisionOnly(workspace)
	if err != nil {
		return appconfig.Saved{}, err
	}
	if expected != currentRevision {
		return appconfig.Saved{}, fmt.Errorf("%w: expected %d, current %d", appconfig.ErrRevisionConflict, expected, currentRevision)
	}
	switch scope {
	case appconfig.ScopeUser:
		err = adapter.saveUser(workspace, patch)
	case appconfig.ScopeRepository:
		err = adapter.saveRepository(workspace, patch)
	default:
		err = fmt.Errorf("unsupported preferences scope %q", scope)
	}
	if err != nil {
		return appconfig.Saved{}, err
	}
	resolved, err := adapter.resolve(ctx, workspace, appconfig.SessionOverrides{})
	if err != nil {
		return appconfig.Saved{}, err
	}
	return appconfig.Saved{Revision: resolved.Revision, Resolved: resolved}, nil
}

func (adapter *Adapter) revisionOnly(workspace fix.WorkspaceIdentity) (appconfig.Revision, error) {
	if _, err := preferences.LoadOrCreate(adapter.userPath, adapter.defaults); err != nil {
		return 0, err
	}
	_, userRaw, _, err := preferences.LoadPartial(adapter.userPath)
	if err != nil {
		return 0, err
	}
	_, _, repositoryRaw, _, err := adapter.loadRepository(workspace)
	if err != nil {
		return 0, err
	}
	return revision(userRaw, repositoryRaw), nil
}

func (adapter *Adapter) resolve(ctx context.Context, workspace fix.WorkspaceIdentity, overrides appconfig.SessionOverrides) (appconfig.Resolved, error) {
	if err := ctx.Err(); err != nil {
		return appconfig.Resolved{}, err
	}
	user, userPartial, userRaw, err := adapter.loadValidUser()
	if err != nil {
		return appconfig.Resolved{}, err
	}

	resolved, err := documentToResolved(user)
	if err != nil {
		return appconfig.Resolved{}, fmt.Errorf("resolve user preferences: %w", err)
	}
	resolved.Origins = builtInOrigins()
	markFixListOrigins(resolved.Origins, preferences.Fix{}, adapter.defaults.Fix, appconfig.OriginBuiltIn)
	markProfileEntryOrigins(resolved.Origins, nil, adapter.defaults.Agents.Profiles, appconfig.OriginBuiltIn)
	markUserOrigins(&resolved, userPartial, adapter.defaults, user)

	repositoryPath, repository, repositoryRaw, exists, err := adapter.loadRepository(workspace)
	if err != nil {
		return appconfig.Resolved{}, err
	}
	if exists {
		if applyErr := adapter.applyRepository(&resolved, repository); applyErr != nil {
			if quarantineErr := preferences.Quarantine(repositoryPath); quarantineErr != nil {
				return appconfig.Resolved{}, fmt.Errorf("reset invalid repository preferences: %w", quarantineErr)
			}
			repositoryRaw = nil
		}
	}
	resolved.Revision = revision(userRaw, repositoryRaw)
	applyOverrides(&resolved, overrides)
	return cloneResolved(resolved), nil
}

func (adapter *Adapter) loadValidUser() (preferences.Document, preferences.PartialDocument, []byte, error) {
	user, err := preferences.LoadOrCreate(adapter.userPath, adapter.defaults)
	if err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, err
	}
	partial, raw, _, err := preferences.LoadPartial(adapter.userPath)
	if err == nil {
		err = adapter.validateDocument(user)
	}
	if err == nil {
		_, err = documentToResolved(user)
	}
	if err == nil {
		return user, partial, raw, nil
	}
	if defaultsErr := adapter.validateDocument(adapter.defaults); defaultsErr != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("validate user preferences: %w", err)
	}
	if _, defaultsErr := documentToResolved(adapter.defaults); defaultsErr != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("resolve user preferences: %w", err)
	}
	user, err = preferences.Recover(adapter.userPath, adapter.defaults)
	if err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, err
	}
	partial, raw, _, err = preferences.LoadPartial(adapter.userPath)
	if err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, err
	}
	if err := adapter.validateDocument(user); err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("validate built-in preferences: %w", err)
	}
	if _, err := documentToResolved(user); err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("resolve built-in preferences: %w", err)
	}
	return user, partial, raw, nil
}

func (adapter *Adapter) loadRepository(workspace fix.WorkspaceIdentity) (string, preferences.PartialDocument, []byte, bool, error) {
	path, hasRepository := adapter.repositoryPreferencesPath(workspace)
	if !hasRepository {
		return "", preferences.PartialDocument{}, nil, false, nil
	}
	value, raw, exists, err := preferences.LoadPartial(path)
	if err == nil {
		return path, value, raw, exists, nil
	}
	if quarantineErr := preferences.Quarantine(path); quarantineErr != nil {
		return "", preferences.PartialDocument{}, nil, false, fmt.Errorf("reset invalid repository preferences: %w", quarantineErr)
	}
	return path, preferences.PartialDocument{}, nil, false, nil
}

func (adapter *Adapter) repositoryPreferencesPath(workspace fix.WorkspaceIdentity) (string, bool) {
	id := strings.TrimSpace(string(workspace.Repository))
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", false
	}
	return filepath.Join(adapter.repositoryRoot, id, "preferences.toml"), true
}

func revision(user, repository []byte) appconfig.Revision {
	hash := sha256.New()
	hash.Write([]byte("user\x00"))
	hash.Write(user)
	hash.Write([]byte("\x00repository\x00"))
	hash.Write(repository)
	sum := hash.Sum(nil)
	value := binary.BigEndian.Uint64(sum[:8])
	if value == 0 {
		value = 1
	}
	return appconfig.Revision(value)
}

func builtInOrigins() map[string]appconfig.Origin {
	result := map[string]appconfig.Origin{
		"fix":                      appconfig.OriginBuiltIn,
		"concurrency":              appconfig.OriginBuiltIn,
		"agents":                   appconfig.OriginBuiltIn,
		"delivery":                 appconfig.OriginBuiltIn,
		"interaction.trend_window": appconfig.OriginBuiltIn,
	}
	for _, key := range []string{"fix.target_score", "fix.focus", "fix.change_scope", "fix.profile", "fix.model", "fix.effort", "fix.prompt_template", "concurrency.max_agents", "concurrency.max_verifiers", "concurrency.max_actors_per_job", "concurrency.max_candidate_preview_bytes", "concurrency.max_candidate_preview_lines", "delivery.workspace", "delivery.git", "delivery.publish", "delivery.remote", "delivery.base_branch", "delivery.branch_template", "delivery.publisher", "delivery.draft_pull_requests", "delivery.command_output_bytes"} {
		result[key] = appconfig.OriginBuiltIn
	}
	return result
}

func markUserOrigins(resolved *appconfig.Resolved, partial preferences.PartialDocument, defaults, user preferences.Document) {
	if partial.Fix != nil && !reflect.DeepEqual(defaults.Fix, user.Fix) {
		resolved.Origins["fix"] = appconfig.OriginUser
		markFixFieldOrigins(resolved.Origins, defaults.Fix, user.Fix, appconfig.OriginUser)
		markFixListOrigins(resolved.Origins, defaults.Fix, user.Fix, appconfig.OriginUser)
	}
	if partial.Concurrency != nil && !reflect.DeepEqual(defaults.Concurrency, user.Concurrency) {
		resolved.Origins["concurrency"] = appconfig.OriginUser
		markConcurrencyFieldOrigins(resolved.Origins, defaults.Concurrency, user.Concurrency, appconfig.OriginUser)
	}
	if partial.Agents != nil && !reflect.DeepEqual(defaults.Agents, user.Agents) {
		resolved.Origins["agents"] = appconfig.OriginUser
		markProfileEntryOrigins(resolved.Origins, defaults.Agents.Profiles, user.Agents.Profiles, appconfig.OriginUser)
	}
	if partial.Delivery != nil && !reflect.DeepEqual(defaults.Delivery, user.Delivery) {
		resolved.Origins["delivery"] = appconfig.OriginUser
		markDeliveryFieldOrigins(resolved.Origins, defaults.Delivery, user.Delivery, appconfig.OriginUser)
	}
	if partial.Interaction != nil && defaults.Interaction.TrendWindow != user.Interaction.TrendWindow {
		resolved.Origins["interaction.trend_window"] = appconfig.OriginUser
	}
}

func markFixFieldOrigins(origins map[string]appconfig.Origin, before, after preferences.Fix, origin appconfig.Origin) {
	values := []struct {
		key     string
		changed bool
	}{{"fix.target_score", before.TargetScore != after.TargetScore}, {"fix.focus", !reflect.DeepEqual(before.Focus, after.Focus)}, {"fix.change_scope", before.ChangeScope != after.ChangeScope}, {"fix.profile", before.Profile != after.Profile}, {"fix.model", before.Model != after.Model}, {"fix.effort", before.Effort != after.Effort}, {"fix.prompt_template", before.PromptTemplate != after.PromptTemplate}}
	for _, value := range values {
		if value.changed {
			origins[value.key] = origin
		}
	}
}
func markConcurrencyFieldOrigins(origins map[string]appconfig.Origin, before, after preferences.Concurrency, origin appconfig.Origin) {
	values := []struct {
		key     string
		changed bool
	}{{"concurrency.max_agents", before.MaxAgents != after.MaxAgents}, {"concurrency.max_verifiers", before.MaxVerifiers != after.MaxVerifiers}, {"concurrency.max_actors_per_job", before.MaxActorsPerJob != after.MaxActorsPerJob}, {"concurrency.max_candidate_preview_bytes", before.MaxCandidatePreviewBytes != after.MaxCandidatePreviewBytes}, {"concurrency.max_candidate_preview_lines", before.MaxCandidatePreviewLines != after.MaxCandidatePreviewLines}}
	for _, v := range values {
		if v.changed {
			origins[v.key] = origin
		}
	}
}
func markDeliveryFieldOrigins(origins map[string]appconfig.Origin, before, after preferences.Delivery, origin appconfig.Origin) {
	values := []struct {
		key     string
		changed bool
	}{{"delivery.workspace", before.Workspace != after.Workspace}, {"delivery.git", before.Git != after.Git}, {"delivery.publish", before.Publish != after.Publish}, {"delivery.remote", before.Remote != after.Remote}, {"delivery.base_branch", before.BaseBranch != after.BaseBranch}, {"delivery.branch_template", before.BranchTemplate != after.BranchTemplate}, {"delivery.publisher", before.Publisher != after.Publisher}, {"delivery.draft_pull_requests", before.DraftPullRequests != after.DraftPullRequests}, {"delivery.command_output_bytes", before.CommandOutputBytes != after.CommandOutputBytes},
		{"delivery.commit_title_template", before.CommitTitleTemplate != after.CommitTitleTemplate}, {"delivery.commit_body_template", before.CommitBodyTemplate != after.CommitBodyTemplate},
		{"delivery.pull_request_title_template", before.PullRequestTitleTemplate != after.PullRequestTitleTemplate}, {"delivery.pull_request_body_template", before.PullRequestBodyTemplate != after.PullRequestBodyTemplate}}
	for _, v := range values {
		if v.changed {
			origins[v.key] = origin
		}
	}
}

func markProfileEntryOrigins(origins map[string]appconfig.Origin, before, after []preferences.AgentProfile, origin appconfig.Origin) {
	prior := make(map[string]preferences.AgentProfile, len(before))
	for _, profile := range before {
		prior[profile.ID] = profile
	}
	for _, profile := range after {
		old, exists := prior[profile.ID]
		prefix := "agents." + profile.ID + "."
		if !exists || !reflect.DeepEqual(old, profile) {
			origins["agents."+profile.ID] = origin
		}
		fields := []struct {
			key     string
			changed bool
		}{{"label", !exists || old.Label != profile.Label}, {"runtime", !exists || old.Runtime != profile.Runtime}, {"executable", !exists || old.Executable != profile.Executable}, {"runtime_profile", !exists || old.RuntimeProfile != profile.RuntimeProfile}, {"authentication_ref", !exists || old.AuthenticationRef != profile.AuthenticationRef}, {"options", !exists || !reflect.DeepEqual(old.Options, profile.Options)}}
		for _, field := range fields {
			if field.changed {
				origins[prefix+field.key] = origin
			}
		}
		for key, value := range profile.Options {
			if !exists || old.Options[key] != value {
				origins[prefix+"options."+key] = origin
			}
		}
	}
}

func markFixListOrigins(origins map[string]appconfig.Origin, before, after preferences.Fix, origin appconfig.Origin) {
	prior := make(map[string]struct{}, len(before.Focus))
	for _, metric := range before.Focus {
		prior[metric] = struct{}{}
	}
	for _, metric := range after.Focus {
		if _, exists := prior[metric]; !exists {
			origins["fix.focus."+metric] = origin
		}
	}
}

func (adapter *Adapter) saveUser(workspace fix.WorkspaceIdentity, patch appconfig.Patch) error {
	value, err := preferences.LoadOrCreate(adapter.userPath, adapter.defaults)
	if err != nil {
		return err
	}
	if err := applyUserPatch(&value, patch); err != nil {
		return err
	}
	if err := adapter.validateDocument(value); err != nil {
		return fmt.Errorf("validate user preferences: %w", err)
	}
	if path, repository, _, exists, err := adapter.loadRepository(workspace); err != nil {
		return err
	} else if exists {
		candidate, err := documentToResolved(value)
		if err != nil {
			return err
		}
		candidate.Origins = builtInOrigins()
		if err := adapter.applyRepository(&candidate, repository); err != nil {
			if quarantineErr := preferences.Quarantine(path); quarantineErr != nil {
				return fmt.Errorf("reset invalid repository preferences: %w", quarantineErr)
			}
		}
	}
	return preferences.Save(adapter.userPath, value)
}

func (adapter *Adapter) saveRepository(workspace fix.WorkspaceIdentity, patch appconfig.Patch) error {
	path, hasRepository := adapter.repositoryPreferencesPath(workspace)
	if !hasRepository {
		return errors.New("repository-scoped preferences require a repository identity")
	}
	_, value, _, _, err := adapter.loadRepository(workspace)
	if err != nil {
		return err
	}
	if err := applyRepositoryPatch(&value, patch); err != nil {
		return err
	}
	if err := validateRepositoryPartial(value); err != nil {
		return err
	}
	// Validate the complete effective configuration, including trusted-plan
	// selection, before replacing the repository document.
	user, err := preferences.LoadOrCreate(adapter.userPath, adapter.defaults)
	if err != nil {
		return err
	}
	candidate, err := documentToResolved(user)
	if err != nil {
		return err
	}
	candidate.Origins = builtInOrigins()
	if err := adapter.applyRepository(&candidate, value); err != nil {
		return fmt.Errorf("validate repository preferences: %w", err)
	}
	return preferences.SavePartial(path, value)
}
