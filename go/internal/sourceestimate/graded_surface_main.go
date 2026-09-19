package sourceestimate

import "sort"

// gradedCallerSurface counts caller choices, required inputs, and exposed
// representation obligations for the supplied owner-root group. It does not
// inspect sibling units or use operation names as evidence.
func gradedCallerSurface(u unit, roots []*operation, workspace ...[]unit) CallerSurface {
	p := u.gradingProfile()
	result := CallerSurface{}
	if len(u.tokens) == 0 {
		return result
	}
	limit := len(u.tokens)
	if limit > gradedSurfaceTokenLimit {
		limit = gradedSurfaceTokenLimit
	}
	tokens := u.tokens[:limit]

	groups := map[string][]*operation{}
	seen := map[string]bool{}
	for _, root := range roots {
		if root == nil || seen[root.id] {
			continue
		}
		seen[root.id] = true
		owner := root.owner
		if owner == "" {
			owner = gradedInferSurfaceOwner(u.tokens[:limit], normalizeLanguage(u.file.Language, u.file.Path), root.name)
			if owner == "" {
				owner = gradedGoConstructorOwner(root)
			}
		}
		groups[owner] = append(groups[owner], root)
	}
	for owner, obligation := range u.callerObligations {
		if len(roots) == 0 && len(obligation.constraints) > 0 {
			if _, ok := groups[owner]; !ok {
				groups[owner] = nil
			}
		}
	}
	owners := make([]string, 0, len(groups))
	for owner := range groups {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		group := groups[owner]
		constructorMax := 0
		allConstructors := true
		for _, op := range group {
			if gradedSurfaceConstructor(op) {
				if len(op.requiredParamNames) > constructorMax {
					constructorMax = len(op.requiredParamNames)
				}
				continue
			}
			allConstructors = false
			units := p.Operation
			ownerUnit := u
			if len(workspace) > 0 && op.file >= 0 && op.file < len(workspace[0]) {
				ownerUnit = workspace[0][op.file]
			}
			if gradedCheapOperation(ownerUnit, op) {
				units = p.Accessor
			}
			result.OperationUnits += units
			result.InputUnits += p.Input * float64(len(op.requiredParamNames))
			if len(workspace) > 0 {
				for _, name := range op.outputBuffers {
					result.RepresentationUnits += p.MutableAlias
					result.Evidence = append(result.Evidence, "caller-output-buffer:"+op.id+":"+name)
				}
			}
			result.Evidence = append(result.Evidence, "operation:"+op.id+":"+formatSurfaceUnits(units))
		}
		if constructorMax > 0 {
			// Constructor overloads are one setup choice. Count the maximum
			// required input set once, including delegating overload families.
			result.InputUnits += p.Input * float64(constructorMax)
			result.Evidence = append(result.Evidence, "constructor-inputs:"+owner+":"+formatSurfaceUnits(p.Input*float64(constructorMax)))
		}
		if owner == "" || allConstructors && len(u.callerObligations[owner].constraints) == 0 {
			continue
		}
		declaration := gradedFindSurfaceOwner(tokens, normalizeLanguage(u.file.Language, u.file.Path), owner)
		if declaration == nil {
			continue
		}
		records := gradedOwnerConstraints(u, owner, gradedConstraintRoots(u, owner, group))
		if obligation, ok := u.callerObligations[owner]; ok {
			records = append(records, obligation.constraints...)
			if obligation.sequencing {
				for _, constraint := range obligation.constraints {
					if constraint.kind == "phase-admission" {
						result.RepresentationUnits += p.Sequencing
						break
					}
				}
			}
			result.Evidence = append(result.Evidence, obligation.evidence...)
		}
		charged := map[string]bool{}
		for _, record := range records {
			result.Evidence = append(result.Evidence, record.evidence(u))
			controlledFields := []string{}
			for _, name := range record.fields {
				field := declaration.fields[name]
				controlled := field.public && field.mutable
				if obligation, ok := u.callerObligations[owner]; ok {
					for _, observed := range obligation.fields {
						controlled = controlled || observed == name
					}
				}
				if controlled {
					controlledFields = append(controlledFields, name)
				}
				if !record.protected && controlled && !charged[name] {
					charged[name] = true
					if record.kind == "indexed-alias" {
						result.RepresentationUnits += p.MutableAlias
					} else {
						result.RepresentationUnits += p.CoupledField
					}
				}
			}
			result.Constraints = append(result.Constraints, record.finding(u, controlledFields))
		}

	}
	sort.Strings(result.Evidence)
	return result
}
