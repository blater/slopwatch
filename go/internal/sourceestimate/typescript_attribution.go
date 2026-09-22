package sourceestimate

import "sort"

// AnalyzeTypeScriptFiles returns bounded per-file projections and passive
// structural roles using the shared parser/index supplied by the caller.
func AnalyzeTypeScriptFiles(files []File) map[string]Result {
	units := make([]unit, 0, len(files))
	for index, file := range files {
		if normalizeLanguage(file.Language, file.Path) != "typescript" {
			continue
		}
		tokens, limited, valid := lex(file.Source)
		pkg := packageName(file.Language, tokens)
		item := unit{inventory: &unitInventory{}, index: index, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: valid}
		item.ops = findOperations(file, index, tokens, pkg)
		units = append(units, item)
	}
	all := make([]*operation, 0)
	for _, unit := range units {
		all = append(all, unit.ops...)
	}
	annotateTypeScriptImports(units)
	byKey := newOperationLookup()
	for _, op := range all {
		indexOperation(byKey, op)
	}
	annotateSourceSurfaceInputs(units, byKey)
	return analyzeTypeScriptUnits(units, byKey)
}

func analyzeTypeScriptUnits(units []unit, byKey *operationLookup, callbacks ...func(File, Result)) map[string]Result {
	roles := supportingTypeScriptOwners(units)
	rolesByFile := map[int]map[string]string{}
	for key, role := range roles {
		if rolesByFile[key.file] == nil {
			rolesByFile[key.file] = map[string]string{}
		}
		rolesByFile[key.file][key.owner] = role
	}
	inbound := buildGoInboundIndex(units, boundedCallGraph(units, byKey, "typescript"))
	results := make(map[string]Result)
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "typescript" {
			continue
		}
		groups := map[string][]*operation{}
		audiences := map[string]string{}
		supporting := rolesByFile[u.index]
		if supporting == nil {
			supporting = map[string]string{}
		}
		moduleGroups := typeScriptModuleGroups(u, units, byKey)
		behavior := false
		for _, op := range u.ops {
			if role := roles[attributionOwner(op)]; role != "" {
				supporting[op.owner] = role
				continue
			}
			behavior = true
			if !op.exposed && inbound.sameFileInbound[op.id] > 0 && inbound.externalInbound[op.id] == 0 {
				continue
			}
			name := op.name
			if op.owner != "" {
				name = op.owner
			} else if group := moduleGroups[op.id]; group != "" {
				name = group
			}
			audience := "local-unresolved"
			if op.exposed {
				audience = "external"
			} else if inbound.externalInbound[op.id] > 0 {
				audience = "module"
			}
			key := audience + ":" + name
			groups[key] = append(groups[key], op)
			audiences[key] = audience
		}
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var best Result
		have := false
		details := []Abstraction{}
		for _, key := range keys {
			projection := estimateRoots(u.index, u, units, byKey, groups[key])
			details = append(details, Abstraction{Grade: projection.Grade, Name: key, Audience: audiences[key], Burden: projection.Burden, Hidden: projection.Hidden, Findings: projection.Findings, RecognizedHidden: projection.RecognizedHidden(), UncertainBurden: projection.Assessment().UncertainBurden, Limitations: append([]string(nil), projection.Limitations...)})
			if !have || largerProjection(projection, best) {
				best, have = projection, true
			}
		}
		if !have {
			best = estimateRoots(u.index, u, units, byKey, nil)
		}
		best.Supporting = supporting
		best.Abstractions = details
		for _, role := range supporting {
			best.Roles = appendUniqueString(best.Roles, role)
		}
		sort.Strings(best.Roles)
		if len(supporting) > 0 && !behavior && typeScriptRoleOnlyFile(u, supporting) {
			best.RoleOnly = true
			best.Applicable = false
			best.Burden, best.Hidden = 0, 0
		}
		results[u.file.Path] = best
		if len(callbacks) > 0 && callbacks[0] != nil {
			callbacks[0](u.file, best)
		}
	}
	return results
}
