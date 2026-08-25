package native

import "github.com/blater/slopwatch/internal/unitplan"

// prepareMissPaths walks each language's combined missing-unit closure once.
// runMissingUnits already invokes one analyzer batch per language, so storing
// the same closure on every package only multiplies memory and CPU work.
func prepareMissPaths(misses []plannedCacheUnit, units map[string]unitplan.Unit) []plannedCacheUnit {
	firstByLanguage, rootsByLanguage := missRoots(misses)
	for language, roots := range rootsByLanguage {
		roots = broadenRoots(language, roots, units)
		snapshotPaths, analysisPaths := missClosurePaths(language, roots, units)
		index := firstByLanguage[language]
		misses[index].snapshotPaths = mapKeys(snapshotPaths)
		misses[index].analysisPaths = mapKeys(analysisPaths)
	}
	return misses
}

func missRoots(misses []plannedCacheUnit) (map[string]int, map[string][]string) {
	firstByLanguage := map[string]int{}
	rootsByLanguage := map[string][]string{}
	for index := range misses {
		language := string(misses[index].plan.Language)
		if _, exists := firstByLanguage[language]; !exists {
			firstByLanguage[language] = index
		}
		rootsByLanguage[language] = append(rootsByLanguage[language], misses[index].plan.ID)
	}
	return firstByLanguage, rootsByLanguage
}

func broadenRoots(language string, roots []string, units map[string]unitplan.Unit) []string {
	for _, id := range roots {
		if units[id].Conservative {
			return languageRoots(language, units)
		}
	}
	return roots
}

func languageRoots(language string, units map[string]unitplan.Unit) []string {
	roots := []string{}
	for id, unit := range units {
		if string(unit.Language) == language {
			roots = append(roots, id)
		}
	}
	return roots
}

func missClosurePaths(language string, roots []string, units map[string]unitplan.Unit) (map[string]bool, map[string]bool) {
	seen := map[string]bool{}
	stack := append([]string(nil), roots...)
	snapshotPaths := map[string]bool{}
	analysisPaths := map[string]bool{}
	for len(stack) > 0 {
		last := len(stack) - 1
		id := stack[last]
		stack = stack[:last]
		unit, exists := units[id]
		if seen[id] || !exists || string(unit.Language) != language {
			continue
		}
		seen[id] = true
		for _, path := range unitInputPaths(unit) {
			snapshotPaths[path] = true
		}
		for _, path := range append(append([]string{}, unit.Sources...), unit.ContextSources...) {
			analysisPaths[path] = true
		}
		stack = append(stack, unit.DirectDependencies...)
	}
	return snapshotPaths, analysisPaths
}
