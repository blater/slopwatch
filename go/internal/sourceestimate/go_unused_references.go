package sourceestimate

import (
	"go/ast"
)

func (p *goUnusedInputPackage) indexReferences(functions []*goUnusedInputFunction) {
	for _, function := range functions {
		p.byName[function.decl.Name.Name] = append(p.byName[function.decl.Name.Name], function)
		p.refValid[function] = true
	}
	for _, file := range p.files {
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			candidates := p.byName[ident.Name]
			if len(candidates) == 0 {
				return true
			}
			if goUnusedIgnoreReferenceIdent(p.parents, ident) {
				return true
			}
			for _, function := range candidates {
				p.observeUnusedReference(function, ident)
			}
			return true
		})
	}
}

func goUnusedOwnSignature(parents map[ast.Node]ast.Node, decl *ast.FuncDecl, ident *ast.Ident) bool {
	field, ok := parents[ident].(*ast.Field)
	if !ok {
		return false
	}
	fieldList, ok := parents[field].(*ast.FieldList)
	if !ok {
		return false
	}
	functionType, ok := parents[fieldList].(*ast.FuncType)
	if !ok {
		return false
	}
	for node := parents[functionType]; node != nil; node = parents[node] {
		if node == decl {
			return true
		}
		if _, isFunction := node.(*ast.FuncDecl); isFunction {
			return false
		}
	}
	return false
}

func goUnusedIgnoreReferenceIdent(parents map[ast.Node]ast.Node, ident *ast.Ident) bool {
	switch parent := parents[ident].(type) {
	case *ast.SelectorExpr:
		return parent.Sel == ident
	case *ast.KeyValueExpr:
		return parent.Key == ident
	case *ast.TypeSpec:
		return parent.Name == ident
	case *ast.Field:
		for _, name := range parent.Names {
			if name != ident {
				continue
			}
			fieldList, ok := parents[parent].(*ast.FieldList)
			if !ok {
				return true
			}
			if _, functionType := parents[fieldList].(*ast.FuncType); functionType {
				return false
			}
			return true
		}
	}
	return false
}

func (p *goUnusedInputPackage) observeUnusedReference(function *goUnusedInputFunction, ident *ast.Ident) {
	if ident == function.decl.Name {
		return
	}
	if goUnusedOwnSignature(p.parents, function.decl, ident) {
		return
	}
	call, ok := p.parents[ident].(*ast.CallExpr)
	if !ok || call.Fun != ident || len(call.Args) != len(function.paramNames) {
		p.refValid[function] = false
		return
	}
	caller, callerAt := p.enclosingFunction(call)
	if caller == nil {
		p.refValid[function] = false
		return
	}
	if !goUnusedCallReachable(p.parents, call) {
		return
	}
	p.refs[function] = append(p.refs[function], goUnusedInputCall{call: call, caller: caller, callerAt: callerAt})
}
