package depth

import "sort"

// ValueKind is the resolved kind used by transfer rules. It is intentionally
// independent of source-language syntax.
type ValueKind string

const (
	KindNumeric      ValueKind = "numeric"
	KindError        ValueKind = "error_result"
	KindBoolean      ValueKind = "boolean"
	KindString       ValueKind = "immutable_string"
	KindBytes        ValueKind = "bytes"
	KindReference    ValueKind = "reference"
	KindRecord       ValueKind = "record"
	KindUnknownValue ValueKind = "unknown"
)

// DomainValue carries semantic recipe identity and live aliases separately.
// Dependencies in a recipe are not copied into Aliases.
type DomainValue struct {
	Recipe       RecipeID
	RecordID     string
	Type         string
	Kind         ValueKind
	Concepts     []string
	Aliases      AliasSet
	Dependencies []string
	Unknown      bool
	Reasons      []string
	Bottom       bool
}

// Value is an alias retained for clients that use the shorter name.
type Value = DomainValue

func NewValue(recipe RecipeID, typ string, kind ValueKind, concepts []string, aliases AliasSet) DomainValue {
	if kind == KindNumeric || kind == KindBoolean || kind == KindString {
		aliases = NewAliasSet()
	}
	return DomainValue{Recipe: recipe, Type: typ, Kind: kind, Concepts: append([]string(nil), concepts...), Aliases: aliases}
}

// ScalarValue explicitly discards mutable aliases even when the scalar
// depends on mutable inputs or state.
func ScalarValue(recipe RecipeID, typ string, kind ValueKind, concepts ...string) DomainValue {
	return DomainValue{Recipe: recipe, Type: typ, Kind: kind, Concepts: append([]string(nil), concepts...), Aliases: NewAliasSet()}
}

func UnknownValue(recipe RecipeID, typ string, reason string) DomainValue {
	if reason == "" {
		reason = ReasonUnknownOperand
	}
	return DomainValue{Recipe: recipe, Type: typ, Kind: KindUnknownValue, Aliases: UnknownAliasSet(reason), Unknown: true, Reasons: []string{reason}}
}

// Copy preserves recipe, concepts and aliases exactly.
func (v DomainValue) Copy() DomainValue {
	v.Concepts = append([]string(nil), v.Concepts...)
	v.Dependencies = append([]string(nil), v.Dependencies...)
	v.Reasons = append([]string(nil), v.Reasons...)
	return v
}

// Bind/reference-copy is an alias-preserving transfer.
func (v DomainValue) Bind() DomainValue { return v.Copy() }

// PrimitiveResult discards live aliases while retaining a separate dependency
// list for later recipe/evidence consumers.
func PrimitiveResult(recipe RecipeID, typ string, kind ValueKind, dependencies []string, concepts ...string) DomainValue {
	deps := append([]string(nil), dependencies...)
	sort.Strings(deps)
	return DomainValue{Recipe: recipe, Type: typ, Kind: kind, Concepts: append([]string(nil), concepts...), Dependencies: deps, Aliases: NewAliasSet()}
}

// Join unions may aliases and loses the value's precision when either input
// is unknown. Unknown precision is monotone and cannot recover later.
func (v DomainValue) Join(other DomainValue) DomainValue {
	if v.isBottom() {
		return other.Copy()
	}
	if other.isBottom() {
		return v.Copy()
	}
	out := joinValueIdentity(v, other)
	out.Aliases = v.Aliases.Join(other.Aliases)
	out.Unknown = out.Unknown || out.Aliases.IsUnknown()
	out.Concepts = sortedValueStrings(v.Concepts, other.Concepts)
	out.Dependencies = sortedValueStrings(v.Dependencies, other.Dependencies)
	for _, reason := range out.Aliases.Reasons() {
		out.Reasons = appendUniqueStrings(out.Reasons, reason.Code)
	}
	return out
}

func joinValueIdentity(a, b DomainValue) DomainValue {
	out := a.Copy()
	out.Unknown = a.Unknown || b.Unknown
	out.Reasons = appendUniqueStrings(out.Reasons, b.Reasons...)
	if a.Recipe != b.Recipe {
		out.Recipe = UnknownRecipeID
		out.Unknown = true
		out.Reasons = appendUniqueStrings(out.Reasons, "recipe_join")
	}
	if a.RecordID != b.RecordID {
		out.RecordID = ""
		out.Unknown = true
		out.Reasons = appendUniqueStrings(out.Reasons, "record_join")
	}
	if a.Type != b.Type {
		out.Type = "unknown"
		out.Unknown = true
		out.Reasons = appendUniqueStrings(out.Reasons, "type_join")
	}
	if a.Kind != b.Kind {
		out.Kind = KindUnknownValue
		out.Unknown = true
		out.Reasons = appendUniqueStrings(out.Reasons, "kind_join")
	}
	return out
}
func sortedValueStrings(a, b []string) []string {
	out := append(append([]string(nil), a...), b...)
	sort.Strings(out)
	return dedupeStrings(out)
}

func (v DomainValue) isBottom() bool {
	return v.Bottom || (v.Recipe == "" && v.Type == "" && v.Kind == "" && !v.Unknown && v.Aliases.IsKnown() && len(v.Concepts) == 0 && len(v.Dependencies) == 0 && len(v.Reasons) == 0)
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := in[:0]
	for _, value := range in {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]bool, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = true
	}
	for _, value := range values {
		if !seen[value] {
			dst = append(dst, value)
			seen[value] = true
		}
	}
	sort.Strings(dst)
	return dst
}

// PackValues retains aliases of reference-valued fields while scalar values
// contribute only their recipes/dependencies.
func PackValues(recipe RecipeID, typ string, fields []DomainValue, concepts ...string) DomainValue {
	var aliases AliasSet
	unknown := false
	var deps []string
	var reasons []string
	for _, field := range fields {
		aliases = aliases.Join(field.Aliases)
		unknown = unknown || field.Unknown || field.Aliases.IsUnknown()
		deps = append(deps, field.Dependencies...)
		for _, reason := range field.Reasons {
			reasons = appendUniqueStrings(reasons, reason)
		}
		for _, reason := range field.Aliases.Reasons() {
			reasons = appendUniqueStrings(reasons, reason.Code)
		}
	}
	unknown = unknown || aliases.IsUnknown()
	for _, reason := range aliases.Reasons() {
		reasons = appendUniqueStrings(reasons, reason.Code)
	}
	sort.Strings(deps)
	return DomainValue{Recipe: recipe, Type: typ, Kind: KindRecord, Concepts: append([]string(nil), concepts...), Aliases: aliases, Dependencies: dedupeStrings(deps), Unknown: unknown, Reasons: reasons}
}

// ReturnedAliasCandidate identifies a returned owned mutable alias for later
// leak proofing. Constructing it does not award L.
type ReturnedAliasCandidate struct {
	Root  AliasRoot
	Owned bool
}

func (v DomainValue) ReturnedAliasCandidates() []ReturnedAliasCandidate {
	if v.Kind != KindReference && v.Kind != KindRecord {
		return nil
	}
	var out []ReturnedAliasCandidate
	for _, root := range v.Aliases.Roots() {
		if root.Mutable && root.Ownership == OwnershipOwned {
			out = append(out, ReturnedAliasCandidate{Root: root, Owned: true})
		}
	}
	return out
}
