package goadapter

import (
	"go/ast"
	"go/types"
	"sort"
)

type fieldReference struct {
	owner string
	field string
}

type methodFieldCollector struct {
	receiverName string
	receiverType string
	ownFields    map[string]struct{}
	item         source
	fields       map[string]struct{}
	foreign      map[fieldReference]struct{}
	stack        []ast.Node
}

func methodFieldFacts(method *ast.FuncDecl, receiverName, receiverType string, ownFields map[string]struct{}, item source) ([]string, []string) {
	collector := &methodFieldCollector{
		receiverName: receiverName, receiverType: receiverType, ownFields: ownFields, item: item,
		fields: make(map[string]struct{}), foreign: make(map[fieldReference]struct{}), stack: make([]ast.Node, 0, 16),
	}
	ast.Inspect(method.Body, collector.inspect)
	fieldList := make([]string, 0, len(collector.fields))
	foreignList := make([]string, 0, len(collector.foreign))
	for value := range collector.fields {
		fieldList = append(fieldList, value)
	}
	for value := range collector.foreign {
		foreignList = append(foreignList, value.owner+"."+value.field)
	}
	sort.Strings(fieldList)
	sort.Strings(foreignList)
	return fieldList, foreignList
}

func (collector *methodFieldCollector) inspect(node ast.Node) bool {
	if node == nil {
		collector.stack = collector.stack[:len(collector.stack)-1]
		return false
	}
	var parent ast.Node
	if len(collector.stack) > 0 {
		parent = collector.stack[len(collector.stack)-1]
	}
	collector.stack = append(collector.stack, node)
	selector, ok := node.(*ast.SelectorExpr)
	if !ok || isCalledSelector(parent, selector) {
		return true
	}
	if collector.item.typesAvailable {
		collector.collectTyped(selector)
	} else {
		collector.collectUntyped(selector)
	}
	return true
}

func isCalledSelector(parent ast.Node, selector *ast.SelectorExpr) bool {
	call, ok := parent.(*ast.CallExpr)
	return ok && call.Fun == selector
}

func (collector *methodFieldCollector) collectTyped(selector *ast.SelectorExpr) {
	field, isField := selectionObjectField(collector.item.typeInfo.Selections[selector])
	if !isField {
		return
	}
	owner := receiverTypeName(collector.item.typeInfo.TypeOf(selector.X))
	if owner == collector.receiverType {
		if _, exists := collector.ownFields[field.Name()]; exists {
			collector.fields[field.Name()] = struct{}{}
			return
		}
	}
	collector.foreign[fieldReference{owner: owner, field: field.Name()}] = struct{}{}
}

func (collector *methodFieldCollector) collectUntyped(selector *ast.SelectorExpr) {
	base, ok := selector.X.(*ast.Ident)
	if !ok {
		return
	}
	if base.Name == collector.receiverName {
		if _, exists := collector.ownFields[selector.Sel.Name]; exists {
			collector.fields[selector.Sel.Name] = struct{}{}
		}
		return
	}
	if _, imported := collector.item.imports[base.Name]; imported {
		return
	}
	collector.foreign[fieldReference{owner: base.Name, field: selector.Sel.Name}] = struct{}{}
}

func selectionObjectField(selection *types.Selection) (*types.Var, bool) {
	if selection == nil || selection.Kind() != types.FieldVal {
		return nil, false
	}
	field, ok := selection.Obj().(*types.Var)
	return field, ok && field.IsField()
}
