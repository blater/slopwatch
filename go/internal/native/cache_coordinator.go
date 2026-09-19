package native

import (
	"context"
	"errors"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

const (
	nativeFactVersion     = "4"
	nativeProtocolVersion = "1"
)

type analyzerUnitsRunner func(context.Context, string, analyzerRequest) (map[string]scoreInputs, error)

type plannedCacheUnit struct {
	plan          unitplan.Unit
	owned         []string
	key           analysiscache.Key
	snapshotPaths []string
	analysisPaths []string
}

type cachePreparation struct {
	units          []plannedCacheUnit
	digests        map[string]analysiscache.Digest
	backendDigests map[string]analysiscache.Digest
	plans          map[string]unitplan.Unit
}

// ErrWorkspaceChanged reports that live workspace inputs changed while an
// analysis snapshot was being prepared or verified. Callers that watch a
// workspace can safely retry this outcome without surfacing a failure.
var ErrWorkspaceChanged = errors.New("workspace changed while analysis snapshot was running")

type persistentAnalysisResult struct {
	document   report.Document
	handled    bool
	err        error
	plan       unitplan.Plan
	discovered map[string][]string
	selected   []string
}

func analyzeWithPersistentCache(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options, plan unitplan.Plan, planErr error, planningOptions Options) persistentAnalysisResult {
	for attempt := 0; attempt < 2; attempt++ {
		if !options.ignoreMatcher.Unchanged() {
			return persistentAnalysisResult{handled: true, err: ErrWorkspaceChanged}
		}
		attemptPlan, attemptDiscovered, attemptSelected := plan, discovered, selected
		if attempt > 0 {
			var err error
			attemptDiscovered, err = discoverPolicy(analyzer, options.Targets, options.IncludeTests, options.FollowSymlinks, options.DisableGitignore, options.ignoreMatcher)
			if err != nil {
				return persistentAnalysisResult{handled: true, err: err}
			}
			attemptSelected, err = selectedLanguages(options.Languages, attemptDiscovered)
			if err != nil {
				return persistentAnalysisResult{handled: true, err: err}
			}
			attemptPlan, err = workspacePlan(analyzer, planningOptions)
			if err != nil {
				return persistentAnalysisResult{handled: true, err: err}
			}
		}
		result := analyzeWithPersistentCacheOnce(analyzer, parent, catalog, attemptDiscovered, attemptSelected, options, attemptPlan, planErr)
		if errors.Is(result.err, ErrWorkspaceChanged) && attempt == 0 {
			continue
		}
		result.plan, result.discovered, result.selected = attemptPlan, attemptDiscovered, attemptSelected
		return result
	}
	return persistentAnalysisResult{handled: true, err: ErrWorkspaceChanged}
}

func analyzeWithPersistentCacheOnce(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options, plan unitplan.Plan, planErr error) persistentAnalysisResult {
	document, handled, err := runPersistentCache(analyzer, parent, catalog, discovered, selected, options, plan, planErr)
	return persistentAnalysisResult{document: document, handled: handled, err: err}
}

func snapshotFilesForMisses(units []plannedCacheUnit, digests map[string]analysiscache.Digest) []analysiscache.SnapshotFile {
	paths := make(map[string]bool)
	for _, unit := range units {
		for _, path := range unit.snapshotPaths {
			paths[path] = true
		}
	}
	result := make([]analysiscache.SnapshotFile, 0, len(paths))
	for _, path := range mapKeys(paths) {
		result = append(result, analysiscache.SnapshotFile{Path: path, Digest: digests[path]})
	}
	return result
}
