package workspace

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func writeDirectoryFile(t *testing.T, root, path string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package p"), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertDirtyPaths(t *testing.T, batch DirtyBatch, paths ...string) {
	t.Helper()
	got := []string{}
	for _, entry := range batch.Entries {
		got = append(got, entry.Path)
	}
	sort.Strings(paths)
	if batch.All || batch.Err != nil || !reflect.DeepEqual(got, paths) {
		t.Fatalf("batch=%+v want=%v", batch, paths)
	}
}

func TestDirectoryMoveRetainsOnlyEligibleSourcesAndRemovesDescendantWatches(t *testing.T) {
	root := t.TempDir()
	writeDirectoryFile(t, root, "old/sub/a.go")
	writeDirectoryFile(t, root, "old/readme.txt")
	writeDirectoryFile(t, root, "other/b.go")
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{})
	if err := os.Rename(filepath.Join(root, "old"), filepath.Join(root, "new")); err != nil {
		t.Fatal(err)
	}
	// Real fsnotify does not populate IsDir for deletion/rename.
	m.handle(Event{Name: "old", Op: OpRename})
	assertDirtyPaths(t, m.Drain(), "old/sub/a.go")
	if m.isWatched(filepath.Join(root, "old")) || m.isWatched(filepath.Join(root, "old/sub")) || !m.isWatched(filepath.Join(root, "other")) {
		t.Fatal("incorrect descendant watch cleanup")
	}
	backend.mu.Lock()
	removed := len(backend.removed)
	backend.mu.Unlock()
	if removed != 2 {
		t.Fatalf("removed backend watches=%d", removed)
	}
	m.handle(Event{Name: "new", Op: OpCreate})
	assertDirtyPaths(t, m.Drain(), "new/sub/a.go")
	if !m.isWatched(filepath.Join(root, "new/sub")) {
		t.Fatal("moved subtree not registered")
	}
	if err := os.RemoveAll(filepath.Join(root, "new")); err != nil {
		t.Fatal(err)
	}
	m.handle(Event{Name: "new", Op: OpRemove})
	assertDirtyPaths(t, m.Drain(), "new/sub/a.go")
}

func TestEmptyIgnoredAndOutOfScopeDirectoriesDoNotScheduleAnalysis(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		t.Run(map[bool]string{false: "nonrecursive", true: "recursive"}[recursive], func(t *testing.T) {
			root := t.TempDir()
			backend := newFakeBackend()
			m := newTestMonitor(t, root, backend, Config{Scopes: []Scope{{Path: root, Directory: true, Recursive: recursive}}, IgnoreDirectory: func(_, name string) bool { return name == "ignored" }})
			if err := os.Mkdir(filepath.Join(root, "empty"), 0755); err != nil {
				t.Fatal(err)
			}
			m.handle(Event{Name: "empty", Op: OpCreate})
			if !m.Drain().Empty() || m.isWatched(filepath.Join(root, "empty")) != recursive {
				t.Fatal("empty directory work/scope incorrect")
			}
			writeDirectoryFile(t, root, "ignored/a.go")
			m.handle(Event{Name: "ignored", Op: OpCreate})
			m.handle(Event{Name: "ignored", Op: OpWrite})
			if !m.Drain().Empty() || m.isWatched(filepath.Join(root, "ignored")) {
				t.Fatal("ignored directory triggered work")
			}
			if !recursive {
				writeDirectoryFile(t, root, "nested/a.go")
				m.handle(Event{Name: "nested", Op: OpCreate})
				if !m.Drain().Empty() || m.isWatched(filepath.Join(root, "nested")) {
					t.Fatal("nonrecursive scope expanded")
				}
			}
		})
	}
}

func TestWatcherErrorAndClosureRetainPendingSourcePaths(t *testing.T) {
	root := t.TempDir()
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{})
	m.handle(Event{Name: "a.go", Op: OpWrite})
	failure := errors.New("watch registration failed")
	m.engine.events.markError(failure, false)
	batch := m.Drain()
	if batch.All || !errors.Is(batch.Err, failure) || len(batch.Entries) != 1 || batch.Entries[0].Path != "a.go" {
		t.Fatalf("error lost source batch: %+v", batch)
	}
	m.handle(Event{Name: "b.go", Op: OpWrite})
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-m.engine.events.stopped:
	case <-time.After(time.Second):
		t.Fatal("event loop did not terminate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	batch, err := m.WaitAndDrain(ctx)
	if !errors.Is(err, io.EOF) || len(batch.Entries) != 1 || batch.Entries[0].Path != "b.go" {
		t.Fatalf("closure batch=%+v err=%v", batch, err)
	}
	_, err = m.WaitAndDrain(ctx)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("closed monitor waited again: %v", err)
	}
}

type failingDirectoryBackend struct {
	*fakeBackend
	path    string
	failure error
}

func (backend *failingDirectoryBackend) Add(path string) error {
	if filepath.Clean(path) == backend.path {
		return backend.failure
	}
	return backend.fakeBackend.Add(path)
}

func TestDirectoryRegistrationFailureRetainsDiscoveredAndPendingSources(t *testing.T) {
	root := t.TempDir()
	failure := errors.New("cannot register new subtree")
	backend := &failingDirectoryBackend{fakeBackend: newFakeBackend(), path: filepath.Join(root, "new", "z-fails"), failure: failure}
	m := newTestMonitor(t, root, backend.fakeBackend, Config{BackendFactory: func() (Backend, error) { return backend, nil }})
	writeDirectoryFile(t, root, "new/a.go")
	writeDirectoryFile(t, root, "new/z-fails/b.go")
	m.handle(Event{Name: "pending.go", Op: OpWrite})
	m.handle(Event{Name: "new", Op: OpCreate})
	batch := m.Drain()
	paths := []string{}
	for _, entry := range batch.Entries {
		paths = append(paths, entry.Path)
	}
	if batch.All || !errors.Is(batch.Err, failure) || !reflect.DeepEqual(paths, []string{"new/a.go", "pending.go"}) {
		t.Fatalf("registration failure batch=%+v", batch)
	}
	if !m.isWatched(filepath.Join(root, "new")) || m.isWatched(backend.path) {
		t.Fatal("failed registration recorded an invalid watch")
	}
}
