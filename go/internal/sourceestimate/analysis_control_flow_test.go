package sourceestimate

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestUnconditionalQueriesDifferential(t *testing.T) {
	sources := []string{"", "x = 1;", "if (x) { if (y) { return z; } x = 1; } else { throw e; } y = 2;", "if (x) return y; if (z) panic(z); x = 1; return x; y = 2;", "for (;;) { while (x) { break; } } loop { x(); } switch (x) { case 1: return; } match x { y => z }", "return x; return y; throw e; panic(x); z = 1;", "{ return x; } y = 1;", "} return x; { y = 2; }", "if (", "if (x)", "if (x) {", "else", "else return x; y = 2;", "panic", "panic("}
	rng := rand.New(rand.NewSource(19))
	words := []string{"if", "else", "for", "while", "loop", "switch", "match", "return", "throw", "panic", "(", ")", "{", "}", ";", "x", "="}
	for i := 0; i < 200; i++ {
		var s strings.Builder
		count := rng.Intn(50) + 1
		for j := 0; j < count; j++ {
			s.WriteString(words[rng.Intn(len(words))])
			s.WriteByte(' ')
		}
		sources = append(sources, s.String())
	}
	for _, language := range []string{"java", "typescript", "go", "rust"} {
		for n, source := range sources {
			t.Run(fmt.Sprintf("%s/%d", language, n), func(t *testing.T) {
				body, _, _ := lex([]byte(source))
				before := append([]token(nil), body...)
				q := &unconditionalQueries{}
				op := operation{language: language, body: body}
				eager := normalizedEagerBody(&op)
				for repeat := 0; repeat < 2; repeat++ {
					for p := len(body); p >= 0; p-- {
						if got, want := q.unconditional(body, p), frozenGradedUnconditional(body, p); got != want {
							t.Fatalf("raw position %d: got %v, want %v", p, got, want)
						}
						if got, want := gradedUnconditional(body, p), frozenGradedUnconditional(body, p); got != want {
							t.Fatalf("isolated position %d: got %v, want %v", p, got, want)
						}
					}
					for p := len(eager); p >= 0; p-- {
						if got, want := normalizedEagerUnconditional(&op, p), frozenGradedUnconditional(eager, p); got != want {
							t.Fatalf("eager position %d: got %v, want %v", p, got, want)
						}
					}
				}
				if len(body) != 0 && !reflect.DeepEqual(before, body) {
					t.Fatal("tokens mutated")
				}
			})
		}
	}
}

func TestUnconditionalQueriesIsolation(t *testing.T) {
	body, _, _ := lex([]byte("if (x) { y = 1; } z = 2;"))
	op := operation{language: "java", body: body}
	normalizedEagerUnconditional(&op, len(normalizedEagerBody(&op)))
	cache := op.normalized.flow
	copy := op
	copy.owner = "contextual"
	copy.id = "different"
	normalizedEagerUnconditional(&copy, 1)
	if copy.normalized.flow != cache {
		t.Fatal("contextual copy did not share immutable-body answers")
	}
	other := operation{language: op.language, body: body}
	normalizedEagerUnconditional(&other, 1)
	if other.normalized.flow == cache {
		t.Fatal("independent operation shared cache")
	}
	replacements := []struct {
		name     string
		body     []token
		language string
	}{
		{"clone", append([]token(nil), body...), "java"}, {"shortened", body[:len(body)-1], "java"}, {"subslice", body[1:], "java"}, {"language", body, "rust"}, {"nil", nil, "java"}, {"empty", []token{}, "java"},
	}
	for _, tc := range replacements {
		t.Run(tc.name, func(t *testing.T) {
			changed := op
			changed.body, changed.language = tc.body, tc.language
			eager := normalizedEagerBody(&changed)
			for p := 0; p <= len(eager); p++ {
				if got, want := normalizedEagerUnconditional(&changed, p), frozenGradedUnconditional(eager, p); got != want {
					t.Fatalf("position %d: got %v, want %v", p, got, want)
				}
			}
			if changed.normalized.flow == cache {
				t.Fatal("replaced body reused cache")
			}
		})
	}
	// Copies made before initialization independently build equivalent indexes.
	fresh := operation{language: "java", body: body}
	beforeInit := fresh
	normalizedEagerUnconditional(&fresh, 1)
	normalizedEagerUnconditional(&beforeInit, 1)
	if fresh.normalized.flow == beforeInit.normalized.flow || !reflect.DeepEqual(fresh.normalized.flow, beforeInit.normalized.flow) {
		t.Fatal("pre-initialization copies must safely use independent equivalent indexes")
	}
	for _, emptyBody := range [][]token{nil, {}} {
		empty := operation{body: emptyBody}
		for _, position := range []int{-1, 0} {
			if !normalizedEagerUnconditional(&empty, position) || empty.normalized.flow != nil {
				t.Fatal("nonpositive position allocated index or changed result")
			}
		}
	}
	unqueried := operation{body: body}
	normalizedEagerBody(&unqueried)
	if unqueried.normalized.flow != nil {
		t.Fatal("unqueried operation allocated flow cache")
	}
}

var unconditionalBenchmarkResult bool

func BenchmarkUnconditionalQueries(b *testing.B) {
	for _, guards := range []int{16, 128} {
		body, _, _ := lex([]byte(strings.Repeat("if (x) { y = 1; } z = 2; ", guards)))
		positions := []int{}
		for i, tok := range body {
			if tok.text == "=" || tok.text == "if" {
				positions = append(positions, i)
			}
		}
		b.Run(fmt.Sprintf("guards_%d", guards), func(b *testing.B) {
			b.Run("original", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					for _, p := range positions {
						unconditionalBenchmarkResult = frozenGradedUnconditional(body, p)
					}
				}
			})
			b.Run("construct", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					op := operation{body: body, language: "java"}
					for _, p := range positions {
						unconditionalBenchmarkResult = normalizedEagerUnconditional(&op, p)
					}
				}
			})
			b.Run("reuse", func(b *testing.B) {
				op := operation{body: body, language: "java"}
				for _, p := range positions {
					normalizedEagerUnconditional(&op, p)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for _, p := range positions {
						unconditionalBenchmarkResult = normalizedEagerUnconditional(&op, p)
					}
				}
			})
		})
	}
}

func BenchmarkUnconditionalScaling(b *testing.B) {
	for _, count := range []int{128, 512, 2048, 8192} {
		for _, source := range []struct{ name, text string }{
			{"guards", "if (x) { y = 1; } z = 2; "},
			{"exits", "if (x) return y; panic(z); return z; "},
		} {
			tokens, _, _ := lex([]byte(strings.Repeat(source.text, count)))
			body := tokens[:count]
			b.Run(fmt.Sprintf("%s/tokens_%d", source.name, count), func(b *testing.B) {
				b.Run("construct_all", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						q := unconditionalQueries{}
						for p := 0; p <= len(body); p++ {
							unconditionalBenchmarkResult = q.unconditional(body, p)
						}
					}
				})
				b.Run("original_all", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						for p := 0; p <= len(body); p++ {
							unconditionalBenchmarkResult = frozenGradedUnconditional(body, p)
						}
					}
				})
			})
		}
	}
}

func TestUnconditionalStructuredDelimiters(t *testing.T) {
	fixtures := map[string]string{
		"nested-balanced":             "if ((x)) { while (y) { if (z) return a; } } else { for (;;) { throw e; } } next();",
		"nested-unclosed-brace":       "if (x) { while (y) { return a; } next();",
		"nested-unclosed-parenthesis": "if ((x) { return a; } next();",
		"crossed-delimiters":          "if ({x) { return a; }) next();",
		"extra-close-brace":           "} if (x) { return a; } { return b; } next();",
		"adjacent-conditional-exits":  "if (x) return a; if (y) throw b; if (z) panic(z); next();",
	}
	for name, source := range fixtures {
		t.Run(name, func(t *testing.T) {
			body, _, _ := lex([]byte(source))
			q := unconditionalQueries{}
			for p := 0; p <= len(body); p++ {
				if got, want := q.unconditional(body, p), frozenGradedUnconditional(body, p); got != want {
					t.Fatalf("position %d: got %v, want %v", p, got, want)
				}
			}
		})
	}
}

func TestConsumerFlowAfterNormalizationShiftsPositions(t *testing.T) {
	const live = "int alias = a; if (alias > b) { return; } useful();"
	var expected []gradedConsumerConstraint
	for _, prefix := range []string{"", "if (false) { if (hidden) { return; } dead(); } "} {
		u := inventoryTestUnit("java", "Example.java", "class Example { void consume(int a, int b) { "+prefix+live+" } }")
		op := u.ops[0]
		if prefix != "" && len(normalizedEagerBody(op)) >= len(op.body) {
			t.Fatal("fixture did not shift positions")
		}
		for repeat := 0; repeat < 2; repeat++ {
			got := gradedConsumerConstraints(op, u, []unit{u}, nil, map[string]map[string]bool{}, 0)
			if len(got) != 1 || !got[0].fields["a"] || !got[0].fields["b"] || len(got[0].fields) != 2 {
				t.Fatalf("unexpected consumer constraints: %+v", got)
			}
			if expected != nil && (!reflect.DeepEqual(expected[0].fields, got[0].fields) || expected[0].lifecycle != got[0].lifecycle) {
				t.Fatal("normalization changed consumer constraint")
			}
			expected = got
		}
	}
}

func BenchmarkUnconditionalColdSparse(b *testing.B) {
	for _, guards := range []int{16, 128} {
		body, _, _ := lex([]byte(strings.Repeat("if (x) { y = 1; } z = 2; ", guards)))
		for _, count := range []int{1, 4} {
			b.Run(fmt.Sprintf("guards_%d/queries_%d", guards, count), func(b *testing.B) {
				b.Run("index", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						q := unconditionalQueries{}
						for p := 1; p <= count; p++ {
							unconditionalBenchmarkResult = q.unconditional(body, p*len(body)/count)
						}
					}
				})
				b.Run("original", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						for p := 1; p <= count; p++ {
							unconditionalBenchmarkResult = frozenGradedUnconditional(body, p*len(body)/count)
						}
					}
				})
			})
		}
	}
}

func BenchmarkUnconditionalBalancedExits(b *testing.B) {
	for _, count := range []int{8, 32, 128, 512} {
		body, _, _ := lex([]byte(strings.Repeat("if (x) return y; if (z) { panic(z); } next(); ", count)))
		b.Run(fmt.Sprintf("tokens_%d", len(body)), func(b *testing.B) {
			b.Run("construct_all", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					q := unconditionalQueries{}
					for p := 0; p <= len(body); p++ {
						unconditionalBenchmarkResult = q.unconditional(body, p)
					}
				}
			})
			// The frozen recursive reference expands exponentially with adjacent
			// conditional returns; keep its measurement bounded to eight guards.
			if count == 8 {
				b.Run("original_all", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						for p := 0; p <= len(body); p++ {
							unconditionalBenchmarkResult = frozenGradedUnconditional(body, p)
						}
					}
				})
			}
		})
	}
}

func TestUnconditionalBoundaryFallback(t *testing.T) {
	for _, source := range []string{"x = 1;", "return x;", "return x", "if (x) return y", ""} {
		body, _, _ := lex([]byte(source))
		op := operation{body: body}
		for _, p := range []int{-1, 0} {
			if !normalizedEagerUnconditional(&op, p) || op.normalized.flow != nil || op.normalized.ready {
				t.Fatal("nonpositive query normalized or allocated")
			}
		}
		evaluate := func(f func() bool) (answer bool, panicked bool) {
			defer func() {
				if recover() != nil {
					panicked = true
				}
			}()
			answer = f()
			return
		}
		got, gotPanic := evaluate(func() bool { return normalizedEagerUnconditional(&op, len(body)+1) })
		want, wantPanic := evaluate(func() bool { return frozenGradedUnconditional(body, len(body)+1) })
		if got != want || gotPanic != wantPanic {
			t.Fatalf("out-of-range fallback changed for %q", source)
		}
	}
}

func TestExactBodyFlowReuseAndReplacement(t *testing.T) {
	body, _, _ := lex([]byte("if (x) { return a; } return b; after();"))
	op := operation{body: body, language: "java"}
	operationBodyUnconditional(&op, body, 1)
	original := op.normalized.flow
	copy := op
	variants := [][]token{body[:len(body)-2], body[1:], append([]token(nil), body...)}
	for _, variant := range variants {
		for p := 0; p <= len(variant); p++ {
			if got, want := operationBodyUnconditional(&copy, variant, p), frozenGradedUnconditional(variant, p); got != want {
				t.Fatalf("replaced position %d: got %v, want %v", p, got, want)
			}
		}
		if copy.normalized.flow == original || op.normalized.flow != original {
			t.Fatal("body replacement modified shared index")
		}
	}
	// A local pass can shorten its body after locating an unconditional return.
	// Its cache must rebuild because unmatched guards/statement ends can change.
	var local unconditionalQueries
	for _, variant := range append([][]token{body}, variants...) {
		for p := 0; p <= len(variant); p++ {
			if got, want := local.unconditional(variant, p), frozenGradedUnconditional(variant, p); got != want {
				t.Fatalf("local position %d: got %v, want %v", p, got, want)
			}
		}
	}
}

func TestProductionUnconditionalManyConditionalExits(t *testing.T) {
	body, _, _ := lex([]byte(strings.Repeat("if (x) return y; ", 512) + "next();"))
	if !gradedUnconditional(body, len(body)) {
		t.Fatal("conditional exits made later call unreachable")
	}
	op := operation{body: body}
	operationBodyUnconditional(&op, body, 1)
	index := op.normalized.flow
	for p := 0; p <= len(body); p++ {
		operationBodyUnconditional(&op, body, p)
	}
	if op.normalized.flow != index {
		t.Fatal("dense queries rebuilt index")
	}
}
