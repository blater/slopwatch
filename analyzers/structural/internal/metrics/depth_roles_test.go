package metrics

import (
	"slopslap.dev/structural/internal/facts"
	"testing"
)

func TestSupportingContractApplicability(t *testing.T) {
	use := facts.ContractUse{Member: "Provider.allocate", Contract: "Allocator", ContractMember: "Allocator.allocate", Consumer: "Arena.new", Slot: "allocator", Use: "Arena.reserve"}
	cases := []struct {
		name       string
		change     func(*facts.BoundaryAssessment)
		applicable bool
	}{
		{"internal injection", func(*facts.BoundaryAssessment) {}, false},
		{"public factory exposure", func(b *facts.BoundaryAssessment) { b.SupportingContract.ExternalRoutes = []string{"Factory.make"} }, true},
		{"incomplete exposure", func(b *facts.BoundaryAssessment) { b.SupportingContract.Exposure = facts.KnowledgePartial }, true},
		{"unrelated member", func(b *facts.BoundaryAssessment) {
			b.SupportingContract.Members = append(b.SupportingContract.Members, "Provider.extra")
		}, true},
		{"no production use", func(b *facts.BoundaryAssessment) { b.SupportingContract.Bindings = nil }, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			b := facts.BoundaryAssessment{Identity: facts.BoundaryIdentity{Artifact: "p", Audience: "external", View: "type", Symbol: "Provider"}, State: facts.KnowledgePartial,
				SupportingContract: &facts.SupportingContract{Inventory: facts.KnowledgeMeasured, Exposure: facts.KnowledgeMeasured, Members: []string{use.Member}, Bindings: []facts.ContractUse{use}}}
			test.change(&b)
			score, err := ScoreBoundary(b)
			if err != nil {
				t.Fatal(err)
			}
			if score.Shallow != nil {
				t.Fatalf("invented numeric discount: %+v", score)
			}
			if !test.applicable {
				if score.State != facts.KnowledgeNotApplicable || len(score.Evidence) != 1 || len(score.Dependencies) != 5 {
					t.Fatalf("missing applicability/evidence: %+v", score)
				}
			} else if score.State == facts.KnowledgeNotApplicable {
				t.Fatalf("blanket role exemption: %+v", score)
			}
		})
	}
}

func TestPartialRoleRetainsExposureDependencies(t *testing.T) {
	boundary := facts.BoundaryAssessment{Identity: facts.BoundaryIdentity{Artifact: "p", Audience: "external", View: "type", Symbol: "Provider"}, State: facts.KnowledgePartial, Dependencies: []string{"Factory.make"}}
	scores := MeasureDepth(&facts.Program{Depth: &facts.DepthFacts{Boundaries: []facts.BoundaryAssessment{boundary}, Flows: []facts.FlowArtifact{{Artifact: "p", Language: "java"}}}})
	if len(scores) != 1 || len(scores[0].Dependencies) != 1 || scores[0].Dependencies[0] != "Factory.make" {
		t.Fatalf("lost applicability dependency: %+v", scores)
	}
}
