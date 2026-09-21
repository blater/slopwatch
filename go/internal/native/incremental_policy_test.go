package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIncrementalBackendUsesStartupConfigurationAndCurrentSources(t *testing.T) {
	for _, language := range []string{"go", "typescript", "typescript-syntax"} {
		for _, cached := range []bool{true, false} {
			for _, deleted := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/cache=%t/deleted=%t", language, cached, deleted), func(t *testing.T) {
					root := t.TempDir()
					config, original, source, before, after := "go.mod", "module example\n", "a.go", "package p\nvar A = 1\n", "package p\nvar A = 2\n"
					catalog := goTestCatalog()
					options := Options{Targets: []string{"."}, Languages: []string{"go"}, ReadCache: true}
					if language != "go" {
						config, original, source, before, after = "tsconfig.json", `{"extends":"./base.json","include":["*.ts"]}`, "a.ts", "export const A = 1;", "export const A = 2;"
						catalog = typescriptTestCatalog()
						options.Languages = []string{"typescript"}
						options.TypeScriptTypes = language != "typescript-syntax"
						writeTestFile(t, root, "base.json", `{"compilerOptions":{"strict":true}}`)
					}
					writeTestFile(t, root, config, original)
					writeTestFile(t, root, source, before)
					analyzer := newChangeTestAnalyzerWithOptions(t, root, options, catalog)
					var requests []analyzerRequest
					analyzer.runUnits = recordChangeRequests(t, &requests)
					if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
						t.Fatal(err)
					}
					if !cached {
						analyzer.SetCacheStore(nil)
					}
					if deleted {
						if err := os.Remove(filepath.Join(root, config)); err != nil {
							t.Fatal(err)
						}
					} else {
						writeTestFile(t, root, config, "invalid changed configuration")
					}
					if language != "go" {
						writeTestFile(t, root, "base.json", "invalid changed base")
					}
					writeTestFile(t, root, ".gitignore", source+"\n")
					writeTestFile(t, root, source, after)
					calls := 0
					analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
						calls++
						for path, expected := range map[string]string{config: original, source: after} {
							data, err := os.ReadFile(filepath.Join(request.Workspace, path))
							if err != nil || string(data) != expected {
								t.Fatalf("backend %s = %q, %v; want %q", path, data, err, expected)
							}
						}
						if language != "go" {
							data, err := os.ReadFile(filepath.Join(request.Workspace, "base.json"))
							if err != nil || string(data) != `{"compilerOptions":{"strict":true}}` {
								t.Fatalf("extends bytes=%q %v", data, err)
							}
						}
						return fakeBatchInputs(t, request), nil
					}
					document, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{source})
					if err != nil {
						t.Fatal(err)
					}
					assertChangePaths(t, document, replaced, []string{source})
					if calls != 1 || len(document.Files) != 1 {
						t.Fatalf("calls=%d files=%v", calls, document.Files)
					}
				})
			}
		}
	}
}

func TestInitialAnalysisStillRejectsConfigurationChurn(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example\n")
	writeTestFile(t, root, "a.go", "package p\n")
	analyzer := newChangeTestAnalyzer(t, root)
	analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
		writeTestFile(t, root, "go.mod", "module changed\n")
		return fakeBatchInputs(t, request), nil
	}
	if _, err := analyzer.Analyze(context.Background(), nil, nil); err != ErrWorkspaceChanged {
		t.Fatalf("initial churn=%v", err)
	}
	if analyzer.plan != nil {
		t.Fatal("failed startup published plan")
	}
}

func TestIncrementalDeletionOfExactFileAndDirectoryTargets(t *testing.T) {
	for _, target := range []string{"pkg/a.go", "pkg"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "go.mod", "module example\n")
			writeTestFile(t, root, "pkg/a.go", "package pkg\n")
			analyzer := newChangeTestAnalyzerWithOptions(t, root, Options{Targets: []string{target}, Languages: []string{"go"}, ReadCache: true}, goTestCatalog())
			var requests []analyzerRequest
			analyzer.runUnits = recordChangeRequests(t, &requests)
			if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(root, target)); err != nil {
				t.Fatal(err)
			}
			requests = nil
			document, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/a.go"})
			if err != nil {
				t.Fatal(err)
			}
			assertDeletedChange(t, document, replaced, requests, "pkg/a.go")
		})
	}
}

func TestFirstTypeScriptSourceUsesEmptyProjectStartupExtends(t *testing.T) {
	for _, cached := range []bool{true, false} {
		for _, deleted := range []bool{true, false} {
			for _, typed := range []bool{true, false} {
				t.Run(fmt.Sprintf("cache=%t/deleted=%t/typed=%t", cached, deleted, typed), func(t *testing.T) {
					root := t.TempDir()
					writeTestFile(t, root, "go.mod", "module example\n")
					writeTestFile(t, root, "a.go", "package p\n")
					config := `{"extends":"./base.json","include":["*.ts"]}`
					base := `{"extends":"./shared.json","compilerOptions":{"strict":true}}`
					shared := `{"compilerOptions":{"target":"ES2022"}}`
					writeTestFile(t, root, "web/tsconfig.json", config)
					writeTestFile(t, root, "web/base.json", base)
					writeTestFile(t, root, "web/shared.json", shared)
					analyzer := newChangeTestAnalyzer(t, root)
					analyzer.options.Languages = nil
					analyzer.options.TypeScriptTypes = typed
					analyzer.catalog.Languages = []string{"go", "typescript"}
					analyzer.catalog.Components[0].Support["typescript"] = "supported"
					writeTestFile(t, analyzer.root, "build/typescript/slopslap-typescript", "typescript analyzer")
					var requests []analyzerRequest
					analyzer.runUnits = recordChangeRequests(t, &requests)
					if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
						t.Fatal(err)
					}
					if !cached {
						analyzer.SetCacheStore(nil)
					}
					for _, path := range []string{"web/base.json", "web/shared.json"} {
						if deleted {
							if err := os.Remove(filepath.Join(root, path)); err != nil {
								t.Fatal(err)
							}
						} else {
							writeTestFile(t, root, path, "changed configuration")
						}
					}
					source := "export const first = 42;"
					writeTestFile(t, root, "web/first.ts", source)
					calls := 0
					analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
						calls++
						for path, expected := range map[string]string{"web/tsconfig.json": config, "web/base.json": base, "web/shared.json": shared, "web/first.ts": source} {
							data, err := os.ReadFile(filepath.Join(request.Workspace, path))
							if err != nil || string(data) != expected {
								t.Fatalf("backend %s=%q %v; want %q", path, data, err, expected)
							}
						}
						return fakeBatchInputs(t, request), nil
					}
					document, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{"web/first.ts"})
					if err != nil {
						t.Fatal(err)
					}
					assertChangePaths(t, document, replaced, []string{"web/first.ts"})
					if calls != 1 || len(document.Files) != 1 {
						t.Fatalf("calls=%d files=%v", calls, document.Files)
					}
				})
			}
		}
	}
}

func TestSyntaxIncrementalSnapshotRetainsAncestorPackageMetadataWithoutTSConfig(t *testing.T) {
	for _, cached := range []bool{true, false} {
		for _, deleted := range []bool{true, false} {
			t.Run(fmt.Sprintf("cache=%t/deleted=%t", cached, deleted), func(t *testing.T) {
				root := t.TempDir()
				metadata := map[string]string{
					"package.json":       `{"name":"root","type":"module"}`,
					"package-lock.json":  `{"lockfileVersion":3}`,
					"pkg/package.json":   `{"name":"nested","type":"commonjs"}`,
					"pkg/pnpm-lock.yaml": "lockfileVersion: '9.0'\n",
					"pkg/yarn.lock":      "# startup lock\n",
				}
				for path, contents := range metadata {
					writeTestFile(t, root, path, contents)
				}
				writeTestFile(t, root, "pkg/src/a.ts", "export const A = 1;")
				analyzer := newChangeTestAnalyzerWithOptions(t, root, Options{Targets: []string{"."}, Languages: []string{"typescript"}, ReadCache: true}, typescriptTestCatalog())
				var requests []analyzerRequest
				analyzer.runUnits = recordChangeRequests(t, &requests)
				if _, err := analyzer.Analyze(context.Background(), nil, nil); err != nil {
					t.Fatal(err)
				}
				if !cached {
					analyzer.SetCacheStore(nil)
				}
				for path := range metadata {
					if deleted {
						if err := os.Remove(filepath.Join(root, path)); err != nil {
							t.Fatal(err)
						}
					} else {
						writeTestFile(t, root, path, "changed metadata")
					}
				}
				source := "export const A = 2;"
				writeTestFile(t, root, "pkg/src/a.ts", source)
				calls := 0
				analyzer.runUnits = func(_ context.Context, _ string, request analyzerRequest) (map[string]scoreInputs, error) {
					calls++
					for path, expected := range metadata {
						data, err := os.ReadFile(filepath.Join(request.Workspace, path))
						if err != nil || string(data) != expected {
							t.Fatalf("backend %s=%q %v; want %q", path, data, err, expected)
						}
					}
					data, err := os.ReadFile(filepath.Join(request.Workspace, "pkg/src/a.ts"))
					if err != nil || string(data) != source {
						t.Fatalf("source=%q %v", data, err)
					}
					return fakeBatchInputs(t, request), nil
				}
				document, replaced, err := analyzer.AnalyzeChanges(context.Background(), []string{"pkg/src/a.ts"})
				if err != nil {
					t.Fatal(err)
				}
				assertChangePaths(t, document, replaced, []string{"pkg/src/a.ts"})
				if calls != 1 || len(document.Files) != 1 {
					t.Fatalf("calls=%d files=%v", calls, document.Files)
				}
			})
		}
	}
}
