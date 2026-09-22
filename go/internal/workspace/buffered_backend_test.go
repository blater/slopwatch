package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/sourcefs"
)

func TestIntakeRetainsPathsAndCoalescesRepetitions(t *testing.T) {
	b := &bufferedBackend{pending: map[string]Event{}, wake: make(chan struct{}, 1), filter: func(e Event) bool { return e.Name != "ignored" }}
	const paths = 16384 // Exceeds the removed 8192-path limit.
	for i := 0; i < paths; i++ {
		b.admit(Event{Name: fmt.Sprint(i), Op: OpCreate})
	}
	for i := 0; i < 100; i++ {
		b.admit(Event{Name: "0", Op: OpWrite})
		b.admit(Event{Name: "ignored", Op: OpWrite})
	}
	if b.loss || len(b.pending) != paths || b.pending["0"].Op != OpCreate|OpWrite {
		t.Fatal("paths lost, repetitions not merged, or exclusions retained")
	}
	for i := 0; i < paths; i++ {
		if _, ok := b.pending[fmt.Sprint(i)]; !ok {
			t.Fatalf("pending path %d was discarded", i)
		}
	}
}

type startupEventFS struct {
	sourcefs.OS
	backend *fakeBackend
	root    string
	sent    bool
}

func (fs *startupEventFS) ReadDir(path string) ([]os.DirEntry, error) {
	entries, err := fs.OS.ReadDir(path)
	if !fs.sent {
		fs.sent = true
		source := filepath.Join(fs.root, "late.go")
		if writeErr := os.WriteFile(source, []byte("package late\n"), 0600); writeErr != nil {
			return nil, writeErr
		}
		fs.backend.events <- Event{Name: source, Op: OpCreate}
	}
	return entries, err
}
func TestStartupIntakeRetainsChangeDuringTraversal(t *testing.T) {
	root := t.TempDir()
	backend := newFakeBackend()
	fs := &startupEventFS{backend: backend, root: root}
	m := newTestMonitor(t, root, backend, Config{FileSystem: fs})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	batch := receiveBatch(t, m)
	assertDirtyPaths(t, batch, "late.go")
}

func TestExplicitRescanRebuildsInventoryAndRetainsConcurrentEvents(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.go")
	if err := os.WriteFile(old, []byte("package old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(root, "new", "added.go")
	if err := os.MkdirAll(filepath.Dir(added), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(added, []byte("package added\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Rescan(context.Background(), func() error { backend.events <- Event{Name: added, Op: OpWrite}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.engine.watch.known[old]; ok {
		t.Fatal("missed deletion retained")
	}
	if _, ok := m.engine.watch.known[added]; !ok {
		t.Fatal("missed creation absent")
	}
	batch := receiveBatch(t, m)
	assertDirtyPaths(t, batch, "new/added.go")
	if err := os.RemoveAll(filepath.Dir(added)); err != nil {
		t.Fatal(err)
	}
	m.handle(Event{Name: filepath.Dir(added), Op: OpRemove, IsDir: true})
	if _, ok := m.engine.watch.known[added]; ok {
		t.Fatal("rescan lost directory child links")
	}
}

func TestLossNoticeDoesNotStopLaterChanges(t *testing.T) {
	root := t.TempDir()
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	backend.errors <- ErrNotificationLoss
	batch := receiveBatch(t, m)
	if batch.LossCount == 0 || batch.Err != nil || batch.Closed || batch.All {
		t.Fatalf("loss interrupted monitoring: %+v", batch)
	}
	path := filepath.Join(root, "after.go")
	if err := os.WriteFile(path, []byte("package after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	backend.events <- Event{Name: path, Op: OpCreate}
	assertDirtyPaths(t, receiveBatch(t, m), "after.go")
}
