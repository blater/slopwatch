package report

import "strconv"

// DepthBoundary is the nullable, versioned v4 depth result for one stable
// boundary. Raw contains only the compact diagnostic fields that do not have
// typed report equivalents; detailed adapter evidence is stored in Evidence.
type DepthBoundary struct {
	DeclarationFiles     []string       `json:"declaration_files,omitempty"`
	Scope                string         `json:"scope,omitempty"`
	ID                   string         `json:"id"`
	Boundary             map[string]any `json:"boundary,omitempty"`
	State                string         `json:"state"`
	PolicyRevision       string         `json:"policy_revision,omitempty"`
	InventoryFingerprint string         `json:"inventory_fingerprint,omitempty"`
	Estimated            bool           `json:"estimated,omitempty"`
	Shallow              *float64       `json:"shallow"`
	Dependencies         []string       `json:"dependencies,omitempty"`
	Reasons              []any          `json:"reasons,omitempty"`
	PreciseReasons       []any          `json:"precise_reasons,omitempty"`
	Evidence             []any          `json:"evidence,omitempty"`
	Files                []string       `json:"files,omitempty"`
	Raw                  map[string]any `json:"raw,omitempty"`
}

// CompactDepthBoundary removes legacy duplicated payload from a boundary
// loaded from a report or cache. Typed fields remain available to scoring and
// display; Raw retains only proof fields that have no typed projection.
// Evidence maps are copied before old preformatted prose is removed so callers
// can compact a cached value without mutating another report sharing it.
func CompactDepthBoundary(depth DepthBoundary) DepthBoundary {
	depth.Raw = compactDepthRaw(depth.Raw)
	depth.Reasons = compactDepthReasons(depth.Reasons)
	depth.PreciseReasons = compactDepthReasons(depth.PreciseReasons)
	if len(depth.Evidence) == 0 {
		return depth
	}
	evidence := make([]any, len(depth.Evidence))
	for index, value := range depth.Evidence {
		item, ok := value.(map[string]any)
		if !ok {
			evidence[index] = value
			continue
		}
		copyItem := make(map[string]any, 4)
		for _, key := range []string{"id", "kind", "status", "details"} {
			if itemValue, ok := item[key]; ok {
				copyItem[key] = itemValue
			}
		}
		evidence[index] = copyItem
	}
	depth.Evidence = evidence
	return depth
}

func compactDepthReasons(reasons []any) []any {
	if len(reasons) == 0 {
		return nil
	}
	result := make([]any, len(reasons))
	for index, value := range reasons {
		item, ok := value.(map[string]any)
		if !ok {
			result[index] = value
			continue
		}
		compact := make(map[string]any, 2)
		if code, exists := item["code"]; exists {
			compact["code"] = code
		}
		if message, ok := item["message"].(string); ok && message != "" && message != item["code"] {
			compact["message"] = message
		}
		result[index] = compact
	}
	return result
}

func compactDepthRaw(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	keys := []string{"obligations", "B8", "H", "burden", "alternatives", "family_hidden", "estimate", "responsibility_ratio", "penalty_basis", "graded", "zero_reason"}
	compact := make(map[string]any, len(keys))
	var obligationIDs map[string]string
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			if key == "obligations" {
				value, obligationIDs = compactDepthObligations(value)
			} else if key == "alternatives" && len(obligationIDs) > 0 {
				value = compactDepthAlternatives(value, obligationIDs)
			}
			compact[key] = value
		}
	}
	if len(compact) == 0 {
		return nil
	}
	return compact
}

func compactDepthObligations(value any) (any, map[string]string) {
	items, ok := value.([]any)
	if !ok {
		return value, nil
	}
	result := make([]any, len(items))
	ids := make(map[string]string, len(items))
	for index, item := range items {
		obligation, ok := item.(map[string]any)
		if !ok {
			result[index] = item
			continue
		}
		compact := make(map[string]any, 3)
		for _, key := range []string{"id", "category", "rule"} {
			if field, exists := obligation[key]; exists {
				if key == "id" {
					if id, ok := field.(string); ok && id != "" {
						shortID, exists := ids[id]
						if !exists {
							shortID = "o" + strconv.Itoa(len(ids))
							ids[id] = shortID
						}
						field = shortID
					}
				}
				compact[key] = field
			}
		}
		result[index] = compact
	}
	return result, ids
}

func compactDepthAlternatives(value any, ids map[string]string) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	result := make([]any, len(items))
	for index, item := range items {
		route, ok := item.([]any)
		if !ok {
			result[index] = item
			continue
		}
		shortRoute := make([]any, len(route))
		for routeIndex, reference := range route {
			if id, ok := reference.(string); ok {
				if shortID, exists := ids[id]; exists {
					reference = shortID
				}
			}
			shortRoute[routeIndex] = reference
		}
		result[index] = shortRoute
	}
	return result
}
