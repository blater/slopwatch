// Package workspace owns the filesystem boundary used by analysis coordinators.
//
// A Monitor deliberately has two outputs: a durable dirty set (the authority)
// and Wake (a best-effort, coalesced notification). A caller must always drain
// the dirty set; a dropped Wake can never drop a path.
package workspace

import (
	"context"
	"sort"
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
	engine    monitorEngine
	closeOnce sync.Once
	startOnce sync.Once
	started   chan struct{}
	startErr  error
}

type monitorEngine struct {
	paths     pathPolicy
	watch     watchManager
	events    eventManager
	dirty     dirtySet
	reconcile ReconcileHook
}

type pathPolicy struct {
	root       string
	scopes     []Scope
	inputs     []Input
	classifier Classifier
}

type watchManager struct {
	backend        Backend
	ignoreDir      func(string, string) bool
	followSymlinks bool
	mu             sync.RWMutex
	watched        map[string]struct{}
}

type eventManager struct {
	paths    *pathPolicy
	watch    *watchManager
	dirty    *dirtySet
	wake     chan struct{}
	done     chan struct{}
	debounce time.Duration
}

func (m *eventManager) markPath(path string, reason Reason, isDir bool) {
	path = m.paths.absolute(path)
	classification, ok := m.paths.classification(path, isDir)
	if !ok {
		return
	}
	rel, ok := m.paths.relative(path)
	if !ok {
		return
	}
	m.dirty.mark(DirtyEntry{Path: rel, Kind: classification.Kind, Reasons: reason})
	m.signal()
}

func (m *eventManager) markAll(reason Reason) {
	m.dirty.markAll(reason)
	m.signal()
}

func (m *eventManager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

type fsnotifyBackend struct {
	watcher   *fsnotify.Watcher
	events    chan Event
	errors    chan error
	done      chan struct{}
	closeOnce sync.Once
}
