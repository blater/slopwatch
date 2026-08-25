package appconfig

import (
	"context"
	"fmt"
	"sync"

	"github.com/blater/slopwatch/internal/fix"
)

// Memory is the deterministic resolver/store used by domain and TUI tests.
// It also documents optimistic-revision and immutable-snapshot semantics for
// file-backed adapters.
type Memory struct {
	mu       sync.RWMutex
	resolved Resolved
}

func NewMemory(initial Resolved) *Memory {
	if initial.SchemaVersion == 0 {
		initial.SchemaVersion = 1
	}
	if initial.Revision == 0 {
		initial.Revision = 1
	}
	return &Memory{resolved: cloneResolved(initial)}
}

func (memory *Memory) Resolve(_ context.Context, _ fix.WorkspaceIdentity, overrides SessionOverrides) (Resolved, error) {
	memory.mu.RLock()
	result := cloneResolved(memory.resolved)
	memory.mu.RUnlock()
	if overrides.TargetScore != nil {
		result.Fix.TargetScore = *overrides.TargetScore
		result.Origins["fix.target_score"] = OriginSession
	}
	if overrides.Profile != nil {
		result.Fix.Profile = *overrides.Profile
		result.Origins["fix.profile"] = OriginSession
	}
	if overrides.Model != nil {
		result.Fix.Model = *overrides.Model
		result.Origins["fix.model"] = OriginSession
	}
	if overrides.Effort != nil {
		result.Fix.Effort = *overrides.Effort
		result.Origins["fix.effort"] = OriginSession
	}
	if overrides.TrendWindow != nil {
		result.TrendWindow = *overrides.TrendWindow
		result.Origins["interaction.trend_window"] = OriginSession
	}
	return result, nil
}

func (memory *Memory) Save(_ context.Context, _ fix.WorkspaceIdentity, scope Scope, patch Patch, expected Revision) (Saved, error) {
	if scope != ScopeUser && scope != ScopeRepository {
		return Saved{}, fmt.Errorf("unsupported preferences scope %q", scope)
	}
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if expected != memory.resolved.Revision {
		return Saved{}, fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expected, memory.resolved.Revision)
	}
	applyPatch(&memory.resolved, patch, originForScope(scope))
	memory.resolved.Revision++
	result := cloneResolved(memory.resolved)
	return Saved{Revision: result.Revision, Resolved: result}, nil
}

func originForScope(scope Scope) Origin {
	if scope == ScopeRepository {
		return OriginRepository
	}
	return OriginUser
}

func applyPatch(value *Resolved, patch Patch, origin Origin) {
	if patch.Fix != nil {
		value.Fix = cloneFixDefaults(*patch.Fix)
		value.Origins["fix"] = origin
	}
	if patch.Concurrency != nil {
		value.Concurrency = *patch.Concurrency
		value.Origins["concurrency"] = origin
	}
	if patch.Profiles != nil {
		value.Profiles = cloneProfiles(*patch.Profiles)
		value.Origins["agents"] = origin
	}
	if patch.Validation != nil {
		value.Validation = cloneValidation(*patch.Validation)
		value.Origins["validation"] = origin
	}
	if patch.ValidationWorkspace != nil {
		value.ValidationWorkspace = *patch.ValidationWorkspace
		value.Origins["validation_workspace"] = origin
	}
	if patch.Delivery != nil {
		value.Delivery = *patch.Delivery
		value.Origins["delivery"] = origin
	}
	if patch.TrendWindow != nil {
		value.TrendWindow = *patch.TrendWindow
		value.Origins["interaction.trend_window"] = origin
	}
}
