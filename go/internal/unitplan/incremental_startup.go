package unitplan

import (
	"github.com/blater/slopwatch/internal/sourcepath"
	"strings"
)

func SourceLanguage(path string) Language {
	parts := strings.Split(path, "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		if IgnoredDirectory(part) {
			return ""
		}
	}
	if !liveSource(path) {
		return ""
	}
	if strings.HasSuffix(path, ".java") {
		if sourcepath.IsJavaResource(path) {
			return ""
		}
		return LanguageJava
	}
	if strings.HasSuffix(path, ".go") {
		return LanguageGo
	}
	if strings.HasSuffix(path, ".rs") {
		return LanguageRust
	}
	if isTypeScriptSource(path) {
		return LanguageTypeScript
	}
	return ""
}
func (index *Index) captureUnits(units []Unit) {
	for _, unit := range units {
		unit = cloneUnit(unit)
		unit.Sources = uniqueStrings(unit.Sources)
		unit.ContextSources = uniqueStrings(unit.ContextSources)
		unit.ConfigInputs = uniqueStrings(unit.ConfigInputs)
		switch unit.Language {
		case LanguageGo:
			counts := map[string]int{}
			for _, path := range unit.Sources {
				if source := index.sources[path]; source != nil {
					for _, value := range source.Imports {
						counts[value]++
					}
				}
			}
			index.goCounts[unit.ID] = counts
			unit.DirectDependencies = nil
			for value := range counts {
				unit.DirectDependencies = append(unit.DirectDependencies, "go:import:"+value)
			}
			if strings.HasPrefix(unit.ID, "go:test-package:") {
				unit.DirectDependencies = append(unit.DirectDependencies, strings.Replace(unit.ID, "go:test-package:", "go:package:", 1))
			} else {
				directory := unit.Directory
				if name := index.goImport(directory); name != "" {
					index.resolutions["go:import:"+name] = unit.ID
				}
			}
		case LanguageJava:
			if unit.ID != "java:workspace-fallback" {
				directory := unit.Directory
				sourceSet := unit.ID[strings.LastIndex(unit.ID, ":")+1:]
				unit.DirectDependencies = index.javaSymbols(directory, sourceSet)
				unit.Conservative = false
				index.resolutions["java:project:"+directory] = javaUnitID(index.javaModules[directory], "main")
			}
		case LanguageRust:
			if unit.ID != "rust:workspace-fallback" {
				directory := unit.Directory
				pkg := index.rustPackages[directory]
				for _, target := range pkg.targets {
					if rustUnitID(pkg, target) == unit.ID {
						unit.DirectDependencies = index.rustSymbols(pkg, target)
						unit.Conservative = len(pkg.targets) > 1
					}
				}
			}
		}
		unit.DirectDependencies = uniqueStrings(unit.DirectDependencies)
		index.installUnit(unit)
	}
	for id, paths := range index.dormantDeclarations {
		if _, exists := index.units[id]; !exists {
			unit := cloneUnit(index.tsTemplates[id])
			unit.ContextSources = uniqueStrings(paths)
			index.installUnit(unit)
		}
	}
	index.dormantDeclarations = nil
	for directory, pkg := range index.rustPackages {
		index.resolutions["rust:project:"+directory] = rustMainIDs(map[string]*rustPackage{directory: pkg})[directory]
		index.resolutions["rust:own:"+directory] = index.resolutions["rust:project:"+directory]
	}
	// Definitions are immutable summaries; membership remains in canonical units
	// and the package's secondary path bucket, not mutable copied project units.
	for _, module := range index.javaModules {
		module.sets = nil
	}
	for _, pkg := range index.rustPackages {
		pkg.sources = nil
		pkg.targets = nil
	}
}
func (index *Index) javaSymbols(directory, sourceSet string) []string {
	module := index.javaModules[directory]
	var symbols []string
	if sourceSet != "main" {
		symbols = append(symbols, javaUnitID(module, "main"))
	}
	for _, dependency := range index.javaDeps[directory].forSourceSet(sourceSet) {
		symbols = append(symbols, "java:project:"+dependency)
	}
	return symbols
}
func (index *Index) rustSymbols(pkg *rustPackage, target rustTarget) []string {
	symbols := []string{"rust:project:" + pkg.directory}
	// Own-main edges remain enabled even under conservative Cargo policy.
	symbols[0] = "rust:own:" + pkg.directory
	for _, dependency := range index.rustDeps[pkg.directory].forTarget(target) {
		symbols = append(symbols, "rust:project:"+dependency)
	}
	return symbols
}
func (index *Index) captureTypeScript(context plannerContext, options Options, configs map[string]*tsConfig, declarations []string) {
	index.tsMode = options.TypeScriptMode
	ids, projects := typeScriptProjects(configs)
	index.tsProjects = projects
	if options.TypeScriptMode != TypeScriptSyntax {
		for project, paths := range assignTypeScriptSources(declarations, projects) {
			index.dormantDeclarations["typescript:typed:"+project] = paths
		}
	}
	packages := typeScriptPackageInputsByProject(context, configs)
	byDirectory := typeScriptPackageInputsByDirectory(context)
	for _, path := range context.files {
		if lastPart(path) == "package.json" {
			index.tsPackages[pathDirectory(path)] = true
		}
	}
	for path, config := range configs {
		inputs, diagnostic := tsConfigInputs(context, config, configs, packages[path])
		byDirectory[config.directory] = append(byDirectory[config.directory], inputs...)
		if !config.project {
			continue
		}
		index.tsTemplates[ids[path]] = typeScriptUnit(ids[path], nil, nil, inputs, typeScriptDependencies(context, config, ids), config.uncertain || diagnostic != nil)
	}
	index.tsSyntaxConfigs = byDirectory
}

// IgnoredDirectory is the planner's context-inventory policy, shared by follow.
// Report discovery has its own presentation filters.
func IgnoredDirectory(name string) bool { return ignoredDirectories[name] }
