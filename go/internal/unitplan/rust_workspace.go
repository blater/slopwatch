package unitplan

import (
	"fmt"
	pathpkg "path"
	"strings"
)

func cargoDiagnostic(path string, err error) []Diagnostic {
	return []Diagnostic{{Path: path, Message: fmt.Sprintf("Cargo metadata could not be parsed (%v); Rust dependencies were broadened", err)}}
}

func validateCargoWorkspaces(manifests map[string]cargoManifest, packages map[string]*rustPackage) *Diagnostic {
	for _, manifest := range manifests {
		if !manifest.workspace {
			continue
		}
		for _, member := range manifest.members {
			if !cargoWorkspaceMemberExists(member, manifest, packages) {
				return &Diagnostic{Path: manifest.path, Message: "Cargo workspace member did not resolve; Rust dependencies were broadened"}
			}
		}
	}
	return nil
}

func cargoWorkspaceMemberExists(member string, manifest cargoManifest, packages map[string]*rustPackage) bool {
	for directory := range packages {
		relative := strings.TrimPrefix(strings.TrimPrefix(directory, manifest.directory), "/")
		if cargoMemberMatch(member, relative) {
			return true
		}
	}
	return false
}

func cargoMemberMatch(pattern, relative string) bool {
	if strings.ContainsAny(pattern, "*?[") {
		matched, err := pathpkg.Match(pattern, relative)
		return err == nil && matched
	}
	return cleanPath(pattern) == cleanPath(relative)
}

func owningCargoWorkspace(directory string, manifests map[string]cargoManifest) (cargoManifest, bool) {
	best := cargoManifest{}
	found := false
	for _, manifest := range manifests {
		if manifest.workspace && pathWithin(directory, manifest.directory) && (!found || len(manifest.directory) > len(best.directory)) {
			best, found = manifest, true
		}
	}
	return best, found
}
