package depth

import (
	"sort"

	"slopslap.dev/structural/internal/facts"
)

type resourceLifecycleEvent struct {
	kind        string
	block       string
	index       int
	instruction string
	ownership   OwnershipState
	guard       RecipeID
}

type resourceLifecycleRoot struct {
	key    string
	events []resourceLifecycleEvent
}

type resourceEdgeKey struct {
	from, to string
	kind     facts.EdgeKind
}

type resourceEventCollection struct {
	roots             map[string]*resourceLifecycleRoot
	escaped           map[string]bool
	ambiguousCleanup  map[string]bool
	allEffectsProven  bool
	resourceEventSeen bool
	reasons           []string
}

func collectResourceEvents(summaries *functionSummaries, function *CompiledFunction, evaluation FlowEvaluation) resourceEventCollection {
	out := resourceEventCollection{roots: map[string]*resourceLifecycleRoot{}, escaped: map[string]bool{}, ambiguousCleanup: map[string]bool{}, allEffectsProven: true}
	locations, indexed := resourceInstructionLocations(function, summaries.budget.charge)
	if !indexed {
		out.allEffectsProven = false
		out.reasons = append(out.reasons, "work_limit")
		return out
	}
	cleanupTargets := map[string]map[string]bool{}
	for _, guarded := range evaluation.Effects {
		if !collectResourceEvent(summaries, &out, cleanupTargets, locations, guarded) {
			out.allEffectsProven = false
		}
	}
	return out
}

func collectResourceEvent(summaries *functionSummaries, out *resourceEventCollection, cleanupTargets map[string]map[string]bool, locations map[string]resourceInstructionLocation, guarded GuardedEffect) bool {
	effect := guarded.Effect
	if effect.Unknown {
		out.reasons = append(out.reasons, "unknown_resource_effect")
		return false
	}
	if !supportedResourceEffect(effect.Kind) {
		out.reasons = append(out.reasons, "unsupported_effect_kind")
		if effect.Kind == string(facts.OpEscape) && effect.RootID != "" {
			key := aliasKey(AliasRoot{ID: effect.RootID, Kind: effect.RootKind, Path: append([]PathSegment(nil), effect.Path...)})
			out.escaped[key] = true
		}
		return false
	}
	root := AliasRoot{ID: effect.RootID, Kind: effect.RootKind, Path: append([]PathSegment(nil), effect.Path...), Ownership: effect.Ownership, Mutable: effect.Mutable}
	if root.ID == "" || effect.Instruction == "" {
		out.reasons = append(out.reasons, "incomplete_resource_effect")
		return false
	}
	key := aliasKey(root)
	markAmbiguousCleanup(out, cleanupTargets, effect.Kind, effect.Instruction, key)
	location, located := locations[effect.Instruction]
	if !located || location.block != guarded.Block || string(location.opcode) != effect.Kind {
		out.reasons = append(out.reasons, "resource_effect_location_unknown")
		return false
	}
	if guarded.Guard == "" {
		out.reasons = append(out.reasons, "resource_event_guard_unknown")
		return false
	}
	if _, known := summaries.arena.Lookup(guarded.Guard); !known {
		out.reasons = append(out.reasons, "resource_event_guard_unknown")
		return false
	}
	entry := out.roots[key]
	if entry == nil {
		entry = &resourceLifecycleRoot{key: key}
		out.roots[key] = entry
	}
	entry.events = append(entry.events, resourceLifecycleEvent{kind: effect.Kind, block: guarded.Block, index: location.index, instruction: effect.Instruction, ownership: effect.Ownership, guard: guarded.Guard})
	out.resourceEventSeen = true
	return true
}

func supportedResourceEffect(kind string) bool {
	return kind == string(facts.OpAcquire) || kind == string(facts.OpUseResource) || kind == string(facts.OpCleanupAttempt)
}

func markAmbiguousCleanup(out *resourceEventCollection, cleanupTargets map[string]map[string]bool, kind, instruction, root string) {
	if kind != string(facts.OpCleanupAttempt) {
		return
	}
	if cleanupTargets[instruction] == nil {
		cleanupTargets[instruction] = map[string]bool{}
	}
	cleanupTargets[instruction][root] = true
	if len(cleanupTargets[instruction]) < 2 {
		return
	}
	for target := range cleanupTargets[instruction] {
		out.ambiguousCleanup[target] = true
	}
}

func proveResourceRoots(summaries *functionSummaries, function *CompiledFunction, evaluation FlowEvaluation, collected resourceEventCollection) ([]ResourceLifecycleWitness, []string, bool) {
	keys := make([]string, 0, len(collected.roots))
	for key := range collected.roots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var witnesses []ResourceLifecycleWitness
	var reasons []string
	proven := true
	for _, key := range keys {
		root := collected.roots[key]
		if collected.escaped[key] {
			proven = false
			reasons = append(reasons, "resource_escaped")
			continue
		}
		if collected.ambiguousCleanup[key] {
			proven = false
			reasons = append(reasons, "ambiguous_resource_cleanup")
			continue
		}
		sortResourceEvents(root.events)
		ok, reason, acquireGuard := proveResourceRoot(summaries.arena, function, root, evaluation, summaries.budget.charge)
		if !ok {
			proven = false
			if reason != "" {
				reasons = append(reasons, reason)
			}
			continue
		}
		if acquireGuard == "" {
			proven = false
			reasons = append(reasons, "missing_resource_acquisition")
			continue
		}
		evidence := make([]string, 0, len(root.events))
		for _, event := range root.events {
			evidence = appendUniqueStrings(evidence, event.instruction)
		}
		witnesses = append(witnesses, ResourceLifecycleWitness{Guard: acquireGuard, Governed: root.key, Rule: "owned_acquire_cleanup_every_exit", Evidence: evidence})
	}
	return witnesses, reasons, proven
}

func sortResourceEvents(events []resourceLifecycleEvent) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].block != events[j].block {
			return events[i].block < events[j].block
		}
		if events[i].index != events[j].index {
			return events[i].index < events[j].index
		}
		return events[i].kind < events[j].kind
	})
}

func resourceReturned(root *resourceLifecycleRoot, evaluation FlowEvaluation) bool {
	if root == nil {
		return true
	}
	return resourceReturnedKey(root.key, evaluation)
}

func resourceReturnedKey(key string, evaluation FlowEvaluation) bool {
	for _, completion := range evaluation.Completions {
		for _, value := range completion.Values {
			if value.Aliases.IsUnknown() {
				if referenceValueKind(value.Kind) {
					return false
				}
				continue
			}
			for _, alias := range value.Aliases.Roots() {
				candidate := aliasKey(alias)
				if candidate == key {
					return false
				}
			}
		}
	}
	return true
}

type resourceInstructionLocation struct {
	block  string
	index  int
	opcode facts.Opcode
}

func resourceInstructionLocations(function *CompiledFunction, charge func(int) bool) (map[string]resourceInstructionLocation, bool) {
	locations := map[string]resourceInstructionLocation{}
	for _, blockID := range function.reachable {
		if !charge(1) {
			return nil, false
		}
		block, ok := function.blocks[blockID]
		if !ok {
			return nil, false
		}
		for index, in := range block.Block.Instructions[:activeInstructionCount(block.Block)] {
			if !charge(1) {
				return nil, false
			}
			locations[in.ID] = resourceInstructionLocation{block: blockID, index: index, opcode: in.Opcode}
		}
	}
	return locations, true
}

type resourceWalkKey struct {
	block string
	path  guardID
	live  guardID
}

type resourceWalker struct {
	arena         *RecipeArena
	function      *CompiledFunction
	root          *resourceLifecycleRoot
	charge        func(int) bool
	guards        *controlGuards
	resolvedEdges map[resourceEdgeKey]guardID
	exits         map[string]guardID
	byBlock       map[string]map[int][]resourceLifecycleEvent
	memo          map[resourceWalkKey]bool
	visiting      map[resourceWalkKey]bool
	sawAcquire    bool
	acquireGuard  guardID
}

func proveResourceRoot(arena *RecipeArena, function *CompiledFunction, root *resourceLifecycleRoot, evaluation FlowEvaluation, charge func(int) bool) (bool, string, RecipeID) {
	if function == nil || root == nil || len(root.events) == 0 {
		return false, "missing_resource_events", ""
	}
	byBlock := resourceEventsByBlock(root.events)
	guards := newControlGuards(arena, charge)
	resolvedEdges, reason := resourceResolvedEdges(guards, evaluation, charge)
	if reason != "" {
		return false, reason, ""
	}
	exits, reason := resourceExitGuards(guards, evaluation)
	if reason != "" {
		return false, reason, ""
	}
	walker := &resourceWalker{arena: arena, function: function, root: root, charge: charge, guards: guards, resolvedEdges: resolvedEdges, exits: exits, byBlock: byBlock, memo: map[resourceWalkKey]bool{}, visiting: map[resourceWalkKey]bool{}}
	ok, reason := walker.walk(function.function.Entry, guardTrue, guardFalse)
	if ok && !walker.sawAcquire {
		return false, "missing_resource_acquisition", ""
	}
	if !ok {
		return false, reason, ""
	}
	return true, "", guards.recipe(walker.acquireGuard)
}

func resourceEventsByBlock(events []resourceLifecycleEvent) map[string]map[int][]resourceLifecycleEvent {
	byBlock := map[string]map[int][]resourceLifecycleEvent{}
	for _, event := range events {
		if byBlock[event.block] == nil {
			byBlock[event.block] = map[int][]resourceLifecycleEvent{}
		}
		byBlock[event.block][event.index] = append(byBlock[event.block][event.index], event)
	}
	return byBlock
}

func resourceExitGuards(guards *controlGuards, evaluation FlowEvaluation) (map[string]guardID, string) {
	if len(evaluation.Completions) == 0 {
		return nil, "missing_resource_exit"
	}
	exits := map[string]guardID{}
	for _, completion := range evaluation.Completions {
		if completion.Block == "" || completion.Guard == "" {
			return nil, "resource_exit_unknown"
		}
		if _, known := guards.arena.Lookup(completion.Guard); !known {
			return nil, "resource_exit_guard_unknown"
		}
		exits[completion.Block] = guards.or(exits[completion.Block], guards.atom(completion.Guard))
	}
	return exits, ""
}

func (w *resourceWalker) walk(blockID string, path, live guardID) (bool, string) {
	if !w.charge(1) {
		return false, "work_limit"
	}
	if path == guardFalse {
		return true, ""
	}
	key := resourceWalkKey{block: blockID, path: path, live: live}
	if result, ok := w.memo[key]; ok {
		return result, ""
	}
	if w.visiting[key] {
		if live != guardFalse {
			return false, "resource_lifecycle_loop"
		}
		return true, ""
	}
	block, ok := w.function.blocks[blockID]
	if !ok {
		return false, "resource_exit_unknown"
	}
	w.visiting[key] = true
	current, reason := w.scanBlock(blockID, block.Block, path, live)
	if reason != "" {
		return w.fail(key, reason)
	}
	edges := w.function.Successors(blockID)
	if len(edges) == 0 {
		return w.finishTerminal(key, blockID, path, current)
	}
	return w.followEdges(key, edges, path, current)
}

func (w *resourceWalker) scanBlock(blockID string, block facts.FlowBlock, path, live guardID) (guardID, string) {
	current := live
	for index := range block.Instructions[:activeInstructionCount(block)] {
		if !w.charge(1) {
			return current, "work_limit"
		}
		for _, event := range w.byBlock[blockID][index] {
			w.observeAcquire(path, event)
			var reason string
			current, reason = applyResourceEvent(w.guards, current, path, event)
			if reason != "" {
				return current, reason
			}
		}
	}
	return current, ""
}

func (w *resourceWalker) observeAcquire(path guardID, event resourceLifecycleEvent) {
	if event.kind != string(facts.OpAcquire) {
		return
	}
	eventGuard := guardTrue
	if event.guard != "" {
		eventGuard = w.guards.atom(event.guard)
	}
	if w.guards.and(path, eventGuard) != guardFalse {
		w.sawAcquire = true
		w.acquireGuard = w.guards.or(w.acquireGuard, w.guards.and(path, eventGuard))
	}
}

func (w *resourceWalker) finishTerminal(key resourceWalkKey, blockID string, path, live guardID) (bool, string) {
	exitGuard, covered := w.exits[blockID]
	if !covered {
		return w.fail(key, "resource_exit_unknown")
	}
	if w.guards.and(path, w.guards.not(exitGuard)) != guardFalse {
		return w.fail(key, "resource_exit_coverage_unknown")
	}
	if w.guards.and(live, exitGuard) != guardFalse {
		return w.finish(key, false, "resource_missing_cleanup")
	}
	return w.finish(key, true, "")
}

func (w *resourceWalker) followEdges(key resourceWalkKey, edges []facts.FlowEdge, path, live guardID) (bool, string) {
	for _, edge := range edges {
		if !w.charge(1) {
			return w.fail(key, "work_limit")
		}
		edgeGuard, reason := resourceEdgeGuard(w.guards, w.resolvedEdges, edge)
		if reason != "" {
			return w.fail(key, reason)
		}
		if ok, reason := w.walk(edge.To, w.guards.and(path, edgeGuard), w.guards.and(live, edgeGuard)); !ok {
			return w.fail(key, reason)
		}
	}
	return w.finish(key, true, "")
}

func (w *resourceWalker) finish(key resourceWalkKey, result bool, reason string) (bool, string) {
	delete(w.visiting, key)
	w.memo[key] = result
	return result, reason
}

func (w *resourceWalker) fail(key resourceWalkKey, reason string) (bool, string) {
	return w.finish(key, false, reason)
}

func applyResourceEvent(guards *controlGuards, live, path guardID, event resourceLifecycleEvent) (guardID, string) {
	eventGuard := guardTrue
	if event.guard != "" {
		eventGuard = guards.atom(event.guard)
	}
	active := guards.and(path, eventGuard)
	if active == guardFalse {
		return live, ""
	}
	if event.ownership != OwnershipOwned {
		return live, "resource_ownership_unknown"
	}
	switch event.kind {
	case string(facts.OpAcquire):
		if guards.and(active, live) != guardFalse {
			return live, "multiple_resource_acquisitions"
		}
		return guards.or(live, active), ""
	case string(facts.OpUseResource):
		if guards.and(active, guards.not(live)) != guardFalse {
			return live, "resource_use_before_acquisition"
		}
		return live, ""
	case string(facts.OpCleanupAttempt):
		if guards.and(active, guards.not(live)) != guardFalse {
			return live, "resource_cleanup_before_acquisition"
		}
		return guards.and(live, guards.not(active)), ""
	default:
		return live, "unsupported_effect_kind"
	}
}

func resourceResolvedEdges(guards *controlGuards, evaluation FlowEvaluation, charge func(int) bool) (map[resourceEdgeKey]guardID, string) {
	if evaluation.Edges == nil {
		return nil, ""
	}
	resolved := map[resourceEdgeKey]guardID{}
	for _, candidate := range evaluation.Edges {
		if !charge(1) {
			return nil, "work_limit"
		}
		if candidate.From == "" || candidate.To == "" || candidate.Guard == "" {
			return nil, "resource_exit_guard_unknown"
		}
		if _, known := guards.arena.Lookup(candidate.Guard); !known {
			return nil, "resource_exit_guard_unknown"
		}
		key := resourceEdgeKey{from: candidate.From, to: candidate.To, kind: candidate.Kind}
		resolved[key] = guards.or(resolved[key], guards.atom(candidate.Guard))
	}
	return resolved, ""
}

func resourceEdgeGuard(guards *controlGuards, resolved map[resourceEdgeKey]guardID, edge facts.FlowEdge) (guardID, string) {
	if resolved != nil {
		guard, ok := resolved[resourceEdgeKey{from: edge.From, to: edge.To, kind: edge.Kind}]
		if !ok {
			return guardFalse, "resource_edge_guard_unknown"
		}
		return guard, ""
	}
	if edge.Kind != facts.EdgeTrue && edge.Kind != facts.EdgeFalse {
		return guardTrue, ""
	}
	return guardFalse, "resource_exit_guard_unknown"
}
