package sourceestimate

import (
	"math"
	"sort"
	"strings"
)

// GradedEvidence separates the caller's observable contract from knowledge of
// its implementation. A simple operation with one input establishes the scale;
// it is not a fictitious unit of hidden responsibility.
type GradedEvidence struct {
	DenominatorReference     float64            `json:"denominator_reference,omitempty"`
	ResponsibilityMultiplier float64            `json:"responsibility_multiplier,omitempty"`
	Surface                  CallerSurface      `json:"surface"`
	ResidualBurden           float64            `json:"residual_burden"`
	SupportedBurden          float64            `json:"supported_burden"`
	Hidden                   float64            `json:"hidden_responsibility"`
	EstimatedHidden          float64            `json:"estimated_hidden_responsibility,omitempty"`
	Responsibilities         map[string]float64 `json:"responsibilities"`
	MaterialLimitations      []string           `json:"material_limitations,omitempty"`
	ZeroReason               string             `json:"zero_reason,omitempty"`
}

func (g *GradedEvidence) Value() float64 {
	if g == nil || g.ResidualBurden <= 0 {
		return 0
	}
	reference, multiplier := g.DenominatorReference, g.ResponsibilityMultiplier
	if reference == 0 || multiplier == 0 {
		p := DefaultCalibration()
		reference, multiplier = p.DenominatorReference, p.ResponsibilityMultiplier
	}
	return math.Round(100 * g.SupportedBurden / (reference + g.ResidualBurden + multiplier*(g.Hidden+g.EstimatedHidden)))
}

func assessGradedEvidence(u unit, roots []*operation, units []unit, byKey map[string][]*operation, evidence []evidenceItem, result Result) *GradedEvidence {
	p := u.gradingProfile()
	g := &GradedEvidence{DenominatorReference: p.DenominatorReference, ResponsibilityMultiplier: p.ResponsibilityMultiplier, Surface: gradedCallerSurface(u, roots, units), Responsibilities: map[string]float64{}}
	rootOwners := map[string]bool{}
	for _, op := range roots {
		rootOwners[op.owner] = true
	}
	for owner := range rootOwners {
		if _, observed := u.callerObligations[owner]; observed {
			continue
		}
		for _, field := range callerDeclaredFields(u, owner) {
			if field.packageVisible && field.mutable {
				g.MaterialLimitations = append(g.MaterialLimitations, "unobserved_package_caller_obligations:"+owner)
				break
			}
		}
	}
	protocol := map[string]map[string]bool{}
	for _, item := range evidence {
		if item.origin == nil || rootOwners[item.origin.owner] {
			continue
		}
		key := itoa(item.origin.file) + "#" + item.origin.owner
		if _, exists := protocol[key]; !exists {
			protocol[key] = publicProtocolPrerequisites(item.origin, units)
		}
	}
	transparent := map[string]bool{}
	for _, root := range roots {
		if c, ok := gradeTransparentCall(root); ok {
			if candidates := resolveCall(root, c, units, byKey); len(candidates) == 1 {
				callee := candidates[0]
				if len(protocol[itoa(callee.file)+"#"+callee.owner]) > 0 {
					transparent[callee.id] = true
				}
			}
		}
	}
	excludedLimits := map[string]bool{}
	ownedUnknown := false
	for _, item := range evidence {
		excluded := item.origin != nil && !rootOwners[item.origin.owner] && len(protocol[itoa(item.origin.file)+"#"+item.origin.owner]) > 0
		if excluded {
			for _, c := range callsIn(item.origin.body) {
				excludedLimits["unresolved_call_range_0_2:"+c.signature] = true
			}
		} else if strings.HasPrefix(item.category, "unknown_") {
			ownedUnknown = true
		}
	}
	if !ownedUnknown {
		excludedLimits["unsupported_outcome_range_0_2"] = true
	}
	storageSnapshots := map[string]bool{}
	for _, item := range evidence {
		if item.category == "storage_snapshot" && item.origin != nil {
			storageSnapshots[item.origin.id] = storageSnapshots[item.origin.id] || item.storageComputed
		}
	}
	seen := map[string]bool{}
	dispatches := map[string]string{}
	dispatchFor := func(op *operation) string {
		if key, assessed := dispatches[op.id]; assessed {
			return key
		}
		key, _ := gradedCapabilityDispatch(op, pruneDeadFalseBranches(op.body))
		dispatches[op.id] = key
		return key
	}
	for _, item := range evidence {
		key := item.key
		if item.category == "validation" && item.origin != nil {
			if dispatch := dispatchFor(item.origin); dispatch != "" {
				key = "validation|capability:" + dispatch
			}
		}
		if item.category == "storage_snapshot" || strings.HasPrefix(item.category, "unknown_") || seen[key] {
			continue
		}
		// Re-exporting an independently available protocol does not transfer
		// its implementation's duties to this wrapper. Duties composed by the
		// wrapper (including cleanup) are counted below at their owning roots.
		if item.origin != nil && !rootOwners[item.origin.owner] && (transparent[item.origin.id] || len(protocol[itoa(item.origin.file)+"#"+item.origin.owner]) > 0 && protocolCallerDuty(item, units[item.origin.file], protocol[itoa(item.origin.file)+"#"+item.origin.owner])) {
			continue
		}
		seen[key] = true
		g.Responsibilities[item.category] += p.categoryWeight(item.category)
	}
	if resource, relocation := gradedRustResourceEffects(u, roots); resource || relocation {
		if resource {
			g.Responsibilities["resource"] = math.Max(p.Resource, g.Responsibilities["resource"])
		}
		if relocation {
			g.Responsibilities["representation-transformation"] = math.Max(p.RepresentationTransformation, g.Responsibilities["representation-transformation"])
		}
	}
	coordinated := map[string]bool{}
	connectedFactories := map[string]int{}
	phaseCommands := map[string]map[string]bool{}
	callerPhases := map[string]map[string]bool{}
	phaseMethods := map[string]map[string]bool{}
	for _, root := range roots {
		ops := gradeOwnedOperations(root, units, byKey)
		calls := map[string]map[string]bool{}
		for _, op := range ops {
			body := pruneDeadFalseBranches(op.body)
			if n := gradedCachedFactories(op, units[op.file], units, byKey); n > 0 {
				connectedFactories[op.id] = n
			}
			if gradedIndependentJDBCCleanup(op, units[op.file], roots) {
				g.Responsibilities["resource"] = math.Max(p.Resource, g.Responsibilities["resource"])
			}
			opReceivers := map[string]bool{}
			for _, c := range callsIn(body) {
				if unreachableCall(body, c) {
					continue
				}
				dot := strings.LastIndexByte(c.name, '.')
				if dot < 0 {
					continue
				}
				receiver := gradeReceiver(op, c.name[:dot])
				if receiver == "this" || receiver == "self" || receiver == op.receiverName {
					continue
				}
				if calls[receiver] == nil {
					calls[receiver] = map[string]bool{}
				}
				calls[receiver][c.name[dot+1:]] = true
				opReceivers[receiver] = true
			}
			for receiver := range opReceivers {
				if gradeLocalCoordination(op, units[op.file], body, receiver) {
					coordinated[receiver] = true
				}
			}
			if !gradedSurfaceConstructor(op) {
				computed, observed := storageSnapshots[op.id]
				if !observed {
					computed = gradeComputedAssignment(op, units[op.file], body)
				}
				if computed {
					g.Responsibilities["invariant-transformation"] = p.InvariantTransformation
				}
				if gradeConsistentWrites(op, units[op.file], body) {
					g.Responsibilities["state-consistency"] = p.StateConsistency
				}
			}
			if gradeTypedAlternatives(op, units[op.file], body) && dispatchFor(op) == "" {
				g.Responsibilities["representation-transformation"] = p.RepresentationTransformation
			}
			if op.language == "rust" && gradeAssertion(body) {
				g.Responsibilities["validation"] = math.Max(p.Validation, g.Responsibilities["validation"])
			}
		}
		for receiver, methods := range calls {
			if !gradeOwnedReceiver(root, units[root.file], receiver) {
				continue
			}
			if phaseCommands[receiver] == nil {
				phaseCommands[receiver] = map[string]bool{}
			}
			if gradeCommand(root) {
				phaseCommands[receiver][root.id] = true
			}
			if callerPhases[receiver] == nil {
				callerPhases[receiver] = map[string]bool{}
				phaseMethods[receiver] = map[string]bool{}
			}
			callerPhases[receiver][root.id] = true
			for name := range methods {
				phaseMethods[receiver][name] = true
			}
		}
		if gradeGuaranteedCleanup(ops, units, byKey) {
			g.Responsibilities["resource"] = math.Max(p.Cleanup, g.Responsibilities["resource"])
		}
	}
	for _, count := range connectedFactories {
		g.Responsibilities["cache-consistency"] += p.StateConsistency * float64(count)
	}
	if len(coordinated) > 0 {
		g.Responsibilities["coordination"] = math.Max(p.Coordination*float64(len(coordinated)), g.Responsibilities["coordination"])
	}
	if g.Surface.RepresentationUnits > 0 && g.Responsibilities["validation"] > p.ExposedValidationCap {
		g.Responsibilities["validation"] = p.ExposedValidationCap
	}
	moduleState, moduleComputed := typeScriptModuleScalarEffects(u, roots, units, byKey)
	if moduleState {
		g.Responsibilities["state"] = math.Max(p.State, g.Responsibilities["state"])
	}
	if moduleComputed {
		g.Responsibilities["invariant-transformation"] = math.Max(p.InvariantTransformation, g.Responsibilities["invariant-transformation"])
	}
	categories := make([]string, 0, len(g.Responsibilities))
	for category := range g.Responsibilities {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		g.Hidden += g.Responsibilities[category]
	}
	g.EstimatedHidden = gradeUnresolvedReturnedDuty(roots, units, byKey, g.Responsibilities, p)
	if gradedPossibleOwnedProjection(roots, u) {
		g.EstimatedHidden = math.Max(g.EstimatedHidden, math.Max(0, p.TransformationEnvelope-g.Responsibilities["invariant-transformation"]-g.Responsibilities["transform"]-g.Responsibilities["representation-transformation"]))
		g.MaterialLimitations = append(g.MaterialLimitations, "unresolved_owned_element_projection")
	}
	if gradedUnresolvedCleanup(roots, units, byKey) {
		g.EstimatedHidden += math.Max(0, p.Resource-g.Responsibilities["resource"])
		g.MaterialLimitations = append(g.MaterialLimitations, "unresolved_connected_cleanup")
	}
	for receiver, phases := range callerPhases {
		if len(phaseCommands[receiver]) > 0 && len(phases) > 1 && len(phaseMethods[receiver]) > 1 {
			transitions := len(phaseCommands[receiver])
			if transitions == len(phases) {
				transitions--
			}
			g.Surface.RepresentationUnits += p.Sequencing * float64(transitions)
			g.Surface.Evidence = append(g.Surface.Evidence, "caller-sequencing:"+receiver)
		}
	}
	g.ResidualBurden = math.Max(0, g.Surface.OperationUnits+g.Surface.InputUnits-p.ResidualReference) + g.Surface.RepresentationUnits
	g.SupportedBurden = g.ResidualBurden
	for _, reason := range result.Limitations {
		if excludedLimits[reason] || strings.HasPrefix(reason, "unsupported_execution_alternatives:") {
			continue
		}
		g.MaterialLimitations = append(g.MaterialLimitations, reason)
	}

	if g.Value() == 0 {
		g.ZeroReason = "lowest_range_supported"
		if len(roots) == 0 && !result.NoAbstractionProven {
			g.ZeroReason = "conservative_uncertainty"
		}
	}
	sort.Strings(g.Surface.Evidence)
	sort.Strings(g.MaterialLimitations)
	return g
}

// Follow only local implementation helpers here. The primary evidence traversal
// already follows owned delegates; this pass reconstructs local coordination
// across helper extraction without awarding an extra duty for the helper call.
func gradeOwnedOperations(root *operation, units []unit, byKey map[string][]*operation) []*operation {
	result := []*operation{}
	seen := map[string]bool{}
	var visit func(*operation, int)
	visit = func(op *operation, depth int) {
		if depth > maxCallDepth || len(result) >= maxCallsPerRoot || seen[op.id] {
			return
		}
		seen[op.id] = true
		result = append(result, op)
		for _, c := range callsIn(pruneDeadFalseBranches(op.body)) {
			matches := resolveCall(op, c, units, byKey)
			if len(matches) == 1 && matches[0].owner == root.owner && !matches[0].exposed {
				visit(matches[0], depth+1)
			}
		}
	}
	visit(root, 0)
	return result
}

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

func gradeComputedAssignment(op *operation, u unit, body []token) bool {
	for _, effect := range gradedStorageEffects(op, u, body) {
		if effect.computed {
			return true
		}
	}
	return false
}

func gradeTypedAlternatives(op *operation, u unit, body []token) bool {
	body = gradedEagerBody(pruneDeadFalseBranches(body), op.language)
	ranges := [][2]int{}
	for i, t := range body {
		if t.text != "switch" && t.text != "match" {
			continue
		}
		open := i + 1
		for open < len(body) && body[open].text != "{" {
			open++
		}
		if open >= len(body) {
			continue
		}
		close := matching(body, open, "{", "}")
		if close < 0 {
			continue
		}
		ranges = append(ranges, [2]int{open, close})
	}
	inside := func(index int) bool {
		for _, span := range ranges {
			if index > span[0] && index < span[1] {
				return true
			}
		}
		return false
	}
	if len(ranges) == 0 {
		return false
	}

	// Rust's final match is itself the returned expression.
	if op.language == "rust" && op.returnType != "" && op.returnType != "()" && len(body) > 0 && body[len(body)-1].text == "}" {
		for _, statement := range gradedResultStatements(body) {
			if len(statement) == 0 || statement[0].text != "match" {
				continue
			}
			open := 0
			for open < len(statement) && statement[open].text != "{" {
				open++
			}
			if open < len(statement) && statement[len(statement)-1].offset == body[len(body)-1].offset && gradedRustReturnedMatchCalls(statement[open+1:len(statement)-1]) {
				return true
			}
		}
	}
	for _, c := range callsIn(body) {
		if !inside(c.position) {
			continue
		}
		for _, buffer := range op.outputBuffers {
			if strings.HasPrefix(c.name, buffer+".") {
				for _, argument := range c.actuals {
					if len(callsIn(argument)) > 0 {
						return true
					}
				}
			}
		}
	}
	// A branch's call is useful only when its result reaches a returned
	// expression. Calls/calculations in discarded branch locals do not qualify.
	for i, tok := range body {
		if !inside(i) {
			continue
		}
		if tok.text == "return" {
			end := statementEnd(body, i+1)
			if len(callsIn(body[i+1:end])) > 0 || hasTransform(body[i+1:end]) {
				return true
			}
		}
		if tok.text == "=" && i > 0 && gradeOwnedFieldReference(op, u, body, i-1) {
			end := statementEnd(body, i+1)
			for j := i + 1; j < end; j++ {
				if body[j].text == "{" && matching(body, j, "{", "}") >= 0 {
					return true
				}
			}
		}
	}
	return false
}

func gradedRustReturnedMatchCalls(arms []token) bool {
	for i, t := range arms {
		if t.text != "=>" {
			continue
		}
		start, end := i+1, i+1
		if start >= len(arms) {
			continue
		}
		if arms[start].text == "{" {
			close := matching(arms, start, "{", "}")
			if close < 0 {
				continue
			}
			statements := gradedResultStatements(arms[start+1 : close])
			for j, statement := range statements {
				if len(statement) == 0 {
					continue
				}
				if statement[0].text == "return" {
					if len(callsIn(statement[1:])) > 0 || hasTransform(statement[1:]) {
						return true
					}
				} else if j == len(statements)-1 && arms[close-1].text != ";" && !gradedResultControl(statement[0].text) {
					assignment := false
					for _, tok := range statement {
						assignment = assignment || tok.text == "=" || tok.text == ":="
					}
					if !assignment && (len(callsIn(statement)) > 0 || hasTransform(statement)) {
						return true
					}
				}
			}
			continue
		}
		depth := 0
		for end < len(arms) {
			v := arms[end].text
			if depth == 0 && v == "," {
				break
			}
			switch v {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
			end++
		}
		if len(callsIn(arms[start:end])) > 0 || hasTransform(arms[start:end]) {
			return true
		}
	}
	return false
}

func gradeAssertion(body []token) bool {
	for i := 0; i+1 < len(body); i++ {
		if (body[i].text == "assert" || body[i].text == "assert_eq") && body[i+1].text == "!" {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func gradeOwnedReceiver(op *operation, u unit, receiver string) bool {
	parts := strings.Split(receiver, ".")
	fields := callerDeclaredFields(u, op.owner)
	for _, part := range parts {
		if _, exists := fields[part]; exists {
			return true
		}
	}
	return false
}

func gradeCommand(op *operation) bool {
	for _, tok := range op.body {
		if tok.text == "return" {
			return false
		}
	}
	// Rust's result type is recorded separately by its inventory.
	if op.language == "rust" && op.returnType != "" && op.returnType != "()" {
		return false
	}
	return true
}

func gradeReceiver(op *operation, receiver string) string {
	// A single borrowed constructor argument retains the owned receiver's
	// identity through a local RAII guard, including tuple-field access.
	dot := strings.IndexByte(receiver, '.')
	local := receiver
	if dot >= 0 {
		local = receiver[:dot]
	}
	for i := 0; i+3 < len(op.body); i++ {
		if op.body[i].text != local || op.body[i+1].text != "=" || op.body[i+3].text != "(" {
			continue
		}
		end := matching(op.body, i+3, "(", ")")
		if end < 0 {
			continue
		}
		arg := op.body[i+4 : end]
		if len(arg) > 0 && arg[0].text == "&" {
			arg = arg[1:]
			if len(arg) > 0 && arg[0].text == "mut" {
				arg = arg[1:]
			}
		}
		if len(arg) == 3 && (arg[0].text == "self" || arg[0].text == op.receiverName) && arg[1].text == "." {
			return arg[0].text + "." + arg[2].text
		}
	}
	return receiver
}

// Preconditions on shared protocol state remain obligations of the caller
// choosing the next primitive stage. Independent data validation and returned
// transformations are retained even when their implementation is delegated.
func protocolCallerDuty(item evidenceItem, u unit, prerequisites map[string]bool) bool {
	switch item.category {
	case "resource", "coordination", "state":
		return true
	case "validation":
		body := item.origin.body
		guards := 0
		for i, tok := range body {
			if tok.text != "if" && tok.text != "assert" && tok.text != "assert_eq" {
				continue
			}
			start := i + 1
			if start < len(body) && body[start].text == "!" {
				start++
			}
			end := start
			if start < len(body) && body[start].text == "(" {
				end = matching(body, start, "(", ")")
			} else {
				for end < len(body) && body[end].text != "{" {
					end++
				}
			}
			if end < start {
				continue
			}
			governed := false
			for j := start; j < end; j++ {
				for _, parameter := range item.origin.paramNames {
					if body[j].text == parameter && (j == 0 || body[j-1].text != ".") {
						return false
					}
				}
				if prerequisites[body[j].text] && gradeOwnedFieldReference(item.origin, u, body, j) {
					governed = true
				}
			}
			if !governed {
				return false
			}
			guards++
		}
		return guards > 0
	}
	return false
}

func gradeConsistentWrites(op *operation, u unit, body []token) bool {
	if op.language == "go" && !gradedMutableGoReceiver(op, u) {
		return false
	}
	for _, constraint := range gradedOwnerConstraints(u, op.owner, u.ops) {
		if constraint.derivedTarget != "" {
			stored := map[string]string{}
			for i, t := range body {
				if t.text != "=" || i == 0 || !gradeOwnedFieldReference(op, u, body, i-1) || !gradedUnconditional(body, i) {
					continue
				}
				rhs := gradedConstraintExpression(op, u, body[i+1:statementEnd(body, i+1)])
				if body[i-1].text == constraint.derivedTarget {
					expectedTokens, _, _ := lex([]byte(constraint.derivedExpression))
					expected := ""
					connected := false
					for _, token := range expectedTokens {
						if replacement, ok := stored[token.text]; ok {
							expected += replacement
							connected = true
						} else {
							expected += token.text
						}
					}
					if connected && rhs == expected && !gradedConstraintChanged(op, u, body, statementEnd(body, i+1), len(body), constraint.fields) {
						return true
					}
				}
				stored[body[i-1].text] = rhs
			}
		}

		if constraint.kind != "index-bounds" || len(constraint.fields) != 2 {
			continue
		}
		for _, target := range constraint.fields {
			for _, data := range constraint.fields {
				if data == target || constraint.indexExpression != target+"-1" {
					continue
				}
				writtenData := false
				for i, t := range body {
					if t.text != "=" || i == 0 || !gradeOwnedFieldReference(op, u, body, i-1) || !gradedUnconditional(body, i) {
						continue
					}
					if body[i-1].text == data {
						writtenData = true
					}
					if body[i-1].text == target && writtenData && gradedConstraintExpression(op, u, body[i+1:statementEnd(body, i+1)]) == "size("+data+")" && !gradedConstraintChanged(op, u, body, statementEnd(body, i+1), len(body), constraint.fields) {
						return true
					}
				}
			}
		}
	}
	return false
}
func gradedMutableGoReceiver(op *operation, u unit) bool {
	if op.language != "go" {
		return true
	}
	for i, t := range u.tokens {
		if t.text != "func" || i+1 >= len(u.tokens) || u.tokens[i+1].text != "(" {
			continue
		}
		close := matching(u.tokens, i+1, "(", ")")
		if close < 0 || close+1 >= len(u.tokens) || u.tokens[close+1].text != op.name {
			continue
		}
		receiver := u.tokens[i+2 : close]
		ownerMatches := false
		pointer := false
		for _, token := range receiver {
			ownerMatches = ownerMatches || token.text == op.owner
			pointer = pointer || token.text == "*"
		}
		if ownerMatches {
			return pointer
		}
	}

	return false
}

func gradeOwnedFieldReference(op *operation, u unit, body []token, index int) bool {
	if index < 0 || index >= len(body) {
		return false
	}
	if op.constraintReceiver != "" {
		return index >= 2 && body[index-1].text == "." && body[index-2].text == op.constraintReceiver && (index < 4 || body[index-3].text != "." || body[index-4].text == "this")
	}
	name := body[index].text
	// Callers have already checked their single field inventory. Do not parse
	// the containing type again for each candidate token.
	if index >= 2 && body[index-1].text == "." {
		receiverIndex := index - 2
		if body[receiverIndex].text == ")" {
			start := gradedStorageReferenceStart(body, index)
			if start < 0 || body[start].text != "(" {
				return false
			}
			receiverIndex = start + 1
			if receiverIndex < len(body) && body[receiverIndex].text == "*" {
				receiverIndex++
			}
			if receiverIndex+1 != index-2 {
				return false
			}
		}
		receiver := body[receiverIndex].text
		return receiver == "this" || receiver == "self" || receiver == op.receiverName || gradeOwnerAlias(op, body, receiver, receiverIndex)
	}
	if op.language != "java" {
		return false
	}
	for _, p := range op.paramNames {
		if p == name {
			return false
		}
	}
	for i := 1; i <= index; i++ {
		if body[i].text != name || !gradedScopeContains(body, i, index) {
			continue
		}
		previous := body[i-1].text
		if previous == "]" && i >= 3 && body[i-2].text == "[" {
			previous = body[i-3].text
		}
		if previous == "let" || previous == "var" || previous == "const" {
			return false
		}
		if i+1 < len(body) && isIdentifier(previous) && previous != "return" && previous != "throw" {
			next := body[i+1].text
			if next == "=" || next == ";" || next == ":" {
				return false
			}
		}
	}
	return true
}
