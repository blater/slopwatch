package sourceestimate

import (
	"go/ast"
)

func (p *goUnusedInputPackage) indexReferences(functions []*goUnusedInputFunction) {
	for _, function := range functions {
		p.referenceWork++
		p.byName[function.decl.Name.Name] = append(p.byName[function.decl.Name.Name], function)
		p.refValid[function] = true
	}
	// Every duplicate's declaration-name identifier is a non-call reference
	// to every other duplicate. Thus the original scan invalidated the whole
	// bucket, regardless of arity, own-signature references, or live calls.
	// Record that result once instead of rechecking it at every identifier.
	for _, candidates := range p.byName {
		if len(candidates) > 1 {
			for _, function := range candidates {
				p.referenceWork++
				p.refValid[function] = false
			}
		}
	}
	for _, file := range p.files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node != nil {
				p.referenceWork++
			}
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			candidates := p.byName[ident.Name]
			if len(candidates) != 1 {
				return true
			}
			if p.goUnusedIgnoreReferenceIdent(ident) {
				return true
			}
			p.referenceWork++
			p.observeUnusedReference(candidates[0], ident)
			return true
		})
	}
}

func (p *goUnusedInputPackage) goUnusedOwnSignature(decl *ast.FuncDecl, ident *ast.Ident) bool {
	parents := p.parents
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
	return p.context[functionType].signature == decl
}

func (p *goUnusedInputPackage) goUnusedIgnoreReferenceIdent(ident *ast.Ident) bool {
	parents := p.parents
	switch parent := parents[ident].(type) {
	case *ast.SelectorExpr:
		return parent.Sel == ident
	case *ast.KeyValueExpr:
		return parent.Key == ident
	case *ast.TypeSpec:
		return parent.Name == ident
	case *ast.Field:
		if !p.context[ident].fieldName {
			return false
		}
		fieldList, ok := parents[parent].(*ast.FieldList)
		if !ok {
			return true
		}
		_, functionType := parents[fieldList].(*ast.FuncType)
		return !functionType
	}
	return false
}

func (p *goUnusedInputPackage) observeUnusedReference(function *goUnusedInputFunction, ident *ast.Ident) {
	if ident == function.decl.Name {
		return
	}
	if p.goUnusedOwnSignature(function.decl, ident) {
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
	if p.context[call].dead {
		return
	}
	p.refs[function] = append(p.refs[function], goUnusedInputCall{call: call, caller: caller, callerAt: callerAt})
}
