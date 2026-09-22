package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/sourcefs"
	"github.com/blater/slopwatch/internal/unitplan"
)

type workspaceChangedPaths struct{ Paths []string }

func (err *workspaceChangedPaths) Error() string { return ErrWorkspaceChanged.Error() }
func (err *workspaceChangedPaths) Unwrap() error { return ErrWorkspaceChanged }

func readNamedSource(fs sourcefs.FileSystem, root, path string) (*unitplan.Source, error) {
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := fs.Stat(absolute)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	stamp := analysiscache.FileStamp{Size: info.Size(), Modified: info.ModTime().UnixNano(), Changed: fileChangeTime(info)}
	data, err := fs.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	after, err := fs.Stat(absolute)
	if err != nil || after.Size() != stamp.Size || after.ModTime().UnixNano() != stamp.Modified || fileChangeTime(after) != stamp.Changed {
		return nil, &workspaceChangedPaths{Paths: []string{path}}
	}
	source := unitplan.ParseSource(path, data)
	source.Digest = analysiscache.Digest(source.DataDigest)
	source.Stamp = stamp
	return source, nil
}
func initializeSession(analyzer *analysisEngine, snapshot *analysisPlanSnapshot) error {
	if snapshot == nil || snapshot.index == nil {
		return ErrIncrementalPlanUnavailable
	}
	fs := sourcefs.Default(analyzer.fileSystem)
	targets := make([]string, 0, len(snapshot.options.Targets))
	for _, target := range snapshot.options.Targets {
		if filepath.IsAbs(target) {
			relative, err := filepath.Rel(analyzer.workspace, target)
			if err != nil {
				return err
			}
			target = relative
		}
		targets = append(targets, filepath.ToSlash(filepath.Clean(target)))
	}
	snapshot.options.Targets = targets
	visibleUnits := map[string]bool{}
	snapshot.visibleCounts = map[string]int{}
	if err := snapshot.index.InitializeSources(func(source *unitplan.Source) error {
		source.Visible = snapshot.startupVisible[source.Path]
		source.Owner = canonicalOwner(snapshot.index, source.Path, snapshot.options)
		if source.Visible {
			snapshot.visibleCounts[string(source.Language)]++
			if source.Owner != "" {
				visibleUnits[source.Owner] = true
			}
		}
		return nil
	}); err != nil {
		return err
	}
	active, _ := indexedVisibleUnits(snapshot.index, visibleUnits, snapshot.options)
	plans := indexedRequestClosure(snapshot.index, active, snapshot.options)
	selected := map[string]bool{}
	for id := range plans {
		selected[id] = true
	}
	snapshot.initialInputs = inputPathsForUnits(plans, selected)
	return snapshot.index.InitializeSources(func(source *unitplan.Source) error {
		actual, err := readNamedSource(fs, analyzer.workspace, source.Path)
		if err == nil && (actual == nil || source.DataDigest != "" && source.DataDigest != actual.DataDigest) {
			err = &workspaceChangedPaths{Paths: []string{source.Path}}
		}
		if err != nil {
			if snapshot.initialInputs[source.Path] {
				return err
			}
			// Retain inventory/facts, but never publish an unverified digest. A later
			// request needing this source must revalidate it through named pending work.
			source.Digest = ""
			return nil
		}
		source.Digest = actual.Digest
		source.Stamp = actual.Stamp
		return nil
	})
}
func verifyInitialSession(analyzer *analysisEngine, snapshot *analysisPlanSnapshot) error {
	if snapshot == nil {
		return nil
	}
	fs := sourcefs.Default(analyzer.fileSystem)
	for path := range snapshot.initialInputs {
		if source, ok := snapshot.index.Source(path); ok {
			if err := verifySourceStamp(fs, analyzer.workspace, source); err != nil {
				return err
			}
		}
	}
	return nil
}
func verifySourceStamp(fs sourcefs.FileSystem, root string, source *unitplan.Source) error {
	if source.Digest == "" {
		return &workspaceChangedPaths{Paths: []string{source.Path}}
	}
	info, err := fs.Stat(filepath.Join(root, filepath.FromSlash(source.Path)))
	if os.IsNotExist(err) {
		return &workspaceChangedPaths{Paths: []string{source.Path}}
	}
	if err != nil {
		return err
	}
	stamp := analysiscache.FileStamp{Size: info.Size(), Modified: info.ModTime().UnixNano(), Changed: fileChangeTime(info)}
	if stamp != source.Stamp {
		return &workspaceChangedPaths{Paths: []string{source.Path}}
	}
	return nil
}
func visibleSource(options Options, path, language string) bool {
	if language == "" || len(options.Languages) > 0 && !pathSet(options.Languages)[language] {
		return false
	}
	if _, ok := discoveredLanguage(path, options.IncludeTests); !ok {
		return false
	}
	if excludedDiscoveryDirectory(path, options.IncludeTests) {
		return false
	}
	if len(options.Targets) == 0 {
		return true
	}
	for _, target := range options.Targets {
		target = filepath.ToSlash(filepath.Clean(target))
		if target == "." || path == target || strings.HasPrefix(path, target+"/") {
			return true
		}
	}
	return false
}
func canonicalOwner(view unitplan.Lookup, path string, options Options) string {
	var owner unitplan.Unit
	for _, unit := range view.Owners(path) {
		if eligibleUnit(unit, options) && (owner.ID == "" || preferPathOwner(unit, owner)) {
			owner = unit
		}
	}
	return owner.ID
}
func eligibleUnit(unit unitplan.Unit, options Options) bool {
	if len(options.Languages) > 0 && !pathSet(options.Languages)[string(unit.Language)] {
		return false
	}
	return options.IncludeTests || !hasCapability(unit, unitplan.CapabilityTests)
}
func effectiveChangeUnit(unit unitplan.Unit, options Options) unitplan.Unit {
	if !options.IncludeTests {
		unit.Sources = withoutTestPaths(unit.Sources, string(unit.Language))
		unit.ContextSources = withoutTestPaths(unit.ContextSources, string(unit.Language))
	}
	if unit.Language == unitplan.LanguageTypeScript {
		unit.Sources = withoutTypeScriptDeclarations(unit.Sources)
	}
	return unit
}
func prepareNamedChanges(analyzer *Analyzer, ctx context.Context) (map[string]*unitplan.Source, map[string]*unitplan.Source, error) {
	changes := map[string]*unitplan.Source{}
	substantive := map[string]*unitplan.Source{}
	snapshot := analyzer.plan
	fs := sourcefs.Default(analyzer.fileSystem)
	for path := range snapshot.pending {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		previous, existed := snapshot.index.Source(path)
		if unitplan.SourceLanguage(path) == "" {
			continue
		}
		eligible, err := namedSourceEligible(fs, analyzer.workspace, path, snapshot.options)
		if err != nil {
			return nil, nil, err
		}
		var next *unitplan.Source
		if eligible {
			next, err = readNamedSource(fs, analyzer.workspace, path)
		}
		if err != nil {
			return nil, nil, err
		}
		if !existed && next == nil {
			continue
		}
		if existed && next != nil && previous.Digest == next.Digest && !previous.NeedsRetry {
			if previous.Stamp != next.Stamp {
				copy := *previous
				copy.Stamp = next.Stamp
				changes[path] = &copy
			}
			continue
		}
		if next != nil {
			next.Visible = visibleSource(snapshot.options, path, string(next.Language))
		}
		changes[path] = next
		substantive[path] = next
	}
	return changes, substantive, nil
}
func retainChangedError(snapshot *analysisPlanSnapshot, err error) {
	var changed *workspaceChangedPaths
	if errors.As(err, &changed) {
		for _, path := range changed.Paths {
			snapshot.pending[path] = true
		}
	}
}

func verifyNamedDelta(analyzer *Analyzer, changes map[string]*unitplan.Source) error {
	fs := sourcefs.Default(analyzer.fileSystem)
	for path, source := range changes {
		eligible, err := namedSourceEligible(fs, analyzer.workspace, path, analyzer.plan.options)
		if err != nil {
			return err
		}
		if !eligible {
			if source != nil {
				return &workspaceChangedPaths{Paths: []string{path}}
			}
			continue
		}
		if source != nil {
			if err := verifySourceStamp(fs, analyzer.workspace, source); err != nil {
				return err
			}
			continue
		}
		info, err := fs.Stat(filepath.Join(analyzer.workspace, filepath.FromSlash(path)))
		if err == nil && info.Mode().IsRegular() {
			return &workspaceChangedPaths{Paths: []string{path}}
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Classify only the named logical path and its ancestors. Explicit targets are
// entry points, as in walkTarget: their own links/ancestors are authorized, but
// nested links below a directory target still obey FollowSymlinks.
func namedSourceEligible(fs sourcefs.FileSystem, root, path string, options Options) (bool, error) {
	if options.ignoreMatcher.Ignored(filepath.Join(root, filepath.FromSlash(path)), false) {
		return false, nil
	}
	if options.FollowSymlinks {
		return true, nil
	}
	boundary := "."
	for _, target := range options.Targets {
		target = filepath.ToSlash(filepath.Clean(target))
		if path == target || strings.HasPrefix(path, target+"/") {
			if boundary == "." || len(target) > len(boundary) {
				boundary = target
			}
		}
	}
	if path == boundary {
		return true, nil
	}
	relative := path
	if boundary != "." {
		relative = strings.TrimPrefix(path, boundary+"/")
	}
	current := boundary
	for _, part := range strings.Split(relative, "/") {
		current = filepath.ToSlash(filepath.Join(current, part))
		info, err := fs.Lstat(filepath.Join(root, filepath.FromSlash(current)))
		if os.IsNotExist(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, nil
		}
	}
	return true, nil
}
