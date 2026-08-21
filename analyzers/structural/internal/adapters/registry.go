// Package adapters defines the structural host's language-adapter boundary.
package adapters

import (
	"fmt"
	"sort"

	"slopslap.dev/structural/internal/facts"
)

// Adapter translates exact language source files into normalized facts.
type Adapter interface {
	Language() string
	FactSchemaVersion() int
	ParserModes() []string
	Analyze(workspace string, paths []string, options map[string]any) (*facts.Program, error)
}

// Registry selects statically linked or externally bridged language adapters.
type Registry struct {
	byLanguage map[string]Adapter
}

// NewRegistry creates a registry and rejects duplicate language claims.
func NewRegistry(items ...Adapter) (*Registry, error) {
	registry := &Registry{byLanguage: make(map[string]Adapter, len(items))}
	for _, item := range items {
		language := item.Language()
		if language == "" {
			return nil, fmt.Errorf("adapter language is empty")
		}
		if _, exists := registry.byLanguage[language]; exists {
			return nil, fmt.Errorf("duplicate adapter for %s", language)
		}
		if item.FactSchemaVersion() != facts.SchemaVersion {
			return nil, fmt.Errorf(
				"adapter %s uses fact schema %d; host requires %d",
				language, item.FactSchemaVersion(), facts.SchemaVersion,
			)
		}
		registry.byLanguage[language] = item
	}
	return registry, nil
}

// Adapter returns the exact registered adapter for a language.
func (r *Registry) Adapter(language string) (Adapter, error) {
	item, exists := r.byLanguage[language]
	if !exists {
		return nil, fmt.Errorf("no structural adapter for %s", language)
	}
	return item, nil
}

// Languages returns stable registered language names.
func (r *Registry) Languages() []string {
	result := make([]string, 0, len(r.byLanguage))
	for language := range r.byLanguage {
		result = append(result, language)
	}
	sort.Strings(result)
	return result
}
