// Package workspace owns the filesystem boundary used by analysis coordinators.
//
// A Monitor deliberately has two outputs: a durable dirty set (the authority)
// and Wake (a best-effort, coalesced notification). A caller must always drain
// the dirty set; a dropped Wake can never drop a path.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Kind describes why an input is watched. It is intentionally independent of
// language and analysis-unit concepts; those belong to the caller's planner.
type Kind uint8

const (
	KindSource Kind = iota
	KindDependency
	KindConfiguration
)

// Reason is a bit set so a burst of filesystem events produces one useful
// dirty entry while retaining all information needed by a coordinator.
type Reason uint16

const (
	ReasonWrite Reason = 1 << iota
	ReasonCreate
	ReasonRemove
	ReasonRename
	ReasonDirectory
	ReasonWatcherError
	ReasonStartupAudit
)

// Classification is supplied by the language/configuration layer. Path is a
// slash-separated path relative to Config.Root (or whatever root the caller
// chose); classifiers should not perform filesystem I/O.
type Classification struct {
	Kind     Kind
	Language string
}

// Classifier decides which source paths are analysis inputs. Explicit Inputs
// always take precedence, allowing build files and lockfiles to be tracked
// without putting language policy in this package.
type Classifier interface {
	Classify(relativePath string, isDirectory bool) (Classification, bool)
}

// ClassifierFunc adapts a function to Classifier.
type ClassifierFunc func(relativePath string, isDirectory bool) (Classification, bool)

func (f ClassifierFunc) Classify(path string, dir bool) (Classification, bool) { return f(path, dir) }

// Scope controls the exact target roots. A non-recursive scope contains only
// the target itself (for files) or direct children (for directories).
type Scope struct {
	Path      string
	Recursive bool
	Kind      Kind
	// Directory may be left false for a file scope. New infers it for an
	// existing directory; setting it explicitly also supports a not-yet-created
	// directory target.
	Directory bool
}

// Input is an explicitly watched dependency/configuration path. Recursive
// directories are useful for generated manifests or a build metadata tree.
type Input struct {
	Path      string
	Kind      Kind
	Recursive bool
	Directory bool
}

// ReconcileRequest is passed to the startup inventory hook. The hook may use
// Scopes and Inputs to enumerate current files and return paths that need
// validation. Filesystem events arriving during the hook remain authoritative.
type ReconcileRequest struct {
	Root   string
	Scopes []Scope
	Inputs []Input
	Full   bool
}

// ReconcileHook is intentionally small so an inventory implementation can use
// an existing fast stat/indexer. Returned paths are marked as startup-audit
// work; an error requests a conservative full audit.
type ReconcileHook func(context.Context, ReconcileRequest) ([]string, error)

// Event is the backend-neutral form of a filesystem notification.
type Event struct {
	Name string
	Op   Op
	// IsDir is optional for injected backends. The fsnotify adapter tracks
	// registered directories so real remove/rename events are classified too.
	IsDir bool
}

// Op mirrors the fsnotify operations needed by a workspace monitor.
type Op uint32

const (
	OpCreate Op = 1 << iota
	OpWrite
	OpRemove
	OpRename
)

// Backend is implemented by fsnotify and by deterministic test fakes.
type Backend interface {
	Add(path string) error
	Events() <-chan Event
	Errors() <-chan error
	Close() error
}

// BackendFactory permits tests to inject barriers/events without a real
// filesystem watcher. A nil factory selects fsnotify.
type BackendFactory func() (Backend, error)

// Config configures a Monitor.
type Config struct {
	Root   string
	Scopes []Scope
	Inputs []Input
	// DependencyPaths and ConfigurationPaths are convenience forms for
	// non-recursive explicit inputs. Inputs can be used when a directory needs
	// recursive tracking or when a caller needs a custom Kind.
	DependencyPaths    []string
	ConfigurationPaths []string
	Classifier         Classifier
	FollowSymlinks     bool

	// IgnoreDirectory is called during recursive registration. Returning true
	// prevents descending into that directory. It is not used for source
	// eligibility; keep that decision in Classifier.
	IgnoreDirectory func(path string, name string) bool

	Debounce       time.Duration
	BackendFactory BackendFactory
	Reconcile      ReconcileHook
}

// DirtyEntry is one authoritative pending input.
type DirtyEntry struct {
	Path    string // slash-separated, workspace-relative
	Kind    Kind
	Reasons Reason
}

// DirtyBatch is a snapshot removed atomically from the durable dirty set.
type DirtyBatch struct {
	Entries []DirtyEntry
	All     bool
	Reasons Reason
}

// Empty reports whether no work was pending in the batch.
func (b DirtyBatch) Empty() bool { return !b.All && len(b.Entries) == 0 }

type dirtySet struct {
	mu      sync.Mutex
	entries map[string]DirtyEntry
	all     bool
	reasons Reason
}

func (s *dirtySet) mark(entry DirtyEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.entries[entry.Path]; ok {
		old.Kind = entry.Kind
		old.Reasons |= entry.Reasons
		s.entries[entry.Path] = old
	} else {
		s.entries[entry.Path] = entry
	}
	return true
}

func (s *dirtySet) markAll(reason Reason) {
	s.mu.Lock()
	s.all = true
	s.reasons |= reason
	s.mu.Unlock()
}

func (s *dirtySet) snapshot() DirtyBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := DirtyBatch{All: s.all, Reasons: s.reasons}
	b.Entries = make([]DirtyEntry, 0, len(s.entries))
	for _, e := range s.entries {
		b.Entries = append(b.Entries, e)
	}
	s.entries = make(map[string]DirtyEntry)
	s.all, s.reasons = false, 0
	sort.Slice(b.Entries, func(i, j int) bool { return b.Entries[i].Path < b.Entries[j].Path })
	return b
}

func (s *dirtySet) pending() bool {
	s.mu.Lock()
	pending := s.all || len(s.entries) != 0
	s.mu.Unlock()
	return pending
}

// Monitor recursively watches configured scopes and maintains durable dirty
// state. Construct one with New, call Start, then consume Wake and Drain.
type Monitor struct {
	root           string
	scopes         []Scope
	inputs         []Input
	classifier     Classifier
	ignoreDir      func(string, string) bool
	followSymlinks bool
	debounce       time.Duration
	reconcile      ReconcileHook
	backend        Backend
	dirty          dirtySet
	wake           chan struct{}
	done           chan struct{}
	closeOnce      sync.Once
	startOnce      sync.Once
	started        chan struct{}
	startErr       error
	watchedMu      sync.RWMutex
	watched        map[string]struct{}
}

// New creates a monitor. It does not touch the filesystem or start goroutines.
func New(cfg Config) (*Monitor, error) {
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace root: %w", err)
	}
	root = filepath.Clean(root)
	if cfg.Debounce <= 0 {
		cfg.Debounce = 80 * time.Millisecond
	}
	factory := cfg.BackendFactory
	if factory == nil {
		factory = func() (Backend, error) {
			w, err := fsnotify.NewWatcher()
			if err != nil {
				return nil, err
			}
			backend := &fsnotifyBackend{watcher: w, events: make(chan Event), errors: make(chan error), done: make(chan struct{})}
			go backend.forward()
			return backend, nil
		}
	}
	backend, err := factory()
	if err != nil {
		return nil, err
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []Scope{{Path: root, Recursive: true, Kind: KindSource}}
	}
	m := &Monitor{
		root: root, classifier: cfg.Classifier, ignoreDir: cfg.IgnoreDirectory,
		followSymlinks: cfg.FollowSymlinks,
		debounce:       cfg.Debounce, reconcile: cfg.Reconcile, backend: backend,
		wake: make(chan struct{}, 1), done: make(chan struct{}), started: make(chan struct{}),
		watched: make(map[string]struct{}),
	}
	for _, scope := range cfg.Scopes {
		scope.Path = m.absolute(scope.Path)
		if !scope.Directory {
			if info, statErr := os.Stat(scope.Path); statErr == nil {
				scope.Directory = info.IsDir()
			}
		}
		m.scopes = append(m.scopes, scope)
	}
	for _, input := range cfg.Inputs {
		input.Path = m.absolute(input.Path)
		if !input.Directory {
			if info, statErr := os.Stat(input.Path); statErr == nil {
				input.Directory = info.IsDir()
			}
		}
		m.inputs = append(m.inputs, input)
	}
	for _, path := range cfg.DependencyPaths {
		m.inputs = append(m.inputs, Input{Path: m.absolute(path), Kind: KindDependency})
	}
	for _, path := range cfg.ConfigurationPaths {
		m.inputs = append(m.inputs, Input{Path: m.absolute(path), Kind: KindConfiguration})
	}
	m.dirty.entries = make(map[string]DirtyEntry)
	return m, nil
}

// Start registers all existing directories, then begins consuming backend
// events. Startup reconciliation runs asynchronously so it cannot delay cache
// publication. Events seen during reconciliation are retained in dirty state.
func (m *Monitor) Start(ctx context.Context) error {
	m.startOnce.Do(func() {
		defer close(m.started)
		m.startErr = m.registerAll()
		if m.startErr != nil {
			return
		}
		go m.run()
		if m.reconcile != nil {
			go m.startupReconcile(ctx)
		}
	})
	<-m.started
	return m.startErr
}

func (m *Monitor) startupReconcile(ctx context.Context) {
	_ = m.Reconcile(ctx, true)
}

// Reconcile invokes the configured inventory hook. It is safe to call after a
// watcher overflow or other full-audit request; events arriving concurrently
// remain in the same authoritative dirty set.
func (m *Monitor) Reconcile(ctx context.Context, full bool) error {
	if m.reconcile == nil {
		return nil
	}
	paths, err := m.reconcile(ctx, ReconcileRequest{Root: m.root, Scopes: m.scopesCopy(), Inputs: m.inputsCopy(), Full: full})
	if err != nil {
		m.markAll(ReasonWatcherError | ReasonStartupAudit)
		return err
	}
	for _, path := range paths {
		m.markPath(path, ReasonStartupAudit, false)
	}
	return nil
}

func (m *Monitor) scopesCopy() []Scope { return append([]Scope(nil), m.scopes...) }
func (m *Monitor) inputsCopy() []Input { return append([]Input(nil), m.inputs...) }

func (m *Monitor) registerAll() error {
	for _, scope := range m.scopes {
		if err := m.registerTarget(scope.Path, scope.Recursive); err != nil {
			return err
		}
	}
	for _, input := range m.inputs {
		if err := m.registerTarget(input.Path, input.Recursive); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) registerTarget(path string, recursive bool) error {
	info, err := os.Stat(path)
	if err != nil {
		// A missing explicit path is watched through its nearest existing
		// ancestor. Its immediate parent may itself be an optional directory
		// such as .cargo, .mvn, or gradle/wrapper.
		if errors.Is(err, os.ErrNotExist) {
			return m.watchNearestExistingAncestor(path)
		}
		return err
	}
	if !info.IsDir() {
		return m.watch(filepath.Dir(path))
	}
	if recursive {
		return m.addTree(path)
	}
	return m.watch(path)
}

func (m *Monitor) watchNearestExistingAncestor(path string) error {
	for candidate := filepath.Dir(path); ; candidate = filepath.Dir(candidate) {
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return m.watch(candidate)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return fmt.Errorf("no existing directory contains watch target %s", path)
		}
	}
}

func (m *Monitor) watch(path string) error {
	if m.isWatched(path) {
		return nil
	}
	if err := m.backend.Add(path); err != nil {
		return err
	}
	m.watchedMu.Lock()
	m.watched[filepath.Clean(path)] = struct{}{}
	m.watchedMu.Unlock()
	return nil
}

func (m *Monitor) addTree(root string) error {
	return m.addDirectoryTree(root, map[string]bool{})
}

func (m *Monitor) addDirectoryTree(directory string, visited map[string]bool) error {
	if m.followSymlinks {
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return err
		}
		if visited[resolved] {
			return nil
		}
		visited[resolved] = true
	}
	if err := m.watch(directory); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		isDirectory := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if !m.followSymlinks {
				continue
			}
			metadata, err := os.Stat(path)
			if err != nil {
				return err
			}
			isDirectory = metadata.IsDir()
		}
		if !isDirectory || m.ignoreDir != nil && m.ignoreDir(path, entry.Name()) {
			continue
		}
		if err := m.addDirectoryTree(path, visited); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) isWatched(path string) bool {
	m.watchedMu.RLock()
	_, ok := m.watched[filepath.Clean(path)]
	m.watchedMu.RUnlock()
	return ok
}

func (m *Monitor) absolute(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(m.root, path))
}

func (m *Monitor) relative(path string) (string, bool) {
	rel, err := filepath.Rel(m.root, m.absolute(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	return filepath.ToSlash(rel), true
}

func contains(scope Scope, path string) bool {
	if filepath.Clean(scope.Path) == filepath.Clean(path) {
		return true
	}
	if !scope.Recursive {
		return scope.Directory && filepath.Dir(filepath.Clean(path)) == filepath.Clean(scope.Path)
	}
	rel, err := filepath.Rel(scope.Path, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *Monitor) inScope(path string) bool {
	for _, scope := range m.scopes {
		if contains(scope, path) {
			return true
		}
	}
	for _, input := range m.inputs {
		if contains(Scope{Path: input.Path, Recursive: input.Recursive, Directory: input.Directory}, path) {
			return true
		}
	}
	return false
}

func (m *Monitor) classification(path string, isDir bool) (Classification, bool) {
	for _, input := range m.inputs {
		if contains(Scope{Path: input.Path, Recursive: input.Recursive, Directory: input.Directory}, path) {
			return Classification{Kind: input.Kind}, true
		}
	}
	rel, ok := m.relative(path)
	if !ok || !m.inScope(path) || m.classifier == nil {
		return Classification{}, false
	}
	classification, ok := m.classifier.Classify(rel, isDir)
	if !ok {
		return Classification{}, false
	}
	for _, scope := range m.scopes {
		if contains(scope, path) && classification.Kind == KindSource {
			// A non-source classifier result is more specific than the scope's
			// default. This lets manifests inside a source tree trigger a full
			// dependency/configuration revalidation.
			classification.Kind = scope.Kind
			break
		}
	}
	return classification, true
}

func (m *Monitor) markPath(path string, reason Reason, isDir bool) {
	path = m.absolute(path)
	classification, ok := m.classification(path, isDir)
	if !ok {
		return
	}
	rel, ok := m.relative(path)
	if !ok {
		return
	}
	m.dirty.mark(DirtyEntry{Path: rel, Kind: classification.Kind, Reasons: reason})
	m.signal()
}

func (m *Monitor) markAll(reason Reason) {
	m.dirty.markAll(reason)
	m.signal()
}

func (m *Monitor) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Monitor) run() {
	events := m.backend.Events()
	errors := m.backend.Errors()
	for {
		select {
		case <-m.done:
			return
		case event, ok := <-events:
			if !ok {
				m.markAll(ReasonWatcherError)
				return
			}
			m.handle(event)
		case _, ok := <-errors:
			if !ok {
				errors = nil
				continue
			}
			m.markAll(ReasonWatcherError)
		}
	}
}

func (m *Monitor) handle(event Event) {
	if event.Name == "" || event.Op == 0 {
		return
	}
	path := m.absolute(event.Name)
	if event.IsDir && event.Op&OpCreate != 0 {
		m.handleCreatedDirectory(path)
		return
	}
	if event.IsDir || (event.Op&(OpRemove|OpRename) != 0 && m.isWatched(path)) {
		var reason Reason
		if event.Op&OpCreate != 0 {
			reason |= ReasonCreate
		}
		if event.Op&OpRemove != 0 {
			reason |= ReasonRemove
		}
		if event.Op&OpRename != 0 {
			reason |= ReasonRename
		}
		m.markAll(reason | ReasonDirectory)
		return
	}
	if event.Op&OpCreate != 0 {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			m.handleCreatedDirectory(path)
			return
		}
	}
	if event.Op&(OpWrite|OpCreate|OpRemove|OpRename) == 0 {
		return
	}
	var reason Reason
	if event.Op&OpWrite != 0 {
		reason |= ReasonWrite
	}
	if event.Op&OpCreate != 0 {
		reason |= ReasonCreate
	}
	if event.Op&OpRemove != 0 {
		reason |= ReasonRemove
	}
	if event.Op&OpRename != 0 {
		reason |= ReasonRename
	}
	m.markPath(path, reason, false)
}

func (m *Monitor) handleCreatedDirectory(path string) {
	if m.inScope(path) {
		// Register before marking: files created with the directory are covered
		// even when notifications are coalesced.
		if err := m.addTree(path); err != nil {
			m.markAll(ReasonWatcherError | ReasonDirectory)
			return
		}
		m.markAll(ReasonCreate | ReasonDirectory)
		return
	}
	// A newly-created directory can be an intermediate component of an exact
	// input that did not exist at startup. Extend the watch chain without
	// treating the directory itself as an analysis change.
	if err := m.registerConfiguredTargetsBelow(path); err != nil {
		m.markAll(ReasonWatcherError | ReasonDirectory)
	}
}

func (m *Monitor) registerConfiguredTargetsBelow(directory string) error {
	for _, scope := range m.scopes {
		if isStrictAncestor(directory, scope.Path) {
			if err := m.registerTarget(scope.Path, scope.Recursive); err != nil {
				return err
			}
		}
	}
	for _, input := range m.inputs {
		if isStrictAncestor(directory, input.Path) {
			if err := m.registerTarget(input.Path, input.Recursive); err != nil {
				return err
			}
		}
	}
	return nil
}

func isStrictAncestor(directory, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(directory), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// Wake returns a coalesced notification channel. The channel is not an event
// log; callers must call Drain or WaitAndDrain to obtain authoritative paths.
func (m *Monitor) Wake() <-chan struct{} { return m.wake }

// Drain atomically takes all currently pending work. It never waits.
func (m *Monitor) Drain() DirtyBatch { return m.dirty.snapshot() }

// WaitAndDrain waits for a wake and then for the configured quiet period. It
// also handles a wake that was coalesced before the caller began waiting.
func (m *Monitor) WaitAndDrain(ctx context.Context) (DirtyBatch, error) {
	if m.dirty.pending() {
		return m.quietDrain(ctx)
	}
	select {
	case <-ctx.Done():
		return DirtyBatch{}, ctx.Err()
	case <-m.done:
		return DirtyBatch{}, context.Canceled
	case <-m.wake:
		return m.quietDrain(ctx)
	}
}

func (m *Monitor) quietDrain(ctx context.Context) (DirtyBatch, error) {
	timer := time.NewTimer(m.debounce)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return DirtyBatch{}, ctx.Err()
		case <-m.done:
			return DirtyBatch{}, context.Canceled
		case <-m.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(m.debounce)
		case <-timer.C:
			if batch := m.Drain(); !batch.Empty() {
				return batch, nil
			}
			if !m.dirty.pending() {
				return DirtyBatch{}, nil
			}
			timer.Reset(m.debounce)
		}
	}
}

// Close stops event processing and releases the backend. It is safe to call
// more than once. Pending dirty state remains drainable after Close.
func (m *Monitor) Close() error {
	var err error
	m.closeOnce.Do(func() {
		close(m.done)
		err = m.backend.Close()
	})
	return err
}

type fsnotifyBackend struct {
	watcher   *fsnotify.Watcher
	events    chan Event
	errors    chan error
	done      chan struct{}
	closeOnce sync.Once
}

func (b *fsnotifyBackend) Add(path string) error { return b.watcher.Add(path) }
func (b *fsnotifyBackend) Events() <-chan Event  { return b.events }
func (b *fsnotifyBackend) Errors() <-chan error  { return b.errors }

func (b *fsnotifyBackend) forward() {
	defer close(b.events)
	defer close(b.errors)
	for {
		select {
		case <-b.done:
			return
		case event, ok := <-b.watcher.Events:
			if !ok {
				return
			}
			var op Op
			if event.Op&fsnotify.Create != 0 {
				op |= OpCreate
			}
			if event.Op&fsnotify.Write != 0 {
				op |= OpWrite
			}
			if event.Op&fsnotify.Remove != 0 {
				op |= OpRemove
			}
			if event.Op&fsnotify.Rename != 0 {
				op |= OpRename
			}
			if op != 0 {
				select {
				case b.events <- Event{Name: event.Name, Op: op}:
				case <-b.done:
					return
				}
			}
		case err, ok := <-b.watcher.Errors:
			if !ok {
				return
			}
			select {
			case b.errors <- err:
			case <-b.done:
				return
			}
		}
	}
}

func (b *fsnotifyBackend) Close() (err error) {
	b.closeOnce.Do(func() {
		close(b.done)
		err = b.watcher.Close()
	})
	return err
}
