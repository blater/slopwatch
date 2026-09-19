package sourceestimate

import (
	"strings"
)

// RustRouteFamily is the bounded source projection for one Rust audience and
// receiver surface. It deliberately carries roots rather than treating every
// parsed function as an externally callable route.
type RustRouteFamily struct {
	Grade            *GradedEvidence
	Findings         []Finding
	RecognizedHidden float64
	UncertainBurden  float64
	Limitations      []string
	ID               string
	Audience         string
	Roots            []string
	Burden           float64
	Hidden           float64
}

// RustAttribution is kept separate from the legacy package result so callers
// can attach Rust file projections to the crate boundary they already own.
type RustAttribution struct {
	Results       map[string]Result
	RouteFamilies map[string][]RustRouteFamily
}

const (
	roleSupportingRustType  = "supporting-rust-value-type"
	roleSupportingRustTrait = "supporting-rust-trait-contract"
)

type rustRange struct {
	start, end int
	name       string
	public     bool
}

type rustImplRange struct {
	start, end int
	trait      string
	selfType   string
	modulePub  bool
}

type rustFunction struct {
	start, paramOpen, paramClose, bodyStart, bodyEnd int
	name, owner                                      string
	impl                                             *rustImplRange
	modulePub                                        bool
	pubFn                                            bool
	restricted, testOnly                             bool
	traitPub                                         bool
	selfPub                                          bool
}

// annotateRustAttribution applies Rust visibility and receiver ownership to a
// parsed operation inventory. It is safe to call before the generic estimate
// chooses roots. Public trait implementations remain roots even when their
// representation type is private; called private helpers do not become roots.
func annotateRustAttribution(units []unit) map[string]rustFunction {
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

	for index := range units {
		if normalizeLanguage(units[index].file.Language, units[index].file.Path) == "rust" {
			units[index].ops = rustOperations(units[index].file, units[index].index, units[index].tokens, units[index].pkg, rustFunctions(units[index].tokens))
		}
	}
	// An imported trait is not necessarily private merely because its
	// declaration is outside this bounded source set. A public operation that
	// returns the trait is concrete escape evidence; retain such an impl as an
	// external route while treating private visitor callbacks as helpers.
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
		for _, function := range rustFunctions(u.tokens) {
			if function.impl != nil && function.impl.trait != "" {
				names[function.impl.trait] = true
			}
		}
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
			for trait := range traitNames[u.pkg] {
				if strings.Contains(op.returnType, trait) {
					traitEscapes[u.pkg+"#"+trait] = true
				}
			}
		}
	}
	all := make([]*operation, 0)
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) == "rust" {
			all = append(all, u.ops...)
		}
	}
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
			if info.impl != nil {
				op.owner = info.owner
			}
			traitKey := ""
			if info.impl != nil {
				traitKey = u.pkg + "#" + info.impl.trait
			}
			info.traitPub = info.impl != nil && (traitVisibility[traitKey] || (!knownTraits[traitKey] && traitEscapes[traitKey]))
			info.selfPub = info.impl != nil && typeVisibility[u.pkg+"#"+info.impl.selfType]
			info.modulePub = rustModuleVisible(u.tokens, info.start)
			annotations[op.id] = info

			external := info.modulePub && ((info.impl != nil && info.traitPub) || (info.pubFn && (info.impl == nil || info.selfPub)))
			if info.impl != nil && info.traitPub && info.modulePub {
				// Rust trait methods are callable through a public trait even
				// when the implementing representation is private.
				external = true
			}
			if external {
				op.exposed = true
				op.packageVisible = true
				continue
			}
			privateTrait := info.impl != nil && info.impl.trait != "" && knownTraits[traitKey] && !info.traitPub
			unknownPrivateTrait := info.impl != nil && info.impl.trait != "" && !knownTraits[traitKey] && !info.selfPub && !info.traitPub
			if (info.impl != nil && info.impl.trait == "Drop") || privateTrait || unknownPrivateTrait {
				// A private trait contract cannot be called as an external route.
				// Drop is the language-defined cleanup hook; its implementation is
				// reached implicitly through the owning value's lifecycle. Keep
				// both kinds of private contract method internal. Public traits are
				// deliberately handled above, including implementations on private
				// representations returned behind the public trait.
				op.exposed = false
				op.packageVisible = false
				continue
			}

			// A private/package function is an internal root only when no
			// operation in the bounded source set calls it. This preserves
			// helper extraction and still exposes genuinely independent
			// module work.
			op.exposed = info.restricted || inbound[op.id] == 0 || crossFileInbound[op.id] > 0
			op.packageVisible = op.exposed
		}
	}
	return annotations
}

// AnalyzeRustAttribution returns per-file Rust estimates and route families.
// It is deliberately source-bounded: unresolved macro expansion, cfg choice,
// and ambiguous cross-file calls remain unknown work in the normal estimator.
func AnalyzeRustAttribution(files []File) (map[string]Result, map[string][]RustRouteFamily) {
	result := analyzeRustAttribution(files)
	return result.Results, result.RouteFamilies
}

// AnalyzeRustWithAttribution is the structured form used by native callers.
func AnalyzeRustWithAttribution(files []File) RustAttribution {
	return analyzeRustAttribution(files)
}

func analyzeRustAttribution(files []File) RustAttribution {
	units := rustUnits(files)
	annotations := annotateRustAttribution(units)
	byKey := operationIndex(units)
	annotateSourceSurfaceInputs(units, byKey)
	annotateOutputObligations(units, byKey)
	return analyzeRustUnits(units, byKey, annotations)
}

// analyzeRustUnits consumes the shared parsed inventory. Native callers can
// annotate once, build their common operation index once, and then pass both
// here without reparsing or creating a second Rust graph.
func analyzeRustUnits(units []unit, byKey map[string][]*operation, annotations map[string]rustFunction) RustAttribution {
	if byKey == nil {
		byKey = operationIndex(units)
	}
	supporting := supportingRustOwners(units)
	results := make(map[string]Result)
	families := make(map[string][]RustRouteFamily)
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "rust" {
			continue
		}
		roots := make([]*operation, 0, len(u.ops))
		hasSupporting := false
		for _, op := range u.ops {
			if op.exposed {
				if _, ok := supporting[attributionOwner(op)]; ok {
					hasSupporting = true
					continue
				}
				roots = append(roots, op)
			}
		}
		if len(roots) == 0 && (rustSupportingOnly(u.tokens) || hasSupporting) {
			role, _ := rustSupportingTypeProof(u.tokens)
			if role == "" {
				role = roleSupportingRustType
			}
			supportingRoles := map[string]string{}
			for _, op := range u.ops {
				if roleName, ok := supporting[attributionOwner(op)]; ok {
					supportingRoles[op.owner] = roleName
				}
			}
			results[u.file.Path] = Result{Applicable: false, Roles: []string{role}, Supporting: supportingRoles, RoleOnly: true, NoAbstractionProven: false}
			families[u.file.Path] = nil
			continue
		}
		var projection Result
		haveProjection := false
		supportingRoles := map[string]string{}
		for _, op := range u.ops {
			if roleName, ok := supporting[attributionOwner(op)]; ok {
				supportingRoles[op.owner] = roleName
			}
		}
		groups := map[string][]*operation{}
		groupOrder := make([]string, 0, len(roots))
		for _, root := range roots {
			audience := rustAudience(root, annotations)
			name := root.owner
			if name == "" {
				name = root.name
			}
			id := u.file.Path + "#" + audience + ":" + name
			if _, ok := groups[id]; !ok {
				groupOrder = append(groupOrder, id)
			}
			groups[id] = append(groups[id], root)
		}
		for _, id := range groupOrder {
			groupRoots := groups[id]
			first := groupRoots[0]
			audience := rustAudience(first, annotations)
			groupProjection := estimateRoots(u.index, u, units, byKey, groupRoots)
			if !haveProjection || largerProjection(groupProjection, projection) {
				projection = groupProjection
				haveProjection = true
			}
			family := RustRouteFamily{Grade: groupProjection.Grade, ID: id, Audience: audience, Burden: 0, Hidden: groupProjection.Hidden, Findings: groupProjection.Findings, RecognizedHidden: groupProjection.RecognizedHidden(), UncertainBurden: groupProjection.Assessment().UncertainBurden, Limitations: groupProjection.Limitations}
			for _, root := range groupRoots {
				family.Roots = append(family.Roots, root.id)
				family.Burden += 1 + 0.25*float64(root.params)
			}
			families[u.file.Path] = append(families[u.file.Path], family)
		}
		for _, family := range families[u.file.Path] {
			projection.Abstractions = append(projection.Abstractions, Abstraction{Grade: family.Grade, Name: family.ID, Audience: family.Audience, Burden: family.Burden, Hidden: family.Hidden, Findings: family.Findings, RecognizedHidden: family.RecognizedHidden, UncertainBurden: family.UncertainBurden, Limitations: family.Limitations})
		}
		for owner, roleName := range supportingRoles {
			projection.Roles = appendUniqueString(projection.Roles, roleName)
			if projection.Supporting == nil {
				projection.Supporting = map[string]string{}
			}
			projection.Supporting[owner] = roleName
		}
		results[u.file.Path] = projection
	}
	return RustAttribution{Results: results, RouteFamilies: families}
}

func rustUnits(files []File) []unit {
	units := make([]unit, 0, len(files))
	for index, file := range files {
		if normalizeLanguage(file.Language, file.Path) != "rust" {
			continue
		}
		tokens, limited, lexicallyValid := lex(file.Source)
		pkg := packageName(file.Language, tokens)
		u := unit{index: index, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: lexicallyValid}
		u.ops = findOperations(file, index, tokens, pkg)
		units = append(units, u)
	}
	return units
}

func rustAudience(op *operation, annotations map[string]rustFunction) string {
	if info, ok := annotations[op.id]; ok {
		if info.modulePub && ((info.impl != nil && info.traitPub) || (info.pubFn && (info.selfPub || info.impl == nil))) {
			return "external"
		}
	}
	return "internal"
}

func operationIndex(units []unit) map[string][]*operation {
	index := make(map[string][]*operation)
	for _, u := range units {
		for _, op := range u.ops {
			indexOperation(index, op)
		}
	}
	return index
}

func rustCallIndex(all []*operation) map[string][]*operation {
	index := make(map[string][]*operation, len(all))
	for _, op := range all {
		index[rustCallKey(op.pkg, op.name, op.owner)] = append(index[rustCallKey(op.pkg, op.name, op.owner)], op)
		index[rustWorkspaceCallKey(op.pkg, op.name, op.owner)] = append(index[rustWorkspaceCallKey(op.pkg, op.name, op.owner)], op)
	}
	return index
}

func rustCallKey(pkg, name, owner string) string {
	return pkg + "#" + owner + "#" + name
}

func rustWorkspaceCallKey(pkg, name, owner string) string {
	return "workspace:rust#" + owner + "#" + name
}

func rustResolveCall(caller *operation, call call, index map[string][]*operation) []*operation {
	name := call.name
	owner := caller.owner
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		owner, name = name[:dot], name[dot+1:]
	} else if separator := strings.LastIndex(name, "::"); separator >= 0 {
		owner, name = name[:separator], name[separator+2:]
	}
	if owner == "" {
		owner = caller.owner
	}
	matches := index[rustCallKey(caller.pkg, name, owner)]
	if len(matches) == 0 && owner != "" {
		matches = index[rustCallKey(caller.pkg, name, "")]
	}
	if len(matches) == 0 && owner != "" {
		if receiverType, ok := caller.fieldTypes[owner]; ok {
			owner = receiverType
		} else if dot := strings.LastIndexByte(owner, '.'); dot >= 0 {
			if receiverType, ok := caller.fieldTypes[owner[dot+1:]]; ok {
				owner = receiverType
			}
		}
		for _, candidate := range index[rustWorkspaceCallKey(caller.pkg, name, owner)] {
			if candidate.file != caller.file && candidate.owner == owner && candidate.exposed {
				matches = append(matches, candidate)
			}
		}
	}
	if len(matches) == 1 {
		return matches
	}
	return nil
}

func rustOperations(file File, index int, tokens []token, pkg string, functions []rustFunction) []*operation {
	fields := sourceFieldTypes(tokens)
	result := make([]*operation, 0, len(functions))
	for number, function := range functions {
		if function.paramOpen < 0 || function.paramClose < function.paramOpen || function.bodyEnd <= function.bodyStart {
			continue
		}
		parameters := tokens[function.paramOpen+1 : function.paramClose]
		returnType := rustReturnType(tokens[function.paramClose+1 : function.bodyStart])
		result = append(result, &operation{
			id: file.Path + "#" + function.name + "/" + itoa(number), name: function.name,
			owner: function.owner, language: "rust", pkg: pkg, file: index,
			params: rustParameterCount(parameters), paramNames: parameterNames(parameters, "rust"),
			requiredParamNames: requiredParameterNames(parameters, "rust"),
			stringParams:       stringParameterNames(parameters, "rust"), fieldTypes: fields,
			exposed: function.pubFn, packageVisible: function.pubFn, testOnly: function.testOnly, parameterTypes: gradedParameterTypes(parameters, "rust"),
			returnType: returnType,
			body:       tokens[function.bodyStart+1 : function.bodyEnd],
		})
	}
	return result
}

// rustReturnType records the declared result type for command/query
// sequencing. The bounded parser keeps only the signature span, stopping at
// a where-clause so generic constraints do not become part of the type name.
func rustReturnType(signature []token) string {
	for i, item := range signature {
		if item.text != "->" || i+1 >= len(signature) {
			continue
		}
		end := len(signature)
		for j := i + 1; j < len(signature); j++ {
			if signature[j].text == "where" {
				end = j
				break
			}
		}
		if end <= i+1 {
			return ""
		}
		return joinTokens(signature[i+1 : end])
	}
	return ""
}

// rustParameterCount measures caller supplied inputs. A method receiver is
// an implementation context, not an additional input at the public boundary.
func rustParameterCount(tokens []token) int {
	count := 0
	for _, part := range splitParameterDeclarations(tokens) {
		receiver := false
		for _, item := range part {
			if item.text == "self" {
				receiver = true
				break
			}
		}
		if !receiver {
			count++
		}
	}
	return count
}

func rustFunctions(tokens []token) []rustFunction {
	result := make([]rustFunction, 0, 8)
	impls := rustImplRanges(tokens)
	types := rustNamedRanges(tokens, "struct")
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "fn" || i+1 >= len(tokens) || !isIdentifier(tokens[i+1].text) {
			continue
		}
		j := i + 2
		if j < len(tokens) && tokens[j].text == "<" {
			end := matching(tokens, j, "<", ">")
			if end < 0 {
				continue
			}
			j = end + 1
		}
		if j >= len(tokens) || tokens[j].text != "(" {
			continue
		}
		close := matching(tokens, j, "(", ")")
		if close < 0 {
			continue
		}
		bodyStart := close + 1
		for bodyStart < len(tokens) && tokens[bodyStart].text != "{" && tokens[bodyStart].text != "=>" && tokens[bodyStart].text != ";" {
			bodyStart++
		}
		if bodyStart >= len(tokens) || tokens[bodyStart].text != "{" {
			continue
		}
		bodyEnd := matching(tokens, bodyStart, "{", "}")
		if bodyEnd < 0 {
			continue
		}
		info := rustFunction{start: i, paramOpen: j, paramClose: close, bodyStart: bodyStart, bodyEnd: bodyEnd, name: tokens[i+1].text,
			pubFn: rustBarePubBefore(tokens, i), restricted: rustRestrictedBefore(tokens, i), testOnly: rustTestOnly(tokens, i), modulePub: rustModuleVisible(tokens, i)}
		for index := range impls {
			if i > impls[index].start && i < impls[index].end {
				info.impl = &impls[index]
				info.owner = impls[index].selfType
				break
			}
		}
		for _, declaration := range types {
			if info.owner == declaration.name {
				info.selfPub = declaration.public
				break
			}
		}
		if info.impl != nil {
			info.traitPub = rustTraitPublic(tokens, info.impl.trait)
		}
		result = append(result, info)
		i = bodyEnd
	}
	return result
}

func rustImplRanges(tokens []token) []rustImplRange {
	result := make([]rustImplRange, 0, 4)
	for i := 0; i < len(tokens); i++ {
		if tokens[i].text != "impl" {
			continue
		}
		bodyStart := i + 1
		angle, paren, bracket := 0, 0, 0
		for bodyStart < len(tokens) {
			text := tokens[bodyStart].text
			if text == "{" && angle == 0 && paren == 0 && bracket == 0 {
				break
			}
			switch text {
			case "<":
				angle++
			case ">":
				if angle > 0 {
					angle--
				}
			case ">>":
				angle = maxInt(0, angle-2)
			case "(":
				paren++
			case ")":
				paren--
			case "[":
				bracket++
			case "]":
				bracket--
			}
			bodyStart++
		}
		if bodyStart >= len(tokens) {
			continue
		}
		bodyEnd := matching(tokens, bodyStart, "{", "}")
		if bodyEnd < 0 {
			continue
		}
		header := tokens[i+1 : bodyStart]
		traitName, selfType := rustImplIdentity(header)
		result = append(result, rustImplRange{start: i, end: bodyEnd, trait: traitName, selfType: selfType, modulePub: rustModuleVisible(tokens, i)})
		i = bodyEnd
	}
	return result
}

func rustNamedRanges(tokens []token, keyword string) []rustRange {
	result := make([]rustRange, 0, 4)
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != keyword || !isIdentifier(tokens[i+1].text) {
			continue
		}
		name := tokens[i+1].text
		public := rustBarePubBefore(tokens, i) && rustModuleVisible(tokens, i)
		end := i + 2
		for end < len(tokens) && tokens[end].text != "{" && tokens[end].text != ";" {
			end++
		}
		if end < len(tokens) && tokens[end].text == "{" {
			if close := matching(tokens, end, "{", "}"); close >= 0 {
				result = append(result, rustRange{start: i, end: close, name: name, public: public})
				i = close
			}
		} else {
			result = append(result, rustRange{start: i, end: end, name: name, public: public})
		}
	}
	return result
}

// Impl generic binders introduce names; they are not the implemented type.
// Split only top-level `for` before `where`, ignoring nested bounds/HRTBs.
func rustImplIdentity(header []token) (traitName, selfType string) {
	if len(header) > 0 && header[0].text == "<" {
		end := rustGenericEnd(header, 0)
		if end < 0 {
			return "", ""
		}
		header = header[end+1:]
	}
	depth := 0
	split := -1
	for i, t := range header {
		switch t.text {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		}
		if depth == 0 && t.text == "where" {
			header = header[:i]
			break
		}
		if depth == 0 && t.text == "for" {
			split = i
		}
	}
	if split >= 0 && split < len(header) {
		return rustImplPathName(header[:split]), rustImplPathName(header[split+1:])
	}
	return "", rustImplPathName(header)
}
func rustGenericEnd(tokens []token, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].text {
		case "<":
			depth++
		case ">":
			depth--
		case ">>":
			depth -= 2
		}
		if depth <= 0 {
			return i
		}
	}
	return -1
}

// Preserve the terminal nominal type/trait identity through qualified paths and
// generic arguments. Lifetimes and reference qualifiers never become owners.
func rustImplPathName(tokens []token) string {
	i := 0
	for i < len(tokens) {
		switch tokens[i].text {
		case "&", "*", "mut", "const", "!":
			i++
		case "'":
			i += 2
		default:
			goto path
		}
	}
path:
	if i < len(tokens) && tokens[i].text == "::" {
		i++
	}
	if i >= len(tokens) || !isIdentifier(tokens[i].text) || tokens[i].text == "fn" {
		return ""
	}
	name := tokens[i].text
	i++
	for i < len(tokens) {
		if tokens[i].text == "<" {
			end := rustGenericEnd(tokens, i)
			if end < 0 {
				return ""
			}
			i = end + 1
			continue
		}
		if tokens[i].text == "::" && i+1 < len(tokens) && isIdentifier(tokens[i+1].text) {
			name = tokens[i+1].text
			i += 2
			continue
		}
		return ""
	}
	if name == "self" || name == "crate" || name == "super" {
		return ""
	}
	return name
}

func rustTraitPublic(tokens []token, name string) bool {
	if name == "" {
		return false
	}
	for _, declaration := range rustNamedRanges(tokens, "trait") {
		if declaration.name == name && declaration.public {
			return true
		}
	}
	return false
}

func rustBarePubBefore(tokens []token, index int) bool {
	start := index - 1
	for start >= 0 && tokens[start].text != "{" && tokens[start].text != "}" && tokens[start].text != ";" {
		start--
	}
	for i := start + 1; i < index; i++ {
		if tokens[i].text != "pub" {
			continue
		}
		if i+1 < index && tokens[i+1].text == "(" {
			continue
		}
		return true
	}
	return false
}

func rustModuleVisible(tokens []token, index int) bool {
	visible := true
	for i := 0; i+1 < index; i++ {
		if tokens[i].text != "mod" || !isIdentifier(tokens[i+1].text) {
			continue
		}
		body := i + 2
		for body < index && tokens[body].text != "{" && tokens[body].text != ";" {
			body++
		}
		if body >= index || tokens[body].text != "{" {
			continue
		}
		end := matching(tokens, body, "{", "}")
		if end < index {
			continue
		}
		if !rustBarePubBefore(tokens, i) {
			visible = false
		}
	}
	return visible
}

func rustSupportingOnly(tokens []token) bool {
	if len(tokens) == 0 || hasRustUncertainSyntax(tokens) {
		return false
	}
	return len(rustNamedRanges(tokens, "struct")) > 0 || len(rustNamedRanges(tokens, "enum")) > 0 || len(rustNamedRanges(tokens, "trait")) > 0
}

// rustSupportingTypeProof is the small structural proof shared by callers
// that need to keep value/trait files in inventory without treating them as
// behavioral roots.
func rustSupportingTypeProof(tokens []token) (string, bool) {
	if !rustSupportingOnly(tokens) {
		return "", false
	}
	if rustTraitOnly(tokens) {
		return roleSupportingRustTrait, true
	}
	return roleSupportingRustType, true
}

// supportingRustOwners proves only the affected receiver/type owner. A
// passive representation beside a service therefore cannot erase or inflate
// the service root. Trait implementations are intentionally excluded because
// their methods are behavior surfaces even when their bodies look like simple
// getters.
func supportingRustOwners(units []unit) map[attributionOwnerKey]string {
	result := map[attributionOwnerKey]string{}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "rust" || u.limited || !u.lexicallyValid {
			continue
		}
		shapes := rustNamedRanges(u.tokens, "struct")
		for _, shape := range shapes {
			if !rustStructHasFields(u.tokens, shape) {
				continue
			}
			var ownerOps []*operation
			for _, op := range u.ops {
				if op.owner != shape.name {
					continue
				}
				ownerOps = append(ownerOps, op)
			}
			if len(ownerOps) == 0 {
				continue
			}
			passive := true
			for _, op := range ownerOps {
				if !rustPassiveOperation(op.body) {
					passive = false
					break
				}
			}
			if passive {
				result[attributionOwnerKey{file: u.index, owner: shape.name}] = roleSupportingRustType
			}
		}
	}
	return result
}

func rustStructHasFields(tokens []token, shape rustRange) bool {
	if shape.end <= shape.start || shape.end >= len(tokens) {
		return false
	}
	open := shape.start + 2
	for open < shape.end && tokens[open].text != "{" {
		open++
	}
	if open >= shape.end {
		return false
	}
	for i := open + 1; i+1 < shape.end; i++ {
		if isIdentifier(tokens[i].text) && (tokens[i+1].text == ":" || tokens[i+1].text == ",") {
			return true
		}
	}
	return false
}

func rustPassiveOperation(body []token) bool {
	body = trimSemicolonTokens(body)
	if len(body) == 0 || gradeAssertion(body) || hasValidation(body) || hasTransform(body) || hasState(body) || len(callsIn(body)) != 0 {
		return false
	}
	// A constructor that only materializes the receiver's fields is part of a
	// passive value carrier. Keep the check deliberately narrow: computed
	// expressions and control flow belong to behavioral abstractions, while a
	// plain `Self { field: input }` literal is just representation transport.
	if len(body) >= 3 && body[0].text == "Self" && body[1].text == "{" {
		for _, item := range body[2:] {
			switch item.text {
			case "+", "-", "*", "/", "%", "&&", "||", "if", "match", "for", "while", "loop":
				return false
			}
		}
		return true
	}
	for i := 0; i+2 < len(body); i++ {
		if body[i].text == "self" && body[i+1].text == "." && isIdentifier(body[i+2].text) {
			return (i >= 1 && body[i-1].text == "return") || i == 0
		}
	}
	return false
}

func rustTraitOnly(tokens []token) bool {
	return len(rustNamedRanges(tokens, "trait")) > 0 && len(rustNamedRanges(tokens, "struct")) == 0 && len(rustNamedRanges(tokens, "enum")) == 0
}

func hasRustUncertainSyntax(tokens []token) bool {
	for _, item := range tokens {
		if item.text == "#" || item.text == "macro_rules" || item.text == "cfg" {
			return true
		}
	}
	return false
}

func rustRestrictedBefore(tokens []token, index int) bool {
	for i := index - 1; i >= 0 && tokens[i].text != "{" && tokens[i].text != "}" && tokens[i].text != ";"; i-- {
		if tokens[i].text == "pub" && i+1 < index && tokens[i+1].text == "(" {
			return true
		}
	}
	return false
}

// Only explicit test attributes exclude code; arbitrary cfg conditions retain
// their production uncertainty. Attribute scope includes an enclosing test mod.
func rustTestOnly(tokens []token, index int) bool {
	for i := 0; i+2 < index; i++ {
		if tokens[i].text != "#" || tokens[i+1].text != "[" {
			continue
		}
		end := matching(tokens, i+1, "[", "]")
		if end < 0 || end >= index {
			continue
		}
		attr := joinTokens(tokens[i+2 : end])
		if attr != "test" && attr != "cfg(test)" {
			continue
		}
		start := end + 1
		for start < len(tokens) && tokens[start].text != "{" && tokens[start].text != ";" {
			start++
		}
		if start < len(tokens) && tokens[start].text == "{" {
			close := matching(tokens, start, "{", "}")
			if index < close {
				return true
			}
		}
	}
	return false
}
