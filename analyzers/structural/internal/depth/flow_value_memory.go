package depth

import "sort"

type MemoryLocation struct {
	RootID   string
	RootKind string
	Path     []PathSegment
}
type MemoryEntry struct {
	Location MemoryLocation
	Value    DomainValue
	Strong   bool
	Sequence uint64
}
type memoryWrite struct {
	MemoryEntry
	Root AliasRoot
}

// MemoryStore retains ordered writes indexed by exact location and wildcard
// root. An exact read starts at its last strong write; superseded history does
// not get rescanned. Dynamic reads explicitly pay for every possible location.
type MemoryStore struct {
	records    map[string]map[string]DomainValue
	writes     []memoryWrite
	byRoot     map[string][]int
	byExact    map[string][]int
	wildcard   map[string][]int
	lastStrong map[string]int
	nextSeq    uint64
	parents    []memoryParent
	arena      *RecipeArena
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byRoot: map[string][]int{}, byExact: map[string][]int{}, wildcard: map[string][]int{}, lastStrong: map[string]int{}}
}
func (m *MemoryStore) Write(root AliasRoot, path []PathSegment, value DomainValue, strong bool) {
	root = cloneRoot(root)
	root.Path = append(root.Path, path...)
	m.nextSeq++
	strong = strong && rootCanStrong(root)
	entry := memoryWrite{Root: root, MemoryEntry: MemoryEntry{Location: MemoryLocation{RootID: root.ID, RootKind: root.Kind, Path: root.Path}, Value: value.Copy(), Strong: strong, Sequence: m.nextSeq}}
	index := len(m.writes)
	m.writes = append(m.writes, entry)
	key := rootIdentity(root)
	m.byRoot[key] = append(m.byRoot[key], index)
	if dynamicPath(root.Path) {
		m.wildcard[key] = append(m.wildcard[key], index)
		return
	}
	location := aliasKey(root)
	m.byExact[location] = append(m.byExact[location], index)
	if strong {
		m.lastStrong[location] = index + 1
	}
}
func (m *MemoryStore) query(root AliasRoot) ([]int, []int) {
	if dynamicPath(root.Path) {
		return m.byRoot[rootIdentity(root)], nil
	}
	key := aliasKey(root)
	cutoff := m.lastStrong[key] - 1
	exact, wild := m.byExact[key], m.wildcard[rootIdentity(root)]
	return exact[sort.SearchInts(exact, cutoff):], wild[sort.SearchInts(wild, cutoff):]
}
func indexSearchWork(n int) int {
	work := 1
	for n > 0 {
		work++
		n /= 2
	}
	return work * 2
}
func (m *MemoryStore) Read(root AliasRoot, path []PathSegment) (DomainValue, bool) {
	return m.ReadWithFallback(root, path, nil)
}
func (m *MemoryStore) ReadWithFallback(root AliasRoot, path []PathSegment, fallback func(AliasRoot, []PathSegment) DomainValue) (DomainValue, bool) {
	remaining := defaultMaxWork
	charge := func(n int) bool {
		if n > remaining {
			return false
		}
		remaining -= n
		return true
	}
	value, found, complete := m.readBounded(root, path, fallback, charge)
	if !complete {
		return UnknownValue(UnknownRecipeID, value.Type, "work_limit"), true
	}
	return value, found
}
func (m *MemoryStore) readBounded(root AliasRoot, path []PathSegment, fallback func(AliasRoot, []PathSegment) DomainValue, charge func(int) bool) (DomainValue, bool, bool) {
	root = cloneRoot(root)
	root.Path = append(root.Path, path...)
	query := memoryQuery{root: root, fallback: fallback, charge: charge, memo: map[*MemoryStore]memoryRead{}}
	result := query.read(m)
	return result.value, result.found, result.complete
}

func joinMemoryWrite(root AliasRoot, value DomainValue, write memoryWrite) DomainValue {
	if write.Strong && pathEqual(root.Path, write.Location.Path) {
		return write.Value.Copy()
	}
	if value.isBottom() {
		value = UnknownValue(UnknownRecipeID, write.Value.Type, "prior_storage")
	}
	return value.Join(write.Value)
}
func mergeWriteIndexes(a, b []int) []int {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	out := make([]int, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	out = append(out, a[i:]...)
	return append(out, b[j:]...)
}
func (m *MemoryStore) Snapshot() []MemoryEntry {
	out := make([]MemoryEntry, len(m.writes))
	for i, w := range m.writes {
		out[i] = w.MemoryEntry
		out[i].Location.Path = append([]PathSegment(nil), w.Location.Path...)
		out[i].Value = w.Value.Copy()
	}
	return out
}
func (m *MemoryStore) Clone() *MemoryStore {
	out := NewMemoryStore()
	out.parents = append([]memoryParent(nil), m.parents...)
	out.arena = m.arena
	out.records = m.records
	for _, w := range m.writes {
		out.Write(w.Root, nil, w.Value, w.Strong)
	}
	return out
}
