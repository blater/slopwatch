package goadapter

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slopslap.dev/structural/internal/facts"
	"sort"
)

type depthNamespace struct {
	sources       []source
	boundary      facts.BoundaryAssessment
	flow          facts.FlowArtifact
	concepts      map[string]bool
	functions     map[string]depthFunction
	routes        []depthRouteCandidate
	fileGaps      map[string][]string
	routeConcepts map[string]map[string]bool
}

type depthFunction struct {
	item   source
	decl   *ast.FuncDecl
	object *types.Func
}

type depthRouteCandidate struct {
	route  facts.Route
	item   source
	decl   *ast.FuncDecl
	object *types.Func
}

func collectDepth(sources []source, failures []facts.FileFailure, fset *token.FileSet) *facts.DepthFacts {
	failedDirectories := map[string]bool{}
	for _, failure := range failures {
		failedDirectories[filepath.Dir(failure.Path)] = true
	}
	groups := map[string]*depthNamespace{}
	for _, item := range sources {
		id := packageID(item)
		group := groups[id]
		if group == nil {
			identity := facts.BoundaryIdentity{Artifact: id, Audience: "external", View: "namespace", Symbol: id}
			group = &depthNamespace{boundary: facts.BoundaryAssessment{Identity: identity, State: facts.KnowledgeMeasured, Knowledge: map[string]facts.Knowledge{}}, flow: facts.FlowArtifact{Artifact: id, Language: "go"}, concepts: map[string]bool{}, functions: map[string]depthFunction{}, fileGaps: map[string][]string{}, routeConcepts: map[string]map[string]bool{}}
			for _, dimension := range []string{"inventory", "burden", "behavior", "alias_effects"} {
				group.boundary.Knowledge[dimension] = facts.Knowledge{State: facts.KnowledgeMeasured, Essential: true}
			}
			groups[id] = group
		}
		group.boundary.Files = append(group.boundary.Files, item.rel)
		group.sources = append(group.sources, item)
		if failedDirectories[filepath.Dir(item.rel)] {
			depthSurfaceGap(group, "incomplete_package_inventory")
		}
		if !item.typesAvailable {
			depthSurfaceGapAt(group, item.rel, "unavailable_go_types")
			continue
		}
		for _, decl := range item.file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok {
				markDepthDeclarations(group, item.rel, decl)
				continue
			}
			object, _ := item.typeInfo.Defs[function.Name].(*types.Func)
			functionID := depthFunctionID(item, object, id, function.Name.Name)
			group.functions[functionID] = depthFunction{item: item, decl: function, object: object}
			group.flow.Functions = append(group.flow.Functions, lowerDepthFunction(item, function, functionID))
			if function.Name.IsExported() && !supportingGoGetter(item, function, object) {
				collectDepthRoute(group, item, function, functionID, fset)
				if function.Recv != nil {
					depthSurfaceGapAt(group, item.rel, "unsupported_type_boundary")
				}
			}
		}
	}
	output := &facts.DepthFacts{}
	keys := make([]string, 0, len(groups))
	for id := range groups {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		group := groups[id]
		normalizeDepthRoutes(group)
		applyGoPassiveEnum(group)
		applyGoPassiveCarrier(group)
		group.boundary.Burden.O = int64(len(group.boundary.RouteFamilies))
		for concept := range group.concepts {
			group.boundary.Concepts = append(group.boundary.Concepts, facts.Concept{ID: concept, Kind: concept})
		}
		sort.Slice(group.boundary.Concepts, func(i, j int) bool { return group.boundary.Concepts[i].ID < group.boundary.Concepts[j].ID })
		group.boundary.Burden.T = int64(len(group.concepts))
		if len(group.boundary.RouteFamilies) == 0 {
			depthSurfaceGap(group, "unresolved_or_absent_public_surface")
		}
		output.Boundaries = append(output.Boundaries, splitDepthBoundaries(group)...)
		output.Flows = append(output.Flows, group.flow)
	}
	return output
}
func collectDepthRoute(group *depthNamespace, item source, decl *ast.FuncDecl, id string, fset *token.FileSet) {
	object, _ := item.typeInfo.Defs[decl.Name].(*types.Func)
	if object == nil {
		depthSurfaceGapAt(group, item.rel, "unresolved_export")
		return
	}
	signature := object.Type().(*types.Signature)
	route := facts.Route{ID: id, Family: id, Signature: goDepthSignature(signature), TargetFunctionID: id, Boundary: group.boundary.Identity}
	if signature.Variadic() || signature.TypeParams().Len() != 0 {
		depthSurfaceGapAt(group, item.rel, "unsupported_signature_shape")
	}
	for index := 0; index < signature.Params().Len(); index++ {
		_, kind, _ := depthType(signature.Params().At(index).Type())
		concept := depthConcept(kind)
		if concept == "unknown" {
			depthSurfaceGapAt(group, item.rel, "unsupported_surface_type")
		}
		group.concepts[concept] = true
	}
	for index := 0; index < signature.Results().Len(); index++ {
		if index == signature.Results().Len()-1 && depthErrorType(signature.Results().At(index).Type()) {
			group.concepts["error"] = true
			continue
		}
		_, kind, _ := depthType(signature.Results().At(index).Type())
		concept := depthConcept(kind)
		if concept == "unknown" {
			depthSurfaceGapAt(group, item.rel, "unsupported_surface_type")
		}
		group.concepts[concept] = true
	}
	group.routes = append(group.routes, depthRouteCandidate{route: route, item: item, decl: decl, object: object})
	concepts := group.routeConcepts[id]
	if concepts == nil {
		concepts = map[string]bool{}
		group.routeConcepts[id] = concepts
	}
	for index := 0; index < signature.Params().Len(); index++ {
		_, kind, _ := depthType(signature.Params().At(index).Type())
		concepts[depthConcept(kind)] = true
	}
	for index := 0; index < signature.Results().Len(); index++ {
		if index == signature.Results().Len()-1 && depthErrorType(signature.Results().At(index).Type()) {
			concepts["error"] = true
			continue
		}
		_, kind, _ := depthType(signature.Results().At(index).Type())
		concepts[depthConcept(kind)] = true
	}
	group.boundary.SourceLocations = append(group.boundary.SourceLocations, location(&analysisContext{fset: fset, current: item}, decl))
}

func depthSurfaceGap(group *depthNamespace, reason string) {
	group.boundary.State = facts.KnowledgePartial
	group.boundary.Knowledge["inventory"] = facts.Knowledge{State: facts.KnowledgePartial, Reason: reason, Essential: true}
	group.boundary.Reasons = append(group.boundary.Reasons, facts.Reason{Code: reason, Dimension: "inventory", Message: reason})
}

func depthSurfaceGapAt(group *depthNamespace, path, reason string) {
	if group.fileGaps[path] == nil {
		group.fileGaps[path] = []string{}
	}
	for _, existing := range group.fileGaps[path] {
		if existing == reason {
			return
		}
	}
	group.fileGaps[path] = append(group.fileGaps[path], reason)
}
func markDepthDeclarations(group *depthNamespace, path string, decl ast.Decl) {
	declaration, ok := decl.(*ast.GenDecl)
	if !ok || declaration.Tok == token.IMPORT {
		return
	}
	for _, spec := range declaration.Specs {
		switch item := spec.(type) {
		case *ast.TypeSpec:
			if item.Name.IsExported() {
				depthSurfaceGapAt(group, path, "unsupported_type_boundary")
			}
		case *ast.ValueSpec:
			for _, name := range item.Names {
				if name.IsExported() {
					depthSurfaceGapAt(group, path, "unsupported_exported_value")
				}
			}
		}
	}
}
