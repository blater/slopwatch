package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopmochi/internal/analysiscache"
	"github.com/blater/slopmochi/internal/report"
	"github.com/blater/slopmochi/internal/unitplan"
)

func TestStartupProjectionIsReconciledWithCurrentSourceInventory(t *testing.T) {
	analyzer := startupProjectionFixture(t)
	document, ok := analyzer.CachedProjection()
	if !ok {
		t.Fatal("current inventory rejected a readable startup projection")
	}
	assertStartupProjection(t, document)
}

func startupProjectionFixture(t *testing.T) *Analyzer {
	t.Helper()
	workspace := t.TempDir()
	writeTestFile(t, workspace, "kept.java", "class Kept {}\n")
	writeTestFile(t, workspace, "new.java", "class New {}\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"java"}, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	view, err := analyzer.viewKey(options)
	if err != nil {
		t.Fatal(err)
	}
	stale := report.Document{Files: []report.File{
		{Path: "kept.java", Language: "java", Complete: true, Score: 12},
		{Path: "deleted.java", Language: "java", Complete: true, Score: 34},
	}}
	projection := analysiscache.ProjectionFromReport(view, stale, analysiscache.FreshnessCurrent)
	ref, err := store.PutProjection(view, projection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitGeneration(view, analysiscache.Generation{Projection: ref}); err != nil {
		t.Fatal(err)
	}

	return analyzer
}

func assertStartupProjection(t *testing.T, document report.Document) {
	t.Helper()
	if len(document.Files) != 2 {
		t.Fatalf("startup files = %d, want current inventory of 2", len(document.Files))
	}
	byPath := map[string]report.File{}
	for _, file := range document.Files {
		byPath[file.Path] = file
	}
	if byPath["kept.java"].Score != 12 || !byPath["kept.java"].Complete {
		t.Fatalf("cached row was not preserved: %#v", byPath["kept.java"])
	}
	if file, exists := byPath["new.java"]; !exists || file.Complete || file.Freshness != report.FreshnessProvisional {
		t.Fatalf("new source was not added provisionally: %#v", file)
	}
	if _, exists := byPath["deleted.java"]; exists {
		t.Fatal("deleted source remained in startup projection")
	}
}

func TestPersistentUnitCacheHitAndContentInvalidation(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 1\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		calls++
		return fakeBatchInputs(t, request), nil
	}

	first := analyzeTestDocument(t, analyzer)
	if calls != 1 || len(first.Files) != 1 {
		t.Fatalf("first analysis calls/files = %d/%d", calls, len(first.Files))
	}
	_ = analyzeTestDocument(t, analyzer)
	hit := analyzeTestDocument(t, analyzer)
	if calls != 1 {
		t.Fatalf("ReadCache=true did not reuse verified unit; calls = %d", calls)
	}
	if !reflect.DeepEqual(first.Files, hit.Files) {
		t.Fatalf("warm files differ from fresh files:\n%#v\n%#v", first.Files, hit.Files)
	}

	source := filepath.Join(workspace, "pkg", "a.go")
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 2\n")
	if err := os.Chtimes(source, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	_ = analyzeTestDocument(t, analyzer)
	if calls != 2 {
		t.Fatalf("same-size/same-mtime content change was a cache hit; calls = %d", calls)
	}
}

func analyzeTestDocument(t *testing.T, analyzer *Analyzer) report.Document {
	t.Helper()
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestCorruptUnitArtifactIsAMiss(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "a.go", "package sample\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		calls++
		return fakeBatchInputs(t, request), nil
	}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	view, err := analyzer.viewKey(options)
	if err != nil {
		t.Fatal(err)
	}
	generation, ok := store.LoadGeneration(view)
	if !ok || len(generation.Units) != 1 {
		t.Fatalf("generation = %#v, %v", generation, ok)
	}
	for _, ref := range generation.Units {
		value := string(ref.Digest)
		path := filepath.Join(store.Root(), "artifacts", value[:2], value[2:])
		if err := os.Truncate(path, 7); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("corrupt artifact was not treated as a miss; calls = %d", calls)
	}
}

func TestUnitIndexReusesFullPackageAcrossTargetViews(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 1\n")
	writeTestFile(t, workspace, "pkg/b.go", "package pkg\nvar B = 2\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"pkg/a.go"}, Languages: []string{"go"}, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		calls++
		return fakeBatchInputs(t, request), nil
	}
	first, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != 1 || first.Files[0].Path != "pkg/a.go" {
		t.Fatalf("first target files = %#v", first.Files)
	}
	second, err := analyzer.Analyze(context.Background(), []string{"pkg/b.go"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cross-view package reuse invoked analyzer; calls = %d", calls)
	}
	if len(second.Files) != 1 || second.Files[0].Path != "pkg/b.go" {
		t.Fatalf("second target files = %#v", second.Files)
	}
}

func TestMissingUnitsUseOneImmutableContextCompleteLanguageBatch(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "a/a.go", "package a\nimport _ \"example/b\"\n")
	writeTestFile(t, workspace, "b/b.go", "package b\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"a/a.go"}, Languages: []string{"go"}, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	var gotPaths []string
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		gotPaths = assertImmutableBatch(t, workspace, request)
		return fakeBatchInputs(t, request), nil
	}
	document, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(gotPaths)
	wantPaths := []string{"a/a.go", "b/b.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("combined context paths = %v, want %v", gotPaths, wantPaths)
	}
	if len(document.Files) != 1 || document.Files[0].Path != "a/a.go" {
		t.Fatalf("target filtering leaked context rows: %#v", document.Files)
	}
}

func assertImmutableBatch(t *testing.T, liveWorkspace string, request analyzerRequest) []string {
	t.Helper()
	if len(request.Units) != 1 {
		t.Fatalf("protocol units = %d, want one combined language unit", len(request.Units))
	}
	if request.Units[0].ID != "go-unit" {
		t.Fatalf("combined unit ID = %q, want coverage-compatible go-unit", request.Units[0].ID)
	}
	if request.Workspace == liveWorkspace {
		t.Fatal("analyzer received the live workspace")
	}
	paths := append([]string(nil), request.Units[0].Paths...)
	for _, path := range paths {
		info, err := os.Stat(filepath.Join(request.Workspace, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("snapshot source %s is writable: %o", path, info.Mode().Perm())
		}
	}
	return paths
}

func TestMutationDuringSnapshotRetriesBeforeCommit(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "a.go", "package sample\nvar A = 1\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		calls++
		inputs := fakeBatchInputs(t, request)
		if calls == 1 {
			writeTestFile(t, workspace, "a.go", "package sample\nvar A = 100\n")
		}
		return inputs, nil
	}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("snapshot mutation calls = %d, want one retry", calls)
	}
	view, err := analyzer.viewKey(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadGeneration(view); !ok {
		t.Fatal("verified retry did not commit a generation")
	}
}

func TestTypedNestedOwnershipIsUniqueAndNestedChangeInvalidatesBothFingerprints(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "tsconfig.json", `{ "compilerOptions": { "strict": true } }`)
	writeTestFile(t, workspace, "outer.ts", "export const outer = 1;\n")
	writeTestFile(t, workspace, "nested/tsconfig.json", `{ "compilerOptions": { "strict": true } }`)
	writeTestFile(t, workspace, "nested/inner.ts", "export const inner = 1;\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"typescript"}, TypeScriptTypes: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, typescriptTestCatalog())
	plan, err := unitplan.PlanWorkspace(workspace, unitplan.Options{TypeScriptMode: unitplan.TypeScriptTyped})
	if err != nil {
		t.Fatal(err)
	}
	discovered := map[string][]string{"typescript": {"nested/inner.ts", "outer.ts"}}
	active := filterPlannedUnits(plan.Units, discovered, []string{"typescript"}, false)
	assertTypedOwnership(t, active)
	first, err := analyzer.prepareCacheUnits(context.Background(), analyzer.catalog, plan.Units, active, options)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workspace, "nested/inner.ts", "export const inner = 2;\n")
	second, err := analyzer.prepareCacheUnits(context.Background(), analyzer.catalog, plan.Units, active, options)
	if err != nil {
		t.Fatal(err)
	}
	assertAllUnitKeysChanged(t, unitKeysByID(first.units), unitKeysByID(second.units))
}

func assertTypedOwnership(t *testing.T, active []plannedCacheUnit) {
	t.Helper()
	owners := map[string]string{}
	for _, unit := range active {
		for _, path := range unit.owned {
			if previous := owners[path]; previous != "" {
				t.Fatalf("%s owned by both %s and %s", path, previous, unit.plan.ID)
			}
			owners[path] = unit.plan.ID
		}
	}
	if len(owners) != 2 || !strings.Contains(owners["nested/inner.ts"], "nested/tsconfig.json") || owners["outer.ts"] == "" {
		t.Fatalf("nested ownership = %#v", owners)
	}
}

func assertAllUnitKeysChanged(t *testing.T, firstKeys, secondKeys map[string]analysiscache.Key) {
	t.Helper()
	if len(firstKeys) != 2 {
		t.Fatalf("typed units = %#v", firstKeys)
	}
	for id, key := range firstKeys {
		if secondKeys[id] == key {
			t.Fatalf("nested content change did not invalidate %s", id)
		}
	}
}

func TestOverlappingRustUnitsHaveDisjointOwnershipAndSharedKeyInputs(t *testing.T) {
	units := []unitplan.Unit{
		{ID: "rust:crate", Language: unitplan.LanguageRust, Mode: unitplan.ModeProject, Sources: []string{"src/lib.rs", "src/shared.rs"}},
		{ID: "rust:workspace", Language: unitplan.LanguageRust, Mode: unitplan.ModeProject, Sources: []string{"src/shared.rs", "src/workspace.rs"}},
	}
	discovered := map[string][]string{"rust": {"src/lib.rs", "src/shared.rs", "src/workspace.rs"}}
	active := filterPlannedUnits(units, discovered, []string{"rust"}, true)
	owners := map[string]string{}
	for _, unit := range active {
		for _, path := range unit.owned {
			if previous := owners[path]; previous != "" {
				t.Fatalf("%s owned by both %s and %s", path, previous, unit.plan.ID)
			}
			owners[path] = unit.plan.ID
		}
		if !pathSet(unit.plan.Sources)["src/shared.rs"] {
			t.Fatalf("%s lost shared conservative key input: %v", unit.plan.ID, unit.plan.Sources)
		}
	}
	if len(active) != 2 || len(owners) != 3 || owners["src/shared.rs"] != "rust:crate" {
		t.Fatalf("overlapping Rust ownership = %#v across %#v", owners, active)
	}
}

func unitKeysByID(units []plannedCacheUnit) map[string]analysiscache.Key {
	result := make(map[string]analysiscache.Key, len(units))
	for _, unit := range units {
		result[unit.plan.ID] = unit.key
	}
	return result
}

func newCacheTestAnalyzer(t *testing.T, workspace string, options Options, store *analysiscache.Store, catalog catalogDocument) *Analyzer {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	installation := t.TempDir()
	if options.Languages[0] == "typescript" {
		writeTestFile(t, installation, "build/typescript/slopslap-typescript", "typescript analyzer")
	} else {
		writeTestFile(t, installation, "analyzers/structural/slopslap-structural", "structural analyzer")
	}
	analyzer := &Analyzer{workspace: resolved, root: installation, catalog: catalog, options: options}
	analyzer.SetCacheStore(store)
	return analyzer
}

func goTestCatalog() catalogDocument {
	threshold := "1"
	return catalogDocument{Languages: []string{"go"}, Components: []componentDescriptor{{
		ID: "metric", Version: "v1", Axis: "structural_core", Kind: "continuous", Aggregator: "sum",
		Support:  map[string]string{"go": "supported"},
		Defaults: componentDefaults{Enabled: true, Weight: "1", Formula: "linear-over-threshold", Threshold: &threshold},
	}}}
}

func typescriptTestCatalog() catalogDocument {
	catalog := goTestCatalog()
	catalog.Languages = []string{"typescript"}
	catalog.Components[0].Support = map[string]string{"typescript": "supported"}
	return catalog
}

func fakeBatchInputs(t *testing.T, request analyzerRequest) map[string]scoreInputs {
	t.Helper()
	result := make(map[string]scoreInputs, len(request.Units))
	for _, unit := range request.Units {
		inputs := newScoreInputs()
		for _, path := range unit.Paths {
			contents, err := os.ReadFile(filepath.Join(request.Workspace, filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			inputs.languages[path] = unit.Language
			inputs.coverage[path] = map[string]string{"metric": "complete"}
			inputs.observations[path] = map[string][]observation{"metric": {{
				component: "metric", path: path, language: unit.Language, scope: "file", value: float64(len(contents)),
				subject: protocolSubject{Name: path, Symbol: path, Start: protocolPosition{Line: 1, Column: 1}, End: protocolPosition{Line: 1, Column: 2}},
			}}}
		}
		inputs.plans = []map[string]any{{"type": "execution_plan", "unit_id": unit.ID, "parser_modes": []string{"test"}, "discovered_source_count": len(unit.Paths), "parsed_source_count": len(unit.Paths)}}
		result[unit.ID] = inputs
	}
	return result
}

func writeTestFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCachedAnalyzeIsSafeForConcurrentCallers(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "a.go", "package sample\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	var lock sync.Mutex
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		lock.Lock()
		calls++
		lock.Unlock()
		return fakeBatchInputs(t, request), nil
	}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, analyzeErr := analyzer.Analyze(context.Background(), nil, nil); analyzeErr != nil {
				t.Errorf("Analyze: %v", analyzeErr)
			}
		}()
	}
	workers.Wait()
	lock.Lock()
	defer lock.Unlock()
	if calls != 1 {
		t.Fatalf("concurrent warm readers invoked analyzer; calls = %d", calls)
	}
}

func TestCacheViewDoesNotDependOnReadPolicy(t *testing.T) {
	workspace := t.TempDir()
	first, err := analysiscache.WorkspaceViewKey(workspace, analysiscache.ViewOptions{Targets: []string{"."}, Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := analysiscache.WorkspaceViewKey(workspace, analysiscache.ViewOptions{Targets: []string{"."}, Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("read policy unexpectedly changed the view identity")
	}
}

func TestReadCacheDefaultsFalse(t *testing.T) {
	if (Options{}).ReadCache {
		t.Fatal("ReadCache default is not false")
	}
}

func TestDependencyFingerprintsInvalidateTransitivelyAcrossLanguages(t *testing.T) {
	for _, language := range []unitplan.Language{
		unitplan.LanguageGo, unitplan.LanguageJava, unitplan.LanguageRust, unitplan.LanguageTypeScript,
	} {
		t.Run(string(language), func(t *testing.T) {
			prefix := string(language) + ":"
			units := map[string]unitplan.Unit{
				prefix + "leaf":   {ID: prefix + "leaf", Language: language},
				prefix + "middle": {ID: prefix + "middle", Language: language, DirectDependencies: []string{prefix + "leaf"}},
				prefix + "root":   {ID: prefix + "root", Language: language, DirectDependencies: []string{prefix + "middle"}},
				prefix + "side":   {ID: prefix + "side", Language: language},
			}
			keys := map[string]analysiscache.Key{}
			for id := range units {
				keys[id] = analysiscache.Key(analysiscache.DigestBytes([]byte(id)))
			}
			before := dependencyGraphFingerprints(units, keys)
			keys[prefix+"leaf"] = analysiscache.Key(analysiscache.DigestBytes([]byte("changed")))
			after := dependencyGraphFingerprints(units, keys)
			for _, id := range []string{prefix + "leaf", prefix + "middle", prefix + "root"} {
				if before[id] == after[id] {
					t.Errorf("transitive change did not invalidate %s", id)
				}
			}
			if before[prefix+"side"] != after[prefix+"side"] {
				t.Fatal("transitive change invalidated an unrelated unit")
			}
		})
	}
}

func TestDependencyFingerprintsHandleCyclesDeterministically(t *testing.T) {
	units := map[string]unitplan.Unit{
		"a": {ID: "a", Language: unitplan.LanguageGo, DirectDependencies: []string{"b"}},
		"b": {ID: "b", Language: unitplan.LanguageGo, DirectDependencies: []string{"a"}},
	}
	keys := map[string]analysiscache.Key{
		"a": analysiscache.Key(analysiscache.DigestBytes([]byte("a"))),
		"b": analysiscache.Key(analysiscache.DigestBytes([]byte("b"))),
	}
	first := dependencyGraphFingerprints(units, keys)
	second := dependencyGraphFingerprints(units, keys)
	if !reflect.DeepEqual(first, second) || first["a"] == "" || first["a"] != first["b"] {
		t.Fatalf("cyclic fingerprints are not stable: first=%v second=%v", first, second)
	}
}

func TestPrepareMissPathsWalksOneCombinedClosurePerLanguage(t *testing.T) {
	units := map[string]unitplan.Unit{}
	var misses []plannedCacheUnit
	for _, language := range []unitplan.Language{
		unitplan.LanguageGo, unitplan.LanguageJava, unitplan.LanguageRust, unitplan.LanguageTypeScript,
	} {
		prefix := string(language)
		leafID, rootID := prefix+":leaf", prefix+":root"
		units[leafID] = unitplan.Unit{ID: leafID, Language: language, Sources: []string{prefix + "/leaf.source"}, ConfigInputs: []string{prefix + "/config"}}
		root := unitplan.Unit{ID: rootID, Language: language, Sources: []string{prefix + "/root.source"}, DirectDependencies: []string{leafID}}
		units[rootID] = root
		misses = append(misses, plannedCacheUnit{plan: root})
	}
	prepared := prepareMissPaths(misses, units)
	for _, miss := range prepared {
		language := string(miss.plan.Language)
		for _, path := range []string{language + "/root.source", language + "/leaf.source", language + "/config"} {
			if !pathSet(miss.snapshotPaths)[path] {
				t.Errorf("%s snapshot omitted %s: %v", language, path, miss.snapshotPaths)
			}
		}
	}
}

func BenchmarkDependencyGraphFingerprintsTwentyFiveThousandUnits(b *testing.B) {
	units := make(map[string]unitplan.Unit, 25_000)
	keys := make(map[string]analysiscache.Key, 25_000)
	languages := []unitplan.Language{unitplan.LanguageGo, unitplan.LanguageJava, unitplan.LanguageRust, unitplan.LanguageTypeScript}
	for _, language := range languages {
		for index := 0; index < 6250; index++ {
			id := fmt.Sprintf("%s:%05d", language, index)
			unit := unitplan.Unit{ID: id, Language: language}
			if index > 0 {
				unit.DirectDependencies = []string{fmt.Sprintf("%s:%05d", language, index-1)}
			}
			units[id] = unit
			keys[id] = analysiscache.Key(analysiscache.DigestBytes([]byte(id)))
		}
	}
	b.ResetTimer()
	for range b.N {
		_ = dependencyGraphFingerprints(units, keys)
	}
}

func TestPartitionCombinedInputsAssignsEachPathExactlyOnce(t *testing.T) {
	combined := newScoreInputs()
	combined.observations["a.go"] = map[string][]observation{"metric": {{path: "a.go", value: 1}}}
	combined.observations["b.go"] = map[string][]observation{"metric": {{path: "b.go", value: 2}}}
	combined.coverage["a.go"] = map[string]string{"metric": "complete"}
	combined.coverage["b.go"] = map[string]string{"metric": "complete"}
	combined.languages["a.go"], combined.languages["b.go"] = "go", "go"
	combined.diagnostics = []map[string]any{{"path": "a.go", "message": "owned"}, {"message": "global"}}
	combined.plans = []map[string]any{{"type": "execution_plan"}}
	units := []plannedCacheUnit{
		{plan: unitplan.Unit{ID: "a", Language: unitplan.LanguageGo}, owned: []string{"a.go"}},
		{plan: unitplan.Unit{ID: "b", Language: unitplan.LanguageGo}, owned: []string{"b.go"}},
	}
	partitioned := partitionCombinedUnitInputs(combined, units)
	if len(partitioned["a"].observations) != 1 || len(partitioned["b"].observations) != 1 ||
		partitioned["a"].observations["b.go"] != nil || partitioned["b"].observations["a.go"] != nil {
		t.Fatalf("partition leaked paths: %#v", partitioned)
	}
	if len(partitioned["a"].diagnostics) != 2 || len(partitioned["b"].diagnostics) != 1 {
		t.Fatalf("diagnostic partition = a:%v b:%v", partitioned["a"].diagnostics, partitioned["b"].diagnostics)
	}
	if partitioned["a"].plans[0]["discovered_source_count"] != 1 || partitioned["b"].plans[0]["unit_id"] != "b" {
		t.Fatalf("execution plans were not normalized per unit: %#v", partitioned)
	}
}

func BenchmarkPartitionCombinedInputsTwentyFiveThousandFiles(b *testing.B) {
	combined := newScoreInputs()
	units := make([]plannedCacheUnit, 25_000)
	for index := range units {
		path := fmt.Sprintf("pkg/%05d/source.go", index)
		id := fmt.Sprintf("go:package:%05d", index)
		combined.observations[path] = map[string][]observation{"metric": {{path: path, value: 1}}}
		combined.coverage[path] = map[string]string{"metric": "complete"}
		combined.languages[path] = "go"
		units[index] = plannedCacheUnit{plan: unitplan.Unit{ID: id, Language: unitplan.LanguageGo}, owned: []string{path}}
	}
	b.ResetTimer()
	for range b.N {
		_ = partitionCombinedUnitInputs(combined, units)
	}
}

func TestSameMtimeFixtureActuallyPreservedTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.go")
	if err := os.WriteFile(path, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Stat(path); err != nil || !got.ModTime().Equal(stamp) {
		t.Fatalf("mtime fixture = %v, %v", got, err)
	}
}
