package native

import "encoding/json"

func dedupeScoreMetadata(inputs *scoreInputs) {
	inputs.diagnostics = dedupeMetadata(inputs.diagnostics, true)
	inputs.plans = dedupeMetadata(inputs.plans, false)
}

func dedupeMetadata(values []map[string]any, ignoreUnit bool) []map[string]any {
	seen := make(map[string]bool, len(values))
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		normalized := metadataWithout(value, ignoreUnit)
		encoded, err := json.Marshal(normalized)
		if err != nil {
			continue
		}
		identity := string(encoded)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		result = append(result, metadataWithoutInvocation(value))
	}
	return result
}

func metadataWithout(value map[string]any, ignoreUnit bool) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		if key == "invocation_id" || (ignoreUnit && key == "unit_id") {
			continue
		}
		result[key] = item
	}
	return result
}

func metadataWithoutInvocation(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		if key != "invocation_id" {
			result[key] = item
		}
	}
	return result
}
