package sourceestimate

import "strings"

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
