package sourceestimate

import (
	"fmt"
	"testing"
)

// Fixed fan-out chains distinguish workspace size from increasing graph density.
// Verify resolved cross-file dependencies before timing so an unresolved shortcut
// cannot masquerade as linear analysis.
func BenchmarkWorkspaceDependencyScaling(b *testing.B) {
	for _, language := range []string{"java", "go", "typescript", "rust"} {
		for _, count := range []int{8, 32, 128, 512} {
			b.Run(fmt.Sprintf("%s/files=%d", language, count), func(b *testing.B) {
				files := make([]File, count)
				for i := range files {
					name, previous := fmt.Sprintf("Node%d", i), fmt.Sprintf("Node%d", i-1)
					var source, path string
					switch language {
					case "java":
						expression := "value + 1"
						if i > 0 {
							expression = previous + ".run(value) + 1"
						}
						source = fmt.Sprintf("public class %s { public static int run(int value){return %s;} }", name, expression)
						path = name + ".java"
					case "go":
						expression := "value + 1"
						if i > 0 {
							expression = previous + "(value) + 1"
						}
						source = fmt.Sprintf("package sample\nfunc %s(value int) int {return %s}", name, expression)
						path = fmt.Sprintf("node%d.go", i)
					case "typescript":
						imports, expression := "", "value + 1"
						if i > 0 {
							imports = fmt.Sprintf("import {%s} from './node%d';", previous, i-1)
							expression = previous + "(value) + 1"
						}
						source = imports + fmt.Sprintf("export function %s(value:number):number {return %s;}", name, expression)
						path = fmt.Sprintf("node%d.ts", i)
					case "rust":
						field, expression := "", "value + 1"
						if i > 0 {
							field = "pub previous:" + previous + ","
							expression = "self.previous.run(value) + 1"
						}
						source = fmt.Sprintf("pub struct %s {%s} impl %s {pub fn run(&self,value:i32)->i32 {%s}}", name, field, name, expression)
						path = fmt.Sprintf("node%d.rs", i)
					}
					files[i] = File{Path: path, Language: language, Source: []byte(source)}
				}
				analyze := func() map[string]Result { return analyzeScalingFiles(files) }
				initial := analyze()
				for i, file := range files {
					result, ok := initial[file.Path]
					if !ok || !result.Applicable || result.Grade == nil {
						b.Fatalf("missing graded result: %s", file.Path)
					}
					if i > 0 && len(result.Dependencies) == 0 {
						b.Fatalf("chain unresolved: %s", file.Path)
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					scalingResults = analyze()
				}
			})
		}
	}
}
