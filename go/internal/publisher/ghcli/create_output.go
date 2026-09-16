package ghcli

import "strings"

func lastNonemptyLine(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if value := strings.TrimSpace(lines[index]); value != "" {
			return value
		}
	}
	return ""
}
