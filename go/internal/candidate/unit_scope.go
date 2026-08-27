package candidate

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/unitplan"
)

// UnitScopePlanner derives supporting test files from the same deterministic
// analysis-unit planner used by native analysis.
type UnitScopePlanner struct{}

func (UnitScopePlanner) Plan(ctx context.Context, workspace fix.WorkspaceIdentity, targets []fix.RepoPath, scope string) ([]fix.RepoPath, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allowed := make(map[fix.RepoPath]bool, len(targets))
	for _, target := range targets {
		allowed[target] = true
	}
	if scope == "targets-only" {
		return sortedPaths(allowed), nil
	}
	if scope == "repository" {
		return nil, nil
	}
	if scope != "targets-and-tests" {
		return nil, errors.New("unsupported candidate scope")
	}
	relRoot, err := filepath.Rel(workspace.RepositoryRoot, workspace.AnalysisRoot)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return nil, errors.New("analysis root is outside repository")
	}
	prefix := ""
	if relRoot != "." {
		prefix = filepath.ToSlash(relRoot) + "/"
	}
	analysisTargets := make([]string, len(targets))
	targetSet := make(map[string]bool, len(targets))
	for i, target := range targets {
		value := target.String()
		if prefix != "" {
			if !strings.HasPrefix(value, prefix) {
				return nil, errors.New("target is outside analysis root")
			}
			value = strings.TrimPrefix(value, prefix)
		}
		analysisTargets[i] = value
		targetSet[value] = true
	}
	plan, err := unitplan.PlanWorkspace(workspace.AnalysisRoot, unitplan.Options{Targets: analysisTargets})
	if err != nil {
		return nil, err
	}
	targetUnits := map[string]bool{}
	for _, unit := range plan.Units {
		for _, source := range unit.Sources {
			if targetSet[source] {
				targetUnits[unit.ID] = true
				break
			}
		}
	}
	for _, unit := range plan.Units {
		// Some planners (notably TypeScript syntax/type projects) model tests in
		// the same analysis unit rather than a distinct CapabilityTests unit.
		// Admit only conventionally identified test sources from that target unit;
		// never widen to every source in a project-sized unit.
		if targetUnits[unit.ID] {
			for _, source := range unit.Sources {
				if !isTestSource(source) {
					continue
				}
				path, parseErr := fix.ParseRepoPath(prefix + source)
				if parseErr != nil {
					return nil, parseErr
				}
				allowed[path] = true
			}
		}
		if !hasCapability(unit.Capabilities, unitplan.CapabilityTests) || (!targetUnits[unit.ID] && !dependsOnAny(unit.DirectDependencies, targetUnits)) {
			continue
		}
		for _, source := range unit.Sources {
			path, parseErr := fix.ParseRepoPath(prefix + source)
			if parseErr != nil {
				return nil, parseErr
			}
			allowed[path] = true
		}
	}
	return sortedPaths(allowed), nil
}

func isTestSource(path string) bool {
	value := strings.ToLower(filepath.ToSlash(path))
	base := filepath.Base(value)
	for _, marker := range []string{"/test/", "/tests/", "/__tests__/", "/src/test/"} {
		if strings.Contains("/"+value, marker) {
			return true
		}
	}
	for _, suffix := range []string{"_test.go", "_test.rs", "test.java", "tests.java", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx", ".test.js", ".test.jsx", ".spec.js", ".spec.jsx"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func hasCapability(values []unitplan.Capability, wanted unitplan.Capability) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
func dependsOnAny(values []string, wanted map[string]bool) bool {
	for _, value := range values {
		if wanted[value] {
			return true
		}
	}
	return false
}
func sortedPaths(values map[fix.RepoPath]bool) []fix.RepoPath {
	result := make([]fix.RepoPath, 0, len(values))
	for path := range values {
		result = append(result, path)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
