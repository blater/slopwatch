package goadapter

import (
	"fmt"
	"go/ast"

	"slopslap.dev/structural/internal/facts"
)

func functionsByPosition(functions []*facts.Function) map[string]*facts.Function {
	output := make(map[string]*facts.Function, len(functions))
	for _, function := range functions {
		output[fmt.Sprintf("%s:%d:%d", function.Location.Path, function.Location.Line, function.Location.Column)] = function
	}
	return output
}

func attachTypeMethod(b *analysisContext, item source, method *ast.FuncDecl, records map[string]*typeRecord, functions map[string]*facts.Function) {
	if method.Recv == nil || len(method.Recv.List) == 0 || method.Body == nil {
		return
	}
	typeName := receiver(method.Recv.List[0].Type)
	record := records[packageID(item)+"|"+typeName]
	if record == nil {
		return
	}
	methodLocation := location(b, method)
	function := functions[fmt.Sprintf("%s:%d:%d", methodLocation.Path, methodLocation.Line, methodLocation.Column)]
	if function == nil {
		return
	}
	record.fact.Methods = append(record.fact.Methods, function)
	if item.typesAvailable {
		record.fact.ForeignTypes = append(record.fact.ForeignTypes, referencedTypes(method, item.typeInfo, record.identity)...)
	} else {
		record.fact.ForeignTypes = append(record.fact.ForeignTypes, namedTypes(method.Type, typeName)...)
	}
	fields, foreign := methodFieldFacts(method, function.ReceiverVar, typeName, record.fields, item)
	record.fact.MethodFields[method.Name.Name] = fields
	record.fact.ForeignFields = append(record.fact.ForeignFields, foreign...)
}

func attachTypeMethods(b *analysisContext, sources []source, records map[string]*typeRecord) {
	functions := functionsByPosition(b.functions)
	for _, item := range sources {
		b.current = item
		for _, declaration := range item.file.Decls {
			if method, ok := declaration.(*ast.FuncDecl); ok {
				attachTypeMethod(b, item, method, records, functions)
			}
		}
	}
}
