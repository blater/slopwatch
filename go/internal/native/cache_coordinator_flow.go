package native

import (
	"context"
	"errors"
	"fmt"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

type persistentCacheState struct {
	analyzer   *analysisEngine
	catalog    catalogDocument
	store      *analysiscache.Store
	view       analysiscache.ViewKey
	prepared   cachePreparation
	generation analysiscache.Generation
	unitInputs map[string]scoreInputs
	unitRefs   map[analysiscache.Key]analysiscache.ArtifactRef
	misses     []plannedCacheUnit
}

func runPersistentCache(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (report.Document, bool, error) {
	state, usable, err := preparePersistentCache(analyzer, parent, catalog, discovered, selected, options)
	if !usable || err != nil {
		return report.Document{}, usable, err
	}
	usable, err = loadPersistentCache(parent, catalog, options, &state)
	if !usable || err != nil {
		return report.Document{}, usable, err
	}
	unchanged, err := verifyPersistentCache(analyzer, parent, &state)
	if err != nil || !unchanged {
		return report.Document{}, true, err
	}
	document, usable, err := assemblePersistentReport(catalog, discovered, selected, options, &state)
	if !usable || err != nil {
		return report.Document{}, usable, err
	}
	return persistPersistentReport(parent, document, discovered, selected, &state)
}

func preparePersistentCache(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (persistentCacheState, bool, error) {
	store := cacheStore(analyzer)
	if store == nil {
		return persistentCacheState{}, false, nil
	}
	typeScriptMode := unitplan.TypeScriptSyntax
	if options.TypeScriptTypes {
		typeScriptMode = unitplan.TypeScriptTyped
	}
	plan, err := unitplan.PlanWorkspace(analyzer.workspace, unitplan.Options{TypeScriptMode: typeScriptMode, Targets: options.Targets})
	if err != nil {
		return persistentFailure(parent)
	}
	active := filterPlannedUnits(plan.Units, discovered, selected, options.IncludeTests)
	if len(active) == 0 {
		return persistentCacheState{}, false, nil
	}
	prepared, err := prepareCacheUnits(analyzer, parent, catalog, plan.Units, active, options)
	if err != nil {
		return persistentFailure(parent)
	}
	view, err := viewKey(analyzer, options)
	if err != nil {
		return persistentCacheState{}, false, nil
	}
	state := persistentCacheState{
		analyzer: analyzer, catalog: catalog, store: store, view: view, prepared: prepared,
		unitInputs: make(map[string]scoreInputs, len(prepared.units)),
		unitRefs:   make(map[analysiscache.Key]analysiscache.ArtifactRef, len(prepared.units)),
	}
	if options.ReadCache {
		state.generation, _ = store.LoadGeneration(view)
	}
	return state, true, nil
}

func persistentFailure(parent context.Context) (persistentCacheState, bool, error) {
	if err := parent.Err(); err != nil {
		return persistentCacheState{}, true, err
	}
	return persistentCacheState{}, false, nil
}

func loadPersistentCache(parent context.Context, catalog catalogDocument, options Options, state *persistentCacheState) (bool, error) {
	loads := loadCachedUnits(parent, state.store, state.generation, state.prepared.units, options.ReadCache)
	state.misses = make([]plannedCacheUnit, 0, len(state.prepared.units))
	for index, unit := range state.prepared.units {
		if inputs, ref, valid := cachedUnitInputs(loads[index], unit, catalog); valid {
			state.unitInputs[unit.plan.ID] = inputs
			state.unitRefs[unit.key] = ref
			continue
		}
		state.misses = append(state.misses, unit)
	}
	if len(state.misses) == 0 {
		return true, nil
	}
	return runPersistentMisses(parent, catalog, options, state)
}

func cachedUnitInputs(load cachedUnitLoad, unit plannedCacheUnit, catalog catalogDocument) (scoreInputs, analysiscache.ArtifactRef, bool) {
	if !load.loaded || load.artifact.UnitID != unit.plan.ID || load.artifact.Language != string(unit.plan.Language) {
		return scoreInputs{}, analysiscache.ArtifactRef{}, false
	}
	inputs, valid := scoreInputsFromArtifact(load.artifact, unit.owned, len(componentsForLanguage(catalog, string(unit.plan.Language))) > 0)
	return inputs, load.ref, valid
}

func runPersistentMisses(parent context.Context, catalog catalogDocument, options Options, state *persistentCacheState) (bool, error) {
	state.misses = prepareMissPaths(state.misses, state.prepared.plans)
	snapshotFiles := snapshotFilesForMisses(state.misses, state.prepared.digests)
	snapshotRoot, cleanup, err := state.store.MaterializeWorkspaceSnapshot(parent, state.analyzer.workspace, snapshotFiles)
	if err != nil {
		if errors.Is(err, analysiscache.ErrWorkspaceSnapshotChanged) {
			return true, errWorkspaceChanged
		}
		if parent.Err() != nil {
			return true, parent.Err()
		}
		return false, nil
	}
	fresh, runErr := runMissingUnits(state.analyzer, parent, snapshotRoot, catalog, state.misses, options)
	cleanupErr := cleanup()
	if runErr != nil {
		return true, runErr
	}
	if cleanupErr != nil {
		return true, cleanupErr
	}
	for id, inputs := range fresh {
		state.unitInputs[id] = inputs
	}
	return true, nil
}

func verifyPersistentCache(analyzer *analysisEngine, parent context.Context, state *persistentCacheState) (bool, error) {
	unchanged, err := verifyWorkspaceInputs(analyzer, parent, state.prepared.digests)
	if err != nil {
		if parent.Err() != nil {
			return true, parent.Err()
		}
		return true, err
	}
	if !unchanged {
		return false, errWorkspaceChanged
	}
	for language, expected := range state.prepared.backendDigests {
		actual, digestErr := backendDigest(analyzer, language)
		if digestErr != nil {
			return true, digestErr
		}
		if actual != expected {
			return false, errWorkspaceChanged
		}
	}
	return true, nil
}

func assemblePersistentReport(catalog catalogDocument, discovered map[string][]string, selected []string, options Options, state *persistentCacheState) (report.Document, bool, error) {
	merged := newScoreInputs()
	for _, unit := range state.prepared.units {
		inputs, exists := state.unitInputs[unit.plan.ID]
		if !exists {
			return report.Document{}, true, fmt.Errorf("cache coordinator has no result for unit %s", unit.plan.ID)
		}
		merged.merge(inputs)
	}
	dedupeScoreMetadata(&merged)
	merged = filterScoreInputs(merged, discoveredPathSet(discovered, selected))
	document, err := scoreInputsReport(catalog, selected, merged, options.PassScore)
	if err != nil {
		return report.Document{}, true, err
	}
	if err := validateDocumentInventory(document, discovered, selected); err != nil {
		return report.Document{}, false, nil
	}
	document.Summary["discovered_source_count"] = len(discoveredPathSet(discovered, selected))
	return document, true, nil
}

func persistPersistentReport(parent context.Context, document report.Document, discovered map[string][]string, selected []string, state *persistentCacheState) (report.Document, bool, error) {
	newRefs, err := persistMissingUnits(parent, state.store, state.catalog, state.prepared.units, state.unitInputs, state.unitRefs)
	if err != nil {
		var reportErr unitReportPersistenceError
		if errors.As(err, &reportErr) {
			return report.Document{}, true, reportErr.err
		}
		return document, true, nil
	}
	for key, ref := range newRefs {
		state.unitRefs[key] = ref
	}
	projection := analysiscache.ProjectionFromReport(state.view, document, analysiscache.FreshnessCurrent)
	projectionRef, err := state.store.PutProjection(state.view, projection)
	if err != nil {
		return document, true, nil
	}
	if _, err := state.store.CommitGeneration(state.view, analysiscache.Generation{Projection: projectionRef, Units: state.unitRefs}); err != nil {
		return document, true, nil
	}
	return document, true, nil
}
