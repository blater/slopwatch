package goadapter

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
)

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
