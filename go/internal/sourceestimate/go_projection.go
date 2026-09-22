package sourceestimate

import "sort"

func estimateGoAttribution(units []unit, byKey *operationLookup, goMethods goMethodIndex, graph map[string][]*operation, callbacks ...func(File, Result)) map[string]Result {
	projections := goMethodProjections(units, byKey, goMethods)
	inbound := buildGoInboundIndex(units, graph)
	results := make(map[string]Result)
	for index, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) == "go" {
			result := projectGoUnit(index, unit, units, byKey, goMethods, projections, inbound)
			results[unit.file.Path] = result
			if len(callbacks) > 0 && callbacks[0] != nil {
				callbacks[0](unit.file, result)
			}
		}
	}
	return results
}

func goMethodProjections(units []unit, byKey *operationLookup, goMethods goMethodIndex) map[string]Result {
	result := make(map[string]Result, len(goMethods.exported))
	keys := make([]string, 0, len(goMethods.exported))
	for key := range goMethods.exported {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		methods := goMethods.exported[key]
		if len(methods) == 0 || methods[0].file < 0 || methods[0].file >= len(units) {
			continue
		}
		method := methods[0]
		result[key] = estimateRoots(method.file, units[method.file], units, byKey, methods)
	}
	return result
}

func projectGoUnit(index int, unit unit, units []unit, byKey *operationLookup, methods goMethodIndex, projections map[string]Result, inbound goInboundData) Result {
	best, haveBest, abstractions := goExternalProjections(index, unit, units, byKey, methods, projections)
	private, audiences := privateGoRoots(unit, inbound, methods)
	for _, op := range private {
		projection := estimateRoots(index, unit, units, byKey, []*operation{op})
		name := op.name
		if op.owner != "" {
			name = op.owner + "." + op.name
		}
		audience := "local-unresolved"
		if audiences[op.id] == "sibling" {
			audience = "package"
		}
		abstractions = append(abstractions, projectionAbstraction(projection, name, audience))
		best, haveBest = chooseProjection(projection, best, haveBest)
	}
	if !haveBest {
		best = estimateRoots(index, unit, units, byKey, nil)
	}
	addGoSupportingRoles(&best, unit, methods)
	best.Abstractions = abstractions
	if goRoleOnlyResult(best, unit, methods) {
		best.Applicable, best.Burden, best.Hidden = false, 0, 0
		best.RoleOnly, best.Abstractions = true, nil
	}
	return best
}

func goExternalProjections(index int, unit unit, units []unit, byKey *operationLookup, methods goMethodIndex, projections map[string]Result) (Result, bool, []Abstraction) {
	best, haveBest := Result{}, false
	abstractions := make([]Abstraction, 0, 3)
	free, owners := goExternalRoots(unit, methods)
	for _, op := range free {
		projection := estimateRoots(index, unit, units, byKey, []*operation{op})
		abstractions = append(abstractions, projectionAbstraction(projection, op.name, "external"))
		best, haveBest = chooseProjection(projection, best, haveBest)
	}
	ownerNames := sortedOwners(owners)
	for _, owner := range ownerNames {
		projection, ok := projections[goMethodGroupKeyFor(unit, owner)]
		if !ok {
			continue
		}
		abstractions = append(abstractions, projectionAbstraction(projection, owner, "external"))
		best, haveBest = chooseProjection(projection, best, haveBest)
	}
	return best, haveBest, abstractions
}

func goExternalRoots(unit unit, methods goMethodIndex) ([]*operation, map[string]bool) {
	free := make([]*operation, 0)
	owners := map[string]bool{}
	for _, op := range unit.ops {
		if op.owner == "" {
			if op.exposed {
				free = append(free, op)
			}
			continue
		}
		if goSupportingRole(methods, op) == "" {
			owners[op.owner] = true
		}
	}
	return free, owners
}

func sortedOwners(owners map[string]bool) []string {
	result := make([]string, 0, len(owners))
	for owner := range owners {
		result = append(result, owner)
	}
	sort.Strings(result)
	return result
}

func projectionAbstraction(projection Result, name, audience string) Abstraction {
	return Abstraction{
		Grade: projection.Grade, Name: name, Audience: audience,
		Burden: projection.Burden, Hidden: projection.Hidden,
		Findings: projection.Findings, RecognizedHidden: projection.RecognizedHidden(),
		UncertainBurden: projection.Assessment().UncertainBurden,
		Limitations:     append([]string(nil), projection.Limitations...),
	}
}

func chooseProjection(candidate, current Result, haveCurrent bool) (Result, bool) {
	if !haveCurrent || largerProjection(candidate, current) {
		return candidate, true
	}
	return current, true
}

func addGoSupportingRoles(result *Result, unit unit, methods goMethodIndex) {
	for _, op := range unit.ops {
		if role := goSupportingRole(methods, op); role != "" {
			result.Roles = appendUniqueString(result.Roles, role)
			if result.Supporting == nil {
				result.Supporting = map[string]string{}
			}
			result.Supporting[op.owner] = role
		}
	}
}

func goRoleOnlyResult(result Result, unit unit, methods goMethodIndex) bool {
	return len(result.Roles) > 0 && !haveBehaviorRoots(unit, methods) && goRoleOnlyFile(unit)
}

func haveBehaviorRoots(unit unit, goMethods goMethodIndex) bool {
	for _, op := range unit.ops {
		if goSupportingRole(goMethods, op) == "" {
			return true
		}
	}
	return false
}

type goInboundData struct {
	sameFileInbound map[string]int
	externalInbound map[string]int
	byID            map[string]*operation
}

func buildGoInboundIndex(units []unit, graph map[string][]*operation) goInboundData {
	inbound := goInboundData{sameFileInbound: map[string]int{}, externalInbound: map[string]int{}, byID: map[string]*operation{}}
	for _, unit := range units {
		for _, op := range unit.ops {
			inbound.byID[op.id] = op
		}
	}
	for callerID, callees := range graph {
		caller := inbound.byID[callerID]
		if caller == nil {
			continue
		}
		for _, callee := range callees {
			if caller.file == callee.file {
				inbound.sameFileInbound[callee.id]++
			} else {
				inbound.externalInbound[callee.id]++
			}
		}
	}
	return inbound
}

func privateGoRoots(unit unit, inbound goInboundData, goMethods goMethodIndex) ([]*operation, map[string]string) {
	roots, audiences := make([]*operation, 0), map[string]string{}
	for _, op := range unit.ops {
		if privateGoCandidate(op, unit, inbound, goMethods) {
			roots = append(roots, op)
			audiences[op.id] = privateGoAudience(op, inbound)
		}
	}
	return roots, audiences
}

func privateGoCandidate(op *operation, unit unit, inbound goInboundData, methods goMethodIndex) bool {
	if op.exposed || goSupportingRole(methods, op) != "" {
		return false
	}
	if op.owner != "" && len(methods.exported[goMethodGroupKey(op)]) != 0 && !nontrivial(op.body) {
		return false
	}
	return inbound.sameFileInbound[op.id] == 0 || inbound.externalInbound[op.id] != 0
}

func privateGoAudience(op *operation, inbound goInboundData) string {
	if inbound.externalInbound[op.id] != 0 {
		return "sibling"
	}
	return "local"
}

func largerProjection(candidate, current Result) bool {
	if candidate.PublishedPenalty() != current.PublishedPenalty() {
		return candidate.PublishedPenalty() > current.PublishedPenalty()
	}
	candidateLeft := candidate.Burden * (current.Burden + 2*current.Hidden)
	currentLeft := current.Burden * (candidate.Burden + 2*candidate.Hidden)
	if candidateLeft != currentLeft {
		return candidateLeft > currentLeft
	}
	if candidate.Burden != current.Burden {
		return candidate.Burden > current.Burden
	}
	if candidate.Hidden != current.Hidden {
		return candidate.Hidden > current.Hidden
	}
	return len(candidate.Dependencies) > len(current.Dependencies)
}
