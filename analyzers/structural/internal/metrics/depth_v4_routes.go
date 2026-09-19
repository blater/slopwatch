package metrics

import (
	"fmt"
	"math"
	"slopslap.dev/structural/internal/facts"
	"sort"
)

type routeChoice struct {
	id               string
	cost, a, e, p, s int64
}

func routeBurden(boundary facts.BoundaryAssessment) (facts.Burden, []string, error) {
	if boundary.RouteFamilies == nil {
		return deriveInventoryBurden(boundary), nil, nil
	}
	result := deriveInventoryBurden(boundary)
	result.A, result.E, result.P, result.S = 0, 0, 0, 0
	families := append([]facts.RouteFamily(nil), boundary.RouteFamilies...)
	sort.Slice(families, func(i, j int) bool { return families[i].ID < families[j].ID })
	selected := make([]string, 0, len(families))
	for _, family := range families {
		choices := make([]routeChoice, 0, len(family.Routes))
		allExposed := map[string]bool{}
		for _, route := range family.Routes {
			for _, slot := range route.ExposedSlots {
				allExposed[slot] = true
			}
		}
		for _, route := range family.Routes {
			required := uniqueStrings(route.RequiredSlots)
			policies := uniqueStrings(route.RequiredPolicies)
			relations := uniqueStrings(route.LifecycleRelations)
			ownExposed := map[string]bool{}
			for _, slot := range route.ExposedSlots {
				ownExposed[slot] = true
			}
			for _, slot := range required {
				if !ownExposed[slot] {
					return result, nil, fmt.Errorf("route %s requires unexposed slot %s", route.ID, slot)
				}
			}
			for _, policy := range policies {
				if !contains(required, policy) {
					return result, nil, fmt.Errorf("route %s policy %s is not a required slot", route.ID, policy)
				}
			}
			choice := routeChoice{id: route.ID, a: int64(len(required)), e: int64(len(allExposed) - len(required)), p: int64(len(policies)), s: int64(len(relations))}
			if !safeCost(&choice.cost, 2, choice.a) || !safeAdd(&choice.cost, choice.e) || !safeMulAdd(&choice.cost, 8, choice.p) || !safeMulAdd(&choice.cost, 8, choice.s) {
				return result, nil, fmt.Errorf("route burden overflow")
			}
			choices = append(choices, choice)
		}
		sort.Slice(choices, func(i, j int) bool {
			if choices[i].cost != choices[j].cost {
				return choices[i].cost < choices[j].cost
			}
			return choices[i].id < choices[j].id
		})
		choice := choices[0]
		selected = append(selected, choice.id)
		if !safeAdd(&result.A, choice.a) || !safeAdd(&result.E, choice.e) || !safeAdd(&result.P, choice.p) || !safeAdd(&result.S, choice.s) {
			return result, nil, fmt.Errorf("route burden overflow")
		}
	}
	return result, selected, nil
}

func safeAdd(target *int64, value int64) bool {
	if value < 0 || *target > math.MaxInt64-value {
		return false
	}
	*target += value
	return true
}
func safeCost(target *int64, factor, value int64) bool {
	if value < 0 || factor < 0 || value != 0 && factor > math.MaxInt64/value {
		return false
	}
	*target = factor * value
	return true
}
func safeMulAdd(target *int64, factor, value int64) bool {
	if value < 0 || factor < 0 || value != 0 && factor > math.MaxInt64/value {
		return false
	}
	return safeAdd(target, factor*value)
}
