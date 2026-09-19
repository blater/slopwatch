package native

// This opt-in diagnostic copies a bounded source sample into an isolated
// workspace. It never edits the source repository or uses the user's cache.
import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
)

func TestPerformanceProfile(t *testing.T) {
	source, output := os.Getenv("SLOPWATCH_PROFILE_SOURCE"), os.Getenv("SLOPWATCH_PROFILE_OUTPUT")
	if source == "" || output == "" {
		t.Skip("set SLOPWATCH_PROFILE_SOURCE and a new SLOPWATCH_PROFILE_OUTPUT directory")
	}
	limit := 40
	if value := os.Getenv("SLOPWATCH_PROFILE_FILES"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 500 {
			t.Fatal("SLOPWATCH_PROFILE_FILES must be between 1 and 500")
		}
	}
	ext := os.Getenv("SLOPWATCH_PROFILE_EXT")
	if ext == "" {
		ext = ".java"
	}
	language := map[string]string{".java": "java", ".go": "go", ".ts": "typescript", ".rs": "rust"}[ext]
	if language == "" {
		t.Fatal("unsupported SLOPWATCH_PROFILE_EXT")
	}
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	source, err = filepath.Abs(source)
	check(err)
	source, err = filepath.EvalSymlinks(source)
	check(err)
	output, err = filepath.Abs(output)
	check(err)
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	check(err)
	output = filepath.Join(parent, filepath.Base(output))
	if relative, err := filepath.Rel(source, output); err != nil || relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatal("profile output must be outside the source directory")
	}
	check(os.Mkdir(output, 0o700)) // Refuse to overwrite an existing capture.
	workspace := filepath.Join(output, "workspace")
	check(os.Mkdir(workspace, 0o700))
	var paths []string
	hashes := map[string]string{}
	var sourceBytes int64
	check(filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "build", "target", "dist", "test", "tests", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || filepath.Ext(path) != ext || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > 2<<20 || sourceBytes+info.Size() > 32<<20 {
			return fmt.Errorf("sample exceeds 2 MiB/file or 32 MiB total at %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(workspace, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		sourceBytes += int64(len(data))
		hashes[filepath.ToSlash(relative)] = fmt.Sprintf("%x", sha256.Sum256(data))
		if len(paths) == limit {
			return fs.SkipAll
		}
		return nil
	}))
	if len(paths) == 0 {
		t.Fatal("no matching sources")
	}
	manifest, err := json.MarshalIndent(paths, "", "  ")
	check(err)
	check(os.WriteFile(filepath.Join(output, "sources.json"), manifest, 0o600))
	writeJSON := func(name string, value any) {
		t.Helper()
		data, err := json.MarshalIndent(value, "", "  ")
		check(err)
		check(os.WriteFile(filepath.Join(output, name), data, 0o600))
	}
	writeJSON("metadata.json", map[string]any{"source": source, "source_bytes": sourceBytes, "source_sha256": hashes, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "gomaxprocs": runtime.GOMAXPROCS(0), "language": language, "profile_scope": "frontend analysis only; excludes backend CPU, setup and report serialization", "selection": "lexical prefix; source only, without build configuration", "allocation_scope": "frontend; includes CPU profiler overhead; heap profiles cumulative"})
	store, err := analysiscache.NewStore(filepath.Join(output, "cache"))
	check(err)
	root := testInstallationRoot(t)
	var backendCalls atomic.Int64
	newAnalyzer := func() *Analyzer {
		a, err := New(workspace, root, Options{Targets: []string{"."}, Languages: []string{language}, ReadCache: true, DisableGitignore: true})
		check(err)
		a.SetCacheStore(store)
		a.runUnits = func(ctx context.Context, executable string, request analyzerRequest) (map[string]scoreInputs, error) {
			backendCalls.Add(1)
			start := time.Now()
			result, err := runAnalyzerUnits(ctx, executable, request)
			t.Logf("backend language=%s elapsed=%s error=%v", language, time.Since(start), err)
			return result, err
		}
		return a
	}
	a := newAnalyzer()
	var baseline []report.File
	mergedScores := map[string]float64{}
	for _, phase := range []string{"cold", "warm", "edit", "fresh"} {
		if phase == "warm" {
			a = newAnalyzer()
		}
		if phase == "fresh" {
			a = newAnalyzer()
			a.SetCacheReads(false) // Normal CLI analysis without --use-cache.
		}
		if phase == "edit" {
			path := filepath.Join(workspace, paths[0])
			data, err := os.ReadFile(path)
			check(err)
			check(os.WriteFile(path, append(data, []byte("\n// performance profile edit\n")...), 0o600))
		}
		runtime.GC()
		backendCalls.Store(0)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		cpu, err := os.Create(filepath.Join(output, phase+".cpu.pprof"))
		check(err)
		check(pprof.StartCPUProfile(cpu))
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		var document report.Document
		var replacements []string
		if phase == "edit" {
			document, replacements, err = a.AnalyzeChanges(ctx, []string{paths[0]})
		} else {
			document, err = a.Analyze(ctx, []string{"."}, []string{language})
		}
		analysisTime := time.Since(start)
		cancel()
		pprof.StopCPUProfile()
		check(cpu.Close())
		runtime.ReadMemStats(&after)
		t.Logf("phase=%s analysis=%s files=%d allocated_bytes=%d error=%v", phase, analysisTime, len(document.Files), after.TotalAlloc-before.TotalAlloc, err)
		check(err)
		runtime.GC() // Publish completed allocations to the sampled heap profile.
		heap, err := os.Create(filepath.Join(output, phase+".heap.pprof"))
		check(err)
		check(pprof.WriteHeapProfile(heap))
		check(heap.Close())
		start = time.Now()
		document.SortAndRank()
		data, err := json.Marshal(document)
		check(err)
		renderTime := time.Since(start)
		t.Logf("phase=%s sort_and_json=%s output_bytes=%d summary=%v", phase, renderTime, len(data), document.Summary)
		writeJSON(phase+".metrics.json", map[string]any{"analysis_ns": analysisTime.Nanoseconds(), "sort_and_json_ns": renderTime.Nanoseconds(), "output_bytes": len(data), "files": len(document.Files), "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "incremental": phase == "edit", "replacement_paths": replacements, "backend_calls": backendCalls.Load(), "backend_calls_instrumented": phase != "fresh"})
		// Full evidence reports are highly repetitive; retain them losslessly
		// without making diagnostic captures another source of disk growth.
		reportFile, err := os.Create(filepath.Join(output, phase+".json.gz"))
		check(err)
		compressed, err := gzip.NewWriterLevel(reportFile, gzip.BestSpeed)
		check(err)
		_, err = compressed.Write(data)
		check(err)
		check(compressed.Close())
		check(reportFile.Close())
		if phase == "cold" {
			baseline = document.Files
			if len(document.Files) != len(paths) {
				t.Fatal("sample and analyzed inventories differ")
			}
			for _, file := range document.Files {
				if _, ok := hashes[file.Path]; !ok {
					t.Fatalf("unexpected analyzed path: %s", file.Path)
				}
				mergedScores[file.Path] = file.Score
			}
		}
		if phase == "warm" {
			if backendCalls.Load() != 0 {
				t.Fatal("unchanged warm run invoked a backend")
			}
			if len(baseline) != len(document.Files) {
				t.Fatal("warm file count differs")
			}
			for i, file := range document.Files {
				if file.Path != baseline[i].Path || file.Score != baseline[i].Score {
					t.Fatal("warm scores differ")
				}
			}
		}
		if phase == "edit" {
			for _, path := range replacements {
				delete(mergedScores, path)
			}
			for _, file := range document.Files {
				mergedScores[file.Path] = file.Score
			}
		}
		if phase == "fresh" {
			if len(mergedScores) != len(document.Files) {
				t.Fatal("incremental/fresh inventories differ")
			}
			for _, file := range document.Files {
				if score, ok := mergedScores[file.Path]; !ok || score != file.Score {
					t.Fatalf("incremental/fresh score differs: %s", file.Path)
				}
			}
		}
	}
	t.Logf("profiles: %s; source sample excludes build configuration and dependencies; heap allocation profiles are cumulative", output)
}
