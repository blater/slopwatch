package scoring

import "testing"

func TestCatalogPreservesDashboardOrderDefaultsAndIsolation(t *testing.T) {
	components := Components()
	if len(components) != 15 {
		t.Fatalf("component count = %d, want 15", len(components))
	}
	if components[0].ID != "cognitive_complexity" || components[0].DefaultWeight != 10 || !components[0].DefaultOn {
		t.Fatalf("first component = %#v", components[0])
	}
	if components[4].ID != "deeply_nested_if" || components[4].DefaultOn {
		t.Fatalf("nesting component = %#v", components[4])
	}
	if components[8].Axis != "typescript_type_safety" || components[8].DefaultOn {
		t.Fatalf("type-safety component = %#v", components[8])
	}

	components[0].DefaultWeight = 99
	if component, _ := ComponentByID("cognitive_complexity"); component.DefaultWeight != 10 {
		t.Fatalf("caller mutated catalog: %#v", component)
	}

	weights := DefaultWeights()
	enabled := DefaultEnabled()
	weights["cognitive_complexity"] = 99
	enabled["cognitive_complexity"] = false
	if DefaultWeights()["cognitive_complexity"] != 10 || !DefaultEnabled()["cognitive_complexity"] {
		t.Fatal("default maps alias package state")
	}
}

func TestUnknownComponentsRetainHistoricalFallbacks(t *testing.T) {
	policy := NewPolicy(nil, nil)
	if DefaultWeight("future_component") != 1 {
		t.Fatalf("unknown default weight = %v, want 1", DefaultWeight("future_component"))
	}
	if ComponentAxis("future_component") != "unknown" {
		t.Fatalf("unknown axis = %q, want unknown", ComponentAxis("future_component"))
	}
	if policy.Enabled("future_component") || policy.WeightFactor("future_component") != 0 {
		t.Fatal("unknown component must remain disabled")
	}
}
