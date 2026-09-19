package goadapter

import (
	"go/ast"
	"go/types"
)

// A private representation with just one direct field accessor does not expose
// an independent behavioral service. Check the complete resolved method set.
func supportingGoGetter(item source, method *ast.FuncDecl, object *types.Func) bool {
	if object == nil || method.Recv == nil || method.Body == nil || len(method.Body.List) != 1 || len(method.Recv.List) != 1 || len(method.Recv.List[0].Names) != 1 {
		return false
	}
	signature, ok := object.Type().(*types.Signature)
	if !ok || signature.Recv() == nil || signature.Params().Len() != 0 || signature.Results().Len() != 1 {
		return false
	}
	typ := signature.Recv().Type()
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Exported() || named.NumMethods() != 1 {
		return false
	}
	ret, ok := method.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	selector, ok := ret.Results[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	if !ok || receiver.Name != method.Recv.List[0].Names[0].Name || item.typeInfo == nil {
		return false
	}
	selection := item.typeInfo.Selections[selector]
	return selection != nil && selection.Kind() == types.FieldVal && len(selection.Index()) == 1
}
