package follow

import (
	"path/filepath"
	"strings"
)

// configurationLanguage recognizes the analysis-affecting inputs owned by
// the language planners. An empty language means the input can affect more
// than one selected language.
func configurationLanguage(relative string) (string, bool) {
	name := strings.ToLower(filepath.Base(relative))
	switch name {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return "go", true
	case "cargo.toml", "cargo.lock", "build.rs":
		return "rust", true
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts",
		"gradle.properties", "gradle.lockfile", "maven.config", "jvm.config":
		return "java", true
	case "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return "typescript", true
	}
	if strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json") {
		return "typescript", true
	}
	path := strings.ToLower(filepath.ToSlash(relative))
	if strings.HasSuffix(path, "/.cargo/config") || strings.HasSuffix(path, "/.cargo/config.toml") ||
		strings.HasSuffix(path, "/.mvn/maven.config") || strings.HasSuffix(path, "/.mvn/jvm.config") ||
		strings.Contains("/"+path, "/gradle/wrapper/") {
		return "", true
	}
	return "", false
}

func isTypeScriptTest(name string) bool {
	for _, suffix := range []string{
		".spec.cts", ".spec.mts", ".spec.ts", ".spec.tsx",
		".test.cts", ".test.mts", ".test.ts", ".test.tsx",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
