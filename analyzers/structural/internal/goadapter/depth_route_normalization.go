package goadapter

import (
	"fmt"
	"go/types"
	"slopslap.dev/structural/internal/facts"
	"strconv"
	"strings"
)

// normalizeDepthRoutes groups public forwarding routes by the local service
// they actually call. The route keeps its own function as TargetFunctionID so
// guards and error adaptation remain independent behavior evidence, while its
// family and caller slots use the resolved service identity.
func normalizeDepthRoutes(group *depthNamespace) {
	if len(group.routes) == 0 {
		return
	}
	families := map[string][]facts.Route{}
	familyOrder := make([]string, 0)
	slotSeen := map[string]bool{}
	public := make([]facts.Route, 0, len(group.routes))
	for _, candidate := range group.routes {
		serviceID := resolveDepthService(group, candidate.route.ID, map[string]bool{})
		if serviceID == "" {
			serviceID = candidate.route.ID
		}
		route, formalPaths := normalizeDepthRoute(group, candidate, serviceID)
		route.Family = serviceID
		if _, exists := families[serviceID]; !exists {
			familyOrder = append(familyOrder, serviceID)
		}
		families[serviceID] = append(families[serviceID], route)
		public = append(public, route)
		for _, slot := range route.ExposedSlots {
			if slotSeen[slot] {
				continue
			}
			slotSeen[slot] = true
			concept := depthSlotConcept(group, slot)
			group.boundary.Slots = append(group.boundary.Slots, facts.Slot{ID: slot, Concept: concept, Required: containsString(route.RequiredSlots, slot)})
		}
		rewriteDepthFormalPaths(group, candidate.route.ID, formalPaths)
	}
	group.flow.PublicRoutes = public
	group.boundary.RouteFamilies = make([]facts.RouteFamily, 0, len(familyOrder))
	for _, id := range familyOrder {
		group.boundary.RouteFamilies = append(group.boundary.RouteFamilies, facts.RouteFamily{ID: id, Routes: families[id]})
	}
}

func depthSlotConcept(group *depthNamespace, slot string) string {
	index := strings.LastIndex(slot, "/arg")
	if index < 0 {
		return "unknown"
	}
	target := group.functions[slot[:index]]
	if target.object == nil {
		return "unknown"
	}
	arg, _ := strconv.Atoi(slot[index+4:])
	parameters := target.object.Type().(*types.Signature).Params()
	if arg < 0 || arg >= parameters.Len() {
		return "unknown"
	}
	_, kind, _ := depthType(parameters.At(arg).Type())
	return depthConcept(kind)
}

func normalizeDepthRoute(group *depthNamespace, candidate depthRouteCandidate, serviceID string) (facts.Route, map[int]string) {
	route := candidate.route
	route.RequiredSlots = nil
	route.ExposedSlots = nil
	formalPaths := map[int]string{}
	service := group.functions[serviceID]
	if service.object == nil {
		service = group.functions[candidate.route.ID]
	}
	if service.object == nil {
		return route, formalPaths
	}
	signature := service.object.Type().(*types.Signature)
	used := map[int]bool{}
	bindingSlots := map[string]string{}
	call := selectedDepthServiceCall(group, candidate.item, candidate.decl, candidate.object)
	if candidate.route.ID == serviceID || call == nil {
		for index := 0; index < candidate.object.Type().(*types.Signature).Params().Len(); index++ {
			path := fmt.Sprintf("%s/arg%d", serviceID, index)
			route.ExposedSlots = append(route.ExposedSlots, path)
			route.RequiredSlots = append(route.RequiredSlots, path)
			formalPaths[index] = path
			used[index] = true
		}
		return route, formalPaths
	}
	for index, argument := range call.Args {
		if index >= signature.Params().Len() {
			break
		}
		dependencies := depthFormalDependencies(candidate.item, argument, candidate.object)
		if len(dependencies) == 0 {
			continue
		}
		path := fmt.Sprintf("%s/arg%d", serviceID, index)
		route.ExposedSlots = append(route.ExposedSlots, path)
		bindingKey := strings.Join(intStrings(dependencies), ",")
		if prior := bindingSlots[bindingKey]; prior != "" {
			path = prior
		} else {
			bindingSlots[bindingKey] = path
			route.RequiredSlots = append(route.RequiredSlots, path)
		}
		for _, formal := range dependencies {
			formalPaths[formal] = path
			used[formal] = true
		}
	}
	// Parameters which do not bind to a service slot remain route-specific
	// caller choices (for example a wrapper-only selector or cleanup token).
	wrapperSignature := candidate.object.Type().(*types.Signature)
	for index := 0; index < wrapperSignature.Params().Len(); index++ {
		if used[index] {
			continue
		}
		path := fmt.Sprintf("%s/arg%d", candidate.route.ID, index)
		route.ExposedSlots = append(route.ExposedSlots, path)
		route.RequiredSlots = append(route.RequiredSlots, path)
		formalPaths[index] = path
	}
	return route, formalPaths
}

func intStrings(values []int) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strconv.Itoa(value)
	}
	return result
}

func resolveDepthService(group *depthNamespace, id string, seen map[string]bool) string {
	if seen[id] {
		return id
	}
	seen[id] = true
	function, ok := group.functions[id]
	if !ok || function.object == nil {
		return id
	}
	call := selectedDepthServiceCall(group, function.item, function.decl, function.object)
	if call == nil {
		return id
	}
	target := depthLocalFunctionID(group, function, depthCallObject(function.item, call))
	if target == "" || target == id {
		return id
	}
	// Do not chase helper chains here. A second hop needs composed bindings;
	// keeping the immediate target is the conservative choice until that
	// composition is available.
	return target
}
