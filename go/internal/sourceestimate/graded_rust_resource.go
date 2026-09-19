package sourceestimate

type gradedRustFlow struct {
	fields   map[string]bool
	identity string
	kind     string
	inner    string
}

// Exact imported pointer operations expose observable destruction/relocation.
// Their operands must be connected to declared self storage. Drop is an implicit
// language entry point; an identically named ordinary method carries no credit.
func gradedRustResourceEffects(u unit, roots []*operation) (resource, relocation bool) {
	if normalizeLanguage(u.file.Language, u.file.Path) != "rust" {
		return
	}
	owners := map[string]bool{}
	for _, op := range roots {
		owners[op.owner] = true
	}
	for owner := range owners {
		if owner == "" {
			continue
		}
		fields := gradedRustPointerFields(u, owner)
		for _, op := range roots {
			if op.owner == owner {
				_, moved := gradedRustPointerEffects(gradedRustExpandHelpers(op.body, u, owner, 0, nil), u.tokens, fields, "self")
				relocation = relocation || moved
			}
		}
		for _, fn := range rustFunctions(u.tokens) {
			if fn.owner != owner || fn.name != "drop" || fn.impl == nil || !gradedRustStandardDrop(u.tokens, *fn.impl) {
				continue
			}
			body := u.tokens[fn.bodyStart+1 : fn.bodyEnd]
			destroyed, moved := gradedRustPointerEffects(gradedRustExpandHelpers(body, u, owner, 0, nil), u.tokens, fields, "self")
			resource = resource || destroyed
			relocation = relocation || moved
		}
	}
	return
}
