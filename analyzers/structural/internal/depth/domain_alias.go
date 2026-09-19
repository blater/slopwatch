package depth

import (
	"sort"
	"strconv"
	"strings"
)

// OwnershipState is deliberately separate from mutability. Borrowing an
// input does not give the callee ownership of its backing storage.
type OwnershipState string

const (
	OwnershipOwned    OwnershipState = "owned"
	OwnershipBorrowed OwnershipState = "borrowed"
	OwnershipMoved    OwnershipState = "moved"
	OwnershipEscaped  OwnershipState = "escaped"
	OwnershipUnknown  OwnershipState = "unknown"
)

// AliasPrecision says whether Roots is complete. Supporting roots retained in
// Evidence after widening are never a complete alias set.
type AliasPrecision string

const (
	AliasKnown   AliasPrecision = "known"
	AliasUnknown AliasPrecision = "unknown"
)

const (
	AliasLimitReason      = "alias_limit"
	AccessPathLimitReason = "access_path_limit"
	UnknownAliasReason    = "UnknownAlias"
	UnknownInputReason    = "unknown_input"
	InvalidPathReason     = "invalid_access_path"
)

// PathSegment is one actual field or index step. Dynamic indexes use Kind
// "index" and Dynamic=true, yielding the canonical wildcard [*].
type PathSegment struct {
	Kind    string
	Name    string
	Dynamic bool
}

func FieldSegment(name string) PathSegment { return PathSegment{Kind: "field", Name: name} }
func IndexSegment(name string) PathSegment { return PathSegment{Kind: "index", Name: name} }
func DynamicIndexSegment() PathSegment     { return PathSegment{Kind: "index", Dynamic: true} }

// AliasRoot is a stable declared/formal/allocation root plus an access path.
// ID must be a canonical semantic identity; local variable names and source
// positions are intentionally not represented here.
type AliasRoot struct {
	ID        string
	Kind      string
	Path      []PathSegment
	Mutable   bool
	Multiple  bool
	Ownership OwnershipState
}

// AliasRootRef is the descriptive spelling used by transfer clients.
type AliasRootRef = AliasRoot

func NewAliasRoot(id string, mutable bool, ownership OwnershipState, path ...PathSegment) AliasRoot {
	return AliasRoot{ID: id, Mutable: mutable, Ownership: ownership, Path: append([]PathSegment(nil), path...)}
}

// FormalRoot creates the canonical root identity used by positional formal
// bindings.
func FormalRoot(index int, mutable bool, ownership OwnershipState, path ...PathSegment) AliasRoot {
	if index < 0 {
		return AliasRoot{Mutable: mutable, Ownership: ownership}
	}
	return AliasRoot{ID: "formal:" + strconv.Itoa(index), Kind: "formal", Mutable: mutable, Ownership: ownership, Path: append([]PathSegment(nil), path...)}
}

func segmentKey(s PathSegment) string {
	if s.Dynamic {
		return "index:*"
	}
	return s.Kind + ":" + s.Name
}

func aliasKey(r AliasRoot) string {
	var b strings.Builder
	writePart := func(value string) {
		b.WriteString(strconv.Itoa(len(value)))
		b.WriteByte(':')
		b.WriteString(value)
	}
	writePart(r.ID)
	writePart(r.Kind)
	for _, segment := range r.Path {
		writePart(segment.Kind)
		writePart(segment.Name)
		if segment.Dynamic {
			writePart("dynamic")
		} else {
			writePart("exact")
		}
	}
	return b.String()
}

func cloneRoot(r AliasRoot) AliasRoot {
	r.Path = append([]PathSegment(nil), r.Path...)
	return r
}

func sortRoots(roots []AliasRoot) {
	sort.Slice(roots, func(i, j int) bool {
		ki, kj := aliasKey(roots[i]), aliasKey(roots[j])
		if ki == kj {
			return roots[i].Ownership < roots[j].Ownership
		}
		return ki < kj
	})
}

// AliasReason records explicit widening or unknown input causes.
type AliasReason struct {
	Code    string
	Message string
}

// AliasSet is immutable from the caller's perspective. Every constructor,
// join, and path operation copies roots and reason slices.
type AliasSet struct {
	roots     []AliasRoot
	evidence  []AliasRoot
	reasons   []AliasReason
	precision AliasPrecision
}

// NewAliasSet creates a known bounded alias set. More than sixteen roots are
// widened deterministically and retain the sorted supporting roots as evidence.
func NewAliasSet(roots ...AliasRoot) AliasSet {
	return makeAliasSet(roots, nil, nil, AliasKnown)
}

func makeAliasSet(roots, evidence []AliasRoot, reasons []AliasReason, precision AliasPrecision) AliasSet {
	reasons = sortedReasons(reasons)
	canon := dedupeRoots(roots)
	e := dedupeRoots(evidence)
	for _, root := range canon {
		if !validRoot(root) {
			return AliasSet{evidence: canon, reasons: sortedReasons(appendReason(reasons, AliasReason{Code: InvalidPathReason, Message: "invalid alias root or path"})), precision: AliasUnknown}
		}
		if len(root.Path) > 4 {
			return AliasSet{evidence: canon, reasons: sortedReasons(appendReason(reasons, AliasReason{Code: AccessPathLimitReason, Message: "path depth exceeds 4"})), precision: AliasUnknown}
		}
	}
	if len(canon) > 16 {
		return AliasSet{evidence: canon, reasons: sortedReasons(appendReason(reasons, AliasReason{Code: AliasLimitReason, Message: "more than 16 possible roots"})), precision: AliasUnknown}
	}
	return AliasSet{roots: canon, evidence: e, reasons: append([]AliasReason(nil), reasons...), precision: precision}
}

func sortedReasons(reasons []AliasReason) []AliasReason {
	out := append([]AliasReason(nil), reasons...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Code == out[j].Code {
			return out[i].Message < out[j].Message
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func validRoot(root AliasRoot) bool {
	if root.ID == "" {
		return false
	}
	validOwnership := root.Ownership == OwnershipOwned || root.Ownership == OwnershipBorrowed || root.Ownership == OwnershipMoved || root.Ownership == OwnershipEscaped || root.Ownership == OwnershipUnknown
	if !validOwnership {
		return false
	}
	for _, segment := range root.Path {
		if !validSegment(segment) {
			return false
		}
	}
	return true
}

func validSegment(segment PathSegment) bool {
	if segment.Dynamic {
		return segment.Kind == "index"
	}
	return (segment.Kind == "field" || segment.Kind == "index") && segment.Name != ""
}

func dedupeRoots(roots []AliasRoot) []AliasRoot {
	byKey := make(map[string]AliasRoot)
	for _, root := range roots {
		key := aliasKey(root)
		if old, ok := byKey[key]; ok {
			old.Mutable = old.Mutable || root.Mutable
			old.Multiple = old.Multiple || root.Multiple
			if old.Ownership != root.Ownership {
				old.Ownership = OwnershipUnknown
			}
			byKey[key] = old
		} else {
			byKey[key] = cloneRoot(root)
		}
	}
	out := make([]AliasRoot, 0, len(byKey))
	for _, root := range byKey {
		out = append(out, root)
	}
	sortRoots(out)
	return out
}

func appendReason(in []AliasReason, reason AliasReason) []AliasReason {
	for _, old := range in {
		if old == reason {
			return append([]AliasReason(nil), in...)
		}
	}
	return append(append([]AliasReason(nil), in...), reason)
}

// UnknownAliasSet creates an explicit imprecise input.
func UnknownAliasSet(reason string) AliasSet {
	if reason == "" {
		reason = UnknownAliasReason
	}
	return AliasSet{precision: AliasUnknown, reasons: []AliasReason{{Code: reason, Message: "alias set is unknown"}}}
}

// Roots returns complete known roots only, defensively copied and sorted.
func (s AliasSet) Roots() []AliasRoot {
	out := make([]AliasRoot, len(s.roots))
	for i, root := range s.roots {
		out[i] = cloneRoot(root)
	}
	return out
}

// SupportingRoots returns roots retained as evidence after widening.
func (s AliasSet) SupportingRoots() []AliasRoot {
	all := append([]AliasRoot(nil), s.evidence...)
	if len(all) == 0 && s.precision == AliasKnown {
		all = append(all, s.roots...)
	}
	for i := range all {
		all[i] = cloneRoot(all[i])
	}
	return all
}

func (s AliasSet) Precision() AliasPrecision {
	if s.precision == "" {
		return AliasKnown
	}
	return s.precision
}
func (s AliasSet) IsKnown() bool {
	return s.precision == AliasKnown || (s.precision == "" && len(s.roots) == 0 && len(s.evidence) == 0 && len(s.reasons) == 0)
}
func (s AliasSet) IsUnknown() bool { return s.precision == AliasUnknown }
func (s AliasSet) Complete() bool  { return s.IsKnown() }

func (s AliasSet) Reasons() []AliasReason {
	return append([]AliasReason(nil), s.reasons...)
}

// Join is a may-alias union. Ownership is must information, so disagreement
// for a root is represented by OwnershipUnknown.
func (s AliasSet) Join(other AliasSet) AliasSet {
	reasons := append([]AliasReason(nil), s.reasons...)
	for _, reason := range other.reasons {
		reasons = appendReason(reasons, reason)
	}
	if s.IsUnknown() || other.IsUnknown() {
		evidence := append(s.SupportingRoots(), other.SupportingRoots()...)
		return makeAliasSet(nil, evidence, appendReason(reasons, AliasReason{Code: UnknownAliasReason, Message: "unknown input joined"}), AliasUnknown)
	}
	return makeAliasSet(append(s.roots, other.roots...), append(s.evidence, other.evidence...), reasons, AliasKnown)
}

// Extend returns a set with one actual path step. Paths beyond four steps
// widen to unknown and retain the prior roots only as evidence.
func (s AliasSet) Extend(segment PathSegment) AliasSet {
	if s.IsUnknown() {
		return s
	}
	if !validSegment(segment) {
		return makeAliasSet(nil, s.roots, appendReason(s.reasons, AliasReason{Code: InvalidPathReason, Message: "invalid path segment"}), AliasUnknown)
	}
	out := make([]AliasRoot, 0, len(s.roots))
	for _, root := range s.roots {
		if len(root.Path) >= 4 {
			return makeAliasSet(nil, s.roots, appendReason(s.reasons, AliasReason{Code: AccessPathLimitReason, Message: "path depth exceeds 4"}), AliasUnknown)
		}
		root.Path = append(append([]PathSegment(nil), root.Path...), segment)
		out = append(out, root)
	}
	return makeAliasSet(out, nil, s.reasons, AliasKnown)
}

// SubstituteRoots binds formal root identities to actual roots. A binding is
// by normalized formal index and never by a local source variable name.
func (s AliasSet) SubstituteRoots(bindings map[int]AliasSet) AliasSet {
	if s.IsUnknown() {
		return s
	}
	out := NewAliasSet()
	for _, root := range s.roots {
		if root.Kind == "formal" && strings.HasPrefix(root.ID, "formal:") {
			index, err := strconv.Atoi(strings.TrimPrefix(root.ID, "formal:"))
			if err == nil {
				if actual, ok := bindings[index]; ok {
					if actual.IsUnknown() {
						return actual
					}
					for _, bound := range actual.Roots() {
						bound.Path = append(append([]PathSegment(nil), bound.Path...), root.Path...)
						out = out.Join(NewAliasSet(bound))
					}
					continue
				}
				return UnknownAliasSet(UnknownInputReason)
			}
		}
		out = out.Join(NewAliasSet(root))
	}
	return out
}

// CanStrongUpdate is true only for a singleton, complete, owned, mutable,
// nonescaped root.
func (s AliasSet) CanStrongUpdate() bool {
	if !s.IsKnown() || len(s.roots) != 1 {
		return false
	}
	r := s.roots[0]
	if !r.Mutable || r.Multiple || r.Ownership != OwnershipOwned {
		return false
	}
	for _, segment := range r.Path {
		if segment.Dynamic {
			return false
		}
	}
	return true
}
