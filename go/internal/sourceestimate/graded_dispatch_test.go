package sourceestimate

import (
	"strings"
	"testing"
)

func TestGradedGoCapabilityDispatch(t *testing.T) {
	base := `package p;type Reader interface{Read(x int)int};type Wrapper interface{Layer()any};type Example struct{target any};func(c *Example)Read(x int)int{BODY};func unavailable()int{return -1}`
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"direct", `switch t:=c.target.(type){case Reader:return t.Read(x);default:return unavailable()}`, true},
		{"wrapped", `rw:=c.target;for{switch t:=rw.(type){case Reader:return t.Read(x);case Wrapper:rw=t.Layer();default:return unavailable()}}`, true},
		{"argument conversion", `switch t:=c.target.(type){case Reader:return t.Read(x+1);default:return unavailable()}`, false},
		{"result conversion", `switch t:=c.target.(type){case Reader:return t.Read(x)+1;default:return unavailable()}`, false},
		{"new representation", `switch t:=c.target.(type){case Reader:return format(t.Read(x));default:return unavailable()}`, false},
		{"independent validation", `if x<0{return unavailable()};switch t:=c.target.(type){case Reader:return t.Read(x);default:return unavailable()}`, false},
		{"branch control", `switch t:=c.target.(type){case Reader:if x<0{return unavailable()};return t.Read(x);default:return unavailable()}`, false},
		{"value switch", `switch x{case 1:return c.read(x);default:return unavailable()}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := strings.Replace(base, "BODY", tc.body, 1)
			u := dispatchTestUnits(source)
			found := false
			for _, op := range u[0].ops {
				if op.owner != "Example" || op.name != "Read" {
					continue
				}
				found = true
				_, got := gradedCapabilityDispatch(op, op.body)
				if got != tc.want {
					t.Fatalf("dispatch=%v want %v: %s", got, tc.want, tc.body)
				}
			}
			if !found {
				t.Fatal("missing method")
			}
			g := structuralResult("go", "example.go", source).Grade
			if tc.want && g.Responsibilities["representation-transformation"] != 0 {
				t.Fatalf("forwarding earned conversion: %+v", g)
			}
			if !tc.want && g.Responsibilities["representation-transformation"] == 0 {
				t.Fatalf("actual conversion/control lost existing evidence: %+v", g)
			}
		})
	}
}

func TestGradedGoCapabilityValidationIdentity(t *testing.T) {
	source := `package p;type Example struct{target any;other any};func(c *Example)Read(x int)int{switch t:=c.target.(type){case Reader:return t.Read(x);default:return unavailable()}};func(d *Example)Write(x int)int{switch value:=d.target.(type){case Reader:return value.Write(x);default:return unavailable()}};func(c *Example)Other(x int)int{switch t:=c.other.(type){case Reader:return t.Read(x);default:return unavailable()}};func(c *Example)Different(x int)int{switch t:=c.target.(type){case Writer:return t.Write(x);default:return unavailable()}}`
	u := dispatchTestUnits(source)
	keys := map[string]string{}
	for _, op := range u[0].ops {
		if op.owner == "Example" {
			var ok bool
			keys[op.name], ok = gradedCapabilityDispatch(op, op.body)
			if !ok {
				t.Fatal("unrecognized", op.name)
			}
		}
	}
	if keys["Read"] == "" || keys["Read"] != keys["Write"] {
		t.Fatal("same receiver and capability must share guard", keys)
	}
	if keys["Read"] == keys["Other"] || keys["Read"] == keys["Different"] {
		t.Fatal("distinct receiver/capability collapsed", keys)
	}
}

func TestGradedGoCapabilityValidationDeduplication(t *testing.T) {
	base := `package p;type Example struct{target any;other any};func(c *Example)Read(x int)int{switch t:=c.target.(type){case Reader:return t.Read(x);default:return unavailable()}};func(c *Example)Ping()error{switch t:=c.target.(type){case Reader:t.Ping();return nil;default:return unavailableError()}}`
	a := structuralResult("go", "example.go", base).Grade
	if a.Responsibilities["validation"] != 1 || a.Responsibilities["representation-transformation"] != 0 {
		t.Fatalf("same capability did not retain exactly one validation duty: %+v", a)
	}
	for _, source := range []string{
		strings.Replace(base, "switch t:=c.target.(type){case Reader:t.Ping()", "switch t:=c.other.(type){case Reader:t.Ping()", 1),
		strings.Replace(base, "case Reader:t.Ping()", "case Writer:t.Ping()", 1),
	} {
		g := structuralResult("go", "example.go", source).Grade
		if g.Responsibilities["validation"] != 2 {
			t.Fatalf("distinct capability validation collapsed: %+v", g)
		}
	}
}

func dispatchTestUnits(source string) []unit {
	f := File{Path: "example.go", Language: "go", Source: []byte(source)}
	tokens, limited, valid := lex(f.Source)
	pkg := packageName(f.Language, tokens)
	u := unit{file: f, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: valid}
	u.ops = findOperations(f, 0, tokens, pkg)
	return []unit{u}
}
