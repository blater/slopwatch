package metrics

import (
	"slopslap.dev/structural/internal/facts"
	"sort"
)

func canonicalReasons(items []facts.Reason) []facts.Reason {
	type reasonKey struct{ code, dimension, message string }
	indices := make(map[reasonKey]int, len(items))
	result := make([]facts.Reason, 0, len(items))
	for _, item := range items {
		key := reasonKey{item.Code, item.Dimension, item.Message}
		if index, exists := indices[key]; exists {
			result[index].FactIDs = append(result[index].FactIDs, item.FactIDs...)
			continue
		}
		indices[key] = len(result)
		item.FactIDs = append([]string(nil), item.FactIDs...)
		result = append(result, item)
	}
	for index := range result {
		result[index].FactIDs = uniqueStrings(result[index].FactIDs)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Code != result[j].Code {
			return result[i].Code < result[j].Code
		}
		if result[i].Dimension != result[j].Dimension {
			return result[i].Dimension < result[j].Dimension
		}
		return result[i].Message < result[j].Message
	})
	return result
}
func canonicalEvidence(items []facts.Evidence) []facts.Evidence {
	result := append([]facts.Evidence(nil), items...)
	for index := range result {
		result[index].Provenance = canonicalProvenance(result[index].Provenance)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}
func canonicalProvenance(items []facts.Provenance) []facts.Provenance {
	result := append([]facts.Provenance(nil), items...)
	for index := range result {
		result[index].FactIDs = uniqueStrings(result[index].FactIDs)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Artifact != result[j].Artifact {
			return result[i].Artifact < result[j].Artifact
		}
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		if result[i].Span.Line != result[j].Span.Line {
			return result[i].Span.Line < result[j].Span.Line
		}
		if result[i].Span.Column != result[j].Span.Column {
			return result[i].Span.Column < result[j].Span.Column
		}
		return result[i].RuleID < result[j].RuleID
	})
	return result
}

// Preserve the witnesses behind alternative IDs in the report, including
// supported witnesses in a partial assessment. Numeric scoring is unchanged.
func canonicalObligations(items map[string]facts.Obligation) []facts.Obligation {
	result := make([]facts.Obligation, 0, len(items))
	for _, item := range items {
		item.Evidence = uniqueStrings(item.Evidence)
		item.Provenance = canonicalProvenance(item.Provenance)
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
