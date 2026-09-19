// Package sourceestimate provides a small, bounded source-only estimate for
// SHALLOW when a language adapter cannot establish a complete semantic proof.
package sourceestimate

import (
	"path/filepath"
	"sort"
	"strings"
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
	normalized                     operationBodies
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
	fieldTypeBindings              map[string]string
	fieldTypeContext               *operationTypeContext
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
	inventory         *unitInventory
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
		units[i] = unit{inventory: &unitInventory{}, calibration: profile, index: i, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: lexicallyValid}
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

// Resolve an owned local constructed with T{} or &T{} without requiring types.
// Ambiguous/reassigned bindings are deliberately not used for helper attribution.
func goLocalReceiverTypes(body []token, fields map[string]string) map[string]string {
	result := fields
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
	cloned := false
	for name, typ := range candidates {
		if writes[name] == 1 {
			if !cloned {
				result = cloneStringMap(fields)
				if result == nil {
					result = map[string]string{}
				}
				cloned = true
			}
			result[name] = typ
		}
	}
	return result
}
