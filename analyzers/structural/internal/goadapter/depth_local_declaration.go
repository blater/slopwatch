package goadapter

import (
	"go/ast"
	"go/token"
	"slopslap.dev/structural/internal/facts"
)

func lowerDepthLocalDeclaration(b *depthLowering, statement *ast.DeclStmt) {
	declaration, ok := statement.Decl.(*ast.GenDecl)
	if !ok || declaration.Tok != token.VAR {
		b.unknown()
		return
	}
	for _, spec := range declaration.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			b.unknown()
			continue
		}
		values := []string{}
		for _, expression := range value.Values {
			values = append(values, lowerDepthExpression(b, expression))
		}
		if len(values) == 1 && len(value.Names) > 1 {
			if tuple, ok := b.tuples[values[0]]; ok {
				values = tuple
			}
		}
		if len(values) != 0 && len(values) != len(value.Names) {
			b.unknown()
			continue
		}
		for index, name := range value.Names {
			if name.Name == "_" {
				continue
			}
			object := b.source.typeInfo.Defs[name]
			if object == nil {
				b.unknown()
				continue
			}
			id := ""
			if len(values) > 0 {
				id = values[index]
			} else {
				typ, kind, _ := depthType(object.Type())
				id = lowerDepthZero(b, typ, kind)
			}
			b.values[object] = id
		}
	}
}

func lowerDepthZero(b *depthLowering, typ string, kind facts.FlowValueKind) string {
	literal := ""
	switch kind {
	case facts.FlowKindError:
		return lowerDepthNilError(b)
	case facts.FlowKindBoolean:
		literal = "false"
	case facts.FlowKindNumeric:
		literal = "0"
	case facts.FlowKindString:
		literal = `""`
	default:
		return b.unknown()
	}
	return b.emit(facts.Instruction{Opcode: facts.OpConstant, Type: typ, ValueKind: kind, Value: &facts.Value{Type: typ, ValueKind: kind, Constant: literal}})
}
