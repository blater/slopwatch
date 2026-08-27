// Package unitplan discovers conservative, cache-safe analysis units for the
// languages supported by slopmochi.
package unitplan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Language identifies the source adapter which owns a unit.
type Language string

const (
	LanguageGo         Language = "go"
	LanguageJava       Language = "java"
	LanguageRust       Language = "rust"
	LanguageTypeScript Language = "typescript"
)

// Mode describes the amount of project context required by an analysis unit.
type Mode string

const (
	ModeSyntax  Mode = "syntax"
	ModeProject Mode = "project"
	ModeTyped   Mode = "typed"
)

// Capability is a stable description of analysis a unit can support.
type Capability string

const (
	CapabilitySyntax       Capability = "syntax"
	CapabilityTypes        Capability = "types"
	CapabilityDependencies Capability = "dependencies"
	CapabilityTests        Capability = "tests"
)

// TypeScriptMode controls TypeScript unit granularity. Auto and Typed create
// typed project units where a tsconfig is present and syntax units elsewhere.
type TypeScriptMode string

const (
	TypeScriptAuto   TypeScriptMode = ""
	TypeScriptSyntax TypeScriptMode = "syntax"
	TypeScriptTyped  TypeScriptMode = "typed"
)

// Options controls planning choices which affect analyzer correctness.
type Options struct {
	TypeScriptMode TypeScriptMode
	// Targets are the user-selected paths relative to root. Explicit symlink
	// targets are part of the authorized inventory even though ordinary tree
	// traversal does not follow nested symlinks.
	Targets []string
}

// Unit is one independently cacheable analyzer invocation. All paths are
// clean, slash-separated, workspace-relative paths. Lists are sorted and
// duplicate-free.
type Unit struct {
	ID           string
	Language     Language
	Mode         Mode
	Capabilities []Capability
	Sources      []string
	// ContextSources are source files owned by direct dependencies. The cache
	// coordinator walks DirectDependencies when it needs a complete transitive
	// analysis snapshot, avoiding a quadratic copy of every dependency closure
	// into every unit.
	ContextSources      []string
	ConfigInputs        []string
	DirectDependencies  []string
	ReverseDependencies []string
	Conservative        bool
}

// Diagnostic records metadata that forced the planner to broaden a unit.
type Diagnostic struct {
	Path    string
	Message string
}

// Plan is a deterministic workspace analysis plan.
type Plan struct {
	Units       []Unit
	Diagnostics []Diagnostic
}

// PlanWorkspace discovers all supported analysis units beneath root. Parse
// failures in build metadata broaden the plan and are reported as diagnostics;
// filesystem failures are returned because silently omitting source would
// under-invalidate the cache.
func PlanWorkspace(root string, options Options) (Plan, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Plan{}, fmt.Errorf("canonicalize workspace: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return Plan{}, fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return Plan{}, fmt.Errorf("workspace is not a directory: %s", root)
	}

	files, err := workspaceFiles(absRoot, options.Targets)
	if err != nil {
		return Plan{}, err
	}
	context := plannerContext{root: absRoot, files: files, fileSet: make(map[string]bool, len(files))}
	for _, path := range files {
		context.fileSet[path] = true
	}

	var plan Plan
	for _, planner := range []func(plannerContext, Options) ([]Unit, []Diagnostic){
		planGo, planJava, planRust, planTypeScript,
	} {
		units, diagnostics := planner(context, options)
		plan.Units = append(plan.Units, units...)
		plan.Diagnostics = append(plan.Diagnostics, diagnostics...)
	}
	finalize(&plan)
	return plan, nil
}

type plannerContext struct {
	root    string
	files   []string
	fileSet map[string]bool
}

var ignoredDirectories = map[string]bool{
	".git": true, ".gradle": true, ".idea": true, ".mypy_cache": true,
	".pytest_cache": true, ".ruff_cache": true, "build": true,
	"coverage": true, "dist": true, "node_modules": true, "out": true,
	"target": true, "vendor": true,
}

func workspaceFiles(root string, targets []string) ([]string, error) {
	var files []string
	err := walkWorkspaceTree(root, func(path string) error {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, cleanPath(relative))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan workspace: %w", err)
	}
	for _, target := range targets {
		targetFiles, err := explicitSymlinkTargetFiles(root, target)
		if err != nil {
			return nil, err
		}
		files = append(files, targetFiles...)
	}
	return uniqueStrings(files), nil
}

func walkWorkspaceTree(start string, addFile func(string) error) error {
	return filepath.WalkDir(start, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != start && ignoredDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		return addFile(path)
	})
}

func explicitSymlinkTargetFiles(root, target string) ([]string, error) {
	logical := target
	if !filepath.IsAbs(logical) {
		logical = filepath.Join(root, filepath.FromSlash(target))
	}
	metadata, err := os.Lstat(logical)
	if err != nil || metadata.Mode()&os.ModeSymlink == 0 {
		return nil, nil
	}
	resolved, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return nil, fmt.Errorf("resolve explicit target %s: %w", target, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		relative, relativeErr := filepath.Rel(root, logical)
		if relativeErr != nil {
			return nil, nil
		}
		return []string{cleanPath(relative)}, nil
	}
	var files []string
	err = walkWorkspaceTree(resolved, func(path string) error {
		physicalRelative, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		workspaceRelative, err := filepath.Rel(root, filepath.Join(logical, physicalRelative))
		if err != nil {
			return err
		}
		files = append(files, cleanPath(workspaceRelative))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan explicit target %s: %w", target, err)
	}
	return files, nil
}

func finalize(plan *Plan) {
	byID := make(map[string]*Unit, len(plan.Units))
	for index := range plan.Units {
		byID[plan.Units[index].ID] = &plan.Units[index]
	}
	for index := range plan.Units {
		unit := &plan.Units[index]
		unit.Sources = uniqueStrings(unit.Sources)
		unit.ConfigInputs = uniqueStrings(unit.ConfigInputs)
		unit.DirectDependencies = knownDependencies(uniqueStrings(unit.DirectDependencies), unit.ID, byID)
		unit.Capabilities = uniqueCapabilities(unit.Capabilities)
	}
	populateContextSources(plan.Units, byID)
	for index := range plan.Units {
		unit := &plan.Units[index]
		for _, dependency := range unit.DirectDependencies {
			if dependencyUnit := byID[dependency]; dependencyUnit != nil && dependency != unit.ID {
				dependencyUnit.ReverseDependencies = append(dependencyUnit.ReverseDependencies, unit.ID)
			}
		}
	}
	for index := range plan.Units {
		plan.Units[index].ReverseDependencies = uniqueStrings(plan.Units[index].ReverseDependencies)
	}
	sort.Slice(plan.Units, func(i, j int) bool { return plan.Units[i].ID < plan.Units[j].ID })
	sort.Slice(plan.Diagnostics, func(i, j int) bool {
		if plan.Diagnostics[i].Path != plan.Diagnostics[j].Path {
			return plan.Diagnostics[i].Path < plan.Diagnostics[j].Path
		}
		return plan.Diagnostics[i].Message < plan.Diagnostics[j].Message
	})
}

func (context plannerContext) read(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(context.root, filepath.FromSlash(path)))
}

func cleanPath(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "" {
		return "."
	}
	return clean
}

func pathWithin(path, directory string) bool {
	if directory == "." {
		return true
	}
	return path == directory || strings.HasPrefix(path, directory+"/")
}

func nearestAncestor(path string, candidates map[string]bool) (string, bool) {
	directory := path
	for {
		if candidates[directory] {
			return directory, true
		}
		if directory == "." {
			return "", false
		}
		directory = cleanPath(filepath.Dir(directory))
	}
}

func relativeIDPath(path string) string {
	if path == "." {
		return "root"
	}
	return path
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = cleanPath(value)
		if value == "." || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func uniqueCapabilities(values []Capability) []Capability {
	seen := make(map[Capability]bool, len(values))
	result := make([]Capability, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func withoutString(values []string, omitted string) []string {
	result := values[:0]
	for _, value := range values {
		if value != omitted {
			result = append(result, value)
		}
	}
	return result
}

func knownDependencies(values []string, owner string, units map[string]*Unit) []string {
	result := values[:0]
	for _, value := range values {
		if value != owner && units[value] != nil {
			result = append(result, value)
		}
	}
	return result
}

func populateContextSources(units []Unit, byID map[string]*Unit) {
	for index := range units {
		unit := &units[index]
		for _, id := range unit.DirectDependencies {
			dependency := byID[id]
			if dependency == nil {
				continue
			}
			unit.ContextSources = append(unit.ContextSources, dependency.Sources...)
		}
		owned := make(map[string]bool, len(unit.Sources))
		for _, source := range unit.Sources {
			owned[source] = true
		}
		context := uniqueStrings(unit.ContextSources)
		unit.ContextSources = context[:0]
		for _, source := range context {
			if !owned[source] {
				unit.ContextSources = append(unit.ContextSources, source)
			}
		}
	}
}
