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
	mu        sync.Mutex
	storage   preferencesStore
	validator profileValidator
}

type preferencesStore struct {
	userPath       string
	repositoryRoot string
	defaults       preferences.Document
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
	user, loadErr := preferences.LoadOrCreate(adapter.storage.userPath, adapter.storage.defaults)
	if loadErr != nil {
		return appconfig.Editable{}, loadErr
	}
	value, convertErr := documentToResolved(user)
	if convertErr != nil {
		return appconfig.Editable{}, convertErr
	}
	_, raw, _, loadErr := preferences.LoadPartial(adapter.storage.userPath)
	if loadErr != nil {
		return appconfig.Editable{}, loadErr
	}
	var repositoryRaw []byte
	_, _, repositoryRaw, _, loadErr = adapter.storage.loadRepository(workspace)
	if loadErr != nil {
		return appconfig.Editable{}, loadErr
	}
	value.Origins = builtInOrigins()
	markFixListOrigins(value.Origins, preferences.Fix{}, adapter.storage.defaults.Fix, appconfig.OriginBuiltIn)
	markProfileEntryOrigins(value.Origins, nil, adapter.storage.defaults.Agents.Profiles, appconfig.OriginBuiltIn)
	markUserOrigins(&value, preferences.PartialDocument{Fix: &user.Fix, Concurrency: &user.Concurrency, Agents: &user.Agents, Delivery: &user.Delivery, Interaction: &user.Interaction}, adapter.storage.defaults, user)
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
		storage:   preferencesStore{userPath: options.UserPath, repositoryRoot: absoluteRepositoryRoot, defaults: preferences.Clone(defaults)},
		validator: profileValidator{runtimeKinds: kinds, catalog: options.ProfileCatalog},
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
	currentRevision, err := adapter.storage.revisionOnly(workspace)
	if err != nil {
		return appconfig.Saved{}, err
	}
	if expected != currentRevision {
		return appconfig.Saved{}, fmt.Errorf("%w: expected %d, current %d", appconfig.ErrRevisionConflict, expected, currentRevision)
	}
	switch scope {
	case appconfig.ScopeUser:
		err = adapter.storage.saveUser(workspace, patch, adapter.validator)
	case appconfig.ScopeRepository:
		err = adapter.storage.saveRepository(workspace, patch, adapter.validator)
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

func (store preferencesStore) revisionOnly(workspace fix.WorkspaceIdentity) (appconfig.Revision, error) {
	if _, err := preferences.LoadOrCreate(store.userPath, store.defaults); err != nil {
		return 0, err
	}
	_, userRaw, _, err := preferences.LoadPartial(store.userPath)
	if err != nil {
		return 0, err
	}
	_, _, repositoryRaw, _, err := store.loadRepository(workspace)
	if err != nil {
		return 0, err
	}
	return revision(userRaw, repositoryRaw), nil
}

func (adapter *Adapter) resolve(ctx context.Context, workspace fix.WorkspaceIdentity, overrides appconfig.SessionOverrides) (appconfig.Resolved, error) {
	if err := ctx.Err(); err != nil {
		return appconfig.Resolved{}, err
	}
	user, userPartial, userRaw, err := adapter.storage.loadValidUser(adapter.validator)
	if err != nil {
		return appconfig.Resolved{}, err
	}

	resolved, err := documentToResolved(user)
	if err != nil {
		return appconfig.Resolved{}, fmt.Errorf("resolve user preferences: %w", err)
	}
	resolved.Origins = builtInOrigins()
	markFixListOrigins(resolved.Origins, preferences.Fix{}, adapter.storage.defaults.Fix, appconfig.OriginBuiltIn)
	markProfileEntryOrigins(resolved.Origins, nil, adapter.storage.defaults.Agents.Profiles, appconfig.OriginBuiltIn)
	markUserOrigins(&resolved, userPartial, adapter.storage.defaults, user)

	repositoryPath, repository, repositoryRaw, exists, err := adapter.storage.loadRepository(workspace)
	if err != nil {
		return appconfig.Resolved{}, err
	}
	if exists {
		if applyErr := adapter.validator.applyRepository(&resolved, repository); applyErr != nil {
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

func (store preferencesStore) loadValidUser(validator profileValidator) (preferences.Document, preferences.PartialDocument, []byte, error) {
	user, err := preferences.LoadOrCreate(store.userPath, store.defaults)
	if err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, err
	}
	partial, raw, _, err := preferences.LoadPartial(store.userPath)
	if err == nil {
		err = validateDocument(user, validator)
	}
	if err == nil {
		_, err = documentToResolved(user)
	}
	if err == nil {
		return user, partial, raw, nil
	}
	if defaultsErr := validateDocument(store.defaults, validator); defaultsErr != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("validate user preferences: %w", err)
	}
	if _, defaultsErr := documentToResolved(store.defaults); defaultsErr != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("resolve user preferences: %w", err)
	}
	user, err = preferences.Recover(store.userPath, store.defaults)
	if err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, err
	}
	partial, raw, _, err = preferences.LoadPartial(store.userPath)
	if err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, err
	}
	if err := validateDocument(user, validator); err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("validate built-in preferences: %w", err)
	}
	if _, err := documentToResolved(user); err != nil {
		return preferences.Document{}, preferences.PartialDocument{}, nil, fmt.Errorf("resolve built-in preferences: %w", err)
	}
	return user, partial, raw, nil
}

func (store preferencesStore) loadRepository(workspace fix.WorkspaceIdentity) (string, preferences.PartialDocument, []byte, bool, error) {
	path, hasRepository := store.repositoryPreferencesPath(workspace)
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

func (store preferencesStore) repositoryPreferencesPath(workspace fix.WorkspaceIdentity) (string, bool) {
	id := strings.TrimSpace(string(workspace.Repository))
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", false
	}
	return filepath.Join(store.repositoryRoot, id, "preferences.toml"), true
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

func (store preferencesStore) saveUser(workspace fix.WorkspaceIdentity, patch appconfig.Patch, validator profileValidator) error {
	value, err := preferences.LoadOrCreate(store.userPath, store.defaults)
	if err != nil {
		return err
	}
	if err := applyUserPatch(&value, patch); err != nil {
		return err
	}
	if err := validateDocument(value, validator); err != nil {
		return fmt.Errorf("validate user preferences: %w", err)
	}
	if path, repository, _, exists, err := store.loadRepository(workspace); err != nil {
		return err
	} else if exists {
		candidate, err := documentToResolved(value)
		if err != nil {
			return err
		}
		candidate.Origins = builtInOrigins()
		if err := validator.applyRepository(&candidate, repository); err != nil {
			if quarantineErr := preferences.Quarantine(path); quarantineErr != nil {
				return fmt.Errorf("reset invalid repository preferences: %w", quarantineErr)
			}
		}
	}
	return preferences.Save(store.userPath, value)
}

func (store preferencesStore) saveRepository(workspace fix.WorkspaceIdentity, patch appconfig.Patch, validator profileValidator) error {
	path, hasRepository := store.repositoryPreferencesPath(workspace)
	if !hasRepository {
		return errors.New("repository-scoped preferences require a repository identity")
	}
	_, value, _, _, err := store.loadRepository(workspace)
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
	user, err := preferences.LoadOrCreate(store.userPath, store.defaults)
	if err != nil {
		return err
	}
	candidate, err := documentToResolved(user)
	if err != nil {
		return err
	}
	candidate.Origins = builtInOrigins()
	if err := validator.applyRepository(&candidate, value); err != nil {
		return fmt.Errorf("validate repository preferences: %w", err)
	}
	return preferences.SavePartial(path, value)
}
