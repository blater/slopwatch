package unitplan

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestCrossLanguageUnitMatrix(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		options     Options
		wantID      string
		wantMode    Mode
		wantSources []string
	}{
		{"go package", map[string]string{"go.mod": "module example.test/app\n", "main.go": "package main\n"}, Options{}, "go:package:root", ModeProject, []string{"main.go"}},
		{"maven source set", map[string]string{"pom.xml": "<project/>", "src/main/java/App.java": "class App {}"}, Options{}, "java:maven:root:main", ModeProject, []string{"src/main/java/App.java"}},
		{"cargo library", map[string]string{"Cargo.toml": "[package]\nname = \"kit\"\n", "src/lib.rs": "pub fn f() {}\n"}, Options{}, "rust:cargo:root:lib:kit", ModeProject, []string{"src/lib.rs"}},
		{"typescript syntax", map[string]string{"src/app.ts": "export const n = 1;\n"}, Options{TypeScriptMode: TypeScriptSyntax}, "typescript:syntax:src/app.ts", ModeSyntax, []string{"src/app.ts"}},
		{"typescript typed", map[string]string{"tsconfig.json": "{}", "src/app.ts": "export const n = 1;\n"}, Options{}, "typescript:typed:tsconfig.json", ModeTyped, []string{"src/app.ts"}},
		{"java fallback", map[string]string{"loose/App.java": "class App {}"}, Options{}, "java:workspace-fallback", ModeProject, []string{"loose/App.java"}},
		{"rust fallback", map[string]string{"loose.rs": "fn main() {}\n"}, Options{}, "rust:workspace-fallback", ModeProject, []string{"loose.rs"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t, test.files)
			plan, err := PlanWorkspace(root, test.options)
			if err != nil {
				t.Fatal(err)
			}
			unit := findUnit(t, plan, test.wantID)
			if unit.Mode != test.wantMode || !reflect.DeepEqual(unit.Sources, test.wantSources) {
				t.Fatalf("unit = mode %q sources %v, want mode %q sources %v", unit.Mode, unit.Sources, test.wantMode, test.wantSources)
			}
			if unit.Language == "" || len(unit.Capabilities) == 0 {
				t.Fatalf("unit omitted language/capabilities: %#v", unit)
			}
			again, err := PlanWorkspace(root, test.options)
			if err != nil || !reflect.DeepEqual(plan, again) {
				t.Fatalf("plan is not stable: second error %v\nfirst %#v\nsecond %#v", err, plan, again)
			}
		})
	}
}

func TestGoDependenciesConfigAndTypeContext(t *testing.T) {
	root := fixture(t, map[string]string{
		"go.work":         "go 1.25\nuse .\n",
		"go.work.sum":     "workspace sum\n",
		"go.mod":          "module example.test/repo\n",
		"go.sum":          "module sum\n",
		"lib/lib.go":      "package lib\n",
		"app/app.go":      "package app\nimport _ \"example.test/repo/lib\"\n",
		"app/app_test.go": "package app\nimport \"testing\"\nfunc TestApp(*testing.T) {}\n",
	})
	plan, err := PlanWorkspace(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	app := findUnit(t, plan, "go:package:app")
	if !reflect.DeepEqual(app.DirectDependencies, []string{"go:package:lib"}) ||
		!reflect.DeepEqual(app.ContextSources, []string{"lib/lib.go"}) {
		t.Fatalf("app dependency snapshot = deps %v context %v", app.DirectDependencies, app.ContextSources)
	}
	wantConfig := []string{"go.mod", "go.sum", "go.work", "go.work.sum"}
	if !reflect.DeepEqual(app.ConfigInputs, wantConfig) {
		t.Fatalf("config inputs = %v, want %v", app.ConfigInputs, wantConfig)
	}
	lib := findUnit(t, plan, "go:package:lib")
	if !reflect.DeepEqual(lib.ReverseDependencies, []string{"go:package:app"}) {
		t.Fatalf("reverse dependencies = %v", lib.ReverseDependencies)
	}
	testUnit := findUnit(t, plan, "go:test-package:app")
	if !reflect.DeepEqual(testUnit.DirectDependencies, []string{"go:package:app"}) ||
		!reflect.DeepEqual(testUnit.ContextSources, []string{"app/app.go", "lib/lib.go"}) {
		t.Fatalf("test snapshot = deps %v context %v", testUnit.DirectDependencies, testUnit.ContextSources)
	}
}

func TestTypeScriptExtendsReferenceAndConfigOwnership(t *testing.T) {
	root := fixture(t, map[string]string{
		"package.json":               "{}",
		"package-lock.json":          "{}",
		"tsconfig.base.json":         "{ // shared\n \"compilerOptions\": {\"strict\": true,},\n}",
		"packages/lib/tsconfig.json": "{\"compilerOptions\": {\"composite\": true}}",
		"packages/lib/src/lib.ts":    "export const value = 1;\n",
		"packages/app/tsconfig.json": "{\"extends\": \"../../tsconfig.base.json\", \"references\": [{\"path\": \"../lib\"},],}",
		"packages/app/src/app.ts":    "export const app = 1;\n",
	})
	plan, err := PlanWorkspace(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	app := findUnit(t, plan, "typescript:typed:packages/app/tsconfig.json")
	wantConfig := []string{"package-lock.json", "package.json", "packages/app/tsconfig.json", "tsconfig.base.json"}
	if !reflect.DeepEqual(app.ConfigInputs, wantConfig) {
		t.Fatalf("config inputs = %v, want %v", app.ConfigInputs, wantConfig)
	}
	wantDependency := []string{"typescript:typed:packages/lib/tsconfig.json"}
	if !reflect.DeepEqual(app.DirectDependencies, wantDependency) || !reflect.DeepEqual(app.ContextSources, []string{"packages/lib/src/lib.ts"}) {
		t.Fatalf("project dependency = deps %v context %v", app.DirectDependencies, app.ContextSources)
	}
	lib := findUnit(t, plan, wantDependency[0])
	if !reflect.DeepEqual(lib.ReverseDependencies, []string{app.ID}) {
		t.Fatalf("reverse dependencies = %v", lib.ReverseDependencies)
	}
	for _, unit := range plan.Units {
		if unit.ID == "typescript:typed:tsconfig.base.json" {
			t.Fatal("extends-only base config became an analysis project")
		}
	}
}

func TestRustAndJavaBuildInputsAreOwned(t *testing.T) {
	root := fixture(t, map[string]string{
		"Cargo.toml":                         "[workspace]\nmembers=[\"crates/core\"]\n",
		"Cargo.lock":                         "# lock\n",
		".cargo/config.toml":                 "[build]\n",
		"crates/core/Cargo.toml":             "[package]\nname=\"core\"\n[[bin]]\nname=\"tool\"\npath=\"cmd/tool.rs\"\n",
		"crates/core/build.rs":               "fn main() {}\n",
		"crates/core/cmd/tool.rs":            "fn main() {}\n",
		"crates/core/cmd/shared.rs":          "fn shared() {}\n",
		"crates/core/src/lib.rs":             "pub fn core() {}\n",
		"pom.xml":                            "<project/>",
		"service/pom.xml":                    "<project/>",
		"service/.mvn/maven.config":          "-T1\n",
		"service/src/test/java/AppTest.java": "class AppTest {}",
	})
	plan, err := PlanWorkspace(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rust := findUnit(t, plan, "rust:cargo:crates/core:lib:core")
	wantRust := []string{".cargo/config.toml", "Cargo.lock", "Cargo.toml", "crates/core/Cargo.toml", "crates/core/build.rs"}
	if !reflect.DeepEqual(rust.ConfigInputs, wantRust) {
		t.Fatalf("rust config inputs = %v, want %v", rust.ConfigInputs, wantRust)
	}
	customTarget := findUnit(t, plan, "rust:cargo:crates/core:bin:tool")
	if !contains(customTarget.Sources, "crates/core/cmd/tool.rs") || !contains(customTarget.ContextSources, "crates/core/cmd/shared.rs") {
		t.Fatalf("custom cargo target snapshot = sources %v context %v", customTarget.Sources, customTarget.ContextSources)
	}
	java := findUnit(t, plan, "java:maven:service:test")
	wantJava := []string{"pom.xml", "service/.mvn/maven.config", "service/pom.xml"}
	if !reflect.DeepEqual(java.ConfigInputs, wantJava) {
		t.Fatalf("java config inputs = %v, want %v", java.ConfigInputs, wantJava)
	}
}

func TestJavaMetadataNarrowsMultiModuleDependencies(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		ids   [3]string
	}{
		{
			name: "maven coordinates",
			files: map[string]string{
				"pom.xml":                           `<project><groupId>dev.test</groupId><artifactId>root</artifactId><modules><module>core</module><module>app</module><module>unrelated</module></modules></project>`,
				"core/pom.xml":                      `<project><parent><groupId>dev.test</groupId><artifactId>root</artifactId></parent><artifactId>core</artifactId></project>`,
				"core/src/main/java/Core.java":      `class Core {}`,
				"app/pom.xml":                       `<project><parent><groupId>dev.test</groupId><artifactId>root</artifactId></parent><artifactId>app</artifactId><dependencies><dependency><groupId>dev.test</groupId><artifactId>core</artifactId></dependency></dependencies></project>`,
				"app/src/main/java/App.java":        `class App {}`,
				"unrelated/pom.xml":                 `<project><parent><groupId>dev.test</groupId><artifactId>root</artifactId></parent><artifactId>unrelated</artifactId></project>`,
				"unrelated/src/main/java/Side.java": `class Side {}`,
			},
			ids: [3]string{"java:maven:core:main", "java:maven:app:main", "java:maven:unrelated:main"},
		},
		{
			name: "gradle project dependency",
			files: map[string]string{
				"settings.gradle.kts":               `include(":core", ":app", ":unrelated")`,
				"build.gradle.kts":                  `plugins { java }`,
				"core/build.gradle.kts":             `plugins { java }`,
				"core/src/main/java/Core.java":      `class Core {}`,
				"app/build.gradle.kts":              `dependencies { implementation(project(":core")) }`,
				"app/src/main/java/App.java":        `class App {}`,
				"unrelated/build.gradle.kts":        `plugins { java }`,
				"unrelated/src/main/java/Side.java": `class Side {}`,
			},
			ids: [3]string{"java:gradle:core:main", "java:gradle:app:main", "java:gradle:unrelated:main"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := PlanWorkspace(fixture(t, test.files), Options{})
			if err != nil {
				t.Fatal(err)
			}
			assertNarrowDependency(t, plan, test.ids[0], test.ids[1], test.ids[2])
		})
	}
}

func TestCargoMetadataNarrowsMultiCrateDependencies(t *testing.T) {
	plan, err := PlanWorkspace(fixture(t, map[string]string{
		"Cargo.toml":           "[workspace]\nmembers = [\"core\", \"app\", \"unrelated\"]\n",
		"core/Cargo.toml":      "[package]\nname = \"core\"\n",
		"core/src/lib.rs":      "pub fn core() {}\n",
		"app/Cargo.toml":       "[package]\nname = \"app\"\n[dependencies]\ncore = { path = \"../core\" }\n",
		"app/src/lib.rs":       "pub fn app() {}\n",
		"unrelated/Cargo.toml": "[package]\nname = \"unrelated\"\n",
		"unrelated/src/lib.rs": "pub fn side() {}\n",
	}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	assertNarrowDependency(t, plan,
		"rust:cargo:core:lib:core", "rust:cargo:app:lib:app", "rust:cargo:unrelated:lib:unrelated")
}

func TestAmbiguousMetadataRetainsBroadDependencies(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		ownerID string
		otherID string
	}{
		{
			name: "malformed Maven",
			files: map[string]string{
				"a/pom.xml":              `<project>`,
				"a/src/main/java/A.java": `class A {}`,
				"b/pom.xml":              `<project><groupId>x</groupId><artifactId>b</artifactId></project>`,
				"b/src/main/java/B.java": `class B {}`,
			},
			ownerID: "java:maven:a:main", otherID: "java:maven:b:main",
		},
		{
			name: "unresolved Cargo path",
			files: map[string]string{
				"Cargo.toml":   "[workspace]\nmembers=[\"a\", \"b\"]\n",
				"a/Cargo.toml": "[package]\nname=\"a\"\n[dependencies]\nb={path=\"../missing\"}\n",
				"a/src/lib.rs": "pub fn a() {}\n",
				"b/Cargo.toml": "[package]\nname=\"b\"\n",
				"b/src/lib.rs": "pub fn b() {}\n",
			},
			ownerID: "rust:cargo:a:lib:a", otherID: "rust:cargo:b:lib:b",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := PlanWorkspace(fixture(t, test.files), Options{})
			if err != nil {
				t.Fatal(err)
			}
			owner := findUnit(t, plan, test.ownerID)
			if !owner.Conservative || !contains(owner.DirectDependencies, test.otherID) || len(plan.Diagnostics) == 0 {
				t.Fatalf("ambiguous graph was not broadened: unit %#v diagnostics %#v", owner, plan.Diagnostics)
			}
		})
	}
}

func assertNarrowDependency(t *testing.T, plan Plan, dependencyID, consumerID, unrelatedID string) {
	t.Helper()
	dependency := findUnit(t, plan, dependencyID)
	consumer := findUnit(t, plan, consumerID)
	unrelated := findUnit(t, plan, unrelatedID)
	if consumer.Conservative || !reflect.DeepEqual(consumer.DirectDependencies, []string{dependencyID}) {
		t.Fatalf("consumer dependencies = %v, conservative=%v", consumer.DirectDependencies, consumer.Conservative)
	}
	if !reflect.DeepEqual(dependency.ReverseDependencies, []string{consumerID}) {
		t.Fatalf("dependency reverse edges = %v", dependency.ReverseDependencies)
	}
	if len(unrelated.DirectDependencies) != 0 || len(unrelated.ReverseDependencies) != 0 {
		t.Fatalf("unrelated unit was connected: direct %v reverse %v", unrelated.DirectDependencies, unrelated.ReverseDependencies)
	}
	closure := reverseClosure(plan, dependencyID)
	if !contains(closure, consumerID) || contains(closure, unrelatedID) {
		t.Fatalf("invalidation closure = %v", closure)
	}
}

func reverseClosure(plan Plan, changed string) []string {
	byID := map[string]Unit{}
	for _, unit := range plan.Units {
		byID[unit.ID] = unit
	}
	seen := map[string]bool{changed: true}
	queue := []string{changed}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, reverse := range byID[current].ReverseDependencies {
			if !seen[reverse] {
				seen[reverse] = true
				queue = append(queue, reverse)
			}
		}
	}
	result := make([]string, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(files[path]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func findUnit(t *testing.T, plan Plan, id string) Unit {
	t.Helper()
	for _, unit := range plan.Units {
		if unit.ID == id {
			return unit
		}
	}
	t.Fatalf("unit %q not found in %v", id, unitIDs(plan.Units))
	return Unit{}
}

func unitIDs(units []Unit) []string {
	result := make([]string, len(units))
	for index, unit := range units {
		result[index] = unit.ID
	}
	return result
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
