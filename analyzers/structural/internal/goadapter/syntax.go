package goadapter

import (
	"fmt"
	"go/ast"
	"strings"

	"slopslap.dev/structural/internal/facts"
)

func location(b *analysisContext, node ast.Node) facts.Location {
	start := b.fset.PositionFor(node.Pos(), false)
	end := b.fset.PositionFor(node.End(), false)
	return facts.Location{Path: b.current.rel, Line: start.Line, Column: start.Column, EndLine: end.Line, EndColumn: end.Column}
}

func receiver(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiver(value.X)
	case *ast.IndexExpr:
		return receiver(value.X)
	case *ast.IndexListExpr:
		return receiver(value.X)
	case *ast.ParenExpr:
		return receiver(value.X)
	default:
		return ""
	}
}

func receiverVariable(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 || len(declaration.Recv.List[0].Names) == 0 {
		return ""
	}
	return declaration.Recv.List[0].Names[0].Name
}

func functionDeclaration(b *analysisContext, declaration *ast.FuncDecl) *facts.Function {
	receiverType := ""
	if declaration.Recv != nil && len(declaration.Recv.List) > 0 {
		receiverType = receiver(declaration.Recv.List[0].Type)
	}
	function := &facts.Function{
		Name: declaration.Name.Name, Receiver: receiverType,
		ReceiverVar: receiverVariable(declaration), Location: location(b, declaration),
	}
	previousReceiver := b.activeReceiver
	b.activeReceiver = function.ReceiverVar
	function.Body = buildStatements(b, declaration.Body.List)
	b.activeReceiver = previousReceiver
	if ast.IsExported(declaration.Name.Name) {
		parameters := signatureTypes(declaration.Type.Params)
		results := signatureTypes(declaration.Type.Results)
		b.operations = append(b.operations, &facts.PublicOperation{
			StableID: fmt.Sprintf("%s:%s:%s", b.current.rel, receiverType, declaration.Name.Name),
			Name:     declaration.Name.Name, OwnerType: receiverType, Location: function.Location,
			Parameters: parameters, Results: results, EmitsOutput: len(results) > 0,
		})
	}
	return function
}

func signatureTypes(fields *ast.FieldList) []*facts.TypeShape {
	if fields == nil {
		return nil
	}
	output := make([]*facts.TypeShape, 0)
	for _, field := range fields.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			output = append(output, signatureShape(field.Type))
		}
	}
	return output
}

func signatureShape(expression ast.Expr) *facts.TypeShape {
	shape := &facts.TypeShape{Kind: "opaque", Complexity: 1}
	var children []*facts.TypeShape
	switch value := expression.(type) {
	case *ast.Ident:
		shape.Name = value.Name
		if isPrimitiveType(value.Name) {
			shape.Kind = "primitive"
		} else {
			shape.Kind = "named"
		}
	case *ast.SelectorExpr:
		shape.Kind = "named"
		shape.Name = receiver(value.X) + "." + value.Sel.Name
	case *ast.StarExpr:
		shape.Kind, children = "pointer", []*facts.TypeShape{signatureShape(value.X)}
	case *ast.ArrayType:
		shape.Kind, children = "array", []*facts.TypeShape{signatureShape(value.Elt)}
	case *ast.MapType:
		shape.Kind, children = "map", []*facts.TypeShape{signatureShape(value.Key), signatureShape(value.Value)}
	case *ast.ChanType:
		shape.Kind, children = "channel", []*facts.TypeShape{signatureShape(value.Value)}
	case *ast.Ellipsis:
		shape.Kind, children = "variadic", []*facts.TypeShape{signatureShape(value.Elt)}
	case *ast.FuncType:
		shape.Kind = "function"
		children = append(children, signatureTypes(value.Params)...)
		children = append(children, signatureTypes(value.Results)...)
	case *ast.StructType:
		shape.Kind = "record"
		children, shape.ExposedMembers = recordSignature(value)
	case *ast.InterfaceType:
		shape.Kind = "interface"
		children = interfaceSignature(value)
	case *ast.IndexExpr:
		shape.Kind, children = "generic", []*facts.TypeShape{signatureShape(value.X), signatureShape(value.Index)}
	case *ast.IndexListExpr:
		shape.Kind = "generic"
		children = genericSignature(value)
	case *ast.ParenExpr:
		return signatureShape(value.X)
	}
	shape.Children = children
	shape.Complexity = shapeComplexity(children)
	shape.StableID = shapeFingerprint(shape)
	return shape
}

func recordSignature(value *ast.StructType) ([]*facts.TypeShape, []string) {
	var children []*facts.TypeShape
	var exposed []string
	for _, field := range value.Fields.List {
		if len(field.Names) == 0 {
			children = append(children, signatureShape(field.Type))
			continue
		}
		for _, name := range field.Names {
			if ast.IsExported(name.Name) {
				exposed = append(exposed, name.Name)
			}
			children = append(children, signatureShape(field.Type))
		}
	}
	return children, exposed
}

func interfaceSignature(value *ast.InterfaceType) []*facts.TypeShape {
	children := make([]*facts.TypeShape, 0, len(value.Methods.List))
	for range value.Methods.List {
		children = append(children, &facts.TypeShape{Kind: "method", StableID: "method", Complexity: 1})
	}
	return children
}

func genericSignature(value *ast.IndexListExpr) []*facts.TypeShape {
	children := []*facts.TypeShape{signatureShape(value.X)}
	for _, index := range value.Indices {
		children = append(children, signatureShape(index))
	}
	return children
}

func shapeComplexity(children []*facts.TypeShape) int {
	complexity := 1
	for _, child := range children {
		complexity += child.Complexity
	}
	return min(complexity, 32)
}

func shapeFingerprint(shape *facts.TypeShape) string {
	parts := []string{shape.Kind, shape.Name}
	for _, child := range shape.Children {
		parts = append(parts, child.StableID)
	}
	for _, member := range shape.ExposedMembers {
		parts = append(parts, member)
	}
	return fmt.Sprintf("%s:%d:%s", strings.Join(parts, ":"), shape.Complexity, strings.Join(shape.ExposedMembers, ","))
}

func isPrimitiveType(name string) bool {
	switch name {
	case "bool", "byte", "complex64", "complex128", "error", "float32", "float64",
		"int", "int8", "int16", "int32", "int64", "rune", "string", "uint", "uint8",
		"uint16", "uint32", "uint64", "uintptr", "any":
		return true
	default:
		return false
	}
}

func functionLiteral(b *analysisContext, literal *ast.FuncLit) *facts.Function {
	position := b.fset.PositionFor(literal.Pos(), false)
	function := &facts.Function{
		Name:     fmt.Sprintf("<anonymous@%d:%d>", position.Line, position.Column),
		Location: location(b, literal), Body: buildStatements(b, literal.Body.List),
	}
	b.functions = append(b.functions, function)
	return function
}
