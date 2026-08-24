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

func methodFieldFacts(method *ast.FuncDecl, receiverName, receiverType string, ownFields map[string]struct{}, item source) ([]string, []string) {
	fields := make(map[string]struct{})
	foreign := make(map[fieldReference]struct{})
	stack := make([]ast.Node, 0, 16)
	ast.Inspect(method.Body, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, node)
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		base, ok := selector.X.(*ast.Ident)
		if call, ok := parent.(*ast.CallExpr); ok && call.Fun == selector {
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
			foreign[fieldReference{owner: owner, field: field.Name()}] = struct{}{}
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
		foreign[fieldReference{owner: base.Name, field: selector.Sel.Name}] = struct{}{}
		return true
	})
	fieldList := make([]string, 0, len(fields))
	foreignList := make([]string, 0, len(foreign))
	for value := range fields {
		fieldList = append(fieldList, value)
	}
	for value := range foreign {
		foreignList = append(foreignList, value.owner+"."+value.field)
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
