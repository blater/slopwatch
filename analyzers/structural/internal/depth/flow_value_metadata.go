package depth

import "slopslap.dev/structural/internal/facts"

func suppliedKind(kind facts.FlowValueKind) ValueKind {
	switch kind {
	case facts.FlowKindError:
		return KindError
	case facts.FlowKindNumeric:
		return KindNumeric
	case facts.FlowKindBoolean:
		return KindBoolean
	case facts.FlowKindString:
		return KindString
	case facts.FlowKindBytes:
		return KindBytes
	case facts.FlowKindReference:
		return KindReference
	case facts.FlowKindRecord:
		return KindRecord
	default:
		return KindUnknownValue
	}
}
func referenceValueKind(kind ValueKind) bool {
	return kind == KindError || kind == KindReference || kind == KindRecord || kind == KindBytes
}
func typedUnknown(typ string, kind ValueKind, reason string) DomainValue {
	v := UnknownValue(UnknownRecipeID, typ, reason)
	if kind == KindNumeric || kind == KindBoolean || kind == KindString {
		v.Aliases = NewAliasSet()
	}
	v.Kind = kind
	return v
}
func firstOperand(in facts.Instruction) string {
	if len(in.Operands) == 0 {
		return ""
	}
	return in.Operands[0]
}
func firstReason(reasons []string, fallback string) string {
	if len(reasons) > 0 {
		return reasons[0]
	}
	return fallback
}
func rootFromFact(in facts.AliasRoot) (AliasRoot, bool) {
	if in.ID == "" || in.Path != "" {
		return AliasRoot{}, false
	}
	path, ok := toPath(in.PathSegments)
	if !ok {
		return AliasRoot{}, false
	}
	root := AliasRoot{ID: in.ID, Kind: in.Kind, Path: path, Mutable: in.Mutable, Ownership: OwnershipState(in.Ownership)}
	return root, validRoot(root)
}
func toPath(in []facts.AliasPathSegment) ([]PathSegment, bool) {
	if len(in) > 4 {
		return nil, false
	}
	out := make([]PathSegment, len(in))
	for i, segment := range in {
		out[i] = PathSegment{Kind: segment.Kind, Name: segment.Name, Dynamic: segment.Dynamic}
		if !validSegment(out[i]) {
			return nil, false
		}
	}
	return out, true
}
func accessPath(in facts.Instruction) ([]PathSegment, bool) {
	path, ok := toPath(in.AccessPath)
	if !ok {
		return nil, false
	}
	if in.FieldID != "" {
		path = append([]PathSegment{FieldSegment(in.FieldID)}, path...)
	}
	return path, len(path) > 0 && len(path) <= 4
}
func targetLocations(aliases AliasSet, path []PathSegment) ([]AliasRoot, bool) {
	if !aliases.IsKnown() {
		return nil, false
	}
	roots := aliases.Roots()
	if len(roots) == 0 {
		return nil, false
	}
	for i := range roots {
		if len(roots[i].Path)+len(path) > 4 {
			return nil, false
		}
		roots[i].Path = append(roots[i].Path, path...)
	}
	return roots, true
}
func rootCanStrong(root AliasRoot) bool { return NewAliasSet(root).CanStrongUpdate() }
