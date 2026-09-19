package depth

import (
	"slopslap.dev/structural/internal/facts"
	"sort"
)

// Validation switches are separate from P: selecting whether to validate does
// not itself satisfy the policy rule for distinct payload-consuming behaviors.
func validationSelectors(arena *RecipeArena, result FlowEvaluation, charge func(int) bool) ([]int, string) {
	payload := map[int]bool{}
	rejected := map[int]bool{}
	booleans := map[int]bool{}
	for _, completion := range result.Completions {
		if !charge(1) {
			return nil, "work_limit"
		}
		if completion.Kind == facts.EdgeNormal {
			for _, value := range completion.Values {
				if value.Kind == KindError {
					continue
				}
				collectPayloadInputs(arena, value.Recipe, payload, charge)
			}
			continue
		}
		recipeAny(arena, completion.Guard, charge, func(node RecipeNode) bool {
			if node.Kind == KindFormal {
				rejected[node.FormalIndex] = true
				if isBooleanType(node.Type) {
					booleans[node.FormalIndex] = true
				}
			}
			return false
		})
	}
	if !charge(0) {
		return nil, "work_limit"
	}
	relevant := false
	for index := range payload {
		relevant = relevant || rejected[index]
	}
	if !relevant {
		return nil, ""
	}
	var out []int
	for index := range booleans {
		if !payload[index] {
			out = append(out, index)
		}
	}
	sort.Ints(out)
	return out, ""
}

func collectPayloadInputs(arena *RecipeArena, root RecipeID, inputs map[int]bool, charge func(int) bool) {
	seen := map[RecipeID]bool{}
	pending := []RecipeID{root}
	for len(pending) != 0 {
		if !charge(1) {
			return
		}
		id := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		node, ok := arena.Lookup(id)
		if !ok {
			continue
		}
		if node.Kind == KindFormal {
			inputs[node.FormalIndex] = true
		}
		if !charge(2 + len(node.Children) + len(node.Fields)) {
			return
		}
		pending = append(pending, node.Children...)
		for _, field := range node.Fields {
			pending = append(pending, field.Recipe)
		}
		if node.Kind == KindSelect {
			pending = append(pending, node.TrueValue, node.FalseValue)
		}
	}
}
