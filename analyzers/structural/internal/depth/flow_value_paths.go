package depth

func rootIdentity(root AliasRoot) string { root.Path = nil; return aliasKey(root) }
func locationString(root AliasRoot, path []PathSegment) string {
	root = cloneRoot(root)
	root.Path = append(root.Path, path...)
	return aliasKey(root)
}
func dynamicPath(path []PathSegment) bool {
	for _, segment := range path {
		if segment.Dynamic {
			return true
		}
	}
	return false
}
func pathEqual(a, b []PathSegment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func pathsOverlap(a, b []PathSegment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !segmentsOverlap(a[i], b[i]) {
			return false
		}
	}
	return true
}
func segmentsOverlap(a, b PathSegment) bool {
	if a.Kind != b.Kind {
		return false
	}
	return a.Dynamic || b.Dynamic || a.Name == b.Name
}
