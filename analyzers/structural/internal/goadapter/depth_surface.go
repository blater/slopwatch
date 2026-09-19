package goadapter

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slopslap.dev/structural/internal/facts"
	"sort"
	"strconv"
	"strings"
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
			concept := "unknown"
			if index := strings.LastIndex(slot, "/arg"); index >= 0 {
				if target := group.functions[slot[:index]]; target.object != nil {
					arg, _ := strconv.Atoi(slot[index+4:])
					if arg >= 0 && arg < target.object.Type().(*types.Signature).Params().Len() {
						_, kind, _ := depthType(target.object.Type().(*types.Signature).Params().At(arg).Type())
						concept = depthConcept(kind)
					}
				}
			}
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

func selectedDepthServiceCall(group *depthNamespace, item source, decl *ast.FuncDecl, owner *types.Func) *ast.CallExpr {
	if decl == nil || decl.Body == nil || owner == nil {
		return nil
	}
	statements := decl.Body.List
	if len(statements) == 0 {
		return nil
	}
	last, ok := statements[len(statements)-1].(*ast.ReturnStmt)
	if !ok || len(last.Results) == 0 {
		return nil
	}
	var serviceCall *ast.CallExpr
	for _, result := range last.Results {
		call, direct := result.(*ast.CallExpr)
		if !direct {
			continue
		}
		if depthLocalFunctionID(group, depthFunction{item: item, object: owner}, depthCallObject(item, call)) != "" {
			if serviceCall != nil {
				return nil
			}
			serviceCall = call
		}
	}
	if serviceCall == nil {
		return nil
	}
	if !depthForwardServiceReturn(item, owner, last, serviceCall) {
		return nil
	}
	for _, statement := range statements[:len(statements)-1] {
		guard, ok := statement.(*ast.IfStmt)
		if !ok || !depthRejectionIf(item, owner, guard) {
			return nil
		}
	}
	return serviceCall
}

func depthForwardServiceReturn(item source, owner *types.Func, result *ast.ReturnStmt, serviceCall *ast.CallExpr) bool {
	if owner == nil || result == nil || serviceCall == nil {
		return false
	}
	for _, argument := range serviceCall.Args {
		if !depthForwardArgumentAllowed(item, owner, argument) {
			return false
		}
	}
	for _, expression := range result.Results {
		if expression == serviceCall {
			continue
		}
		ident, ok := expression.(*ast.Ident)
		if !ok || ident.Name != "nil" {
			return false
		}
	}
	return true
}

func depthForwardArgumentAllowed(item source, owner *types.Func, expression ast.Expr) bool {
	for {
		paren, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = paren.X
	}
	if ident, ok := expression.(*ast.Ident); ok {
		signature, _ := owner.Type().(*types.Signature)
		if signature != nil {
			for index := 0; index < signature.Params().Len(); index++ {
				if item.typeInfo.ObjectOf(ident) == signature.Params().At(index) {
					return true
				}
			}
		}
	}
	if item.typeInfo != nil {
		if value, ok := item.typeInfo.Types[expression]; ok && value.Value != nil {
			return true
		}
	}
	return false
}

func depthRejectionIf(item source, owner *types.Func, statement *ast.IfStmt) bool {
	if statement == nil || statement.Init != nil || statement.Else != nil || !depthPurePredicate(item, owner, statement.Cond) {
		return false
	}
	if statement.Body == nil || len(statement.Body.List) != 1 {
		return false
	}
	switch value := statement.Body.List[0].(type) {
	case *ast.ReturnStmt:
		return depthKnownRejectionReturn(item, owner, value)
	case *ast.ExprStmt:
		call, ok := value.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !depthForwardArgumentAllowed(item, owner, call.Args[0]) {
			return false
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return false
		}
		builtin, ok := item.typeInfo.ObjectOf(ident).(*types.Builtin)
		return ok && builtin.Name() == "panic"
	default:
		return false
	}
}

func depthKnownRejectionReturn(item source, owner *types.Func, result *ast.ReturnStmt) bool {
	if !depthRejectedReturn(item, owner, result) {
		return false
	}
	if len(result.Results) == 0 {
		return false
	}
	last := result.Results[len(result.Results)-1]
	call, ok := last.(*ast.CallExpr)
	if !ok || !depthForwardErrorCall(item, call) {
		return false
	}
	for _, expression := range result.Results[:len(result.Results)-1] {
		if !depthPurePredicate(item, owner, expression) {
			return false
		}
	}
	for _, argument := range call.Args {
		if !depthPurePredicate(item, owner, argument) {
			return false
		}
	}
	return true
}

func depthForwardErrorCall(item source, call *ast.CallExpr) bool {
	if item.depthImports == nil || call == nil {
		return false
	}
	function := depthCallObject(item, call)
	entry, ok := item.depthImports.bindings[function]
	return ok && entry.ID == "go.error"
}

func depthPurePredicate(item source, owner *types.Func, expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	switch value := expression.(type) {
	case *ast.ParenExpr:
		return depthPurePredicate(item, owner, value.X)
	case *ast.Ident:
		return depthForwardArgumentAllowed(item, owner, value)
	case *ast.BasicLit:
		return true
	case *ast.UnaryExpr:
		switch value.Op {
		case token.ADD, token.SUB, token.NOT:
			return depthPurePredicate(item, owner, value.X)
		default:
			return false
		}
	case *ast.BinaryExpr:
		switch value.Op {
		case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
			token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ,
			token.LAND, token.LOR:
			return depthPurePredicate(item, owner, value.X) && depthPurePredicate(item, owner, value.Y)
		default:
			return false
		}
	default:
		return false
	}
}

func depthRejectedReturn(item source, owner *types.Func, result *ast.ReturnStmt) bool {
	if owner == nil || result == nil {
		return false
	}
	signature, _ := owner.Type().(*types.Signature)
	if signature == nil || signature.Results().Len() == 0 || !depthErrorType(signature.Results().At(signature.Results().Len()-1).Type()) || len(result.Results) == 0 {
		return false
	}
	last := result.Results[len(result.Results)-1]
	if ident, ok := last.(*ast.Ident); ok && ident.Name == "nil" {
		return false
	}
	call, ok := last.(*ast.CallExpr)
	return ok && depthForwardErrorCall(item, call)
}

func callFunIdent(call *ast.CallExpr) *ast.Ident {
	if ident, ok := call.Fun.(*ast.Ident); ok {
		return ident
	}
	return nil
}

func depthCallObject(item source, call *ast.CallExpr) *types.Func {
	if call == nil || item.typeInfo == nil {
		return nil
	}
	if ident := callFunIdent(call); ident != nil {
		called, _ := item.typeInfo.Uses[ident].(*types.Func)
		if called != nil {
			return called
		}
		called, _ = item.typeInfo.ObjectOf(ident).(*types.Func)
		return called
	}
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		called, _ := item.typeInfo.ObjectOf(selector.Sel).(*types.Func)
		return called
	}
	return nil
}

func depthLocalFunctionID(group *depthNamespace, owner depthFunction, called *types.Func) string {
	if called == nil || owner.object == nil || called.Pkg() != owner.object.Pkg() || called.Parent() != owner.object.Pkg().Scope() {
		return ""
	}
	id := depthFunctionID(owner.item, called, packageID(owner.item), called.Name())
	if _, ok := group.functions[id]; !ok {
		return ""
	}
	return id
}

func depthFunctionID(item source, object *types.Func, packageID, fallback string) string {
	if object == nil {
		return packageID + "/" + fallback
	}
	signature, _ := object.Type().(*types.Signature)
	if signature == nil || signature.Recv() == nil {
		return packageID + "/" + object.Name()
	}
	return packageID + "/" + depthReceiverName(signature.Recv().Type()) + "." + object.Name()
}

func depthReceiverName(typ types.Type) string {
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem()
	}
	if named, ok := typ.(*types.Named); ok && named.Obj() != nil {
		return named.Obj().Name()
	}
	return types.TypeString(typ, nil)
}

func depthFormalDependencies(item source, expression ast.Expr, owner *types.Func) []int {
	if owner == nil || expression == nil {
		return nil
	}
	params := map[types.Object]int{}
	signature := owner.Type().(*types.Signature)
	for index := 0; index < signature.Params().Len(); index++ {
		params[signature.Params().At(index)] = index
	}
	seen := map[int]bool{}
	var result []int
	ast.Inspect(expression, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if index, found := params[item.typeInfo.ObjectOf(ident)]; found && !seen[index] {
			seen[index] = true
			result = append(result, index)
		}
		return true
	})
	sort.Ints(result)
	return result
}

func rewriteDepthFormalPaths(group *depthNamespace, functionID string, paths map[int]string) {
	if len(paths) == 0 {
		return
	}
	for functionIndex := range group.flow.Functions {
		if group.flow.Functions[functionIndex].ID != functionID {
			continue
		}
		for formalIndex := range group.flow.Functions[functionIndex].Formals {
			if path := paths[formalIndex]; path != "" {
				group.flow.Functions[functionIndex].Formals[formalIndex].Path = path
			}
		}
	}
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func goDepthSignature(signature *types.Signature) string {
	if signature == nil {
		return ""
	}
	params := make([]string, signature.Params().Len())
	for index := range params {
		params[index] = types.TypeString(signature.Params().At(index).Type(), nil)
	}
	if signature.Variadic() && len(params) > 0 {
		params[len(params)-1] = "..." + params[len(params)-1]
	}
	results := make([]string, signature.Results().Len())
	for index := range results {
		results[index] = types.TypeString(signature.Results().At(index).Type(), nil)
	}
	return "(" + strings.Join(params, ",") + ")->(" + strings.Join(results, ",") + ")"
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
