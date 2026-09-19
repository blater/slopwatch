package sourceestimate

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
		classifyJavaShape(name, shape, tokens, operations)
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

func classifyJavaShape(name string, shape *javaClassShape, tokens []token, operations []*operation) {
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
