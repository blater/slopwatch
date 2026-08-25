package unitplan

import "strings"

func cargoPackageName(contents string) string {
	section := ""
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if isCargoSectionLine(line) {
			section = cargoSection(line)
			continue
		}
		if section == "package" {
			if name, ok := cargoPackageField(line); ok {
				return name
			}
		}
	}
	return ""
}

func isCargoSectionLine(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
}

func cargoSection(line string) string {
	return strings.TrimSpace(strings.Trim(line, "[]"))
}

func cargoPackageField(line string) (string, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) != "name" {
		return "", false
	}
	return strings.Trim(strings.TrimSpace(parts[1]), `"'`), true
}
