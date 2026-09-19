package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
	"github.com/blater/slopwatch/internal/unitplan"
)

func TestEmptyProfileDefaultsToResponsibilityV4(t *testing.T) {
	if got := normalizeOptions(Options{}).ShallowProfile; got != ShallowProfileResponsibilityV4 {
		t.Fatalf("empty profile normalized to %q", got)
	}
	if got := normalizeOptions(Options{ShallowProfile: ShallowProfileLegacy}).ShallowProfile; got != ShallowProfileLegacy {
		t.Fatalf("explicit legacy normalized to %q", got)
	}
}

func TestActiveCatalogOnlyChangesShallowProfileComponent(t *testing.T) {
	catalog := catalogDocument{Components: []componentDescriptor{
		{ID: "module_shallowness", Version: "ousterhout-v3", Axis: "structural_core", Defaults: componentDefaults{Enabled: true}},
		{ID: "cognitive_complexity", Version: "pmd-v1", Axis: "structural_core", Defaults: componentDefaults{Enabled: true}},
		{ID: "explicit_any", Version: "typescript-local-sink-v1", Axis: "typescript_type_safety", Defaults: componentDefaults{Enabled: true}},
	}}
	legacy := activeCatalog(catalog, Options{TypeScriptTypes: true, ShallowProfile: ShallowProfileLegacy})
	profiled := activeCatalog(catalog, Options{TypeScriptTypes: true, ShallowProfile: ShallowProfileResponsibilityV4})
	defaulted := activeCatalog(catalog, Options{TypeScriptTypes: true})
	if legacy.Components[0].Version != "ousterhout-v3" {
		t.Fatalf("legacy shallow version = %q", legacy.Components[0].Version)
	}
	if profiled.Components[0].Version != ShallowDefinitionV4 {
		t.Fatalf("profiled shallow version = %q", profiled.Components[0].Version)
	}
	if defaulted.Components[0].Version != profiled.Components[0].Version {
		t.Fatalf("omitted shallow profile = %q, explicit v4 = %q", defaulted.Components[0].Version, profiled.Components[0].Version)
	}
	for index := 1; index < len(catalog.Components); index++ {
		if profiled.Components[index].Version != catalog.Components[index].Version || profiled.Components[index].Defaults.Enabled != catalog.Components[index].Defaults.Enabled {
			t.Fatalf("profile changed non-SHALLOW component %d: %#v", index, profiled.Components[index])
		}
	}
	if catalog.Components[0].Version != "ousterhout-v3" {
		t.Fatal("active profile mutated source catalog")
	}
}

func TestShallowProfileChangesWorkspaceAndUnitCacheKeys(t *testing.T) {
	workspace := t.TempDir()
	legacy := Options{Targets: []string{"."}, Languages: []string{"go"}, ShallowProfile: ShallowProfileLegacy}
	profiled := Options{Targets: []string{"."}, Languages: []string{"go"}, ShallowProfile: ShallowProfileResponsibilityV4}
	legacyView, err := viewKey(&analysisEngine{workspace: workspace}, legacy)
	if err != nil {
		t.Fatal(err)
	}
	profiledView, err := viewKey(&analysisEngine{workspace: workspace}, profiled)
	if err != nil {
		t.Fatal(err)
	}
	if legacyView == profiledView {
		t.Fatal("legacy and responsibility-v4 workspace views share a cache key")
	}
	defaultView, err := viewKey(&analysisEngine{workspace: workspace}, Options{Targets: []string{"."}, Languages: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if defaultView != profiledView {
		t.Fatal("omitted and explicit v4 profiles share no cache key")
	}

	unit := unitplan.Unit{ID: "go", Language: unitplan.LanguageGo, Mode: unitplan.ModeSyntax, Sources: []string{"main.go"}}
	catalog := catalogDocument{}
	digest := analysiscache.Digest("catalog")
	legacyKey, err := analysiscache.UnitKey(unitKeyInput(unit, nil, nil, "analyzer", digest, catalog, legacy))
	if err != nil {
		t.Fatal(err)
	}
	profiledKey, err := analysiscache.UnitKey(unitKeyInput(unit, nil, nil, "analyzer", digest, catalog, profiled))
	if err != nil {
		t.Fatal(err)
	}
	if legacyKey == profiledKey {
		t.Fatal("legacy and responsibility-v4 units share a cache key")
	}
	if samePlanOptions(legacy, profiled) {
		t.Fatal("incremental plan options ignored shallow profile")
	}
}

func TestResponsibilityProfileAddsTypeScriptEvaluatorContract(t *testing.T) {
	analyzer := &analysisEngine{root: "/installation"}
	legacy := analyzerRequestOptions(analyzer, Options{ShallowProfile: ShallowProfileLegacy}, "typescript", "off")
	if _, ok := legacy["depth_evaluator_path"]; ok {
		t.Fatal("legacy TypeScript request unexpectedly selected a depth evaluator")
	}
	profiled := analyzerRequestOptions(analyzer, Options{ShallowProfile: ShallowProfileResponsibilityV4}, "typescript", "off")
	if got := profiled["depth_policy_revision"]; got != ShallowPolicyRevisionV4 {
		t.Fatalf("depth policy revision = %#v", got)
	}
	want := "/installation/analyzers/structural/slopslap-structural"
	if got := profiled["depth_evaluator_path"]; got != want {
		t.Fatalf("depth evaluator path = %#v, want %q", got, want)
	}
}

func TestTypeScriptDepthCacheTracksSharedEvaluator(t *testing.T) {
	root := t.TempDir()
	analyzer := &analysisEngine{root: root}
	write := func(language, content string) {
		t.Helper()
		path := analyzerExecutable(root, language)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	digest := func(options Options) analysiscache.Digest {
		t.Helper()
		result, err := backendDigest(analyzer, "typescript", options)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	write("typescript", "typescript analyzer")
	legacy := digest(Options{ShallowProfile: ShallowProfileLegacy}) // Legacy does not require the shared evaluator.
	write("go", "evaluator before")
	profile := Options{ShallowProfile: ShallowProfileResponsibilityV4}
	before := digest(profile)
	write("go", "evaluator after")
	if before == digest(profile) {
		t.Fatal("changed shared evaluator reused TypeScript v4 cache identity")
	}
	if legacy != digest(Options{ShallowProfile: ShallowProfileLegacy}) {
		t.Fatal("shared evaluator changed legacy TypeScript cache identity")
	}
}
