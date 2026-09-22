package sourceestimate

func rustVisibilityMaps(units []unit) (map[string]bool, map[string]bool, map[string]bool) {
	typeVisibility := map[string]bool{}
	traitVisibility := map[string]bool{}
	knownTraits := map[string]bool{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "rust" {
			continue
		}
		for _, declaration := range rustNamedRanges(u.tokens, "struct") {
			key := u.pkg + "#" + declaration.name
			typeVisibility[key] = typeVisibility[key] || declaration.public
		}
		for _, declaration := range rustNamedRanges(u.tokens, "enum") {
			key := u.pkg + "#" + declaration.name
			typeVisibility[key] = typeVisibility[key] || declaration.public
		}
		for _, declaration := range rustNamedRanges(u.tokens, "trait") {
			key := u.pkg + "#" + declaration.name
			knownTraits[key] = true
			traitVisibility[key] = traitVisibility[key] || declaration.public
		}
	}

	return typeVisibility, traitVisibility, knownTraits
}

func rustTraitEscapes(units []unit) map[string]bool {
	traitNames := map[string]map[string]bool{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "rust" {
			continue
		}
		names := traitNames[u.pkg]
		if names == nil {
			names = map[string]bool{}
			traitNames[u.pkg] = names
		}
		for _, declaration := range rustNamedRanges(u.tokens, "trait") {
			names[declaration.name] = true
		}
		for _, function := range rustUnitFunctions(u) {
			if function.impl != nil && function.impl.trait != "" {
				names[function.impl.trait] = true
			}
		}
	}
	matchers := make(map[string]*rustTraitMatcher, len(traitNames))
	for pkg, names := range traitNames {
		matchers[pkg] = newRustTraitMatcher(names)
	}
	traitEscapes := map[string]bool{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "rust" {
			continue
		}
		for _, op := range u.ops {
			if !op.exposed || op.returnType == "" {
				continue
			}
			// Imported traits have no local declaration; the return signature
			// still provides bounded evidence for a named impl trait.
			matchers[u.pkg].match(op.returnType, func(trait string) {
				traitEscapes[u.pkg+"#"+trait] = true
			})
		}
	}
	return traitEscapes
}

func rustInboundCalls(all []*operation) (map[string]int, map[string]int) {
	callIndex := rustCallIndex(all)
	inbound := make(map[string]int, len(all))
	crossFileInbound := make(map[string]int, len(all))
	for _, caller := range all {
		if caller.testOnly {
			continue
		}
		for _, call := range callsIn(caller.body) {
			for _, callee := range rustResolveCall(caller, call, callIndex) {
				inbound[callee.id]++
				if caller.file != callee.file {
					crossFileInbound[callee.id]++
				}
			}
		}
	}

	return inbound, crossFileInbound
}
