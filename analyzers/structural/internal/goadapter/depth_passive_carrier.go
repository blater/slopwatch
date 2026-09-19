package goadapter

import (
	"go/ast"
	"go/token"
	"go/types"

	"slopslap.dev/structural/internal/facts"
)

type goCarrier struct {
	fields map[string]bool
	read   map[string]bool
}

// The namespace exemption requires every declaration and method to fit the
// passive role. A carrier beside unrelated exported behavior cannot hide it.
func applyGoPassiveCarrier(group *depthNamespace) {
	for _, reason := range group.boundary.Reasons {
		if reason.Code == "incomplete_package_inventory" {
			return
		}
	}
	carriers := map[string]*goCarrier{}
	for _, item := range group.sources {
		for _, declaration := range item.file.Decls {
			decl, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			if decl.Tok == token.IMPORT || decl.Tok == token.CONST {
				continue
			}
			if decl.Tok != token.TYPE {
				return
			}
			for _, spec := range decl.Specs {
				typ := spec.(*ast.TypeSpec)
				structure, ok := typ.Type.(*ast.StructType)
				if !ok {
					// Named primitive status types add no executable behavior.
					if ident, scalar := typ.Type.(*ast.Ident); scalar && passiveGoScalar(ident.Name) {
						continue
					}
					return
				}
				if typ.TypeParams != nil {
					return
				}
				carrier := &goCarrier{fields: map[string]bool{}, read: map[string]bool{}}
				for _, field := range structure.Fields.List {
					if len(field.Names) == 0 {
						return
					}
					for _, name := range field.Names {
						carrier.fields[name.Name] = true
						carrier.read[name.Name] = name.IsExported()
					}
				}
				if len(carrier.fields) == 0 {
					return
				}
				carriers[typ.Name.Name] = carrier
			}
		}
	}
	if len(carriers) == 0 {
		return
	}
	var methods []facts.RouteFamily
	fields := 0
	for _, item := range group.sources {
		for _, declaration := range item.file.Decls {
			method, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if method.Recv == nil || len(method.Recv.List) != 1 || len(method.Recv.List[0].Names) != 1 || method.Body == nil {
				return
			}
			receiver := method.Recv.List[0]
			typ := receiver.Type
			if pointer, ok := typ.(*ast.StarExpr); ok {
				typ = pointer.X
			}
			name, ok := typ.(*ast.Ident)
			if !ok || carriers[name.Name] == nil {
				return
			}
			carrier := carriers[name.Name]
			parameters := map[string]bool{}
			for _, field := range method.Type.Params.List {
				for _, parameter := range field.Names {
					parameters[parameter.Name] = true
				}
			}
			if !passiveGoMethod(item, method, receiver.Names[0].Name, parameters, carrier) {
				return
			}
			if method.Name.IsExported() {
				id := group.boundary.Identity.Artifact + "/" + name.Name + "." + method.Name.Name
				signature := name.Name + "." + method.Name.Name
				if item.typeInfo != nil {
					if object, ok := item.typeInfo.Defs[method.Name].(*types.Func); ok {
						signature = object.FullName() + types.TypeString(object.Type(), nil)
					}
				}
				route := facts.Route{ID: id, Family: id, TargetFunctionID: id, Boundary: group.boundary.Identity, Signature: signature}
				methods = append(methods, facts.RouteFamily{ID: id, Routes: []facts.Route{route}})
			}
		}
	}
	for _, carrier := range carriers {
		for field := range carrier.fields {
			if !carrier.read[field] {
				return
			}
			fields++
		}
	}
	group.boundary.RouteFamilies = append(group.boundary.RouteFamilies, methods...)
	group.boundary.Evidence = append(group.boundary.Evidence, facts.Evidence{ID: "passive-result-carrier-v1:" + group.boundary.Identity.String(), Kind: "passive-result-carrier-v1", Status: "proven", Details: map[string]any{"fields": fields, "methods": len(methods), "types": len(carriers)}})
}

func passiveGoMethod(item source, method *ast.FuncDecl, receiver string, parameters map[string]bool, carrier *goCarrier) bool {
	for _, statement := range method.Body.List {
		switch node := statement.(type) {
		case *ast.ReturnStmt:
			if len(method.Body.List) != 1 || len(node.Results) == 0 {
				return false
			}
			for _, result := range node.Results {
				field := passiveGoField(result, receiver)
				if !carrier.fields[field] {
					return false
				}
				carrier.read[field] = true
			}
		case *ast.AssignStmt:
			if node.Tok != token.ASSIGN || len(node.Lhs) != len(node.Rhs) {
				return false
			}
			for index, left := range node.Lhs {
				if !carrier.fields[passiveGoField(left, receiver)] || !passiveGoValue(item, node.Rhs[index], parameters) {
					return false
				}
			}
		case *ast.EmptyStmt:
		default:
			return false
		}
	}
	return true
}

func passiveGoField(expression ast.Expr, receiver string) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	owner, ok := selector.X.(*ast.Ident)
	if !ok || owner.Name != receiver {
		return ""
	}
	return selector.Sel.Name
}

func passiveGoValue(item source, expression ast.Expr, parameters map[string]bool) bool {
	switch node := expression.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		if parameters[node.Name] || node.Name == "nil" || node.Name == "true" || node.Name == "false" {
			return true
		}
		if item.typeInfo != nil {
			_, constant := item.typeInfo.Uses[node].(*types.Const)
			return constant
		}
	case *ast.SelectorExpr:
		if item.typeInfo != nil {
			_, constant := item.typeInfo.Uses[node.Sel].(*types.Const)
			return constant
		}
	case *ast.UnaryExpr:
		_, literal := node.X.(*ast.BasicLit)
		return literal && (node.Op == token.SUB || node.Op == token.ADD)
	}
	return false
}

func passiveGoScalar(name string) bool {
	switch name {
	case "bool", "string", "byte", "rune", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64":
		return true
	}
	return false
}
