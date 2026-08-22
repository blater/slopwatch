package metrics

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestInterfaceRoleClassificationIsConservativeAndDeterministic(t *testing.T) {
	primitive := &facts.TypeShape{Kind: "primitive", Complexity: 1}
	tests := []struct {
		name       string
		operations []*facts.PublicOperation
		types      []*facts.Type
		want       string
	}{
		{name: "protocol", operations: []*facts.PublicOperation{{Results: []*facts.TypeShape{primitive}}}, types: []*facts.Type{{Name: "Service", Kind: "interface"}}, want: roleProtocol},
		{name: "command", operations: []*facts.PublicOperation{{}}, want: roleCommand},
		{name: "query", operations: []*facts.PublicOperation{{Results: []*facts.TypeShape{primitive}}}, want: roleQuery},
		{name: "data", types: []*facts.Type{{Name: "State", Fields: []facts.Field{{Name: "Value", Public: true}}}}, want: roleData},
		{name: "unknown", types: []*facts.Type{{Name: "Marker"}}, want: roleUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			role, confidence, basis := interfaceRole(test.operations, test.types)
			if role != test.want || (role != roleUnknown && confidence <= 0) || basis == "" {
				t.Fatalf("role = %q, confidence = %v, basis = %q; want %q", role, confidence, basis, test.want)
			}
		})
	}
}

func TestRoleShapeReferenceIsConfidenceAndSizeAware(t *testing.T) {
	primitive := &facts.TypeShape{Kind: "primitive", Complexity: 1}
	value, attributes := moduleShallownessWithEvidence(
		[]*facts.PublicOperation{{Results: []*facts.TypeShape{primitive}}},
		[]*facts.Type{{Name: "Service", Kind: "interface"}},
		nil,
	)
	if value != 18 || attributes["depth_reference"] != 0.86 || attributes["reference_basis"] != "role-shape-v2" {
		t.Fatalf("role metadata changed baseline: value=%d attributes=%#v", value, attributes)
	}
}

func TestRoleShapeReferenceKeepsWeakEvidenceNearNeutral(t *testing.T) {
	reference, basis := depthReferenceForRole(roleGeneral, 0.6, 1, 0)
	if reference != 1 || basis != referencePolicy {
		t.Fatalf("weak general reference = %v, %q", reference, basis)
	}
	protocol, _ := depthReferenceForRole(roleProtocol, 1, 4, 0)
	command, _ := depthReferenceForRole(roleCommand, 0.9, 4, 0)
	if protocol >= 0.73 || command <= 1.1 {
		t.Fatalf("role priors not applied: protocol=%v command=%v", protocol, command)
	}
}
