package goadapter

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slopslap.dev/structural/internal/facts"
)

type depthLowering struct {
	tuples      map[string][]string
	errorValues map[string]bool
	errorResult bool
	source      source
	function    facts.FlowFunction
	current     int
	next        int
	values      map[types.Object]string
	pkg         *types.Package
	results     []string
	resultObjs  []types.Object
	allNamed    bool
}

func lowerDepthFunction(item source, declaration *ast.FuncDecl, id string) facts.FlowFunction {
	b := &depthLowering{source: item, function: facts.FlowFunction{ID: id, Entry: "entry", Blocks: []facts.FlowBlock{{ID: "entry"}}}, values: map[types.Object]string{}, errorValues: map[string]bool{}, tuples: map[string][]string{}}
	object, _ := item.typeInfo.Defs[declaration.Name].(*types.Func)
	if object == nil || declaration.Body == nil {
		b.unknown()
		return b.function
	}
	signature := object.Type().(*types.Signature)
	b.errorResult = depthErrorResult(signature)
	b.allNamed = true
	b.pkg = object.Pkg()
	for index := 0; index < signature.Results().Len(); index++ {
		typ, kind, _ := depthType(signature.Results().At(index).Type())
		b.function.Results = append(b.function.Results, facts.Formal{ID: fmt.Sprintf("result%d", index), Type: typ, ValueKind: kind})
		result := signature.Results().At(index)
		resultID := fmt.Sprintf("result%d", index)
		b.results = append(b.results, resultID)
		b.resultObjs = append(b.resultObjs, result)
		if result.Name() != "" {
			b.values[result] = resultID
		} else {
			b.allNamed = false
		}
	}
	for index := 0; index < signature.Params().Len(); index++ {
		parameter := signature.Params().At(index)
		typ, kind, _ := depthType(parameter.Type())
		formal := facts.Formal{ID: fmt.Sprintf("arg%d", index), Path: fmt.Sprintf("%s/arg%d", id, index), Type: typ, ValueKind: kind}
		b.function.Formals = append(b.function.Formals, formal)
		b.values[parameter] = formal.ID
	}
	lowerDepthStatements(b, declaration.Body.List)
	return b.function
}
func (b *depthLowering) emit(in facts.Instruction) string {
	b.next++
	in.ID = fmt.Sprintf("n%d", b.next)
	if len(in.Results) == 0 && in.Opcode != facts.OpReturn && in.Opcode != facts.OpThrow && in.Opcode != facts.OpBranch && !(in.Opcode == facts.OpCall && in.Type == "void") {
		in.Results = []string{in.ID}
	}
	b.function.Blocks[b.current].Instructions = append(b.function.Blocks[b.current].Instructions, in)
	return in.ID
}
func (b *depthLowering) unknown() string {
	return b.emit(facts.Instruction{Opcode: facts.OpUnknown, Type: "unknown", ValueKind: facts.FlowKindUnknown})
}
func lowerDepthExpression(b *depthLowering, expression ast.Expr) string {
	if expression == nil {
		return b.unknown()
	}
	if depthNil(b, expression) {
		return lowerDepthNilError(b)
	}
	typ := b.source.typeInfo.TypeOf(expression)
	name, kind, mode := depthType(typ)
	if value, ok := b.source.typeInfo.Types[expression]; ok && value.Value != nil {
		return b.emit(facts.Instruction{Opcode: facts.OpConstant, Type: name, ValueKind: kind, Value: &facts.Value{Type: name, ValueKind: kind, Constant: value.Value.ExactString()}})
	}
	switch node := expression.(type) {
	case *ast.CallExpr:
		return lowerDepthCall(b, node)
	case *ast.Ident:
		if value, ok := b.values[b.source.typeInfo.ObjectOf(node)]; ok {
			return value
		}
	case *ast.ParenExpr:
		return lowerDepthExpression(b, node.X)
	case *ast.BinaryExpr:
		if value, ok := lowerDepthErrorComparison(b, node); ok {
			return value
		}
		if node.Op == token.LAND || node.Op == token.LOR {
			return lowerDepthShortCircuit(b, node)
		}
		left, right := lowerDepthExpression(b, node.X), lowerDepthExpression(b, node.Y)
		_, _, mode = depthType(b.source.typeInfo.TypeOf(node.X))
		if depthUnmodeledArithmeticExit(b.source, node, mode) {
			return b.unknown()
		}
		return b.emit(facts.Instruction{Opcode: facts.OpPrimitive, Type: name, ValueKind: kind, Operator: node.Op.String(), ArithmeticMode: mode, Operands: []string{left, right}})
	case *ast.UnaryExpr:
		operand := lowerDepthExpression(b, node.X)
		return b.emit(facts.Instruction{Opcode: facts.OpPrimitive, Type: name, ValueKind: kind, Operator: node.Op.String(), ArithmeticMode: mode, Operands: []string{operand}})
	}
	return b.unknown()
}
func lowerDepthStatements(b *depthLowering, statements []ast.Stmt) {
	for _, statement := range statements {
		if depthTerminated(b) {
			return
		}
		lowerDepthStatement(b, statement)
	}
}
func depthTerminated(b *depthLowering) bool {
	instructions := b.function.Blocks[b.current].Instructions
	if len(instructions) == 0 {
		return false
	}
	last := instructions[len(instructions)-1].Opcode
	return last == facts.OpReturn || last == facts.OpThrow
}
func lowerDepthStatement(b *depthLowering, statement ast.Stmt) {
	switch node := statement.(type) {
	case *ast.DeclStmt:
		lowerDepthLocalDeclaration(b, node)
	case *ast.ExprStmt:
		lowerDepthExpression(b, node.X)
	case *ast.ReturnStmt:
		lowerDepthReturn(b, node)
	case *ast.AssignStmt:
		lowerDepthAssignment(b, node)
	case *ast.IncDecStmt:
		lowerDepthIncrement(b, node)
	case *ast.BlockStmt:
		lowerDepthStatements(b, node.List)
	case *ast.ForStmt:
		lowerDepthLoop(b, node)
	case *ast.IfStmt:
		lowerDepthIf(b, node)
	case *ast.EmptyStmt:
	default:
		b.unknown()
	}
}
func lowerDepthAssignment(b *depthLowering, node *ast.AssignStmt) {
	values := lowerDepthAssignedValues(b, node)
	if len(values) != len(node.Lhs) {
		b.unknown()
		return
	}
	for i, left := range node.Lhs {
		identifier, ok := left.(*ast.Ident)
		if !ok {
			b.unknown()
			continue
		}
		if identifier.Name == "_" {
			continue
		}
		object := b.source.typeInfo.ObjectOf(identifier)
		if object == nil {
			b.unknown()
			continue
		}
		if _, local := b.values[object]; !local && node.Tok != token.DEFINE {
			b.unknown()
			continue
		}
		if node.Tok != token.ASSIGN && node.Tok != token.DEFINE {
			old, ok := b.values[object]
			if !ok {
				b.unknown()
				continue
			}
			typ, kind, mode := depthType(object.Type())
			operator := node.Tok.String()
			operator = operator[:len(operator)-1]
			values[i] = b.emit(facts.Instruction{Opcode: facts.OpPrimitive, Type: typ, ValueKind: kind, ArithmeticMode: mode, Operator: operator, Operands: []string{old, values[i]}})
		}
		b.values[object] = values[i]
	}
}

func depthUnmodeledArithmeticExit(item source, node *ast.BinaryExpr, mode string) bool {
	if mode != "integer" {
		return false
	}
	switch node.Op {
	case token.QUO, token.REM:
		value := item.typeInfo.Types[node.Y].Value
		return value == nil || constant.Sign(value) == 0
	case token.SHL, token.SHR:
		value := item.typeInfo.Types[node.Y].Value
		return value == nil || constant.Sign(value) < 0
	default:
		return false
	}
}
func lowerDepthIncrement(b *depthLowering, node *ast.IncDecStmt) {
	id, ok := node.X.(*ast.Ident)
	if !ok {
		b.unknown()
		return
	}
	object := b.source.typeInfo.ObjectOf(id)
	previous, ok := b.values[object]
	if !ok {
		b.unknown()
		return
	}
	typ, kind, mode := depthType(object.Type())
	one := b.emit(facts.Instruction{Opcode: facts.OpConstant, Type: typ, ValueKind: kind, Value: &facts.Value{Type: typ, ValueKind: kind, Constant: "1"}})
	operator := "+"
	if node.Tok == token.DEC {
		operator = "-"
	}
	b.values[object] = b.emit(facts.Instruction{Opcode: facts.OpPrimitive, Type: typ, ValueKind: kind, ArithmeticMode: mode, Operator: operator, Operands: []string{previous, one}})
}
