package sourceestimate

// annotateRustAttribution applies Rust visibility and receiver ownership to a
// parsed operation inventory. It is safe to call before the generic estimate
// chooses roots. Public trait implementations remain roots even when their
// representation type is private; called private helpers do not become roots.
func annotateRustAttribution(units []unit) map[string]rustFunction {
	typeVisibility, traitVisibility, knownTraits := rustVisibilityMaps(units)

	for index := range units {
		if normalizeLanguage(units[index].file.Language, units[index].file.Path) == "rust" {
			units[index].ops = rustOperations(units[index].file, units[index].index, units[index].tokens, units[index].pkg, rustFunctions(units[index].tokens))
		}
	}
	// An imported trait is not necessarily private merely because its
	// declaration is outside this bounded source set. A public operation that
	// returns the trait is concrete escape evidence; retain such an impl as an
	// external route while treating private visitor callbacks as helpers.
	traitEscapes := rustTraitEscapes(units)

	all := make([]*operation, 0)
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) == "rust" {
			all = append(all, u.ops...)
		}
	}
	inbound, crossFileInbound := rustInboundCalls(all)

	visibility := rustVisibility{typeVisibility, traitVisibility, knownTraits, traitEscapes, inbound, crossFileInbound}
	annotations := make(map[string]rustFunction, len(all))
	for index := range units {
		u := &units[index]
		if normalizeLanguage(u.file.Language, u.file.Path) != "rust" {
			continue
		}
		functions := rustFunctions(u.tokens)
		for functionIndex, op := range u.ops {
			if functionIndex >= len(functions) {
				// A malformed or macro-generated item is never promoted by a
				// best-effort name match.
				op.exposed = false
				op.packageVisible = false
				continue
			}
			info := functions[functionIndex]
			if info.testOnly {
				op.exposed = false
				op.packageVisible = false
				continue
			}
			annotations[op.id] = visibility.annotate(*u, op, info)
		}
	}
	return annotations
}

type rustVisibility struct {
	typeVisibility, traitVisibility, knownTraits, traitEscapes map[string]bool
	inbound, crossFileInbound                                  map[string]int
}

func (v rustVisibility) annotate(u unit, op *operation, info rustFunction) rustFunction {
	if info.impl != nil {
		op.owner = info.owner
	}
	traitKey := ""
	if info.impl != nil {
		traitKey = u.pkg + "#" + info.impl.trait
	}
	info.traitPub = info.impl != nil && (v.traitVisibility[traitKey] || (!v.knownTraits[traitKey] && v.traitEscapes[traitKey]))
	info.selfPub = info.impl != nil && v.typeVisibility[u.pkg+"#"+info.impl.selfType]
	info.modulePub = rustModuleVisible(u.tokens, info.start)

	if rustExternalRoute(info) {
		op.exposed = true
		op.packageVisible = true
		return info
	}
	if v.privateContract(info, traitKey) {
		// A private trait contract cannot be called as an external route.
		// Drop is the language-defined cleanup hook; its implementation is
		// reached implicitly through the owning value's lifecycle. Keep
		// both kinds of private contract method internal. Public traits are
		// deliberately handled above, including implementations on private
		// representations returned behind the public trait.
		op.exposed = false
		op.packageVisible = false
		return info
	}

	// A private/package function is an internal root only when no
	// operation in the bounded source set calls it. This preserves
	// helper extraction and still exposes genuinely independent
	// module work.
	op.exposed = info.restricted || v.inbound[op.id] == 0 || v.crossFileInbound[op.id] > 0
	op.packageVisible = op.exposed
	return info
}

func rustExternalRoute(info rustFunction) bool {
	if !info.modulePub {
		return false
	}
	if info.impl == nil {
		return info.pubFn
	}
	// Public trait methods remain callable even on a private representation.
	return info.traitPub || info.pubFn && info.selfPub
}

func (v rustVisibility) privateContract(info rustFunction, traitKey string) bool {
	if info.impl == nil || info.impl.trait == "" {
		return false
	}
	if info.impl.trait == "Drop" {
		return true
	}
	if v.knownTraits[traitKey] {
		return !info.traitPub
	}
	return !info.selfPub && !info.traitPub
}
