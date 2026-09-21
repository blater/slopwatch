package follow

import (
	"context"
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
	if !change.IgnoreRules {
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
	if !change.IgnoreRules {
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
	if !change.IgnoreRules {
		t.Fatalf("nested ancestor creation=%+v", change)
	}
}

func TestRuleEventPreservesWatcherPolicyAndSimultaneousSource(t *testing.T) {
	model := settingsRefreshFixture(t)
	old := model.watcher
	if err := old.start(); err != nil {
		t.Fatal(err)
	}
	change := watchResult(t, old, func() {
		watchWrite(t, model.options.Workspace, ".gitignore", "a.go\n")
		watchWrite(t, model.options.Workspace, "a.go", "package edited")
	})
	if !change.IgnoreRules || len(change.Paths) != 1 || change.Paths[0] != "a.go" {
		t.Fatalf("combined batch=%+v", change)
	}
	_, command := handleSourceChange(model, change)
	if command == nil || model.watcher != old || !model.analyzing || len(model.files.Document.Files) != 1 {
		t.Fatal("rule edit lost source work or changed session")
	}
	if _, _, ok := old.eligible(filepath.Join(model.options.Workspace, "a.go")); !ok {
		t.Fatal("edited rules changed frozen eligibility")
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

func TestAncestorPollingCancellationRetainsAlreadyDrainedSourceBatch(t *testing.T) {
	root := t.TempDir()
	rulePath := filepath.Join(root, ".gitignore")
	watcher := &sourceWatcher{ancestorRules: map[string]string{rulePath: sourceignore.InputFingerprint(rulePath)}}
	watchWrite(t, root, ".gitignore", "ignored/\n")
	batch := workspacefs.DirtyBatch{Entries: []workspacefs.DirtyEntry{{Path: "live.go", Kind: workspacefs.KindSource}}}
	release := make(chan struct{})
	defer close(release)
	waiter := func(ctx context.Context) (workspacefs.DirtyBatch, error) {
		// The batch has been drained, but delivery races the ancestor poll.
		select {
		case <-ctx.Done():
			return batch, ctx.Err()
		case <-release:
			return batch, nil
		}
	}
	type result struct {
		batch   workspacefs.DirtyBatch
		err     error
		changed bool
	}
	ready := make(chan result, 1)
	go func() {
		got, err, changed := watcher.waitBatchFrom(waiter, time.Millisecond)
		ready <- result{got, err, changed}
	}()
	select {
	case got := <-ready:
		if got.err != nil || !got.changed || len(got.batch.Entries) != 1 || got.batch.Entries[0].Path != "live.go" {
			t.Fatalf("poll lost source batch: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("poll did not cancel pending waiter")
	}
}
