package native

func partitionCombinedUnitInputs(combined scoreInputs, units []plannedCacheUnit) map[string]scoreInputs {
	result, owners := partitionTargets(units)
	for path, observations := range combined.observations {
		if owner := owners[path]; owner != "" {
			inputs := result[owner]
			inputs.observations[path] = observations
			result[owner] = inputs
		}
	}
	for path, coverage := range combined.coverage {
		if owner := owners[path]; owner != "" {
			inputs := result[owner]
			inputs.coverage[path] = coverage
			result[owner] = inputs
		}
	}
	for path, language := range combined.languages {
		if owner := owners[path]; owner != "" {
			inputs := result[owner]
			inputs.languages[path] = language
			result[owner] = inputs
		}
	}
	for id, boundary := range combined.depth {
		for owner, files := range depthFilesByOwner(boundary.Files, owners) {
			inputs := result[owner]
			boundary.Files = files
			inputs.depth[id] = boundary
			for _, path := range files {
				inputs.depthByPath[path] = appendUnique(inputs.depthByPath[path], id)
			}
			result[owner] = inputs
		}
	}
	for path, state := range combined.depthStates {
		if owner := owners[path]; owner != "" {
			inputs := result[owner]
			inputs.depthStates[path] = mergeDepthState(inputs.depthStates[path], state)
			result[owner] = inputs
		}
	}
	partitionDiagnostics(result, owners, combined.diagnostics, units)
	partitionPlans(result, combined.plans, units)
	return result
}

func depthFilesByOwner(files []string, owners map[string]string) map[string][]string {
	result := map[string][]string{}
	for _, path := range files {
		if owner := owners[path]; owner != "" {
			result[owner] = appendUnique(result[owner], path)
		}
	}
	return result
}

func partitionTargets(units []plannedCacheUnit) (map[string]scoreInputs, map[string]string) {
	result := make(map[string]scoreInputs, len(units))
	owners := make(map[string]string)
	for _, unit := range units {
		result[unit.plan.ID] = newScoreInputs()
		for _, path := range unit.owned {
			owners[path] = unit.plan.ID
		}
	}
	return result, owners
}

func partitionDiagnostics(result map[string]scoreInputs, owners map[string]string, diagnostics []map[string]any, units []plannedCacheUnit) {
	for _, diagnostic := range diagnostics {
		path, hasPath := metadataPath(diagnostic)
		if hasPath {
			appendDiagnostic(result, owners[path], diagnostic)
			continue
		}
		for _, unit := range units {
			appendDiagnostic(result, unit.plan.ID, diagnostic)
		}
	}
}

func appendDiagnostic(result map[string]scoreInputs, unitID string, diagnostic map[string]any) {
	if unitID == "" {
		return
	}
	inputs := result[unitID]
	inputs.diagnostics = append(inputs.diagnostics, normalizeMetadata(diagnostic, unitID))
	result[unitID] = inputs
}

func partitionPlans(result map[string]scoreInputs, plans []map[string]any, units []plannedCacheUnit) {
	for _, unit := range units {
		inputs := result[unit.plan.ID]
		for _, plan := range plans {
			normalized := normalizeMetadata(plan, unit.plan.ID)
			normalized["discovered_source_count"] = len(unit.owned)
			normalized["parsed_source_count"] = len(unit.owned)
			inputs.plans = append(inputs.plans, normalized)
		}
		result[unit.plan.ID] = inputs
	}
}

func metadataPath(metadata map[string]any) (string, bool) {
	value := metadata["path"]
	switch value := value.(type) {
	case string:
		return value, value != ""
	case *string:
		return dereferenceString(value), value != nil && *value != ""
	default:
		return "", false
	}
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizeMetadata(metadata map[string]any, unitID string) map[string]any {
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if key != "invocation_id" {
			result[key] = value
		}
	}
	result["unit_id"] = unitID
	return result
}
