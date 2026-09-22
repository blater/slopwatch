package unitplan

import (
	"strings"
)

// Prepare applies named final source states to a sparse candidate. Nil means
// deletion. Content-only edits preserve membership except Go import edges.
func (index *Index) Prepare(changes map[string]*Source) *PlanDelta {
	delta := &PlanDelta{memberships: map[string]*unitMembership{}, goMembers: map[string]map[string]bool{}, tsSourceCount: index.tsSourceCount, tsDeclarations: map[string]map[string]bool{}, base: index, Units: map[string]*Unit{}, Sources: changes, counts: map[string]map[string]int{}, resolutions: map[string]string{}, rustMembers: map[string]map[string]bool{}}
	for path, next := range changes {
		previous, existed := index.Source(path)
		if isTypeScriptDeclaration(path) {
			continue
		}
		if existed && previous.Language == LanguageTypeScript && next == nil {
			delta.tsSourceCount--
		}
		if !existed && next != nil && next.Language == LanguageTypeScript {
			delta.tsSourceCount++
		}
	}
	for path, source := range changes {
		previous, existed := index.Source(path)
		language := SourceLanguage(path)
		if source != nil {
			language = source.Language
		} else if existed {
			language = previous.Language
		}
		switch language {
		case LanguageGo:
			delta.goSource(path, previous, source)
		case LanguageJava:
			if existed != (source != nil) {
				delta.javaSource(path, source != nil)
			}
		case LanguageTypeScript:
			if existed != (source != nil) {
				delta.typeScriptSource(path, source != nil)
			}
		case LanguageRust:
			if existed != (source != nil) {
				delta.rustSource(path, source != nil)
			}
		}
	}
	for id := range delta.goMembers {
		delta.finishGoUnit(id)
	}
	for id := range delta.memberships {
		delta.finishMembership(id)
	}
	for directory := range delta.rustMembers {
		delta.rustPackage(directory)
	}
	delta.indexCandidates()
	return delta
}
func (delta *PlanDelta) mutable(id string, template Unit) *Unit {
	if unit, ok := delta.Units[id]; ok {
		if unit != nil {
			return unit
		}
		replacement := cloneUnit(template)
		delta.Units[id] = &replacement
		return &replacement
	}
	unit, ok := delta.base.raw(id)
	if !ok {
		unit = template
	}
	unit = cloneUnit(unit)
	delta.Units[id] = &unit
	return &unit
}

type unitMembership struct{ sources, context map[string]bool }

func (delta *PlanDelta) membership(unit *Unit) *unitMembership {
	if members := delta.memberships[unit.ID]; members != nil {
		return members
	}
	members := &unitMembership{sources: map[string]bool{}, context: map[string]bool{}}
	for _, path := range unit.Sources {
		delta.base.observeWork("membership_seed")
		members.sources[path] = true
	}
	for _, path := range unit.ContextSources {
		delta.base.observeWork("membership_seed")
		members.context[path] = true
	}
	if unit.Language == LanguageTypeScript && unit.Mode == ModeSyntax {
		for path := range delta.base.tsDeclarations[unit.ID] {
			delta.base.observeWork("membership_seed")
			members.context[path] = true
		}
	}
	delta.memberships[unit.ID] = members
	return members
}
func updateMember(members map[string]bool, path string, exists bool) {
	if exists {
		members[path] = true
	} else {
		delete(members, path)
	}
}
func (delta *PlanDelta) finishMembership(id string) {
	unit := delta.Units[id]
	members := delta.memberships[id]
	delta.base.observeWork("membership_finalize")
	unit.Sources = keys(members.sources)
	unit.ContextSources = keys(members.context)
	if unit.Language == LanguageTypeScript {
		if template, typed := delta.base.tsTemplates[id]; typed {
			unit.Conservative = template.Conservative
			for _, member := range unit.Sources {
				delta.base.observeWork("typescript_owner")
				if len(delta.base.typeScriptOwners(member)) > 1 {
					unit.Conservative = true
				}
			}
		} else {
			delta.tsDeclarations[id] = members.context
			unit.ConfigInputs = nil
			seen := map[string]bool{}
			for _, source := range unit.Sources {
				delta.base.observeWork("typescript_config_source")
				for directory := pathDirectory(source); ; directory = pathDirectory(directory) {
					if seen[directory] {
						break
					}
					seen[directory] = true
					unit.ConfigInputs = append(unit.ConfigInputs, delta.base.tsSyntaxConfigs[directory]...)
					if directory == "." {
						break
					}
				}
			}
			unit.ConfigInputs = uniqueStrings(unit.ConfigInputs)
		}
	}
	if len(unit.Sources) == 0 && (unit.Language != LanguageTypeScript || unit.Mode == ModeSyntax || len(unit.ContextSources) == 0) {
		delta.Units[id] = nil
	}
}
func (delta *PlanDelta) goSource(path string, previous, next *Source) {
	directory := pathDirectory(path)
	id := "go:package:" + relativeIDPath(directory)
	test := strings.HasSuffix(path, "_test.go")
	if test {
		id = "go:test-package:" + relativeIDPath(directory)
	}
	template := Unit{Directory: directory, ID: id, Language: LanguageGo, Mode: ModeProject, Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies}}
	if test {
		template.Capabilities = append(template.Capabilities, CapabilityTests)
	}
	moduleFound := false
	for current := directory; ; current = pathDirectory(current) {
		for _, config := range delta.base.goConfigs[current] {
			name := lastPart(config)
			if name == "go.work" || name == "go.work.sum" {
				template.ConfigInputs = append(template.ConfigInputs, config)
			}
		}
		if _, ok := delta.base.goModules[current]; ok && !moduleFound {
			moduleFound = true
			for _, config := range delta.base.goConfigs[current] {
				if name := lastPart(config); name == "go.mod" || name == "go.sum" {
					template.ConfigInputs = append(template.ConfigInputs, config)
				}
			}
		}
		if current == "." {
			break
		}
	}
	unit := delta.mutable(id, template)
	members, initialized := delta.goMembers[id]
	if !initialized {
		members = map[string]bool{}
		for _, member := range unit.Sources {
			delta.base.observeWork("go_member")
			members[member] = true
		}
		delta.goMembers[id] = members
	}
	if next == nil {
		delete(members, path)
	} else {
		members[path] = true
	}
	counts, ok := delta.counts[id]
	if !ok {
		counts = map[string]int{}
		for name, count := range delta.base.goCounts[id] {
			counts[name] = count
		}
		delta.counts[id] = counts
	}
	if previous != nil {
		for _, name := range previous.Imports {
			counts[name]--
			if counts[name] == 0 {
				delete(counts, name)
			}
		}
	}
	if next != nil {
		for _, name := range next.Imports {
			counts[name]++
		}
	}
}
func (delta *PlanDelta) finishGoUnit(id string) {
	unit := delta.Units[id]
	unit.Sources = keys(delta.goMembers[id])
	directory := unit.Directory
	test := strings.HasPrefix(id, "go:test-package:")
	unit.DirectDependencies = nil
	for name := range delta.counts[id] {
		unit.DirectDependencies = append(unit.DirectDependencies, "go:import:"+name)
	}
	if test {
		unit.DirectDependencies = append(unit.DirectDependencies, "go:package:"+relativeIDPath(directory))
	}
	unit.Conservative = delta.base.goImport(directory) == ""
	for _, path := range unit.Sources {
		delta.base.observeWork("go_member")
		if source, ok := delta.Source(path); ok && source.Uncertain {
			unit.Conservative = true
		}
	}
	unit.ConfigInputs = uniqueStrings(unit.ConfigInputs)
	unit.DirectDependencies = uniqueStrings(unit.DirectDependencies)
	if len(unit.Sources) == 0 {
		delta.Units[id] = nil
	}
	if !test {
		if name := delta.base.goImport(directory); name != "" {
			if len(unit.Sources) > 0 {
				delta.resolutions["go:import:"+name] = id
			} else {
				delta.resolutions["go:import:"+name] = ""
			}
		}
	}
}

func (delta *PlanDelta) javaSource(path string, exists bool) {
	module := (*javaModule)(nil)
	for directory := pathDirectory(path); ; directory = pathDirectory(directory) {
		if candidate := delta.base.javaModules[directory]; candidate != nil {
			module = candidate
			break
		}
		if directory == "." {
			break
		}
	}
	template := Unit{ID: "java:workspace-fallback", Language: LanguageJava, Mode: ModeProject, Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies}, ConfigInputs: delta.base.javaFallbackConfigs, Conservative: true}
	if module != nil {
		sourceSet := javaSourceSet(module.directory, path)
		template = Unit{Directory: module.directory, ID: javaUnitID(module, sourceSet), Language: LanguageJava, Mode: ModeProject, Capabilities: javaCapabilities(sourceSet), ConfigInputs: delta.base.javaConfigs[module.directory], DirectDependencies: delta.base.javaSymbols(module.directory, sourceSet)}
		delta.resolutions["java:project:"+module.directory] = javaUnitID(module, "main")
	}
	unit := delta.mutable(template.ID, template)
	updateMember(delta.membership(unit).sources, path, exists)
}
func (index *Index) typeScriptOwners(path string) []string {
	var owners []string
	if index.tsMode != TypeScriptSyntax {
		for directory := pathDirectory(path); ; directory = pathDirectory(directory) {
			for _, project := range index.tsProjects[directory] {
				owners = append(owners, "typescript:typed:"+project)
			}
			if directory == "." {
				break
			}
		}
	}
	if len(owners) > 0 {
		return owners
	}
	directory, ok := nearestAncestor(pathDirectory(path), index.tsPackages)
	if !ok {
		directory = "."
	}
	return []string{"typescript:syntax:" + relativeIDPath(directory)}
}
func (delta *PlanDelta) typeScriptSource(path string, exists bool) {
	for _, id := range delta.base.typeScriptOwners(path) {
		template, typed := delta.base.tsTemplates[id]
		if !typed {
			template = Unit{ID: id, Language: LanguageTypeScript, Mode: ModeSyntax, Capabilities: []Capability{CapabilitySyntax}}
		}
		members := delta.membership(delta.mutable(id, template))
		if isTypeScriptDeclaration(path) {
			updateMember(members.context, path, exists)
		} else {
			updateMember(members.sources, path, exists)
		}
	}
}
func (delta *PlanDelta) rustSource(path string, exists bool) {
	directory := ""
	for current := pathDirectory(path); ; current = pathDirectory(current) {
		if delta.base.rustPackages[current] != nil {
			directory = current
			break
		}
		if current == "." {
			break
		}
	}
	if directory == "" {
		template := rustFallbackUnit(nil, delta.base.rustFallbackConfigs)
		unit := delta.mutable(template.ID, template)
		updateMember(delta.membership(unit).sources, path, exists)
		return
	}
	members, ok := delta.rustMembers[directory]
	if !ok {
		members = map[string]bool{}
		for member := range delta.base.rustMembers[directory] {
			members[member] = true
		}
		delta.rustMembers[directory] = members
	}
	if exists {
		members[path] = true
	} else {
		delete(members, path)
	}
}
func (delta *PlanDelta) rustPackage(directory string) {
	definition := delta.base.rustPackages[directory]
	pkg := *definition
	pkg.sources = keys(delta.rustMembers[directory])
	context := plannerContext{fileSet: delta.rustMembers[directory]}
	pkg.targets = standardRustTargets(context, &pkg)
	for _, source := range pkg.sources {
		if target, ok := rustSourceTarget(source, directory); ok {
			pkg.targets = append(pkg.targets, target)
		}
	}
	pkg.targets = mergeExplicitTargets(pkg.targets, delta.base.rustExplicit[directory], context.fileSet)
	if len(pkg.targets) == 0 && len(pkg.sources) > 0 {
		pkg.targets = []rustTarget{{kind: "crate", name: pkg.name, root: pkg.sources[0], main: true}}
	}
	// The indexed membership bucket gives every prior target without a unit scan.
	oldIDs := map[string]bool{}
	for path := range delta.base.rustMembers[directory] {
		for _, id := range delta.base.Consumers(path) {
			if strings.HasPrefix(id, "rust:cargo:"+relativeIDPath(directory)+":") {
				oldIDs[id] = true
			}
		}
	}
	for id := range oldIDs {
		delta.Units[id] = nil
	}
	main := rustMainIDs(map[string]*rustPackage{directory: &pkg})[directory]
	delta.resolutions["rust:project:"+directory] = main
	delta.resolutions["rust:own:"+directory] = main
	for _, target := range pkg.targets {
		unit := Unit{Directory: directory, ID: rustUnitID(&pkg, target), Language: LanguageRust, Mode: ModeProject, Capabilities: rustCapabilities(target), Sources: rustTargetSources(&pkg, target), ContextSources: append([]string(nil), pkg.sources...), ConfigInputs: delta.base.rustConfigs[directory], DirectDependencies: delta.base.rustSymbols(&pkg, target), Conservative: len(pkg.targets) > 1}
		delta.Units[unit.ID] = &unit
	}
}
