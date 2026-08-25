// Package sourcepath classifies source-looking paths whose repository role is
// more specific than their filename extension.
package sourcepath

import "strings"

// IsJavaResource reports whether path is beneath a conventional Java resource
// source root such as src/main/resources or src/test/resources. Java-looking
// files there are resources or templates, not compiler inputs.
func IsJavaResource(path string) bool {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(path, "\\", "/")), "/")
	for index := 0; index+2 < len(parts)-1; index++ {
		if parts[index] == "src" && parts[index+1] != "" && parts[index+2] == "resources" {
			return true
		}
	}
	return false
}
