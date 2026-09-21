package native

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

// ErrIncrementalPlanUnavailable reports that no successful initial plan exists.
// Callers retain published results and surface the failed incremental update.
var ErrIncrementalPlanUnavailable = errors.New("incremental analysis requires a successful initial analysis")

type analysisPlanSnapshot struct {
	plan       unitplan.Plan
	discovered map[string][]string
	selected   []string
	options    Options
}

// AnalyzeChanges refreshes only units affected by changed workspace paths and
// returns the paths whose previous rows should be replaced. The saved plan is
// retained until the complete refresh succeeds, allowing callers to retry a
// transient filesystem or analyzer failure safely.
func (analyzer *Analyzer) AnalyzeChanges(parent context.Context, changed []string) (report.Document, []string, error) {
	analyzer.planMu.Lock()
	defer analyzer.planMu.Unlock()
	if analyzer.plan == nil {
		return report.Document{}, nil, ErrIncrementalPlanUnavailable
	}
	return analyzeChanges(analyzer, parent, changed)
}

func newPlanSnapshot(plan unitplan.Plan, discovered map[string][]string, selected []string, options Options) *analysisPlanSnapshot {
	return &analysisPlanSnapshot{plan: plan, discovered: cloneDiscovered(discovered), selected: append([]string(nil), selected...), options: options}
}

func workspacePlan(analyzer *analysisEngine, options Options) (unitplan.Plan, error) {
	typeScriptMode := unitplan.TypeScriptSyntax
	if options.TypeScriptTypes {
		typeScriptMode = unitplan.TypeScriptTyped
	}
	return unitplan.PlanWorkspace(analyzer.workspace, unitplan.Options{TypeScriptMode: typeScriptMode, Targets: options.Targets, DisableGitignore: options.DisableGitignore, IgnoreMatcher: options.ignoreMatcher, Configuration: options.configuration})
}

func cloneDiscovered(discovered map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(discovered))
	for language, paths := range discovered {
		clone[language] = append([]string(nil), paths...)
	}
	return clone
}

func analyzeChanges(analyzer *Analyzer, parent context.Context, changed []string) (report.Document, []string, error) {
	previous := analyzer.plan
	options := previous.options
	options.ReadCache = analysisOptions(analyzer.engine(), nil, nil).ReadCache
	current, err := currentChangePlan(analyzer, options, previous.selected)
	if err != nil {
		return report.Document{}, nil, err
	}
	options = current.options
	if !options.ignoreMatcher.Unchanged() {
		return report.Document{}, nil, ErrWorkspaceChanged
	}
	if previous.options.ignorePolicy != "" && previous.options.ignorePolicy != current.options.ignorePolicy {
		return report.Document{}, nil, ErrIncrementalPlanUnavailable
	}
	paths, err := normalizeChangedPaths(analyzer.workspace, changed)
	if err != nil {
		return report.Document{}, nil, err
	}
	oldUnits := allChangeUnits(previous.plan, previous.selected, options.IncludeTests)
	newUnits := allChangeUnits(current.plan, current.selected, options.IncludeTests)
	affected := affectedChangeUnits(oldUnits, newUnits, paths)
	if len(affected) == 0 {
		analyzer.plan = current
		return report.Document{}, nil, nil
	}
	oldVisible := filterPlannedUnits(previous.plan.Units, previous.discovered, previous.selected, options.IncludeTests)
	newVisible := filterPlannedUnits(current.plan.Units, current.discovered, current.selected, options.IncludeTests)
	replacements := replacementPaths(oldVisible, newVisible, affected, previous, current)
	currentUnits := selectAffectedUnits(newVisible, affected)
	if len(currentUnits) == 0 {
		analyzer.plan = current
		return report.Document{}, replacements, nil
	}
	document, err := runChangedUnits(analyzer, parent, current, currentUnits, options)
	if err != nil {
		return report.Document{}, nil, err
	}
	if !options.ignoreMatcher.Unchanged() {
		return report.Document{}, nil, ErrWorkspaceChanged
	}
	document.Diagnostics = append(document.Diagnostics, options.ignoreMatcher.Diagnostics()...)
	analyzer.plan = current
	return document, replacements, nil
}

func currentChangePlan(analyzer *Analyzer, options Options, selected []string) (*analysisPlanSnapshot, error) {
	discovered, err := discoverPolicyWithMissingTargets(analyzer.engine(), options.Targets, options.IncludeTests, options.FollowSymlinks, options.DisableGitignore, true, options.ignoreMatcher)
	if err != nil {
		return nil, err
	}
	plan, err := workspacePlan(analyzer.engine(), options)
	if err != nil {
		return nil, err
	}
	if options.ignoreMatcher != nil {
		options.ignorePolicy = options.ignoreMatcher.Fingerprint()
	}
	return newPlanSnapshot(plan, discovered, changeSelected(options, discovered, selected), options), nil
}

func changeSelected(options Options, discovered map[string][]string, previous []string) []string {
	if len(options.Languages) > 0 {
		return append([]string(nil), options.Languages...)
	}
	selected := make([]string, 0, len(discovered))
	for language, paths := range discovered {
		if len(paths) > 0 {
			selected = append(selected, language)
		}
	}
	if len(selected) == 0 {
		return append([]string(nil), previous...)
	}
	sort.Strings(selected)
	return selected
}

func samePlanOptions(left, right Options) bool {
	return sameStrings(left.Targets, right.Targets) && sameStrings(left.Languages, right.Languages) &&
		left.DisableGitignore == right.DisableGitignore && left.IncludeTests == right.IncludeTests && left.TypeScriptTypes == right.TypeScriptTypes &&
		shallowProfile(left) == shallowProfile(right) && left.FollowSymlinks == right.FollowSymlinks
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func normalizeChangedPaths(workspace string, changed []string) (map[string]bool, error) {
	paths := make(map[string]bool, len(changed))
	for _, changedPath := range changed {
		if changedPath == "" {
			continue
		}
		path := filepath.Clean(changedPath)
		if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("changed path %q is outside the analysis root", changedPath)
		}
		if filepath.IsAbs(path) {
			relative, err := filepath.Rel(workspace, path)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("changed path %q is outside the analysis root", changedPath)
			}
			path = relative
		}
		paths[filepath.ToSlash(path)] = true
	}
	return paths, nil
}

func affectedChangeUnits(oldUnits, newUnits []plannedCacheUnit, changed map[string]bool) map[string]bool {
	all := mergeChangeUnits(oldUnits, newUnits)
	affected := make(map[string]bool)
	markChangedUnits(affected, oldUnits, changed)
	markChangedUnits(affected, newUnits, changed)
	expandReverseDependencies(affected, all)
	conservativeLanguagesForChanges(affected, oldUnits, newUnits, changed)
	// A conservative unit may have been added by the language-wide pass. Its
	// reverse dependents need the same invalidation as directly changed units.
	expandReverseDependencies(affected, all)
	return affected
}

func plannedUnitsByID(units []plannedCacheUnit) map[string]plannedCacheUnit {
	result := make(map[string]plannedCacheUnit, len(units))
	for _, unit := range units {
		result[unit.plan.ID] = unit
	}
	return result
}

func unitPlansByID(units []unitplan.Unit) map[string]unitplan.Unit {
	result := make(map[string]unitplan.Unit, len(units))
	for _, unit := range units {
		result[unit.ID] = unit
	}
	return result
}

func allChangeUnits(plan unitplan.Plan, selected []string, includeTests bool) []plannedCacheUnit {
	languages := pathSet(selected)
	result := make([]plannedCacheUnit, 0, len(plan.Units))
	for _, item := range plan.Units {
		if !languages[string(item.Language)] || !includeTests && hasCapability(item, unitplan.CapabilityTests) {
			continue
		}
		effective := item
		if !includeTests {
			effective.Sources = withoutTestPaths(effective.Sources, string(effective.Language))
			effective.ContextSources = withoutTestPaths(effective.ContextSources, string(effective.Language))
		}
		if len(effective.Sources) == 0 {
			continue
		}
		result = append(result, plannedCacheUnit{plan: effective, owned: append([]string(nil), effective.Sources...)})
	}
	return result
}

func mergeChangeUnits(oldUnits, newUnits []plannedCacheUnit) map[string]plannedCacheUnit {
	result := plannedUnitsByID(oldUnits)
	for _, unit := range newUnits {
		if previous, exists := result[unit.plan.ID]; exists {
			dependencies := append(append([]string{}, previous.plan.ReverseDependencies...), unit.plan.ReverseDependencies...)
			unit.plan.ReverseDependencies = sortedUnique(dependencies)
		}
		result[unit.plan.ID] = unit
	}
	return result
}

func sortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func markChangedUnits(affected map[string]bool, units []plannedCacheUnit, changed map[string]bool) {
	for _, unit := range units {
		for _, path := range unitInputPaths(unit.plan) {
			if changed[path] {
				affected[unit.plan.ID] = true
				break
			}
		}
	}
}

func changedUnitLanguage(unit plannedCacheUnit, changed map[string]bool) bool {
	for _, path := range unitInputPaths(unit.plan) {
		if changed[path] {
			return true
		}
	}
	return false
}

func expandReverseDependencies(affected map[string]bool, units map[string]plannedCacheUnit) {
	queue := make([]string, 0, len(affected))
	for id := range affected {
		queue = append(queue, id)
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, dependent := range units[id].plan.ReverseDependencies {
			if !affected[dependent] {
				affected[dependent] = true
				queue = append(queue, dependent)
			}
		}
	}
}

func conservativeLanguagesForChanges(affected map[string]bool, oldUnits, newUnits []plannedCacheUnit, changed map[string]bool) {
	languages := map[string]bool{}
	for _, units := range [][]plannedCacheUnit{oldUnits, newUnits} {
		for _, unit := range units {
			// A changed unit can invalidate conservative units in its language,
			// even when the changed unit itself is not conservative. This is the
			// planner's contract for units which depend on whole-language state.
			if changedUnitLanguage(unit, changed) || affected[unit.plan.ID] && unit.plan.Conservative {
				languages[string(unit.plan.Language)] = true
			}
		}
	}
	for _, unit := range newUnits {
		if unit.plan.Conservative && languages[string(unit.plan.Language)] {
			affected[unit.plan.ID] = true
		}
	}
}

func replacementPaths(oldUnits, newUnits []plannedCacheUnit, affected map[string]bool, previous, current *analysisPlanSnapshot) []string {
	paths := map[string]bool{}
	visible := discoveredPathSet(previous.discovered, previous.selected)
	for path := range discoveredPathSet(current.discovered, current.selected) {
		visible[path] = true
	}
	for _, units := range [][]plannedCacheUnit{oldUnits, newUnits} {
		for _, unit := range units {
			if !affected[unit.plan.ID] {
				continue
			}
			for _, path := range unit.owned {
				if visible[path] {
					paths[path] = true
				}
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func selectAffectedUnits(units []plannedCacheUnit, affected map[string]bool) []plannedCacheUnit {
	result := make([]plannedCacheUnit, 0, len(units))
	for _, unit := range units {
		if affected[unit.plan.ID] {
			result = append(result, unit)
		}
	}
	return result
}

func runChangedUnits(analyzer *Analyzer, parent context.Context, current *analysisPlanSnapshot, units []plannedCacheUnit, options Options) (report.Document, error) {
	catalog := activeCatalog(analyzer.catalog, options)
	discovered := discoveredForUnits(current.discovered, current.selected, units)
	cacheOptions := options
	cacheOptions.Targets = mapKeys(discoveredPathSet(discovered, current.selected))
	document, handled, err := runPersistentCache(analyzer.engine(), parent, catalog, discovered, current.selected, cacheOptions, current.plan, nil)
	if handled {
		return document, err
	}
	if err != nil {
		return report.Document{}, err
	}

	// The cache is optional. Prepare the complete dependency closure before the
	// uncached run so affected units still receive the same cross-file context
	// as a normal analysis.
	prepared := prepareMissPaths(units, unitPlansByID(current.plan.Units))
	paths := map[string]bool{}
	for _, unit := range prepared {
		for _, path := range unit.snapshotPaths {
			paths[path] = true
		}
	}
	digests, stamps, err := cachedWorkspaceDigests(analyzer.engine(), parent, paths, analysiscache.Generation{}, options.configuration.Bytes())
	if err != nil {
		return report.Document{}, err
	}
	var store *analysiscache.Store
	root, cleanup, err := store.MaterializeWorkspaceSnapshot(parent, analyzer.workspace, snapshotFilesForMisses(prepared, digests), options.configuration.Bytes())
	if errors.Is(err, analysiscache.ErrWorkspaceSnapshotChanged) {
		return report.Document{}, ErrWorkspaceChanged
	}
	if err != nil {
		return report.Document{}, err
	}
	defer cleanup()
	inputs, err := runMissingUnits(analyzer.engine(), parent, root, catalog, prepared, options)
	if err != nil {
		return report.Document{}, err
	}
	unchanged, err := verifyWorkspaceStamps(analyzer.engine(), parent, stamps)
	if err != nil {
		return report.Document{}, err
	}
	if !unchanged {
		return report.Document{}, ErrWorkspaceChanged
	}
	return changeReport(catalog, current.selected, discovered, units, inputs, options)
}

func discoveredForUnits(inventory map[string][]string, selected []string, units []plannedCacheUnit) map[string][]string {
	paths := make(map[string]map[string]bool, len(selected))
	for _, language := range selected {
		paths[language] = map[string]bool{}
	}
	visible := discoveredPathSet(inventory, selected)
	for _, unit := range units {
		language := string(unit.plan.Language)
		if paths[language] == nil {
			paths[language] = map[string]bool{}
		}
		for _, path := range unit.owned {
			if visible[path] {
				paths[language][path] = true
			}
		}
	}
	result := make(map[string][]string, len(paths))
	for language, pathSet := range paths {
		result[language] = mapKeys(pathSet)
	}
	return result
}

func changeReport(catalog catalogDocument, selected []string, discovered map[string][]string, units []plannedCacheUnit, inputs map[string]scoreInputs, options Options) (report.Document, error) {
	merged := newScoreInputs()
	paths := discoveredPathSet(discovered, selected)
	for _, unit := range units {
		merged.merge(inputs[unit.plan.ID])
	}
	merged = filterScoreInputs(merged, paths)
	document, err := scoreInputsReport(catalog, selected, merged, options.PassScore)
	if err != nil {
		return report.Document{}, err
	}
	document.Summary["discovered_source_count"] = len(paths)
	return document, nil
}
