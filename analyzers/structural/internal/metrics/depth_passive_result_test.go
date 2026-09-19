package metrics

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestPassiveResultCarrierZeroIsRoleExemption(t *testing.T) {
	boundary := boundaryFromCase(scoringCase{ID: "carrier", State: facts.KnowledgePartial, Burden: facts.Burden{O: 4, T: 2, A: 2}})
	boundary.Evidence = []facts.Evidence{{ID: "role", Kind: "passive-result-carrier-v1", Status: "proven"}}
	score, err := ScoreBoundary(boundary)
	if err != nil {
		t.Fatal(err)
	}
	if score.Shallow == nil || *score.Shallow != 0 || score.H != 0 || score.B8 == 0 || score.Estimated || score.InventoryFingerprint != "" {
		t.Fatalf("role exemption must preserve burden, avoid inventing H, and retain fix guard: %+v", score)
	}
	boundary.Evidence[0].Status = "unknown"
	score, err = ScoreBoundary(boundary)
	if err != nil || score.Shallow != nil {
		t.Fatalf("unproven role acquired exemption: %+v %v", score, err)
	}
}

func TestPassiveValueRolesPublishZeroWithoutInventingResponsibility(t *testing.T) {
	for _, kind := range []string{"passive-value-object-v1", "passive-enum-v1"} {
		boundary := boundaryFromCase(scoringCase{ID: kind, State: facts.KnowledgePartial, Burden: facts.Burden{O: 2}})
		boundary.Evidence = []facts.Evidence{{ID: kind, Kind: kind, Status: "proven"}}
		score, err := ScoreBoundary(boundary)
		if err != nil || score.Shallow == nil || *score.Shallow != 0 || score.H != 0 || score.Estimated {
			t.Fatalf("%s: %+v %v", kind, score, err)
		}
	}
}
