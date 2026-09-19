package native

import (
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceestimate"
	"testing"
)

func TestGoDeclarationOnlyAttribution(t *testing.T) {
	for _, source := range []string{"package p; type Request struct { Value int }; type Service interface { Run(Request) error }", "package p; const A=1"} {
		if !goDeclarationOnly([]byte(source)) {
			t.Fatal(source)
		}
	}
	for _, source := range []string{"package p; var X = audit()", "package p; func Run() {}", "package p; type Broken struct {", "package p; func External()"} {
		if goDeclarationOnly([]byte(source)) {
			t.Fatal(source)
		}
	}
	value := 49.0
	boundary := report.DepthBoundary{ID: "pkg", Scope: "package", State: "partial", Estimated: true, Shallow: &value, DeclarationFiles: []string{"service.go"}}
	for _, path := range []string{"service.go", "implementation.go"} {
		component, err := scoreDepthComponent(depthDescriptor(), "", map[string]report.DepthBoundary{"pkg": boundary}, []string{"pkg"}, "partial", path)
		if err != nil {
			t.Fatal(err)
		}
		if path == "service.go" {
			if *component.RawMaximum != 0 || component.Contribution != 0 || component.DepthRole != "declaration-only-contract" || component.DepthScope != "file" {
				t.Fatalf("%+v", component)
			}
		} else if *component.RawMaximum != 49 || component.DepthScope != "file" || !component.DepthEstimated {
			t.Fatalf("%+v", component)
		}
	}
}

func TestOuterContractDoesNotSuppressNestedBehavior(t *testing.T) {
	file := sourceestimate.File{Path: "Choice.java", Language: "java", Source: []byte(`public sealed interface Choice permits Choice.Value { record Value(String text) implements Choice { public Value { java.util.Objects.requireNonNull(text); } } }`)}
	_, attributed := sourceestimate.AnalyzeWithAttribution([]sourceestimate.File{file})
	if !attributed[file.Path].Applicable {
		t.Fatal("nested constructor not inventoried")
	}
	inputs := newScoreInputs()
	inputs.depth["contract"] = report.DepthBoundary{ID: "contract", State: "not_applicable", Files: []string{file.Path}, Boundary: map[string]any{"symbol": "Choice"}}
	inputs.depthByPath[file.Path] = []string{"contract"}
	projectAttributedFileDepth(&inputs, file, attributed[file.Path])
	var numeric bool
	for _, id := range inputs.depthByPath[file.Path] {
		b := inputs.depth[id]
		if needsSourceDepth(b) {
			b = estimateDepthBoundary(b, attributed[file.Path])
			numeric = numeric || b.Shallow != nil
		}
	}
	if !numeric {
		t.Fatal("outer interface N/A suppressed nested implementation")
	}
}
