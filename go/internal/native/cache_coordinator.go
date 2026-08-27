package native

import (
	"context"
	"errors"

	"github.com/blater/slopmochi/internal/analysiscache"
	"github.com/blater/slopmochi/internal/report"
	"github.com/blater/slopmochi/internal/unitplan"
)

const (
	nativeFactVersion     = "2"
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

var errWorkspaceChanged = errors.New("workspace changed while analysis snapshot was running")

func analyzeWithPersistentCache(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (report.Document, bool, error) {
	for attempt := 0; attempt < 2; attempt++ {
		document, handled, err := analyzeWithPersistentCacheOnce(analyzer, parent, catalog, discovered, selected, options)
		if errors.Is(err, errWorkspaceChanged) && attempt == 0 {
			continue
		}
		return document, handled, err
	}
	return report.Document{}, true, errWorkspaceChanged
}

func analyzeWithPersistentCacheOnce(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (report.Document, bool, error) {
	return runPersistentCache(analyzer, parent, catalog, discovered, selected, options)
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
