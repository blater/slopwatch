package unitplan

type resolvedCargoDependency struct {
	owner string
	kind  string
}

func cargoManifests(context plannerContext) (map[string]cargoManifest, []Diagnostic) {
	manifests := map[string]cargoManifest{}
	for _, file := range context.files {
		if lastPart(file) != "Cargo.toml" {
			continue
		}
		data, err := context.read(file)
		if err != nil {
			return manifests, cargoDiagnostic(file, err)
		}
		manifest, err := parseCargoManifest(file, string(data))
		if err != nil {
			return manifests, cargoDiagnostic(file, err)
		}
		manifests[manifest.directory] = manifest
	}
	return manifests, nil
}

func resolveCargoDependency(
	directory string,
	manifest cargoManifest,
	dependency cargoDependency,
	manifests map[string]cargoManifest,
	packages map[string]*rustPackage,
) (resolvedCargoDependency, bool, *Diagnostic) {
	resolved := dependency
	base := directory
	if dependency.workspace {
		workspace, found := owningCargoWorkspace(directory, manifests)
		if !found {
			return resolvedCargoDependency{}, false, &Diagnostic{Path: manifest.path, Message: "inherited Cargo dependency had no workspace owner; Rust dependencies were broadened"}
		}
		resolved, found = workspace.workspaceDeps[dependency.alias]
		if !found {
			return resolvedCargoDependency{}, false, &Diagnostic{Path: manifest.path, Message: "inherited Cargo dependency was absent from workspace.dependencies; Rust dependencies were broadened"}
		}
		base = workspace.directory
	}
	if resolved.path == "" {
		return resolvedCargoDependency{}, false, nil
	}
	owner := joinPath(base, resolved.path)
	pkg := packages[owner]
	if pkg == nil {
		return resolvedCargoDependency{}, false, &Diagnostic{Path: manifest.path, Message: "Cargo path dependency did not resolve to a workspace package; Rust dependencies were broadened"}
	}
	packageID := resolved.packageID
	if packageID == "" {
		packageID = resolved.alias
	}
	if packageID != "" && packageID != pkg.name {
		return resolvedCargoDependency{}, false, &Diagnostic{Path: manifest.path, Message: "Cargo path dependency package name did not match its manifest; Rust dependencies were broadened"}
	}
	return resolvedCargoDependency{owner: owner, kind: dependency.kind}, true, nil
}

func cargoDependencyGraph(context plannerContext, packages map[string]*rustPackage) (map[string]cargoDependencies, bool, []Diagnostic) {
	result := make(map[string]cargoDependencies, len(packages))
	manifests, diagnostics := cargoManifests(context)
	if len(diagnostics) > 0 {
		return result, false, diagnostics
	}
	if diagnostic := validateCargoWorkspaces(manifests, packages); diagnostic != nil {
		return result, false, []Diagnostic{*diagnostic}
	}

	for directory := range packages {
		manifest, ok := manifests[directory]
		if !ok || manifest.packageName == "" {
			return result, false, []Diagnostic{{Path: joinPath(directory, "Cargo.toml"), Message: "Cargo package metadata was incomplete; Rust dependencies were broadened"}}
		}
		for _, dependency := range manifest.dependencies {
			resolved, relevant, diagnostic := resolveCargoDependency(directory, manifest, dependency, manifests, packages)
			if diagnostic != nil {
				return result, false, []Diagnostic{*diagnostic}
			}
			if !relevant {
				continue
			}
			entry := result[directory]
			if dependency.kind == "dev" {
				entry.dev = append(entry.dev, resolved.owner)
			} else {
				entry.main = append(entry.main, resolved.owner)
			}
			result[directory] = entry
		}
	}
	return result, true, nil
}
