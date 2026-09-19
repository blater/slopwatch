package depth

import (
	"sort"
	"strconv"
	"strings"
)

type substitutionKey struct {
	node RecipeID
	bind string
}

func bindingKey(bindings map[int]RecipeID) string {
	keys := make([]int, 0, len(bindings))
	for key := range bindings {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(strconv.Itoa(key))
		b.WriteByte(':')
		b.WriteString(string(bindings[key]))
		b.WriteByte(';')
	}
	return b.String()
}

// Substitute replaces only formal nodes by positional recipe IDs. It never
// inspects or rewrites expression text. Unknown/missing bindings return a
// typed unknown node and a RecipeError.
func (a *recipeSubstituter) Substitute(root RecipeID, bindings map[int]RecipeID) (RecipeID, error) {
	if _, ok := a.nodes[root]; !ok {
		return a.unknownResult(ReasonUnknownRecipe, string(root))
	}
	for _, actual := range bindings {
		if _, ok := a.nodes[actual]; !ok {
			return a.unknownResult(ReasonInvalidChild, string(actual))
		}
	}
	memo := make(map[substitutionKey]RecipeID)
	keySuffix := bindingKey(bindings)
	var sub func(RecipeID) (RecipeID, error)
	sub = func(id RecipeID) (RecipeID, error) {
		key := substitutionKey{node: id, bind: keySuffix}
		if out, ok := memo[key]; ok {
			return out, nil
		}
		n := a.nodes[id]
		if n.Kind == KindFormal {
			if actual, ok := bindings[n.FormalIndex]; ok {
				memo[key] = actual
				return actual, nil
			}
			out, err := a.unknownResult(ReasonMissingFormal, strconv.Itoa(n.FormalIndex))
			memo[key] = out
			return out, err
		}
		if n.Kind == KindUnknown {
			memo[key] = id
			return id, &RecipeError{Code: ReasonUnknownOperand, Message: n.Literal}
		}
		if n.Kind == KindLoop {
			if out, done, err := pruneLoopInitial(n, a.nodes, sub); done {
				memo[key] = out
				return out, err
			}
		}
		if n.Kind == KindSelect {
			out, err := substituteSelectNode(a, n, sub)
			memo[key] = out
			return out, err
		}
		children, err := substituteChildren(n.Children, sub)
		if err != nil {
			return rememberSubstitutionError(a, memo, key, err)
		}
		fields, err := substituteFields(n.Fields, sub)
		if err != nil {
			return rememberSubstitutionError(a, memo, key, err)
		}
		predicate, trueValue, falseValue, err := substituteSelectParts(n, sub)
		if err != nil {
			return rememberSubstitutionError(a, memo, key, err)
		}
		out, err := rebuildSubstituted(a, n, children, fields, predicate, trueValue, falseValue)
		memo[key] = out
		return out, err
	}
	return sub(root)
}

func substituteSelectNode(a *recipeSubstituter, node recipeNode, sub func(RecipeID) (RecipeID, error)) (RecipeID, error) {
	predicate, err := sub(node.Predicate)
	if err != nil {
		return predicate, err
	}
	if constant, ok := a.nodes[predicate]; ok && constant.Kind == KindConstant {
		if strings.EqualFold(strings.TrimSpace(constant.Literal), "true") {
			return sub(node.TrueValue)
		}
		if strings.EqualFold(strings.TrimSpace(constant.Literal), "false") {
			return sub(node.FalseValue)
		}
	}
	whenTrue, err := sub(node.TrueValue)
	if err != nil {
		return whenTrue, err
	}
	whenFalse, err := sub(node.FalseValue)
	if err != nil {
		return whenFalse, err
	}
	return a.builder.Select(node.Type, predicate, whenTrue, whenFalse)
}

func substituteChildren(children []RecipeID, sub func(RecipeID) (RecipeID, error)) ([]RecipeID, error) {
	out := make([]RecipeID, len(children))
	for i, child := range children {
		var err error
		out[i], err = sub(child)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func substituteFields(fields []FieldRecipeBinding, sub func(RecipeID) (RecipeID, error)) ([]FieldRecipeBinding, error) {
	out := make([]FieldRecipeBinding, len(fields))
	for i, field := range fields {
		out[i].Field = field.Field
		var err error
		out[i].Recipe, err = sub(field.Recipe)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func substituteSelectParts(n recipeNode, sub func(RecipeID) (RecipeID, error)) (RecipeID, RecipeID, RecipeID, error) {
	if n.Predicate == "" {
		return "", "", "", nil
	}
	predicate, err := sub(n.Predicate)
	if err != nil {
		return "", "", "", err
	}
	trueValue, err := sub(n.TrueValue)
	if err != nil {
		return "", "", "", err
	}
	falseValue, err := sub(n.FalseValue)
	return predicate, trueValue, falseValue, err
}

func rememberSubstitutionError(a *recipeSubstituter, memo map[substitutionKey]RecipeID, key substitutionKey, err error) (RecipeID, error) {
	code, message := ReasonUnknownOperand, err.Error()
	if typed, ok := err.(*RecipeError); ok {
		code, message = typed.Code, typed.Message
	}
	unknown, _ := a.unknownResult(code, message)
	memo[key] = unknown
	return unknown, err
}

func rebuildSubstituted(a *recipeSubstituter, n recipeNode, children []RecipeID, fields []FieldRecipeBinding, predicate, trueValue, falseValue RecipeID) (RecipeID, error) {
	switch n.Kind {
	case KindPrimitive:
		return buildPrimitive(a.builder.recipeStore, n.Type, n.ArithmeticMode, n.Operator, children)
	case KindFieldRead:
		if len(children) != 1 {
			return a.unknownResult(ReasonInvalidArity, "field read")
		}
		return a.builder.FieldRead(n.Type, n.Field, children[0])
	case KindPack:
		return a.builder.Pack(n.Type, fields)
	case KindSelect:
		return a.builder.Select(n.Type, predicate, trueValue, falseValue)
	case KindStorage:
		return a.builder.Storage(n.Type, n.StorageID)
	default:
		return a.intern(RecipeNode{Kind: n.Kind, Type: n.Type, Operator: n.Operator, ArithmeticMode: n.ArithmeticMode,
			FormalIndex: n.FormalIndex, Literal: n.Literal, Field: n.Field, Predicate: predicate,
			TrueValue: trueValue, FalseValue: falseValue, RecurrenceID: n.RecurrenceID, StorageID: n.StorageID,
			Children: children, Fields: fields}), nil
	}
}

// SubstituteResult includes deterministic work accounting for callers that
// need eligibility charges as well as the substituted identity.
type SubstituteResult struct {
	Recipe        RecipeID
	ElementVisits int
	Unknown       bool
	Reason        string
}

func (a *recipeSubstituter) SubstituteWithAccounting(root RecipeID, bindings map[int]RecipeID) SubstituteResult {
	out, err := a.Substitute(root, bindings)
	input := a.analyzer.BuildOutcome(root)
	output := a.analyzer.BuildOutcome(out)
	result := SubstituteResult{Recipe: out, ElementVisits: input.ElementVisits + output.ElementVisits + len(bindings)}
	if output.Unknown {
		result.Unknown = true
		result.Reason = output.Reason
	}
	if err != nil {
		result.Unknown = true
		if typed, ok := err.(*RecipeError); ok {
			result.Reason = typed.Code
		} else {
			result.Reason = ReasonUnknownOperand
		}
	}
	return result
}
