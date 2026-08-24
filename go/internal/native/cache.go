package native

import (
	"sync"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
)

type analyzerCache struct {
	mu    sync.RWMutex
	store *analysiscache.Store
}

// EnableDefaultCache enables the shared persistent cache. Cache setup failure
// deliberately degrades to an ordinary uncached run.
func (analyzer *Analyzer) EnableDefaultCache() {
	store, err := analysiscache.NewDefaultStore()
	if err != nil {
		return
	}
	analyzer.cache.mu.Lock()
	analyzer.cache.store = store
	analyzer.cache.mu.Unlock()
}

// SetCacheStore injects the persistent store used for verified reads and
// writes. Passing nil disables persistence. Tests should use this method with a
// temporary store rather than touching the default user cache.
func (analyzer *Analyzer) SetCacheStore(store *analysiscache.Store) {
	analyzer.cache.mu.Lock()
	analyzer.cache.store = store
	analyzer.cache.mu.Unlock()
}

// SetCacheReads changes reuse policy without affecting cache writes. Follow
// mode uses this to make a first-ever launch take the ordinary fresh path,
// then enables package reuse after that initial result is visible.
func (analyzer *Analyzer) SetCacheReads(enabled bool) {
	analyzer.optionsMu.Lock()
	analyzer.options.ReadCache = enabled
	analyzer.optionsMu.Unlock()
}

func (analyzer *Analyzer) cacheStore() *analysiscache.Store {
	analyzer.cache.mu.RLock()
	defer analyzer.cache.mu.RUnlock()
	return analyzer.cache.store
}

func (analyzer *Analyzer) viewKey(options Options) (analysiscache.ViewKey, error) {
	return analysiscache.WorkspaceViewKey(analyzer.workspace, analysiscache.ViewOptions{
		Targets: options.Targets, Languages: options.Languages,
		IncludeTests: options.IncludeTests, TypeScriptTypes: options.TypeScriptTypes,
		FollowSymlinks: options.FollowSymlinks,
	})
}

// CachedProjection returns the last complete view immediately. Its rows are
// always provisional until the current workspace has been reconciled.
func (analyzer *Analyzer) CachedProjection() (report.Document, bool) {
	store := analyzer.cacheStore()
	if store == nil {
		return report.Document{}, false
	}
	analyzer.optionsMu.RLock()
	options := analyzer.options
	analyzer.optionsMu.RUnlock()
	if !options.ReadCache || options.FollowSymlinks {
		return report.Document{}, false
	}
	view, err := analyzer.viewKey(options)
	if err != nil {
		return report.Document{}, false
	}
	generation, ok := store.LoadGeneration(view)
	if !ok {
		return report.Document{}, false
	}
	projection, ok := store.LoadProjection(generation.Projection, view)
	if !ok {
		return report.Document{}, false
	}
	document := report.Document{
		Calibrated: true, Files: projection.ReportFiles(), ProfileSetHash: "native-balanced-v1",
		SchemaVersion: 3, Summary: map[string]any{"cache_state": "provisional"},
	}
	for index := range document.Files {
		document.Files[index].Freshness = report.FreshnessProvisional
		document.Files[index].FreshnessNote = "validating current workspace"
	}
	document.SortAndRank()
	return document, true
}

func (analyzer *Analyzer) persistProjection(document report.Document, options Options) {
	store := analyzer.cacheStore()
	if store == nil {
		return
	}
	view, err := analyzer.viewKey(options)
	if err != nil {
		return
	}
	projection := analysiscache.ProjectionFromReport(view, document, analysiscache.FreshnessCurrent)
	reference, err := store.PutProjection(view, projection)
	if err != nil {
		return
	}
	previous, found := store.LoadGeneration(view)
	units := map[analysiscache.Key]analysiscache.ArtifactRef{}
	if found {
		for key, value := range previous.Units {
			units[key] = value
		}
	}
	_, _ = store.CommitGeneration(view, analysiscache.Generation{Projection: reference, Units: units})
}
