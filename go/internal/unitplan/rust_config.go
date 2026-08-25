package unitplan

import "strings"

func rustConfigInputsByPackage(context plannerContext, packages map[string]*rustPackage) map[string][]string {
	byOwner, buildScripts := rustConfigOwners(context)
	result := make(map[string][]string, len(packages))
	for directory, pkg := range packages {
		result[directory] = inheritedRustConfigs(directory, byOwner)
		result[directory] = append(result[directory], pkg.manifest)
		if buildScript := buildScripts[directory]; buildScript != "" {
			result[directory] = append(result[directory], buildScript)
		}
	}
	return result
}

func rustConfigOwners(context plannerContext) (map[string][]string, map[string]string) {
	byOwner := map[string][]string{}
	buildScripts := map[string]string{}
	for _, file := range context.files {
		owner := pathDirectory(file)
		switch lastPart(file) {
		case "Cargo.toml", "Cargo.lock":
			byOwner[owner] = append(byOwner[owner], file)
		case "build.rs":
			buildScripts[owner] = file
		}
		if isCargoConfig(file) {
			workspace := pathDirectory(owner)
			byOwner[workspace] = append(byOwner[workspace], file)
		}
	}
	return byOwner, buildScripts
}

func isCargoConfig(file string) bool {
	return file == ".cargo/config" ||
		file == ".cargo/config.toml" ||
		strings.HasSuffix(file, "/.cargo/config") ||
		strings.HasSuffix(file, "/.cargo/config.toml")
}

func inheritedRustConfigs(directory string, byOwner map[string][]string) []string {
	var result []string
	for current := directory; ; current = pathDirectory(current) {
		result = append(result, byOwner[current]...)
		if current == "." {
			return result
		}
	}
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
