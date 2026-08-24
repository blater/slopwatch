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
	"time"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

const (
	nativeFactVersion     = "2"
	nativeProtocolVersion = "1"
)

type analyzerUnitsRunner func(context.Context, string, analyzerRequest, float64) (map[string]scoreInputs, error)

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
	plan, err := unitplan.PlanWorkspace(analyzer.workspace, unitplan.Options{TypeScriptMode: typeScriptMode})
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
	prepared, err := analyzer.prepareCacheUnits(parent, store, catalog, plan.Units, active, options)
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
	for _, unit := range prepared.units {
		artifact := analysiscache.UnitArtifact{}
		ref := analysiscache.ArtifactRef{}
		loaded := false
		if options.ReadCache {
			var candidate bool
			ref, candidate = generation.Units[unit.key]
			if candidate {
				artifact, loaded = store.LoadUnit(ref, unit.key)
			}
			if !loaded {
				artifact, ref, loaded = store.LoadUnitByKey(unit.key)
			}
		}
		if loaded && artifact.UnitID == unit.plan.ID && artifact.Language == string(unit.plan.Language) {
			inputs, valid := scoreInputsFromArtifact(artifact, unit.owned, len(componentsForLanguage(catalog, string(unit.plan.Language))) > 0)
			if valid {
				unitInputs[unit.plan.ID] = inputs
				unitRefs[unit.key] = ref
				continue
			}
		}
		misses = append(misses, unit)
	}

	if len(misses) > 0 {
		snapshotFiles := snapshotFilesForMisses(misses, prepared.digests)
		snapshotRoot, cleanup, materializeErr := store.MaterializeSnapshot(parent, snapshotFiles)
		if materializeErr != nil {
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

	// Cache failures never prevent a verified report from being returned.
	for _, unit := range prepared.units {
		if _, exists := unitRefs[unit.key]; exists {
			continue
		}
		inputs := unitInputs[unit.plan.ID]
		unitReport, reportErr := scoreInputsReport(catalog, []string{string(unit.plan.Language)}, inputs, nil)
		if reportErr != nil {
			return report.Document{}, true, reportErr
		}
		ref, putErr := store.PutUnit(unit.key, analysiscache.UnitArtifact{
			UnitID: unit.plan.ID, UnitKey: unit.key, Language: string(unit.plan.Language),
			SnapshotKey: unit.key, Report: unitReport,
		})
		if putErr != nil {
			return document, true, nil
		}
		unitRefs[unit.key] = ref
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

func (analyzer *Analyzer) prepareCacheUnits(ctx context.Context, store *analysiscache.Store, catalog catalogDocument, all []unitplan.Unit, active []plannedCacheUnit, options Options) (cachePreparation, error) {
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
	paths := make(map[string]bool)
	for id := range relevant {
		unit := byID[id]
		for _, path := range unitInputPaths(unit) {
			paths[path] = true
		}
	}
	digests := make(map[string]analysiscache.Digest, len(paths))
	sortedPaths := mapKeys(paths)
	digests, err := analyzer.hashWorkspacePaths(ctx, sortedPaths, store)
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
	result := cachePreparation{digests: digests, backendDigests: analyzerDigests, units: make([]plannedCacheUnit, len(active))}
	for index, unit := range active {
		dependencies := make([]analysiscache.DependencyFingerprint, 0, len(unit.plan.DirectDependencies))
		for _, dependency := range unit.plan.DirectDependencies {
			dependencies = append(dependencies, analysiscache.DependencyFingerprint{
				UnitID: dependency, Fingerprint: dependencyClosureFingerprint(dependency, byID, localKeys),
			})
		}
		key, keyErr := analysiscache.UnitKey(analyzer.unitKeyInput(unit.plan, dependencies, digests, analyzerDigests[string(unit.plan.Language)], catalogDigest, catalog, options))
		if keyErr != nil {
			return cachePreparation{}, keyErr
		}
		unit.key = key
		closure := dependencyUnitClosure(unit.plan.ID, byID)
		snapshotPaths := make(map[string]bool)
		analysisPaths := make(map[string]bool)
		for _, id := range closure {
			dependencyUnit := byID[id]
			for _, path := range unitInputPaths(dependencyUnit) {
				snapshotPaths[path] = true
			}
			for _, path := range append(append([]string{}, dependencyUnit.Sources...), dependencyUnit.ContextSources...) {
				analysisPaths[path] = true
			}
		}
		unit.snapshotPaths = mapKeys(snapshotPaths)
		unit.analysisPaths = mapKeys(analysisPaths)
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

func dependencyUnitClosure(root string, units map[string]unitplan.Unit) []string {
	seen := make(map[string]bool)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		unit, exists := units[id]
		if !exists {
			return
		}
		seen[id] = true
		for _, dependency := range unit.DirectDependencies {
			visit(dependency)
		}
	}
	visit(root)
	return mapKeys(seen)
}

func dependencyClosureFingerprint(root string, units map[string]unitplan.Unit, localKeys map[string]analysiscache.Key) analysiscache.Key {
	closure := dependencyUnitClosure(root, units)
	parts := make([]string, 0, len(closure))
	for _, id := range closure {
		parts = append(parts, id+"="+string(localKeys[id]))
	}
	return analysiscache.Key(analysiscache.DigestBytes([]byte(strings.Join(parts, "\n"))))
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
				Limits:     map[string]int{"max_seconds": int(options.Timeout)},
			}
			timeout := time.Duration(options.Timeout * float64(time.Second))
			if timeout <= 0 {
				timeout = 120 * time.Second
			}
			runContext, stop := context.WithTimeout(ctx, timeout)
			defer stop()
			runner := analyzer.runUnits
			if runner == nil {
				runner = runAnalyzerUnits
			}
			inputs, err := runner(runContext, analyzerExecutable(analyzer.root, language), request, options.Timeout)
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
			partitioned := make(map[string]scoreInputs, len(units))
			for _, unit := range units {
				filtered := partitionCombinedInputs(combined, unit)
				if !hasOwnedCoverage(filtered, unit.owned) {
					results <- languageResult{err: fmt.Errorf("%s analyzer omitted owned paths for unit %s", language, unit.plan.ID)}
					cancel()
					return
				}
				partitioned[unit.plan.ID] = filtered
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

func partitionCombinedInputs(combined scoreInputs, unit plannedCacheUnit) scoreInputs {
	result := filterScoreInputs(combined, pathSet(unit.owned))
	result.diagnostics = result.diagnostics[:0]
	for _, diagnostic := range combined.diagnostics {
		path, hasPath := metadataPath(diagnostic)
		if hasPath && !pathSet(unit.owned)[path] {
			continue
		}
		result.diagnostics = append(result.diagnostics, normalizeMetadata(diagnostic, unit.plan.ID))
	}
	result.plans = result.plans[:0]
	for _, plan := range combined.plans {
		normalized := normalizeMetadata(plan, unit.plan.ID)
		normalized["discovered_source_count"] = len(unit.owned)
		normalized["parsed_source_count"] = len(unit.owned)
		result.plans = append(result.plans, normalized)
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
	actual, err := analyzer.hashWorkspacePaths(ctx, paths, nil)
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

func (analyzer *Analyzer) hashWorkspacePaths(ctx context.Context, paths []string, store *analysiscache.Store) (map[string]analysiscache.Digest, error) {
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
				if store != nil {
					digest, err = store.PutSource(contents)
					if err != nil {
						results[index].err = err
						continue
					}
				}
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
