package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

const (
	nativeFactVersion     = "2"
	nativeProtocolVersion = "1"
)

type analyzerUnitsRunner func(context.Context, string, analyzerRequest) (map[string]scoreInputs, error)

type plannedCacheUnit struct {
	plan          unitplan.Unit
	owned         []string
	key           analysiscache.Key
	snapshotPaths []string
	analysisPaths []string
}

type cachePreparation struct {
	units          []plannedCacheUnit
	digests        map[string]analysiscache.Digest
	backendDigests map[string]analysiscache.Digest
	plans          map[string]unitplan.Unit
}

var errWorkspaceChanged = errors.New("workspace changed while analysis snapshot was running")

func (analyzer *Analyzer) analyzeWithPersistentCache(parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (report.Document, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		document, handled, err := analyzer.analyzeWithPersistentCacheOnce(parent, catalog, discovered, selected, options)
		if errors.Is(err, errWorkspaceChanged) && attempt == 0 {
			continue
		}
		return document, handled, err
	}
	return report.Document{}, true, errWorkspaceChanged
}

func (analyzer *Analyzer) analyzeWithPersistentCacheOnce(parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (report.Document, bool, error) {
	store := analyzer.cacheStore()
	if store == nil {
		return report.Document{}, false, nil
	}
	typeScriptMode := unitplan.TypeScriptSyntax
	if options.TypeScriptTypes {
		typeScriptMode = unitplan.TypeScriptTyped
	}
	plan, err := unitplan.PlanWorkspace(analyzer.workspace, unitplan.Options{
		TypeScriptMode: typeScriptMode,
		Targets:        options.Targets,
	})
	if err != nil {
		if parent.Err() != nil {
			return report.Document{}, true, parent.Err()
		}
		return report.Document{}, false, nil
	}
	active := filterPlannedUnits(plan.Units, discovered, selected, options.IncludeTests)
	if len(active) == 0 {
		return report.Document{}, false, nil
	}
	prepared, err := analyzer.prepareCacheUnits(parent, catalog, plan.Units, active, options)
	if err != nil {
		if parent.Err() != nil {
			return report.Document{}, true, parent.Err()
		}
		return report.Document{}, false, nil
	}

	view, err := analyzer.viewKey(options)
	if err != nil {
		return report.Document{}, false, nil
	}
	var generation analysiscache.Generation
	if options.ReadCache {
		generation, _ = store.LoadGeneration(view)
	}
	unitInputs := make(map[string]scoreInputs, len(prepared.units))
	unitRefs := make(map[analysiscache.Key]analysiscache.ArtifactRef, len(prepared.units))
	misses := make([]plannedCacheUnit, 0, len(prepared.units))
	loads := loadCachedUnits(parent, store, generation, prepared.units, options.ReadCache)
	for index, unit := range prepared.units {
		load := loads[index]
		if load.loaded && load.artifact.UnitID == unit.plan.ID && load.artifact.Language == string(unit.plan.Language) {
			inputs, valid := scoreInputsFromArtifact(load.artifact, unit.owned, len(componentsForLanguage(catalog, string(unit.plan.Language))) > 0)
			if valid {
				unitInputs[unit.plan.ID] = inputs
				unitRefs[unit.key] = load.ref
				continue
			}
		}
		misses = append(misses, unit)
	}

	if len(misses) > 0 {
		misses = prepareMissPaths(misses, prepared.plans)
		snapshotFiles := snapshotFilesForMisses(misses, prepared.digests)
		snapshotRoot, cleanup, materializeErr := store.MaterializeWorkspaceSnapshot(parent, analyzer.workspace, snapshotFiles)
		if materializeErr != nil {
			if errors.Is(materializeErr, analysiscache.ErrWorkspaceSnapshotChanged) {
				return report.Document{}, true, errWorkspaceChanged
			}
			if parent.Err() != nil {
				return report.Document{}, true, parent.Err()
			}
			return report.Document{}, false, nil
		}
		fresh, runErr := analyzer.runMissingUnits(parent, snapshotRoot, catalog, misses, options)
		cleanupErr := cleanup()
		if runErr != nil {
			return report.Document{}, true, runErr
		}
		if cleanupErr != nil {
			return report.Document{}, true, cleanupErr
		}
		for id, inputs := range fresh {
			unitInputs[id] = inputs
		}
	}

	unchanged, verifyErr := analyzer.verifyWorkspaceInputs(parent, prepared.digests)
	if verifyErr != nil {
		if parent.Err() != nil {
			return report.Document{}, true, parent.Err()
		}
		return report.Document{}, true, verifyErr
	}
	if !unchanged {
		return report.Document{}, true, errWorkspaceChanged
	}
	for language, expected := range prepared.backendDigests {
		actual, digestErr := analyzer.backendDigest(language)
		if digestErr != nil {
			return report.Document{}, true, digestErr
		}
		if actual != expected {
			return report.Document{}, true, errWorkspaceChanged
		}
	}

	merged := newScoreInputs()
	for _, unit := range prepared.units {
		inputs, exists := unitInputs[unit.plan.ID]
		if !exists {
			return report.Document{}, true, fmt.Errorf("cache coordinator has no result for unit %s", unit.plan.ID)
		}
		merged.merge(inputs)
	}
	dedupeScoreMetadata(&merged)
	wanted := discoveredPathSet(discovered, selected)
	merged = filterScoreInputs(merged, wanted)
	document, err := scoreInputsReport(catalog, selected, merged, options.PassScore)
	if err != nil {
		return report.Document{}, true, err
	}
	if err := validateDocumentInventory(document, discovered, selected); err != nil {
		// A planner or stale cache may be conservative, but it may never return a
		// partial report. Fall back to the ordinary discovery-driven analyzer.
		return report.Document{}, false, nil
	}
	document.Summary["discovered_source_count"] = len(wanted)

	// Cache failures never prevent a verified report from being returned.
	newRefs, persistErr := persistMissingUnits(parent, store, catalog, prepared.units, unitInputs, unitRefs)
	if persistErr != nil {
		var reportErr unitReportPersistenceError
		if errors.As(persistErr, &reportErr) {
			return report.Document{}, true, reportErr.err
		}
		return document, true, nil
	}
	for key, ref := range newRefs {
		unitRefs[key] = ref
	}
	projection := analysiscache.ProjectionFromReport(view, document, analysiscache.FreshnessCurrent)
	projectionRef, putErr := store.PutProjection(view, projection)
	if putErr != nil {
		return document, true, nil
	}
	if _, commitErr := store.CommitGeneration(view, analysiscache.Generation{Projection: projectionRef, Units: unitRefs}); commitErr != nil {
		return document, true, nil
	}
	return document, true, nil
}

type unitReportPersistenceError struct{ err error }

func (failure unitReportPersistenceError) Error() string { return failure.err.Error() }
func (failure unitReportPersistenceError) Unwrap() error { return failure.err }

func persistMissingUnits(ctx context.Context, store *analysiscache.Store, catalog catalogDocument, units []plannedCacheUnit, inputs map[string]scoreInputs, existing map[analysiscache.Key]analysiscache.ArtifactRef) (map[analysiscache.Key]analysiscache.ArtifactRef, error) {
	type persistResult struct {
		ref analysiscache.ArtifactRef
		err error
	}
	results := make([]persistResult, len(units))
	jobs := make(chan int)
	workers := min(runtime.GOMAXPROCS(0), 16)
	if workers < 1 {
		workers = 1
	}
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				unit := units[index]
				if _, exists := existing[unit.key]; exists {
					continue
				}
				if contextErr := ctx.Err(); contextErr != nil {
					results[index].err = contextErr
					continue
				}
				unitReport, reportErr := scoreInputsReport(catalog, []string{string(unit.plan.Language)}, inputs[unit.plan.ID], nil)
				if reportErr != nil {
					results[index].err = unitReportPersistenceError{err: reportErr}
					continue
				}
				results[index].ref, results[index].err = store.PutUnit(unit.key, analysiscache.UnitArtifact{
					UnitID: unit.plan.ID, UnitKey: unit.key, Language: string(unit.plan.Language),
					SnapshotKey: unit.key, Report: unitReport,
				})
			}
		}()
	}
	for index := range units {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	refs := make(map[analysiscache.Key]analysiscache.ArtifactRef)
	for index, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		if result.ref.Digest != "" {
			refs[units[index].key] = result.ref
		}
	}
	return refs, nil
}

type cachedUnitLoad struct {
	artifact analysiscache.UnitArtifact
	ref      analysiscache.ArtifactRef
	loaded   bool
}

func loadCachedUnits(ctx context.Context, store *analysiscache.Store, generation analysiscache.Generation, units []plannedCacheUnit, enabled bool) []cachedUnitLoad {
	loads := make([]cachedUnitLoad, len(units))
	if !enabled || len(units) == 0 {
		return loads
	}
	workers := min(runtime.GOMAXPROCS(0), 16)
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				unit := units[index]
				ref, candidate := generation.Units[unit.key]
				artifact := analysiscache.UnitArtifact{}
				loaded := false
				if candidate {
					artifact, loaded = store.LoadUnit(ref, unit.key)
				}
				if !loaded {
					artifact, ref, loaded = store.LoadUnitByKey(unit.key)
				}
				loads[index] = cachedUnitLoad{artifact: artifact, ref: ref, loaded: loaded}
			}
		}()
	}
	for index := range units {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return loads
}

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
	owners := make(map[string]int)
	for index := range candidates {
		for _, path := range candidates[index].owned {
			current, exists := owners[path]
			if !exists || preferPathOwner(candidates[index].plan, candidates[current].plan) {
				owners[path] = index
			}
		}
	}
	result := make([]plannedCacheUnit, 0, len(candidates))
	for index, candidate := range candidates {
		owned := candidate.owned[:0]
		for _, path := range candidate.owned {
			if owners[path] == index {
				owned = append(owned, path)
			}
		}
		if len(owned) == 0 || len(intersectPaths(owned, wanted)) == 0 {
			continue
		}
		candidate.owned = append([]string(nil), owned...)
		result = append(result, candidate)
	}
	return result
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

func containsTestPath(paths []string, language string) bool {
	for _, path := range paths {
		lower := strings.ToLower(path)
		switch language {
		case "go":
			if strings.HasSuffix(lower, "_test.go") {
				return true
			}
		case "rust":
			if strings.Contains(lower, "/tests/") || strings.Contains(lower, "/benches/") || strings.HasPrefix(lower, "tests/") || strings.HasPrefix(lower, "benches/") {
				return true
			}
		case "java":
			if strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") {
				return true
			}
		case "typescript":
			if typescriptTest(filepath.Base(lower)) {
				return true
			}
		}
	}
	return false
}

func withoutTestPaths(paths []string, language string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if !containsTestPath([]string{path}, language) {
			result = append(result, path)
		}
	}
	return result
}

func discoveredPathSet(discovered map[string][]string, selected []string) map[string]bool {
	result := make(map[string]bool)
	for _, language := range selected {
		for _, path := range discovered[language] {
			result[path] = true
		}
	}
	return result
}

func intersectPaths(paths []string, wanted map[string]bool) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if wanted[path] {
			result = append(result, path)
		}
	}
	return result
}

func (analyzer *Analyzer) prepareCacheUnits(ctx context.Context, catalog catalogDocument, all []unitplan.Unit, active []plannedCacheUnit, options Options) (cachePreparation, error) {
	byID := make(map[string]unitplan.Unit, len(all))
	for _, unit := range all {
		byID[unit.ID] = unit
	}
	// Active units carry scope-independent canonical ownership with test paths
	// removed when tests are disabled. Use that effective plan consistently for
	// hashing, keying, and snapshot construction.
	for _, unit := range active {
		byID[unit.plan.ID] = unit.plan
	}
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
	conservativeLanguages := map[string]bool{}
	for _, unit := range active {
		if unit.plan.Conservative {
			conservativeLanguages[string(unit.plan.Language)] = true
		}
	}
	if len(conservativeLanguages) > 0 {
		for id, unit := range byID {
			language := string(unit.Language)
			if !conservativeLanguages[language] {
				continue
			}
			if !options.IncludeTests {
				if hasCapability(unit, unitplan.CapabilityTests) {
					delete(byID, id)
					continue
				}
				unit.Sources = withoutTestPaths(unit.Sources, language)
				unit.ContextSources = withoutTestPaths(unit.ContextSources, language)
				if len(unit.Sources) == 0 {
					delete(byID, id)
					continue
				}
				byID[id] = unit
			}
			includeDependencies(id)
		}
	}
	paths := make(map[string]bool)
	for id := range relevant {
		unit := byID[id]
		for _, path := range unitInputPaths(unit) {
			paths[path] = true
		}
	}
	digests := make(map[string]analysiscache.Digest, len(paths))
	sortedPaths := mapKeys(paths)
	// Hashing and persistence are deliberately separate. Cache lookup needs
	// content identities, not 25,000 durable source-blob writes. Miss snapshots
	// are copied directly from digest-verified workspace inputs below.
	digests, err := analyzer.hashWorkspacePaths(ctx, sortedPaths)
	if err != nil {
		return cachePreparation{}, err
	}
	catalogDigest, err := analysisCatalogDigest(catalog)
	if err != nil {
		return cachePreparation{}, err
	}
	analyzerDigests := make(map[string]analysiscache.Digest)
	for id := range relevant {
		language := string(byID[id].Language)
		if analyzerDigests[language] == "" {
			digest, digestErr := analyzer.backendDigest(language)
			if digestErr != nil {
				return cachePreparation{}, digestErr
			}
			analyzerDigests[language] = digest
		}
	}
	localKeys := make(map[string]analysiscache.Key, len(relevant))
	for id := range relevant {
		unit := byID[id]
		key, keyErr := analysiscache.UnitKey(analyzer.unitKeyInput(unit, nil, digests, analyzerDigests[string(unit.Language)], catalogDigest, catalog, options))
		if keyErr != nil {
			return cachePreparation{}, keyErr
		}
		localKeys[id] = key
	}
	dependencyFingerprints := dependencyGraphFingerprints(byID, localKeys)
	conservativeFingerprints := conservativeLanguageFingerprints(byID, localKeys)
	result := cachePreparation{digests: digests, backendDigests: analyzerDigests, plans: byID, units: make([]plannedCacheUnit, len(active))}
	for index, unit := range active {
		dependencies := make([]analysiscache.DependencyFingerprint, 0, len(unit.plan.DirectDependencies))
		for _, dependency := range unit.plan.DirectDependencies {
			dependencies = append(dependencies, analysiscache.DependencyFingerprint{
				UnitID: dependency, Fingerprint: dependencyFingerprints[dependency],
			})
		}
		if unit.plan.Conservative {
			language := string(unit.plan.Language)
			dependencies = append(dependencies, analysiscache.DependencyFingerprint{
				UnitID: language + ":conservative-workspace", Fingerprint: conservativeFingerprints[language],
			})
		}
		key, keyErr := analysiscache.UnitKey(analyzer.unitKeyInput(unit.plan, dependencies, digests, analyzerDigests[string(unit.plan.Language)], catalogDigest, catalog, options))
		if keyErr != nil {
			return cachePreparation{}, keyErr
		}
		unit.key = key
		result.units[index] = unit
	}
	sort.Slice(result.units, func(i, j int) bool { return result.units[i].plan.ID < result.units[j].plan.ID })
	return result, nil
}

func analysisCatalogDigest(catalog catalogDocument) (analysiscache.Digest, error) {
	type componentIdentity struct {
		ID               string            `json:"id"`
		Version          string            `json:"version"`
		Axis             string            `json:"axis"`
		Kind             string            `json:"kind"`
		Aggregator       string            `json:"aggregator"`
		DeduplicationKey []string          `json:"deduplication_key"`
		Support          map[string]string `json:"support"`
		Enabled          bool              `json:"enabled"`
	}
	identity := struct {
		Languages  []string             `json:"languages"`
		Analyzers  []analyzerDescriptor `json:"analyzers"`
		Components []componentIdentity  `json:"components"`
	}{Languages: catalog.Languages, Analyzers: catalog.Analyzers}
	for _, component := range catalog.Components {
		identity.Components = append(identity.Components, componentIdentity{
			ID: component.ID, Version: component.Version, Axis: component.Axis,
			Kind: component.Kind, Aggregator: component.Aggregator,
			DeduplicationKey: component.DeduplicationKey, Support: component.Support,
			Enabled: component.Defaults.Enabled,
		})
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return analysiscache.DigestBytes(encoded), nil
}

func (analyzer *Analyzer) unitKeyInput(unit unitplan.Unit, dependencies []analysiscache.DependencyFingerprint, digests map[string]analysiscache.Digest, analyzerDigest, catalogDigest analysiscache.Digest, catalog catalogDocument, options Options) analysiscache.UnitKeyInput {
	sources := fingerprints(append(append([]string{}, unit.Sources...), unit.ContextSources...), digests)
	configuration := fingerprints(unit.ConfigInputs, digests)
	typeMode := "off"
	if options.TypeScriptTypes {
		typeMode = "auto"
	}
	components := componentsForLanguage(catalog, string(unit.Language))
	definitions := make([]analysiscache.ComponentDefinition, len(components))
	for index, component := range components {
		definitions[index] = analysiscache.ComponentDefinition{ID: component.ID, Version: component.Version}
	}
	return analysiscache.UnitKeyInput{
		UnitID: unit.ID, Language: string(unit.Language), Sources: sources,
		Configuration: configuration, Dependencies: dependencies,
		AnalyzerDigest: analyzerDigest, FactVersion: nativeFactVersion,
		ProtocolVersion: nativeProtocolVersion, CatalogVersion: string(catalogDigest),
		Components: definitions, ParserMode: string(unit.Mode), TypeAnalysisMode: typeMode,
		IncludeTests: unitOutputIncludesTests(unit),
		Toolchain:    map[string]string{"go_runtime": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH},
	}
}

func unitOutputIncludesTests(unit unitplan.Unit) bool {
	return hasCapability(unit, unitplan.CapabilityTests) || containsTestPath(unit.Sources, string(unit.Language))
}

func fingerprints(paths []string, digests map[string]analysiscache.Digest) []analysiscache.InputFingerprint {
	seen := make(map[string]bool, len(paths))
	result := make([]analysiscache.InputFingerprint, 0, len(paths))
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			result = append(result, analysiscache.InputFingerprint{Path: path, ContentHash: digests[path]})
		}
	}
	return result
}

func componentsForLanguage(catalog catalogDocument, language string) []requestedComponent {
	result := []requestedComponent{}
	for _, descriptor := range catalog.Components {
		if descriptor.Defaults.Enabled && descriptor.supported(language) {
			result = append(result, requestedComponent{ID: descriptor.ID, Version: descriptor.Version})
		}
	}
	return result
}

func unitInputPaths(unit unitplan.Unit) []string {
	seen := make(map[string]bool)
	for _, paths := range [][]string{unit.Sources, unit.ContextSources, unit.ConfigInputs} {
		for _, path := range paths {
			seen[path] = true
		}
	}
	return mapKeys(seen)
}

// dependencyGraphFingerprints computes one compact Merkle identity per unit.
// A unit is visited once; callers do not repeatedly materialize its complete
// transitive closure. Cycles are collapsed first so malformed or conservative
// project graphs remain deterministic and every member invalidates together.
func dependencyGraphFingerprints(units map[string]unitplan.Unit, localKeys map[string]analysiscache.Key) map[string]analysiscache.Key {
	ids := mapKeys(localKeys)
	index := 0
	indices := make(map[string]int, len(ids))
	lowlinks := make(map[string]int, len(ids))
	onStack := make(map[string]bool, len(ids))
	stack := make([]string, 0, len(ids))
	components := make([][]string, 0, len(ids))
	var connect func(string)
	connect = func(id string) {
		index++
		indices[id] = index
		lowlinks[id] = index
		stack = append(stack, id)
		onStack[id] = true
		for _, dependency := range units[id].DirectDependencies {
			if localKeys[dependency] == "" {
				continue
			}
			if indices[dependency] == 0 {
				connect(dependency)
				lowlinks[id] = min(lowlinks[id], lowlinks[dependency])
			} else if onStack[dependency] {
				lowlinks[id] = min(lowlinks[id], indices[dependency])
			}
		}
		if lowlinks[id] != indices[id] {
			return
		}
		component := []string{}
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == id {
				break
			}
		}
		sort.Strings(component)
		components = append(components, component)
	}
	for _, id := range ids {
		if indices[id] == 0 {
			connect(id)
		}
	}
	componentOf := make(map[string]int, len(ids))
	for component, members := range components {
		for _, id := range members {
			componentOf[id] = component
		}
	}
	componentKeys := make(map[int]analysiscache.Key, len(components))
	var fingerprintComponent func(int) analysiscache.Key
	fingerprintComponent = func(component int) analysiscache.Key {
		if key := componentKeys[component]; key != "" {
			return key
		}
		parts := []string{"dependency-component-v1"}
		dependencies := map[int]bool{}
		for _, id := range components[component] {
			parts = append(parts, "unit:"+id+"="+string(localKeys[id]))
			for _, dependency := range units[id].DirectDependencies {
				dependencyComponent, exists := componentOf[dependency]
				if exists && dependencyComponent != component {
					dependencies[dependencyComponent] = true
				}
			}
		}
		dependencyComponents := make([]int, 0, len(dependencies))
		for dependency := range dependencies {
			dependencyComponents = append(dependencyComponents, dependency)
		}
		sort.Slice(dependencyComponents, func(i, j int) bool {
			return components[dependencyComponents[i]][0] < components[dependencyComponents[j]][0]
		})
		for _, dependency := range dependencyComponents {
			parts = append(parts, "dependency:"+components[dependency][0]+"="+string(fingerprintComponent(dependency)))
		}
		key := analysiscache.Key(analysiscache.DigestBytes([]byte(strings.Join(parts, "\n"))))
		componentKeys[component] = key
		return key
	}
	result := make(map[string]analysiscache.Key, len(ids))
	for _, id := range ids {
		result[id] = fingerprintComponent(componentOf[id])
	}
	return result
}

func conservativeLanguageFingerprints(units map[string]unitplan.Unit, localKeys map[string]analysiscache.Key) map[string]analysiscache.Key {
	parts := map[string][]string{}
	for id, key := range localKeys {
		language := string(units[id].Language)
		parts[language] = append(parts[language], id+"="+string(key))
	}
	result := make(map[string]analysiscache.Key, len(parts))
	for language, values := range parts {
		sort.Strings(values)
		result[language] = analysiscache.Key(analysiscache.DigestBytes([]byte("conservative-language-v1\n" + strings.Join(values, "\n"))))
	}
	return result
}

// prepareMissPaths walks each language's combined missing-unit closure once.
// runMissingUnits already invokes one analyzer batch per language, so storing
// the same closure on every package only multiplies memory and CPU work.
func prepareMissPaths(misses []plannedCacheUnit, units map[string]unitplan.Unit) []plannedCacheUnit {
	firstByLanguage := map[string]int{}
	rootsByLanguage := map[string][]string{}
	for index := range misses {
		language := string(misses[index].plan.Language)
		if _, exists := firstByLanguage[language]; !exists {
			firstByLanguage[language] = index
		}
		rootsByLanguage[language] = append(rootsByLanguage[language], misses[index].plan.ID)
	}
	for language, roots := range rootsByLanguage {
		broad := false
		for _, id := range roots {
			if units[id].Conservative {
				broad = true
				break
			}
		}
		if broad {
			roots = roots[:0]
			for id, unit := range units {
				if string(unit.Language) == language {
					roots = append(roots, id)
				}
			}
		}
		seen := map[string]bool{}
		stack := append([]string(nil), roots...)
		snapshotPaths := map[string]bool{}
		analysisPaths := map[string]bool{}
		for len(stack) > 0 {
			last := len(stack) - 1
			id := stack[last]
			stack = stack[:last]
			if seen[id] {
				continue
			}
			unit, exists := units[id]
			if !exists || string(unit.Language) != language {
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
		index := firstByLanguage[language]
		misses[index].snapshotPaths = mapKeys(snapshotPaths)
		misses[index].analysisPaths = mapKeys(analysisPaths)
	}
	return misses
}

func mapKeys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (analyzer *Analyzer) backendDigest(language string) (analysiscache.Digest, error) {
	paths := []string{analyzerExecutable(analyzer.root, language)}
	structural := filepath.Join(analyzer.root, "analyzers", "structural")
	switch language {
	case "java":
		paths = append(paths, filepath.Join(structural, "slopslap-structural-java.jar"), filepath.Join(structural, "java-runtime", "bin", "java"))
	case "rust":
		name := "slopslap-structural-rust"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		paths = append(paths, filepath.Join(structural, name))
	}
	type identityFile struct {
		Path   string               `json:"path"`
		Digest analysiscache.Digest `json:"digest"`
	}
	files := make([]identityFile, 0, len(paths))
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		files = append(files, identityFile{Path: filepath.Base(path), Digest: analysiscache.DigestBytes(contents)})
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	return analysiscache.DigestBytes(encoded), nil
}

func analyzerExecutable(root, language string) string {
	if language == "typescript" {
		return filepath.Join(root, "build", "typescript", "slopslap-typescript")
	}
	return filepath.Join(root, "analyzers", "structural", "slopslap-structural")
}

func snapshotFilesForMisses(units []plannedCacheUnit, digests map[string]analysiscache.Digest) []analysiscache.SnapshotFile {
	paths := make(map[string]bool)
	for _, unit := range units {
		for _, path := range unit.snapshotPaths {
			paths[path] = true
		}
	}
	result := make([]analysiscache.SnapshotFile, 0, len(paths))
	for _, path := range mapKeys(paths) {
		result = append(result, analysiscache.SnapshotFile{Path: path, Digest: digests[path]})
	}
	return result
}

func (analyzer *Analyzer) runMissingUnits(parent context.Context, workspace string, catalog catalogDocument, misses []plannedCacheUnit, options Options) (map[string]scoreInputs, error) {
	byLanguage := make(map[string][]plannedCacheUnit)
	result := make(map[string]scoreInputs, len(misses))
	for _, unit := range misses {
		language := string(unit.plan.Language)
		components := componentsForLanguage(catalog, language)
		if len(components) == 0 {
			result[unit.plan.ID] = newScoreInputs()
			continue
		}
		byLanguage[language] = append(byLanguage[language], unit)
	}
	type languageResult struct {
		inputs map[string]scoreInputs
		err    error
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan languageResult, len(byLanguage))
	var workers sync.WaitGroup
	for language, units := range byLanguage {
		language, units := language, units
		workers.Add(1)
		go func() {
			defer workers.Done()
			invocation, err := invocationID()
			if err != nil {
				results <- languageResult{err: err}
				cancel()
				return
			}
			// scoreInputs uses the conventional <language>-unit identifier to
			// infer language for clean files that emit coverage but no metrics.
			combinedID := language + "-unit"
			combinedPaths := make(map[string]bool)
			for _, unit := range units {
				for _, path := range unit.analysisPaths {
					if sourceLanguages[strings.ToLower(filepath.Ext(path))] == language {
						combinedPaths[path] = true
					}
				}
			}
			typeMode := "off"
			if options.TypeScriptTypes {
				typeMode = "auto"
			}
			request := analyzerRequest{
				Type: "request", Version: 1, Invocation: invocation, Workspace: workspace,
				Units:      []protocolUnit{{ID: combinedID, Language: language, Paths: mapKeys(combinedPaths), Metadata: map[string]any{"batched_units": len(units)}}},
				Components: componentsForLanguage(catalog, language),
				Options:    map[string]any{"include_tests": options.IncludeTests, "typescript_types": typeMode},
				Limits:     map[string]int{},
			}
			runner := analyzer.runUnits
			if runner == nil {
				runner = runAnalyzerUnits
			}
			inputs, err := runner(ctx, analyzerExecutable(analyzer.root, language), request)
			if err != nil {
				results <- languageResult{err: fmt.Errorf("%s analyzer failed: %w", language, err)}
				cancel()
				return
			}
			combined, exists := inputs[combinedID]
			if !exists {
				results <- languageResult{err: fmt.Errorf("%s analyzer omitted combined batch %s", language, combinedID)}
				cancel()
				return
			}
			partitioned := partitionCombinedUnitInputs(combined, units)
			for _, unit := range units {
				if !hasOwnedCoverage(partitioned[unit.plan.ID], unit.owned) {
					results <- languageResult{err: fmt.Errorf("%s analyzer omitted owned paths for unit %s", language, unit.plan.ID)}
					cancel()
					return
				}
			}
			results <- languageResult{inputs: partitioned}
		}()
	}
	workers.Wait()
	close(results)
	for item := range results {
		if item.err != nil {
			return nil, item.err
		}
		for id, inputs := range item.inputs {
			result[id] = inputs
		}
	}
	return result, nil
}

func partitionCombinedUnitInputs(combined scoreInputs, units []plannedCacheUnit) map[string]scoreInputs {
	result := make(map[string]scoreInputs, len(units))
	owners := make(map[string]string)
	for _, unit := range units {
		result[unit.plan.ID] = newScoreInputs()
		for _, path := range unit.owned {
			owners[path] = unit.plan.ID
		}
	}
	for path, observations := range combined.observations {
		if owner := owners[path]; owner != "" {
			inputs := result[owner]
			inputs.observations[path] = observations
			result[owner] = inputs
		}
	}
	for path, coverage := range combined.coverage {
		if owner := owners[path]; owner != "" {
			inputs := result[owner]
			inputs.coverage[path] = coverage
			result[owner] = inputs
		}
	}
	for path, language := range combined.languages {
		if owner := owners[path]; owner != "" {
			inputs := result[owner]
			inputs.languages[path] = language
			result[owner] = inputs
		}
	}
	for _, diagnostic := range combined.diagnostics {
		path, hasPath := metadataPath(diagnostic)
		if hasPath {
			owner := owners[path]
			if owner == "" {
				continue
			}
			inputs := result[owner]
			inputs.diagnostics = append(inputs.diagnostics, normalizeMetadata(diagnostic, owner))
			result[owner] = inputs
			continue
		}
		for _, unit := range units {
			inputs := result[unit.plan.ID]
			inputs.diagnostics = append(inputs.diagnostics, normalizeMetadata(diagnostic, unit.plan.ID))
			result[unit.plan.ID] = inputs
		}
	}
	for _, unit := range units {
		inputs := result[unit.plan.ID]
		for _, plan := range combined.plans {
			normalized := normalizeMetadata(plan, unit.plan.ID)
			normalized["discovered_source_count"] = len(unit.owned)
			normalized["parsed_source_count"] = len(unit.owned)
			inputs.plans = append(inputs.plans, normalized)
		}
		result[unit.plan.ID] = inputs
	}
	return result
}

func metadataPath(metadata map[string]any) (string, bool) {
	value := metadata["path"]
	switch value := value.(type) {
	case string:
		return value, value != ""
	case *string:
		return dereferenceString(value), value != nil && *value != ""
	default:
		return "", false
	}
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizeMetadata(metadata map[string]any, unitID string) map[string]any {
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if key != "invocation_id" {
			result[key] = value
		}
	}
	result["unit_id"] = unitID
	return result
}

func dedupeScoreMetadata(inputs *scoreInputs) {
	inputs.diagnostics = dedupeMetadata(inputs.diagnostics, true)
	inputs.plans = dedupeMetadata(inputs.plans, false)
}

func dedupeMetadata(values []map[string]any, ignoreUnit bool) []map[string]any {
	seen := make(map[string]bool, len(values))
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		normalized := make(map[string]any, len(value))
		for key, item := range value {
			if key == "invocation_id" || (ignoreUnit && key == "unit_id") {
				continue
			}
			normalized[key] = item
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			continue
		}
		identity := string(encoded)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		clean := make(map[string]any, len(value))
		for key, item := range value {
			if key != "invocation_id" {
				clean[key] = item
			}
		}
		result = append(result, clean)
	}
	return result
}

func (analyzer *Analyzer) verifyWorkspaceInputs(ctx context.Context, expected map[string]analysiscache.Digest) (bool, error) {
	paths := mapKeys(expected)
	actual, err := analyzer.hashWorkspacePaths(ctx, paths)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, path := range paths {
		if actual[path] != expected[path] {
			return false, nil
		}
	}
	return true, nil
}

func (analyzer *Analyzer) hashWorkspacePaths(ctx context.Context, paths []string) (map[string]analysiscache.Digest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		digest analysiscache.Digest
		err    error
	}
	results := make([]result, len(paths))
	workers := min(runtime.GOMAXPROCS(0), 8)
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				if err := ctx.Err(); err != nil {
					results[index].err = err
					continue
				}
				path := paths[index]
				contents, err := os.ReadFile(filepath.Join(analyzer.workspace, filepath.FromSlash(path)))
				if err != nil {
					results[index].err = err
					continue
				}
				digest := analysiscache.DigestBytes(contents)
				results[index].digest = digest
			}
		}()
	}
	for index := range paths {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	digests := make(map[string]analysiscache.Digest, len(paths))
	for index, item := range results {
		if item.err != nil {
			return nil, fmt.Errorf("hash workspace input %s: %w", paths[index], item.err)
		}
		digests[paths[index]] = item.digest
	}
	return digests, nil
}

func scoreInputsFromArtifact(artifact analysiscache.UnitArtifact, owned []string, requireCoverage bool) (scoreInputs, bool) {
	inputs := newScoreInputs()
	inputs.diagnostics = append(inputs.diagnostics, artifact.Report.Diagnostics...)
	inputs.plans = append(inputs.plans, artifact.Report.ExecutionPlans...)
	for _, file := range artifact.Report.Files {
		inputs.languages[file.Path] = file.Language
		inputs.coverage[file.Path] = cloneCoverage(file.Coverage)
		for componentID, component := range file.Components {
			for _, evidence := range component.Evidence {
				path := evidence.Location.Path
				if path == "" {
					path = file.Path
				}
				inputs.observations[path] = ensureObservationComponents(inputs.observations[path])
				inputs.observations[path][componentID] = append(inputs.observations[path][componentID], observation{
					component: componentID, path: path, language: file.Language,
					scope: evidence.Scope, value: evidence.Value,
					subject: protocolSubject{
						Name: evidence.Name, Symbol: evidence.Symbol, Routine: evidence.Routine,
						Start: protocolPosition{Line: evidence.Location.Start.Line, Column: evidence.Location.Start.Column, Offset: evidence.Location.Start.Offset},
						End:   protocolPosition{Line: evidence.Location.End.Line, Column: evidence.Location.End.Column, Offset: evidence.Location.End.Offset},
					},
					attributes: evidence.Attributes, provenance: evidence.Provenance,
				})
			}
		}
	}
	inputs = filterScoreInputs(inputs, pathSet(owned))
	return inputs, !requireCoverage || hasOwnedCoverage(inputs, owned)
}

func ensureObservationComponents(value map[string][]observation) map[string][]observation {
	if value == nil {
		return map[string][]observation{}
	}
	return value
}

func filterScoreInputs(inputs scoreInputs, allowed map[string]bool) scoreInputs {
	result := newScoreInputs()
	for path, components := range inputs.observations {
		if allowed[path] {
			result.observations[path] = components
		}
	}
	for path, coverage := range inputs.coverage {
		if allowed[path] {
			result.coverage[path] = coverage
		}
	}
	for path, language := range inputs.languages {
		if allowed[path] {
			result.languages[path] = language
		}
	}
	result.diagnostics = append(result.diagnostics, inputs.diagnostics...)
	result.plans = append(result.plans, inputs.plans...)
	return result
}

func hasOwnedCoverage(inputs scoreInputs, owned []string) bool {
	for _, path := range owned {
		if _, exists := inputs.coverage[path]; !exists {
			return false
		}
	}
	return true
}

func pathSet(paths []string) map[string]bool {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		result[path] = true
	}
	return result
}

func cloneCoverage(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
