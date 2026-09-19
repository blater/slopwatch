package depth

import (
	"sort"
	"strings"
)

func (a *recipeBuilder) Formal(index int, typ string) (RecipeID, error) {
	if index < 0 {
		return a.unknownResult(ReasonInvalidFormal, "negative formal index")
	}
	if typ == "" {
		return a.unknownResult(ReasonInvalidType, "formal type is empty")
	}
	return a.intern(RecipeNode{Kind: KindFormal, Type: typ, FormalIndex: index}), nil
}

func (a *recipeBuilder) Constant(typ, literal string) RecipeID {
	return a.intern(RecipeNode{Kind: KindConstant, Type: typ, Literal: literal})
}

func (a *recipeBuilder) FieldRead(typ, field string, receiver RecipeID) (RecipeID, error) {
	if typ == "" {
		return a.unknownResult(ReasonInvalidType, "field read type is empty")
	}
	if field == "" {
		return a.unknownResult(ReasonUnknownOperand, "field identity is empty")
	}
	if err := a.validateChildren([]RecipeID{receiver}, 1, 1); err != nil {
		return a.unknownResult(err.(*RecipeError).Code, err.Error())
	}
	return a.intern(RecipeNode{Kind: KindFieldRead, Type: typ, Field: field, Children: []RecipeID{receiver}}), nil
}

func (a *recipeBuilder) Pack(typ string, fields []FieldRecipeBinding) (RecipeID, error) {
	canon := append([]FieldRecipeBinding(nil), fields...)
	for _, field := range canon {
		if _, ok := a.nodes[field.Recipe]; !ok {
			return a.unknownResult(ReasonInvalidChild, string(field.Recipe))
		}
	}
	sort.Slice(canon, func(i, j int) bool {
		if canon[i].Field == canon[j].Field {
			return canon[i].Recipe < canon[j].Recipe
		}
		return canon[i].Field < canon[j].Field
	})
	for i := 1; i < len(canon); i++ {
		if canon[i-1].Field == canon[i].Field {
			return a.unknownResult(ReasonDuplicateField, canon[i].Field)
		}
	}
	return a.intern(RecipeNode{Kind: KindPack, Type: typ, Fields: canon}), nil
}

func (a *recipeBuilder) Select(typ string, predicate, whenTrue, whenFalse RecipeID) (RecipeID, error) {
	if err := a.validateChildren([]RecipeID{predicate, whenTrue, whenFalse}, 3, 3); err != nil {
		return a.unknownResult(err.(*RecipeError).Code, err.Error())
	}
	if whenTrue == whenFalse {
		return whenTrue, nil
	}
	predicate, whenTrue, whenFalse = simplifySelectPredicate(a, predicate, whenTrue, whenFalse)
	whenTrue, whenFalse = simplifySelectBranches(a, predicate, whenTrue, whenFalse)
	if whenTrue == whenFalse {
		return whenTrue, nil
	}
	if node, ok := a.nodes[predicate]; ok && node.Type != "" && node.Type != "bool" && node.Type != "boolean" {
		return a.unknownResult(ReasonInvalidType, node.Type)
	}
	if isBooleanType(typ) && booleanLiteral(a, whenTrue, "true") && booleanLiteral(a, whenFalse, "false") {
		return predicate, nil
	}
	if node, ok := a.nodes[predicate]; ok && node.Kind == KindConstant {
		if strings.EqualFold(strings.TrimSpace(node.Literal), "true") {
			return whenTrue, nil
		}
		if strings.EqualFold(strings.TrimSpace(node.Literal), "false") {
			return whenFalse, nil
		}
	}
	return a.intern(RecipeNode{Kind: KindSelect, Type: typ, Predicate: predicate, TrueValue: whenTrue, FalseValue: whenFalse}), nil
}

func (a *recipeBuilder) RecurrenceRef(typ, identity string, references []RecipeID) (RecipeID, error) {
	if identity == "" {
		return a.unknownResult(ReasonInvalidRecurrence, "empty recurrence identity")
	}
	if err := a.validateChildren(references, 0, -1); err != nil {
		return a.unknownResult(err.(*RecipeError).Code, err.Error())
	}
	canon := append([]RecipeID(nil), references...)
	sort.Slice(canon, func(i, j int) bool { return canon[i] < canon[j] })
	return a.intern(RecipeNode{Kind: KindRecurrence, Type: typ, RecurrenceID: identity, Children: canon}), nil
}

func (a *recipeBuilder) Storage(typ, identity string) (RecipeID, error) {
	if typ == "" {
		return a.unknownResult(ReasonInvalidType, "storage type is empty")
	}
	if identity == "" {
		return a.unknownResult(ReasonUnknownOperand, "storage identity is empty")
	}
	return a.intern(RecipeNode{Kind: KindStorage, Type: typ, StorageID: identity}), nil
}

func (a *recipeMetadata) Lookup(id RecipeID) (RecipeNode, bool) {
	node, ok := a.nodes[id]
	if !ok {
		return RecipeNode{}, false
	}
	out := node.RecipeNode
	out.Children = append([]RecipeID(nil), out.Children...)
	out.Fields = append([]FieldRecipeBinding(nil), out.Fields...)
	return out, true
}

func (a *recipeMetadata) Snapshot() []RecipeNode {
	ids := make([]string, 0, len(a.nodes))
	for id := range a.nodes {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	out := make([]RecipeNode, 0, len(ids))
	for _, raw := range ids {
		node, _ := a.Lookup(RecipeID(raw))
		out = append(out, node)
	}
	return out
}
