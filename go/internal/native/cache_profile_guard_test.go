package native

import (
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/unitplan"
)

func TestV4CachedProjectionRejectsLegacyMetadata(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, workspace, "service.go", "package sample\nfunc Run() {}\n")
	store, err := analysiscache.NewStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true, ShallowProfile: ShallowProfileResponsibilityV4}
	analyzer := newCacheTestAnalyzer(t, workspace, options, store, catalogDocument{Components: []componentDescriptor{depthDescriptor()}})
	view, err := analyzer.viewKey(options)
	if err != nil {
		t.Fatal(err)
	}
	projection := analysiscache.DisplayProjection{ViewKey: view, Files: []analysiscache.DisplayFile{{Path: "service.go", Language: "go", Complete: true, Freshness: analysiscache.FreshnessCurrent}}}
	ref, err := store.PutProjection(view, projection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitGeneration(view, analysiscache.Generation{Projection: ref}); err != nil {
		t.Fatal(err)
	}
	if _, ok := analyzer.CachedProjection(); ok {
		t.Fatal("v4 accepted projection without schema/profile/policy metadata")
	}
}

func TestV4GuardPolicyInvalidatesR36(t *testing.T) {
	testV4PolicyInvalidation(t, "r36")
}

func TestV4LookupPolicyInvalidatesR37(t *testing.T) {
	testV4PolicyInvalidation(t, "r37")
}

func testV4PolicyInvalidation(t *testing.T, oldPolicy string) {
	t.Helper()
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
	oldView, err := analysiscache.WorkspaceViewKey(workspace, analysiscache.ViewOptions{Targets: []string{".", "\x00shallow-profile=" + ShallowProfileResponsibilityV4 + "\x00policy=" + oldPolicy}, Languages: options.Languages})
	if err != nil {
		t.Fatal(err)
	}
	if view == oldView {
		t.Fatal(oldPolicy + " display view remains reusable")
	}
	input := unitKeyInput(unitplan.Unit{}, nil, nil, "analyzer", "catalog", catalog, options)
	currentKey, err := analysiscache.UnitKey(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Toolchain["shallow_policy_revision"] = oldPolicy
	oldKey, err := analysiscache.UnitKey(input)
	if err != nil {
		t.Fatal(err)
	}
	if currentKey == oldKey {
		t.Fatal(oldPolicy + " analysis unit remains reusable")
	}
	schema, hash, profile, policy, err := reportIdentity(activeCatalog(catalog, options))
	if err != nil {
		t.Fatal(err)
	}
	projection := analysiscache.DisplayProjection{ViewKey: view, SchemaVersion: schema, ProfileSetHash: hash, ScoreProfile: profile, PolicyRevision: oldPolicy, Files: []analysiscache.DisplayFile{{Path: "service.go", Language: "go", Complete: true, Freshness: analysiscache.FreshnessCurrent}}}
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
		t.Fatal(oldPolicy + " projection accepted under current view")
	}
	projection.PolicyRevision = policy
	projection.ScorePolicyRevision = StructuralScoringPolicyRevision
	put()
	if _, ok := analyzer.CachedProjection(); !ok {
		t.Fatal("current policy projection rejected")
	}
}

func TestV4StructuralResponsibilityPolicyInvalidatesR38(t *testing.T) {
	testV4PolicyInvalidation(t, "r39")
}

func TestV4RejectsPriorDiagnosticPolicy(t *testing.T) {
	testV4PolicyInvalidation(t, "r40")
}
