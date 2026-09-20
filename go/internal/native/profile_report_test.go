package native

import (
	"testing"

	"github.com/blater/slopwatch/internal/analysiscache"
)

func TestDepthV4ReportIdentityIsVersionedAndCatalogSensitive(t *testing.T) {
	legacyDescriptor := depthDescriptor()
	legacyDescriptor.Version = "ousterhout-v3"
	legacy, err := scoreInputsReport(catalogDocument{Components: []componentDescriptor{legacyDescriptor}}, []string{"go"}, newScoreInputs(), nil)
	if err != nil {
		t.Fatal(err)
	}
	v4, err := scoreInputsReport(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, newScoreInputs(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.SchemaVersion != 3 || legacy.ProfileSetHash == "" || legacy.ProfileSetHash == "native-balanced-v1" || legacy.ScorePolicyRevision != StructuralScoringPolicyRevision || v4.SchemaVersion != 4 || v4.ScoreProfile != ShallowProfileResponsibilityV4 || v4.PolicyRevision != ShallowPolicyRevisionV4 || v4.ScorePolicyRevision != StructuralScoringPolicyRevision || v4.ProfileSetHash == legacy.ProfileSetHash {
		t.Fatalf("profile report identity mismatch: legacy=%#v v4=%#v", legacy, v4)
	}
	changed := depthDescriptor()
	changed.Defaults.Weight = "6"
	changedReport, err := scoreInputsReport(catalogDocument{Components: []componentDescriptor{changed}}, []string{"go"}, newScoreInputs(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if changedReport.ProfileSetHash == v4.ProfileSetHash {
		t.Fatal("v4 report identity ignored catalog weight")
	}
}

func TestDepthV4ProjectionPreservesProfileAndComponentMetadata(t *testing.T) {
	path := "service.go"
	inputs, err := collectScoreInputs([]protocolRecord{depthRecord(path, "boundary", "measured", 30), {Type: "coverage", Component: "module_shallowness", Path: &path, Language: "go", UnitID: "go-unit", State: "complete"}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := scoreInputsReport(catalogDocument{Components: []componentDescriptor{depthDescriptor()}}, []string{"go"}, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	projection := analysiscache.ProjectionFromReport("view", document, analysiscache.FreshnessCurrent)
	if projection.SchemaVersion != 4 || projection.ProfileSetHash != document.ProfileSetHash || projection.PolicyRevision != ShallowPolicyRevisionV4 || projection.ScorePolicyRevision != StructuralScoringPolicyRevision || len(projection.Depth) != 1 {
		t.Fatalf("v4 projection metadata lost: %#v", projection)
	}
	files := projection.ReportFiles()
	component := files[0].Components["module_shallowness"]
	if component.DepthVersion != ShallowDefinitionV4 || component.DepthState != "measured" || component.RawMaximum == nil {
		t.Fatalf("v4 component projection lost metadata: %#v", component)
	}
}
