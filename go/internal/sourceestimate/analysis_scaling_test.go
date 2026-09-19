package sourceestimate

import (
	"fmt"
	"strings"
	"testing"
)

var scalingResults map[string]Result

// Exercise the same shared pipeline as native.withSourceDepthEstimates, including
// cross-file caller/constraint annotation for every supported language.
func analyzeScalingFiles(files []File) map[string]Result {
	_, results := AnalyzeWithAttribution(files)
	return results
}

// Keep source shape per file fixed so growth measures workspace size rather
// than a changing mix of large methods. This is a diagnostic benchmark, not
// enterprise acceptance or a representative connected dependency graph.
func BenchmarkWorkspaceFileScaling(b *testing.B) {
	for _, language := range []string{"java", "go", "typescript", "rust"} {
		for _, count := range []int{8, 32, 128, 512} {
			b.Run(fmt.Sprintf("%s/files=%d", language, count), func(b *testing.B) {
				files := make([]File, count)
				bytes := 0
				for i := range files {
					owner := fmt.Sprintf("Example%d", i)
					var source, ext string
					switch language {
					case "java":
						ext = "java"
						source = fmt.Sprintf("public class %s { public int[] data; public int count; public int read(){ if(count < 0 || count >= data.length) throw new IllegalArgumentException(); return data[count]; } }", owner)
					case "go":
						ext = "go"
						source = fmt.Sprintf("package sample\ntype %s struct { Data []int; Count int }; func(e *%s) Read() int { if e.Count < 0 || e.Count >= len(e.Data) { panic(\"bounds\") }; return e.Data[e.Count] }", owner, owner)
					case "typescript":
						ext = "ts"
						source = fmt.Sprintf("export class %s { data:number[]=[]; count:number=0; read():number { if(this.count < 0 || this.count >= this.data.length) throw new Error(); return this.data[this.count]; } }", owner)
					case "rust":
						ext = "rs"
						source = fmt.Sprintf("pub struct %s { pub data:Vec<i32>, pub count:usize } impl %s { pub fn read(&self)->i32 { if self.count >= self.data.len() { panic!(\"bounds\"); } self.data[self.count] } }", owner, owner)
					}
					files[i] = File{Path: fmt.Sprintf("sample/%s.%s", owner, ext), Language: language, Source: []byte(source)}
					bytes += len(source)
				}
				verify := analyzeScalingFiles(files)
				if len(verify) != len(files) {
					b.Fatalf("got %d results for %d files", len(verify), len(files))
				}
				for _, file := range files {
					if result, ok := verify[file.Path]; !ok || !result.Applicable || result.Grade == nil {
						b.Fatalf("missing applicable graded result for %s", file.Path)
					}
				}
				b.ReportAllocs()
				b.SetBytes(int64(bytes))
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					scalingResults = analyzeScalingFiles(files)
				}
			})
		}
	}
}

var scalingCalls []call

// This isolates call extraction. Exact nested argument signatures can contain
// quadratic output bytes, but extraction now borrows spans without emitting them.
func BenchmarkNestedCallScaling(b *testing.B) {
	for _, depth := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			body, _, _ := lex([]byte(strings.Repeat("f(", depth) + "x" + strings.Repeat(")", depth)))
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				scalingCalls = callsIn(body)
			}
		})
	}
}

// One unresolved release query per unit models an unfavorable workspace shape.
// It measures the lookup itself, not an assertion that every Rust file invokes it.
// BenchmarkRustReleaseFallbackScaling exercises unprepared compatibility lookups.
// Production prepared-workspace coverage is BenchmarkRustReleaseInventoryScaling.
func BenchmarkRustReleaseFallbackScaling(b *testing.B) {
	for _, count := range []int{8, 32, 128} {
		b.Run(fmt.Sprintf("files=%d", count), func(b *testing.B) {
			units := make([]unit, count)
			for i := range units {
				units[i] = inventoryTestUnit("rust", fmt.Sprintf("r%d.rs", i), fmt.Sprintf("struct Resource%d {} impl Resource%d { fn close(&self) {} }", i, i))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				for range units {
					gradedRustRelease(nil, unit{}, nil, units, nil, false, gradedRustGuard{}, "Missing", "close")
				}
			}
		})
	}
}

// Grow fields and short methods together while keeping each method's actual
// work fixed. This detects declaration-cross-product work hidden by file-count
// benchmarks. The largest case stays below parser token/operation limits.
func BenchmarkDeclarationScaling(b *testing.B) {
	for _, language := range []string{"java", "go", "typescript", "rust"} {
		for _, count := range []int{8, 32, 128, 256} {
			b.Run(fmt.Sprintf("%s/declarations=%d", language, count), func(b *testing.B) {
				var fields, methods strings.Builder
				for i := 0; i < count; i++ {
					switch language {
					case "java":
						fmt.Fprintf(&fields, "public int f%d;", i)
						fmt.Fprintf(&methods, "public int m%d(int value){this.f0 += value;return this.f0;}", i)
					case "go":
						fmt.Fprintf(&fields, "F%d int;", i)
						fmt.Fprintf(&methods, "func(e *Example) M%d(value int) int { e.F0 += value; return e.F0 };", i)
					case "typescript":
						fmt.Fprintf(&fields, "f%d:number=0;", i)
						fmt.Fprintf(&methods, "m%d(value:number):number {this.f0 += value;return this.f0;}", i)
					case "rust":
						fmt.Fprintf(&fields, "pub f%d:i32,", i)
						fmt.Fprintf(&methods, "pub fn m%d(&mut self,value:i32)->i32 {self.f0 += value;self.f0}", i)
					}
				}
				var source, ext string
				switch language {
				case "java":
					source = "public class Example {" + fields.String() + methods.String() + "}"
					ext = "java"
				case "go":
					source = "package sample; type Example struct {" + fields.String() + "};" + methods.String()
					ext = "go"
				case "typescript":
					source = "export class Example {" + fields.String() + methods.String() + "}"
					ext = "ts"
				case "rust":
					source = "pub struct Example {" + fields.String() + "} impl Example {" + methods.String() + "}"
					ext = "rs"
				}
				file := File{Path: "example." + ext, Language: language, Source: []byte(source)}
				analyze := func() map[string]Result { return analyzeScalingFiles([]File{file}) }
				tokens, limited, valid := lex(file.Source)
				if limited || !valid {
					b.Fatal("invalid/limited scaling fixture")
				}
				if len(findOperations(file, 0, tokens, packageName(language, tokens))) != count {
					b.Fatal("operation count changed")
				}
				result := analyze()
				if r, ok := result[file.Path]; !ok || !r.Applicable || r.Grade == nil {
					b.Fatal("missing applicable grade")
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(source)))
				b.ResetTimer()
				for n := 0; n < b.N; n++ {
					scalingResults = analyze()
				}
			})
		}
	}
}
