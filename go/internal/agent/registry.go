package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

var ErrUnknownRuntime = errors.New("unknown agent runtime")

// Registry is safe for concurrent reads and registration. Composition should
// finish registration before jobs are admitted; duplicate runtime kinds are
// rejected rather than silently replaced.
type Registry struct {
	mu         sync.RWMutex
	strategies map[RuntimeKind]Strategy
}

func NewRegistry() *Registry {
	return &Registry{strategies: map[RuntimeKind]Strategy{}}
}

func (registry *Registry) Register(kind RuntimeKind, strategy Strategy) error {
	if kind == "" || strategy == nil {
		return errors.New("runtime kind and strategy are required")
	}
	descriptor := strategy.ProfileDescriptor()
	if descriptor.Runtime != kind || descriptor.Label == "" {
		return fmt.Errorf("register runtime %q: invalid profile descriptor", kind)
	}
	keys := make(map[string]struct{}, len(descriptor.Fields))
	for _, field := range descriptor.Fields {
		if field.Key == "" || field.Label == "" || field.Kind == "" {
			return fmt.Errorf("register runtime %q: invalid profile field", kind)
		}
		if _, exists := keys[field.Key]; exists {
			return fmt.Errorf("register runtime %q: duplicate profile field %q", kind, field.Key)
		}
		keys[field.Key] = struct{}{}
		if field.OptionKey != "" && field.Key != "options."+field.OptionKey {
			return fmt.Errorf("register runtime %q: option field is not namespaced", kind)
		}
		if field.Kind == ProfileFieldChoice && len(field.Choices) == 0 {
			return fmt.Errorf("register runtime %q: choice field has no choices", kind)
		}
		if field.Pattern != "" {
			if _, err := regexp.Compile(field.Pattern); err != nil {
				return fmt.Errorf("register runtime %q: invalid pattern for field %q: %w", kind, field.Key, err)
			}
		}
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.strategies[kind]; exists {
		return fmt.Errorf("register runtime %q: duplicate", kind)
	}
	registry.strategies[kind] = strategy
	return nil
}

func (registry *Registry) Descriptor(kind RuntimeKind) (ProfileDescriptor, error) {
	strategy, err := registry.Strategy(kind)
	if err != nil {
		return ProfileDescriptor{}, err
	}
	return strategy.ProfileDescriptor(), nil
}

func (registry *Registry) ValidateProfile(profile Profile) error {
	strategy, err := registry.Strategy(profile.Runtime)
	if err != nil {
		return err
	}
	return strategy.ValidateProfile(profile)
}

func (registry *Registry) Probe(ctx context.Context, profile Profile) ProbeResult {
	strategy, err := registry.Strategy(profile.Runtime)
	if err != nil {
		return ProbeResult{Runtime: profile.Runtime, State: ProbeUnavailable, Diagnostic: err.Error()}
	}
	return strategy.Probe(ctx, profile)
}

func (registry *Registry) Strategy(kind RuntimeKind) (Strategy, error) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	strategy, exists := registry.strategies[kind]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrUnknownRuntime, kind)
	}
	return strategy, nil
}

func (registry *Registry) Kinds() []RuntimeKind {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	result := make([]RuntimeKind, 0, len(registry.strategies))
	for kind := range registry.strategies {
		result = append(result, kind)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
