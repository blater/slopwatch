package sourceestimate

import (
	"sort"
	"strings"
)

// Module state connects public routes only when their eager effects refer to
// the same declared storage. Mere co-location or matching method names do not
// turn a file into an abstraction.
type typeScriptModuleUse struct{ read, write, scalarState, scalarComputed bool }

func typeScriptModuleGroups(u unit, units []unit, index map[string][]*operation) map[string]string {
	storage := typeScriptModuleStorage(u.tokens)
	if len(storage) == 0 {
		return nil
	}
	roots := []*operation{}
	uses := map[string]map[string]typeScriptModuleUse{}
	for _, op := range u.ops {
		if op.owner != "" || !op.exposed {
			continue
		}
		roots = append(roots, op)
		visits := 0
		uses[op.id] = typeScriptModuleUses(op, storage, units, index, map[string]bool{}, nil, &visits, 0)
	}
	parent := make([]int, len(roots))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	users := map[string][]int{}
	writers := map[string]bool{}
	for i, root := range roots {
		for field, use := range uses[root.id] {
			if use.read || use.write {
				users[field] = append(users[field], i)
			}
			writers[field] = writers[field] || use.write
		}
	}
	for field, indices := range users {
		if !writers[field] || len(indices) < 2 {
			continue
		}
		first := indices[0]
		for _, i := range indices[1:] {
			parent[find(i)] = find(first)
		}
	}
	members := map[int][]int{}
	for i := range roots {
		p := find(i)
		members[p] = append(members[p], i)
	}
	groupFields := map[int][]string{}
	for field, indices := range users {
		if writers[field] && len(indices) > 1 {
			p := find(indices[0])
			groupFields[p] = append(groupFields[p], field)
		}
	}
	result := map[string]string{}
	for p, group := range members {
		if len(group) < 2 {
			continue
		}
		fields := groupFields[p]
		sort.Strings(fields)
		key := "module-state:" + strings.Join(fields, ",")
		for _, i := range group {
			result[roots[i].id] = key
		}
	}
	return result
}
