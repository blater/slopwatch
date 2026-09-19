package sourceestimate

// Inventories are analysis-owned and describe immutable source, not attributed
// operations. Contextual token slices must never reuse the full-unit inventory.
type rustInventory struct {
	source                *token
	length                int
	functions             []rustFunction
	byName                map[rustMember][]rustFunction
	raw                   []*operation
	rawByName             map[rustMember][]*operation
	rawReady              bool
	pointerContracts      map[string]bool
	pointerContractsReady bool
	path, pkg             string
	index                 int
}
type rustMember struct{ owner, name string }
type rustReleaseCandidate struct {
	unit     int
	function rustFunction
}
type rustWorkspaceInventory struct {
	first   *unit
	length  int
	members map[rustMember][]rustReleaseCandidate
}

func rustUnitInventory(u unit) *rustInventory {
	var source *token
	if len(u.tokens) > 0 {
		source = &u.tokens[0]
	}
	if u.inventory != nil && u.inventory.rust != nil {
		cached := u.inventory.rust
		if cached.source == source && cached.length == len(u.tokens) && cached.path == u.file.Path && cached.pkg == u.pkg && cached.index == u.index {
			return cached
		}
	}
	result := &rustInventory{source: source, length: len(u.tokens), path: u.file.Path, pkg: u.pkg, index: u.index, functions: rustFunctions(u.tokens), byName: map[rustMember][]rustFunction{}}
	for _, fn := range result.functions {
		key := rustMember{fn.owner, fn.name}
		result.byName[key] = append(result.byName[key], fn)
	}
	// A contextual copy may share inventory ownership with its full unit. Do not
	// overwrite an existing inventory when its token identity differs.
	if u.inventory != nil && u.inventory.rust == nil {
		u.inventory.rust = result
	}
	return result
}
func rustUnitFunctions(u unit) []rustFunction { return rustUnitInventory(u).functions }
func rustUnitMembers(u unit, owner, name string) []rustFunction {
	return rustUnitInventory(u).byName[rustMember{owner, name}]
}
func (inventory *rustInventory) rawOperations(u unit) []*operation {
	if !inventory.rawReady {
		inventory.raw = rustOperations(u.file, u.index, u.tokens, u.pkg, inventory.functions)
		inventory.rawByName = map[rustMember][]*operation{}
		for _, op := range inventory.raw {
			key := rustMember{op.owner, op.name}
			inventory.rawByName[key] = append(inventory.rawByName[key], op)
		}
		inventory.rawReady = true
	}
	return inventory.raw
}
func rustUnitRawOperations(u unit) []*operation { return rustUnitInventory(u).rawOperations(u) }
func rustUnitRawMembers(u unit, owner, name string) []*operation {
	inventory := rustUnitInventory(u)
	inventory.rawOperations(u)
	return inventory.rawByName[rustMember{owner, name}]
}

// Rebuild at annotation boundaries for the exact active slice and order. The
// lookup identity check also protects fallback callers passing a subset/copy.
// Tokens, unit membership and order remain immutable until the next prepare.
// Preparation is cheap: broad legacy cross-language parsing is deferred until
// a release query actually needs the workspace member index.
func prepareRustWorkspace(units []unit) {
	if len(units) == 0 {
		return
	}
	hasRust := false
	for _, u := range units {
		if normalizeLanguage(u.file.Language, u.file.Path) == "rust" {
			hasRust = true
			break
		}
	}
	if !hasRust {
		return
	}
	workspace := &rustWorkspaceInventory{first: &units[0], length: len(units)}
	for i := range units {
		if units[i].inventory == nil {
			units[i].inventory = &unitInventory{}
		}
	}
	for i := range units {
		units[i].inventory.rustWorkspace = workspace
	}
}
func rustReleaseCandidates(units []unit, owner, name string) []rustReleaseCandidate {
	if len(units) == 0 {
		return nil
	}
	if inventory := units[0].inventory; inventory != nil && inventory.rustWorkspace != nil {
		workspace := inventory.rustWorkspace
		if workspace.first == &units[0] && workspace.length == len(units) {
			if workspace.members == nil {
				workspace.members = map[rustMember][]rustReleaseCandidate{}
				for i, u := range units {
					for _, fn := range rustUnitFunctions(u) {
						key := rustMember{fn.owner, fn.name}
						workspace.members[key] = append(workspace.members[key], rustReleaseCandidate{i, fn})
					}
				}
			}
			return workspace.members[rustMember{owner, name}]
		}
	}
	// Manually constructed or contextual slices have no matching analysis index.
	result := []rustReleaseCandidate{}
	for i, u := range units {
		for _, fn := range rustUnitMembers(u, owner, name) {
			result = append(result, rustReleaseCandidate{i, fn})
		}
	}
	return result
}

func rustUnitPointerContracts(u unit) map[string]bool {
	inventory := rustUnitInventory(u)
	if !inventory.pointerContractsReady {
		inventory.pointerContracts = gradedRustPointerContracts(u.tokens)
		inventory.pointerContractsReady = true
	}
	return inventory.pointerContracts
}
