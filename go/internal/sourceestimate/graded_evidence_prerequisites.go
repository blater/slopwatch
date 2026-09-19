package sourceestimate

// Preconditions on shared protocol state remain obligations of the caller
// choosing the next primitive stage. Independent data validation and returned
// transformations are retained even when their implementation is delegated.
func publicProtocolPrerequisites(op *operation, units []unit) map[string]bool {
	if op.owner == "" || op.file < 0 || op.file >= len(units) {
		return nil
	}
	u := units[op.file]
	publicType := false
	for i := 0; i+2 < len(u.tokens); i++ {
		if (u.tokens[i].text == "class" || u.tokens[i].text == "struct") && u.tokens[i+1].text == op.owner {
			for j := maxInt(0, i-3); j < i; j++ {
				publicType = publicType || u.tokens[j].text == "public" || u.tokens[j].text == "pub" || u.tokens[j].text == "export"
			}
		}
		if u.file.Language == "go" && u.tokens[i].text == "type" && u.tokens[i+1].text == op.owner && !unexportedGoName(op.owner) {
			publicType = true
		}
	}
	if !publicType {
		return nil
	}
	result := map[string]bool{}
	fields := callerDeclaredFields(u, op.owner)
	writers := map[string]map[string]bool{}
	readers := map[string]map[string]bool{}
	for _, candidate := range u.ops {
		if candidate.owner != op.owner || !candidate.exposed || gradedSurfaceConstructor(candidate) {
			continue
		}
		initialized := map[string]bool{}
		params := map[string]bool{}
		for _, p := range candidate.paramNames {
			params[p] = true
		}
		for i, tok := range candidate.body {
			if _, exists := fields[tok.text]; !exists || !gradeOwnedFieldReference(candidate, u, candidate.body, i) {
				continue
			}
			member := i > 0 && candidate.body[i-1].text == "."
			if params[tok.text] && !member {
				continue
			}
			next := ""
			if i+1 < len(candidate.body) {
				next = candidate.body[i+1].text
			}
			write := next == "=" || next == "+=" || next == "-=" || next == "++" || next == "--"
			read := next != "="
			if read && !initialized[tok.text] {
				if readers[tok.text] == nil {
					readers[tok.text] = map[string]bool{}
				}
				readers[tok.text][candidate.id] = true
			}
			if write {
				if writers[tok.text] == nil {
					writers[tok.text] = map[string]bool{}
				}
				writers[tok.text][candidate.id] = true
				initialized[tok.text] = true
			}
		}
	}
	for field, operations := range writers {
		for writer := range operations {
			for reader := range readers[field] {
				if reader != writer {
					result[field] = true
				}
			}
		}
	}
	return result
}
