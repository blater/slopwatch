package unitplan

import "strings"

type cargoTargetFields struct {
	kind string
	name string
	path string
}

type cargoTargetParser struct {
	pkg     *rustPackage
	current cargoTargetFields
	result  []rustTarget
}

func explicitCargoTargets(contents string, pkg *rustPackage) []rustTarget {
	parser := cargoTargetParser{pkg: pkg}
	for _, line := range strings.Split(contents, "\n") {
		parser.consume(line)
	}
	parser.flush()
	return parser.result
}

func (parser *cargoTargetParser) consume(line string) {
	line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
	if isCargoSection(line) {
		parser.flush()
		parser.current.kind = cargoTargetKind(line)
		return
	}
	if parser.current.kind == "" {
		return
	}
	key, value, ok := cargoAssignment(line)
	if !ok {
		return
	}
	switch key {
	case "name":
		parser.current.name = value
	case "path":
		parser.current.path = value
	}
}

func isCargoSection(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func cargoTargetKind(line string) string {
	section := strings.Trim(strings.TrimSpace(line), "[]")
	switch section {
	case "lib", "bin", "example", "test", "bench":
		return section
	default:
		return ""
	}
}

func cargoAssignment(line string) (string, string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.Trim(strings.TrimSpace(parts[1]), `"'`), true
}

func (parser *cargoTargetParser) flush() {
	if parser.current.kind == "" {
		return
	}
	name := parser.current.name
	if name == "" {
		name = parser.pkg.name
	}
	root := parser.current.path
	if root == "" {
		root = defaultCargoTargetRoot(parser.current.kind, name, parser.pkg.name)
	}
	if root != "" {
		parser.result = append(parser.result, rustTarget{
			kind: parser.current.kind, name: name, root: joinPath(parser.pkg.directory, root),
			main:  parser.current.kind == "lib" || parser.current.kind == "bin",
			tests: parser.current.kind == "test" || parser.current.kind == "bench",
		})
	}
	parser.current = cargoTargetFields{}
}

func defaultCargoTargetRoot(kind, name, packageName string) string {
	if kind == "lib" {
		return "src/lib.rs"
	}
	if kind == "bin" && name == packageName {
		return "src/main.rs"
	}
	return ""
}
