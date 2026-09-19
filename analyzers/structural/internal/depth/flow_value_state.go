package depth

import "context"

type TransferStatus string

const (
	TransferOK          TransferStatus = "ok"
	TransferUnsupported TransferStatus = "unsupported"
	TransferPartial     TransferStatus = "partial"
	TransferLimit       TransferStatus = "limit"
	TransferCancelled   TransferStatus = "cancelled"
)

type TransferOptions struct {
	Context      context.Context
	MaxWork      int
	summaries    *functionSummaries
	sharedCharge func(int) bool
}
type TransferEffect struct {
	Kind        string
	RootID      string
	RootKind    string
	Ownership   OwnershipState
	Mutable     bool
	Path        []PathSegment
	Value       RecipeID
	Operands    []RecipeID
	Unknown     bool
	Reason      string
	Instruction string
}
type TransferResult struct {
	NormalGuard       RecipeID
	Rejections        []FlowCompletion
	Status            TransferStatus
	Values            []DomainValue
	Effects           []TransferEffect
	Work              int
	Reasons           []string
	ResourceWitnesses []ResourceLifecycleWitness
}

// TransferState owns one evaluation's mutable state. Record field maps and
// recipes are immutable; snapshots do not expose mutable backing storage.
type TransferState struct {
	values        map[string]DomainValue
	memory        *MemoryStore
	records       map[string]map[string]DomainValue
	effects       []TransferEffect
	reasons       []string
	work          int
	escaped       map[string]bool
	allocations   map[string]bool
	multiple      map[string]bool
	invalidRoots  map[string]bool
	unknownMemory bool
}

func NewTransferState() *TransferState {
	return &TransferState{values: map[string]DomainValue{}, memory: NewMemoryStore(), records: map[string]map[string]DomainValue{}, escaped: map[string]bool{}, allocations: map[string]bool{}, multiple: map[string]bool{}, invalidRoots: map[string]bool{}}
}
func (s *TransferState) Seed(id string, value DomainValue) {
	if s != nil && id != "" {
		s.values[id] = value.Copy()
	}
}
func (s *TransferState) Value(id string) (DomainValue, bool) {
	if s == nil {
		return DomainValue{}, false
	}
	v, ok := s.values[id]
	return effectiveValue(s, v), ok
}
func (s *TransferState) ValuesSnapshot() map[string]DomainValue {
	out := make(map[string]DomainValue, len(s.values))
	for id, value := range s.values {
		out[id] = effectiveValue(s, value)
	}
	return out
}
func (s *TransferState) MemorySnapshot() []MemoryEntry {
	out := s.memory.Snapshot()
	for i := range out {
		out[i].Value = effectiveValue(s, out[i].Value)
	}
	return out
}
func (s *TransferState) EffectsSnapshot() []TransferEffect {
	out := make([]TransferEffect, len(s.effects))
	for i, effect := range s.effects {
		out[i] = effect.Copy()
	}
	return out
}
func (s *TransferState) Reasons() []string { return append([]string(nil), s.reasons...) }
func (s *TransferState) Work() int         { return s.work }
func (e TransferEffect) Copy() TransferEffect {
	e.Path = append([]PathSegment(nil), e.Path...)
	e.Operands = append([]RecipeID(nil), e.Operands...)
	return e
}

func effectiveValue(s *TransferState, v DomainValue) DomainValue {
	out := v.Copy()
	if out.Aliases.IsUnknown() {
		return out
	}
	roots := out.Aliases.Roots()
	for i := range roots {
		roots[i].Multiple = roots[i].Multiple || rootMarked(s.multiple, roots[i])
		if rootMarked(s.escaped, roots[i]) {
			roots[i].Ownership = OwnershipEscaped
		}
	}
	out.Aliases = NewAliasSet(roots...)
	return out
}

// clone is for control-flow branch snapshots, never ordinary instructions.
func (s *TransferState) clone() *TransferState {
	out := NewTransferState()
	for id, value := range s.values {
		out.values[id] = value.Copy()
	}
	for id, fields := range s.records {
		out.records[id] = fields
	}
	out.memory = s.memory.Clone()
	out.effects = s.EffectsSnapshot()
	out.reasons = s.Reasons()
	out.work = s.work
	for id, value := range s.escaped {
		out.escaped[id] = value
	}
	for id, value := range s.invalidRoots {
		out.invalidRoots[id] = value
	}
	for key, value := range s.allocations {
		out.allocations[key] = value
	}
	for key, value := range s.multiple {
		out.multiple[key] = value
	}
	out.unknownMemory = s.unknownMemory
	return out
}

func rootMarked(marked map[string]bool, root AliasRoot) bool {
	for depth := 0; depth <= len(root.Path); depth++ {
		prefix := root
		prefix.Path = root.Path[:depth]
		if marked[aliasKey(prefix)] {
			return true
		}
	}
	return false
}
