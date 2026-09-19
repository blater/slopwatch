package metrics

import (
	"fmt"
	"slopslap.dev/structural/internal/facts"
)

func validateRouteFamilies(families []facts.RouteFamily) error {
	seen := map[string]bool{}
	for _, family := range families {
		if family.ID == "" {
			return fmt.Errorf("route family has empty ID")
		}
		if seen[family.ID] {
			return fmt.Errorf("duplicate route family ID %s", family.ID)
		}
		seen[family.ID] = true
		if len(family.Routes) == 0 {
			return fmt.Errorf("route family %s has no routes", family.ID)
		}
		if err := validateRoutes(family); err != nil {
			return err
		}
	}
	return nil
}

func validateRoutes(family facts.RouteFamily) error {
	seen := map[string]bool{}
	for _, route := range family.Routes {
		if route.ID == "" {
			return fmt.Errorf("route in family %s has empty ID", family.ID)
		}
		if seen[route.ID] {
			return fmt.Errorf("duplicate route ID %s", route.ID)
		}
		seen[route.ID] = true
		if hasEmpty(route.ExposedSlots) || hasEmpty(route.RequiredSlots) || hasEmpty(route.RequiredPolicies) || hasEmpty(route.LifecycleRelations) {
			return fmt.Errorf("route %s has empty slot or relation", route.ID)
		}
		if route.Family != "" && route.Family != family.ID {
			return fmt.Errorf("route %s belongs to family %s, want %s", route.ID, route.Family, family.ID)
		}
		exposed := make(map[string]bool, len(route.ExposedSlots))
		for _, slot := range route.ExposedSlots {
			exposed[slot] = true
		}
		required := make(map[string]bool, len(route.RequiredSlots))
		for _, slot := range route.RequiredSlots {
			required[slot] = true
			if !exposed[slot] {
				return fmt.Errorf("route %s requires unexposed slot %s", route.ID, slot)
			}
		}
		for _, policy := range route.RequiredPolicies {
			if !required[policy] {
				return fmt.Errorf("route %s policy %s is not a required slot", route.ID, policy)
			}
		}
	}
	return nil
}

func obligationIndex(items []facts.Obligation) (map[string]facts.Obligation, error) {
	result := make(map[string]facts.Obligation, len(items))
	for _, item := range items {
		if item.ID == "" {
			return nil, fmt.Errorf("obligation has empty ID")
		}
		if _, exists := result[item.ID]; exists {
			return nil, fmt.Errorf("duplicate obligation ID %s", item.ID)
		}
		switch item.Category {
		case facts.ObligationValidation, facts.ObligationResource, facts.ObligationState, facts.ObligationCoordination, facts.ObligationTransform, facts.ObligationOutcome:
		default:
			return nil, fmt.Errorf("unknown obligation category %q", item.Category)
		}
		result[item.ID] = item
	}
	return result, nil
}
