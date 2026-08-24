package native

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/unitplan"
)

func TestPersistentUnitCacheHitAndContentInvalidation(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 1\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, Timeout: 10, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest, _ float64) (map[string]scoreInputs, error) {
		calls++
		return fakeBatchInputs(t, request), nil
	}

	first, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(first.Files) != 1 {
		t.Fatalf("first analysis calls/files = %d/%d", calls, len(first.Files))
	}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	hit, err := analyzer.Analyze(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("same-size/same-mtime content change was a cache hit; calls = %d", calls)
	}
}

func TestCorruptUnitArtifactIsAMiss(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "a.go", "package sample\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, Timeout: 10, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest, _ float64) (map[string]scoreInputs, error) {
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
	options := Options{Targets: []string{"pkg/a.go"}, Languages: []string{"go"}, Timeout: 10, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest, _ float64) (map[string]scoreInputs, error) {
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
	options := Options{Targets: []string{"a/a.go"}, Languages: []string{"go"}, Timeout: 10, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	var gotPaths []string
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest, _ float64) (map[string]scoreInputs, error) {
		if len(request.Units) != 1 {
			t.Fatalf("protocol units = %d, want one combined language unit", len(request.Units))
		}
		if request.Units[0].ID != "go-unit" {
			t.Fatalf("combined unit ID = %q, want coverage-compatible go-unit", request.Units[0].ID)
		}
		if request.Workspace == workspace {
			t.Fatal("analyzer received the live workspace")
		}
		gotPaths = append([]string(nil), request.Units[0].Paths...)
		for _, path := range gotPaths {
			info, err := os.Stat(filepath.Join(request.Workspace, filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o222 != 0 {
				t.Fatalf("snapshot source %s is writable: %o", path, info.Mode().Perm())
			}
		}
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

func TestMutationDuringSnapshotRetriesBeforeCommit(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "go.mod", "module example\n")
	writeTestFile(t, workspace, "a.go", "package sample\nvar A = 1\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, Timeout: 10, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest, _ float64) (map[string]scoreInputs, error) {
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
	options := Options{Targets: []string{"."}, Languages: []string{"typescript"}, TypeScriptTypes: true, Timeout: 10}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, typescriptTestCatalog())
	plan, err := unitplan.PlanWorkspace(workspace, unitplan.Options{TypeScriptMode: unitplan.TypeScriptTyped})
	if err != nil {
		t.Fatal(err)
	}
	discovered := map[string][]string{"typescript": {"nested/inner.ts", "outer.ts"}}
	active := filterPlannedUnits(plan.Units, discovered, []string{"typescript"}, false)
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
	first, err := analyzer.prepareCacheUnits(context.Background(), store, analyzer.catalog, plan.Units, active, options)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workspace, "nested/inner.ts", "export const inner = 2;\n")
	second, err := analyzer.prepareCacheUnits(context.Background(), store, analyzer.catalog, plan.Units, active, options)
	if err != nil {
		t.Fatal(err)
	}
	firstKeys, secondKeys := unitKeysByID(first.units), unitKeysByID(second.units)
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
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, Timeout: 10, ReadCache: true}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, goTestCatalog())
	var lock sync.Mutex
	calls := 0
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest, _ float64) (map[string]scoreInputs, error) {
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
