package sourceestimate

func gradedCallerArgumentRefs(body []token, bindings map[string]string) map[string]bool {
	return gradedCallerArgumentRefsIndexed(body, bindings, indexGradedCallerBody(body))
}

func gradedCallerArgumentRefsIndexed(body []token, bindings map[string]string, index gradedCallerBodyIndex) map[string]bool {
	refs := map[string]bool{}
	for _, c := range callsIn(body) {
		if !gradedCallerCallIsBound(c, bindings, index.callRoot(body, c)) {
			continue
		}
		gradedCallerAddArgumentDependencies(refs, c)
	}
	return refs
}

func gradedCallerCallIsBound(c call, bindings map[string]string, root string) bool {
	// Preserve all prefix spellings, including dotted keys and empty-valued
	// bindings: eligibility depends on membership, not the resolved type.
	for i := 0; i < len(c.name); i++ {
		if c.name[i] == '.' {
			if _, ok := bindings[c.name[:i]]; ok {
				return true
			}
		}
	}
	_, bound := bindings[root]
	return bound
}

func gradedCallerAddArgumentDependencies(refs map[string]bool, c call) {
	for _, arg := range c.actuals {
		for dep := range gradedCallerDependencies(arg) {
			refs[dep] = true
		}
	}
}

// Dependency identities retain both the field and receiver write epochs.
func gradedCallerDependencyVersions(expression []token, epochs map[string]int) map[string]bool {
	versioned := map[string]bool{}
	for dep := range gradedCallerDependencies(expression) {
		versioned[dep+"@"+itoa(epochs[dep])+"/"+itoa(epochs[gradedCallerDependencyRoot(dep)])] = true
	}
	return versioned
}

func gradedCallerDependencyRoot(dependency string) string {
	for i, character := range dependency {
		if character == '.' {
			return dependency[:i]
		}
	}
	return dependency
}
