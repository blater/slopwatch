package sourceestimate

import "sort"

const (
	roleJavaPassiveData = "supporting-java-data-type"
	roleJavaErrorData   = "supporting-java-error-type"
)

// JavaAttribution is the bounded structural classification used by callers
// which need to separate nested support types from service entrypoints. It is
// deliberately based on declaration shape and operation bodies; names are
// never used as role heuristics.
type JavaAttribution struct {
	File        string
	Owner       string
	Audience    string
	Role        string
	Supporting  bool
	Constructor bool
}

type javaClassShape struct {
	name        string
	public      bool
	exception   bool
	passive     bool
	start       int
	end         int
	constructor bool
}

type attributionOwnerKey struct {
	file  int
	owner string
}

func attributionOwner(op *operation) attributionOwnerKey {
	if op == nil {
		return attributionOwnerKey{}
	}
	return attributionOwnerKey{file: op.file, owner: op.owner}
}

// ClassifyJavaSources returns per-file, per-owner structural roles. A nested
// passive/error type is supporting evidence; methods on an ordinary
// package-private service remain callable roots.
func ClassifyJavaSources(files []File) map[string][]JavaAttribution {
	units := javaUnits(files)
	supporting := supportingJavaOwners(units)
	output := make(map[string][]JavaAttribution)
	for _, unit := range units {
		shapes := javaClassShapes(unit.tokens, unit.ops)
		seen := map[string]bool{}
		for _, op := range unit.ops {
			shape, ok := shapes[op.owner]
			role, supported := supporting[attributionOwner(op)]
			if !ok || !supported || seen[op.owner] {
				continue
			}
			seen[op.owner] = true
			output[unit.file.Path] = append(output[unit.file.Path], JavaAttribution{
				File: unit.file.Path, Owner: op.owner, Audience: javaAudience(shape), Role: role, Supporting: true,
				Constructor: shape.constructor,
			})
		}
	}
	for path := range output {
		sort.Slice(output[path], func(left, right int) bool { return output[path][left].Owner < output[path][right].Owner })
	}
	return output
}

// AnalyzeJavaFiles computes source fallback estimates per Java file while
// retaining private helpers in the shared operation graph. Constructors and
// proven passive/error accessors never become behavioral roots.
func AnalyzeJavaFiles(files []File) map[string]Result {
	units := javaUnits(files)
	all := make([]*operation, 0)
	for _, unit := range units {
		all = append(all, unit.ops...)
	}
	byKey := make(map[string][]*operation, len(all))
	for _, op := range all {
		indexOperation(byKey, op)
	}
	annotateSourceSurfaceInputs(units, byKey)
	return analyzeJavaUnits(units, byKey)
}

// analyzeJavaUnits projects Java roots from a shared parser/index. Keeping
// this separate from AnalyzeJavaFiles lets the mixed-language attribution
// pipeline reuse one cross-file call graph rather than reparsing Java files.
func analyzeJavaUnits(units []unit, byKey map[string][]*operation) map[string]Result {
	supporting := supportingJavaOwners(units)
	results := make(map[string]Result)
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "java" {
			continue
		}
		shapes := javaClassShapes(unit.tokens, unit.ops)
		roles := map[string]string{}
		rootsByOwner := map[string][]*operation{}
		for _, op := range unit.ops {
			role, supported := supporting[attributionOwner(op)]
			if supported {
				op.exposed = false
				roles[op.owner] = role
				continue
			}
			if op.owner != "" {
				// findOperations intentionally keeps a broad class-level export
				// bit so helper resolution can see every method. Recompute the
				// root surface here: private helpers remain reachable dependencies,
				// while package-visible methods are roots for either class audience.
				constructor := op.name == op.owner
				op.exposed = op.packageVisible && (!constructor || len(op.body) != 0)
			}
			if op.exposed {
				rootsByOwner[op.owner] = append(rootsByOwner[op.owner], op)
			}
		}

		var best Result
		haveBest := false
		abstractions := make([]Abstraction, 0, len(rootsByOwner))
		owners := make([]string, 0, len(rootsByOwner))
		for owner := range rootsByOwner {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		for _, owner := range owners {
			projection := estimateRoots(unit.index, unit, units, byKey, rootsByOwner[owner])
			shape := shapes[owner]
			audience := "external"
			if shape != nil {
				audience = javaAudience(shape)
			}
			name := owner
			if name == "" {
				name = "free"
			}
			abstractions = append(abstractions, Abstraction{Grade: projection.Grade, Name: name, Audience: audience, Burden: projection.Burden, Hidden: projection.Hidden, Findings: projection.Findings, RecognizedHidden: projection.RecognizedHidden(), UncertainBurden: projection.Assessment().UncertainBurden, Limitations: append([]string(nil), projection.Limitations...)})
			if !haveBest || largerProjection(projection, best) {
				best, haveBest = projection, true
			}
		}
		if !haveBest {
			best = estimateRoots(unit.index, unit, units, byKey, nil)
		}
		for owner, role := range roles {
			best.Roles = appendUniqueString(best.Roles, role)
			if best.Supporting == nil {
				best.Supporting = map[string]string{}
			}
			best.Supporting[owner] = role
		}
		best.Abstractions = abstractions
		if len(best.Roles) > 0 && !javaHasBehaviorRoot(unit, supporting) && javaRoleOnlySafe(unit, shapes) {
			best.Applicable, best.Burden, best.Hidden = false, 0, 0
			best.RoleOnly = true
			best.Abstractions = nil
		}
		results[unit.file.Path] = best
	}
	return results
}

func javaUnits(files []File) []unit {
	units := make([]unit, 0, len(files))
	for index, file := range files {
		if normalizeLanguage(file.Language, file.Path) != "java" {
			continue
		}
		tokens, limited, valid := lex(file.Source)
		pkg := packageName(file.Language, tokens)
		item := unit{index: index, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: valid}
		item.ops = findOperations(file, index, tokens, pkg)
		hasExternal := false
		for _, op := range item.ops {
			hasExternal = hasExternal || op.exposed
		}
		if !hasExternal {
			for _, op := range item.ops {
				op.exposed = op.packageVisible
			}
		}
		units = append(units, item)
	}
	return units
}

func javaHasBehaviorRoot(unit unit, supporting map[attributionOwnerKey]string) bool {
	for _, op := range unit.ops {
		constructor := op.owner != "" && op.name == op.owner
		if op.exposed && !(constructor && (supporting[attributionOwner(op)] != "" || len(op.body) == 0)) {
			return true
		}
	}
	return false
}

func supportingJavaOwners(units []unit) map[attributionOwnerKey]string {
	output := map[attributionOwnerKey]string{}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "java" || unit.limited || !unit.lexicallyValid {
			continue
		}
		shapes := javaClassShapes(unit.tokens, unit.ops)
		for _, op := range unit.ops {
			shape := shapes[op.owner]
			if shape == nil || !shape.passive {
				continue
			}
			if shape.exception {
				output[attributionOwner(op)] = roleJavaErrorData
			} else {
				output[attributionOwner(op)] = roleJavaPassiveData
			}
		}
	}
	return output
}

func javaAudience(shape *javaClassShape) string {
	if shape.public {
		return "external"
	}
	return "package"
}

func javaClassShapes(tokens []token, operations []*operation) map[string]*javaClassShape {
	shapes := map[string]*javaClassShape{}
	for index := 0; index+1 < len(tokens); index++ {
		if tokens[index].text != "class" || !isIdentifier(tokens[index+1].text) {
			continue
		}
		name := tokens[index+1].text
		open := index + 2
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
		shape := &javaClassShape{name: name, public: hasModifier(tokens, index, "public"), start: open, end: close}
		shape.exception = javaThrowableBase(tokens[index+2 : open])
		shapes[name] = shape
	}
	for name, shape := range shapes {
		methods := 0
		passive := true
		getter := false
		fields := javaInstanceFieldNames(tokens, shape.start, shape.end)
		field := len(fields) != 0
		for _, op := range operations {
			if op.owner != name {
				continue
			}
			if op.name == name {
				shape.constructor = true
				if !javaPassiveConstructor(op.body) {
					passive = false
				}
				continue
			}
			methods++
			if javaPassiveGetter(op, fields) {
				getter = true
			} else if !javaPassiveMutation(op.body) {
				passive = false
			}
		}
		shape.passive = field && getter && methods > 0 && passive && !javaUnsafeInitialization(tokens, shape.start, shape.end)
	}
	return shapes
}

func javaThrowableBase(tokens []token) bool {
	for index, item := range tokens {
		if item.text != "extends" || index+1 >= len(tokens) {
			continue
		}
		switch tokens[index+1].text {
		case "Throwable", "Exception", "RuntimeException", "Error", "AssertionError", "ReflectiveOperationException":
			return true
		}
	}
	return false
}

func hasJavaField(tokens []token, start, end int) bool {
	depth := 0
	segment := []token{}
	for index := start + 1; index < end; index++ {
		switch tokens[index].text {
		case "{":
			depth++
		case "}":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 && javaFieldSegment(segment) {
				return true
			}
			segment = nil
		default:
			if depth == 0 {
				segment = append(segment, tokens[index])
			}
		}
	}
	return false
}

func javaFieldSegment(segment []token) bool {
	identifiers := 0
	for _, item := range segment {
		if item.kind == "identifier" && item.text != "private" && item.text != "public" && item.text != "protected" && item.text != "static" && item.text != "final" {
			identifiers++
		}
		if item.text == "(" {
			return false
		}
	}
	return identifiers >= 2
}

func javaUnsafeInitialization(tokens []token, start, end int) bool {
	depth := 0
	segment := []token{}
	for index := start + 1; index < end; index++ {
		item := tokens[index]
		switch item.text {
		case "{":
			if depth == 0 && javaInitializerSegment(segment) {
				return true
			}
			depth++
			segment = nil
		case "}":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 && javaUnsafeFieldInitializer(segment) {
				return true
			}
			segment = nil
		default:
			if depth == 0 {
				segment = append(segment, item)
			}
		}
	}
	return false
}

func javaInitializerSegment(segment []token) bool {
	if len(segment) == 0 {
		return true
	}
	for _, item := range segment {
		if item.text == "(" {
			return false
		}
	}
	return true
}

func javaUnsafeFieldInitializer(segment []token) bool {
	equal := -1
	for index, item := range segment {
		if item.text == "=" {
			equal = index
			break
		}
	}
	if equal < 0 {
		return false
	}
	for _, item := range segment[equal+1:] {
		switch item.text {
		case "new", "(", ")", "+", "-", "*", "/", "%", "++", "--":
			return true
		}
	}
	return false
}

func javaPassiveConstructor(body []token) bool {
	return javaPassiveStatements(body, true)
}

func javaPassiveMutation(body []token) bool {
	return javaPassiveStatements(body, false)
}

func javaPassiveStatements(body []token, constructor bool) bool {
	if len(body) == 0 {
		return true
	}
	for _, item := range body {
		switch item.text {
		case "if", "else", "for", "while", "switch", "try", "catch", "throw", "new", "++", "--", "+=", "-=", "*=", "/=":
			return false
		}
	}
	if constructor {
		return javaAssignmentsOnly(body)
	}
	return javaAssignmentsOnly(body)
}

func javaPassiveGetter(operation *operation, fields map[string]bool) bool {
	if operation == nil || operation.params != 0 {
		return false
	}
	trimmed := trimJavaSemicolon(operation.body)
	if len(trimmed) < 2 || trimmed[0].text != "return" {
		return false
	}
	value := trimmed[1:]
	if len(value) == 1 {
		return value[0].kind == "identifier" && fields[value[0].text]
	}
	return len(value) == 3 && value[0].text == "this" && value[1].text == "." && value[2].kind == "identifier" && fields[value[2].text]
}

// javaInstanceFieldNames returns only names declared by top-level instance
// field segments. Static constants may be harmless data, but a getter for one
// does not prove that the object itself is a passive carrier.
func javaInstanceFieldNames(tokens []token, start, end int) map[string]bool {
	fields := map[string]bool{}
	depth := 0
	segment := []token{}
	flush := func() {
		if len(segment) == 0 || javaSegmentHasCall(segment) || javaSegmentHasModifier(segment, "static") {
			segment = nil
			return
		}
		for _, name := range javaSegmentFieldNames(segment) {
			fields[name] = true
		}
		segment = nil
	}
	for index := start + 1; index < end; index++ {
		item := tokens[index]
		switch item.text {
		case "{", "(":
			if depth == 0 {
				// A top-level method/initializer is not a field declaration.
				segment = nil
			}
			depth++
		case "}", ")":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 {
				flush()
			}
		default:
			if depth == 0 {
				segment = append(segment, item)
			}
		}
	}
	flush()
	return fields
}

func javaSegmentHasModifier(segment []token, modifier string) bool {
	for _, item := range segment {
		if item.text == modifier {
			return true
		}
	}
	return false
}

func javaSegmentHasCall(segment []token) bool {
	for _, item := range segment {
		if item.text == "(" {
			return true
		}
	}
	return false
}

func javaSegmentFieldNames(segment []token) []string {
	names := []string{}
	seenType := false
	for index, item := range segment {
		if item.text == "=" {
			break
		}
		if item.text == "," {
			continue
		}
		if item.kind != "identifier" || item.text == "private" || item.text == "protected" || item.text == "public" || item.text == "final" || item.text == "volatile" || item.text == "transient" {
			continue
		}
		if !seenType {
			seenType = true
			continue
		}
		// A declaration's first identifier is its type. Later identifiers
		// before a comma/initializer are field names; arrays and qualified
		// types remain conservative because their token shape is not scalar.
		if index == 0 || (index > 0 && segment[index-1].text == ".") {
			continue
		}
		names = append(names, item.text)
	}
	return names
}

func javaAssignmentsOnly(body []token) bool {
	statements := splitJavaStatements(body)
	if len(statements) == 0 {
		return true
	}
	for _, statement := range statements {
		equal := -1
		for index, item := range statement {
			if item.text == "=" {
				if equal >= 0 {
					return false
				}
				equal = index
			}
			if item.text == "(" || item.text == ")" || item.text == "+" || item.text == "-" || item.text == "*" || item.text == "/" {
				return false
			}
		}
		if equal <= 0 || equal+1 >= len(statement) || !javaFieldReference(statement[:equal]) {
			return false
		}
		for _, item := range statement[equal+1:] {
			if item.kind != "identifier" && item.kind != "number" && item.kind != "string" && item.text != "null" && item.text != "true" && item.text != "false" && item.text != "." {
				return false
			}
		}
	}
	return true
}

func javaFieldReference(items []token) bool {
	if len(items) == 1 {
		return items[0].kind == "identifier"
	}
	return len(items) == 3 && items[0].text == "this" && items[1].text == "." && items[2].kind == "identifier"
}

func splitJavaStatements(body []token) [][]token {
	output := [][]token{}
	start := 0
	for index, item := range body {
		if item.text == ";" {
			if index > start {
				output = append(output, body[start:index])
			}
			start = index + 1
		}
	}
	if start < len(body) {
		output = append(output, body[start:])
	}
	return output
}

func trimJavaSemicolon(body []token) []token {
	end := len(body)
	for end > 0 && body[end-1].text == ";" {
		end--
	}
	return body[:end]
}

func javaRoleOnlySafe(u unit, shapes map[string]*javaClassShape) bool {
	if u.limited || !u.lexicallyValid {
		return false
	}
	for _, shape := range shapes {
		if javaUnsafeInitialization(u.tokens, shape.start, shape.end) {
			return false
		}
	}
	return true
}
