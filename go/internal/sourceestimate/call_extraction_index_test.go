package sourceestimate

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func TestCallExtractionDifferential(t *testing.T) {
	if got := (call{name: "f", signature: "explicit/exact"}).callSignature(); got != "explicit/exact" {
		t.Fatalf("prebuilt signature changed: %q", got)
	}
	rng := rand.New(rand.NewSource(731))
	alphabet := []string{"(", ")", "[", "]", "{", "}", ",", "f", "g", ".", "::", "123", "if", " ", ""}
	inputs := [][]token{nil, {}, {{text: "f"}, {text: "("}, {text: ")"}}, {{text: "a"}, {text: "::"}, {text: "b"}, {text: "."}, {text: "f"}, {text: "("}, {text: "["}, {text: ")"}, {text: ","}}}
	for _, source := range []string{
		`class Java { void f() { this.service.run(inner(1, new int[]{2,3}), next()); } }`,
		`class TS { f() { this.service.run(inner([1,2]), {key: next()}); } }`,
		`func Go() { pkg.Run(inner([]int{1,2}), next()) }`,
		`fn rust() { module::Type::run(inner(vec![1,2]), next()); }`,
	} {
		body, _, _ := lex([]byte(source))
		inputs = append(inputs, body)
	}

	for trial := 0; trial < 1200; trial++ {
		body := make([]token, rng.Intn(96))
		for i := range body {
			body[i] = token{text: alphabet[rng.Intn(len(alphabet))], kind: "number", literal: fmt.Sprint(i), offset: i * 3, line: i + 1}
		}
		inputs = append(inputs, body)
	}
	for trial, body := range inputs {
		before := append([]token(nil), body...)
		index := indexCallDelimiters(body)
		for start := 0; start <= len(body); start++ {
			for end := start; end <= len(body); end++ {
				got, want := index.arguments(body, start, end), referenceSplitArguments(body[start:end])
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("trial %d range %d:%d: got %#v want %#v", trial, start, end, got, want)
				}
			}
		}
		got, want := callsIn(body), referenceCallsIn(body)
		for i := range got {
			got[i].signature = got[i].callSignature()
			got[i].argumentTokens = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("trial %d calls: got %#v want %#v", trial, got, want)
		}
		if len(body) > 0 && !reflect.DeepEqual(body, before) {
			t.Fatal("input mutated")
		}
	}
}

var extractedCallsSink []call
var extractedSignatureSink string

func BenchmarkCallExtractionIndexed(b *testing.B) {
	for _, shape := range []string{"nested", "adjacent", "qualified"} {
		for _, depth := range []int{16, 64, 256, 1024} {
			body := make([]token, 0, depth*4)
			for i := 0; i < depth; i++ {
				switch shape {
				case "nested":
					body = append(body, token{text: "f"}, token{text: "("})
				case "adjacent":
					body = append(body, token{text: "f"}, token{text: "("}, token{text: "x"}, token{text: ")"})
				case "qualified":
					body = append(body, token{text: "part"}, token{text: "::"})
				}
			}
			if shape == "nested" {
				for i := 0; i < depth; i++ {
					body = append(body, token{text: ")"})
				}
			}
			if shape == "qualified" {
				body = append(body, token{text: "f"}, token{text: "("}, token{text: ")"})
			}
			for _, mode := range []string{"indexed", "reference", "eager-signatures"} {
				b.Run(fmt.Sprintf("%s/%d/%s", shape, depth, mode), func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						if mode == "reference" {
							extractedCallsSink = referenceCallsIn(body)
						} else {
							extractedCallsSink = callsIn(body)
							if mode == "eager-signatures" {
								for _, c := range extractedCallsSink {
									extractedSignatureSink = c.callSignature()
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestCallExtractionNoCandidates(t *testing.T) {
	for _, size := range []int{0, 1, 16, 64, 256, 1024, 4096} {
		body := noCallTokens(size)
		got, want := callsIn(body), referenceCallsIn(body)
		if !reflect.DeepEqual(got, want) || got == nil || cap(got) != cap(want) {
			t.Fatalf("size %d: empty result semantics changed", size)
		}
	}
}

func noCallTokens(size int) []token {
	pattern := []string{"if", "(", "value", ")", "{", "return", "value", "+", "1", ";", "}"}
	body := make([]token, size)
	for i := range body {
		body[i] = token{text: pattern[i%len(pattern)], offset: i * 2, line: i + 1}
	}
	return body
}

func BenchmarkCallExtractionNoCandidates(b *testing.B) {
	for _, size := range []int{16, 64, 256, 1024, 4096} {
		body := noCallTokens(size)
		for _, mode := range []string{"indexed", "reference"} {
			b.Run(fmt.Sprintf("%d/%s", size, mode), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if mode == "reference" {
						extractedCallsSink = referenceCallsIn(body)
					} else {
						extractedCallsSink = callsIn(body)
					}
				}
			})
		}
	}
}

func BenchmarkCallExtractionTiny(b *testing.B) {
	for _, source := range []string{"f()", "this.service.run(value)", "if (valid) { a(b(), c(value)); }"} {
		body, _, _ := lex([]byte(source))
		for _, mode := range []string{"current", "reference"} {
			b.Run(source+"/"+mode, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if mode == "reference" {
						extractedCallsSink = referenceCallsIn(body)
					} else {
						extractedCallsSink = callsIn(body)
					}
				}
			})
		}
	}
}
