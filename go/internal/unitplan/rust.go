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
	packages := map[string]*rustPackage{}
	manifestDirs := map[string]bool{}
	for _, file := range context.files {
		if lastPart(file) != "Cargo.toml" {
			continue
		}
		directory := pathDirectory(file)
		data, _ := context.read(file)
		name := cargoPackageName(string(data))
		if name != "" {
			packages[directory] = &rustPackage{directory: directory, manifest: file, name: name}
			manifestDirs[directory] = true
		}
	}

	var fallback []string
	for _, file := range context.files {
		if !strings.HasSuffix(file, ".rs") {
			continue
		}
		if lastPart(file) == "build.rs" {
			continue
		}
		directory, ok := nearestAncestor(pathDirectory(file), manifestDirs)
		if !ok {
			fallback = append(fallback, file)
			continue
		}
		packages[directory].sources = append(packages[directory].sources, file)
	}

	for _, pkg := range packages {
		pkg.targets = rustTargets(context, pkg)
	}
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
	localDependencies, narrow, diagnostics := cargoDependencyGraph(context, packages)
	if len(fallback) > 0 {
		narrow = false
		diagnostics = append(diagnostics, Diagnostic{Path: fallback[0], Message: "Rust source outside a Cargo package broadened the workspace dependency graph"})
	}

	var units []Unit
	for _, pkg := range packages {
		configs := rustConfigInputs(context, pkg)
		for _, target := range pkg.targets {
			dependencies := []string{}
			if ownMain := mainIDs[pkg.directory]; ownMain != "" && ownMain != rustUnitID(pkg, target) {
				dependencies = append(dependencies, ownMain)
			}
			if narrow {
				for _, directory := range localDependencies[pkg.directory].forTarget(target) {
					if id := mainIDs[directory]; id != "" {
						dependencies = append(dependencies, id)
					}
				}
			} else {
				for directory, id := range mainIDs {
					if directory != pkg.directory {
						dependencies = append(dependencies, id)
					}
				}
				if len(fallback) > 0 {
					dependencies = append(dependencies, "rust:workspace-fallback")
				}
			}
			capabilities := []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies}
			if target.tests {
				capabilities = append(capabilities, CapabilityTests)
			}
			units = append(units, Unit{
				ID: rustUnitID(pkg, target), Language: LanguageRust, Mode: ModeProject,
				Capabilities: capabilities, Sources: rustTargetSources(pkg, target),
				ContextSources: pkg.sources,
				ConfigInputs:   configs, DirectDependencies: dependencies,
				Conservative: !narrow || len(pkg.targets) > 1,
			})
		}
	}
	if len(fallback) > 0 {
		dependencies := make([]string, 0, len(mainIDs))
		for _, id := range mainIDs {
			dependencies = append(dependencies, id)
		}
		units = append(units, Unit{
			ID: "rust:workspace-fallback", Language: LanguageRust, Mode: ModeProject,
			Capabilities: []Capability{CapabilitySyntax, CapabilityTypes, CapabilityDependencies},
			Sources:      fallback, ConfigInputs: rustWorkspaceConfigs(context), DirectDependencies: dependencies, Conservative: true,
		})
	}
	return units, diagnostics
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
	var targets []rustTarget
	lib := joinPath(pkg.directory, "src/lib.rs")
	if context.fileSet[lib] {
		targets = append(targets, rustTarget{kind: "lib", name: pkg.name, root: lib, main: true})
	}
	main := joinPath(pkg.directory, "src/main.rs")
	if context.fileSet[main] {
		targets = append(targets, rustTarget{kind: "bin", name: pkg.name, root: main, main: true})
	}
	for _, source := range pkg.sources {
		relative := strings.TrimPrefix(strings.TrimPrefix(source, pkg.directory), "/")
		switch {
		case strings.HasPrefix(relative, "src/bin/") && strings.Count(relative, "/") == 2 && strings.HasSuffix(relative, ".rs"):
			targets = append(targets, rustTarget{kind: "bin", name: strings.TrimSuffix(lastPart(source), ".rs"), root: source, main: true})
		case strings.HasPrefix(relative, "src/bin/") && strings.Count(relative, "/") == 3 && lastPart(source) == "main.rs":
			targets = append(targets, rustTarget{kind: "bin", name: lastPart(pathDirectory(source)), root: source, main: true})
		case strings.HasPrefix(relative, "tests/") && strings.Count(relative, "/") == 1:
			targets = append(targets, rustTarget{kind: "test", name: strings.TrimSuffix(lastPart(source), ".rs"), root: source, tests: true})
		case strings.HasPrefix(relative, "examples/") && strings.Count(relative, "/") == 1:
			targets = append(targets, rustTarget{kind: "example", name: strings.TrimSuffix(lastPart(source), ".rs"), root: source})
		case strings.HasPrefix(relative, "examples/") && strings.Count(relative, "/") == 2 && lastPart(source) == "main.rs":
			targets = append(targets, rustTarget{kind: "example", name: lastPart(pathDirectory(source)), root: source})
		case strings.HasPrefix(relative, "benches/") && strings.Count(relative, "/") == 1:
			targets = append(targets, rustTarget{kind: "bench", name: strings.TrimSuffix(lastPart(source), ".rs"), root: source, tests: true})
		case strings.HasPrefix(relative, "benches/") && strings.Count(relative, "/") == 2 && lastPart(source) == "main.rs":
			targets = append(targets, rustTarget{kind: "bench", name: lastPart(pathDirectory(source)), root: source, tests: true})
		}
	}
	if data, err := context.read(pkg.manifest); err == nil {
		for _, explicit := range explicitCargoTargets(string(data), pkg) {
			replaced := false
			for index := range targets {
				if targets[index].kind == explicit.kind && targets[index].root == explicit.root {
					targets[index] = explicit
					replaced = true
					break
				}
			}
			if !replaced && context.fileSet[explicit.root] {
				targets = append(targets, explicit)
			}
		}
	}
	if len(targets) == 0 && len(pkg.sources) > 0 {
		targets = append(targets, rustTarget{kind: "crate", name: pkg.name, root: pkg.sources[0], main: true})
	}
	return targets
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

func explicitCargoTargets(contents string, pkg *rustPackage) []rustTarget {
	type targetFields struct {
		kind string
		name string
		path string
	}
	var result []rustTarget
	current := targetFields{}
	flush := func() {
		if current.kind == "" {
			return
		}
		name := current.name
		if name == "" {
			name = pkg.name
		}
		root := current.path
		if root == "" {
			if current.kind == "lib" {
				root = "src/lib.rs"
			} else if current.kind == "bin" && name == pkg.name {
				root = "src/main.rs"
			}
		}
		if root != "" {
			result = append(result, rustTarget{
				kind: current.kind, name: name, root: joinPath(pkg.directory, root),
				main:  current.kind == "lib" || current.kind == "bin",
				tests: current.kind == "test" || current.kind == "bench",
			})
		}
		current = targetFields{}
	}
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section := strings.Trim(strings.TrimSpace(line), "[]")
			switch section {
			case "lib", "bin", "example", "test", "bench":
				current.kind = section
			}
			continue
		}
		if current.kind == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch strings.TrimSpace(parts[0]) {
		case "name":
			current.name = value
		case "path":
			current.path = value
		}
	}
	flush()
	return result
}

func rustUnitID(pkg *rustPackage, target rustTarget) string {
	return "rust:cargo:" + relativeIDPath(pkg.directory) + ":" + target.kind + ":" + target.name
}

func rustConfigInputs(context plannerContext, pkg *rustPackage) []string {
	result := []string{pkg.manifest}
	buildScript := joinPath(pkg.directory, "build.rs")
	if context.fileSet[buildScript] {
		result = append(result, buildScript)
	}
	for _, file := range context.files {
		base := lastPart(file)
		directory := pathDirectory(file)
		if (base == "Cargo.toml" || base == "Cargo.lock") && pathWithin(pkg.directory, directory) {
			result = append(result, file)
		}
		if (file == ".cargo/config" || file == ".cargo/config.toml" || strings.HasSuffix(file, "/.cargo/config") || strings.HasSuffix(file, "/.cargo/config.toml")) && pathWithin(pkg.directory, pathDirectory(pathDirectory(file))) {
			result = append(result, file)
		}
	}
	return result
}

func rustWorkspaceConfigs(context plannerContext) []string {
	var result []string
	for _, file := range context.files {
		if lastPart(file) == "Cargo.toml" || lastPart(file) == "Cargo.lock" || lastPart(file) == "build.rs" || strings.Contains(file, "/.cargo/config") {
			result = append(result, file)
		}
	}
	return result
}

func cargoPackageName(contents string) string {
	section := ""
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		if section == "package" && strings.HasPrefix(line, "name") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			}
		}
	}
	return ""
}
