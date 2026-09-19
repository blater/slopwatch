package depth

import "sort"

// Carrier recipes and field aliases have separate identities. Joining two
// carriers therefore joins corresponding fields, rather than confusing their
// contained references with storage for the carrier itself.
func selectControlRecord(arena *RecipeArena, records map[string]map[string]DomainValue, p RecipeID, a, b DomainValue, charge func(int) bool) DomainValue {
	if !charge(valueWork(a) + valueWork(b)) {
		return typedUnknown(a.Type, a.Kind, "work_limit")
	}
	if a.RecordID == b.RecordID {
		return selectControlValue(arena, p, a, b)
	}
	left, leftOK := records[a.RecordID]
	right, rightOK := records[b.RecordID]
	if !leftOK || !rightOK {
		return selectControlValue(arena, p, a, b)
	}
	fields := joinControlFields(arena, records, p, left, right, charge)
	// Matching carrier IDs allow the metadata join to retain precision. The
	// resulting carrier then receives its own complete field-and-alias identity.
	adjusted := a.Copy()
	adjusted.RecordID = b.RecordID
	out := selectControlValue(arena, p, adjusted, b)
	for _, value := range fields {
		if !charge(valueWork(value)) {
			return typedUnknown(a.Type, a.Kind, "work_limit")
		}
		out.Unknown = out.Unknown || value.Unknown
		out.Reasons = appendUniqueStrings(out.Reasons, value.Reasons...)
	}
	out.RecordID = recordIdentity(out.Recipe, fields)
	records[out.RecordID] = fields
	return out
}
func joinControlFields(arena *RecipeArena, records map[string]map[string]DomainValue, p RecipeID, left, right map[string]DomainValue, charge func(int) bool) map[string]DomainValue {
	names := map[string]bool{}
	for _, fields := range []map[string]DomainValue{left, right} {
		for name := range fields {
			if !charge(1) {
				return nil
			}
			names[name] = true
		}
	}
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	out := make(map[string]DomainValue, len(keys))
	for _, name := range keys {
		if !charge(1) {
			return nil
		}
		a, aOK := left[name]
		b, bOK := right[name]
		if !aOK {
			a = typedUnknown(b.Type, b.Kind, "missing_record_field")
		}
		if !bOK {
			b = typedUnknown(a.Type, a.Kind, "missing_record_field")
		}
		out[name] = selectControlRecord(arena, records, p, a, b, charge)
	}
	return out
}
