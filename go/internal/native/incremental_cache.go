package native

import (
	"context"
	"errors"
	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourcefs"
	"github.com/blater/slopwatch/internal/unitplan"
)

func prepareIndexedCache(analyzer *Analyzer, ctx context.Context, view unitplan.Lookup, active []plannedCacheUnit, catalog catalogDocument, options Options) (cachePreparation, error) {
	plans := indexedRequestClosure(view, active, options)
	relevant := map[string]bool{}
	for id := range plans {
		relevant[id] = true
	}
	paths := inputPathsForUnits(plans, relevant)
	digests := map[string]analysiscache.Digest{}
	stamps := map[string]analysiscache.FileStamp{}
	for path := range paths {
		if err := ctx.Err(); err != nil {
			return cachePreparation{}, err
		}
		if data, ok := options.configuration.Bytes()[path]; ok {
			digests[path] = analysiscache.DigestBytes(data)
			continue
		}
		source, ok := view.Source(path)
		if !ok {
			return cachePreparation{}, &workspaceChangedPaths{Paths: []string{path}}
		}
		if err := verifySourceStamp(sourcefs.Default(analyzer.fileSystem), analyzer.workspace, source); err != nil {
			return cachePreparation{}, err
		}
		digests[path] = source.Digest
		stamps[path] = source.Stamp
	}
	local, err := localUnitKeys(plans, relevant, digests, catalog, options)
	if err != nil {
		return cachePreparation{}, err
	}
	fingerprints := dependencyGraphFingerprints(plans, local)
	conservative := conservativeLanguageFingerprints(plans, local)
	prepared := cachePreparation{plans: plans, digests: digests, stamps: stamps}
	for _, unit := range active {
		dependencies := dependencyFingerprintsFor(unit.plan, fingerprints, conservative)
		unit.key, err = analysiscache.UnitKey(unitKeyInput(unit.plan, dependencies, digests, catalog, options))
		if err != nil {
			return cachePreparation{}, err
		}
		prepared.units = append(prepared.units, unit)
	}
	return prepared, nil
}
func runIndexedChanges(analyzer *Analyzer, ctx context.Context, view unitplan.Lookup, active []plannedCacheUnit, options Options) (report.Document, error) {
	if analyzer.snapshotFileSystem != nil {
		ctx = analysiscache.WithSnapshotFileSystem(ctx, analyzer.snapshotFileSystem)
	}
	catalog := activeCatalog(analyzer.catalog, options)
	prepared, err := prepareIndexedCache(analyzer, ctx, view, active, catalog, options)
	if err != nil {
		return report.Document{}, err
	}
	discovered, selected := selectedDiscovery(active, options)
	options.Targets = mapKeys(discoveredPathSet(discovered, selected))
	store := cacheStore(analyzer.engine())
	state := persistentCacheState{analyzer: analyzer.engine(), catalog: catalog, store: store, prepared: prepared, unitInputs: map[string]scoreInputs{}, unitRefs: map[analysiscache.Key]analysiscache.ArtifactRef{}}
	if store != nil {
		state.view, err = viewKey(analyzer.engine(), options)
		if err != nil {
			return report.Document{}, err
		}
		if options.ReadCache {
			state.generation, _ = store.LoadGeneration(state.view)
		}
	}
	loads := loadCachedUnits(ctx, store, state.generation, prepared.units, store != nil && options.ReadCache)
	for i, unit := range prepared.units {
		if inputs, ref, valid := cachedUnitInputs(loads[i], unit, catalog); valid {
			state.unitInputs[unit.plan.ID] = inputs
			state.unitRefs[unit.key] = ref
		} else {
			state.misses = append(state.misses, unit)
		}
	}
	if len(state.misses) > 0 {
		state.misses = prepareMissPaths(state.misses, prepared.plans)
		root, cleanup, err := store.MaterializeWorkspaceSnapshot(ctx, analyzer.workspace, snapshotFilesForMisses(state.misses, prepared.digests), options.configuration.Bytes())
		if err != nil {
			if errors.Is(err, analysiscache.ErrWorkspaceSnapshotChanged) {
				var mismatch *analysiscache.WorkspaceSnapshotChangedError
				if errors.As(err, &mismatch) {
					return report.Document{}, &workspaceChangedPaths{Paths: mismatch.Paths}
				}
				return report.Document{}, ErrWorkspaceChanged
			}
			return report.Document{}, err
		}
		fresh, runErr := runMissingUnits(analyzer.engine(), ctx, root, catalog, state.misses, options)
		cleanupErr := cleanup()
		if runErr != nil {
			return report.Document{}, runErr
		}
		if cleanupErr != nil {
			return report.Document{}, cleanupErr
		}
		for id, inputs := range fresh {
			state.unitInputs[id] = inputs
		}
	}
	// Hits and misses verify exactly the same committed/candidate input records.
	for path := range prepared.stamps {
		source, ok := view.Source(path)
		if !ok {
			return report.Document{}, &workspaceChangedPaths{Paths: []string{path}}
		}
		if err := verifySourceStamp(sourcefs.Default(analyzer.fileSystem), analyzer.workspace, source); err != nil {
			return report.Document{}, err
		}
	}
	document, _, err := assemblePersistentReport(catalog, discovered, selected, options, &state)
	if err != nil {
		return report.Document{}, err
	}
	document.Diagnostics = append(document.Diagnostics, options.ignoreMatcher.Diagnostics()...)
	if store != nil {
		document, _, err = persistPersistentReport(ctx, document, discovered, selected, &state)
	}
	return document, err
}
