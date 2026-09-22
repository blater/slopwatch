package native

import (
	"sync"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceignore"
)

type analyzerCache struct {
	mu    sync.RWMutex
	store *analysiscache.Store
}

// EnableDefaultCache enables the shared persistent cache. Cache setup failure
// deliberately degrades to an ordinary uncached run.
func (analyzer *Analyzer) EnableDefaultCache() {
	root, err := analysiscache.DefaultRoot()
	if err != nil {
		return
	}
	analyzer.EnableCache(root)
}

// EnableCache enables persistence at an explicitly selected child of the
// Slopwatch user directory. Setup failure degrades to an uncached run.
func (analyzer *Analyzer) EnableCache(root string) {
	store, err := analysiscache.NewStore(root)
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

// SetCacheReads changes reuse policy without affecting cache writes.
func (analyzer *Analyzer) SetCacheReads(enabled bool) {
	analyzer.optionsMu.Lock()
	analyzer.options.ReadCache = enabled
	analyzer.optionsMu.Unlock()
}

func cacheStore(analyzer *analysisEngine) *analysiscache.Store {
	analyzer.cache.mu.RLock()
	defer analyzer.cache.mu.RUnlock()
	return analyzer.cache.store
}

func (analyzer *Analyzer) viewKey(options Options) (analysiscache.ViewKey, error) {
	return viewKey(analyzer.engine(), options)
}

func viewKey(analyzer *analysisEngine, options Options) (analysiscache.ViewKey, error) {
	policy := "gitignore-on-v2"
	if options.DisableGitignore {
		policy = "gitignore-off-v2"
	}
	return analysiscache.WorkspaceViewKey(analyzer.workspace, analysiscache.ViewOptions{
		GitignorePolicy: policy,
		Targets:         cacheViewTargets(options), Languages: options.Languages,
		IncludeTests: options.IncludeTests, TypeScriptTypes: options.TypeScriptTypes,
		FollowSymlinks: options.FollowSymlinks,
	})
}

func cacheViewTargets(options Options) []string {
	return append([]string(nil), options.Targets...)
}

// CachedProjection returns the last complete view immediately. Its rows are
// always provisional until the current workspace has been reconciled.
func (analyzer *Analyzer) CachedProjection() (report.Document, bool) {
	return cachedProjection(analyzer.engine())
}

func cachedProjection(analyzer *analysisEngine) (report.Document, bool) {
	store := cacheStore(analyzer)
	if store == nil {
		return report.Document{}, false
	}
	analyzer.optionsMu.RLock()
	options := analyzer.options
	analyzer.optionsMu.RUnlock()
	if !options.ReadCache || options.FollowSymlinks {
		return report.Document{}, false
	}
	view, err := viewKey(analyzer, options)
	if err != nil {
		return report.Document{}, false
	}
	generation, ok := store.LoadGeneration(view)
	if !ok {
		return report.Document{}, false
	}
	projection, ok := store.LoadProjection(generation.Projection)
	if !ok {
		return report.Document{}, false
	}
	matcher, err := sourceignore.New(analyzer.workspace, options.DisableGitignore)
	if err != nil {
		return report.Document{}, false
	}
	document := projectionDocument(projection)
	// Filter only cached paths. Initial analysis reconciles the source inventory
	// after the dashboard opens; missing cached sources remain provisional.
	files := document.Files[:0]
	for _, file := range document.Files {
		if !matcher.Ignored(file.Path, false) {
			files = append(files, file)
		}
	}
	document.Files = files
	document.Summary["discovered_source_count"] = len(document.Files)
	for index := range document.Files {
		document.Files[index].Freshness = report.FreshnessProvisional
		document.Files[index].FreshnessNote = "validating current workspace"
	}
	document.SortAndRank()
	return document, true
}

func persistProjection(analyzer *analysisEngine, document report.Document, options Options) {
	store := cacheStore(analyzer)
	if store == nil {
		return
	}
	view, err := viewKey(analyzer, options)
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

func projectionDocument(projection analysiscache.DisplayProjection) report.Document {
	schemaVersion, profileHash := projection.SchemaVersion, projection.ProfileSetHash
	if schemaVersion == 0 {
		schemaVersion = 3
	}
	if profileHash == "" {
		profileHash = "native-balanced-v1"
	}
	for id, boundary := range projection.Depth {
		projection.Depth[id] = report.CompactDepthBoundary(boundary)
	}
	document := report.Document{
		Calibrated: true, Files: projection.ReportFiles(), ProfileSetHash: profileHash,
		ScoreProfile: projection.ScoreProfile, PolicyRevision: projection.PolicyRevision, ScorePolicyRevision: projection.ScorePolicyRevision,
		SchemaVersion: schemaVersion, Depth: projection.Depth, Summary: map[string]any{
			"cache_state": "provisional", "discovered_source_count": len(projection.Files),
		},
	}
	return document
}
