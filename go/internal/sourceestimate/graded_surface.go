package sourceestimate

import (
	"sort"
	"strconv"
	"unicode"
)

// CallerSurface is a bounded estimate of the ordinary work a caller still
// carries at an abstraction boundary. It is intentionally separate from the
// responsibility/depth estimate: a zero surface is not proof of a passive
// role, and these units do not alter scoring by themselves.
type CallerSurface struct {
	OperationUnits      float64
	InputUnits          float64
	RepresentationUnits float64
	Evidence            []string
	Constraints         []ConstraintEvidence
}

const gradedSurfaceTokenLimit = 20000

type gradedSurfaceField struct {
	name           string
	typeName       string
	public         bool
	packageVisible bool
	mutable        bool
	alias          bool
}

type gradedSurfaceOwner struct {
	open, close int
	fields      map[string]gradedSurfaceField
}

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

func gradedSurfaceConstructor(op *operation) bool {
	if op == nil {
		return false
	}
	if surfaceConstructor(op) || op.name == "constructor" {
		return true
	}
	if gradedGoConstructorOwner(op) != "" {
		return true
	}
	if op.language == "rust" && op.name == "new" {
		for i := 0; i+1 < len(op.body); i++ {
			if op.body[i].text == "Self" && op.body[i+1].text == "{" {
				return true
			}
			if op.owner != "" && op.body[i].text == op.owner && op.body[i+1].text == "{" {
				return true
			}
		}
	}
	return false
}

func gradedGoConstructorOwner(op *operation) string {
	if op == nil || op.language != "go" || op.owner != "" {
		return ""
	}
	for i := 0; i+3 < len(op.body); i++ {
		if op.body[i].text != "return" {
			continue
		}
		j := i + 1
		if j < len(op.body) && op.body[j].text == "&" {
			j++
		}
		if j+1 < len(op.body) && isIdentifier(op.body[j].text) && op.body[j+1].text == "{" {
			return op.body[j].text
		}
	}
	// A common Go constructor initializes a local and returns that local. It
	// is recognized only when the returned identifier is tied to a typed
	// struct literal, avoiding arbitrary `name {` expressions in the body.
	for i := 0; i+4 < len(op.body); i++ {
		if !isIdentifier(op.body[i].text) || (op.body[i+1].text != ":=" && op.body[i+1].text != "=") {
			continue
		}
		j := i + 2
		if op.body[j].text == "&" {
			j++
		}
		if j+1 >= len(op.body) || !isIdentifier(op.body[j].text) || op.body[j+1].text != "{" {
			continue
		}
		local, owner := op.body[i].text, op.body[j].text
		for k := j + 2; k+1 < len(op.body); k++ {
			if op.body[k].text == "return" && op.body[k+1].text == local {
				return owner
			}
		}
	}
	return ""
}

func gradedInferSurfaceOwner(tokens []token, language, operationName string) string {
	if language != "rust" {
		return ""
	}
	candidate := ""
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].text != "impl" {
			continue
		}
		owner := ""
		for j := i + 1; j < len(tokens) && tokens[j].text != "{"; j++ {
			if tokens[j].text == "for" && j+1 < len(tokens) && isIdentifier(tokens[j+1].text) {
				owner = tokens[j+1].text
				break
			}
			if owner == "" && isIdentifier(tokens[j].text) {
				owner = tokens[j].text
			}
		}
		if owner == "" {
			continue
		}
		open := i
		for open < len(tokens) && tokens[open].text != "{" {
			open++
		}
		if open >= len(tokens) {
			continue
		}
		close := matching(tokens, open, "{", "}")
		if close < 0 {
			continue
		}
		for j := open + 1; j+1 < close; j++ {
			if tokens[j].text == "fn" && tokens[j+1].text == operationName {
				if candidate != "" && candidate != owner {
					return ""
				}
				candidate = owner
				break
			}
		}
		i = close
	}
	return candidate
}

// callerDeclaredFields exposes the bounded owner inventory to the graded
// evidence pass. The returned map is scoped to this unit and owner; it never
// performs a workspace-wide declaration scan.
func callerDeclaredFields(u unit, owner string) map[string]gradedSurfaceField {
	limit := len(u.tokens)
	if limit > gradedSurfaceTokenLimit {
		limit = gradedSurfaceTokenLimit
	}
	if limit == 0 {
		return nil
	}
	declaration := gradedFindSurfaceOwner(u.tokens[:limit], normalizeLanguage(u.file.Language, u.file.Path), owner)
	if declaration == nil {
		return nil
	}
	return declaration.fields
}

func formatSurfaceUnits(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func gradedCheapOperation(u unit, op *operation) bool {
	body := trimSemicolonTokens(op.body)
	if op.language == "rust" && len(body) > 0 && op.returnType != "" && op.returnType != "()" && body[len(body)-1].text != ";" && body[0].text != "return" {
		body = append([]token{{text: "return"}}, body...)
	}
	if len(body) == 0 || gradedSurfaceConstructor(op) {
		return true
	}
	// A direct field setter has one assignment and no control flow, call, or
	// computed expression. It still exposes a small ordinary caller choice.
	if len(body) >= 4 && gradedHasAssignment(body) && !gradedBodyHasCall(body) && !gradedBodyHasControl(body) {
		meaningful := 0
		for _, item := range body {
			if isIdentifier(item.text) {
				meaningful++
			}
		}
		if meaningful <= 3 {
			return true
		}
	}
	// Pure accessors and simple value predicates are cheap when they return a
	// source value directly. Arithmetic and other transformations remain a
	// caller-visible operation even when the expression is short.
	return gradedDirectRead(body) || gradedOwnedQuery(u, op, body)
}

func gradedDirectRead(body []token) bool {
	if len(body) < 2 || body[0].text != "return" || gradedBodyHasCall(body) || gradedBodyHasControl(body) || gradedHasAssignment(body) {
		return false
	}
	for _, item := range body[1:] {
		switch item.text {
		case "+", "-", "*", "/", "%", "&", "|", "^", "<<", ">>", "?", ":":
			return false
		}
	}
	return true
}

// gradedOwnedQuery admits a sole read-only query through a field declared by
// the owner (for example `return graph.child(edge)`). The declaration and
// field type checks keep package/static utility calls and private helper calls
// from being mistaken for cheap accessors.
func gradedOwnedQuery(u unit, op *operation, body []token) bool {
	if op == nil || op.owner == "" || len(body) < 5 || body[0].text != "return" || gradedBodyHasControl(body) || gradedHasAssignment(body) {
		return false
	}
	open := -1
	for i := 1; i < len(body); i++ {
		if body[i].text == "(" {
			open = i
			break
		}
		if body[i].text == ";" {
			return false
		}
	}
	if open < 3 {
		return false
	}
	close := matching(body, open, "(", ")")
	if close < 0 || close != len(body)-1 || body[open-1].text == "(" {
		return false
	}
	for _, item := range body[open+1 : close] {
		if item.text == "(" || item.text == ")" || item.text == "+" || item.text == "-" || item.text == "*" || item.text == "/" {
			return false
		}
	}
	fields := callerDeclaredFields(u, op.owner)
	if len(fields) == 0 {
		return false
	}
	// The method name is immediately before the call's opening parenthesis.
	// A declared field immediately before a member dot is the receiver root.
	for i := 1; i+1 < open-1; i++ {
		fieldName := body[i].text
		field, declared := fields[fieldName]
		if !declared {
			continue
		}
		if body[i+1].text != "." {
			continue
		}
		if _, typed := op.fieldTypes[fieldName]; !typed && field.typeName == "" {
			continue
		}
		for _, prefix := range body[1:i] {
			if !isIdentifier(prefix.text) && prefix.text != "." {
				return false
			}
		}
		return true
	}
	return false
}

func gradedHasAssignment(body []token) bool {
	for _, item := range body {
		if item.text == "=" || item.text == ":=" || item.text == "+=" || item.text == "-=" || item.text == "*=" || item.text == "/=" {
			return true
		}
	}
	return false
}

func gradedBodyHasCall(body []token) bool {
	for i := 1; i < len(body); i++ {
		if body[i].text == "(" && isIdentifier(body[i-1].text) && body[i-1].text != "if" && body[i-1].text != "return" {
			return true
		}
	}
	return false
}

func gradedBodyHasControl(body []token) bool {
	for _, item := range body {
		switch item.text {
		case "if", "else", "for", "while", "switch", "match", "try", "catch", "throw":
			return true
		}
	}
	return false
}

func gradedFindSurfaceOwner(tokens []token, language, owner string) *gradedSurfaceOwner {
	for i := 0; i+2 < len(tokens); i++ {
		nameIndex := -1
		switch language {
		case "go":
			if tokens[i].text == "type" && i+3 < len(tokens) && tokens[i+2].text == "struct" {
				nameIndex = i + 1
			}
		case "rust":
			if tokens[i].text == "struct" && isIdentifier(tokens[i+1].text) {
				nameIndex = i + 1
			}
		default:
			if tokens[i].text == "class" && isIdentifier(tokens[i+1].text) {
				nameIndex = i + 1
			}
		}
		if nameIndex < 0 || tokens[nameIndex].text != owner {
			continue
		}
		open := nameIndex + 1
		for open < len(tokens) && tokens[open].text != "{" {
			open++
		}
		if open >= len(tokens) {
			continue
		}
		close := matching(tokens, open, "{", "}")
		if close < 0 {
			continue
		}
		return &gradedSurfaceOwner{open: open, close: close, fields: gradedSurfaceFields(tokens, language, open, close)}
	}
	return nil
}

func gradedSurfaceFields(tokens []token, language string, open, close int) map[string]gradedSurfaceField {
	fields := map[string]gradedSurfaceField{}
	if language == "go" {
		// Go's semicolons are inserted by the compiler and are not retained by
		// name followed by a type token, as the existing Go field inventory does.
		// A declaration may contain several names (`Value,Checksum int`), so
		// parse one semicolon-delimited field declaration at a time.
		for i := open + 1; i < close; {
			end := i
			for end < close && tokens[end].text != ";" && (tokens[end].line == 0 || tokens[end].line == tokens[i].line) {
				end++
			}
			segment := tokens[i:end]
			// Names precede the entire type, including slice/map prefixes.
			names := []string{}
			typeIndex := 0
			for typeIndex < len(segment) && isIdentifier(segment[typeIndex].text) {
				names = append(names, segment[typeIndex].text)
				typeIndex++
				if typeIndex >= len(segment) || segment[typeIndex].text != "," {
					break
				}
				typeIndex++
			}
			if typeIndex < len(segment) {
				for _, name := range names {
					fields[name] = gradedSurfaceField{name: name, typeName: joinTokens(segment[typeIndex:]), public: unicode.IsUpper(rune(name[0])), mutable: true, alias: gradedAliasType(segment[typeIndex:])}
				}
			}

			i = end
			if i < close && tokens[i].text == ";" {
				i++
			}
		}
		return fields
	}
	if language == "rust" {
		for i := open + 1; i < close; {
			end := i
			for end < close && tokens[end].text != "," {
				end++
			}
			segment := tokens[i:end]
			for j := 0; j+1 < len(segment); j++ {
				if !isIdentifier(segment[j].text) || segment[j+1].text != ":" {
					continue
				}
				public := j > 0 && segment[j-1].text == "pub"
				typeName := ""
				if j+2 < len(segment) && isIdentifier(segment[j+2].text) {
					typeName = segment[j+2].text
				}
				fields[segment[j].text] = gradedSurfaceField{name: segment[j].text, typeName: typeName, public: public, mutable: true, alias: gradedAliasType(segment[j+2:])}
				break
			}
			i = end + 1
		}
		return fields
	}
	// Java and TypeScript class members have explicit semicolon or method
	// braces. The parser only admits top-level members within the owner.
	for i := open + 1; i < close; {
		if methodEnd := gradedMemberMethodEnd(tokens, i, close); methodEnd > i {
			if language == "typescript" && tokens[i].text == "constructor" {
				gradedTypeScriptConstructorFields(fields, tokens, i, methodEnd)
			}
			i = methodEnd
			continue
		}
		end := i
		depth := 0
		for end < close {
			switch tokens[end].text {
			case "{":
				depth++
			case "}":
				if depth > 0 {
					depth--
				}
			case ";":
				if depth == 0 {
					end++
					goto memberEnd
				}
			}
			end++
		}
	memberEnd:
		segment := tokens[i:end]
		public, mutable, explicitVisibility := false, true, false
		for _, item := range segment {
			switch item.text {
			case "public", "protected", "export":
				explicitVisibility = true
				public = item.text == "public" || item.text == "export"
			case "private", "readonly", "final", "const", "static":
				if item.text == "private" {
					explicitVisibility = true
				}
				if item.text == "readonly" || item.text == "final" || item.text == "const" {
					mutable = false
				}
			}
		}
		if language == "typescript" && !explicitVisibility {
			public = true
		}
		if language == "java" {
			gradedJavaFields(fields, segment, public, mutable)
			if !explicitVisibility {
				for name, f := range fields {
					if f.name == name && gradedSegmentContains(segment, name) {
						f.packageVisible = true
						fields[name] = f
					}
				}
			}
		}
		if language != "typescript" {
			i = end
			if i == open+1 {
				i++
			}
			continue
		}
		for j := 0; j < len(segment); j++ {
			if !isIdentifier(segment[j].text) {
				continue
			}
			if language == "typescript" && j+1 < len(segment) && (segment[j+1].text == ":" || segment[j+1].text == "=") {
				fields[segment[j].text] = gradedSurfaceField{name: segment[j].text, typeName: gradedFieldTypeAfter(segment, j+1), public: public, mutable: mutable, alias: gradedAliasType(segment[j+2:])}
			}
		}
		i = end
		if i == open+1 {
			i++
		}
	}
	return fields
}

func gradedTypeScriptConstructorFields(fields map[string]gradedSurfaceField, tokens []token, start, methodEnd int) {
	paren := -1
	for i := start; i < methodEnd; i++ {
		if tokens[i].text == "(" {
			paren = i
			break
		}
	}
	if paren < 0 {
		return
	}
	end := matching(tokens, paren, "(", ")")
	if end < 0 || end > methodEnd {
		return
	}
	for i := paren + 1; i < end; {
		segmentEnd := i
		depth := 0
		for segmentEnd < end {
			switch tokens[segmentEnd].text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					goto parameterEnd
				}
			}
			segmentEnd++
		}
	parameterEnd:
		segment := tokens[i:segmentEnd]
		public, mutable, property := false, true, false
		for _, item := range segment {
			switch item.text {
			case "public", "export":
				public, property = true, true
			case "private", "protected":
				property = true
			case "readonly":
				mutable, property = false, true
			}
		}
		if property {
			for j, item := range segment {
				if !isIdentifier(item.text) || item.text == "public" || item.text == "private" || item.text == "protected" || item.text == "readonly" {
					continue
				}
				if j+1 < len(segment) && (segment[j+1].text == ":" || segment[j+1].text == "=") {
					fields[item.text] = gradedSurfaceField{name: item.text, typeName: gradedFieldTypeAfter(segment, j+1), public: public, mutable: mutable, alias: gradedAliasType(segment[j+1:])}
					break
				}
			}
		}
		i = segmentEnd + 1
	}
}

func gradedMemberMethodEnd(tokens []token, start, close int) int {
	paren := -1
	for i := start; i < close; i++ {
		switch tokens[i].text {
		case "=", ";":
			return 0
		case "(":
			paren = i
			break
		case "{":
			if paren < 0 {
				return 0
			}
		}
		if paren >= 0 {
			break
		}
	}
	if paren < 0 {
		return 0
	}
	paramsEnd := matching(tokens, paren, "(", ")")
	if paramsEnd < 0 {
		return 0
	}
	bodyStart := paramsEnd + 1
	for bodyStart < close && tokens[bodyStart].text != "{" {
		if tokens[bodyStart].text == ";" {
			return 0
		}
		bodyStart++
	}
	if bodyStart >= close {
		return 0
	}
	bodyEnd := matching(tokens, bodyStart, "{", "}")
	if bodyEnd < 0 {
		return 0
	}
	return bodyEnd + 1
}

func gradedFieldTypeAfter(segment []token, separator int) string {
	if separator < 0 || separator+1 >= len(segment) || segment[separator].text == "=" {
		return ""
	}
	if isIdentifier(segment[separator+1].text) {
		return segment[separator+1].text
	}
	return ""
}

func gradedJavaFields(fields map[string]gradedSurfaceField, segment []token, public, mutable bool) {
	modifiers := map[string]bool{"public": true, "private": true, "protected": true, "static": true, "final": true, "volatile": true, "transient": true}
	i := 0
	for i < len(segment) && modifiers[segment[i].text] {
		i++
	}
	if i >= len(segment) || !isIdentifier(segment[i].text) {
		return
	}
	typ := segment[i].text
	i++
	for i+1 < len(segment) && segment[i].text == "." && isIdentifier(segment[i+1].text) {
		typ = segment[i+1].text
		i += 2
	}
	if i < len(segment) && segment[i].text == "<" {
		depth := 1
		i++
		for i < len(segment) && depth > 0 {
			switch segment[i].text {
			case "<":
				depth++
			case ">":
				depth--
			case ">>":
				depth -= 2
			case ">>>":
				depth -= 3
			}
			i++
		}
	}
	alias := false
	for i+1 < len(segment) && segment[i].text == "[" && segment[i+1].text == "]" {
		alias = true
		i += 2
	}
	for i < len(segment) {
		if !isIdentifier(segment[i].text) {
			return
		}
		name := segment[i].text
		i++
		fields[name] = gradedSurfaceField{name: name, typeName: typ, public: public, mutable: mutable, alias: alias || gradedAliasType(segment)}
		depth := 0
		for i < len(segment) {
			t := segment[i].text
			if t == "(" || t == "{" || t == "[" {
				depth++
			}
			if t == ")" || t == "}" || t == "]" {
				depth--
			}
			if t == "," && depth == 0 {
				i++
				break
			}
			i++
		}
	}
}

func gradedSegmentHasCall(segment []token) bool {
	for i := 1; i < len(segment); i++ {
		if segment[i].text == "(" && isIdentifier(segment[i-1].text) {
			return true
		}
	}
	return false
}

func gradedAliasType(tokens []token) bool {
	for _, item := range tokens {
		switch item.text {
		case "[", "]", "*", "&", "Vec", "map", "Map", "List", "Set", "HashMap", "slice":
			return true
		}
	}
	return false
}

func gradedSegmentContains(segment []token, name string) bool {
	for _, t := range segment {
		if t.text == name {
			return true
		}
	}
	return false
}
