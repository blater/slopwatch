package metrics

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slopslap.dev/structural/internal/facts"
	"sort"
	"strings"
)

type inventoryRoute struct {
	ID, Family, Signature                                                         string
	RequiredSlots, ExposedSlots, RequiredPolicies, LifecycleRelations, Sequencing []string
}

type boundaryInventory struct {
	Identity      facts.BoundaryIdentity
	Applicability string
	Concepts      []facts.Concept
	Slots         []facts.Slot
	Routes        []inventoryRoute
}

func inventoryFingerprint(boundary facts.BoundaryAssessment) string {
	if boundary.State == facts.KnowledgeUnavailable {
		return ""
	}
	if inventory, ok := boundary.Knowledge["inventory"]; !ok || inventory.State != facts.KnowledgeMeasured {
		return ""
	}
	inventory := boundaryInventory{
		Identity: boundary.Identity, Applicability: inventoryApplicability(boundary.State),
		Concepts: canonicalConcepts(boundary.Concepts), Slots: canonicalSlots(boundary.Slots),
	}
	routes, ok := canonicalRoutes(boundary.RouteFamilies)
	if !ok {
		return ""
	}
	inventory.Routes = routes
	payload, err := json.Marshal(inventory)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest)
}

func inventoryApplicability(state facts.KnowledgeState) string {
	if state == facts.KnowledgeNotApplicable {
		return "not_applicable"
	}
	return "assessable"
}

func canonicalConcepts(values []facts.Concept) []facts.Concept {
	result := append([]facts.Concept(nil), values...)
	for index := range result {
		result[index].Children = uniqueStrings(result[index].Children)
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.Join([]string{result[left].ID, result[left].Kind, strings.Join(result[left].Children, "\x00")}, "\x00") < strings.Join([]string{result[right].ID, result[right].Kind, strings.Join(result[right].Children, "\x00")}, "\x00")
	})
	return result
}

func canonicalSlots(values []facts.Slot) []facts.Slot {
	result := append([]facts.Slot(nil), values...)
	sort.Slice(result, func(left, right int) bool {
		return strings.Join([]string{result[left].ID, result[left].Concept, fmt.Sprint(result[left].Required), fmt.Sprint(result[left].Policy), result[left].Path}, "\x00") < strings.Join([]string{result[right].ID, result[right].Concept, fmt.Sprint(result[right].Required), fmt.Sprint(result[right].Policy), result[right].Path}, "\x00")
	})
	return result
}

func canonicalRoutes(families []facts.RouteFamily) ([]inventoryRoute, bool) {
	result := make([]inventoryRoute, 0)
	for _, family := range families {
		for _, item := range family.Routes {
			if item.Signature == "" {
				return nil, false
			}
			familyID := item.Family
			if familyID == "" {
				familyID = family.ID
			}
			result = append(result, inventoryRoute{item.ID, familyID, item.Signature, uniqueStrings(item.RequiredSlots), uniqueStrings(item.ExposedSlots), uniqueStrings(item.RequiredPolicies), uniqueStrings(item.LifecycleRelations), uniqueStrings(item.Sequencing)})
		}
	}
	sort.Slice(result, func(left, right int) bool { return routeKey(result[left]) < routeKey(result[right]) })
	return result, true
}

func routeKey(route inventoryRoute) string {
	return strings.Join([]string{route.ID, route.Family, route.Signature, strings.Join(route.RequiredSlots, "\x00"), strings.Join(route.ExposedSlots, "\x00"), strings.Join(route.RequiredPolicies, "\x00"), strings.Join(route.LifecycleRelations, "\x00"), strings.Join(route.Sequencing, "\x00")}, "\x00")
}
