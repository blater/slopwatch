package native

import (
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/sourceestimate"
	"github.com/blater/slopwatch/internal/unitplan"
)

func TestCalibrationDoesNotChangeCacheIdentity(t *testing.T) {
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
	input := unitKeyInput(unitplan.Unit{}, nil, nil, catalog, options)
	before, err := analysiscache.UnitKey(input)
	if err != nil {
		t.Fatal(err)
	}
	after, err := analysiscache.UnitKey(input)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("cache identity changed without a source or scope change")
	}
	if current == changed {
		t.Fatal("calibration fixture did not produce distinct identities")
	}
}

func TestCachedProjectionIgnoresStoredCalibrationIdentity(t *testing.T) {
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
	schema, _, profile, policy, err := reportIdentity(active)
	if err != nil {
		t.Fatal(err)
	}
	changed := sourceestimate.DefaultCalibration()
	changed.Validation *= 1.2
	_, otherHash, _, _, err := reportIdentityWithCalibration(active, changed.Identity())
	if err != nil {
		t.Fatal(err)
	}
	projection := analysiscache.DisplayProjection{ViewKey: view, SchemaVersion: schema, ProfileSetHash: otherHash, ScoreProfile: profile, PolicyRevision: policy, ScorePolicyRevision: StructuralScoringPolicyRevision, Files: []analysiscache.DisplayFile{{Path: "service.go", Language: "go", Complete: true, Freshness: analysiscache.FreshnessCurrent}}}
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
	if _, ok := analyzer.CachedProjection(); !ok {
		t.Fatal("cache projection was rejected based on calibration identity")
	}
}
