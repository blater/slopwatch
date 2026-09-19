package sourceestimate

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestCallerArgumentRefsMatchBindingScan(t *testing.T) {
	for _, source := range []string{
		`d.range(s.low,s.high);`,
		`d.start().next(s.low,s.high);`,
		`this.d.start(x).next(s.low,s.high);`,
		`unbound.next(s.low); d.wrap(other.call(s.high)).next(s.low);`,
		`a.b.c(s.low); standalone(s.high);`,
	} {
		body, _, _ := lex([]byte(source))
		for _, bindings := range []map[string]string{{"d": "Driver"}, {"d": ""}, {"this": "", "this.d": "Driver"}, {"a.b": "Driver"}, {"unbound": ""}, {"standalone": ""}, {}} {
			got := gradedCallerArgumentRefs(body, bindings)
			want := referenceCallerArgumentRefs(body, bindings)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s bindings=%v got %v want %v", source, bindings, got, want)
			}
		}
	}
}

func referenceCallerArgumentRefs(body []token, bindings map[string]string) map[string]bool {
	refs := map[string]bool{}
	for _, c := range callsIn(body) {
		for receiver := range bindings {
			if !strings.HasPrefix(c.name, receiver+".") && referenceCallerCallRoot(body, c) != receiver {
				continue
			}
			for _, arg := range c.actuals {
				for dep := range gradedCallerDependencies(arg) {
					refs[dep] = true
				}
			}
		}
	}
	return refs
}

func TestCallerScanCalleeOnlyBindingAndShadows(t *testing.T) {
	for _, tc := range []struct {
		parameter, prefix string
		want              bool
	}{
		{"", "", true},
		{"Unknown s", "", false},
		{"State s", "", true},
		{"", "State s=null;", false},
		{"", "s=null;", false},
	} {
		units := []unit{
			gradedSurfaceUnit("java", "State.java", `package p;class State{int low;int high;}`),
			gradedSurfaceUnit("java", "Caller.java", `package p;class Caller{State s;Driver d;int run(`+tc.parameter+`){`+tc.prefix+`return d.check();}}`),
			gradedSurfaceUnit("java", "Driver.java", `package p;class Driver{int check(){if(s.low>=s.high)return -1;return effect();}}`),
		}
		for i := range units {
			units[i].inventory = &unitInventory{}
			for _, op := range units[i].ops {
				op.file = i
			}
		}
		byKey, types := gradedCallerPrepare(units)
		got := gradedCallerScanOperation(units[1], units[1].ops[0], units, byKey, gradedCallerResolver{types: types}, map[string]bool{}, map[string]map[string]map[string]bool{}, func(fieldRef, *operation) {})
		if (len(got) > 0) != tc.want {
			t.Fatalf("parameter=%q prefix=%q got %d consumers want=%v", tc.parameter, tc.prefix, len(got), tc.want)
		}
	}
}

func referenceCallerCallRoot(body []token, c call) string {
	start := c.position
	for start >= 2 && body[start-1].text == "." {
		if body[start-2].text != ")" {
			start -= 2
			continue
		}
		open := -1
		for j := start - 3; j >= 0; j-- {
			if body[j].text == "(" && matching(body, j, "(", ")") == start-2 {
				open = j
				break
			}
		}
		if open < 1 {
			return ""
		}
		start = open - 1
	}
	if start >= 0 && start < len(body) {
		return body[start].text
	}
	return ""
}
func referenceCallerPredicateRefs(body []token) map[int]int {
	refs := map[int]int{}
	for j, tok := range body {
		if tok.text != "if" || j+1 >= len(body) || body[j+1].text != "(" {
			continue
		}
		close := matching(body, j+1, "(", ")")
		if close <= j {
			continue
		}
		for k := j + 2; k < close; k++ {
			refs[k] = close + 1
		}
	}
	return refs
}
func TestCallerBodyIndexReference(t *testing.T) {
	rng := rand.New(rand.NewSource(717))
	alphabet := []string{"if", "(", ")", ".", "d", "next", "{", "}"}
	for sample := 0; sample < 1000; sample++ {
		body := make([]token, rng.Intn(90))
		for i := range body {
			body[i].text = alphabet[rng.Intn(len(alphabet))]
		}
		index := indexGradedCallerBody(body)
		for i := range body {
			c := call{position: i}
			if got, want := index.callRoot(body, c), referenceCallerCallRoot(body, c); got != want {
				t.Fatalf("root at %d body=%v got %q want %q", i, body, got, want)
			}
		}
		if got, want := gradedCallerPredicateRefsIndexed(body, index), referenceCallerPredicateRefs(body); !reflect.DeepEqual(got, want) {
			t.Fatalf("predicate body=%v got %v want %v", body, got, want)
		}
	}
}
