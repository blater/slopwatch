package workspace

import "context"

// Start registers all existing directories, then begins consuming backend
// events. Startup reconciliation runs asynchronously so it cannot delay cache
// publication. Events seen during reconciliation are retained in dirty state.
func (m *Monitor) Start(ctx context.Context) error {
	m.startOnce.Do(func() {
		defer close(m.started)
		m.startErr = m.engine.watch.registerAll(m.engine.paths.scopes, m.engine.paths.inputs)
		if m.startErr != nil {
			_ = m.Close()
			return
		}
		go m.engine.events.run()
		if m.engine.reconcile != nil {
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
// an explicitly requested inventory audit; events arriving concurrently
// remain in the same authoritative dirty set. Follow mode does not call it.
func (m *Monitor) Reconcile(ctx context.Context, full bool) error {
	if m.engine.reconcile == nil {
		return nil
	}
	paths, err := m.engine.reconcile(ctx, ReconcileRequest{Root: m.engine.paths.root, Scopes: m.engine.paths.scopesCopy(), Inputs: m.engine.paths.inputsCopy(), Full: full})
	if err != nil {
		m.engine.events.markError(err, false)
		return err
	}
	for _, path := range paths {
		m.engine.events.markPath(path, ReasonStartupAudit, false)
	}
	return nil
}

func (m *pathPolicy) scopesCopy() []Scope { return append([]Scope(nil), m.scopes...) }
func (m *pathPolicy) inputsCopy() []Input { return append([]Input(nil), m.inputs...) }

// Rescan repeats startup registration only on explicit user request. Native
// intake continues while inventory mutation and the startup verifier serialize.
func (m *Monitor) Rescan(ctx context.Context, verify func() error, prepare ...func() error) error {
	m.engine.events.mutation.Lock()
	defer m.engine.events.mutation.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, setup := range prepare {
		if err := setup(); err != nil {
			return err
		}
	}
	watch := &m.engine.watch
	watch.mu.Lock()
	oldWatches := watch.watched
	watch.watched = map[string]struct{}{}
	watch.known = map[string]Classification{}
	watch.children = map[string]map[string]struct{}{}
	watch.complete = map[string]bool{}
	watch.mu.Unlock()
	if err := watch.registerAll(m.engine.paths.scopes, m.engine.paths.inputs); err != nil {
		return err
	}
	for path := range oldWatches {
		if !watch.isWatched(path) {
			_ = watch.backend.Remove(path)
		}
	}
	if verify != nil {
		return verify()
	}
	return nil
}

// LossVersion lets an explicit rescan distinguish prior loss from loss during it.
func (m *Monitor) LossVersion() uint64 {
	if b, ok := m.engine.watch.backend.(*bufferedBackend); ok {
		return b.lossVersion.Load()
	}
	return 0
}
