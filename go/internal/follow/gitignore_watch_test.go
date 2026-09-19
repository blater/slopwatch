package follow

import (
	"context"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceignore"
	workspacefs "github.com/blater/slopwatch/internal/workspace"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func watchWrite(t *testing.T, root, path, data string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

func watchResult(t *testing.T, watcher *sourceWatcher, act func()) sourceChange {
	t.Helper()
	result := make(chan sourceChange, 1)
	go func() { result <- watcher.wait() }()
	act()
	select {
	case change := <-result:
		if change.Err != nil {
			t.Fatal(change.Err)
		}
		return change
	case <-time.After(5 * time.Second):
		watcher.close()
		t.Fatal("watch event did not arrive")
		return sourceChange{}
	}
}

func TestIgnoreRuleCreateChangeDeleteRebuildsWatchInventory(t *testing.T) {
	root := t.TempDir()
	watchWrite(t, root, ".git", "gitdir: unused")
	watchWrite(t, root, "nested/blocked/real.go", "package p")
	watcher, err := newSourceWatcher(root, []string{"."}, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { watcher.close() }()
	if err := watcher.start(); err != nil {
		t.Fatal(err)
	}
	change := watchResult(t, watcher, func() { watchWrite(t, root, "nested/.gitignore", "blocked/\n") })
	if !change.Full || !change.IgnoreRules {
		t.Fatalf("create=%+v", change)
	}
	watcher.close()
	watcher, err = newSourceWatcher(root, []string{"."}, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.start(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := watcher.eligible(filepath.Join(root, "nested/blocked/real.go")); ok {
		t.Fatal("ignored file eligible")
	}
	change = watchResult(t, watcher, func() { watchWrite(t, root, "nested/.gitignore", "blocked/\n!blocked/real.go\n") })
	if !change.IgnoreRules {
		t.Fatal("rule edit not detected")
	}
	change = watchResult(t, watcher, func() {
		if err := os.Remove(filepath.Join(root, "nested/.gitignore")); err != nil {
			t.Fatal(err)
		}
	})
	if !change.IgnoreRules {
		t.Fatal("rule deletion not detected")
	}
	watcher.close()
	watcher, err = newSourceWatcher(root, []string{"."}, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := watcher.start(); err != nil {
		t.Fatal(err)
	}
	change = watchResult(t, watcher, func() { watchWrite(t, root, "nested/blocked/real.go", "package changed") })
	if len(change.Paths) != 1 || change.Paths[0] != "nested/blocked/real.go" {
		t.Fatalf("restored directory not watched: %+v", change)
	}
}

func TestAncestorIgnoreCreationRefreshesExplicitFileScope(t *testing.T) {
	root := t.TempDir()
	watchWrite(t, root, ".git", "gitdir: unused")
	watchWrite(t, root, "sub/file.go", "package p")
	watcher, err := newSourceWatcher(filepath.Join(root, "sub"), []string{"file.go"}, true, false, []string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if err := watcher.start(); err != nil {
		t.Fatal(err)
	}
	change := watchResult(t, watcher, func() { watchWrite(t, root, ".gitignore", "file.go\n") })
	if !change.Full || !change.IgnoreRules {
		t.Fatalf("ancestor creation=%+v", change)
	}
}

func TestMissingNestedIgnoreForExplicitFileBecomesConfigurationInput(t *testing.T) {
	root := t.TempDir()
	watchWrite(t, root, ".git", "gitdir: unused")
	watchWrite(t, root, "deep/sub/file.go", "package p")
	watcher, err := newSourceWatcher(root, []string{"deep/sub/file.go"}, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.close()
	if err := watcher.start(); err != nil {
		t.Fatal(err)
	}
	change := watchResult(t, watcher, func() { watchWrite(t, root, "deep/.gitignore", "sub/*.go\n") })
	if !change.Full || !change.IgnoreRules {
		t.Fatalf("nested ancestor creation=%+v", change)
	}
}

func TestRealRuleEventReplacesModelWatcherAndAdmitsDirectoryEdits(t *testing.T) {
	model := settingsRefreshFixture(t)
	root := model.options.Workspace
	watchWrite(t, root, "blocked/real.go", "package p")
	watchWrite(t, root, ".gitignore", "blocked/\n")
	// Install the initial policy after fixture creation.
	initial := model.refreshIgnorePolicy()().(watcherReconfigured)
	executeReplacementAnalysis(t, model, model.handleWatcherReconfigured(initial))
	analyzer := model.analyzer.(*refreshAnalyzer)
	analyzer.document = report.Document{Files: []report.File{testFile("a.go", 1), testFile("blocked/real.go", 3)}}
	old := model.watcher
	change := watchResult(t, old, func() {
		if err := os.Remove(filepath.Join(root, ".gitignore")); err != nil {
			t.Fatal(err)
		}
	})
	if !change.IgnoreRules {
		t.Fatal("real event lost ignore classification")
	}
	_, replacement := handleSourceChange(model, change)
	if replacement == nil || !model.watchNeedsWait {
		t.Fatal("real event did not consume wait and request replacement")
	}
	executeReplacementAnalysis(t, model, model.handleWatcherReconfigured(replacement().(watcherReconfigured)))
	if model.watcher == old || len(model.files.Document.Files) != 2 {
		t.Fatal("rule event did not refresh inventory and watcher")
	}
	edit := watchResult(t, model.watcher, func() { watchWrite(t, root, "blocked/real.go", "package edited") })
	if len(edit.Paths) != 1 || edit.Paths[0] != "blocked/real.go" {
		t.Fatalf("admitted directory edit=%+v", edit)
	}
	_, command := handleSourceChange(model, edit)
	if command == nil || !model.analyzing {
		t.Fatal("newly admitted edit did not start analysis")
	}
}

func TestFrequentReadySourceBatchesCannotStarveAncestorRulePolling(t *testing.T) {
	root := t.TempDir()
	rulePath := filepath.Join(root, ".gitignore")
	watcher := &sourceWatcher{ancestorRules: map[string]string{rulePath: sourceignore.InputFingerprint(rulePath)}}
	batch := workspacefs.DirtyBatch{Entries: []workspacefs.DirtyEntry{{Path: "live.go", Kind: workspacefs.KindSource}}}
	ready := func(context.Context) (workspacefs.DirtyBatch, error) { return batch, nil }
	for index := 0; index < 8; index++ {
		if index == 4 {
			watchWrite(t, root, ".gitignore", "ignored/\n")
		}
		// Every batch is ready immediately, and the hour-long timer cannot assist
		// detection: this exercises the starvation path without timing assumptions.
		got, err, changed := watcher.waitBatchFrom(ready, time.Hour)
		if err != nil || len(got.Entries) != 1 || got.Entries[0].Path != "live.go" {
			t.Fatalf("source batch lost: %+v %v", got, err)
		}
		if changed != (index == 4) {
			t.Fatalf("batch %d ancestor change=%v", index, changed)
		}
	}
}
