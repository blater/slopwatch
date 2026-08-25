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
// watcher overflow or other full-audit request; events arriving concurrently
// remain in the same authoritative dirty set.
func (m *Monitor) Reconcile(ctx context.Context, full bool) error {
	if m.engine.reconcile == nil {
		return nil
	}
	paths, err := m.engine.reconcile(ctx, ReconcileRequest{Root: m.engine.paths.root, Scopes: m.engine.paths.scopesCopy(), Inputs: m.engine.paths.inputsCopy(), Full: full})
	if err != nil {
		m.engine.events.markAll(ReasonWatcherError | ReasonStartupAudit)
		return err
	}
	for _, path := range paths {
		m.engine.events.markPath(path, ReasonStartupAudit, false)
	}
	return nil
}

func (m *pathPolicy) scopesCopy() []Scope { return append([]Scope(nil), m.scopes...) }
func (m *pathPolicy) inputsCopy() []Input { return append([]Input(nil), m.inputs...) }
