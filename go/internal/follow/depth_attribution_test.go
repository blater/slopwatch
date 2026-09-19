package follow

import (
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
	"strings"
	"testing"
)

func TestDepthFileAttributionDisplay(t *testing.T) {
	value := 46.0
	file := report.File{Components: map[string]report.Component{"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthScope: "package", DepthState: "partial", DepthEstimated: true, RawMaximum: &value, DepthBoundaryIDs: []string{"pkg"}}}}
	rendered := renderMetricCell(file, columnDefinitions[4], style.SurfaceScreen)
	if !strings.Contains(rendered, "46") {
		t.Fatal(rendered)
	}
	zero := 0.0
	component := file.Components["module_shallowness"]
	component.DepthScope, component.DepthRole, component.RawMaximum = "file", "declaration-only-contract", &zero
	file.Components["module_shallowness"] = component
	document := report.Document{Depth: map[string]report.DepthBoundary{"pkg": {ID: "pkg", Scope: "package", State: "partial", Estimated: true, Shallow: &value}}}
	lines := strings.Join(depthSummaryLines(document, file), "\n")
	if !strings.Contains(lines, "file SHALLOW 0, no SCORE penalty") || strings.Contains(lines, "value contributes to SCORE") {
		t.Fatal(lines)
	}
}
