package unitplan

import (
	"github.com/blater/slopwatch/internal/analysiscache"
	"sort"
	"strings"
)

// Lookup exposes only named records and explicitly selected buckets. Incremental
// callers cannot enumerate the workspace through this contract.
type Lookup interface {
	Unit(string) (Unit, bool)
	Owners(string) []Unit
	Source(string) (*Source, bool)
	Consumers(string) []string
	Referrers(string) []string
	Group(Language) []string
	Conservative(Language) []string
}

type Source struct {
	NeedsRetry bool
	Digest     analysiscache.Digest
	Stamp      analysiscache.FileStamp
	Visible    bool
	Owner      string
	Path       string
	Language   Language
	Imports    []string
	Uncertain  bool
	DataDigest string
}

type Index struct {
	work                func(string)
	tsSourceCount       int
	dormantDeclarations map[string][]string
	tsDeclarations      map[string]map[string]bool
	conservative        map[Language]map[string]bool
	units               map[string]Unit
	sources             map[string]*Source
	owners              map[string]map[string]bool
	consumers           map[string]map[string]bool
	groups              map[Language]map[string]bool
	refs                map[string]map[string]bool
	resolutions         map[string]string
	goModules           map[string]string
	goConfigs           map[string][]string
	goCounts            map[string]map[string]int
	javaModules         map[string]*javaModule
	javaConfigs         map[string][]string
	javaDeps            map[string]javaDependencies
	javaNarrow          bool
	javaFallbackConfigs []string
	rustPackages        map[string]*rustPackage
	rustMembers         map[string]map[string]bool
	rustConfigs         map[string][]string
	rustDeps            map[string]cargoDependencies
	rustNarrow          bool
	rustFallbackConfigs []string
	rustExplicit        map[string][]rustTarget
	tsTemplates         map[string]Unit
	tsProjects          map[string][]string
	tsPackages          map[string]bool
	tsSyntaxConfigs     map[string][]string
	tsMode              TypeScriptMode
}

func newIndex() *Index {
	return &Index{owners: map[string]map[string]bool{}, dormantDeclarations: map[string][]string{}, tsDeclarations: map[string]map[string]bool{}, conservative: map[Language]map[string]bool{}, units: map[string]Unit{}, sources: map[string]*Source{}, consumers: map[string]map[string]bool{}, groups: map[Language]map[string]bool{}, refs: map[string]map[string]bool{}, resolutions: map[string]string{}, goModules: map[string]string{}, goConfigs: map[string][]string{}, goCounts: map[string]map[string]int{}, rustMembers: map[string]map[string]bool{}, rustExplicit: map[string][]rustTarget{}, tsTemplates: map[string]Unit{}, tsProjects: map[string][]string{}, tsPackages: map[string]bool{}, tsSyntaxConfigs: map[string][]string{}}
}
func keys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for id := range values {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}
func (index *Index) Source(path string) (*Source, bool) {
	source, ok := index.sources[path]
	return source, ok
}
func (index *Index) Consumers(path string) []string { return keys(index.consumers[path]) }
func (index *Index) Group(language Language) []string {
	if !index.enabled(language) {
		return nil
	}
	return keys(index.groups[language])
}
func (index *Index) Referrers(target string) []string { return keys(index.refs[target]) }
func (index *Index) Unit(id string) (Unit, bool)      { return effectiveUnit(index, id) }
func (index *Index) raw(id string) (Unit, bool)       { unit, ok := index.units[id]; return unit, ok }
func (index *Index) resolve(symbol string) string {
	if id, ok := index.resolutions[symbol]; ok {
		return id
	}
	return symbol
}
func (index *Index) narrow(language Language) bool {
	if language == LanguageJava {
		return index.javaNarrow && len(index.units["java:workspace-fallback"].Sources) == 0
	}
	if language == LanguageRust {
		return index.rustNarrow && len(index.units["rust:workspace-fallback"].Sources) == 0
	}
	return true
}

type unitLookup interface {
	enabled(Language) bool
	raw(string) (Unit, bool)
	resolve(string) string
	narrow(Language) bool
}

func effectiveUnit(view unitLookup, id string) (Unit, bool) {
	unit, ok := view.raw(id)
	if !ok || !view.enabled(unit.Language) {
		return Unit{}, false
	}
	unit.DirectDependencies = nil
	unit.ReverseDependencies = nil
	if unit.Language == LanguageJava || unit.Language == LanguageRust {
		unit.Conservative = unit.Conservative || !view.narrow(unit.Language)
	}
	raw, _ := view.raw(id)
	unit.ContextSources = append([]string(nil), unit.ContextSources...)
	for _, symbol := range raw.DirectDependencies {
		if (strings.HasPrefix(symbol, "java:project:") || strings.HasPrefix(symbol, "rust:project:")) && !view.narrow(unit.Language) {
			continue
		}
		dependency := view.resolve(symbol)
		if dependency == id {
			continue
		}
		if target, exists := view.raw(dependency); exists {
			unit.DirectDependencies = append(unit.DirectDependencies, dependency)
			unit.ContextSources = append(unit.ContextSources, target.Sources...)
		}
	}
	unit.DirectDependencies = uniqueStrings(unit.DirectDependencies)
	owned := map[string]bool{}
	for _, path := range unit.Sources {
		owned[path] = true
	}
	context := uniqueStrings(unit.ContextSources)
	unit.ContextSources = nil
	for _, path := range context {
		if !owned[path] {
			unit.ContextSources = append(unit.ContextSources, path)
		}
	}
	return unit, true
}

// PlanDelta is a sparse overlay, never a copy of the installed index. Its
// modified units own their slices. Commit is called only by native after success.
type PlanDelta struct {
	tsSourceCount  int
	goMembers      map[string]map[string]bool
	memberships    map[string]*unitMembership
	tsDeclarations map[string]map[string]bool
	base           *Index
	consumers      map[string]map[string]bool
	owners         map[string]map[string]bool
	refs           map[string]map[string]bool
	Units          map[string]*Unit
	Sources        map[string]*Source
	counts         map[string]map[string]int
	resolutions    map[string]string
	rustMembers    map[string]map[string]bool
}

func (delta *PlanDelta) raw(id string) (Unit, bool) {
	if value, ok := delta.Units[id]; ok {
		if value == nil {
			return Unit{}, false
		}
		return *value, true
	}
	return delta.base.raw(id)
}
func (delta *PlanDelta) resolve(symbol string) string {
	if id, ok := delta.resolutions[symbol]; ok {
		return id
	}
	return delta.base.resolve(symbol)
}
func (delta *PlanDelta) narrow(language Language) bool {
	if language == LanguageJava {
		unit, _ := delta.raw("java:workspace-fallback")
		return delta.base.javaNarrow && len(unit.Sources) == 0
	}
	if language == LanguageRust {
		unit, _ := delta.raw("rust:workspace-fallback")
		return delta.base.rustNarrow && len(unit.Sources) == 0
	}
	return true
}
func (delta *PlanDelta) Unit(id string) (Unit, bool) { return effectiveUnit(delta, id) }
func (delta *PlanDelta) Source(path string) (*Source, bool) {
	if source, ok := delta.Sources[path]; ok {
		return source, source != nil
	}
	return delta.base.Source(path)
}
func (delta *PlanDelta) Consumers(path string) []string {
	result := map[string]bool{}
	for _, id := range delta.base.Consumers(path) {
		if _, changed := delta.Units[id]; !changed {
			result[id] = true
		}
	}
	for id := range delta.consumers[path] {
		delta.base.observeWork("consumer")
		result[id] = true
	}
	return keys(result)
}
func (delta *PlanDelta) Group(language Language) []string {
	if !delta.enabled(language) {
		return nil
	}
	result := map[string]bool{}
	for _, id := range keys(delta.base.groups[language]) {
		if unit, ok := delta.raw(id); ok && unit.Language == language {
			result[id] = true
		}
	}
	for id, unit := range delta.Units {
		if unit != nil && unit.Language == language {
			result[id] = true
		}
	}
	return keys(result)
}
func (delta *PlanDelta) Referrers(symbol string) []string {
	result := map[string]bool{}
	for _, id := range delta.base.Referrers(symbol) {
		if _, changed := delta.Units[id]; !changed {
			result[id] = true
		}
	}
	for id := range delta.refs[symbol] {
		delta.base.observeWork("referrer")
		result[id] = true
	}
	return keys(result)
}
func reverse(view Lookup, resolver unitLookup, id string) []string {
	raw, ok := resolver.raw(id)
	if !ok {
		return nil
	}
	symbols := []string{id}
	switch raw.Language {
	case LanguageGo:
		symbols = append(symbols, "go:import:"+goImportForUnit(raw.ID, view))
	case LanguageJava:
		if strings.HasSuffix(id, ":main") {
			symbols = append(symbols, "java:project:"+raw.Directory)
		}
	case LanguageRust: // The package main resolution is checked below.
		directory := raw.Directory
		symbol := "rust:project:" + directory
		if resolver.resolve(symbol) == id {
			symbols = append(symbols, symbol, "rust:own:"+directory)
		}
	}
	result := map[string]bool{}
	for _, symbol := range symbols {
		for _, ref := range view.Referrers(symbol) {
			unit, exists := view.Unit(ref)
			if !exists {
				continue
			}
			for _, dep := range unit.DirectDependencies {
				if dep == id {
					result[ref] = true
				}
			}
		}
	}
	return keys(result)
}
func (index *Index) Reverse(id string) []string     { return reverse(index, index, id) }
func (delta *PlanDelta) Reverse(id string) []string { return reverse(delta, delta, id) }
func goImportForUnit(id string, view Lookup) string {
	var index *Index
	switch value := view.(type) {
	case *Index:
		index = value
	case *PlanDelta:
		index = value.base
	default:
		return ""
	}
	unit, ok := index.raw(id)
	if delta, yes := view.(*PlanDelta); yes {
		unit, ok = delta.raw(id)
	}
	if !ok || !strings.HasPrefix(id, "go:package:") {
		return ""
	}
	return index.goImport(unit.Directory)
}
func (index *Index) goImport(directory string) string {
	for current := directory; ; current = pathDirectory(current) {
		if module, ok := index.goModules[current]; ok {
			if module == "" {
				return ""
			}
			relative := strings.TrimPrefix(strings.TrimPrefix(directory, current), "/")
			if relative != "" && relative != "." {
				return module + "/" + relative
			}
			return module
		}
		if current == "." {
			return ""
		}
	}
}
func cloneUnit(unit Unit) Unit {
	unit.Sources = append([]string(nil), unit.Sources...)
	unit.ContextSources = append([]string(nil), unit.ContextSources...)
	unit.DirectDependencies = append([]string(nil), unit.DirectDependencies...)
	unit.ConfigInputs = append([]string(nil), unit.ConfigInputs...)
	unit.Capabilities = append([]Capability(nil), unit.Capabilities...)
	return unit
}
func bucketAdd(index map[string]map[string]bool, path, id string) {
	if index[path] == nil {
		index[path] = map[string]bool{}
	}
	index[path][id] = true
}
func (index *Index) installUnit(unit Unit) {
	index.units[unit.ID] = unit
	for _, path := range unit.Sources {
		bucketAdd(index.owners, path, unit.ID)
	}
	if unit.Conservative {
		if index.conservative[unit.Language] == nil {
			index.conservative[unit.Language] = map[string]bool{}
		}
		index.conservative[unit.Language][unit.ID] = true
	}
	if index.groups[unit.Language] == nil {
		index.groups[unit.Language] = map[string]bool{}
	}
	index.groups[unit.Language][unit.ID] = true
	for _, paths := range [][]string{unit.Sources, unit.ContextSources} {
		for _, path := range paths {
			bucketAdd(index.consumers, path, unit.ID)
		}
	}
	for _, symbol := range unit.DirectDependencies {
		bucketAdd(index.refs, symbol, unit.ID)
	}
}
func (index *Index) removeUnit(id string) {
	unit, ok := index.units[id]
	if !ok {
		return
	}
	for _, paths := range [][]string{unit.Sources, unit.ContextSources} {
		for _, path := range paths {
			delete(index.consumers[path], id)
			if len(index.consumers[path]) == 0 {
				delete(index.consumers, path)
			}
		}
	}
	for _, symbol := range unit.DirectDependencies {
		delete(index.refs[symbol], id)
		if len(index.refs[symbol]) == 0 {
			delete(index.refs, symbol)
		}
	}
	delete(index.groups[unit.Language], id)
	delete(index.conservative[unit.Language], id)
	delete(index.units, id)
	for _, path := range unit.Sources {
		delete(index.owners[path], id)
		if len(index.owners[path]) == 0 {
			delete(index.owners, path)
		}
	}
}
func (delta *PlanDelta) Commit() {
	delta.base.tsSourceCount = delta.tsSourceCount
	for id := range delta.Units {
		delta.base.removeUnit(id)
	}
	for _, unit := range delta.Units {
		if unit != nil {
			delta.base.installUnit(*unit)
		}
	}
	for path, source := range delta.Sources {
		if source == nil {
			delete(delta.base.sources, path)
		} else {
			delta.base.sources[path] = source
		}
	}
	for id, counts := range delta.counts {
		delta.base.goCounts[id] = counts
	}
	for symbol, id := range delta.resolutions {
		delta.base.resolutions[symbol] = id
	}
	for id, members := range delta.tsDeclarations {
		delta.base.tsDeclarations[id] = members
	}
	for directory, members := range delta.rustMembers {
		delta.base.rustMembers[directory] = members
	}
}

// InitializeSources is a startup-only construction pass. It is deliberately
// absent from Lookup, which is the contract supplied to incremental helpers.
func (index *Index) InitializeSources(initialize func(*Source) error) error {
	for _, source := range index.sources {
		if err := initialize(source); err != nil {
			return err
		}
	}
	return nil
}

func (index *Index) Conservative(language Language) []string {
	if !index.enabled(language) {
		return nil
	}
	if !index.narrow(language) {
		return index.Group(language)
	}
	return keys(index.conservative[language])
}
func (delta *PlanDelta) Conservative(language Language) []string {
	if !delta.enabled(language) {
		return nil
	}
	if !delta.narrow(language) {
		return delta.Group(language)
	}
	result := map[string]bool{}
	for _, id := range keys(delta.base.conservative[language]) {
		if unit, ok := delta.raw(id); ok && unit.Conservative {
			result[id] = true
		}
	}
	for id, unit := range delta.Units {
		if unit != nil && unit.Language == language && unit.Conservative {
			result[id] = true
		}
	}
	return keys(result)
}

func (index *Index) enabled(language Language) bool {
	return language != LanguageTypeScript || index.tsSourceCount > 0
}
func (delta *PlanDelta) enabled(language Language) bool {
	return language != LanguageTypeScript || delta.tsSourceCount > 0
}

// Owners returns only ownership-selection metadata, without materializing source
// or dependency arrays. Overlapping units remain candidates for caller policy.
func ownerMetadata(unit Unit) Unit {
	return Unit{ID: unit.ID, Language: unit.Language, Mode: unit.Mode, Capabilities: unit.Capabilities}
}
func (index *Index) Owners(path string) []Unit {
	result := []Unit{}
	for id := range index.owners[path] {
		index.observeWork("owner")
		if unit, ok := index.raw(id); ok && index.enabled(unit.Language) {
			result = append(result, ownerMetadata(unit))
		}
	}
	return result
}
func (delta *PlanDelta) Owners(path string) []Unit {
	result := []Unit{}
	for id := range delta.base.owners[path] {
		if _, changed := delta.Units[id]; !changed {
			if unit, ok := delta.raw(id); ok && delta.enabled(unit.Language) {
				result = append(result, ownerMetadata(unit))
			}
		}
	}
	for id := range delta.owners[path] {
		delta.base.observeWork("owner")
		unit, _ := delta.raw(id)
		if delta.enabled(unit.Language) {
			result = append(result, ownerMetadata(unit))
		}
	}
	return result
}

// Candidate buckets are built once from touched units, never once per lookup.
func (delta *PlanDelta) indexCandidates() {
	delta.consumers = map[string]map[string]bool{}
	delta.owners = map[string]map[string]bool{}
	delta.refs = map[string]map[string]bool{}
	for id, unit := range delta.Units {
		if unit == nil {
			continue
		}
		for _, path := range unit.Sources {
			delta.base.observeWork("candidate_member")
			bucketAdd(delta.owners, path, id)
			bucketAdd(delta.consumers, path, id)
		}
		for _, path := range unit.ContextSources {
			bucketAdd(delta.consumers, path, id)
		}
		for _, symbol := range unit.DirectDependencies {
			bucketAdd(delta.refs, symbol, id)
		}
	}
}

func (index *Index) observeWork(operation string) {
	if index.work != nil {
		index.work(operation)
	}
}
