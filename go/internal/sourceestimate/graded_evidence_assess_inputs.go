package sourceestimate

import "strings"

func newGradedEvidenceAssessment(u unit, roots []*operation, units []unit, byKey map[string][]*operation, evidence []evidenceItem) *gradedEvidenceAssessment {
	p := u.gradingProfile()
	a := &gradedEvidenceAssessment{unit: u, roots: roots, profile: p, evidence: &GradedEvidence{DenominatorReference: p.DenominatorReference, ResponsibilityMultiplier: p.ResponsibilityMultiplier, Surface: gradedCallerSurface(u, roots, units), Responsibilities: map[string]float64{}}, rootOwners: map[string]bool{}, protocol: map[string]map[string]bool{}, dispatches: map[string]string{}}
	for _, op := range roots {
		a.rootOwners[op.owner] = true
	}
	for owner := range a.rootOwners {
		if _, observed := u.callerObligations[owner]; observed {
			continue
		}
		for _, field := range callerDeclaredFields(u, owner) {
			if field.packageVisible && field.mutable {
				a.evidence.MaterialLimitations = append(a.evidence.MaterialLimitations, "unobserved_package_caller_obligations:"+owner)
				break
			}
		}
	}
	for _, item := range evidence {
		if item.origin == nil || a.rootOwners[item.origin.owner] {
			continue
		}
		key := itoa(item.origin.file) + "#" + item.origin.owner
		if _, exists := a.protocol[key]; !exists {
			a.protocol[key] = publicProtocolPrerequisites(item.origin, units)
		}
	}
	for _, root := range roots {
		if c, ok := gradeTransparentCall(root); ok {
			if candidates := resolveCall(root, c, units, byKey); len(candidates) == 1 {
				callee := candidates[0]
				if len(a.protocol[itoa(callee.file)+"#"+callee.owner]) > 0 {
					a.transparent = appendTransparent(a.transparent, callee.id)
				}
			}
		}
	}
	return a
}
func appendTransparent(values map[string]bool, id string) map[string]bool {
	if values == nil {
		values = map[string]bool{}
	}
	values[id] = true
	return values
}
func gradedEvidenceLimits(evidence []evidenceItem, rootOwners map[string]bool, protocol map[string]map[string]bool, limitations []string) (map[string]bool, bool, map[string]bool) {
	excludedLimits, storageSnapshots := map[string]bool{}, map[string]bool{}
	ownedUnknown := false
	needed := neededCallLimitations(limitations)
	seen := map[*operation]bool{}
	for _, item := range evidence {
		excluded := item.origin != nil && !rootOwners[item.origin.owner] && len(protocol[itoa(item.origin.file)+"#"+item.origin.owner]) > 0
		if excluded {
			if !seen[item.origin] && len(needed) > 0 {
				seen[item.origin] = true
				matchCallLimitations(item.origin.body, needed, excludedLimits)
			}
		} else if strings.HasPrefix(item.category, "unknown_") {
			ownedUnknown = true
		}
		if item.category == "storage_snapshot" && item.origin != nil {
			storageSnapshots[item.origin.id] = storageSnapshots[item.origin.id] || item.storageComputed
		}
	}
	return excludedLimits, ownedUnknown, storageSnapshots
}
