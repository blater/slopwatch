package native

import (
	"path/filepath"
	"testing"
)

func TestBundledCatalogMatchesShallownessDefinition(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range catalog.Components {
		if component.ID == "module_shallowness" {
			if component.Version != "ousterhout-v3" {
				t.Fatalf("module_shallowness catalog version = %q, want ousterhout-v3", component.Version)
			}
			return
		}
	}
	t.Fatal("module_shallowness missing from bundled catalog")
}

func TestBundledCatalogUsesFrozenContinuousStructuralCurves(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		baseline string
		formula  string
	}{
		"cognitive_complexity":         {baseline: "0", formula: "continuous-log"},
		"cyclomatic_method_complexity": {baseline: "1", formula: "continuous-log"},
		"npath_complexity":             {baseline: "1", formula: "continuous-log"},
	}
	for _, component := range catalog.Components {
		expected, ok := want[component.ID]
		if !ok {
			continue
		}
		if component.Defaults.Formula != expected.formula || component.Defaults.Baseline == nil || *component.Defaults.Baseline != expected.baseline {
			t.Fatalf("%s defaults = %#v, want formula %q baseline %q", component.ID, component.Defaults, expected.formula, expected.baseline)
		}
		delete(want, component.ID)
	}
	if len(want) != 0 {
		t.Fatalf("curve components missing from catalog: %#v", want)
	}
}
