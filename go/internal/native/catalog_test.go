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
