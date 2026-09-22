package follow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
	tea "github.com/charmbracelet/bubbletea"
)

func TestMonitorDeliversOutsideTargetProductionContext(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncached", true: "cached"}[cached], func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				".gitignore":         "",
				"go.mod":             "module example\n\ngo 1.24\n",
				"app/main.go":        "package app\nimport \"example/specs/lib\"\nimport _ \"example/newdep/sub\"\nfunc Value(x int) int { return lib.Value(x) }\n",
				"specs/lib/lib.go":   "package lib\nfunc Value(x int) int { return x+1 }\n",
				"unrelated/other.go": "package unrelated\nfunc Other() int { return 9 }\n",
			}
			write := func(workspace, path, contents string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(workspace, path)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(workspace, path), []byte(contents), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for path, data := range files {
				write(root, path, data)
			}
			watcher, err := newSourceWatcher(root, []string{"app/main.go"}, false, false, []string{"go"})
			if err != nil {
				t.Fatal(err)
			}
			defer watcher.close()
			if err := watcher.start(); err != nil {
				t.Fatal(err)
			}
			options := native.Options{Targets: []string{"app/main.go"}, Languages: []string{"go"}, ReadCache: cached}
			analyzer, err := native.New(root, nativeRefreshInstallationRoot(t), options)
			if err != nil {
				t.Fatal(err)
			}
			if cached {
				analyzer.EnableCache(filepath.Join(t.TempDir(), "cache"))
			}
			// Exercise production watcher-ready wiring after the rules have changed.
			// The captured startup policy still admits the explicitly selected app.
			write(root, ".gitignore", "app/main.go\n")
			model := refreshModel(root, report.Document{}, analyzer)
			model.options.Targets = options.Targets
			model.watcher = watcher
			_, startup := handleWatcherReady(&model, watcherReady{watcher: watcher})
			command := startup()
			var initial analysisResult
			if batch, ok := command.(tea.BatchMsg); ok {
				initial = batch[len(batch)-1]().(analysisResult)
			} else {
				initial = command.(analysisResult)
			}
			if initial.err != nil || len(initial.document.Files) != 1 || initial.document.Files[0].Path != "app/main.go" {
				t.Fatalf("production startup lost captured policy: %+v", initial)
			}
			type mutation struct {
				expected []string
				apply    func()
			}
			updates := []mutation{}
			add := func(path, data string) {
				updates = append(updates, mutation{[]string{path}, func() { files[path] = data; write(root, path, data) }})
			}
			add("specs/lib/lib.go", "package lib\nfunc Value(x int) int { if x<0 { return 0 };return x+2 }\n")
			add("specs/lib/new.go", "package lib\nfunc Extra() int { return 3 }\n")
			add("newdep/sub/a.go", "package sub\nfunc Added() int { return 1 }\n")
			updates = append(updates, mutation{[]string{"newdep/sub/a.go"}, func() {
				delete(files, "newdep/sub/a.go")
				if err := os.RemoveAll(filepath.Join(root, "newdep")); err != nil {
					t.Fatal(err)
				}
			}})
			add("newdep/sub/b.go", "package sub\nfunc Recreated() int { return 2 }\n")
			updates = append(updates, mutation{[]string{"newdep/sub/b.go", "newdep/sub/c.go"}, func() {
				outside := t.TempDir()
				write(outside, "replacement/sub/c.go", "package sub\nfunc Replaced() int { return 3 }\n")
				if err := os.Rename(filepath.Join(root, "newdep"), filepath.Join(outside, "old")); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Join(outside, "replacement"), filepath.Join(root, "newdep")); err != nil {
					t.Fatal(err)
				}
				delete(files, "newdep/sub/b.go")
				files["newdep/sub/c.go"] = "package sub\nfunc Replaced() int { return 3 }\n"
			}})
			for _, step := range updates {
				step.apply()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				found := false
				remaining := map[string]bool{}
				for _, path := range step.expected {
					remaining[path] = true
				}
				var paths []string
				for !found {
					batch, err := watcher.monitor.WaitAndDrain(ctx)
					if err != nil {
						cancel()
						t.Fatal(err)
					}
					for _, entry := range batch.Entries {
						paths = append(paths, entry.Path)
						delete(remaining, entry.Path)
						found = len(remaining) == 0
					}
				}
				cancel()
				got, replaced, err := analyzer.AnalyzeChanges(context.Background(), paths)
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Files) != 1 || got.Files[0].Path != "app/main.go" || len(replaced) != 1 || replaced[0] != "app/main.go" {
					t.Fatalf("outside-target context leaked into report: files=%v replaced=%v", got.Files, replaced)
				}
				referenceRoot := t.TempDir()
				for path, data := range files {
					write(referenceRoot, path, data)
				}
				reference, err := native.New(referenceRoot, nativeRefreshInstallationRoot(t), options)
				if err != nil {
					t.Fatal(err)
				}
				if cached {
					reference.EnableCache(filepath.Join(t.TempDir(), "reference-cache"))
				}
				want, err := reference.Analyze(context.Background(), nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(want.Files) != 1 || got.Files[0].Score != want.Files[0].Score {
					t.Fatalf("incremental score %v differs from independent reference %v", got.Files, want.Files)
				}
			}
		})
	}
}

func TestMonitorSymlinkReplacementUsesNativeLogicalPathPolicy(t *testing.T) {
	for _, directory := range []bool{false, true} {
		for _, policy := range []string{"disabled", "enabled", "explicit"} {
			t.Run(fmt.Sprintf("directory=%t/%s", directory, policy), func(t *testing.T) {
				root, outside := t.TempDir(), t.TempDir()
				path, entry := "a.go", "a.go"
				if directory {
					path, entry = "branch/a.go", "branch"
				}
				write := func(base, path, data string) {
					t.Helper()
					if err := os.MkdirAll(filepath.Dir(filepath.Join(base, path)), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(base, path), []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				}
				original := "package a\nfunc Value(x int) int { return x+1 }\n"
				write(root, "go.mod", "module example\n\ngo 1.24\n")
				write(root, path, original)
				targets := []string{"."}
				if policy == "explicit" {
					targets = []string{entry}
				}
				following := policy == "enabled"
				watcher, err := newSourceWatcher(root, targets, false, following, []string{"go"})
				if err != nil {
					t.Fatal(err)
				}
				defer watcher.close()
				if err := watcher.start(); err != nil {
					t.Fatal(err)
				}
				analyzer, err := native.New(root, nativeRefreshInstallationRoot(t), native.Options{Targets: targets, Languages: []string{"go"}, FollowSymlinks: following, ReadCache: true})
				if err != nil {
					t.Fatal(err)
				}
				analyzer.EnableCache(filepath.Join(t.TempDir(), "cache"))
				initial, err := analyzer.AnalyzeStartup(native.WithFollowMatcher(context.Background(), watcher.matcher), nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(initial.Files) != 1 {
					t.Fatalf("initial files=%v", initial.Files)
				}
				// File replacement deliberately keeps identical bytes: policy exclusion
				// must remove the row before the unchanged-digest fast path can retain it.
				replacement := original
				if directory {
					replacement = "package a\nfunc Value(x int) int { if x<0 { return 0 }; return x+2 }\n"
				}
				write(outside, "replacement/a.go", replacement)
				if err := os.Rename(filepath.Join(root, entry), filepath.Join(outside, "old")); err != nil {
					t.Fatal(err)
				}
				destination := filepath.Join(outside, "replacement", "a.go")
				if directory {
					destination = filepath.Join(outside, "replacement")
				}
				if err := os.Symlink(destination, filepath.Join(root, entry)); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				paths := []string{}
				found := false
				for !found {
					batch, err := watcher.monitor.WaitAndDrain(ctx)
					if err != nil {
						t.Fatal(err)
					}
					for _, item := range batch.Entries {
						paths = append(paths, item.Path)
						if item.Path == path {
							found = true
						}
					}
				}
				changed, replaced, err := analyzer.AnalyzeChanges(context.Background(), paths)
				if err != nil {
					t.Fatal(err)
				}
				rows := map[string]report.File{}
				for _, file := range initial.Files {
					rows[file.Path] = file
				}
				for _, path := range replaced {
					delete(rows, path)
				}
				for _, file := range changed.Files {
					rows[file.Path] = file
				}
				if policy == "disabled" {
					if len(rows) != 0 || len(replaced) != 1 || replaced[0] != path {
						t.Fatalf("excluded replacement retained row: rows=%v replaced=%v", rows, replaced)
					}
					// A duplicate removal notification must not enter a physical-existence retry loop.
					if _, _, err := analyzer.AnalyzeChanges(context.Background(), []string{path}); err != nil {
						t.Fatal(err)
					}
				} else {
					if len(rows) != 1 {
						t.Fatalf("authorized replacement removed row: %v", rows)
					}
					if directory && len(changed.Files) != 1 {
						t.Fatalf("authorized directory replacement not analyzed: %v", changed.Files)
					}
				}
			})
		}
	}
}
