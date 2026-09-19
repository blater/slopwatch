package goadapter

import (
	"go/ast"
	"go/types"
	"sort"

	"slopslap.dev/structural/internal/facts"
)

type depthFileProjectionIndex struct {
	fileReceivers  map[string]map[string]bool
	fileRoutes     map[string][]string
	receiverRoutes map[string][]string
	routeOrder     map[string]int
	routeFamily    map[string]int
	familyRoutes   map[int][]facts.Route
	slotOrder      map[string]int
	locations      map[string][]facts.Location
}

// splitDepthBoundaries projects one package flow inventory into source-owned
// boundary views. The flow stays package-wide so helpers can be resolved once;
// only routes, slots, concepts, and source evidence are selected per file.
func splitDepthBoundaries(group *depthNamespace) []facts.BoundaryAssessment {
	index := indexDepthProjection(group)
	output := make([]facts.BoundaryAssessment, 0, len(group.sources))
	for _, item := range group.sources {
		selected := selectedDepthRoutes(index, item.rel)
		boundary := projectDepthBoundary(group, index, item.rel, selected)
		output = append(output, boundary)
	}
	return output
}

func indexDepthProjection(group *depthNamespace) depthFileProjectionIndex {
	index := depthFileProjectionIndex{
		fileReceivers: map[string]map[string]bool{}, fileRoutes: map[string][]string{},
		receiverRoutes: map[string][]string{}, routeOrder: map[string]int{}, routeFamily: map[string]int{},
		familyRoutes: map[int][]facts.Route{}, slotOrder: map[string]int{}, locations: map[string][]facts.Location{},
	}
	for _, item := range group.sources {
		for _, declaration := range item.file.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok || method.Recv == nil || item.typeInfo == nil {
				continue
			}
			object, _ := item.typeInfo.Defs[method.Name].(*types.Func)
			if object == nil {
				continue
			}
			signature, _ := object.Type().(*types.Signature)
			if signature == nil || signature.Recv() == nil {
				continue
			}
			name := depthReceiverName(signature.Recv().Type())
			if index.fileReceivers[item.rel] == nil {
				index.fileReceivers[item.rel] = map[string]bool{}
			}
			index.fileReceivers[item.rel][name] = true
		}
	}
	for routeIndex, candidate := range group.routes {
		id := candidate.route.ID
		index.routeOrder[id] = routeIndex
		if candidate.decl.Recv == nil {
			index.fileRoutes[candidate.item.rel] = append(index.fileRoutes[candidate.item.rel], id)
			continue
		}
		signature, _ := candidate.object.Type().(*types.Signature)
		if signature != nil && signature.Recv() != nil {
			name := depthReceiverName(signature.Recv().Type())
			index.receiverRoutes[name] = append(index.receiverRoutes[name], id)
		}
	}
	for familyIndex, family := range group.boundary.RouteFamilies {
		for _, route := range family.Routes {
			index.routeFamily[route.ID] = familyIndex
			index.familyRoutes[familyIndex] = append(index.familyRoutes[familyIndex], route)
		}
	}
	for slotIndex, slot := range group.boundary.Slots {
		index.slotOrder[slot.ID] = slotIndex
	}
	for _, location := range group.boundary.SourceLocations {
		index.locations[location.Path] = append(index.locations[location.Path], location)
	}
	return index
}

func selectedDepthRoutes(index depthFileProjectionIndex, path string) map[string]bool {
	selected := map[string]bool{}
	for _, id := range index.fileRoutes[path] {
		selected[id] = true
	}
	for receiver := range index.fileReceivers[path] {
		for _, id := range index.receiverRoutes[receiver] {
			selected[id] = true
		}
	}
	return selected
}

func projectDepthBoundary(group *depthNamespace, index depthFileProjectionIndex, path string,
	selected map[string]bool) facts.BoundaryAssessment {
	identity := facts.BoundaryIdentity{Artifact: group.flow.Artifact, Audience: "external", View: "file", Symbol: path}
	boundary := group.boundary
	boundary.Identity = identity
	boundary.Files = []string{path}
	boundary.State = facts.KnowledgeMeasured
	boundary.Knowledge = measuredDepthKnowledge()
	boundary.Reasons = nil
	boundary.Obligations = nil
	boundary.FamilyAlternatives = nil
	boundary.FamilyAlternativeIDs = nil
	boundary.SelectedRoutes = nil
	boundary.Dependencies = nil
	boundary.RouteFamilies = projectDepthFamilies(group.boundary.RouteFamilies, index, selected, identity)
	boundary.Slots = projectDepthSlots(group.boundary.Slots, index, boundary.RouteFamilies)
	boundary.Concepts = projectDepthConcepts(group, selected)
	boundary.Burden.O = int64(len(boundary.RouteFamilies))
	boundary.Burden.T = int64(len(boundary.Concepts))
	boundary.SourceLocations = append([]facts.Location(nil), index.locations[path]...)
	boundary.Evidence = nil
	if len(selected) > 0 || len(group.sources) == 1 {
		boundary.Evidence = append(boundary.Evidence, group.boundary.Evidence...)
	}
	for _, reason := range group.boundary.Reasons {
		if reason.Code == "incomplete_package_inventory" {
			addProjectedDepthReason(&boundary, reason.Code)
		}
	}
	for id := range selected {
		if function, ok := group.functions[id]; ok && function.decl.Recv != nil {
			addProjectedDepthReason(&boundary, "unsupported_type_boundary")
			break
		}
	}
	for _, reason := range group.fileGaps[path] {
		addProjectedDepthReason(&boundary, reason)
	}
	if len(boundary.RouteFamilies) == 0 && len(boundary.Evidence) == 0 {
		addProjectedDepthReason(&boundary, "unresolved_or_absent_public_surface")
	}
	return boundary
}

func projectDepthFamilies(families []facts.RouteFamily, index depthFileProjectionIndex,
	selected map[string]bool, identity facts.BoundaryIdentity) []facts.RouteFamily {
	selectedByFamily := map[int][]facts.Route{}
	ids := make([]string, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return index.routeOrder[ids[left]] < index.routeOrder[ids[right]] })
	for _, id := range ids {
		familyIndex, ok := index.routeFamily[id]
		if !ok {
			continue
		}
		for _, route := range index.familyRoutes[familyIndex] {
			if route.ID != id {
				continue
			}
			route.Boundary = identity
			selectedByFamily[familyIndex] = append(selectedByFamily[familyIndex], route)
		}
	}
	output := make([]facts.RouteFamily, 0, len(selectedByFamily))
	for familyIndex, family := range families {
		if routes := selectedByFamily[familyIndex]; len(routes) > 0 {
			output = append(output, facts.RouteFamily{ID: family.ID, Routes: routes})
		}
	}
	return output
}

func projectDepthSlots(slots []facts.Slot, index depthFileProjectionIndex,
	families []facts.RouteFamily) []facts.Slot {
	used := map[int]bool{}
	for _, family := range families {
		for _, route := range family.Routes {
			for _, slot := range append(append([]string{}, route.RequiredSlots...), route.ExposedSlots...) {
				if slotIndex, ok := index.slotOrder[slot]; ok {
					used[slotIndex] = true
				}
			}
		}
	}
	indices := make([]int, 0, len(used))
	for index := range used {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	output := make([]facts.Slot, 0, len(indices))
	for _, slotIndex := range indices {
		if slotIndex >= 0 && slotIndex < len(slots) {
			output = append(output, slots[slotIndex])
		}
	}
	return output
}

func projectDepthConcepts(group *depthNamespace, selected map[string]bool) []facts.Concept {
	concepts := map[string]bool{}
	for id := range selected {
		for concept := range group.routeConcepts[id] {
			concepts[concept] = true
		}
	}
	output := make([]facts.Concept, 0, len(concepts))
	for concept := range concepts {
		output = append(output, facts.Concept{ID: concept, Kind: concept})
	}
	sort.Slice(output, func(left, right int) bool { return output[left].ID < output[right].ID })
	return output
}

func measuredDepthKnowledge() map[string]facts.Knowledge {
	output := map[string]facts.Knowledge{}
	for _, dimension := range []string{"inventory", "burden", "behavior", "alias_effects"} {
		output[dimension] = facts.Knowledge{State: facts.KnowledgeMeasured, Essential: true}
	}
	return output
}

func addProjectedDepthReason(boundary *facts.BoundaryAssessment, reason string) {
	boundary.State = facts.KnowledgePartial
	boundary.Knowledge["inventory"] = facts.Knowledge{State: facts.KnowledgePartial, Reason: reason, Essential: true}
	boundary.Reasons = append(boundary.Reasons, facts.Reason{Code: reason, Dimension: "inventory", Message: reason})
}
