package depth

import "slopslap.dev/structural/internal/facts"

func passiveCreationApplicable(boundary facts.BoundaryAssessment) bool {
	if boundary.State != facts.KnowledgeMeasured || !completeEssentialKnowledge(boundary.Knowledge) {
		return false
	}
	if len(boundary.Obligations) != 0 || len(boundary.LeakRoots) != 0 || boundary.Burden.S != 0 || boundary.Burden.L != 0 {
		return false
	}
	creation := boundary.Creation
	if creation == nil || creation.Knowledge != facts.KnowledgeMeasured || !creation.DataOnly || !creation.Accessible {
		return false
	}
	if len(creation.PossibleFailures) != 0 || len(creation.Behavior) != 1 || creation.Behavior[0] != "normal" {
		return false
	}
	if creation.ID == "" || creation.CanonicalType == "" || creation.Route == "" || creation.Family != "create:"+creation.CanonicalType {
		return false
	}
	return passiveCreationRoutes(boundary.RouteFamilies, creation.Family, creation.PassiveAccessors, boundary.Identity)
}

func passiveCreationRoutes(families []facts.RouteFamily, creationFamily string, accessors []string, identity facts.BoundaryIdentity) bool {
	if len(families) == 0 || duplicateStrings(accessors) {
		return false
	}
	accessorSet, ok := passiveAccessorSet(accessors)
	if !ok {
		return false
	}
	creationCount, creationRoutes, seenAccessors, ok := scanPassiveRouteFamilies(families, creationFamily, accessorSet, identity)
	return ok && creationCount == 1 && creationRoutes > 0 && len(seenAccessors) == len(accessorSet)
}

func passiveAccessorSet(accessors []string) (map[string]bool, bool) {
	set := make(map[string]bool, len(accessors))
	for _, accessor := range accessors {
		if accessor == "" {
			return nil, false
		}
		set[accessor] = true
	}
	return set, true
}

func scanPassiveRouteFamilies(families []facts.RouteFamily, creationFamily string, accessorSet map[string]bool, identity facts.BoundaryIdentity) (int, int, map[string]bool, bool) {
	creationCount, creationRoutes := 0, 0
	routeIDs := make(map[string]bool)
	seenAccessors := make(map[string]bool)
	for _, family := range families {
		if family.ID == "" || len(family.Routes) == 0 {
			return 0, 0, nil, false
		}
		if family.ID == creationFamily {
			creationCount++
			creationRoutes += len(family.Routes)
		}
		for _, route := range family.Routes {
			if !passiveRouteMetadata(route, family.ID, identity) || route.ID == "" || routeIDs[route.ID] {
				return 0, 0, nil, false
			}
			routeIDs[route.ID] = true
			if family.ID == creationFamily {
				continue
			}
			if !accessorSet[route.ID] || seenAccessors[route.ID] {
				return 0, 0, nil, false
			}
			seenAccessors[route.ID] = true
		}
	}
	return creationCount, creationRoutes, seenAccessors, true
}

func passiveRouteMetadata(route facts.Route, family string, identity facts.BoundaryIdentity) bool {
	if route.Family != "" && route.Family != family {
		return false
	}
	return route.Boundary == (facts.BoundaryIdentity{}) || route.Boundary == identity
}

func duplicateStrings(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}

func passiveCreationDetails(creation *facts.CreationFacts) map[string]any {
	return map[string]any{
		"family":              creation.Family,
		"read_only_accessors": len(creation.PassiveAccessors) != 0,
	}
}

func passiveCreationProvenance(boundary facts.BoundaryAssessment, creation *facts.CreationFacts) []facts.Provenance {
	if len(boundary.SourceLocations) == 0 {
		return nil
	}
	location := boundary.SourceLocations[0]
	factIDs := append([]string{creation.ID}, creation.PassiveAccessors...)
	return []facts.Provenance{{Path: location.Path, Span: location, RuleID: PassiveCreationRule, FactIDs: factIDs}}
}
