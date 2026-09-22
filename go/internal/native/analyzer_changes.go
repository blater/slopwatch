package native

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/unitplan"
)

// ErrIncrementalPlanUnavailable reports that no successful initial plan exists.
// Callers retain published results and surface the failed incremental update.
var ErrIncrementalPlanUnavailable = errors.New("incremental analysis requires a successful initial analysis")

type analysisPlanSnapshot struct {
	index          *unitplan.Index
	startupVisible map[string]bool
	initialInputs  map[string]bool
	pending        map[string]bool
	visibleCounts  map[string]int
	selected       []string
	options        Options
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
	return &analysisPlanSnapshot{startupVisible: discoveredPathSet(discovered, selected), index: plan.Index, pending: map[string]bool{}, visibleCounts: map[string]int{}, selected: append([]string(nil), selected...), options: options}
}

func workspacePlan(analyzer *analysisEngine, options Options) (unitplan.Plan, error) {
	typeScriptMode := unitplan.TypeScriptSyntax
	if options.TypeScriptTypes {
		typeScriptMode = unitplan.TypeScriptTyped
	}
	return unitplan.PlanWorkspace(analyzer.workspace, unitplan.Options{FileSystem: analyzer.fileSystem, BeforeDiscovery: analyzer.beforeDiscovery, TypeScriptMode: typeScriptMode, Targets: options.Targets, DisableGitignore: options.DisableGitignore, IgnoreMatcher: options.ignoreMatcher, Configuration: options.configuration})
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
