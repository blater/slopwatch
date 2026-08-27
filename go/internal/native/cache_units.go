package native

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopmochi/internal/unitplan"
)

func filterPlannedUnits(units []unitplan.Unit, discovered map[string][]string, selected []string, includeTests bool) []plannedCacheUnit {
	wanted := discoveredPathSet(discovered, selected)
	selectedLanguages := make(map[string]bool, len(selected))
	for _, language := range selected {
		selectedLanguages[language] = true
	}
	candidates := make([]plannedCacheUnit, 0)
	for _, unit := range units {
		language := string(unit.Language)
		if !selectedLanguages[language] {
			continue
		}
		if !includeTests && hasCapability(unit, unitplan.CapabilityTests) {
			continue
		}
		effective := unit
		if !includeTests {
			effective.Sources = withoutTestPaths(effective.Sources, language)
			effective.ContextSources = withoutTestPaths(effective.ContextSources, language)
		}
		if len(effective.Sources) == 0 {
			continue
		}
		candidates = append(candidates, plannedCacheUnit{plan: effective, owned: append([]string(nil), effective.Sources...)})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].plan.ID < candidates[j].plan.ID })
	owners := pathOwners(candidates)
	result := make([]plannedCacheUnit, 0, len(candidates))
	for index, candidate := range candidates {
		owned := ownedPaths(candidate.owned, owners, index)
		if len(owned) == 0 || len(intersectPaths(owned, wanted)) == 0 {
			continue
		}
		candidate.owned = owned
		result = append(result, candidate)
	}
	return result
}

func pathOwners(candidates []plannedCacheUnit) map[string]int {
	owners := make(map[string]int)
	for index := range candidates {
		for _, path := range candidates[index].owned {
			current, exists := owners[path]
			if !exists || preferPathOwner(candidates[index].plan, candidates[current].plan) {
				owners[path] = index
			}
		}
	}
	return owners
}

func ownedPaths(paths []string, owners map[string]int, owner int) []string {
	owned := make([]string, 0, len(paths))
	for _, path := range paths {
		if owners[path] == owner {
			owned = append(owned, path)
		}
	}
	return owned
}

func preferPathOwner(candidate, current unitplan.Unit) bool {
	if candidate.Language == unitplan.LanguageTypeScript && current.Language == unitplan.LanguageTypeScript {
		candidateDepth := typeScriptUnitDepth(candidate)
		currentDepth := typeScriptUnitDepth(current)
		if candidateDepth != currentDepth {
			return candidateDepth > currentDepth
		}
	}
	return candidate.ID < current.ID
}

func typeScriptUnitDepth(unit unitplan.Unit) int {
	const prefix = "typescript:typed:"
	if !strings.HasPrefix(unit.ID, prefix) {
		return 0
	}
	directory := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(unit.ID, prefix)))
	if directory == "." {
		return 1
	}
	return strings.Count(directory, "/") + 2
}

func hasCapability(unit unitplan.Unit, capability unitplan.Capability) bool {
	for _, item := range unit.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}
