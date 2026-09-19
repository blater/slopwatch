package native

import "encoding/json"

// A boundary can be reported for many source files. Keep its shared reasons and
// evidence once, rather than growing the ledger with every file association.
func appendUniqueAny(current []any, values ...any) []any {
	seen := make(map[string]bool, len(current)+len(values))
	var result []any
	for _, items := range [][]any{current, values} {
		for _, item := range items {
			encoded, err := json.Marshal(item)
			if err != nil {
				result = append(result, item)
				continue
			}
			key := string(encoded)
			if !seen[key] {
				seen[key] = true
				result = append(result, item)
			}
		}
	}
	return result
}
