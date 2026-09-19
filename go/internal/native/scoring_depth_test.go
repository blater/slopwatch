package native

import (
	"reflect"
	"testing"
)

func TestDepthV4UsesMaximumAndRetainsBoundaryLedger(t *testing.T) {
	path := "service.go"
	na := depthRecord(path, "na", "not_applicable", 0)
	na.Value = nil
	na.Attributes["depth"].(map[string]any)["shallow"] = nil
	records := []protocolRecord{
		na,
		depthRecord(path, "small", "measured", 30),
		depthRecord(path, "large", "measured", 70),
		{Type: "coverage", Component: "module_shallowness", Definition: "responsibility-burden-v4", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"},
	}
	descriptor := depthDescriptor()
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{descriptor}}, []string{"go"}, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := document.Files[0].Components["module_shallowness"]
	if component.RawMaximum == nil || *component.RawMaximum != 70 || len(component.Subjects) != 2 {
		t.Fatalf("depth maximum/subjects = %#v", component)
	}
	if got, want := component.DepthBoundaryIDs, []string{"large", "na", "small"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("depth boundary index = %#v, want %#v", got, want)
	}
	if component.Contribution == 0 || component.DepthState != "measured" {
		t.Fatalf("depth score = %#v", component)
	}
	if len(document.Depth) != 3 || len(document.Depth["small"].Files) != 1 || document.Depth["small"].PolicyRevision != "r7" || document.Depth["small"].InventoryFingerprint != "inventory-small" {
		t.Fatalf("depth ledger = %#v", document.Depth)
	}
	if document.Files[0].Components["module_shallowness"].DepthState != "measured" {
		t.Fatalf("N/A changed measured aggregate: %#v", document.Files[0].Components["module_shallowness"])
	}
}

func TestDepthV4NullableStatesDoNotBecomeZeroSubjects(t *testing.T) {
	path := "service.go"
	na := depthRecord(path, "na", "not_applicable", 0)
	na.Value = nil
	na.Attributes["depth"].(map[string]any)["shallow"] = nil
	partial := depthRecord(path, "partial", "partial", 0)
	partial.Value = nil
	partial.Attributes["depth"].(map[string]any)["shallow"] = nil
	measured := depthRecord(path, "measured", "measured", 70)
	records := []protocolRecord{na, partial, measured, {Type: "coverage", Component: "module_shallowness", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"}}
	threshold := 100.0
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, records, &threshold)
	if err != nil {
		t.Fatal(err)
	}
	component := document.Files[0].Components["module_shallowness"]
	if component.DepthState != "partial" || component.RawMaximum == nil || *component.RawMaximum != 70 || len(component.Subjects) != 1 || component.Subjects[0].Value != 70 || component.Contribution == 0 {
		t.Fatalf("partial depth aggregation = %#v", component)
	}
	if document.Files[0].Complete {
		t.Fatal("partial depth was presented as complete")
	}
	if document.Files[0].ValidZero || document.Files[0].Passed == nil || *document.Files[0].Passed {
		t.Fatalf("partial depth was presented as valid zero/pass: %#v", document.Files[0])
	}
	if document.Depth["na"].Shallow != nil || document.Depth["na"].State != "not_applicable" {
		t.Fatalf("N/A boundary = %#v", document.Depth["na"])
	}
}

func TestDepthV4UnidentifiedUnavailableInvalidatesFile(t *testing.T) {
	path := "empty.go"
	unknown := depthRecord(path, "", "unavailable", 0)
	unknown.Value = nil
	unknown.Attributes["depth"].(map[string]any)["shallow"] = nil
	records := []protocolRecord{unknown, {Type: "coverage", Component: "module_shallowness", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"}}
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := document.Files[0].Components["module_shallowness"]
	if component.DepthVersion == "" || component.DepthState != "unavailable" || document.Files[0].Complete || document.Files[0].ValidZero {
		t.Fatalf("unidentified unavailable depth was measured as zero: file=%#v component=%#v", document.Files[0], component)
	}
}

func TestDepthV4ConflictingMeasuredScoresInvalidateFile(t *testing.T) {
	path := "service.go"
	records := []protocolRecord{depthRecord(path, "boundary", "measured", 30), depthRecord(path, "boundary", "measured", 70), {Type: "coverage", Component: "module_shallowness", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"}}
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := document.Files[0].Components["module_shallowness"]
	if component.DepthState != "unavailable" || document.Files[0].Complete || document.Files[0].ValidZero {
		t.Fatalf("conflicting depth scores were accepted: file=%#v component=%#v", document.Files[0], component)
	}
}

func TestDepthV4UnidentifiedUnavailableInvalidatesMeasuredPath(t *testing.T) {
	path := "service.go"
	unknown := depthRecord(path, "", "unavailable", 0)
	unknown.Value = nil
	unknown.Attributes["depth"].(map[string]any)["shallow"] = nil
	records := []protocolRecord{depthRecord(path, "boundary", "measured", 70), unknown, {Type: "coverage", Component: "module_shallowness", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"}}
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := document.Files[0].Components["module_shallowness"]
	if component.DepthState != "unavailable" || component.RawMaximum == nil || *component.RawMaximum != 70 || component.Contribution == 0 || document.Files[0].Complete {
		t.Fatalf("known numeric depth was erased by unavailable boundary: file=%#v component=%#v", document.Files[0], component)
	}
}

func TestDepthV4DoesNotChangeOtherComponents(t *testing.T) {
	path := "service.go"
	records := []protocolRecord{
		depthRecord(path, "depth", "measured", 30),
		{Type: "measurement", Component: "other", Definition: "other-v1", Path: &path, Language: "go", Value: 2.0},
		{Type: "coverage", Component: "module_shallowness", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"},
		{Type: "coverage", Component: "other", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"},
	}
	other := componentDescriptor{ID: "other", Version: "other-v1", Axis: "other", Kind: "continuous", Support: map[string]string{"go": "supported"}, Defaults: componentDefaults{Enabled: true, Weight: "2", Formula: "count"}}
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{depthDescriptor(), other}}, []string{"go"}, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := document.Files[0].Components["other"].Contribution; got != 4 {
		t.Fatalf("other component changed: %v", got)
	}
}

func depthDescriptor() componentDescriptor {
	return componentDescriptor{ID: "module_shallowness", Version: "responsibility-burden-v4", Axis: "structural_core", Kind: "continuous", Aggregator: "max", Support: map[string]string{"go": "supported"}, Defaults: componentDefaults{Enabled: true, Threshold: stringPtr("20"), Weight: "5", Formula: "log-ratio"}}
}

func depthRecord(path, id, state string, shallow float64) protocolRecord {
	return protocolRecord{Type: "measurement", Component: "module_shallowness", Definition: "responsibility-burden-v4", Path: &path, Language: "go", Value: shallow, Attributes: map[string]any{"boundary_id": id, "knowledge_state": state, "policy_revision": "r7", "inventory_fingerprint": "inventory-" + id, "depth": map[string]any{"boundary": map[string]any{"symbol": id}, "state": state, "shallow": shallow, "inventory_fingerprint": "inventory-" + id}}}
}

func stringPtr(value string) *string { return &value }

func TestDepthV4MissingMeasuredLedgerIsUnavailable(t *testing.T) {
	path := "service.go"
	inputs := newScoreInputs()
	inputs.languages[path] = "go"
	inputs.coverage[path] = map[string]string{"module_shallowness": "complete"}
	inputs.depthStates[path] = "measured"
	document, err := scoreInputsReport(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	file := document.Files[0]
	component := file.Components["module_shallowness"]
	if file.Complete || file.ValidZero || component.DepthState != "unavailable" || component.RawMaximum != nil {
		t.Fatalf("missing measured ledger accepted: file=%+v", file)
	}
}

func TestDepthV4EstimatesRetainEvidenceWithoutRankingContribution(t *testing.T) {
	path := "Codec.java"
	estimate := depthRecord(path, "codec", "measured", 100)
	estimate.Attributes["depth"].(map[string]any)["estimated"] = true
	records := []protocolRecord{estimate, depthRecord(path, "known", "measured", 30),
		{Type: "coverage", Component: "module_shallowness", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"}}
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	component := document.Files[0].Components["module_shallowness"]
	if component.DepthState != "partial" || !component.DepthEstimated || component.RawMaximum == nil || *component.RawMaximum != 100 || component.Contribution == 0 || component.ObservedContribution == 0 {
		t.Fatalf("provisional estimate was not reported numerically: %+v", component)
	}
	if len(component.Evidence) != 2 || len(component.Subjects) != 2 || !document.Depth["codec"].Estimated || document.Depth["codec"].State != "partial" || *document.Depth["codec"].Shallow != 100 {
		t.Fatalf("provisional evidence lost: %+v / %+v", component, document.Depth)
	}
}

func TestDepthV4StoresCompactRawAndEvidence(t *testing.T) {
	path := "service.go"
	record := depthRecord(path, "boundary", "measured", 30)
	raw := record.Attributes["depth"].(map[string]any)
	raw["obligations"] = []any{map[string]any{"id": "obligation-1", "category": "X", "rule": "transform", "governed": "drop", "provenance": []any{"large"}}}
	raw["B8"] = float64(4)
	raw["H"] = float64(1)
	raw["burden"] = map[string]any{"O": float64(1), "T": float64(2)}
	raw["alternatives"] = []any{[]any{"obligation-1", "route-b"}}
	raw["family_hidden"] = map[string]any{"family-a": true}
	raw["estimate"] = map[string]any{"model": "source-responsibility-v1", "burden": float64(2), "hidden": float64(1)}
	raw["reasons"] = []any{map[string]any{"code": "retained-as-typed"}}
	raw["large_adapter_payload"] = map[string]any{"functions": []any{"unreferenced"}}
	records := []protocolRecord{record, {Type: "coverage", Component: "module_shallowness", Definition: "responsibility-burden-v4", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"}}
	document, err := scoreRecords(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, records, nil)
	if err != nil {
		t.Fatal(err)
	}
	boundary := document.Depth["boundary"]
	wantRaw := map[string]any{
		"obligations": []any{map[string]any{"id": "o0", "category": "X", "rule": "transform"}},
		"B8":          raw["B8"], "H": raw["H"], "burden": raw["burden"],
		"alternatives": []any{[]any{"o0", "route-b"}}, "family_hidden": raw["family_hidden"], "estimate": raw["estimate"],
	}
	if !reflect.DeepEqual(boundary.Raw, wantRaw) {
		t.Fatalf("raw payload was not compacted: got=%#v want=%#v", boundary.Raw, wantRaw)
	}
	obligation := boundary.Raw["obligations"].([]any)[0].(map[string]any)
	if len(obligation) != 3 || obligation["id"] != "o0" || obligation["governed"] != nil || obligation["provenance"] != nil {
		t.Fatalf("obligation payload was not compacted: %#v", obligation)
	}
	if _, ok := document.Files[0].Components["module_shallowness"].Evidence[0].Attributes["depth"]; ok {
		t.Fatal("depth evidence retained a duplicate raw payload")
	}
	if got := document.Files[0].Components["module_shallowness"].Evidence[0].Attributes["boundary_id"]; got != "boundary" {
		t.Fatalf("depth evidence boundary id = %#v", got)
	}
}
