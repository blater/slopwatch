package unitplan

import "strings"

type rustPackage struct {
	directory string
	manifest  string
	name      string
	sources   []string
	targets   []rustTarget
}

type rustTarget struct {
	kind  string
	name  string
	root  string
	tests bool
	main  bool
}

func planRust(context plannerContext, _ Options) ([]Unit, []Diagnostic) {
	packages, fallback := discoverRustPackages(context)
	assignRustTargets(context, packages)
	mainIDs := rustMainIDs(packages)
	localDependencies, narrow, diagnostics := cargoDependencyGraph(context, packages)
	configInputs := rustConfigInputsByPackage(context, packages)
	if len(fallback) > 0 {
		narrow = false
		diagnostics = append(diagnostics, rustFallbackDiagnostic(fallback[0]))
	}
	units := rustUnits(packages, localDependencies, narrow, configInputs, mainIDs)
	if len(fallback) > 0 {
		units = append(units, rustFallbackUnit(fallback, rustWorkspaceConfigs(context)))
	}
	return units, diagnostics
}

func discoverRustPackages(context plannerContext) (map[string]*rustPackage, []string) {
	packages := rustManifestPackages(context)
	manifestDirs := make(map[string]bool, len(packages))
	for directory := range packages {
		manifestDirs[directory] = true
	}
	var fallback []string
	for _, file := range context.files {
		if !isRustSource(file) || lastPart(file) == "build.rs" {
			continue
		}
		directory, ok := nearestAncestor(pathDirectory(file), manifestDirs)
		if ok {
			packages[directory].sources = append(packages[directory].sources, file)
		} else {
			fallback = append(fallback, file)
		}
	}
	return packages, fallback
}

func rustManifestPackages(context plannerContext) map[string]*rustPackage {
	packages := map[string]*rustPackage{}
	for _, file := range context.files {
		if lastPart(file) != "Cargo.toml" {
			continue
		}
		directory := pathDirectory(file)
		data, _ := context.read(file)
		if name := cargoPackageName(string(data)); name != "" {
			packages[directory] = &rustPackage{directory: directory, manifest: file, name: name}
		}
	}
	return packages
}

func isRustSource(file string) bool { return strings.HasSuffix(file, ".rs") }

func assignRustTargets(context plannerContext, packages map[string]*rustPackage) {
	for _, pkg := range packages {
		pkg.targets = rustTargets(context, pkg)
	}
}

func rustMainIDs(packages map[string]*rustPackage) map[string]string {
	mainIDs := map[string]string{}
	for directory, pkg := range packages {
		for _, target := range pkg.targets {
			if target.main {
				mainIDs[directory] = rustUnitID(pkg, target)
				if target.kind == "lib" {
					break
				}
			}
		}
	}
	return mainIDs
}

func rustUnits(packages map[string]*rustPackage, localDependencies map[string]cargoDependencies, narrow bool, configInputs map[string][]string, mainIDs map[string]string) []Unit {
	var units []Unit
	for _, pkg := range packages {
		for _, target := range pkg.targets {
			units = append(units, rustUnit(pkg, target, localDependencies, narrow, configInputs[pkg.directory], mainIDs))
		}
	}
	return units
}

func rustUnit(pkg *rustPackage, target rustTarget, localDependencies map[string]cargoDependencies, narrow bool, configs []string, mainIDs map[string]string) Unit {
	return Unit{
		ID: rustUnitID(pkg, target), Language: LanguageRust, Mode: ModeProject,
		Capabilities: rustCapabilities(target), Sources: rustTargetSources(pkg, target),
		ContextSources: pkg.sources, ConfigInputs: configs,
		DirectDependencies: rustDependencies(pkg, target, localDependencies, narrow, mainIDs),
		Conservative:       !narrow || len(pkg.targets) > 1,
	}
}

func rustCapabilities(target rustTarget) []Capability {
	capabilities := []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies}
	if target.tests {
		capabilities = append(capabilities, CapabilityTests)
	}
	return capabilities
}

func rustDependencies(pkg *rustPackage, target rustTarget, localDependencies map[string]cargoDependencies, narrow bool, mainIDs map[string]string) []string {
	dependencies := []string{}
	if ownMain := mainIDs[pkg.directory]; ownMain != "" && ownMain != rustUnitID(pkg, target) {
		dependencies = append(dependencies, ownMain)
	}
	if !narrow {
		return dependencies
	}
	for _, directory := range localDependencies[pkg.directory].forTarget(target) {
		if id := mainIDs[directory]; id != "" {
			dependencies = append(dependencies, id)
		}
	}
	return dependencies
}

func rustFallbackDiagnostic(path string) Diagnostic {
	return Diagnostic{Path: path, Message: "Rust source outside a Cargo package broadened the workspace dependency graph"}
}

func rustFallbackUnit(sources, configs []string) Unit {
	return Unit{
		ID: "rust:workspace-fallback", Language: LanguageRust, Mode: ModeProject,
		Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
		Sources:      sources, ConfigInputs: configs, Conservative: true,
	}
}

type cargoDependencies struct {
	main []string
	dev  []string
}

func (dependencies cargoDependencies) forTarget(target rustTarget) []string {
	result := append([]string(nil), dependencies.main...)
	if target.tests || target.kind == "example" {
		result = append(result, dependencies.dev...)
	}
	return result
}

func rustTargets(context plannerContext, pkg *rustPackage) []rustTarget {
	targets := standardRustTargets(context, pkg)
	for _, source := range pkg.sources {
		if target, ok := rustSourceTarget(source, pkg.directory); ok {
			targets = append(targets, target)
		}
	}
	if data, err := context.read(pkg.manifest); err == nil {
		targets = mergeExplicitTargets(targets, explicitCargoTargets(string(data), pkg), context.fileSet)
	}
	if len(targets) == 0 && len(pkg.sources) > 0 {
		targets = append(targets, rustTarget{kind: "crate", name: pkg.name, root: pkg.sources[0], main: true})
	}
	return targets
}

func standardRustTargets(context plannerContext, pkg *rustPackage) []rustTarget {
	var targets []rustTarget
	lib := joinPath(pkg.directory, "src/lib.rs")
	if context.fileSet[lib] {
		targets = append(targets, rustTarget{kind: "lib", name: pkg.name, root: lib, main: true})
	}
	main := joinPath(pkg.directory, "src/main.rs")
	if context.fileSet[main] {
		targets = append(targets, rustTarget{kind: "bin", name: pkg.name, root: main, main: true})
	}
	return targets
}

func rustSourceTarget(source, directory string) (rustTarget, bool) {
	relative := strings.TrimPrefix(strings.TrimPrefix(source, directory), "/")
	switch {
	case strings.HasPrefix(relative, "src/bin/"):
		return rustBinaryTarget(source, relative)
	case strings.HasPrefix(relative, "tests/"):
		return rustNamedTarget(source, relative, "test", true)
	case strings.HasPrefix(relative, "examples/"):
		return rustExampleTarget(source, relative)
	case strings.HasPrefix(relative, "benches/"):
		return rustBenchTarget(source, relative)
	}
	return rustTarget{}, false
}

func rustBinaryTarget(source, relative string) (rustTarget, bool) {
	if strings.Count(relative, "/") == 2 && strings.HasSuffix(relative, ".rs") {
		return rustTarget{kind: "bin", name: strings.TrimSuffix(lastPart(source), ".rs"), root: source, main: true}, true
	}
	if strings.Count(relative, "/") == 3 && lastPart(source) == "main.rs" {
		return rustTarget{kind: "bin", name: lastPart(pathDirectory(source)), root: source, main: true}, true
	}
	return rustTarget{}, false
}

func rustNamedTarget(source, relative, kind string, tests bool) (rustTarget, bool) {
	if strings.Count(relative, "/") != 1 {
		return rustTarget{}, false
	}
	return rustTarget{kind: kind, name: strings.TrimSuffix(lastPart(source), ".rs"), root: source, tests: tests}, true
}

func rustExampleTarget(source, relative string) (rustTarget, bool) {
	if strings.Count(relative, "/") == 1 {
		return rustNamedTarget(source, relative, "example", false)
	}
	if strings.Count(relative, "/") == 2 && lastPart(source) == "main.rs" {
		return rustTarget{kind: "example", name: lastPart(pathDirectory(source)), root: source}, true
	}
	return rustTarget{}, false
}

func rustBenchTarget(source, relative string) (rustTarget, bool) {
	if strings.Count(relative, "/") == 1 {
		return rustNamedTarget(source, relative, "bench", true)
	}
	if strings.Count(relative, "/") == 2 && lastPart(source) == "main.rs" {
		return rustTarget{kind: "bench", name: lastPart(pathDirectory(source)), root: source, tests: true}, true
	}
	return rustTarget{}, false
}

func mergeExplicitTargets(targets, explicit []rustTarget, files map[string]bool) []rustTarget {
	for _, candidate := range explicit {
		if index := targetIndex(targets, candidate); index >= 0 {
			targets[index] = candidate
		} else if files[candidate.root] {
			targets = append(targets, candidate)
		}
	}
	return targets
}

func targetIndex(targets []rustTarget, candidate rustTarget) int {
	for index, target := range targets {
		if target.kind == candidate.kind && target.root == candidate.root {
			return index
		}
	}
	return -1
}

func rustTargetSources(pkg *rustPackage, target rustTarget) []string {
	var result []string
	for _, source := range pkg.sources {
		relative := strings.TrimPrefix(strings.TrimPrefix(source, pkg.directory), "/")
		if strings.HasPrefix(relative, "src/") || source == target.root {
			result = append(result, source)
		}
	}
	return result
}

func rustUnitID(pkg *rustPackage, target rustTarget) string {
	return "rust:cargo:" + relativeIDPath(pkg.directory) + ":" + target.kind + ":" + target.name
}
