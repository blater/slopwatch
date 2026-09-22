package sourceestimate

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestOperationLookupCollisionWork(t *testing.T) {
	for _, language := range []string{"typescript", "rust"} {
		for _, size := range []int{64, 256, 1024} {
			t.Run(fmt.Sprintf("%s/%d", language, size), func(t *testing.T) {
				index := newOperationLookup()
				work := map[string]int{}
				index.work = func(kind string) { work[kind]++ }
				for i := 0; i < size; i++ {
					indexOperation(index, &operation{id: fmt.Sprint(i), name: "run", owner: "Service", language: language, pkg: "p", file: i, exposed: true})
				}
				if work["index_candidate"] != 3*size {
					t.Fatalf("index work=%v", work)
				}
				for i := 0; i < size; i++ {
					caller := &operation{language: language, pkg: "p", file: size + i, fieldTypes: map[string]string{"field": "Service"}}
					matches := resolveCall(caller, call{name: "field.run"}, nil, index)
					if matches.count() != size || matches.unique() != nil {
						t.Fatalf("ambiguity changed: %+v", matches)
					}
				}
				if work["query"] != size || work["edge_candidate"] != 0 || work["index_candidate"] != 3*size {
					t.Fatalf("queries scanned collisions: %v", work)
				}
				// Repeated syntactically distinct calls share a selected bucket. The
				// graph needs all edges once, while ordinary measurements need none.
				caller := &operation{id: "caller", language: language, pkg: "p", file: size, fieldTypes: map[string]string{"field": "Service"}}
				caller.body, _, _ = lex([]byte(strings.Repeat("field.run();", 32)))
				edges := appendResolvedCalls(nil, caller, nil, index)
				if len(edges) != size || work["edge_candidate"] != size {
					t.Fatalf("graph repeated its bucket: edges=%d work=%v", len(edges), work)
				}
			})
		}
	}
}

func TestOperationLookupExcludedFileDoesNotScanItsCandidates(t *testing.T) {
	index := newOperationLookup()
	for i := 0; i < 1024; i++ {
		indexOperation(index, &operation{id: fmt.Sprint(i), name: "run", owner: "S", language: "rust", file: 0, exposed: true})
	}
	unique := &operation{id: "unique", name: "run", owner: "S", language: "rust", file: 1, exposed: true}
	indexOperation(index, unique)
	for i := 0; i < 1024; i++ {
		indexOperation(index, &operation{id: fmt.Sprint(i + 1024), name: "run", owner: "S", language: "rust", file: 0, exposed: true})
	}
	caller := &operation{language: "rust", file: 0}
	selection := findWorkspaceCallMatches(caller, "run", "S", index)
	if selection.count() != 1 || selection.unique() != unique {
		t.Fatal("excluded-file unique identity changed")
	}
	work := map[string]int{}
	index.work = func(kind string) { work[kind]++ }
	var got []*operation
	selection.each(index, func(op *operation) { got = append(got, op) })
	if !reflect.DeepEqual(got, []*operation{unique}) || work["edge_candidate"] != 1 || work["edge_run"] != 3 {
		t.Fatalf("excluded file scanned: %v", work)
	}
}

func TestOperationLookupMatchesReferenceScopesAndImports(t *testing.T) {
	for _, language := range []string{"java", "go", "typescript", "rust"} {
		var ops []*operation
		for file := 0; file < 4; file++ {
			for _, owner := range []string{"", "S", "Other"} {
				for _, exposed := range []bool{false, true} {
					ops = append(ops, &operation{id: fmt.Sprintf("%s/%d/%s/%v", language, file, owner, exposed), language: language, pkg: "p", file: file, owner: owner, name: "run", exposed: exposed})
				}
			}
		}
		units := []unit{{file: File{Path: "a.ts"}}, {file: File{Path: "b.ts"}}, {file: File{Path: "c.ts"}}, {file: File{Path: "d.ts"}}}
		index := newOperationLookup()
		reference := map[string][]*operation{}
		for _, op := range ops {
			indexOperation(index, op)
			reference[operationKey(op)] = append(reference[operationKey(op)], op)
			if language == "rust" || language == "typescript" {
				key := workspaceOperationKey(op, op.name, op.owner)
				reference[key] = append(reference[key], op)
			}
		}
		for _, owner := range []string{"", "S", "Contextual"} {
			for file := 0; file < 5; file++ {
				caller := &operation{language: language, pkg: "p", file: file, owner: owner, fieldTypes: map[string]string{"field": "S"}, imports: map[string]sourceImport{"alias": {file: "b.ts", name: "S", index: 1, pkg: "p"}, "module": {file: "b.ts", name: "*", index: 1, pkg: "p"}, "missing": {file: "bad.ts", name: "S", index: 1, pkg: "p"}}}
				for _, name := range []string{"run", "S.run", "field.run", "Other.run", "this.run", "self.run", "alias.run", "module.run", "missing.run", "absent"} {
					c := call{name: name}
					want := referenceResolveCall(caller, c, units, reference)
					selection := resolveCall(caller, c, units, index)
					var got []*operation
					selection.each(index, func(op *operation) { got = append(got, op) })
					if len(got) != len(want) || len(want) > 0 && !reflect.DeepEqual(got, want) {
						t.Fatalf("%s/%s/%d/%s: got=%v want=%v", language, owner, file, name, got, want)
					}
					if selection.count() != len(want) || len(want) == 1 && selection.unique() != want[0] {
						t.Fatal("status disagrees with enumeration")
					}
				}
			}
		}
	}
}

func TestOperationLookupRebuiltAfterAnnotations(t *testing.T) {
	op := &operation{language: "rust", owner: "Before", name: "run", file: 1, exposed: false}
	units := []unit{{ops: []*operation{op}}}
	before := operationIndex(units)
	op.owner, op.exposed = "After", true
	after := operationIndex(units)
	caller := &operation{language: "rust", file: 0}
	if findWorkspaceCallMatches(caller, "run", "After", before).count() != 0 || findWorkspaceCallMatches(caller, "run", "After", after).unique() != op || findCallMatches(caller, "run", "Before", after).count() != 0 {
		t.Fatal("rebuilt lookup retained stale annotation keys")
	}
}

func referenceResolveCall(caller *operation, c call, units []unit, byKey map[string][]*operation) []*operation {
	if matches, handled := referenceResolveTypeScriptImportedCall(caller, c, units, byKey); handled {
		return matches
	}
	name, owner, hasReceiverType := referenceResolveCallOwner(caller, c.name)
	matches := referenceFindCallMatches(caller, name, owner, byKey)
	if len(matches) == 0 && owner != "" && referenceCrossFileCallAllowed(caller, owner, hasReceiverType) {
		matches = referenceFindWorkspaceCallMatches(caller, name, owner, byKey)
	}
	if len(matches) == 0 && owner != "" && caller.owner != "" {
		matches = referenceFindCallMatches(caller, name, "", byKey)
	}
	return matches
}

func referenceResolveCallOwner(caller *operation, callName string) (string, string, bool) {
	name, owner := callName, ""
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		owner, name = name[:dot], name[dot+1:]
	}
	receiverType, hasReceiverType := operationFieldType(caller, owner)
	if !hasReceiverType {
		if dot := strings.LastIndexByte(owner, '.'); dot >= 0 {
			receiverType, hasReceiverType = operationFieldType(caller, owner[dot+1:])
		}
	}
	if hasReceiverType {
		owner = receiverType
	}
	if owner == "" || owner == "this" || owner == "self" {
		owner = caller.owner
	}
	return name, owner, hasReceiverType
}

func referenceFindCallMatches(caller *operation, name, owner string, byKey map[string][]*operation) []*operation {
	matches := make([]*operation, 0)
	for _, candidate := range byKey[scopedOperationKey(caller, name, owner)] {
		if !referenceSameCallScope(caller, candidate, owner) {
			continue
		}
		matches = append(matches, candidate)
	}
	return matches
}

func referenceSameCallScope(caller, candidate *operation, owner string) bool {
	if (caller.language == "typescript" || caller.language == "rust") && candidate.file != caller.file {
		return false
	}
	if owner != "" && candidate.owner != "" && candidate.owner != owner {
		return false
	}
	return candidate.file == caller.file || sameLanguage(candidate.language, caller.language)
}

func referenceCrossFileCallAllowed(caller *operation, owner string, hasReceiverType bool) bool {
	if caller.language != "typescript" && caller.language != "rust" {
		return false
	}
	explicitOwner := hasReceiverType
	if caller.language == "rust" {
		explicitOwner = explicitOwner || owner != caller.owner
	}
	if caller.language == "typescript" {
		_, explicitImport := caller.imports[owner]
		explicitOwner = explicitOwner || explicitImport
	}
	return explicitOwner
}

func referenceFindWorkspaceCallMatches(caller *operation, name, owner string, byKey map[string][]*operation) []*operation {
	matches := make([]*operation, 0)
	for _, candidate := range byKey[workspaceOperationKey(caller, name, owner)] {
		if candidate.file != caller.file && candidate.owner == owner && candidate.exposed {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func referenceResolveTypeScriptImportedCall(caller *operation, c call, units []unit, byKey map[string][]*operation) ([]*operation, bool) {
	if caller.language != "typescript" {
		return nil, false
	}
	alias, member := c.name, ""
	if dot := strings.IndexByte(alias, '.'); dot >= 0 {
		alias, member = alias[:dot], alias[dot+1:]
	}
	binding, ok := caller.imports[alias]
	if !ok {
		return nil, false
	}
	name := binding.name
	if name == "*" {
		name = member
	}
	if name == "" || strings.Contains(name, ".") {
		return nil, true
	}
	matches := []*operation{}
	lookup := *caller
	lookup.file = binding.index
	lookup.pkg = binding.pkg
	owner := ""
	if member != "" && binding.name != "*" {
		owner, name = binding.name, member
	}
	for _, candidate := range byKey[scopedOperationKey(&lookup, name, owner)] {
		if candidate.owner == owner && candidate.exposed && candidate.file >= 0 && candidate.file < len(units) && units[candidate.file].file.Path == binding.file {
			matches = append(matches, candidate)
		}
	}
	return matches, true
}

func TestRustVisibilityLookupUsesStatusBuckets(t *testing.T) {
	for _, size := range []int{64, 256, 1024} {
		ops := make([]*operation, 0, size)
		reference := map[string][]*operation{}
		for i := 0; i < size; i++ {
			op := &operation{id: fmt.Sprint(i), language: "rust", pkg: fmt.Sprint(i), file: i, name: "run", owner: "S", exposed: i%2 == 0}
			ops = append(ops, op)
			for _, key := range []string{rustCallKey(op.pkg, op.name, op.owner), rustWorkspaceCallKey(op.pkg, op.name, op.owner)} {
				reference[key] = append(reference[key], op)
			}
		}
		index := rustCallIndex(ops)
		work := map[string]int{}
		index.work = func(kind string) { work[kind]++ }
		for i := 0; i < size; i++ {
			caller := &operation{language: "rust", pkg: "other", file: i, owner: "C", fieldTypes: map[string]string{"field": "S"}}
			for _, name := range []string{"field.run", "S.run", "S::run", "run"} {
				want := referenceRustResolveCall(caller, call{name: name}, reference)
				got := rustResolveCall(caller, call{name: name}, index)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("size=%d caller=%d name=%s: resolution changed", size, i, name)
				}
			}
		}
		if work["query"] != 4*size || work["edge_candidate"] != 0 {
			t.Fatalf("Rust unique lookup enumerated: %v", work)
		}
	}
}

func referenceRustResolveCall(caller *operation, call call, index map[string][]*operation) []*operation {
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
		if receiverType, ok := operationFieldType(caller, owner); ok {
			owner = receiverType
		} else if dot := strings.LastIndexByte(owner, '.'); dot >= 0 {
			if receiverType, ok := operationFieldType(caller, owner[dot+1:]); ok {
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

func TestRustAcquisitionBucketSummaryKeepsLastWriter(t *testing.T) {
	u := gradedSurfaceUnit("java", "Driver.java", `class Driver { boolean active; void acquire() { active=true; } void release() { active=false; } void noop() {} }`)
	methods := map[string]*operation{}
	for _, op := range u.ops {
		methods[op.name] = op
	}
	for _, order := range [][]string{{"acquire", "release", "noop"}, {"release", "acquire", "noop"}} {
		index := newOperationLookup()
		for _, name := range order {
			if methods[name] == nil {
				t.Fatalf("missing method %s", name)
			}
			index.add(index.scoped, "selected", methods[name])
		}
		selection := callSelection{bucket: index.scoped["selected"]}
		reset := map[string]string{"active": "false"}
		want := map[string]bool{}
		for _, op := range selection.bucket.candidates {
			for effect, state := range reset {
				if gradedOperationWritesField(op, u, effect) {
					want[effect] = gradedHasBooleanWrite(op, []unit{u}, effect, gradedOppositeBoolean(state))
				}
			}
		}
		if len(want) != 1 {
			t.Fatal("fixture has no field-write proof")
		}
		visits := 0
		index.work = func(kind string) {
			if kind == "edge_candidate" {
				visits++
			}
		}
		cache := map[callSelection]map[string]bool{}
		for i := 0; i < 128; i++ {
			got := gradedRustAcquisitionEffects(selection, index, []unit{u}, reset, cache)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("order=%v got=%v want=%v", order, got, want)
			}
		}
		if visits != len(order) {
			t.Fatalf("repeated acquisition calls enumerated %d candidates", visits)
		}
	}
}
