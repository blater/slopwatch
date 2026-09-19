package depth

// Parents are immutable predecessor stores. Only the current block appends
// writes; sharing reconverged stores avoids copying or replaying whole histories.
type memoryParent struct {
	store *MemoryStore
	guard RecipeID
}
type memoryRead struct {
	value           DomainValue
	found, complete bool
}
type memoryQuery struct {
	root     AliasRoot
	fallback func(AliasRoot, []PathSegment) DomainValue
	charge   func(int) bool
	memo     map[*MemoryStore]memoryRead
}

func (q *memoryQuery) read(m *MemoryStore) memoryRead {
	if !q.charge(1) {
		return memoryRead{}
	}
	if result, ok := q.memo[m]; ok {
		return result
	}
	result := q.base(m)
	if !result.complete {
		return result
	}
	first, second := m.query(q.root)
	if !q.charge(len(first) + len(second) + indexSearchWork(len(m.writes))) {
		return memoryRead{}
	}
	for _, index := range mergeWriteIndexes(first, second) {
		write := m.writes[index]
		if !q.charge(1 + len(q.root.Path) + valueWork(result.value) + valueWork(write.Value)) {
			return memoryRead{}
		}
		if !pathsOverlap(q.root.Path, write.Location.Path) {
			continue
		}
		result.value = joinMemoryWrite(q.root, result.value, write)
		result.found = true
	}
	q.memo[m] = result
	return result
}
func (q *memoryQuery) base(m *MemoryStore) memoryRead {
	result := memoryRead{complete: true}
	// An exact strong assignment replaces every earlier value, including the
	// branch history. Query starts at that write, so no prior read is needed.
	if m.lastStrong[aliasKey(q.root)] > 0 && !dynamicPath(q.root.Path) {
		return result
	}
	if len(m.parents) == 0 {
		if q.fallback != nil {
			result.value = q.fallback(q.root, nil)
		}
		return result
	}
	for i, parent := range m.parents {
		next := q.read(parent.store)
		if !next.complete {
			return next
		}
		if !q.charge(valueWork(next.value) + valueWork(result.value)) {
			return memoryRead{}
		}
		if i == 0 {
			result = next
			continue
		}
		result.value = selectControlRecord(m.arena, m.records, parent.guard, next.value, result.value, q.charge)
		result.found = result.found || next.found
	}
	return result
}

// locations visits each shared store once; callers use current reads, not old
// stored values, to determine containment after overwritten assignments.
func (m *MemoryStore) locations(root AliasRoot, charge func(int) bool) ([]memoryWrite, bool) {
	queue := []*MemoryStore{m}
	seen := map[*MemoryStore]bool{}
	locations := map[string]bool{}
	var out []memoryWrite
	for len(queue) > 0 {
		if !charge(1) {
			return nil, false
		}
		store := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[store] {
			continue
		}
		seen[store] = true
		for _, p := range store.parents {
			if !charge(1) {
				return nil, false
			}
			queue = append(queue, p.store)
		}
		for _, index := range store.byRoot[rootIdentity(root)] {
			w := store.writes[index]
			if !charge(1 + len(w.Root.Path)) {
				return nil, false
			}
			key := aliasKey(w.Root)
			if locations[key] {
				continue
			}
			locations[key] = true
			out = append(out, w)
		}
	}
	return out, true
}
