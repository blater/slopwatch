package follow

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func v4DisplayFixture(state string) (report.Document, report.File) {
	file := report.File{
		Path: "service.ts", Rank: 1, Coverage: map[string]string{"module_shallowness": "complete"},
		Components: map[string]report.Component{"module_shallowness": {
			DepthVersion: "responsibility-burden-v4", DepthState: state,
			DepthBoundaryIDs: []string{"10:service.ts8:external6:module10:service.ts"},
			Subjects:         []report.SubjectContribution{{Subject: "service.ts#run", Value: 70}},
		}},
	}
	document := report.Document{Files: []report.File{file}, Depth: map[string]report.DepthBoundary{
		"10:service.ts8:external6:module10:service.ts": {
			ID: "10:service.ts8:external6:module10:service.ts", State: state, Estimated: state == "partial", Files: []string{"service.ts"},
			Raw: map[string]any{"obligations": []any{map[string]any{"category": "X", "rule": "connected_numeric_outcome"}},
				"estimate": map[string]any{"model": "source-responsibility-v1", "burden": float64(4), "hidden": float64(2), "categories": map[string]any{"X": float64(1)}},
			},
			Reasons:  []any{map[string]any{"code": "incomplete_flow_evaluation", "message": "incomplete flow"}},
			Evidence: []any{map[string]any{"kind": "supporting-contract-v1", "details": map[string]any{"member": "service.ts#run", "contract_member": "Contract.run", "consumer": "Service", "slot": "arg0"}}},
		},
	}}
	return document, file
}

func TestV4ShallowDisplayDistinguishesMeasuredNAAndIncomplete(t *testing.T) {
	_, file := v4DisplayFixture("measured")
	maximum := 70.0
	file.Components["module_shallowness"] = report.Component{DepthVersion: "responsibility-burden-v4", DepthState: "measured", RawMaximum: &maximum, Subjects: []report.SubjectContribution{{Value: 70}, {Value: 30}}}
	if text := ansi.Strip(renderMetricCell(file, columnDefinitions[4], style.SurfaceScreen)); !strings.Contains(text, "70") || strings.Contains(text, "100") {
		t.Fatalf("measured v4 cell = %q, want raw maximum 70", text)
	}
	component := file.Components["module_shallowness"]
	component.DepthEstimated = true
	component.RawMaximum = &maximum
	file.Components["module_shallowness"] = component
	if text := ansi.Strip(renderMetricCell(file, columnDefinitions[4], style.SurfaceScreen)); !strings.Contains(text, "70") || strings.Contains(text, "X") {
		t.Fatalf("provisional estimate was hidden instead of displayed numerically: %q", text)
	}

	_, file = v4DisplayFixture("not_applicable")
	if text := ansi.Strip(renderMetricCell(file, columnDefinitions[4], style.SurfaceScreen)); !strings.Contains(text, "N/A") {
		t.Fatalf("N/A v4 cell = %q", text)
	}
	_, file = v4DisplayFixture("partial")
	if text := ansi.Strip(renderMetricCell(file, columnDefinitions[4], style.SurfaceScreen)); !strings.Contains(text, "X") || strings.Contains(text, "0") {
		t.Fatalf("partial v4 cell = %q, want X without fake zero", text)
	}
}

func TestV4NumericEstimateDisplaysAndSortsWithFailedCoverage(t *testing.T) {
	lowValue, highValue := 30.0, 70.0
	low := report.File{Path: "low.go", Coverage: map[string]string{"module_shallowness": "failed"}, Components: map[string]report.Component{
		"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "partial", DepthEstimated: true, RawMaximum: &lowValue},
	}}
	high := report.File{Path: "high.go", Coverage: map[string]string{"module_shallowness": "failed"}, Components: map[string]report.Component{
		"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "partial", DepthEstimated: true, RawMaximum: &highValue},
	}}
	if value, exists, _ := metric(high, "deep"); !exists || value != 70 || metricFailed(high, "deep") {
		t.Fatalf("failed coverage hid numeric estimate: value=%v exists=%v", value, exists)
	}
	if filesLess("deep", false, high, low) || !filesLess("deep", false, low, high) {
		t.Fatalf("numeric estimates were not sortable: high=%#v low=%#v", high, low)
	}
	if text := ansi.Strip(renderMetricCell(high, columnDefinitions[4], style.SurfaceScreen)); !strings.Contains(text, "70") || strings.Contains(text, "X") {
		t.Fatalf("numeric estimate was not displayed: %q", text)
	}
}

func TestV4ShallowDetailAndInfoShowLedgerReasonsAndEvidence(t *testing.T) {
	document, file := v4DisplayFixture("partial")
	model := Model{files: FilesState{Document: document}, options: Options{Limit: 10}, infoKey: "deep", width: 80}
	detail := ansi.Strip(strings.Join(detailContent(model, file, 100), "\n"))
	if !strings.Contains(detail, "responsibility X: connected numeric outcome") || !strings.Contains(detail, "SHALLOW V4 EVIDENCE") || !strings.Contains(detail, "evidence: service.ts#run supports Contract.run through production binding Service/arg0") || !strings.Contains(detail, "estimate model: source-responsibility-v1") || !strings.Contains(detail, "recognized hidden responsibility: 2") {
		t.Fatalf("detail omitted v4 ledger evidence: %q", detail)
	}
	info := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(infoView(model)), "│", " ")), " ")
	if strings.Contains(info, "incomplete flow") || !strings.Contains(info, "service.ts#run supports Contract.run through production binding Service/arg0") {
		t.Fatalf("info omitted v4 ledger evidence: %q", info)
	}
}

func TestV4DisplayDoesNotScanUnindexedLedger(t *testing.T) {
	file := report.File{Path: "service.go", Components: map[string]report.Component{
		"module_shallowness": {DepthVersion: "responsibility-burden-v4", DepthState: "partial"},
	}}
	document := report.Document{Depth: map[string]report.DepthBoundary{
		"unindexed": {ID: "unindexed", State: "partial", Files: []string{"service.go"}},
	}}
	if got := depthBoundaries(document, file); len(got) != 0 {
		t.Fatalf("unindexed depth ledger was displayed: %#v", got)
	}
}

func TestV4PassiveCarrierDisplaysZeroAndRole(t *testing.T) {
	document, file := v4DisplayFixture("measured")
	zero := 0.0
	component := file.Components["module_shallowness"]
	component.RawMaximum = &zero
	file.Components["module_shallowness"] = component
	id := component.DepthBoundaryIDs[0]
	boundary := document.Depth[id]
	boundary.Raw, boundary.Reasons = nil, nil
	boundary.Shallow = &zero
	boundary.Evidence = []any{map[string]any{"kind": "passive-result-carrier-v1", "status": "proven"}}
	document.Depth[id] = boundary
	cell := ansi.Strip(renderMetricCell(file, columnDefinitions[4], style.SurfaceScreen))
	if !strings.Contains(cell, "0") || strings.Contains(cell, "N/A") || strings.Contains(cell, "X") {
		t.Fatalf("carrier cell: %q", cell)
	}
	info := strings.Join(depthSummaryLines(document, file), "\n")
	if !strings.Contains(info, "Passive result carrier") || !strings.Contains(info, "no penalty") || !strings.Contains(info, "does not mean exceptional functional depth") {
		t.Fatalf("carrier role missing: %s", info)
	}
}

func TestV4ValueRoleExplanations(t *testing.T) {
	for kind, role := range map[string]string{"passive-value-object-v1": "Passive value object", "passive-enum-v1": "Passive enum"} {
		text := depthEvidenceText(report.DepthBoundary{}, map[string]any{"kind": kind, "status": "proven"})
		if !strings.Contains(text, role) || !strings.Contains(text, "SHALLOW 0") || !strings.Contains(text, "no penalty") {
			t.Fatalf("%s: %s", kind, text)
		}
	}
}

func TestEstimatedModeDoesNotImplyWarningOrUnsupportedFinding(t *testing.T) {
	document, file := v4DisplayFixture("partial")
	id := file.Components["module_shallowness"].DepthBoundaryIDs[0]
	boundary := document.Depth[id]
	estimate := boundary.Raw["estimate"].(map[string]any)
	estimate["findings"] = []any{map[string]any{"kind": "unused-input", "support": "observed-callers", "operation": "service.run", "parameter": "unused", "caller_files": []string{"caller.ts"}}}
	estimate["abstractions"] = []any{map[string]any{"name": "other", "burden": 1.0, "hidden": 0.0, "published_penalty": 0.0, "limitations": []string{"incomplete_flow_evaluation", "unresolved_call_range_0_2:delegate.run/x"}}}
	document.Depth[id] = boundary
	text := strings.Join(depthSummaryLines(document, file), "\n")
	for _, want := range []string{"Estimated", "Finding unused-input", "observed caller burden", "caller.ts", "other: Delegated behavior unresolved: delegate.run"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	for _, unwanted := range []string{"incomplete flow", "incomplete behavioral", "Provisional", "ineligible", "incomplete_flow_evaluation"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("generic proof gate leaked as warning %q: %s", unwanted, text)
		}
	}
	if boundary.State != "partial" || len(boundary.Reasons) == 0 {
		t.Fatal("presentation discarded semantic diagnostics")
	}
}
