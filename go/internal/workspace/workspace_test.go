package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	events chan Event
	errors chan error
	mu     sync.Mutex
	added  []string
	closed bool
}

type existingPathBackend struct{ *fakeBackend }

func (backend *existingPathBackend) Add(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("watch path is not a directory")
	}
	return backend.fakeBackend.Add(path)
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{events: make(chan Event, 32), errors: make(chan error, 8)}
}
func (f *fakeBackend) Add(path string) error {
	f.mu.Lock()
	f.added = append(f.added, filepath.Clean(path))
	f.mu.Unlock()
	return nil
}
func (f *fakeBackend) Events() <-chan Event { return f.events }
func (f *fakeBackend) Errors() <-chan error { return f.errors }
func (f *fakeBackend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.events)
		close(f.errors)
	}
	return nil
}

func sourceClassifier(path string, dir bool) (Classification, bool) {
	if dir {
		return Classification{}, false
	}
	switch filepath.Ext(path) {
	case ".go":
		return Classification{Kind: KindSource, Language: "go"}, true
	case ".java":
		return Classification{Kind: KindSource, Language: "java"}, true
	case ".rs":
		return Classification{Kind: KindSource, Language: "rust"}, true
	case ".ts", ".tsx":
		return Classification{Kind: KindSource, Language: "typescript"}, true
	default:
		return Classification{}, false
	}
}

func newTestMonitor(t *testing.T, root string, backend *fakeBackend, extra Config) *Monitor {
	t.Helper()
	extra.Root = root
	if extra.Classifier == nil {
		extra.Classifier = ClassifierFunc(sourceClassifier)
	}
	if extra.BackendFactory == nil {
		extra.BackendFactory = func() (Backend, error) { return backend, nil }
	}
	if extra.Debounce == 0 {
		extra.Debounce = time.Millisecond
	}
	m, err := New(extra)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func receiveBatch(t *testing.T, m *Monitor) DirtyBatch {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	batch, err := m.WaitAndDrain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestMonitorDurableDirtySetCoalescesWakeWithoutDroppingPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{Scopes: []Scope{{Path: "src", Recursive: true}}})

	backend.events <- Event{Name: "src/a.go", Op: OpWrite}
	backend.events <- Event{Name: "src/b.java", Op: OpWrite}
	batch := receiveBatch(t, m)
	if batch.All || len(batch.Entries) != 2 {
		t.Fatalf("expected two path entries, got %#v", batch)
	}
	paths := []string{batch.Entries[0].Path, batch.Entries[1].Path}
	sort.Strings(paths)
	if want := []string{"src/a.go", "src/b.java"}; !equalStrings(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}

	// Wake is a hint, not the source of truth: even when it is consumed once,
	// another event remains recoverable from the dirty set.
	backend.events <- Event{Name: "src/a.go", Op: OpWrite}
	backend.events <- Event{Name: "src/a.go", Op: OpRename}
	batch = receiveBatch(t, m)
	if len(batch.Entries) != 1 || batch.Entries[0].Reasons&(ReasonWrite|ReasonRename) != (ReasonWrite|ReasonRename) {
		t.Fatalf("merged reasons = %#v", batch)
	}
}

func TestMonitorPreservesExactFileScopeAndConfiguredInputs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"selected.go", "sibling.go", "package.json"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{
		Scopes: []Scope{{Path: "selected.go", Recursive: false}},
		Inputs: []Input{{Path: "package.json", Kind: KindConfiguration}},
	})
	backend.events <- Event{Name: "sibling.go", Op: OpWrite}
	backend.events <- Event{Name: "selected.go", Op: OpWrite}
	backend.events <- Event{Name: "package.json", Op: OpWrite}
	batch := receiveBatch(t, m)
	if len(batch.Entries) != 2 {
		t.Fatalf("entries = %#v, want selected source and config", batch)
	}
	got := map[string]Kind{}
	for _, entry := range batch.Entries {
		got[entry.Path] = entry.Kind
	}
	if got["selected.go"] != KindSource || got["package.json"] != KindConfiguration {
		t.Fatalf("kinds = %#v", got)
	}
	if _, ok := got["sibling.go"]; ok {
		t.Fatal("exact file scope admitted sibling")
	}
}

func TestMonitorTracksMissingNestedInputsThroughExistingAncestors(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	base := newFakeBackend()
	backend := &existingPathBackend{fakeBackend: base}
	m, err := New(Config{
		Root:   root,
		Scopes: []Scope{{Path: project, Recursive: true, Kind: KindSource, Directory: true}},
		Inputs: []Input{
			{Path: ".cargo/config.toml", Kind: KindConfiguration},
			{Path: ".mvn/maven.config", Kind: KindConfiguration},
			{Path: "gradle/wrapper/gradle-wrapper.properties", Kind: KindConfiguration},
		},
		Classifier:     ClassifierFunc(sourceClassifier),
		BackendFactory: func() (Backend, error) { return backend, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("monitor rejected an absent optional input: %v", err)
	}
	defer m.Close()

	cargoDirectory := filepath.Join(root, ".cargo")
	if err := os.Mkdir(cargoDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	m.handle(Event{Name: cargoDirectory, Op: OpCreate, IsDir: true})
	if !m.isWatched(cargoDirectory) {
		t.Fatal("new intermediate directory did not extend the watch chain")
	}
	gradleDirectory := filepath.Join(root, "gradle")
	if err := os.Mkdir(gradleDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	m.handle(Event{Name: gradleDirectory, Op: OpCreate, IsDir: true})
	wrapperDirectory := filepath.Join(gradleDirectory, "wrapper")
	if err := os.Mkdir(wrapperDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	m.handle(Event{Name: wrapperDirectory, Op: OpCreate, IsDir: true})
	if !m.isWatched(wrapperDirectory) {
		t.Fatal("multi-level optional input did not extend the watch chain")
	}
	config := filepath.Join(cargoDirectory, "config.toml")
	if err := os.WriteFile(config, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m.handle(Event{Name: config, Op: OpCreate})
	batch := m.Drain()
	if len(batch.Entries) != 1 || batch.Entries[0].Path != ".cargo/config.toml" || batch.Entries[0].Kind != KindConfiguration {
		t.Fatalf("created optional input was not tracked: %#v", batch)
	}
}

func TestMonitorFollowsExplicitSymlinkScopeButNotNestedSymlinksByDefault(t *testing.T) {
	root := t.TempDir()
	project := t.TempDir()
	ordinary := filepath.Join(project, "ordinary")
	nestedTarget := t.TempDir()
	if err := os.Mkdir(ordinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nestedTarget, filepath.Join(project, "nested")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	logicalTarget := filepath.Join(root, "project")
	if err := os.Symlink(project, logicalTarget); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{
		Scopes: []Scope{{Path: logicalTarget, Recursive: true, Kind: KindSource, Directory: true}},
	})
	if !m.isWatched(logicalTarget) || !m.isWatched(filepath.Join(logicalTarget, "ordinary")) {
		t.Fatalf("explicit symlink scope was not registered: %v", backend.added)
	}
	if m.isWatched(filepath.Join(logicalTarget, "nested")) {
		t.Fatal("nested symlink was followed without --follow-symlinks")
	}
}

func TestMonitorCanFollowNestedSymlinksWithoutCycling(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	nestedTarget := t.TempDir()
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nestedTarget, filepath.Join(project, "nested")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(project, filepath.Join(project, "cycle")); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{
		Scopes:         []Scope{{Path: project, Recursive: true, Kind: KindSource, Directory: true}},
		FollowSymlinks: true,
	})
	if !m.isWatched(filepath.Join(project, "nested")) {
		t.Fatalf("nested symlink was not registered: %v", backend.added)
	}
}

func TestClassifierCanMarkConfigurationInsideSourceScope(t *testing.T) {
	root := t.TempDir()
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{
		Scopes: []Scope{{Path: root, Recursive: true, Kind: KindSource, Directory: true}},
		Classifier: ClassifierFunc(func(path string, directory bool) (Classification, bool) {
			if directory {
				return Classification{}, false
			}
			if path == "go.mod" {
				return Classification{Kind: KindConfiguration, Language: "go"}, true
			}
			return Classification{Kind: KindSource, Language: "go"}, true
		}),
	})
	backend.events <- Event{Name: "go.mod", Op: OpWrite}
	batch := receiveBatch(t, m)
	if len(batch.Entries) != 1 || batch.Entries[0].Kind != KindConfiguration {
		t.Fatalf("configuration event = %#v", batch)
	}
}

func TestMonitorNewDirectoryRegistersTreeAndRequestsFullAudit(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{Scopes: []Scope{{Path: "src", Recursive: true}}})
	newDir := filepath.Join(root, "src", "new")
	if err := os.Mkdir(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "new.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	backend.events <- Event{Name: newDir, Op: OpCreate}
	batch := receiveBatch(t, m)
	if !batch.All || batch.Reasons&(ReasonDirectory|ReasonCreate) != (ReasonDirectory|ReasonCreate) {
		t.Fatalf("directory event batch = %#v", batch)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	found := false
	for _, path := range backend.added {
		if path == newDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("new directory was not registered: %v", backend.added)
	}
}

func TestMonitorWatcherErrorForcesDirtyAll(t *testing.T) {
	root := t.TempDir()
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{})
	backend.errors <- errors.New("queue overflow")
	batch := receiveBatch(t, m)
	if !batch.All || batch.Reasons&ReasonWatcherError == 0 {
		t.Fatalf("error batch = %#v", batch)
	}
}

func TestMonitorStartupReconcileIsAsyncAndKeepsReturnedPaths(t *testing.T) {
	root := t.TempDir()
	backend := newFakeBackend()
	started := make(chan struct{})
	finish := make(chan struct{})
	m := newTestMonitor(t, root, backend, Config{Reconcile: func(ctx context.Context, request ReconcileRequest) ([]string, error) {
		close(started)
		<-finish
		return []string{"src/a.go"}, nil
	}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("startup reconciliation did not begin")
	}
	backend.events <- Event{Name: "src/a.go", Op: OpWrite}
	close(finish)
	batch := receiveBatch(t, m)
	if len(batch.Entries) != 1 || batch.Entries[0].Path != "src/a.go" || batch.Entries[0].Reasons&(ReasonWrite|ReasonStartupAudit) != (ReasonWrite|ReasonStartupAudit) {
		t.Fatalf("reconciled batch = %#v", batch)
	}
}

func TestMonitorReconcileErrorRequestsFullAudit(t *testing.T) {
	root := t.TempDir()
	backend := newFakeBackend()
	m := newTestMonitor(t, root, backend, Config{Reconcile: func(context.Context, ReconcileRequest) ([]string, error) {
		return nil, errors.New("inventory unavailable")
	}})
	batch := receiveBatch(t, m)
	if !batch.All || batch.Reasons&(ReasonWatcherError|ReasonStartupAudit) != (ReasonWatcherError|ReasonStartupAudit) {
		t.Fatalf("reconcile error batch = %#v", batch)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
