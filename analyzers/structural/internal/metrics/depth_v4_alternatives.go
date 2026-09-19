package metrics

import (
	"fmt"
	"math"
	"slopslap.dev/structural/internal/facts"
	"sort"
)

const maxDepthAlternatives = 32

func composeAlternatives(families [][]facts.ObligationSet, obligations map[string]facts.Obligation) ([]facts.ObligationSet, bool, error) {
	if len(families) == 0 {
		return []facts.ObligationSet{{}}, false, nil
	}
	ordered := make([][]facts.ObligationSet, 0, len(families))
	for familyIndex, family := range families {
		if len(family) == 0 {
			return nil, false, fmt.Errorf("family %d has no alternatives", familyIndex)
		}
		hiddenFamily := make([]facts.ObligationSet, 0, len(family))
		for _, set := range family {
			hidden := facts.ObligationSet{}
			for _, id := range set {
				if id == "" {
					return nil, false, fmt.Errorf("family %d has empty obligation reference", familyIndex)
				}
				item, ok := obligations[id]
				if !ok {
					return nil, false, fmt.Errorf("unknown obligation reference %s", id)
				}
				if item.Category != facts.ObligationOutcome {
					hidden = append(hidden, id)
				}
			}
			hiddenFamily = append(hiddenFamily, hidden)
		}
		sets := canonicalSets(hiddenFamily)
		if len(sets) > maxDepthAlternatives {
			return nil, true, nil
		}
		ordered = append(ordered, sets)
	}
	sort.Slice(ordered, func(i, j int) bool { return familyKey(ordered[i]) < familyKey(ordered[j]) })
	composed := []facts.ObligationSet{{}}
	for _, sets := range ordered {
		next := make([]facts.ObligationSet, 0, len(composed)*len(sets))
		for _, left := range composed {
			for _, right := range sets {
				next = append(next, canonicalSet(append(append(facts.ObligationSet(nil), left...), right...)))
			}
		}
		next = canonicalSets(next)
		if len(next) > maxDepthAlternatives {
			return nil, true, nil
		}
		composed = next
	}
	return composed, false, nil
}

func minimumHiddenWeight(alternatives []facts.ObligationSet, obligations map[string]facts.Obligation) uint64 {
	minimum := uint64(math.MaxUint64)
	for _, alternative := range alternatives {
		total := uint64(0)
		for _, id := range alternative {
			total += obligationWeight(obligations[id].Category)
		}
		if total < minimum {
			minimum = total
		}
	}
	if minimum == math.MaxUint64 {
		return 0
	}
	return minimum
}
func obligationWeight(category facts.ObligationCategory) uint64 {
	switch category {
	case facts.ObligationValidation:
		return 1
	case facts.ObligationResource, facts.ObligationState, facts.ObligationCoordination, facts.ObligationTransform:
		return 2
	}
	return 0
}
func canonicalSet(items facts.ObligationSet) facts.ObligationSet {
	result := append(facts.ObligationSet(nil), items...)
	sort.Strings(result)
	out := result[:0]
	for _, id := range result {
		if id != "" && (len(out) == 0 || out[len(out)-1] != id) {
			out = append(out, id)
		}
	}
	return out
}
func canonicalSets(items []facts.ObligationSet) []facts.ObligationSet {
	result := make([]facts.ObligationSet, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = canonicalSet(item)
		key := joinIDs(item)
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return joinIDs(result[i]) < joinIDs(result[j]) })
	return result
}
func joinIDs(items facts.ObligationSet) string {
	result := ""
	for _, id := range items {
		result += fmt.Sprintf("%d:%s;", len(id), id)
	}
	return result
}
func familyKey(items []facts.ObligationSet) string {
	result := ""
	for _, item := range items {
		key := joinIDs(item)
		result += fmt.Sprintf("%d:%s;", len(key), key)
	}
	return result
}
