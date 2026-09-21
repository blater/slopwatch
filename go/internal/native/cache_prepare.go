package native

import (
	"context"
	"sort"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/unitplan"
)

func (analyzer *Analyzer) prepareCacheUnits(ctx context.Context, catalog catalogDocument, all []unitplan.Unit, active []plannedCacheUnit, options Options) (cachePreparation, error) {
	return prepareCacheUnits(analyzer.engine(), ctx, catalog, all, active, options)
}

func prepareCacheUnits(analyzer *analysisEngine, ctx context.Context, catalog catalogDocument, all []unitplan.Unit, active []plannedCacheUnit, options Options) (cachePreparation, error) {
	byID := unitsByID(all)
	// Active units carry scope-independent canonical ownership with test paths
	// removed when tests are disabled. Use that effective plan consistently for
	// hashing, keying, and snapshot construction.
	for _, unit := range active {
		byID[unit.plan.ID] = unit.plan
	}
	relevant := relevantUnitIDs(byID, active, options.IncludeTests)
	paths := inputPathsForUnits(byID, relevant)
	var previous analysiscache.Generation
	if store := cacheStore(analyzer); store != nil && options.ReadCache {
		if view, err := viewKey(analyzer, options); err == nil {
			previous, _ = store.LoadGeneration(view)
		}
	}
	digests, stamps, err := cachedWorkspaceDigests(analyzer, ctx, paths, previous, options.configuration.Bytes())
	if err != nil {
		return cachePreparation{}, err
	}
	localKeys, err := localUnitKeys(byID, relevant, digests, catalog, options)
	if err != nil {
		return cachePreparation{}, err
	}
	dependencyFingerprints := dependencyGraphFingerprints(byID, localKeys)
	conservativeFingerprints := conservativeLanguageFingerprints(byID, localKeys)
	result := cachePreparation{stamps: stamps, digests: digests, plans: byID, units: make([]plannedCacheUnit, len(active))}
	for index, unit := range active {
		dependencies := dependencyFingerprintsFor(unit.plan, dependencyFingerprints, conservativeFingerprints)
		key, keyErr := analysiscache.UnitKey(unitKeyInput(unit.plan, dependencies, digests, catalog, options))
		if keyErr != nil {
			return cachePreparation{}, keyErr
		}
		unit.key = key
		result.units[index] = unit
	}
	sort.Slice(result.units, func(i, j int) bool { return result.units[i].plan.ID < result.units[j].plan.ID })
	return result, nil
}

func unitsByID(units []unitplan.Unit) map[string]unitplan.Unit {
	byID := make(map[string]unitplan.Unit, len(units))
	for _, unit := range units {
		byID[unit.ID] = unit
	}
	return byID
}

func relevantUnitIDs(byID map[string]unitplan.Unit, active []plannedCacheUnit, includeTests bool) map[string]bool {
	relevant := make(map[string]bool)
	var includeDependencies func(string)
	includeDependencies = func(id string) {
		if relevant[id] {
			return
		}
		relevant[id] = true
		for _, dependency := range byID[id].DirectDependencies {
			includeDependencies(dependency)
		}
	}
	for _, unit := range active {
		includeDependencies(unit.plan.ID)
	}
	conservativeLanguages := conservativeLanguages(active)
	for id, unit := range byID {
		language := string(unit.Language)
		if !conservativeLanguages[language] {
			continue
		}
		if !includeTests && hasCapability(unit, unitplan.CapabilityTests) {
			delete(byID, id)
			continue
		}
		if !includeTests {
			unit.Sources = withoutTestPaths(unit.Sources, language)
			unit.ContextSources = withoutTestPaths(unit.ContextSources, language)
			if len(unit.Sources) == 0 && len(unit.ContextSources) == 0 {
				delete(byID, id)
				continue
			}
			byID[id] = unit
		}
		includeDependencies(id)
	}
	return relevant
}

func conservativeLanguages(active []plannedCacheUnit) map[string]bool {
	result := map[string]bool{}
	for _, unit := range active {
		if unit.plan.Conservative {
			result[string(unit.plan.Language)] = true
		}
	}
	return result
}

func inputPathsForUnits(units map[string]unitplan.Unit, relevant map[string]bool) map[string]bool {
	paths := make(map[string]bool)
	for id := range relevant {
		for _, path := range unitInputPaths(units[id]) {
			paths[path] = true
		}
	}
	return paths
}

func localUnitKeys(units map[string]unitplan.Unit, relevant map[string]bool, digests map[string]analysiscache.Digest, catalog catalogDocument, options Options) (map[string]analysiscache.Key, error) {
	keys := make(map[string]analysiscache.Key, len(relevant))
	for id := range relevant {
		unit := units[id]
		key, err := analysiscache.UnitKey(unitKeyInput(unit, nil, digests, catalog, options))
		if err != nil {
			return nil, err
		}
		keys[id] = key
	}
	return keys, nil
}

func dependencyFingerprintsFor(unit unitplan.Unit, fingerprints, conservative map[string]analysiscache.Key) []analysiscache.DependencyFingerprint {
	dependencies := make([]analysiscache.DependencyFingerprint, 0, len(unit.DirectDependencies)+1)
	for _, dependency := range unit.DirectDependencies {
		dependencies = append(dependencies, analysiscache.DependencyFingerprint{UnitID: dependency, Fingerprint: fingerprints[dependency]})
	}
	if unit.Conservative {
		language := string(unit.Language)
		dependencies = append(dependencies, analysiscache.DependencyFingerprint{UnitID: language + ":conservative-workspace", Fingerprint: conservative[language]})
	}
	return dependencies
}
