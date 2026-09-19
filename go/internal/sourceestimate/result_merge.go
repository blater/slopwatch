package sourceestimate

import (
	"sort"
	"strings"
)

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// Merge combines file estimates belonging to one logical source boundary.
// Burden is additive across caller operations; normalized responsibilities
// and residual unknown opportunities are counted once.
func Merge(results ...Result) Result {
	if len(results) == 1 {
		return results[0]
	}
	merged := newMergedResult(results)
	for _, result := range results {
		mergeResult(&merged, result)
	}
	return finishMergedResult(merged)
}

func newMergedResult(results []Result) Result {
	return Result{
		Categories: map[string]float64{}, evidence: map[string]float64{},
		NoAbstractionProven: len(results) > 0, RoleOnly: len(results) > 0,
	}
}

func mergeResult(merged *Result, result Result) {
	merged.Applicable = merged.Applicable || result.Applicable
	merged.Findings = append(merged.Findings, result.Findings...)
	merged.NoAbstractionProven = merged.NoAbstractionProven && result.NoAbstractionProven
	merged.RoleOnly = merged.RoleOnly && result.RoleOnly
	for _, role := range result.Roles {
		merged.Roles = appendUniqueString(merged.Roles, role)
	}
	mergeSupporting(merged, result)
	merged.Abstractions = append(merged.Abstractions, result.Abstractions...)
	merged.Burden += result.Burden
	merged.UncertainBurden += result.UncertainBurden
	merged.Dependencies = append(merged.Dependencies, result.Dependencies...)
	merged.Limitations = append(merged.Limitations, result.Limitations...)
	mergeEvidence(merged, result)
}

func mergeSupporting(merged *Result, result Result) {
	if len(result.Supporting) == 0 {
		return
	}
	if merged.Supporting == nil {
		merged.Supporting = map[string]string{}
	}
	for owner, role := range result.Supporting {
		merged.Supporting[owner] = role
	}
}

func mergeEvidence(merged *Result, result Result) {
	if len(result.evidence) == 0 {
		for category, value := range result.Categories {
			mergeEvidenceItem(merged, "fallback|"+category, category, value)
		}
		return
	}
	for key, value := range result.evidence {
		if _, exists := merged.evidence[key]; exists {
			continue
		}
		category := key
		if separator := strings.IndexByte(category, '|'); separator >= 0 {
			category = category[:separator]
		}
		mergeEvidenceItem(merged, key, category, value)
	}
}

func mergeEvidenceItem(merged *Result, key, category string, value float64) {
	if _, exists := merged.evidence[key]; exists {
		return
	}
	merged.evidence[key] = value
	merged.Categories[category] += value
	merged.Hidden += value
}

func finishMergedResult(merged Result) Result {
	merged.Dependencies = uniqueSorted(merged.Dependencies)
	merged.Limitations = uniqueSorted(merged.Limitations)
	if !merged.Applicable {
		merged.Burden, merged.Hidden = 0, 0
	}
	return merged
}

func uniqueSorted(values []string) []string {
	sort.Strings(values)
	return unique(values)
}
