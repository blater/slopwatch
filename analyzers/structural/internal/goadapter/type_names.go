package goadapter

import (
	"go/ast"
	"go/types"
	"sort"
)

var builtinTypes = map[string]struct{}{
	"any": {}, "bool": {}, "byte": {}, "complex64": {}, "complex128": {},
	"error": {}, "float32": {}, "float64": {}, "int": {}, "int8": {}, "int16": {},
	"int32": {}, "int64": {}, "rune": {}, "string": {}, "uint": {}, "uint8": {},
	"uint16": {}, "uint32": {}, "uint64": {}, "uintptr": {},
}

func namedTypes(node ast.Node, self string) []string {
	output := make([]string, 0)
	var visit func(ast.Expr)
	visitFields := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			visit(field.Type)
		}
	}
	visit = func(current ast.Expr) {
		switch value := current.(type) {
		case *ast.SelectorExpr:
			if prefix, ok := value.X.(*ast.Ident); ok {
				output = append(output, prefix.Name+"."+value.Sel.Name)
			} else {
				visit(value.X)
			}
		case *ast.Ident:
			if value.Name != self {
				if _, builtin := builtinTypes[value.Name]; !builtin && value.Name != "comparable" {
					output = append(output, value.Name)
				}
			}
		case *ast.ParenExpr:
			visit(value.X)
		case *ast.StarExpr:
			visit(value.X)
		case *ast.ArrayType:
			visit(value.Elt)
		case *ast.MapType:
			visit(value.Key)
			visit(value.Value)
		case *ast.ChanType:
			visit(value.Value)
		case *ast.Ellipsis:
			visit(value.Elt)
		case *ast.IndexExpr:
			visit(value.X)
			visit(value.Index)
		case *ast.IndexListExpr:
			visit(value.X)
			for _, index := range value.Indices {
				visit(index)
			}
		case *ast.FuncType:
			visitFields(value.TypeParams)
			visitFields(value.Params)
			visitFields(value.Results)
		case *ast.StructType:
			visitFields(value.Fields)
		case *ast.InterfaceType:
			visitFields(value.Methods)
		}
	}
	if value, ok := node.(ast.Expr); ok {
		visit(value)
	}
	return output
}

func namedTypeIdentity(value *types.Named) string {
	object := value.Obj()
	if object == nil || object.Pkg() == nil {
		return ""
	}
	return object.Pkg().Path() + "." + object.Name()
}

func addReferencedType(output map[string]struct{}, value types.Type, self string) {
	switch current := value.(type) {
	case *types.Alias:
		addReferencedType(output, types.Unalias(current), self)
	case *types.Named:
		identity := namedTypeIdentity(current)
		if identity != "" && identity != self {
			output[identity] = struct{}{}
		}
		for index := 0; index < current.TypeArgs().Len(); index++ {
			addReferencedType(output, current.TypeArgs().At(index), self)
		}
	case *types.Pointer:
		addReferencedType(output, current.Elem(), self)
	case *types.Array:
		addReferencedType(output, current.Elem(), self)
	case *types.Slice:
		addReferencedType(output, current.Elem(), self)
	case *types.Map:
		addReferencedType(output, current.Key(), self)
		addReferencedType(output, current.Elem(), self)
	case *types.Chan:
		addReferencedType(output, current.Elem(), self)
	case *types.Signature:
		addTupleTypes(output, current.Params(), self)
		addTupleTypes(output, current.Results(), self)
	}
}

func addTupleTypes(output map[string]struct{}, tuple *types.Tuple, self string) {
	if tuple == nil {
		return
	}
	for index := 0; index < tuple.Len(); index++ {
		addReferencedType(output, tuple.At(index).Type(), self)
	}
}

func referencedTypes(node ast.Node, info *types.Info, self string) []string {
	output := make(map[string]struct{})
	ast.Inspect(node, func(current ast.Node) bool {
		expression, ok := current.(ast.Expr)
		if ok {
			if value := info.TypeOf(expression); value != nil {
				addReferencedType(output, value, self)
			}
		}
		return true
	})
	values := make([]string, 0, len(output))
	for value := range output {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func receiverTypeName(value types.Type) string {
	for {
		switch current := value.(type) {
		case *types.Pointer:
			value = current.Elem()
		case *types.Alias:
			value = types.Unalias(current)
		case *types.Named:
			return current.Obj().Name()
		default:
			return ""
		}
	}
}
