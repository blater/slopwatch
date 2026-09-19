package depth

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestPassiveCreationApplicabilityRequiresOnlyCanonicalCreationRoutes(t *testing.T) {
	tests := []struct {
		name string
		edit func(*facts.BoundaryAssessment)
		want facts.KnowledgeState
	}{
		{"plain creation", func(*facts.BoundaryAssessment) {}, facts.KnowledgeNotApplicable},
		{"constructor fact keeps route identity", func(boundary *facts.BoundaryAssessment) {
			boundary.Creation.ID = "p.Type#<init>()"
			boundary.Creation.Route = "p.Type#<init>()"
		}, facts.KnowledgeNotApplicable},
		{"immutable getter carrier", func(boundary *facts.BoundaryAssessment) {
			boundary.Creation.PassiveAccessors = []string{"p.Type#value()"}
			boundary.RouteFamilies = append(boundary.RouteFamilies, facts.RouteFamily{ID: "value", Routes: []facts.Route{{ID: "p.Type#value()"}}})
		}, facts.KnowledgeNotApplicable},
		{"extra behavior", func(boundary *facts.BoundaryAssessment) {
			boundary.Creation.PassiveAccessors = []string{"p.Type#value()"}
			boundary.RouteFamilies = append(boundary.RouteFamilies,
				facts.RouteFamily{ID: "value", Routes: []facts.Route{{ID: "p.Type#value()"}}},
				facts.RouteFamily{ID: "service", Routes: []facts.Route{{ID: "p.Type#service()"}}})
		}, facts.KnowledgeMeasured},
		{"unknown accessor", func(boundary *facts.BoundaryAssessment) {
			boundary.Creation.PassiveAccessors = []string{"p.Type#missing()"}
		}, facts.KnowledgeMeasured},
		{"missing accessor proof", func(boundary *facts.BoundaryAssessment) {
			boundary.RouteFamilies = append(boundary.RouteFamilies, facts.RouteFamily{ID: "value", Routes: []facts.Route{{ID: "p.Type#value()"}}})
		}, facts.KnowledgeMeasured},
		{"missing constructor proof", func(boundary *facts.BoundaryAssessment) {
			boundary.Creation.PassiveAccessors = []string{"p.Type#value()"}
			boundary.RouteFamilies = []facts.RouteFamily{{ID: "value", Routes: []facts.Route{{ID: "p.Type#value()"}}}}
		}, facts.KnowledgeMeasured},
		{"duplicate accessor proof", func(boundary *facts.BoundaryAssessment) {
			boundary.Creation.PassiveAccessors = []string{"p.Type#value()", "p.Type#value()"}
			boundary.RouteFamilies = append(boundary.RouteFamilies, facts.RouteFamily{ID: "value", Routes: []facts.Route{{ID: "p.Type#value()"}}})
		}, facts.KnowledgeMeasured},
		{"contradictory route family metadata", func(boundary *facts.BoundaryAssessment) {
			boundary.RouteFamilies[0].Routes[0].Family = "other"
		}, facts.KnowledgeMeasured},
		{"contradictory route boundary metadata", func(boundary *facts.BoundaryAssessment) {
			boundary.RouteFamilies[0].Routes[0].Boundary = facts.BoundaryIdentity{Symbol: "p.Other"}
		}, facts.KnowledgeMeasured},
		{"mixed service", func(boundary *facts.BoundaryAssessment) {
			boundary.RouteFamilies = append(boundary.RouteFamilies, facts.RouteFamily{ID: "service", Routes: []facts.Route{{ID: "service"}}})
		}, facts.KnowledgeMeasured},
		{"incomplete boundary", func(boundary *facts.BoundaryAssessment) {
			boundary.State = facts.KnowledgePartial
		}, facts.KnowledgePartial},
		{"known obligation", func(boundary *facts.BoundaryAssessment) {
			boundary.Obligations = []facts.Obligation{{ID: "state", Category: facts.ObligationState}}
		}, facts.KnowledgeMeasured},
		{"caller lifecycle", func(boundary *facts.BoundaryAssessment) {
			boundary.Burden.S = 1
		}, facts.KnowledgeMeasured},
		{"noncanonical family", func(boundary *facts.BoundaryAssessment) {
			boundary.Creation.CanonicalType = "p.Other"
		}, facts.KnowledgeMeasured},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			boundary := passiveCreationBoundary()
			test.edit(&boundary)
			result := ApplyBoundaryRoles(boundary)
			if result.State != test.want {
				t.Fatalf("state = %q, want %q: %#v", result.State, test.want, result)
			}
			if test.want == facts.KnowledgeNotApplicable {
				if len(result.Evidence) != 1 || result.Evidence[0].Kind != PassiveCreationRule || len(result.Evidence[0].Provenance) != 1 {
					t.Fatalf("passive creation evidence = %#v", result.Evidence)
				}
				if test.name == "immutable getter carrier" {
					provenance := result.Evidence[0].Provenance[0]
					if len(provenance.FactIDs) != 2 || provenance.FactIDs[1] != "p.Type#value()" {
						t.Fatalf("carrier provenance fact IDs = %#v", provenance.FactIDs)
					}
					details := result.Evidence[0].Details
					if details["family"] != "create:p.Type" || details["read_only_accessors"] != true ||
						len(details) != 2 || result.Evidence[0].Description != "" {
						t.Fatalf("carrier evidence details = %#v", details)
					}
				}
			}
		})
	}
}

func passiveCreationBoundary() facts.BoundaryAssessment {
	measured := facts.Knowledge{State: facts.KnowledgeMeasured, Essential: true}
	return facts.BoundaryAssessment{
		Identity:        facts.BoundaryIdentity{Artifact: "p", Audience: "external", View: "type", Symbol: "p.Type"},
		State:           facts.KnowledgeMeasured,
		Knowledge:       map[string]facts.Knowledge{"inventory": measured, "burden": measured, "behavior": measured, "alias_effects": measured},
		RouteFamilies:   []facts.RouteFamily{{ID: "create:p.Type", Routes: []facts.Route{{ID: "p.Type#<init>()"}}}},
		Creation:        &facts.CreationFacts{ID: "create:p.Type", CanonicalType: "p.Type", Family: "create:p.Type", Route: "create:p.Type", Behavior: []string{"normal"}, DataOnly: true, Accessible: true, Knowledge: facts.KnowledgeMeasured},
		SourceLocations: []facts.Location{{Path: "Type.java", Line: 1, Column: 1}},
	}
}
