package unitplan

import (
	"path"
	"strings"
)

func joinPath(parts ...string) string {
	return cleanPath(path.Join(parts...))
}

func lastPart(value string) string { return path.Base(value) }

func pathDirectory(value string) string {
	return cleanPath(path.Dir(value))
}

func hasPathSegment(pathValue, segment string) bool {
	for _, part := range strings.Split(pathValue, "/") {
		if part == segment {
			return true
		}
	}
	return false
}
