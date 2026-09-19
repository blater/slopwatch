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
		item := unit{inventory: &unitInventory{}, index: index, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: valid}
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
