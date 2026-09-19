package native

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/sourceestimate"
	"github.com/blater/slopwatch/internal/unitplan"
)

func TestCalibrationKeysDefaultProfile(t *testing.T) {
	p := sourceestimate.DefaultCalibration()
	current := p.Identity()
	p.Input *= 1.2
	changed := p.Identity()
	options := Options{ShallowProfile: ShallowProfileResponsibilityV4}
	catalog := activeCatalog(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, options)
	_, defaultReport, _, _, err := reportIdentity(catalog)
	if err != nil {
		t.Fatal(err)
	}
	_, changedReport, _, _, err := reportIdentityWithCalibration(catalog, changed)
	if err != nil {
		t.Fatal(err)
	}
	if defaultReport == changedReport {
		t.Fatal("report ignores calibration")
	}
	input := unitKeyInput(unitplan.Unit{}, nil, nil, "analyzer", "catalog", catalog, options)
	if input.Toolchain["shallow_calibration"] != current {
		t.Fatal("unit does not use default calibration")
	}
	before, err := analysiscache.UnitKey(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Toolchain["shallow_calibration"] = changed
	after, err := analysiscache.UnitKey(input)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("unit key ignores changed calibration")
	}
	if !strings.Contains(strings.Join(cacheViewTargets(options), ""), current) {
		t.Fatal("display cache omits calibration")
	}
	legacy := Options{ShallowProfile: ShallowProfileLegacy}
	if strings.Contains(strings.Join(cacheViewTargets(legacy), ""), current) {
		t.Fatal("legacy changed by graded calibration")
	}
}

// Exercise persisted projections, not just digest comparison: an incompatible
// calibration projection cannot be reused even if stored under today's view.
func TestCalibrationCachedProjectionRejectsIncompatibleProfile(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "service.go", "package sample\nfunc Run() {}\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true, ShallowProfile: ShallowProfileResponsibilityV4}
	catalog := catalogDocument{Components: []componentDescriptor{depthDescriptor()}}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, catalog)
	view, err := analyzer.viewKey(options)
	if err != nil {
		t.Fatal(err)
	}
	active := activeCatalog(catalog, options)
	schema, currentHash, profile, policy, err := reportIdentity(active)
	if err != nil {
		t.Fatal(err)
	}
	changed := sourceestimate.DefaultCalibration()
	changed.Validation *= 1.2
	_, otherHash, _, _, err := reportIdentityWithCalibration(active, changed.Identity())
	if err != nil {
		t.Fatal(err)
	}
	projection := analysiscache.DisplayProjection{ViewKey: view, SchemaVersion: schema, ProfileSetHash: otherHash, ScoreProfile: profile, PolicyRevision: policy, Files: []analysiscache.DisplayFile{{Path: "service.go", Language: "go", Complete: true, Freshness: analysiscache.FreshnessCurrent}}}
	put := func() {
		t.Helper()
		ref, err := store.PutProjection(view, projection)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.CommitGeneration(view, analysiscache.Generation{Projection: ref}); err != nil {
			t.Fatal(err)
		}
	}
	put()
	if _, ok := analyzer.CachedProjection(); ok {
		t.Fatal("reused persisted projection from incompatible calibration")
	}
	projection.ProfileSetHash = currentHash
	put()
	if _, ok := analyzer.CachedProjection(); !ok {
		t.Fatal("rejected persisted current calibration projection")
	}
}
