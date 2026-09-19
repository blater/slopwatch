package depth

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

func transferPack(t *transferStep) {
	if t.in.Type == "" {
		t.unknown("missing_pack_metadata")
		return
	}
	recipes := make([]FieldRecipeBinding, 0, len(t.in.FieldBindings))
	values := make([]DomainValue, 0, len(t.in.FieldBindings))
	fields := map[string]DomainValue{}
	for _, binding := range t.in.FieldBindings {
		if !t.budget.charge(1) {
			return
		}
		if binding.Field == "" {
			t.unknown("missing_field_identity")
			return
		}
		if _, exists := fields[binding.Field]; exists {
			t.unknown("duplicate_field_identity")
			return
		}
		value, ok := t.operand(binding.Value)
		if !ok {
			t.unknown(ReasonUnknownOperand)
			return
		}
		fields[binding.Field] = value
		values = append(values, value)
		recipes = append(recipes, FieldRecipeBinding{Field: binding.Field, Recipe: value.Recipe})
	}
	if !t.budget.charge(len(recipes)) {
		return
	}
	recipe, err := t.arena.Pack(t.in.Type, recipes)
	if err != nil {
		t.unknown(reasonCode(err))
		return
	}
	value, complete := packTransferValues(t, recipe, values)
	if !complete {
		return
	}
	value.RecordID = recordIdentity(recipe, fields)
	t.delta.records = map[string]map[string]DomainValue{value.RecordID: fields}
	t.output(value)
}
func recordIdentity(recipe RecipeID, fields map[string]DomainValue) string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	payload := appendString(nil, string(recipe))
	for _, name := range names {
		value := fields[name]
		payload = appendString(payload, name)
		payload = appendString(payload, string(value.Recipe))
		payload = appendString(payload, value.RecordID)
		payload = appendString(payload, value.Type)
		payload = appendString(payload, string(value.Kind))
		for _, items := range [][]string{value.Concepts, value.Dependencies, value.Reasons} {
			payload = appendString(payload, stringSliceIdentity(items))
		}
		if value.Unknown {
			payload = append(payload, 1)
		} else {
			payload = append(payload, 0)
		}
		for _, root := range value.Aliases.SupportingRoots() {
			payload = appendString(payload, aliasKey(root))
			payload = appendString(payload, string(root.Ownership))
			if root.Multiple {
				payload = append(payload, 1)
			} else {
				payload = append(payload, 0)
			}
			if root.Mutable {
				payload = append(payload, 1)
			} else {
				payload = append(payload, 0)
			}
		}
		payload = appendString(payload, string(value.Aliases.Precision()))
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
func readRecord(t *transferStep, receiver DomainValue, path []PathSegment) bool {
	if receiver.RecordID == "" {
		return false
	}
	value := receiver
	for _, segment := range path {
		if !t.budget.charge(1) {
			return true
		}
		if segment.Kind != "field" || segment.Dynamic {
			return false
		}
		fields, ok := t.state.records[value.RecordID]
		if !ok {
			return false
		}
		value, ok = fields[segment.Name]
		if !ok {
			t.unknown("missing_record_field")
			return true
		}
		if !t.budget.charge(valueWork(value)) {
			return true
		}
	}
	t.output(effectiveValue(t.state, value))
	return true
}

func packTransferValues(t *transferStep, recipe RecipeID, values []DomainValue) (DomainValue, bool) {
	value := PackValues(recipe, t.in.Type, nil)
	for _, field := range values {
		if !t.budget.charge(valueWork(value) + valueWork(field)) {
			return DomainValue{}, false
		}
		value = PackValues(recipe, t.in.Type, []DomainValue{value, field})
	}
	return value, true
}
func stringSliceIdentity(items []string) string {
	var data []byte
	for _, item := range items {
		data = appendString(data, item)
	}
	return string(data)
}
