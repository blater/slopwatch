package metrics

import (
	"slopslap.dev/structural/internal/depth"
	"slopslap.dev/structural/internal/facts"
	"sort"
)

const (
	DepthV4Definition     = "responsibility-burden-v4"
	DepthV4PolicyRevision = "r36"
)

// DepthScore is the nullable result for one stable boundary. HKnown separates
// a supported partial H from a complete hidden-responsibility total.
type DepthScore struct {
	Boundary             facts.BoundaryIdentity `json:"boundary"`
	State                facts.KnowledgeState   `json:"state"`
	Burden               facts.Burden           `json:"burden"`
	B8                   uint64                 `json:"B8"`
	H                    uint64                 `json:"H"`
	HKnown               bool                   `json:"h_known"`
	Estimated            bool                   `json:"estimated,omitempty"`
	Shallow              *int                   `json:"shallow"`
	SelectedRoutes       []string               `json:"selected_routes"`
	Alternatives         []facts.ObligationSet  `json:"alternatives"`
	FamilyHidden         map[string]uint64      `json:"family_hidden,omitempty"`
	Obligations          []facts.Obligation     `json:"obligations,omitempty"`
	Reasons              []facts.Reason         `json:"reasons"`
	PreciseReasons       []facts.Reason         `json:"precise_reasons,omitempty"`
	Evidence             []facts.Evidence       `json:"evidence"`
	Dependencies         []string               `json:"dependencies,omitempty"`
	InventoryFingerprint string                 `json:"inventory_fingerprint,omitempty"`
}

// ScoreBoundary validates and scores one normalized boundary. Invalid facts
// return an error; partial facts preserve supported burden/evidence and withhold
// the numeric score.
func ScoreBoundary(boundary facts.BoundaryAssessment) (DepthScore, error) {
	boundary = depth.ApplyBoundaryRoles(boundary)
	result := DepthScore{Boundary: boundary.Identity, State: boundary.State, Burden: boundary.Burden, InventoryFingerprint: inventoryFingerprint(boundary)}
	result.Dependencies = uniqueStrings(boundary.Dependencies)
	if err := validateBoundary(boundary); err != nil {
		return result, err
	}
	result.Reasons, result.Evidence = canonicalReasons(boundary.Reasons), canonicalEvidence(boundary.Evidence)
	if passiveDataRole(boundary.Evidence) {
		// This is a role exemption, not an assertion of exceptional functional
		// depth. Keep the real interface inventory and H=0; publish no penalty.
		burden, routes, err := routeBurden(boundary)
		if err != nil {
			return result, err
		}
		result.Burden, result.SelectedRoutes = burden, routes
		result.B8, err = burdenB8(burden)
		if err != nil {
			return result, err
		}
		zero := 0
		result.State, result.Shallow, result.HKnown = facts.KnowledgeMeasured, &zero, true
		// A role proof does not upgrade incomplete semantic/inventory evidence
		// into authorization for automated rewrites.
		if essentialUnknown(boundary) {
			result.InventoryFingerprint = ""
		}
		return result, nil
	}
	if boundary.State == facts.KnowledgeNotApplicable {
		return result, nil
	}
	burden, routes, err := routeBurden(boundary)
	if err != nil {
		return result, err
	}
	result.Burden, result.SelectedRoutes = burden, routes
	result.B8, err = burdenB8(burden)
	if err != nil {
		return result, err
	}
	obligations, err := obligationIndex(boundary.Obligations)
	if err != nil {
		return result, err
	}
	result.Obligations = canonicalObligations(obligations)
	if missingFamilyBehavior(boundary.FamilyAlternatives) && (boundary.State != facts.KnowledgeMeasured || essentialUnknown(boundary)) {
		if result.State == facts.KnowledgeMeasured {
			result.State = facts.KnowledgePartial
		}
		return result, nil
	}
	alternatives, limited, err := composeAlternatives(boundary.FamilyAlternatives, obligations)
	if err != nil {
		return result, err
	}
	if limited {
		result.State = facts.KnowledgePartial
		result.Alternatives = nil
		result.Reasons = canonicalReasons(append(result.Reasons, facts.Reason{Code: "alternative_limit", Dimension: "behavior", Message: "obligation alternatives exceeded 32"}))
		return result, nil
	}
	result.Alternatives, result.H = alternatives, minimumHiddenWeight(alternatives, obligations)
	// Preserve family attribution for diagnostics and source acceptance. These
	// totals must not be summed: responsibilities can be shared across families.
	if len(boundary.FamilyAlternativeIDs) == len(boundary.FamilyAlternatives) {
		result.FamilyHidden = make(map[string]uint64, len(boundary.FamilyAlternativeIDs))
		for index, id := range boundary.FamilyAlternativeIDs {
			result.FamilyHidden[id] = minimumHiddenWeight(boundary.FamilyAlternatives[index], obligations)
		}
	}
	result.HKnown = boundary.State == facts.KnowledgeMeasured && !essentialUnknown(boundary)
	if boundary.State != facts.KnowledgeMeasured || !result.HKnown {
		if boundary.State == facts.KnowledgeMeasured {
			result.State = facts.KnowledgePartial
		}
		return result, nil
	}
	result.Shallow, err = shallowInteger(result.B8, result.H)
	return result, err
}

func passiveDataRole(evidence []facts.Evidence) bool {
	for _, item := range evidence {
		if (item.Kind == "passive-result-carrier-v1" || item.Kind == "passive-value-object-v1" || item.Kind == "passive-enum-v1") && item.Status == "proven" {
			return true
		}
	}
	return false
}

func missingFamilyBehavior(families [][]facts.ObligationSet) bool {
	for _, family := range families {
		if len(family) == 0 {
			return true
		}
	}
	return false
}

// MeasureDepth scores every optional v4 boundary in canonical identity order.
func MeasureDepth(program *facts.Program) []DepthScore {
	if program == nil || program.Depth == nil {
		return nil
	}
	boundaries := program.Depth.Boundaries
	if len(program.Depth.Flows) != 0 {
		boundaries = depth.AssessFlowBoundaries(program.Depth)
	}
	items := make([]DepthScore, 0, len(boundaries))
	for _, boundary := range boundaries {
		item, err := ScoreBoundary(boundary)
		if err == nil {
			item = scoreBoundedAssessment(boundary, item)
		} else {
			item.State, item.HKnown, item.Shallow = facts.KnowledgeUnavailable, false, nil
			item.Reasons = append(item.Reasons, facts.Reason{Code: "malformed_facts", Dimension: "boundary", Message: err.Error()})
			item.Reasons = canonicalReasons(item.Reasons)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Boundary.String() < items[j].Boundary.String() })
	return items
}

func essentialUnknown(boundary facts.BoundaryAssessment) bool {
	for _, dimension := range []string{"inventory", "burden", "behavior", "alias_effects"} {
		item, ok := boundary.Knowledge[dimension]
		if !ok || item.State != facts.KnowledgeMeasured {
			return true
		}
	}
	for dimension, item := range boundary.Knowledge {
		if item.Essential && (item.State == facts.KnowledgePartial || item.State == facts.KnowledgeUnavailable) && dimension != "evidence" && dimension != "outcome" && dimension != "relevance" {
			return true
		}
	}
	return false
}

func deriveInventoryBurden(boundary facts.BoundaryAssessment) facts.Burden {
	result := boundary.Burden
	if boundary.RouteFamilies != nil {
		result.O = int64(len(boundary.RouteFamilies))
	}
	if boundary.Concepts != nil {
		seen := map[string]bool{}
		for _, concept := range boundary.Concepts {
			seen[concept.ID] = true
		}
		result.T = int64(len(seen))
	}
	if boundary.LeakRoots != nil {
		result.L = int64(len(uniqueStrings(boundary.LeakRoots)))
	}
	return result
}

func uniqueStrings(items []string) []string {
	result := append([]string(nil), items...)
	sort.Strings(result)
	out := result[:0]
	for _, item := range result {
		if item != "" && (len(out) == 0 || out[len(out)-1] != item) {
			out = append(out, item)
		}
	}
	return out
}
func contains(items []string, value string) bool {
	index := sort.SearchStrings(items, value)
	return index < len(items) && items[index] == value
}
