package goadapter

import (
	"go/ast"
	"go/types"
	"sort"
)

func methodFieldFacts(method *ast.FuncDecl, receiverName, receiverType string, ownFields map[string]struct{}, item source) ([]string, []string) {
	fields := make(map[string]struct{})
	foreign := make(map[string]struct{})
	parents := make(map[ast.Node]ast.Node)
	stack := make([]ast.Node, 0)
	ast.Inspect(method.Body, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	ast.Inspect(method.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		base, ok := selector.X.(*ast.Ident)
		if call, ok := parents[selector].(*ast.CallExpr); ok && call.Fun == selector {
			return true
		}
		if item.typesAvailable {
			selection := item.typeInfo.Selections[selector]
			field, isField := selectionObjectField(selection)
			if !isField {
				return true
			}
			owner := receiverTypeName(item.typeInfo.TypeOf(selector.X))
			if owner == receiverType {
				if _, exists := ownFields[field.Name()]; exists {
					fields[field.Name()] = struct{}{}
					return true
				}
			}
			foreign[owner+"."+field.Name()] = struct{}{}
			return true
		}
		if !ok {
			return true
		}
		if base.Name == receiverName {
			if _, exists := ownFields[selector.Sel.Name]; exists {
				fields[selector.Sel.Name] = struct{}{}
			}
			return true
		}
		if _, imported := item.imports[base.Name]; imported {
			return true
		}
		foreign[base.Name+"."+selector.Sel.Name] = struct{}{}
		return true
	})
	fieldList := make([]string, 0, len(fields))
	foreignList := make([]string, 0, len(foreign))
	for value := range fields {
		fieldList = append(fieldList, value)
	}
	for value := range foreign {
		foreignList = append(foreignList, value)
	}
	sort.Strings(fieldList)
	sort.Strings(foreignList)
	return fieldList, foreignList
}

func selectionObjectField(selection *types.Selection) (*types.Var, bool) {
	if selection == nil || selection.Kind() != types.FieldVal {
		return nil, false
	}
	field, ok := selection.Obj().(*types.Var)
	return field, ok && field.IsField()
}
