package native

import (
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
)

func TestCachedProjectionDoesNotRequireIdentityMetadata(t *testing.T) {
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
	projection := analysiscache.DisplayProjection{
		ViewKey: view,
		Files:   []analysiscache.DisplayFile{{Path: "service.go", Language: "go", Complete: true, Freshness: analysiscache.FreshnessCurrent}},
	}
	ref, err := store.PutProjection(view, projection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitGeneration(view, analysiscache.Generation{Projection: ref}); err != nil {
		t.Fatal(err)
	}
	if _, ok := analyzer.CachedProjection(); !ok {
		t.Fatal("cache projection was rejected because identity metadata was absent")
	}
}

func TestCachedProjectionAcceptsPriorPolicyMetadata(t *testing.T) {
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
	projection := analysiscache.DisplayProjection{
		ViewKey:             view,
		SchemaVersion:       3,
		ProfileSetHash:      "old-profile",
		ScoreProfile:        "old-profile",
		PolicyRevision:      "r36",
		ScorePolicyRevision: "old-scoring-policy",
		Files:               []analysiscache.DisplayFile{{Path: "service.go", Language: "go", Complete: true, Freshness: analysiscache.FreshnessCurrent}},
	}
	ref, err := store.PutProjection(view, projection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitGeneration(view, analysiscache.Generation{Projection: ref}); err != nil {
		t.Fatal(err)
	}
	if _, ok := analyzer.CachedProjection(); !ok {
		t.Fatal("cache projection was rejected because stored policy metadata was old")
	}
}
