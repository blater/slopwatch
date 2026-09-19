package sourceestimate

// Build once, then share the bounded inbound representation index with every
// owner projection. Caller effects never become hidden duties of the callee.
func gradedCallerPrepare(units []unit) (map[string][]*operation, map[string][]gradedCallerType) {
	shadows := map[string]map[string]bool{}
	for _, u := range units {
		key := normalizeLanguage(u.file.Language, u.file.Path) + "#" + u.pkg
		if shadows[key] == nil {
			shadows[key] = map[string]bool{}
		}
		for _, name := range []string{"len", "panic", "Vec"} {
			if gradedPackageShadow(u, name) {
				shadows[key][name] = true
			}
		}
	}
	for i := range units {
		units[i].shadowedBuiltins = shadows[normalizeLanguage(units[i].file.Language, units[i].file.Path)+"#"+units[i].pkg]
	}
	byKey := map[string][]*operation{}
	for _, u := range units {
		for _, op := range u.ops {
			indexOperation(byKey, op)
		}
	}
	types := map[string][]gradedCallerType{}
	packageTypes := map[string]map[string]bool{}
	for index, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "java" {
			continue
		}
		for i, t := range u.tokens {
			if t.text != "class" || i+1 >= len(u.tokens) {
				continue
			}
			owner := u.tokens[i+1].text
			decl := gradedFindSurfaceOwner(u.tokens, "java", owner)
			if decl == nil {
				continue
			}
			parent := ""
			for j := i + 2; j < decl.open; j++ {
				if u.tokens[j].text == "extends" && j+1 < decl.open {
					parent = u.tokens[j+1].text
				}
			}
			if packageTypes[u.pkg] == nil {
				packageTypes[u.pkg] = map[string]bool{}
			}
			packageTypes[u.pkg][owner] = true
			key := u.pkg + "#" + owner
			types[key] = append(types[key], gradedCallerType{index, owner, parent, decl.fields})
		}
	}
	for index := range units {
		units[index].packageTypes = packageTypes[units[index].pkg]
	}
	return byKey, types
}
