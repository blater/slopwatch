package goadapter

import (
	"os"
	"path/filepath"
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestDepthFileBoundariesIsolateExportedFreeFunctions(t *testing.T) {
	root := t.TempDir()
	writeGoDepthFile(t, root, "first.go", `package p; func First(x int) int { return x + 1 }`)
	writeGoDepthFile(t, root, "second.go", `package p; func Second(x int) int { return x * 2 }`)
	program, err := (Adapter{}).Analyze(root, []string{"first.go", "second.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	if program.Depth == nil || len(program.Depth.Boundaries) != 2 {
		t.Fatalf("depth boundaries = %#v", program.Depth)
	}
	for _, boundary := range program.Depth.Boundaries {
		if len(boundary.Files) != 1 || len(boundary.RouteFamilies) != 1 || len(boundary.RouteFamilies[0].Routes) != 1 {
			t.Fatalf("sibling route leaked into %s: %#v", boundary.Identity.Symbol, boundary)
		}
		if boundary.Files[0] == "first.go" && boundary.RouteFamilies[0].Routes[0].ID != ".|p/First" {
			t.Fatalf("first route = %#v", boundary.RouteFamilies[0])
		}
		if boundary.Files[0] == "second.go" && boundary.RouteFamilies[0].Routes[0].ID != ".|p/Second" {
			t.Fatalf("second route = %#v", boundary.RouteFamilies[0])
		}
	}
}

func TestDepthFileBoundaryKeepsPrivateHelperWithItsCaller(t *testing.T) {
	root := t.TempDir()
	writeGoDepthFile(t, root, "service.go", `package p; func helper(x int) int { return x + 1 }; func Service(x int) int { return helper(x) }`)
	writeGoDepthFile(t, root, "unrelated.go", `package p; func Unrelated(x int) int { return x * x }`)
	program, err := (Adapter{}).Analyze(root, []string{"service.go", "unrelated.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	service := depthFileBoundary(program.Depth, "service.go")
	if service == nil || len(service.RouteFamilies) != 1 || service.RouteFamilies[0].ID != ".|p/helper" {
		t.Fatalf("caller/helper family = %#v", service)
	}
	unrelated := depthFileBoundary(program.Depth, "unrelated.go")
	if unrelated == nil || len(unrelated.RouteFamilies) != 1 || unrelated.RouteFamilies[0].ID != ".|p/Unrelated" {
		t.Fatalf("unrelated family = %#v", unrelated)
	}
}

func TestDepthFileBoundaryRepeatsFullSplitReceiverSurface(t *testing.T) {
	root := t.TempDir()
	writeGoDepthFile(t, root, "one.go", `package p; type Service struct{}; func (Service) One(x int) int { return x + 1 }`)
	writeGoDepthFile(t, root, "two.go", `package p; func (Service) Two(x int) int { return x * 2 }`)
	program, err := (Adapter{}).Analyze(root, []string{"one.go", "two.go"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"one.go", "two.go"} {
		boundary := depthFileBoundary(program.Depth, path)
		if boundary == nil || len(boundary.RouteFamilies) != 2 {
			t.Fatalf("receiver surface for %s = %#v", path, boundary)
		}
	}
}

func writeGoDepthFile(t *testing.T, root, name, source string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
}

func depthFileBoundary(depth *facts.DepthFacts, path string) *facts.BoundaryAssessment {
	if depth == nil {
		return nil
	}
	for index := range depth.Boundaries {
		if len(depth.Boundaries[index].Files) == 1 && depth.Boundaries[index].Files[0] == path {
			return &depth.Boundaries[index]
		}
	}
	return nil
}
