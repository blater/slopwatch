package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/analysiscache"
)

func TestStartupVerificationInvalidatesChangedInputs(t *testing.T) {
	workspace, analyzer := cacheHitFixture(t)
	calls := 0
	analyzer.runUnits = countingFakeBatchRunner(t, &calls)
	first := analyzeTestDocument(t, analyzer)
	// Simulate an existing cache created before file stamps were introduced.
	store := cacheStore(analyzer.engine())
	view, err := analyzer.viewKey(analyzer.options)
	if err != nil {
		t.Fatal(err)
	}
	generation, _ := store.LoadGeneration(view)
	generation.InputStamps = nil
	generation.InputDigests = nil
	if _, err := store.CommitGeneration(view, generation); err != nil {
		t.Fatal(err)
	}
	verified, err := analyzer.AnalyzeStartup(context.Background(), nil)
	if err != nil || calls != 1 || len(verified.Files) != len(first.Files) || verified.Files[0].Score != first.Files[0].Score {
		t.Fatalf("warm verification: calls=%d err=%v", calls, err)
	}
	upgraded, _ := store.LoadGeneration(view)
	if len(upgraded.InputStamps) == 0 || len(upgraded.InputDigests) == 0 {
		t.Fatal("startup did not upgrade old cache metadata")
	}
	for _, change := range []struct{ path, text string }{
		{"pkg/a.go", "package pkg\nvar A = 2\n"},
		{"pkg/new.go", "package pkg\nvar New = 3\n"},
		{"go.mod", "module changed\n"},
	} {
		before := calls
		writeTestFile(t, workspace, change.path, change.text)
		if _, err := analyzer.AnalyzeStartup(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		if calls <= before {
			t.Fatalf("change to %s reused stale report", change.path)
		}
	}
	before := calls
	writeTestFile(t, analyzer.root, "analyzers/structural/slopslap-structural", "updated backend")
	if _, err := analyzer.AnalyzeStartup(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if calls <= before {
		t.Fatal("changed backend reused stale report")
	}
	if err := os.Remove(filepath.Join(workspace, "pkg/new.go")); err != nil {
		t.Fatal(err)
	}
	doc, err := analyzer.AnalyzeStartup(context.Background(), nil)
	if err != nil || len(doc.Files) != 1 {
		t.Fatalf("deleted source retained: files=%d err=%v", len(doc.Files), err)
	}
}

func TestWorkspaceDigestReuseReadsOnlyChangedFiles(t *testing.T) {
	workspace, analyzer := cacheHitFixture(t)
	paths := map[string]bool{"pkg/a.go": true}
	first, stamps, err := cachedWorkspaceDigests(analyzer.engine(), context.Background(), paths, analysiscache.Generation{})
	if err != nil {
		t.Fatal(err)
	}
	// A sentinel proves this takes the metadata path instead of hashing contents.
	previous := analysiscache.Generation{InputStamps: stamps, InputDigests: map[string]analysiscache.Digest{"pkg/a.go": "saved-digest"}}
	reused, _, err := cachedWorkspaceDigests(analyzer.engine(), context.Background(), paths, previous)
	if err != nil || reused["pkg/a.go"] != "saved-digest" {
		t.Fatalf("unchanged input was rehashed: %v %v", reused, err)
	}
	writeTestFile(t, workspace, "pkg/a.go", "package pkg\nvar A = 200\n")
	changed, _, err := cachedWorkspaceDigests(analyzer.engine(), context.Background(), paths, previous)
	if err != nil || changed["pkg/a.go"] == "saved-digest" || changed["pkg/a.go"] == first["pkg/a.go"] {
		t.Fatalf("modified input was not rehashed: %v %v", changed, err)
	}
	unchanged, err := verifyWorkspaceStamps(analyzer.engine(), context.Background(), stamps)
	if err != nil || unchanged {
		t.Fatalf("concurrent modification passed final check: unchanged=%v err=%v", unchanged, err)
	}
}

// This measures the complete startup verification path with real filesystem
// discovery/planning/cache IO, excluding initial semantic analysis and fixture
// creation. Each iteration uses a new Analyzer (no in-memory cached plan).
func BenchmarkStartupVerification(b *testing.B) {
	for _, count := range []int{10000, 20000, 30000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			workspace := b.TempDir()
			writeTestFile(b, workspace, "go.mod", "module example\n")
			for i := 0; i < count; i++ {
				writeTestFile(b, workspace, fmt.Sprintf("pkg%d/f%d.go", i/100, i), fmt.Sprintf("package pkg%d\nvar V%d = 1\n// %s\n", i/100, i, strings.Repeat("source padding ", 280)))
			}
			store, err := analysiscache.NewStore(filepath.Join(b.TempDir(), "cache"))
			if err != nil {
				b.Fatal(err)
			}
			options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true, ShallowProfile: ShallowProfileLegacy}
			analyzer := newCacheTestAnalyzer(b, workspace, options, store, goTestCatalog())
			analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
				return fakeBatchInputs(b, request), nil
			}
			paths := make(map[string]bool, count)
			for i := 0; i < count; i++ {
				paths[fmt.Sprintf("pkg%d/f%d.go", i/100, i)] = true
			}
			coldStart := time.Now()
			_, stamps, err := cachedWorkspaceDigests(analyzer.engine(), context.Background(), paths, analysiscache.Generation{})
			if err != nil {
				b.Fatal(err)
			}
			if valid, err := verifyWorkspaceStamps(analyzer.engine(), context.Background(), stamps); err != nil || !valid {
				b.Fatalf("cold input verification: %v", err)
			}
			coldDuration := time.Since(coldStart)
			if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			b.ReportMetric(coldDuration.Seconds(), "uncached-inputs-sec")
			for i := 0; i < b.N; i++ {
				fresh := &Analyzer{workspace: analyzer.workspace, root: analyzer.root, catalog: analyzer.catalog, options: options}
				fresh.SetCacheStore(store)
				fresh.runUnits = func(context.Context, string, analyzerRequest) (map[string]scoreInputs, error) {
					b.Fatal("unchanged workspace invoked backend")
					return nil, nil
				}
				start := time.Now()
				doc, err := fresh.AnalyzeStartup(context.Background(), nil)
				if err != nil || len(doc.Files) != count {
					b.Fatalf("files=%d err=%v", len(doc.Files), err)
				}
				if time.Since(start) > time.Duration(count/10000)*10*time.Second {
					b.Fatal("verification exceeded 10 seconds per 10,000 files")
				}
			}
		})
	}
}

// Kafka's root Gradle build owns nested src/main/java trees. Names containing
// "test" (CompositeStrategy, DeleteStreams...) must not silently lose ownership.
func TestStartupCacheCoversNestedGradleProductionSources(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "build.gradle", "plugins { id 'java' }\n")
	for _, name := range []string{"CompositeStrategy", "DeleteStreamsGroupOffsetsOptions", "TestPurgatoryPerformance"} {
		writeTestFile(t, workspace, "clients/src/main/java/"+name+".java", "class "+name+" {}\n")
	}
	writeTestFile(t, workspace, "clients/src/test/java/Fixture.java", "class Fixture {}\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := goTestCatalog()
	catalog.Languages = []string{"java"}
	catalog.Components[0].Support = map[string]string{"java": "supported"}
	analyzer := newCacheTestAnalyzer(t, workspace, Options{Targets: []string{"."}, Languages: []string{"java"}, ReadCache: true, ShallowProfile: ShallowProfileLegacy}, store, catalog)
	writeTestFile(t, analyzer.root, "analyzers/structural/slopslap-structural-java.jar", "test jar")
	writeTestFile(t, analyzer.root, "analyzers/structural/java-runtime/bin/java", "test java")
	calls := 0
	analyzer.runUnits = countingFakeBatchRunner(t, &calls)
	first, err := analyzer.AnalyzeStartup(context.Background(), nil)
	if err != nil || len(first.Files) != 3 {
		t.Fatalf("initial files=%d err=%v", len(first.Files), err)
	}
	before := calls
	second, err := analyzer.AnalyzeStartup(context.Background(), nil)
	if err != nil || len(second.Files) != 3 || calls != before {
		t.Fatalf("unchanged startup reran analysis: calls=%d before=%d files=%d err=%v", calls, before, len(second.Files), err)
	}
	if second.Summary["cache_state"] != "current" {
		t.Fatal("startup did not reuse saved projection")
	}
}
