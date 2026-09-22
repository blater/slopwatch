package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/sourcefs"
	"github.com/blater/slopwatch/internal/unitplan"
)

type observedSourceFS struct {
	mu sync.Mutex
	sourcefs.OS
	reads, stats, dirs map[string]int
}

func newObservedSourceFS() *observedSourceFS {
	return &observedSourceFS{reads: map[string]int{}, stats: map[string]int{}, dirs: map[string]int{}}
}
func (fs *observedSourceFS) ReadFile(path string) ([]byte, error) {
	fs.mu.Lock()
	fs.reads[path]++
	fs.mu.Unlock()
	return fs.OS.ReadFile(path)
}
func (fs *observedSourceFS) Stat(path string) (os.FileInfo, error) {
	fs.mu.Lock()
	fs.stats[path]++
	fs.mu.Unlock()
	return fs.OS.Stat(path)
}
func (fs *observedSourceFS) Lstat(path string) (os.FileInfo, error) {
	fs.mu.Lock()
	fs.stats[path]++
	fs.mu.Unlock()
	return fs.OS.Lstat(path)
}
func (fs *observedSourceFS) ReadDir(path string) ([]os.DirEntry, error) {
	fs.mu.Lock()
	fs.dirs[path]++
	fs.mu.Unlock()
	return fs.OS.ReadDir(path)
}
func (fs *observedSourceFS) reset() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	clear(fs.reads)
	clear(fs.stats)
	clear(fs.dirs)
}

type countedLookup struct {
	changeLookup
	counts map[string]int
}

func (v countedLookup) Unit(id string) (unitplan.Unit, bool) {
	v.counts["unit"]++
	return v.changeLookup.Unit(id)
}
func (v countedLookup) Source(path string) (*unitplan.Source, bool) {
	v.counts["source"]++
	return v.changeLookup.Source(path)
}
func (v countedLookup) Consumers(path string) []string {
	members := v.changeLookup.Consumers(path)
	v.counts["consumer_members"] += len(members)
	return members
}
func (v countedLookup) Referrers(path string) []string {
	members := v.changeLookup.Referrers(path)
	v.counts["referrer_members"] += len(members)
	return members
}
func (v countedLookup) Group(language unitplan.Language) []string {
	members := v.changeLookup.Group(language)
	v.counts["group_members"] += len(members)
	return members
}
func (v countedLookup) Conservative(language unitplan.Language) []string {
	members := v.changeLookup.Conservative(language)
	v.counts["conservative_members"] += len(members)
	return members
}
func (v countedLookup) Reverse(id string) []string {
	members := v.changeLookup.Reverse(id)
	v.counts["reverse_members"] += len(members)
	return members
}

func TestOneSpaceEditWorkIsIndependentOfWorkspaceSize(t *testing.T) {
	var reference map[string]int
	metrics := []map[string]any{}
	for _, size := range []int{8, 30000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "go.mod", "module example\n")
			writeTestFile(t, root, "app/main.go", "package app\nimport _ \"example/lib\"\n")
			writeTestFile(t, root, "lib/lib.go", "package lib\n")
			for i := 2; i < size; i++ {
				writeTestFile(t, root, fmt.Sprintf("unrelated/p%05d/p.go", i), "package p\n")
			}
			analyzer := newChangeTestAnalyzerWithOptions(t, root, Options{Targets: []string{"app"}, Languages: []string{"go"}, ReadCache: true}, goTestCatalog())
			fs := newObservedSourceFS()
			backendFS := newObservedSourceFS()
			analyzer.snapshotFileSystem = backendFS
			analyzer.fileSystem = fs
			starts := 0
			analyzer.beforeDiscovery = func() error { starts++; return nil }
			requests := []analyzerRequest{}
			analyzer.runUnits = recordChangeRequests(t, &requests)
			stopPeak := samplePeakHeap()
			setup := time.Now()
			if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
				t.Fatal(err)
			}
			cold := time.Since(setup)
			warmAnalyzer := newCacheTestAnalyzer(t, root, analyzer.options, cacheStore(analyzer.engine()), goTestCatalog())
			warmRequests := []analyzerRequest{}
			warmAnalyzer.runUnits = recordChangeRequests(t, &warmRequests)
			warmStart := time.Now()
			if _, err := warmAnalyzer.Analyze(context.Background(), nil, nil); err != nil {
				t.Fatal(err)
			}
			warm := time.Since(warmStart)
			if len(warmRequests) != 0 {
				t.Fatal("warm setup invoked backend")
			}

			analyzer.beforeDiscovery = func() error { t.Fatal("post-startup full planner/discovery invoked"); return errors.New("forbidden") }
			analyzer.beforeSourceDiscovery = analyzer.beforeDiscovery
			counts := map[string]int{}
			analyzer.decorateLookup = func(view changeLookup) changeLookup { return countedLookup{view, counts} }
			fs.reset()
			requests = nil
			duplicate := time.Now()
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/main.go"}); err != nil {
				t.Fatal(err)
			}
			unchanged := time.Since(duplicate)
			if len(requests) != 0 {
				t.Fatal("duplicate notification invoked backend")
			}
			fs.reset()
			clear(counts)
			writeTestFile(t, root, "app/main.go", "package app\nimport _ \"example/lib\"\n ")
			start := time.Now()
			document, replacements, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/main.go"})
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			assertChangePaths(t, document, replacements, []string{"app/main.go"})
			if len(fs.dirs) != 0 {
				t.Fatalf("directory enumeration: %v", fs.dirs)
			}
			if len(backendFS.reads) != 2 {
				t.Fatalf("backend materialization reads=%v", backendFS.reads)
			}
			if len(fs.reads) != 1 || fs.reads[filepath.Join(analyzer.workspace, "app/main.go")] != 1 {
				t.Fatalf("planning reads: %v", fs.reads)
			}
			if starts != 1 {
				t.Fatalf("startup phases=%d", starts)
			}
			if len(requests) != 1 {
				t.Fatalf("backend requests=%d", len(requests))
			}
			assertRequestPaths(t, requests, []string{"app/main.go", "lib/lib.go"}, nil)
			if reference == nil {
				reference = map[string]int{}
				for key, value := range counts {
					reference[key] = value
				}
			} else if !reflect.DeepEqual(reference, counts) {
				t.Fatalf("workspace-dependent lookup work: small=%v large=%v", reference, counts)
			}

			editCounts := map[string]int{}
			for key, value := range counts {
				editCounts[key] = value
			}
			fs.reset()
			requests = nil
			writeTestFile(t, root, "lib/new.go", "package lib\n")
			directoryStart := time.Now()
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/new.go"}); err != nil {
				t.Fatal(err)
			}
			directoryCreate := time.Since(directoryStart)
			if len(fs.dirs) != 0 || len(fs.reads) != 1 {
				t.Fatalf("new member planning operations: dirs=%v reads=%v", fs.dirs, fs.reads)
			}
			if err := os.Remove(filepath.Join(root, "lib/new.go")); err != nil {
				t.Fatal(err)
			}
			fs.reset()
			directoryStart = time.Now()
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/new.go"}); err != nil {
				t.Fatal(err)
			}
			directoryDelete := time.Since(directoryStart)
			if len(fs.dirs) != 0 || len(fs.reads) != 0 {
				t.Fatalf("deleted member planning operations: dirs=%v reads=%v", fs.dirs, fs.reads)
			}
			requests = nil
			fs.reset()
			writeTestFile(t, root, "app/main.go", "package app\nimport _ \"example/lib\"\n")
			hitStart := time.Now()
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/main.go"}); err != nil {
				t.Fatal(err)
			}
			hitElapsed := time.Since(hitStart)
			if len(requests) != 0 {
				t.Fatal("return to cached bytes invoked backend")
			}
			analyzer.SetCacheStore(nil)
			requests = nil
			fs.reset()
			writeTestFile(t, root, "app/main.go", "package app\nimport _ \"example/lib\"\n  ")
			uncachedStart := time.Now()
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/main.go"}); err != nil {
				t.Fatal(err)
			}
			uncachedElapsed := time.Since(uncachedStart)
			if len(requests) != 1 || len(fs.dirs) != 0 || len(fs.reads) != 1 {
				t.Fatalf("uncached request work: requests=%d dirs=%v reads=%v", len(requests), fs.dirs, fs.reads)
			}
			peakHeap := stopPeak()
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			metrics = append(metrics, map[string]any{"source_files": size, "cold_setup_ns": cold.Nanoseconds(), "warm_setup_ns": warm.Nanoseconds(), "directory_member_create_ns": directoryCreate.Nanoseconds(), "directory_member_delete_ns": directoryDelete.Nanoseconds(), "cache_hit_edit_ns": hitElapsed.Nanoseconds(), "uncached_edit_ns": uncachedElapsed.Nanoseconds(), "peak_sampled_heap_bytes": peakHeap, "unchanged_notification_ns": unchanged.Nanoseconds(), "one_space_edit_ns": elapsed.Nanoseconds(), "planning_source_reads": len(fs.reads), "directory_reads": len(fs.dirs), "lookup_counts": editCounts, "backend_context_sources": 2, "process_heap_bytes_after": memory.HeapAlloc})
		})
	}
	if cwd, err := os.Getwd(); err == nil {
		path := filepath.Join(cwd, "..", "..", "..", "build", "incremental-refresh-work.json")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.MarshalIndent(metrics, "", "  ")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIncrementalEntryPointsCannotExportOrRediscover(t *testing.T) {
	// This focused architectural guard covers the production entry chain. Startup
	// constructors and bounded selected-unit helpers are intentionally separate.
	files := []string{"incremental_changes.go", "incremental_sources.go", "incremental_selection.go", "incremental_cache.go"}
	forbidden := []string{"PlanWorkspace(", "discoverPolicy(", "discoverPolicyWithMissingTargets(", "allChangeUnits(", "filterPlannedUnits(", "os.ReadDir(", "filepath.Walk", "range snapshot.plan.Units", "range snapshot.discovered"}
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if name == "incremental_sources.go" {
			start := strings.Index(text, "func initializeSession(")
			end := strings.Index(text, "func verifySourceStamp(")
			text = text[:start] + text[end:]
		}
		for _, call := range forbidden {
			if strings.Contains(text, call) {
				t.Errorf("%s calls forbidden %s", name, call)
			}
		}
	}
}

func TestIdenticalByteReplacementRefreshesStampBeforeDependencyEdit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example\n")
	writeTestFile(t, root, "lib/lib.go", "package lib\n")
	writeTestFile(t, root, "app/app.go", "package app\nimport _ \"example/lib\"\n")
	analyzer := newChangeTestAnalyzer(t, root)
	requests := []analyzerRequest{}
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	requests = nil
	writeTestFile(t, root, "lib/lib.go", "package lib\n")
	if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/lib.go"}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Fatal("metadata-only edit invoked backend")
	}
	writeTestFile(t, root, "app/app.go", "package app\nimport _ \"example/lib\"\n ")
	if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/app.go"}); err != nil {
		t.Fatal(err)
	}
}

func samplePeakHeap() func() uint64 {
	stop := make(chan struct{})
	result := make(chan uint64, 1)
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		var peak uint64
		for {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			if memory.HeapAlloc > peak {
				peak = memory.HeapAlloc
			}
			select {
			case <-stop:
				result <- peak
				return
			case <-ticker.C:
			}
		}
	}()
	return func() uint64 { close(stop); return <-result }
}

type failingSourceFS struct {
	sourcefs.OS
	failPath   string
	failure    error
	mutatePath string
	mutate     func()
}

func (fs *failingSourceFS) ReadFile(path string) ([]byte, error) {
	if path == fs.failPath {
		return nil, fs.failure
	}
	return fs.OS.ReadFile(path)
}
func (fs *failingSourceFS) Stat(path string) (os.FileInfo, error) {
	info, err := fs.OS.Stat(path)
	if path == fs.mutatePath && fs.mutate != nil {
		change := fs.mutate
		fs.mutate = nil
		change()
	}
	return info, err
}

func TestFailedNamedBatchSurvivesDifferentLaterEvent(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example\n")
	writeTestFile(t, root, "a/a.go", "package a\n")
	writeTestFile(t, root, "b/b.go", "package b\n")
	analyzer := newChangeTestAnalyzer(t, root)
	requests := []analyzerRequest{}
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	prior, _ := analyzer.plan.index.Source("a/a.go")
	priorDigest := prior.Digest
	writeTestFile(t, root, "a/a.go", "package a\n ")
	failure := errors.New("named input read failed")
	fs := &failingSourceFS{failPath: filepath.Join(analyzer.workspace, "a/a.go"), failure: failure}
	analyzer.fileSystem = fs
	if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"a/a.go"}); !errors.Is(err, failure) {
		t.Fatalf("failure=%v", err)
	}
	if current, _ := analyzer.plan.index.Source("a/a.go"); current.Digest != priorDigest {
		t.Fatal("failed batch committed source")
	}
	fs.failPath = ""
	requests = nil
	writeTestFile(t, root, "b/b.go", "package b\n ")
	document, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{"b/b.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replaced, []string{"a/a.go", "b/b.go"})
	assertRequestPaths(t, requests, []string{"a/a.go", "b/b.go"}, nil)
	if len(analyzer.plan.pending) != 0 {
		t.Fatalf("successful dirty paths retained: %v", analyzer.plan.pending)
	}
}
func TestInputRaceCarriesExactContextPathOnHitAndMiss(t *testing.T) {
	for _, hit := range []bool{false, true} {
		t.Run(fmt.Sprint(hit), func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "go.mod", "module example\n")
			original := "package app\nimport _ \"example/lib\"\n"
			writeTestFile(t, root, "app/app.go", original)
			writeTestFile(t, root, "lib/lib.go", "package lib\n")
			analyzer := newChangeTestAnalyzer(t, root)
			requests := []analyzerRequest{}
			analyzer.runUnits = recordChangeRequests(t, &requests)
			if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, root, "app/app.go", original+" ")
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/app.go"}); err != nil {
				t.Fatal(err)
			}
			next := original + "  "
			if hit {
				next = original
			}
			writeTestFile(t, root, "app/app.go", next)
			fs := &failingSourceFS{mutatePath: filepath.Join(analyzer.workspace, "lib/lib.go"), mutate: func() { writeTestFile(t, root, "lib/lib.go", "package lib\nconst Changed=1\n") }}
			analyzer.fileSystem = fs
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/app.go"}); !errors.Is(err, ErrWorkspaceChanged) {
				t.Fatalf("race=%v", err)
			}
			if !analyzer.plan.pending["lib/lib.go"] || !analyzer.plan.pending["app/app.go"] {
				t.Fatalf("exact race inputs not pending: %v", analyzer.plan.pending)
			}
			document, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{"non-source.txt"})
			if err != nil {
				t.Fatal(err)
			}
			assertChangePaths(t, document, replaced, []string{"app/app.go", "lib/lib.go"})
		})
	}
}
func TestEmptyFollowSessionAcceptsFirstSourceWithoutDiscovery(t *testing.T) {
	root := t.TempDir()
	analyzer := newChangeTestAnalyzer(t, root)
	analyzer.options.Languages = nil
	requests := []analyzerRequest{}
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.AnalyzeStartup(context.Background(), nil); !errors.Is(err, ErrNoSources) {
		t.Fatalf("empty startup=%v", err)
	}
	if analyzer.plan == nil {
		t.Fatal("empty startup did not retain session")
	}
	analyzer.beforeDiscovery = func() error { t.Fatal("first source replanned workspace"); return ErrIncrementalPlanUnavailable }
	analyzer.beforeSourceDiscovery = analyzer.beforeDiscovery
	writeTestFile(t, root, "first.go", "package first\n")
	document, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{"first.go"})
	if err != nil {
		t.Fatal(err)
	}
	assertChangePaths(t, document, replaced, []string{"first.go"})
}
func TestConservativeRefreshReadsOnlyNamedPlanningInput(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a/a.go", "package a\n")
	writeTestFile(t, root, "b/b.go", "package b\n")
	analyzer := newChangeTestAnalyzer(t, root)
	requests := []analyzerRequest{}
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	fs := newObservedSourceFS()
	analyzer.fileSystem = fs
	requests = nil
	writeTestFile(t, root, "a/a.go", "package a\n ")
	if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"a/a.go"}); err != nil {
		t.Fatal(err)
	}
	if len(fs.reads) != 1 || len(fs.dirs) != 0 {
		t.Fatalf("conservative planning read unrelated inputs: %v %v", fs.reads, fs.dirs)
	}
	assertRequestPaths(t, requests, []string{"a/a.go", "b/b.go"}, nil)
}

type startupRaceFS struct {
	sourcefs.OS
	path   string
	change func()
}

func (fs *startupRaceFS) ReadFile(path string) ([]byte, error) {
	data, err := fs.OS.ReadFile(path)
	if path == fs.path && fs.change != nil {
		change := fs.change
		fs.change = nil
		change()
	}
	return data, err
}
func TestStartupImportFactsCannotUseLaterDigest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example\n")
	writeTestFile(t, root, "app/app.go", "package app\nimport _ \"example/old\"\n")
	analyzer := newChangeTestAnalyzer(t, root)
	requests := []analyzerRequest{}
	analyzer.runUnits = recordChangeRequests(t, &requests)
	analyzer.fileSystem = &startupRaceFS{path: filepath.Join(analyzer.workspace, "app/app.go"), change: func() { writeTestFile(t, root, "app/app.go", "package app\nimport _ \"example/new\"\n") }}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); !errors.Is(err, ErrWorkspaceChanged) {
		t.Fatalf("mixed startup facts/digest accepted: %v", err)
	}
	if analyzer.plan != nil {
		t.Fatal("inconsistent initial session was installed")
	}
}

func TestScopedStartupToleratesOnlyUnrequiredUnavailableInputs(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "go.mod", "module example\n")
			writeTestFile(t, root, "app/a.go", "package app\nimport _ \"example/lib\"\n")
			writeTestFile(t, root, "lib/lib.go", "package lib\n")
			writeTestFile(t, root, "unrelated/A.java", "class A {}")
			if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(root, "dangling.go")); err != nil {
				t.Fatal(err)
			}
			analyzer := newChangeTestAnalyzerWithOptions(t, root, Options{Targets: []string{"app/a.go"}, Languages: []string{"go"}, ReadCache: true}, goTestCatalog())
			requests := []analyzerRequest{}
			analyzer.runUnits = recordChangeRequests(t, &requests)
			bad := "unrelated/A.java"
			if required {
				bad = "lib/lib.go"
			}
			failure := errors.New("unreadable source")
			analyzer.fileSystem = &failingSourceFS{failPath: filepath.Join(analyzer.workspace, bad), failure: failure}
			_, err := analyzer.Analyze(context.Background(), nil, nil)
			if required {
				if !errors.Is(err, failure) {
					t.Fatalf("required unavailable input=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(requests) != 1 {
				t.Fatalf("scoped analysis did not run: %v", requests)
			}
		})
	}
}
func TestOutsideTargetDiagnosticRetriesIdenticalDependency(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example\n")
	writeTestFile(t, root, "app/a.go", "package app\nimport _ \"example/lib\"\n")
	writeTestFile(t, root, "lib/lib.go", "package lib\n")
	analyzer := newChangeTestAnalyzerWithOptions(t, root, Options{Targets: []string{"app/a.go"}, Languages: []string{"go"}, ReadCache: true}, goTestCatalog())
	requests := []analyzerRequest{}
	analyzer.runUnits = recordChangeRequests(t, &requests)
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	analyzer.runUnits = func(context.Context, string, analyzerRequest) (map[string]scoreInputs, error) {
		return nil, errors.New("transient dependency failure")
	}
	writeTestFile(t, root, "lib/lib.go", "package lib\nconst A=2\n")
	doc, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"lib/lib.go"})
	if err != nil {
		t.Fatal(err)
	}
	requireRecoveryDiagnostic(t, doc.Diagnostics, "native.analyzer_failed")
	requests = nil
	analyzer.runUnits = recordChangeRequests(t, &requests)
	doc, _, err = analyzer.AnalyzeChanges(context.Background(), []string{"lib/lib.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || len(doc.Files) != 1 || !doc.Files[0].Complete {
		t.Fatalf("outside dependency retry skipped: requests=%v files=%v", requests, doc.Files)
	}
}
func TestPreparedTopologyFailureDoesNotCommitAndRetriesDifferentEvent(t *testing.T) {
	for _, verification := range []bool{false, true} {
		t.Run(fmt.Sprint(verification), func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "go.mod", "module example\n")
			writeTestFile(t, root, "app/a.go", "package app\nimport _ \"example/old\"\n")
			writeTestFile(t, root, "old/a.go", "package old\n")
			analyzer := newChangeTestAnalyzer(t, root)
			requests := []analyzerRequest{}
			analyzer.runUnits = recordChangeRequests(t, &requests)
			if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
				t.Fatal(err)
			}
			prior, _ := analyzer.plan.index.Unit("go:package:app")
			priorReverse := analyzer.plan.index.Reverse("go:package:old")
			writeTestFile(t, root, "app/a.go", "package app\nimport _ \"example/new\"\n")
			writeTestFile(t, root, "new/a.go", "package new\n")
			failure := errors.New("materialization failed")
			if verification {
				analyzer.runUnits = func(ctx context.Context, root string, request analyzerRequest) (map[string]scoreInputs, error) {
					writeTestFile(t, analyzer.workspace, "new/a.go", "package new\nconst Later=1\n")
					return fakeBatchInputs(t, request), nil
				}
			} else {
				analyzer.snapshotFileSystem = &failingSourceFS{failPath: filepath.Join(analyzer.workspace, "new/a.go"), failure: failure}
			}
			_, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"app/a.go", "new/a.go"})
			if err == nil {
				t.Fatal("prepared failure succeeded")
			}
			current, _ := analyzer.plan.index.Unit("go:package:app")
			if !reflect.DeepEqual(current, prior) || !reflect.DeepEqual(analyzer.plan.index.Reverse("go:package:old"), priorReverse) {
				t.Fatal("failed preparation changed committed edges")
			}
			if _, ok := analyzer.plan.index.Source("new/a.go"); ok {
				t.Fatal("failed new membership committed")
			}
			if _, ok := analyzer.plan.index.Unit("go:package:new"); ok {
				t.Fatal("failed resolution committed")
			}
			analyzer.snapshotFileSystem = nil
			analyzer.runUnits = recordChangeRequests(t, &requests)
			writeTestFile(t, root, "old/a.go", "package old\nconst Other=2\n")
			if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{"old/a.go"}); err != nil {
				t.Fatal(err)
			}
			current, _ = analyzer.plan.index.Unit("go:package:app")
			if !reflect.DeepEqual(current.DirectDependencies, []string{"go:package:new"}) || len(analyzer.plan.index.Reverse("go:package:old")) != 0 || !reflect.DeepEqual(analyzer.plan.index.Reverse("go:package:new"), []string{"go:package:app"}) {
				t.Fatalf("retried topology differs: %+v", current)
			}
		})
	}
}

func TestIndexedOwnerMetadataPreservesTypeScriptOverlapPreference(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tsconfig.json", "{}")
	writeTestFile(t, root, "nested/tsconfig.json", "{}")
	writeTestFile(t, root, "nested/a.ts", "export const a=1;")
	plan, err := unitplan.PlanWorkspace(root, unitplan.Options{TypeScriptMode: unitplan.TypeScriptTyped})
	if err != nil {
		t.Fatal(err)
	}
	assertOwner := func(view unitplan.Lookup) {
		t.Helper()
		if got := canonicalOwner(view, "nested/a.ts", Options{Languages: []string{"typescript"}}); got != "typescript:typed:nested/tsconfig.json" {
			t.Fatalf("canonical overlap owner=%s", got)
		}
	}
	assertOwner(plan.Index)
	delta := plan.Index.Prepare(map[string]*unitplan.Source{"nested/a.ts": unitplan.ParseSource("nested/a.ts", []byte("export const a=2;")), "nested/b.ts": unitplan.ParseSource("nested/b.ts", []byte("export const b=3;"))})
	assertOwner(delta)
	delta.Commit()
	assertOwner(plan.Index)
}
