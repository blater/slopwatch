package metrics

import (
	"sort"

	"slopslap.dev/structural/internal/facts"
)

// scoreBoundedAssessment applies the adapter's bounded v4 result only after
// the precise result has been validated. The bounded result is deliberately
// scored through the same v4 scorer and therefore cannot introduce a legacy
// formula or silently repair malformed facts.
func scoreBoundedAssessment(boundary facts.BoundaryAssessment, precise DepthScore) DepthScore {
	if precise.State != facts.KnowledgePartial && precise.State != facts.KnowledgeUnavailable {
		return precise
	}
	bounded := boundary.BoundedAssessment
	if bounded == nil || bounded.BoundedAssessment != nil || bounded.Identity != boundary.Identity ||
		!sameFiles(boundary.Files, bounded.Files) ||
		(bounded.State != facts.KnowledgeMeasured && bounded.State != facts.KnowledgeNotApplicable) {
		return precise
	}
	result, err := ScoreBoundary(*bounded)
	if err != nil || (result.State != facts.KnowledgeMeasured && result.State != facts.KnowledgeNotApplicable) {
		return precise
	}
	result.Estimated = true
	result.PreciseReasons = append([]facts.Reason(nil), precise.Reasons...)
	// The bounded inventory is intentionally not a proof that the complete
	// source inventory has been understood. It must not authorize automated
	// fixes that rely on a stable complete-inventory fingerprint.
	result.InventoryFingerprint = ""
	return result
}

func sameFiles(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
