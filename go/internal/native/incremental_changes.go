package native

import (
	"context"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

func analyzeChanges(analyzer *Analyzer, parent context.Context, changed []string) (report.Document, []string, error) {
	snapshot := analyzer.plan
	paths, err := normalizeChangedPaths(analyzer.workspace, changed)
	if err != nil {
		return report.Document{}, nil, err
	}
	for path := range paths {
		snapshot.pending[path] = true
	}
	changes, substantive, err := prepareNamedChanges(analyzer, parent)
	if err != nil {
		retainChangedError(snapshot, err)
		return report.Document{}, nil, err
	}
	// Every pending named path was inspected. No new event can enter under planMu;
	// notifications received meanwhile remain in follow's existing event queue.
	incorporated := mapKeys(snapshot.pending)
	if len(changes) == 0 {
		for _, path := range incorporated {
			delete(snapshot.pending, path)
		}
		return report.Document{}, nil, nil
	}
	delta := snapshot.index.Prepare(changes)
	options := snapshot.options
	options.ReadCache = analysisOptions(analyzer.engine(), nil, nil).ReadCache
	var previousView, currentView changeLookup = snapshot.index, delta
	if analyzer.decorateLookup != nil {
		previousView = analyzer.decorateLookup(previousView)
		currentView = analyzer.decorateLookup(currentView)
	}
	affected := affectedIndexedUnits(previousView, currentView, substantive)
	_, oldPaths := indexedVisibleUnits(previousView, affected, options)
	units, newPaths := indexedVisibleUnits(currentView, affected, options)
	for path := range newPaths {
		oldPaths[path] = true
	}
	var document report.Document
	if len(units) > 0 {
		document, err = runIndexedChanges(analyzer, parent, currentView, units, options)
		if err != nil {
			retainChangedError(snapshot, err)
			return report.Document{}, nil, err
		}
	}
	if err := verifyNamedDelta(analyzer, changes); err != nil {
		retainChangedError(snapshot, err)
		return report.Document{}, nil, err
	}
	retry := false
	for _, diagnostic := range document.Diagnostics {
		if transientAnalyzerDiagnostic(diagnostic) {
			retry = true
		}
	}
	requestPlans := indexedRequestClosure(currentView, units, options)
	requestIDs := map[string]bool{}
	for id := range requestPlans {
		requestIDs[id] = true
	}
	for path := range inputPathsForUnits(requestPlans, requestIDs) {
		source, ok := delta.Source(path)
		if ok && source.NeedsRetry != retry {
			copy := *source
			copy.NeedsRetry = retry
			delta.Sources[path] = &copy
		}
	}
	for path, next := range changes {
		if old, ok := snapshot.index.Source(path); ok && old.Visible {
			snapshot.visibleCounts[string(old.Language)]--
		}
		if next != nil {
			next.Owner = canonicalOwner(delta, path, options)
			if next.Visible {
				snapshot.visibleCounts[string(next.Language)]++
			}
		}
	}
	delta.Commit()
	snapshot.selected = selectedIndexedLanguages(snapshot)
	for _, path := range incorporated {
		delete(snapshot.pending, path)
	}
	return document, mapKeys(oldPaths), nil
}
func selectedIndexedLanguages(snapshot *analysisPlanSnapshot) []string {
	if len(snapshot.options.Languages) > 0 {
		return snapshot.options.Languages
	}
	languages := map[string]bool{}
	for language, count := range snapshot.visibleCounts {
		if count > 0 {
			languages[language] = true
		}
	}
	return mapKeys(languages)
}

// Selected units and their required context are the only inputs to cache/report
// assembly. Construction/export inventory slices never enter this route.
func selectedDiscovery(units []plannedCacheUnit, options Options) (map[string][]string, []string) {
	grouped := map[string][]string{}
	for _, unit := range units {
		language := string(unit.plan.Language)
		for _, path := range unit.owned {
			if visibleSource(options, path, language) {
				grouped[language] = append(grouped[language], path)
			}
		}
	}
	return grouped, mapKeys(grouped)
}

var _ unitplan.Lookup = (*unitplan.PlanDelta)(nil)
