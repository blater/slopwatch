// Package sourceestimate provides a small, bounded source-only estimate for
// SHALLOW when a language adapter cannot establish a complete semantic proof.
package sourceestimate

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// File is one source unit. Source may be incomplete or use unresolved types.
type File struct {
	Path     string
	Language string
	Source   []byte
}

// Result is deliberately independent of the report model. Burden and Hidden
// are units for the caller's 100*B/(B+2H) projection.
type Result struct {
	Grade           *GradedEvidence
	Findings        []Finding
	UncertainBurden float64
	Burden          float64
	Hidden          float64
	Applicable      bool
	// Roles records bounded structural roles that are orthogonal to the
	// behavioral estimate, such as an error representation getter.
	Roles        []string
	RoleOnly     bool
	Abstractions []Abstraction
	Supporting   map[string]string
	// NoAbstractionProven is true only when this source is explicitly bounded
	// to contain no callable abstraction. An empty result is intentionally not
	// enough; callers must distinguish absence proof from parser failure.
	NoAbstractionProven bool
	Categories          map[string]float64
	Limitations         []string
	Dependencies        []string
	evidence            map[string]float64
}

// Abstraction preserves the bounded projections considered for one file.
// Burden/Hidden on Result remain the strongest meaningful file projection.
type Abstraction struct {
	Grade            *GradedEvidence
	Findings         []Finding
	RecognizedHidden float64
	UncertainBurden  float64
	Limitations      []string
	Name             string
	Audience         string
	Burden           float64
	Hidden           float64
}

type token struct {
	text, kind, literal string
	offset, line        int
}

type operation struct {
	constraintReceiver             string
	findings                       []Finding
	id, name, owner, language, pkg string
	receiverName, returnType       string
	file                           int
	params                         int
	paramNames                     []string
	requiredParamNames             []string
	stringParams                   map[string]bool
	fieldTypes                     map[string]string
	imports                        map[string]sourceImport
	exposed                        bool
	packageVisible                 bool
	body                           []token
	compactConstructor             bool
	expressionBody                 bool
	testOnly                       bool
	parameterTypes                 map[string]string
	outputBuffers                  []string
	outputAssessed                 bool
}

type unit struct {
	shadowedBuiltins  map[string]bool
	calibration       CalibrationProfile
	index             int
	file              File
	tokens            []token
	pkg               string
	ops               []*operation
	limited           bool
	lexicallyValid    bool
	callerObligations map[string]gradedCallerObligation
	packageTypes      map[string]bool
}

const (
	maxTokensPerFile     = 20000
	maxOperationsPerFile = 512
	maxCallDepth         = 4
	maxCallsPerRoot      = 64
)

const roleSupportingErrorRepresentation = "supporting-error-representation"
const roleSupportingDataRepresentation = "supporting-data-representation"

type goMethodIndex struct {
	all, exported        map[string][]*operation
	supporting           map[string]string
	supportingOperations map[string]string
}

func indexGoMethods(units []unit) goMethodIndex {
	index := goMethodIndex{
		all:        map[string][]*operation{},
		exported:   map[string][]*operation{},
		supporting: map[string]string{}, supportingOperations: map[string]string{},
	}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "go" {
			continue
		}
		for _, op := range unit.ops {
			if op.owner == "" {
				continue
			}
			key := goMethodGroupKey(op)
			index.all[key] = append(index.all[key], op)
			if op.exposed {
				index.exported[key] = append(index.exported[key], op)
			}
		}
	}
	fields := goDeclaredFields(units)
	stringFields := goStringFields(units)
	for key, methods := range index.all {
		if len(methods) != 1 || len(index.exported[key]) != 1 {
			continue
		}
		op := methods[0]
		if !unexportedGoName(op.owner) || !fields[goTypeFieldKey(op, returnedField(op))] {
			continue
		}
		if op.name == "Error" && op.returnType == "string" && op.params == 0 && pureStringFieldReturn(op) && stringFields[goTypeFieldKey(op, returnedField(op))] {
			index.supporting[key] = roleSupportingErrorRepresentation
		} else if op.name != "Error" && op.params == 0 && pureStringFieldReturn(op) {
			index.supporting[key] = roleSupportingDataRepresentation
		}
	}
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) != "go" {
			continue
		}
		for _, op := range u.ops {
			if op.owner != "" {
				continue
			}
			body := trimSemicolonTokens(op.body)
			if len(body) < 5 || body[0].text != "return" {
				continue
			}
			start := 1
			if body[start].text == "&" {
				start++
			}
			if start+2 >= len(body) || body[start+1].text != "{" || body[len(body)-1].text != "}" {
				continue
			}
			role := index.supporting[op.pkg+"#"+body[start].text]
			if role == "" {
				continue
			}
			params := map[string]bool{}
			for _, name := range op.paramNames {
				params[name] = true
			}
			pure := true
			for i := start + 2; i < len(body)-1; i++ {
				item := body[i]
				if item.text == "," || item.text == ":" {
					continue
				}
				if i+1 < len(body) && body[i+1].text == ":" && isIdentifier(item.text) {
					continue
				}
				if params[item.text] || item.kind == "string" || item.kind == "number" || item.text == "nil" || item.text == "true" || item.text == "false" {
					continue
				}
				pure = false
				break
			}
			if pure {
				index.supportingOperations[op.id] = role
			}
		}
	}
	return index
}

func unexportedGoName(name string) bool {
	if name == "" {
		return false
	}
	return unicode.IsLower(rune(name[0]))
}

func goTypeFieldKey(op *operation, field string) string {
	return op.pkg + "#" + op.owner + "." + field
}

func returnedField(op *operation) string {
	body := trimSemicolonTokens(op.body)
	if len(body) == 4 && body[0].text == "return" && body[1].text == op.receiverName && body[2].text == "." && isIdentifier(body[3].text) {
		return body[3].text
	}
	return ""
}

func pureStringFieldReturn(op *operation) bool { return returnedField(op) != "" }

func trimSemicolonTokens(body []token) []token {
	for len(body) > 0 && body[len(body)-1].text == ";" {
		body = body[:len(body)-1]
	}
	return body
}

func goStringFields(units []unit) map[string]bool {
	fields := map[string]bool{}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "go" {
			continue
		}
		tokens := unit.tokens
		for i := 0; i+3 < len(tokens); i++ {
			if tokens[i].text != "type" || !isIdentifier(tokens[i+1].text) || tokens[i+2].text != "struct" || tokens[i+3].text != "{" {
				continue
			}
			end := matching(tokens, i+3, "{", "}")
			if end < 0 {
				continue
			}
			owner := tokens[i+1].text
			for j := i + 4; j+1 < end; j++ {
				if isIdentifier(tokens[j].text) && tokens[j+1].text == "string" {
					fields[unit.pkg+"#"+owner+"."+tokens[j].text] = true
				}
			}
			i = end
		}
	}
	return fields
}

func goDeclaredFields(units []unit) map[string]bool {
	fields := map[string]bool{}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "go" {
			continue
		}
		for i := 0; i+3 < len(unit.tokens); i++ {
			if unit.tokens[i].text != "type" || !isIdentifier(unit.tokens[i+1].text) || unit.tokens[i+2].text != "struct" || unit.tokens[i+3].text != "{" {
				continue
			}
			end := matching(unit.tokens, i+3, "{", "}")
			if end < 0 {
				continue
			}
			for j := i + 4; j+1 < end; j++ {
				if isIdentifier(unit.tokens[j].text) && (isIdentifier(unit.tokens[j+1].text) || unit.tokens[j+1].text == "*") {
					fields[unit.pkg+"#"+unit.tokens[i+1].text+"."+unit.tokens[j].text] = true
				}
			}
			i = end
		}
	}
	return fields
}

// Analyze parses each file once and computes the package-rooted estimate used
// by the existing source fallback.
func Analyze(files []File) map[string]Result {
	packageResults, _ := analyzeWithAttribution(files, true)
	return packageResults
}

// AnalyzeGoFiles returns per-file Go projections while retaining one shared
// operation graph for helper resolution. Non-Go inputs are ignored.
func AnalyzeGoFiles(files []File) map[string]Result {
	_, fileResults := analyzeWithAttribution(files, false)
	return fileResults
}

// AnalyzeWithAttribution computes non-Go legacy package-rooted estimates and,
// for Go inputs, per-file projections from the same parsed operation graph.
// Go callers should use the second map: the first map intentionally omits Go
// package projections so native attribution does not repeat that work.
func AnalyzeWithAttribution(files []File) (map[string]Result, map[string]Result) {
	return analyzeWithAttribution(files, false)
}

func analyzeWithAttribution(files []File, includeGoPackage bool, profiles ...CalibrationProfile) (map[string]Result, map[string]Result) {
	profile := DefaultCalibration()
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	units := make([]unit, len(files))
	all := make([]*operation, 0)
	for i, file := range files {
		tokens, limited, lexicallyValid := lex(file.Source)
		pkg := packageName(file.Language, tokens)
		if normalizeLanguage(file.Language, file.Path) == "go" {
			pkg = filepath.ToSlash(filepath.Dir(file.Path)) + "@" + pkg
		}
		units[i] = unit{calibration: profile, index: i, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: lexicallyValid}
		units[i].ops = findOperations(file, i, tokens, units[i].pkg)
		hasExternal := false
		for _, op := range units[i].ops {
			hasExternal = hasExternal || op.exposed
		}
		if !hasExternal && (normalizeLanguage(file.Language, file.Path) == "java" || normalizeLanguage(file.Language, file.Path) == "rust") {
			for _, op := range units[i].ops {
				op.exposed = op.packageVisible
			}
		}
		if len(units[i].ops) >= maxOperationsPerFile {
			units[i].limited = true
		}
	}
	rustAnnotations := annotateRustAttribution(units)
	annotateTypeScriptImports(units)
	for _, u := range units {
		all = append(all, u.ops...)
	}
	byKey := make(map[string][]*operation, len(all))
	for _, op := range all {
		indexOperation(byKey, op)
	}
	annotateGoUnusedInputs(units)
	annotateSourceSurfaceInputs(units, byKey)
	annotateCallerObligations(units)
	annotateConstraintWitnesses(units)
	annotateOutputObligations(units, byKey)
	goMethods := indexGoMethods(units)
	goGraph := goCallGraph(units, byKey)
	packageResults := make(map[string]Result, len(files))
	for index, unit := range units {
		if includeGoPackage {
			packageResults[unit.file.Path] = estimateUnit(index, unit, units, byKey, goMethods.supporting)
		}
	}
	if includeGoPackage {
		return packageResults, nil
	}
	fileResults := estimateGoAttribution(units, byKey, goMethods, goGraph)
	for path, result := range analyzeTypeScriptUnits(units, byKey) {
		fileResults[path] = result
	}
	for path, result := range analyzeJavaUnits(units, byKey) {
		fileResults[path] = result
	}
	rustResults := analyzeRustUnits(units, byKey, rustAnnotations)
	for path, result := range rustResults.Results {
		fileResults[path] = result
	}
	return packageResults, fileResults
}

func estimateUnit(index int, u unit, units []unit, byKey map[string][]*operation, supporting map[string]string) Result {
	roots := make([]*operation, 0, len(u.ops))
	for _, op := range u.ops {
		if op.exposed && supporting[goMethodGroupKey(op)] == "" {
			roots = append(roots, op)
		}
	}
	result := estimateRoots(index, u, units, byKey, roots)
	for _, op := range u.ops {
		if op.exposed && supporting[goMethodGroupKey(op)] != "" {
			role := supporting[goMethodGroupKey(op)]
			result.Roles = appendUniqueString(result.Roles, role)
			if result.Supporting == nil {
				result.Supporting = map[string]string{}
			}
			result.Supporting[op.owner] = role
		}
	}
	if len(result.Roles) > 0 && len(roots) == 0 {
		result.Applicable, result.Burden, result.Hidden = false, 0, 0
		result.RoleOnly = true
	}
	return result
}

func estimateRoots(index int, u unit, units []unit, byKey map[string][]*operation, roots []*operation) Result {
	r := Result{Categories: map[string]float64{}, evidence: map[string]float64{}}
	seen := map[string]bool{}
	evidenceSeen := map[string]bool{}
	gradedEvidence := []evidenceItem{}
	if !u.lexicallyValid {
		r.Limitations = append(r.Limitations, "invalid_source_syntax")
	}
	if u.limited {
		r.Limitations = append(r.Limitations, "token_limit")
	}
	for _, op := range roots {
		r.Applicable = true
		r.Findings = append(r.Findings, op.findings...)
		r.Burden += 1 + 0.25*float64(op.params)
		budget := maxCallsPerRoot
		state := measureOperation(op, units, byKey, seen, 0, nil, &budget)
		if len(state.limitations) > 0 || u.limited || !u.lexicallyValid {
			r.UncertainBurden += 1 + 0.25*float64(op.params)
		}
		if key, transformed, _ := returnedTransformation(op, units, byKey); transformed {
			state.evidence = append(state.evidence, evidenceItem{category: "transform", key: "transform|" + key})
		}
		gradedEvidence = append(gradedEvidence, state.evidence...)
		for _, item := range state.evidence {
			if item.category == "storage_snapshot" {
				continue
			}
			key := item.key
			if item.category == "unknown_call" || item.category == "unknown_outcome" {
				key = item.category
			}
			if evidenceSeen[key] {
				continue
			}
			evidenceSeen[key] = true
			weight := categoryWeight(item.category)
			r.Categories[item.category] += weight
			r.Hidden += weight
			r.evidence[key] = weight
		}
		r.Dependencies = append(r.Dependencies, state.dependencies...)
		r.Limitations = append(r.Limitations, state.limitations...)
	}
	for _, obligation := range u.callerObligations {
		if len(obligation.fields) > 0 && len(obligation.constraints) > 0 {
			r.Applicable = true
		}
	}
	if !r.Applicable && (unresolvedSurface(u.tokens) || u.file.Language == "java" && javaUninventoriedInitializer(u.tokens)) {
		r.Applicable = true
		r.Burden = 1
		r.Hidden = 1
		r.Categories["unknown_outcome"] = 1
		r.UncertainBurden = r.Burden
		r.Limitations = append(r.Limitations, "unresolved_source_surface")
	}
	sort.Strings(r.Dependencies)
	r.Dependencies = unique(r.Dependencies)
	sort.Strings(r.Limitations)
	r.Limitations = unique(r.Limitations)
	if !r.Applicable {
		r.Burden = 0
		r.Hidden = 0
		r.NoAbstractionProven = noAbstractionProven(u)
	}
	r.Grade = assessGradedEvidence(u, roots, units, byKey, gradedEvidence, r)
	return r
}

// An interface can contain executable lambda/constructor initializers even
// when it has no method body. Lack of a callable root cannot prove N/A there.
func javaUninventoriedInitializer(tokens []token) bool {
	for i, tok := range tokens {
		if tok.text == "new" || tok.text == "->" || tok.text == "-" && i+1 < len(tokens) && tokens[i+1].text == ">" {
			return true
		}
	}
	return false
}

func estimateGoAttribution(units []unit, byKey map[string][]*operation, goMethods goMethodIndex, graph map[string][]*operation) map[string]Result {
	methodProjections := make(map[string]Result, len(goMethods.exported))
	groupKeys := make([]string, 0, len(goMethods.exported))
	for key := range goMethods.exported {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)
	for _, owner := range groupKeys {
		methods := goMethods.exported[owner]
		if len(methods) == 0 || methods[0].file < 0 || methods[0].file >= len(units) {
			continue
		}
		methodUnit := units[methods[0].file]
		methodProjections[owner] = estimateRoots(methods[0].file, methodUnit, units, byKey, methods)
	}
	inbound := buildGoInboundIndex(units, graph)
	results := make(map[string]Result)
	for index, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != "go" {
			continue
		}
		var best Result
		haveBest := false
		abstractions := make([]Abstraction, 0, 3)
		free := make([]*operation, 0)
		owners := map[string]bool{}
		for _, op := range unit.ops {
			if op.owner == "" {
				if op.exposed {
					free = append(free, op)
				}
			} else if goSupportingRole(goMethods, op) == "" {
				// A private method still belongs to the receiver's abstraction.
				// If that receiver has exported methods elsewhere, use the
				// cached full-surface projection for this file as well.
				owners[op.owner] = true
			}
		}
		ownerNames := make([]string, 0, len(owners))
		for owner := range owners {
			ownerNames = append(ownerNames, owner)
		}
		sort.Strings(ownerNames)
		for _, entry := range free {
			projection := estimateRoots(index, unit, units, byKey, []*operation{entry})
			abstractions = append(abstractions, Abstraction{Grade: projection.Grade, Name: entry.name, Audience: "external", Burden: projection.Burden, Hidden: projection.Hidden, Findings: projection.Findings, RecognizedHidden: projection.RecognizedHidden(), UncertainBurden: projection.Assessment().UncertainBurden, Limitations: append([]string(nil), projection.Limitations...)})
			if !haveBest || largerProjection(projection, best) {
				best, haveBest = projection, true
			}
		}
		for _, owner := range ownerNames {
			if projection, ok := methodProjections[goMethodGroupKeyFor(unit, owner)]; ok {
				abstractions = append(abstractions, Abstraction{Grade: projection.Grade, Name: owner, Audience: "external", Burden: projection.Burden, Hidden: projection.Hidden, Findings: projection.Findings, RecognizedHidden: projection.RecognizedHidden(), UncertainBurden: projection.Assessment().UncertainBurden, Limitations: append([]string(nil), projection.Limitations...)})
				if !haveBest || largerProjection(projection, best) {
					best = projection
					haveBest = true
				}
			}
		}
		private, privateAudiences := privateGoRoots(unit, inbound, goMethods)
		for _, entry := range private {
			projection := estimateRoots(index, unit, units, byKey, []*operation{entry})
			audience := "local-unresolved"
			if privateAudiences[entry.id] == "sibling" {
				audience = "package"
			}
			name := entry.name
			if entry.owner != "" {
				name = entry.owner + "." + entry.name
			}
			abstractions = append(abstractions, Abstraction{Grade: projection.Grade, Name: name, Audience: audience, Burden: projection.Burden, Hidden: projection.Hidden, Findings: projection.Findings, RecognizedHidden: projection.RecognizedHidden(), UncertainBurden: projection.Assessment().UncertainBurden, Limitations: append([]string(nil), projection.Limitations...)})
			if !haveBest || largerProjection(projection, best) {
				best, haveBest = projection, true
			}
		}
		if !haveBest {
			best = estimateRoots(index, unit, units, byKey, nil)
		}
		for _, op := range unit.ops {
			if goSupportingRole(goMethods, op) != "" {
				role := goSupportingRole(goMethods, op)
				best.Roles = appendUniqueString(best.Roles, role)
				if best.Supporting == nil {
					best.Supporting = map[string]string{}
				}
				best.Supporting[op.owner] = role
			}
		}
		best.Abstractions = abstractions
		if len(best.Roles) > 0 && !haveBehaviorRoots(unit, goMethods) && goRoleOnlyFile(unit) {
			best.Applicable, best.Burden, best.Hidden = false, 0, 0
			best.RoleOnly = true
			best.Abstractions = nil
		}
		results[unit.file.Path] = best
	}
	return results
}

func haveBehaviorRoots(unit unit, goMethods goMethodIndex) bool {
	for _, op := range unit.ops {
		if goSupportingRole(goMethods, op) == "" {
			return true
		}
	}
	return false
}

func markDependencies(reachable map[string]bool, result Result) {
	for _, dependency := range result.Dependencies {
		reachable[dependency] = true
	}
}

func goCallGraph(units []unit, byKey map[string][]*operation) map[string][]*operation {
	return boundedCallGraph(units, byKey, "go")
}
func boundedCallGraph(units []unit, byKey map[string][]*operation, language string) map[string][]*operation {
	graph := map[string][]*operation{}
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) != language {
			continue
		}
		for _, op := range unit.ops {
			calls := callsIn(op.body)
			if len(calls) > maxCallsPerRoot {
				calls = calls[:maxCallsPerRoot]
			}
			seen := map[string]bool{}
			for _, call := range calls {
				if unreachableCall(op.body, call) {
					continue
				}
				for _, candidate := range resolveCall(op, call, units, byKey) {
					if seen[candidate.id] {
						continue
					}
					seen[candidate.id] = true
					graph[op.id] = append(graph[op.id], candidate)
				}
			}
		}
	}
	return graph
}

type goInboundData struct {
	sameFileInbound map[string]int
	externalInbound map[string]int
	byID            map[string]*operation
}

func buildGoInboundIndex(units []unit, graph map[string][]*operation) goInboundData {
	inbound := goInboundData{sameFileInbound: map[string]int{}, externalInbound: map[string]int{}, byID: map[string]*operation{}}
	for _, unit := range units {
		for _, candidate := range unit.ops {
			inbound.byID[candidate.id] = candidate
		}
	}
	for callerID, callees := range graph {
		caller := inbound.byID[callerID]
		if caller == nil {
			continue
		}
		for _, callee := range callees {
			if caller.file == callee.file {
				inbound.sameFileInbound[callee.id]++
			} else {
				inbound.externalInbound[callee.id]++
			}
		}
	}
	return inbound
}

func privateGoRoots(unit unit, inbound goInboundData, goMethods goMethodIndex) ([]*operation, map[string]string) {
	roots := make([]*operation, 0)
	audiences := map[string]string{}
	for _, op := range unit.ops {
		if op.exposed || goSupportingRole(goMethods, op) != "" {
			continue
		}
		// Receiver methods in a type surface are generally helpers for the
		// exported type projection. Keep an independent private method only
		// when its body demonstrates substantive behavior; this preserves the
		// full-surface rating for split helper files without hiding real work.
		if op.owner != "" && len(goMethods.exported[goMethodGroupKey(op)]) != 0 && !nontrivial(op.body) {
			continue
		}
		if inbound.sameFileInbound[op.id] == 0 || inbound.externalInbound[op.id] != 0 {
			roots = append(roots, op)
			if inbound.externalInbound[op.id] != 0 {
				audiences[op.id] = "sibling"
			} else {
				audiences[op.id] = "local"
			}
		}
	}
	return roots, audiences
}

func largerProjection(candidate, current Result) bool {
	if candidate.PublishedPenalty() != current.PublishedPenalty() {
		return candidate.PublishedPenalty() > current.PublishedPenalty()
	}
	candidateLeft := candidate.Burden * (current.Burden + 2*current.Hidden)
	currentLeft := current.Burden * (candidate.Burden + 2*candidate.Hidden)
	if candidateLeft != currentLeft {
		return candidateLeft > currentLeft
	}
	if candidate.Burden != current.Burden {
		return candidate.Burden > current.Burden
	}
	if candidate.Hidden != current.Hidden {
		return candidate.Hidden > current.Hidden
	}
	return len(candidate.Dependencies) > len(current.Dependencies)
}

func goMethodGroupKey(op *operation) string {
	return op.pkg + "#" + op.owner
}

func goMethodGroupKeyFor(unit unit, key string) string {
	// method group keys are package-qualified; retain the caller's package when
	// selecting the cached projection for one file.
	if strings.HasPrefix(key, unit.pkg+"#") {
		return key
	}
	return unit.pkg + "#" + key
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// Merge combines file estimates belonging to one logical source boundary.
// Burden is additive across caller operations; normalized responsibilities
// and residual unknown opportunities are counted once.
func Merge(results ...Result) Result {
	if len(results) == 1 {
		return results[0]
	}
	merged := Result{Categories: map[string]float64{}, evidence: map[string]float64{}}
	if len(results) > 0 {
		merged.NoAbstractionProven = true
		merged.RoleOnly = true
	}
	for _, result := range results {
		merged.Applicable = merged.Applicable || result.Applicable
		merged.Findings = append(merged.Findings, result.Findings...)
		merged.NoAbstractionProven = merged.NoAbstractionProven && result.NoAbstractionProven
		merged.RoleOnly = merged.RoleOnly && result.RoleOnly
		for _, role := range result.Roles {
			merged.Roles = appendUniqueString(merged.Roles, role)
		}
		if len(result.Supporting) != 0 {
			if merged.Supporting == nil {
				merged.Supporting = map[string]string{}
			}
			for owner, role := range result.Supporting {
				merged.Supporting[owner] = role
			}
		}
		merged.Abstractions = append(merged.Abstractions, result.Abstractions...)
		merged.Burden += result.Burden
		merged.UncertainBurden += result.UncertainBurden
		merged.Dependencies = append(merged.Dependencies, result.Dependencies...)
		merged.Limitations = append(merged.Limitations, result.Limitations...)
		if len(result.evidence) == 0 {
			for category, value := range result.Categories {
				key := "fallback|" + category
				if _, exists := merged.evidence[key]; !exists {
					merged.evidence[key] = value
					merged.Categories[category] += value
					merged.Hidden += value
				}
			}
			continue
		}
		for key, value := range result.evidence {
			if _, exists := merged.evidence[key]; exists {
				continue
			}
			merged.evidence[key] = value
			category := key
			if separator := strings.IndexByte(category, '|'); separator >= 0 {
				category = category[:separator]
			}
			merged.Categories[category] += value
			merged.Hidden += value
		}
	}
	merged.Dependencies = uniqueSorted(merged.Dependencies)
	merged.Limitations = uniqueSorted(merged.Limitations)
	if !merged.Applicable {
		merged.Burden, merged.Hidden = 0, 0
	}
	return merged
}

type operationMeasure struct {
	evidence     []evidenceItem
	dependencies []string
	limitations  []string
}

type evidenceItem struct {
	category, key   string
	storageComputed bool
	origin          *operation
}

func categoryWeight(category string) float64 {
	switch category {
	case "transform", "state", "resource", "coordination":
		return 2
	default:
		return 1
	}
}

func measureOperation(op *operation, units []unit, byKey map[string][]*operation, seen map[string]bool, depth int, bindings map[string][]token, budget *int) operationMeasure {
	m := operationMeasure{}
	if op == nil || depth > maxCallDepth || seen[op.id] {
		if op != nil {
			m.evidence = append(m.evidence, evidenceItem{category: "unknown_outcome", key: "unknown_outcome|" + op.id + "|cutoff"})
		}
		if depth > maxCallDepth {
			m.limitations = append(m.limitations, "call_depth_limit")
		} else if op != nil {
			m.limitations = append(m.limitations, "call_cycle")
		}
		return m
	}
	seen[op.id] = true
	defer delete(seen, op.id)
	body, expanded := substituteBody(op.body, bindings, op.language)
	if op.compactConstructor {
		// Component reassignments feed implicit field writes; they are not
		// disposable locals even when the compact body has no return.
		body, expanded = op.body, true
	}
	if !expanded {
		return operationMeasure{evidence: []evidenceItem{{category: "unknown_outcome", key: "unknown_outcome|expansion"}}, limitations: []string{"expression_expansion_limit"}}
	}
	body = pruneDeadFalseBranches(body)
	for _, item := range body {
		switch item.text {
		case "if", "?", "&&", "||", "switch", "try", "catch", "finally", "for", "while", "loop", "match", "await", "yield", "goto", "select", "unsafe":
			m.limitations = append(m.limitations, "unsupported_execution_alternatives:"+item.text)
		}
	}
	for i, item := range body {
		if op.language == "rust" && item.text == "!" && i > 0 && isIdentifier(body[i-1].text) {
			m.limitations = append(m.limitations, "unexpanded_macro")
		}
	}
	body = markStringParams(body, op.stringParams)
	if combinesOwnedErrors(op, units) {
		m.evidence = append(m.evidence, evidenceItem{category: "coordination", key: "coordination|owned-error-aggregation|" + op.owner})
	}
	for category := range localCategories(body, op, units[op.file]) {
		if category == "state" {
			continue
		}
		m.evidence = append(m.evidence, evidenceItem{category: category, key: category + "|" + canonicalBody(body)})
	}
	if gradeUncoveredStorageState(op, units[op.file], body) {
		m.evidence = append(m.evidence, evidenceItem{category: "state", key: "state|" + op.id})
	}
	if gradedCleanupMode([]*operation{op}, units, byKey, true) {
		m.evidence = append(m.evidence, evidenceItem{category: "resource", key: "resource|" + op.id})
	}
	m.evidence = append(m.evidence, evidenceItem{category: "storage_snapshot", key: "storage_snapshot|" + op.id, origin: op, storageComputed: gradeComputedAssignment(op, units[op.file], body)})
	calls := callsIn(body)
	if len(calls) > maxCallsPerRoot {
		calls = calls[:maxCallsPerRoot]
		m.limitations = append(m.limitations, "call_limit")
	}
	unknownCalls := map[string]bool{}
	for _, call := range calls {
		if unreachableCall(body, call) {
			continue
		}
		if *budget <= 0 {
			m.evidence = append(m.evidence, evidenceItem{category: "unknown_outcome", key: "unknown_outcome|" + op.id + "|budget"})
			m.limitations = append(m.limitations, "call_budget_limit")
			break
		}
		*budget--
		candidates := resolveCall(op, call, units, byKey)
		if len(candidates) == 1 {
			callee := candidates[0]
			m.dependencies = append(m.dependencies, callee.id)
			childBindings := make(map[string][]token, len(callee.paramNames))
			for i, formal := range callee.paramNames {
				if i < len(call.actuals) {
					childBindings[formal] = call.actuals[i]
				}
			}
			child := measureOperation(callee, units, byKey, seen, depth+1, childBindings, budget)
			m.evidence = append(m.evidence, child.evidence...)
			m.dependencies = append(m.dependencies, child.dependencies...)
			m.limitations = append(m.limitations, child.limitations...)
			continue
		}
		// A void/no-argument call can still manage resources or mutate state.
		// Its lack of a returned value is not evidence of zero responsibility.
		unknownCalls[call.signature] = true
	}
	if len(unknownCalls) != 0 {
		m.evidence = append(m.evidence, evidenceItem{category: "unknown_call", key: "unknown_call|" + op.id})
		for signature := range unknownCalls {
			m.limitations = append(m.limitations, "unresolved_call_range_0_2:"+signature)
		}
	}
	// A nontrivial body with an unsupported outcome still receives a bounded
	// uncertainty unit. Identity and declaration-only bodies remain at zero.
	if onlyStorageSnapshots(m.evidence) && nontrivial(body) {
		if _, _, complete := returnedTransformation(op, units, byKey); complete && !op.compactConstructor {
			return m
		}
		m.evidence = append(m.evidence, evidenceItem{category: "unknown_outcome", key: "unknown_outcome|" + canonicalBody(body)})
		m.limitations = append(m.limitations, "unsupported_outcome_range_0_2")
	}
	for i := range m.evidence {
		if m.evidence[i].origin == nil {
			m.evidence[i].origin = op
		}
	}
	return m
}

func localCategories(body []token, op *operation, u unit) map[string]bool {
	result := map[string]bool{}
	if hasValidationContext(body, op.language, gradedBuiltinPanic(op, u)) {
		result["validation"] = true
	}
	if hasState(body) {
		result["state"] = true
	}
	return result
}

func hasTransform(body []token) bool {
	for i, item := range body {
		if item.text != "+" && item.text != "-" && item.text != "*" && item.text != "/" && item.text != "%" && item.text != "&" && item.text != "|" && item.text != "^" {
			continue
		}
		left, right := "", ""
		if i > 0 {
			left = body[i-1].text
		}
		if i+1 < len(body) {
			right = body[i+1].text
		}
		leftToken, rightToken := token{text: left}, token{text: right}
		if i > 0 {
			leftToken = body[i-1]
		}
		if i+1 < len(body) {
			rightToken = body[i+1]
		}
		if item.text == "+" && noopAddition(leftToken, rightToken) || item.text == "-" && right == "0" || item.text == "*" && (left == "0" || left == "1" || right == "0" || right == "1") || item.text == "/" && right == "1" {
			continue
		}
		// A unary sign and a bitwise expression over a literal are not useful
		// evidence of a hidden transformation.
		if (item.text == "+" || item.text == "-") && (i == 0 || isOperator(left)) {
			continue
		}
		return true
	}
	return false
}

func noopAddition(left, right token) bool {
	if left.kind == "stringvar" || right.kind == "stringvar" || left.kind == "string" && left.text == "__string__" || right.kind == "string" && right.text == "__string__" {
		return false
	}
	if left.text == "__empty_string__" || right.text == "__empty_string__" {
		return true
	}
	return left.text == "0" || right.text == "0"
}

func hasValidation(body []token) bool {
	return hasValidationContext(body, "", false)
}

func hasValidationContext(body []token, language string, builtinPanic bool) bool {
	guard := false
	guardedThrow := false
	returns := make([]string, 0, 2)
	for i, item := range body {
		switch item.text {
		case "if", "match", "switch", "assert":
			if item.text == "match" && language != "rust" || item.text == "assert" && language != "java" {
				continue
			}
			if i+1 >= len(body) || body[i+1].text == "=" || body[i+1].text == ";" {
				continue
			}
			if item.text == "if" && i+3 < len(body) && body[i+2].text == "false" && body[i+3].text == "&&" {
				continue
			}
			guard = true
		case "throw", "panic":
			if item.text == "panic" && (!builtinPanic || language != "go" || i+1 >= len(body) || body[i+1].text != "(" || i > 0 && body[i-1].text == ".") {
				continue
			}
			if item.text == "throw" && language != "java" && language != "typescript" {
				continue
			}
			if guard {
				guardedThrow = true
			}
		case "return":
			returns = append(returns, "return")
		}
	}
	if guardedThrow {
		return true
	}
	if !guard || len(returns) < 2 {
		return false
	}
	// Two identical outcomes do not establish a validation split. Keep the
	// proof conservative when expressions are unavailable.
	values := returnedValues(body)
	return len(values) >= 2 && values[0] != values[1]
}

func returnedValues(body []token) []string {
	values := make([]string, 0, 2)
	for i, item := range body {
		if item.text != "return" || i+1 >= len(body) {
			continue
		}
		end := i + 1
		for end < len(body) && body[end].text != ";" && body[end].text != "}" {
			end++
		}
		values = append(values, joinTokens(body[i+1:end]))
		if len(values) == 2 {
			break
		}
	}
	return values
}

func hasState(body []token) bool {
	for i := 0; i+3 < len(body); i++ {
		if !isMember(body, i) {
			continue
		}
		switch body[i+3].text {
		case "++", "--":
			return true
		case "+=", "-=", "*=", "/=", "%=":
			end := statementEnd(body, i+4)
			if compoundUpdateChanges(body[i+4:end], body[i+3].text) {
				return true
			}
		case "=":
			end := statementEnd(body, i+4)
			if readsAndChangesMember(body[i:i+3], body[i+4:end]) {
				return true
			}
		}
	}
	for i := 1; i+2 < len(body); i++ {
		if (body[i-1].text == "++" || body[i-1].text == "--") && isMember(body, i) {
			return true
		}
	}
	return false
}

func isMember(body []token, start int) bool {
	return start+2 < len(body) && isIdentifier(body[start].text) && body[start+1].text == "." && isIdentifier(body[start+2].text)
}

func statementEnd(body []token, start int) int {
	for i := start; i < len(body); i++ {
		if body[i].text == ";" || body[i].text == "}" {
			return i
		}
	}
	return len(body)
}

func compoundUpdateChanges(rhs []token, operator string) bool {
	if len(rhs) != 1 || rhs[0].kind != "number" {
		return true
	}
	switch operator {
	case "+=", "-=":
		return rhs[0].text != "0"
	case "*=", "/=":
		return rhs[0].text != "1"
	}
	return true
}

func readsAndChangesMember(member, rhs []token) bool {
	read := false
	for i := 0; i+2 < len(rhs); i++ {
		if sameMember(member, rhs[i:i+3]) {
			read = true
			break
		}
	}
	return read && hasTransform(rhs)
}

func sameMember(left, right []token) bool {
	return len(left) == 3 && len(right) >= 3 && left[0].text == right[0].text && left[1].text == right[1].text && left[2].text == right[2].text
}

type call struct {
	signature, name string
	hasArguments    bool
	actuals         [][]token
	position        int
}

func callsIn(body []token) []call {
	result := make([]call, 0, 8)
	for i := 0; i+1 < len(body); i++ {
		if !isIdentifier(body[i].text) || body[i+1].text != "(" || isControl(body[i].text) {
			continue
		}
		close := matching(body, i+1, "(", ")")
		if close < 0 {
			continue
		}
		args := close > i+2
		name := body[i].text
		parts := []string{body[i].text}
		for cursor := i - 1; cursor >= 1 && (body[cursor].text == "." || body[cursor].text == "::"); cursor -= 2 {
			if !isReceiverPart(body[cursor-1]) {
				break
			}
			parts = append([]string{body[cursor-1].text}, parts...)
		}
		if len(parts) > 1 {
			name = strings.Join(parts, ".")
		}
		actuals := splitArguments(body[i+2 : close])
		result = append(result, call{signature: name + "/" + strings.TrimSpace(joinTokens(body[i+2:close])), name: name, hasArguments: args, actuals: actuals, position: i})
	}
	return result
}

func isReceiverPart(item token) bool {
	return isIdentifier(item.text) || item.kind == "number"
}

func splitArguments(body []token) [][]token {
	if len(body) == 0 {
		return nil
	}
	result := make([][]token, 0, 2)
	start, depth := 0, 0
	for i, item := range body {
		switch item.text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				result = append(result, body[start:i])
				start = i + 1
			}
		}
	}
	result = append(result, body[start:])
	return result
}

func substituteBody(body []token, bindings map[string][]token, language string) ([]token, bool) {
	if len(bindings) == 0 {
		return body, true
	}
	result := make([]token, 0, len(body))
	for _, item := range body {
		if replacement, ok := bindings[item.text]; ok && isIdentifier(item.text) {
			if len(result)+len(replacement) > maxTokensPerFile {
				return nil, false
			}
			result = append(result, replacement...)
		} else {
			result = append(result, item)
		}
	}
	return result, true
}

func unreachableCall(body []token, candidate call) bool {
	i := candidate.position
	for i >= 2 && body[i-1].text == "." {
		i -= 2
	}
	return i >= 2 && (body[i-2].text == "false" && body[i-1].text == "&&" || body[i-2].text == "true" && body[i-1].text == "||")
}

func resolveCall(caller *operation, c call, units []unit, byKey map[string][]*operation) []*operation {
	if matches, handled := resolveTypeScriptImportedCall(caller, c, units, byKey); handled {
		return matches
	}
	name := c.name
	owner := ""
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		owner = name[:dot]
		name = name[dot+1:]
	}
	receiverType, hasReceiverType := caller.fieldTypes[owner]
	if !hasReceiverType {
		if dot := strings.LastIndexByte(owner, '.'); dot >= 0 {
			receiverType, hasReceiverType = caller.fieldTypes[owner[dot+1:]]
		}
	}
	if hasReceiverType {
		owner = receiverType
	}
	if owner == "" || owner == "this" || owner == "self" {
		owner = caller.owner
	}
	find := func(targetOwner string) []*operation {
		matches := make([]*operation, 0)
		for _, candidate := range byKey[scopedOperationKey(caller, name, targetOwner)] {
			if (caller.language == "typescript" || caller.language == "rust") && candidate.file != caller.file {
				continue
			}
			if targetOwner != "" && candidate.owner != "" && candidate.owner != targetOwner {
				continue
			}
			if candidate.file == caller.file || sameLanguage(candidate.language, caller.language) {
				matches = append(matches, candidate)
			}
		}
		return matches
	}
	matches := find(owner)
	if len(matches) == 0 && (caller.language == "typescript" || caller.language == "rust") && owner != "" {
		// Cross-file delegation is allowed only for an explicit receiver type (or
		// a qualified Rust/TypeScript owner). The index is prebuilt and an
		// ambiguous owner remains unresolved rather than guessing.
		explicitOwner := hasReceiverType
		if caller.language == "rust" {
			explicitOwner = explicitOwner || owner != caller.owner
		}
		if caller.language == "typescript" {
			_, explicitImport := caller.imports[owner]
			explicitOwner = explicitOwner || explicitImport
		}
		if explicitOwner {
			for _, candidate := range byKey[workspaceOperationKey(caller, name, owner)] {
				if candidate.file != caller.file && candidate.owner == owner && candidate.exposed {
					matches = append(matches, candidate)
				}
			}
		}
	}
	if len(matches) == 0 && owner != "" && caller.owner != "" {
		// A Go method may call a package-level helper without qualification.
		// Prefer a same-receiver method, then fall back to the package helper.
		matches = find("")
	}
	return matches
}

func canonicalBody(body []token) string {
	var b strings.Builder
	for _, item := range body {
		if isIdentifier(item.text) {
			b.WriteString("id")
		} else {
			b.WriteString(item.text)
		}
		b.WriteByte(' ')
	}
	return b.String()
}

func findOperations(file File, index int, tokens []token, pkg string) []*operation {
	lang := normalizeLanguage(file.Language, file.Path)
	result := make([]*operation, 0, 8)
	fieldTypes := sourceFieldTypes(tokens)
	owners := lexicalOwners(tokens, lang)
	recordHeaders, compactConstructors := javaCompactConstructors(file, index, tokens, pkg)
	exportedClasses := exportedClassNames(tokens)
	for i := 0; i < len(tokens); i++ {
		if recordHeaders[i] {
			continue
		}
		if len(result) >= maxOperationsPerFile {
			break
		}
		nameIndex, paramOpen, exported := -1, -1, false
		singleArrowParam := false
		receiverStart, receiverEnd := -1, -1
		switch lang {
		case "go":
			if tokens[i].text != "func" {
				continue
			}
			j := i + 1
			if j < len(tokens) && tokens[j].text == "(" {
				end := matching(tokens, j, "(", ")")
				if end < 0 {
					continue
				}
				receiverStart, receiverEnd = j+1, end
				j = end + 1
			}
			if j >= len(tokens) || !isIdentifier(tokens[j].text) || j+1 >= len(tokens) || tokens[j+1].text != "(" {
				continue
			}
			nameIndex, exported = j, unicode.IsUpper(rune(tokens[j].text[0]))
		case "rust":
			if tokens[i].text != "fn" || i+1 >= len(tokens) || !isIdentifier(tokens[i+1].text) {
				continue
			}
			j := i + 2
			if j < len(tokens) && tokens[j].text == "<" {
				genericEnd := matching(tokens, j, "<", ">")
				if genericEnd < 0 {
					continue
				}
				j = genericEnd + 1
			}
			if j >= len(tokens) || tokens[j].text != "(" {
				continue
			}
			nameIndex, paramOpen, exported = i+1, j, hasModifier(tokens, i, "pub")
		default:
			if tokens[i].text == "function" && i+1 < len(tokens) && isIdentifier(tokens[i+1].text) && i+2 < len(tokens) && tokens[i+2].text == "(" {
				nameIndex, exported = i+1, hasModifier(tokens, i, "export") || hasModifier(tokens, i, "public")
			} else if lang == "typescript" && tokens[i].text == "const" && i+3 < len(tokens) &&
				isIdentifier(tokens[i+1].text) && tokens[i+2].text == "=" {
				if tokens[i+3].text == "(" {
					close := matching(tokens, i+3, "(", ")")
					if close >= 0 && arrowToken(tokens, close) >= 0 {
						nameIndex, paramOpen, exported = i+1, i+3, hasModifier(tokens, i, "export")
					}
				} else if isIdentifier(tokens[i+3].text) && i+4 < len(tokens) && tokens[i+4].text == "=>" {
					nameIndex, paramOpen, exported = i+1, i+3, hasModifier(tokens, i, "export")
					singleArrowParam = true
				}
			} else if isIdentifier(tokens[i].text) && i+1 < len(tokens) && tokens[i+1].text == "(" && !isControl(tokens[i].text) {
				close := matching(tokens, i+1, "(", ")")
				if close >= 0 && declarationBody(tokens, close) >= 0 {
					nameIndex = i
					private := hasModifier(tokens, i, "private")
					exported = !private && (hasModifier(tokens, i, "public") || hasModifier(tokens, i, "protected") || hasModifier(tokens, i, "export") || hasExportedType(tokens, i) || exportedClasses[owners[i]])
				}
			}
		}
		packageVisible := lang == "go" || lang == "rust" || lang == "java" && !hasModifier(tokens, nameIndex, "private")
		if nameIndex < 0 {
			continue
		}
		if paramOpen < 0 {
			paramOpen = nameIndex + 1
		}
		close := paramOpen
		var parameters []token
		if singleArrowParam {
			if paramOpen >= len(tokens) || !isIdentifier(tokens[paramOpen].text) {
				continue
			}
			parameters = tokens[paramOpen : paramOpen+1]
		} else {
			if paramOpen >= len(tokens) || tokens[paramOpen].text != "(" {
				continue
			}
			close = matching(tokens, paramOpen, "(", ")")
			if close < 0 {
				continue
			}
			parameters = tokens[paramOpen+1 : close]
		}
		bodyStart := close + 1
		for bodyStart < len(tokens) && tokens[bodyStart].text != "{" && tokens[bodyStart].text != "=>" {
			bodyStart++
		}
		if bodyStart >= len(tokens) {
			continue
		}
		returnType := ""
		if lang == "go" {
			returnType = goReturnType(tokens[close+1 : bodyStart])
		}
		bodyEnd := 0
		var body []token
		if tokens[bodyStart].text == "=>" {
			if bodyStart+1 < len(tokens) && tokens[bodyStart+1].text == "{" {
				bodyEnd = matching(tokens, bodyStart+1, "{", "}")
				if bodyEnd < 0 {
					continue
				}
				body = tokens[bodyStart+2 : bodyEnd]
			} else {
				bodyEnd = expressionBodyEnd(tokens, bodyStart+1)
				body = tokens[bodyStart+1 : bodyEnd]
			}
		} else {
			bodyEnd = matching(tokens, bodyStart, "{", "}")
			if bodyEnd < 0 {
				bodyEnd = len(tokens)
			}
			body = tokens[bodyStart+1 : bodyEnd]
		}
		owner, receiverName := owners[nameIndex], ""
		opFieldTypes := fieldTypes
		if lang == "go" && receiverStart >= 0 && receiverEnd >= receiverStart {
			owner, receiverName = goReceiver(tokens[receiverStart:receiverEnd])
			if receiverName != "" && owner != "" {
				opFieldTypes = cloneStringMap(fieldTypes)
				if opFieldTypes == nil {
					opFieldTypes = map[string]string{}
				}
				opFieldTypes[receiverName] = owner
			}
		}
		if lang == "go" {
			opFieldTypes = goLocalReceiverTypes(body, opFieldTypes)
		}
		op := &operation{id: file.Path + "#" + tokens[nameIndex].text + "/" + itoa(len(result)), name: tokens[nameIndex].text, owner: owner, receiverName: receiverName, returnType: returnType, language: lang, pkg: pkg, file: index, params: parameterCount(parameters), paramNames: parameterNames(parameters, lang), requiredParamNames: requiredParameterNames(parameters, lang), stringParams: stringParameterNames(parameters, lang), fieldTypes: opFieldTypes, exposed: exported, packageVisible: packageVisible, body: body}
		op.parameterTypes = gradedParameterTypes(parameters, lang)
		op.expressionBody = tokens[bodyStart].text == "=>" && bodyStart+1 < len(tokens) && tokens[bodyStart+1].text != "{"
		result = append(result, op)
		i = bodyEnd
	}
	if remaining := maxOperationsPerFile - len(result); len(compactConstructors) > remaining {
		compactConstructors = compactConstructors[:remaining]
	}
	result = append(result, compactConstructors...)
	return result
}

func exportedClassNames(tokens []token) map[string]bool {
	result := map[string]bool{}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].text == "export" && tokens[i+1].text == "class" && isIdentifier(tokens[i+2].text) {
			result[tokens[i+2].text] = true
		}
	}
	return result
}

func sourceFieldTypes(tokens []token) map[string]string {
	result := map[string]string{}
	ambiguous := map[string]bool{}
	add := func(name, fieldType string) {
		if ambiguous[name] {
			return
		}
		if previous, exists := result[name]; exists && previous != fieldType {
			delete(result, name)
			ambiguous[name] = true
			return
		}
		result[name] = fieldType
	}
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != "struct" || tokens[i+1].text != "{" {
			continue
		}
		end := matching(tokens, i+1, "{", "}")
		if end < 0 {
			continue
		}
		for field := i + 2; field+1 < end; field++ {
			if !isIdentifier(tokens[field].text) {
				continue
			}
			if tokens[field+1].text == ":" && field+2 < end && isIdentifier(tokens[field+2].text) {
				add(tokens[field].text, tokens[field+2].text)
			} else if isIdentifier(tokens[field+1].text) && (field == i+2 || tokens[field-1].text == "," || tokens[field-1].text == ";") {
				add(tokens[field].text, tokens[field+1].text)
			}
		}
	}
	for i := 0; i+3 < len(tokens); i++ {
		if isIdentifier(tokens[i].text) && isIdentifier(tokens[i+1].text) && tokens[i+2].text == "=" && tokens[i+3].text == "new" && i+4 < len(tokens) && isIdentifier(tokens[i+4].text) {
			add(tokens[i+1].text, tokens[i+4].text)
		}
		// Rust struct fields and typed TypeScript fields provide explicit
		// receiver ownership even when construction is in another file.
		if i+2 < len(tokens) && isIdentifier(tokens[i].text) && tokens[i+1].text == ":" && isIdentifier(tokens[i+2].text) {
			previous := ""
			if i > 0 {
				previous = tokens[i-1].text
			}
			if previous == "{" || previous == "," || previous == ";" || previous == "pub" || previous == "private" || previous == "protected" || previous == "readonly" || previous == "mut" {
				add(tokens[i].text, tokens[i+2].text)
			}
		}
	}
	return result
}

func goReceiver(tokens []token) (owner, name string) {
	identifiers := make([]string, 0, 2)
	for _, item := range tokens {
		if isIdentifier(item.text) {
			identifiers = append(identifiers, item.text)
		}
	}
	if len(identifiers) == 0 {
		return "", ""
	}
	if len(identifiers) >= 2 && isIdentifier(tokens[0].text) {
		return identifiers[1], identifiers[0]
	}
	return identifiers[0], ""
}

func goReturnType(tokens []token) string {
	if len(tokens) == 1 && tokens[0].text == "string" {
		return "string"
	}
	if len(tokens) == 3 && tokens[0].text == "(" && tokens[1].text == "string" && tokens[2].text == ")" {
		return "string"
	}
	return ""
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func operationKey(op *operation) string {
	return scopedOperationKey(op, op.name, op.owner)
}

func workspaceOperationKey(op *operation, name, owner string) string {
	if op.language == "rust" {
		// Rust source files in one bounded request can have different lexical
		// module names; explicit receiver ownership and unique candidates below
		// provide the crate-local scope.
		return "workspace:rust#" + owner + "#" + name
	}
	return "workspace:" + op.language + "#" + op.pkg + "#" + owner + "#" + name
}

func indexOperation(index map[string][]*operation, op *operation) {
	index[operationKey(op)] = append(index[operationKey(op)], op)
	if op.language == "typescript" || op.language == "rust" {
		index[workspaceOperationKey(op, op.name, op.owner)] = append(index[workspaceOperationKey(op, op.name, op.owner)], op)
	}
}

func scopedOperationKey(op *operation, name, owner string) string {
	scope := op.pkg
	if op.language == "typescript" || op.language == "rust" {
		scope += ":file:" + itoa(op.file)
	}
	return name + "@" + scope + "#" + owner
}

func lexicalOwners(tokens []token, language string) []string {
	owners := make([]string, len(tokens))
	if language != "java" && language != "typescript" {
		return owners
	}
	stack := make([]string, 0, 4)
	pending := ""
	for i, item := range tokens {
		if len(stack) != 0 {
			owners[i] = stack[len(stack)-1]
		}
		if (item.text == "class" || item.text == "interface" || item.text == "record") && i+1 < len(tokens) && isIdentifier(tokens[i+1].text) {
			pending = tokens[i+1].text
			continue
		}
		if item.text == "{" {
			if pending != "" {
				stack = append(stack, pending)
				pending = ""
			} else if len(stack) != 0 {
				stack = append(stack, stack[len(stack)-1])
			} else {
				stack = append(stack, "")
			}
		} else if item.text == "}" && len(stack) != 0 {
			stack = stack[:len(stack)-1]
		}
	}
	return owners
}

func unresolvedSurface(tokens []token) bool {
	publicSurface := false
	unknownType := false
	for _, item := range tokens {
		switch item.text {
		case "export", "pub", "public":
			publicSurface = true
		case "Missing", "Unknown", "Unresolved":
			unknownType = true
		}
	}
	return publicSurface && unknownType
}
func hasModifier(tokens []token, index int, modifier string) bool {
	start := index - 8
	if start < 0 {
		start = 0
	}
	for i := index - 1; i >= start; i-- {
		if tokens[i].text == ";" || tokens[i].text == "{" || tokens[i].text == "}" {
			break
		}
		if tokens[i].text == modifier {
			return true
		}
	}
	return false
}

func hasExportedType(tokens []token, index int) bool {
	depth := 0
	for i := index - 1; i >= 0 && index-i < 1024; i-- {
		switch tokens[i].text {
		case "}":
			depth++
		case "{":
			if depth > 0 {
				depth--
				continue
			}
			for j := i - 1; j >= 0 && i-j < 64; j-- {
				if tokens[j].text == "export" && j+1 < len(tokens) && tokens[j+1].text == "class" {
					return true
				}
				if tokens[j].text == "class" || tokens[j].text == "interface" {
					break
				}
			}
			return false
		}
	}
	return false
}

func declarationBody(tokens []token, close int) int {
	for i := close + 1; i < minInt(close+16, len(tokens)); i++ {
		if tokens[i].text == ";" {
			return -1
		}
		if tokens[i].text == "{" || tokens[i].text == "=>" {
			return i
		}
	}
	return -1
}

func arrowToken(tokens []token, close int) int {
	for i := close + 1; i < len(tokens) && i <= close+32; i++ {
		if tokens[i].text == "=>" {
			return i
		}
		if tokens[i].text == ";" || tokens[i].text == "{" {
			return -1
		}
	}
	return -1
}

func expressionBodyEnd(tokens []token, start int) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].text {
		case "(", "[":
			depth++
		case ")", "]":
			if depth > 0 {
				depth--
			}
		case ";", "}":
			if depth == 0 {
				return i
			}
		}
	}
	return len(tokens)
}

func parameterCount(tokens []token) int {
	return len(splitParameterDeclarations(tokens))
}

func parameterNames(tokens []token, language string) []string {
	parts := splitParameterDeclarations(tokens)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		// Rust receiver parameters are part of the method signature, but they
		// are not caller supplied inputs. Lifetimes can precede the receiver
		// (`&'a mut self`), so check the binding itself before choosing the
		// first identifier as the parameter name.
		if language == "rust" {
			receiver := false
			for _, item := range part {
				if item.text == "self" {
					receiver = true
					break
				}
			}
			if receiver {
				continue
			}
		}
		identifiers := make([]string, 0, 2)
		for _, item := range part {
			if isIdentifier(item.text) && item.text != "const" && item.text != "mut" {
				identifiers = append(identifiers, item.text)
			}
		}
		if len(identifiers) == 0 {
			continue
		}
		if language == "typescript" {
			result = append(result, identifiers[0])
		} else if language == "java" {
			result = append(result, identifiers[len(identifiers)-1])
		} else {
			result = append(result, identifiers[0])
		}
	}
	return result
}

func stringParameterNames(tokens []token, language string) map[string]bool {
	parts := splitArguments(tokens)
	result := map[string]bool{}
	for _, part := range parts {
		isString := false
		for _, item := range part {
			if item.text == "String" || item.text == "string" {
				isString = true
			}
		}
		if !isString {
			continue
		}
		names := parameterNames(part, language)
		for _, name := range names {
			result[name] = true
		}
	}
	return result
}

func markStringParams(body []token, names map[string]bool) []token {
	if len(names) == 0 {
		return body
	}
	result := append([]token(nil), body...)
	for i := range result {
		if names[result[i].text] {
			result[i].kind = "stringvar"
		}
	}
	return result
}
func hasReturnedValue(body []token) bool {
	for i, item := range body {
		if item.text == "return" && i+1 < len(body) && body[i+1].text != ";" {
			return true
		}
	}
	return false
}
func nontrivial(body []token) bool {
	if hasTransform(body) || hasValidation(body) || hasState(body) {
		return true
	}
	for _, item := range body {
		switch item.text {
		case "for", "while", "throw", "new", "await", "yield", "match", "switch", "try", "catch", "synchronized", "defer", "=", "+=", "-=", "++", "--", "<<", ">>":
			return true
		}
	}
	for index, item := range body {
		if item.text == "if" && !definitelyFalseIf(body, index) && !identicalReturnedOutcomes(body) {
			return true
		}
	}
	return false
}

func identicalReturnedOutcomes(body []token) bool {
	values := returnedValues(body)
	if len(values) < 2 {
		return false
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return false
		}
	}
	return true
}

func definitelyFalseIf(body []token, start int) bool {
	conditionStart, conditionEnd := start+1, start+1
	if conditionStart < len(body) && body[conditionStart].text == "(" {
		close := matching(body, conditionStart, "(", ")")
		if close < 0 {
			return false
		}
		conditionStart++
		conditionEnd = close
	} else {
		for conditionEnd < len(body) && body[conditionEnd].text != "{" && body[conditionEnd].text != ";" {
			conditionEnd++
		}
	}
	condition := body[conditionStart:conditionEnd]
	if len(condition) == 1 && condition[0].text == "false" {
		return true
	}
	if len(condition) < 3 || condition[0].text != "false" || condition[1].text != "&&" {
		return false
	}
	for _, item := range condition[2:] {
		if item.text == "||" {
			return false
		}
	}
	return true
}

func hasReachableCall(body []token) bool {
	for _, candidate := range callsIn(body) {
		if !unreachableCall(body, candidate) {
			return true
		}
	}
	return false
}

func noAbstractionProven(u unit) bool {
	if u.limited || !u.lexicallyValid {
		return false
	}
	if len(u.tokens) == 0 {
		return true
	}
	switch normalizeLanguage(u.file.Language, u.file.Path) {
	case "go":
		return packageOnlyGo(u.tokens)
	case "java":
		return packageOnlyJava(u.tokens)
	default:
		return false
	}
}

func packageOnlyGo(tokens []token) bool {
	index := 0
	if !consumeToken(tokens, &index, "package") || !consumeIdentifier(tokens, &index) {
		return false
	}
	if index < len(tokens) && tokens[index].text != ";" {
		return false
	}
	for index < len(tokens) {
		if tokens[index].text == ";" {
			index++
			continue
		}
		if tokens[index].text != "import" {
			return false
		}
		index++
		if index < len(tokens) && tokens[index].text == "(" {
			index++
			for index < len(tokens) && tokens[index].text != ")" {
				if tokens[index].text == ";" {
					index++
					continue
				}
				if !consumeGoImport(tokens, &index) {
					return false
				}
			}
			if !consumeToken(tokens, &index, ")") {
				return false
			}
			if index < len(tokens) && tokens[index].text != ";" {
				return false
			}
			continue
		}
		if !consumeGoImport(tokens, &index) {
			return false
		}
		if index < len(tokens) && tokens[index].text != ";" {
			return false
		}
	}
	return true
}

func consumeGoImport(tokens []token, index *int) bool {
	if *index >= len(tokens) {
		return false
	}
	if tokens[*index].kind == "identifier" || tokens[*index].text == "." {
		(*index)++
		if *index >= len(tokens) {
			return false
		}
	}
	if tokens[*index].kind != "string" {
		return false
	}
	(*index)++
	return true
}

func packageOnlyJava(tokens []token) bool {
	index := 0
	if !consumeToken(tokens, &index, "package") || !consumeQualifiedName(tokens, &index) || !consumeToken(tokens, &index, ";") {
		return false
	}
	for index < len(tokens) {
		if !consumeToken(tokens, &index, "import") {
			return false
		}
		if index < len(tokens) && tokens[index].text == "static" {
			index++
		}
		if !consumeQualifiedName(tokens, &index) {
			return false
		}
		if index < len(tokens) && tokens[index].text == "." {
			index++
			if !consumeToken(tokens, &index, "*") {
				return false
			}
		}
		if !consumeToken(tokens, &index, ";") {
			return false
		}
	}
	return true
}

func consumeQualifiedName(tokens []token, index *int) bool {
	if !consumeIdentifier(tokens, index) {
		return false
	}
	for *index+1 < len(tokens) && tokens[*index].text == "." && tokens[*index+1].text != "*" {
		(*index)++
		if !consumeIdentifier(tokens, index) {
			return false
		}
	}
	return true
}

func consumeToken(tokens []token, index *int, text string) bool {
	if *index >= len(tokens) || tokens[*index].text != text {
		return false
	}
	(*index)++
	return true
}

func consumeIdentifier(tokens []token, index *int) bool {
	if *index >= len(tokens) || tokens[*index].kind != "identifier" {
		return false
	}
	(*index)++
	return true
}

func packageName(language string, tokens []token) string {
	lang := normalizeLanguage(language, "")
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text == "package" || tokens[i].text == "namespace" {
			if lang == "go" {
				// Go's package clause ends at the inserted semicolon at a
				// newline or at an explicit semicolon. The package name is
				// exactly the first identifier after the keyword; scanning
				// farther would fold the following declarations into the key.
				if tokens[i+1].kind == "identifier" {
					return tokens[i+1].text
				}
				continue
			}
			parts := make([]string, 0, 3)
			for j := i + 1; j < len(tokens) && tokens[j].text != ";" && tokens[j].text != "{"; j++ {
				if tokens[j].kind == "identifier" {
					parts = append(parts, tokens[j].text)
				}
			}
			if len(parts) != 0 {
				name := strings.Join(parts, ".")
				return name
			}
		}
	}
	if lang == "rust" {
		for i := 0; i+1 < len(tokens); i++ {
			if tokens[i].text == "mod" && tokens[i+1].kind == "identifier" {
				return tokens[i+1].text
			}
		}
	}
	return filepath.Dir(".")
}
func normalizeLanguage(language, path string) string {
	l := strings.ToLower(language)
	if strings.Contains(l, "typescript") || strings.Contains(l, "javascript") || strings.HasSuffix(strings.ToLower(path), ".ts") || strings.HasSuffix(strings.ToLower(path), ".tsx") {
		return "typescript"
	}
	if strings.Contains(l, "rust") || strings.HasSuffix(strings.ToLower(path), ".rs") {
		return "rust"
	}
	if strings.Contains(l, "go") || strings.HasSuffix(strings.ToLower(path), ".go") {
		return "go"
	}
	return "java"
}
func sameLanguage(a, b string) bool { return a == b }
func lex(source []byte) ([]token, bool, bool) {
	out := make([]token, 0, len(source)/3)
	limited := false
	valid := true
	for i := 0; i < len(source); {
		if len(out) >= maxTokensPerFile {
			locateTokens(out, source)
			return out, true, valid
		}
		c := source[i]
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '/' {
			i += 2
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '*' {
			i += 2
			closed := false
			for i+1 < len(source) && !(source[i] == '*' && source[i+1] == '/') {
				i++
			}
			if i+1 < len(source) {
				i += 2
				closed = true
			}
			if !closed {
				valid = false
				break
			}
			continue
		}
		// Rust lifetimes (`'a`, `'_`) are apostrophe-prefixed identifiers,
		// rather than quoted strings. The generic lexer has no language
		// argument, so recognize the unquoted form while leaving ordinary
		// single-quoted strings and character literals to the branch below.
		if c == '\'' && i+1 < len(source) && (unicode.IsLetter(rune(source[i+1])) || source[i+1] == '_') {
			j := i + 2
			for j < len(source) && (unicode.IsLetter(rune(source[j])) || unicode.IsDigit(rune(source[j])) || source[j] == '_') {
				j++
			}
			if j >= len(source) || source[j] != '\'' {
				out = append(out, token{text: "'", kind: "lifetime", offset: i})
				i++
				continue
			}
		}
		if c == '"' || c == '\'' || c == '`' {
			start := i
			quote := c
			i++
			closed := false
			for i < len(source) {
				if source[i] == '\\' {
					i += 2
					continue
				}
				if source[i] == quote {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				valid = false
			}
			kind := "__string__"
			if i-start == 2 {
				kind = "__empty_string__"
			}
			out = append(out, token{text: kind, kind: "string", literal: string(source[start:minInt(i, len(source))]), offset: start})
			continue
		}
		if unicode.IsLetter(rune(c)) || c == '_' || c == '$' {
			start := i
			i++
			for i < len(source) && (unicode.IsLetter(rune(source[i])) || unicode.IsDigit(rune(source[i])) || source[i] == '_' || source[i] == '$') {
				i++
			}
			out = append(out, token{text: string(source[start:i]), kind: "identifier", offset: start})
			if len(out) > maxTokensPerFile {
				limited = true
			}
			continue
		}
		if unicode.IsDigit(rune(c)) {
			start := i
			i++
			for i < len(source) && (unicode.IsDigit(rune(source[i])) || source[i] == '.' && i+1 < len(source) && unicode.IsDigit(rune(source[i+1]))) {
				i++
			}
			out = append(out, token{text: string(source[start:i]), kind: "number", offset: start})
			continue
		}
		if i+1 < len(source) {
			two := string(source[i : i+2])
			switch two {
			case "==", "!=", "<=", ">=", "=>", "++", "--", "+=", "-=", "*=", "/=", "%=", ":=", "&&", "||", "::", "->", "<<", ">>":
				out = append(out, token{text: two, offset: i})
				i += 2
				continue
			}
		}
		out = append(out, token{text: string(c), offset: i})
		i++
	}
	locateTokens(out, source)
	return out, limited, valid
}
func locateTokens(tokens []token, source []byte) {
	at, line := 0, 1
	for i := range tokens {
		for at < tokens[i].offset && at < len(source) {
			if source[at] == '\n' {
				line++
			}
			at++
		}
		tokens[i].line = line
	}
}

func matching(tokens []token, start int, open, close string) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		if tokens[i].text == open {
			depth++
		}
		if tokens[i].text == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}
func isIdentifier(s string) bool {
	if s == "" || !(unicode.IsLetter(rune(s[0])) || s[0] == '_' || s[0] == '$') {
		return false
	}
	for _, r := range s[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
			return false
		}
	}
	return true
}
func isControl(s string) bool {
	switch s {
	case "if", "for", "while", "switch", "catch", "return", "sizeof", "match":
		return true
	}
	return false
}
func isOperator(s string) bool { return strings.Contains("+-*/%&|^=!<>", s) }
func joinTokens(tokens []token) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.text)
	}
	return b.String()
}
func unique(values []string) []string {
	out := values[:0]
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func uniqueSorted(values []string) []string {
	sort.Strings(values)
	return unique(values)
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var out [20]byte
	i := len(out)
	for value > 0 {
		i--
		out[i] = digits[value%10]
		value /= 10
	}
	return string(out[i:])
}

func goSupportingRole(index goMethodIndex, op *operation) string {
	if role := index.supportingOperations[op.id]; role != "" {
		return role
	}
	return index.supporting[goMethodGroupKey(op)]
}

// Resolve an owned local constructed with T{} or &T{} without requiring types.
// Ambiguous/reassigned bindings are deliberately not used for helper attribution.
func goLocalReceiverTypes(body []token, fields map[string]string) map[string]string {
	result := cloneStringMap(fields)
	if result == nil {
		result = map[string]string{}
	}
	writes := map[string]int{}
	candidates := map[string]string{}
	for i := 0; i+2 < len(body); i++ {
		if !isIdentifier(body[i].text) || (body[i+1].text != ":=" && body[i+1].text != "=") {
			continue
		}
		name := body[i].text
		writes[name]++
		j := i + 2
		if body[j].text == "&" {
			j++
		}
		if j+1 < len(body) && isIdentifier(body[j].text) && body[j+1].text == "{" {
			candidates[name] = body[j].text
		}
	}
	for name, typ := range candidates {
		if writes[name] == 1 {
			result[name] = typ
		}
	}
	return result
}
