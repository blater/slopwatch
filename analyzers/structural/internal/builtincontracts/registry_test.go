package builtincontracts

import (
	"bytes"
	"os"
	"testing"
)

func TestApprovedRegistryAdmission(t *testing.T) {
	normative, err := os.ReadFile("../../../../docs/evidence/shallow-v4/builtin-registry.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(normative, manifest) {
		t.Fatal("embedded registry differs from approved manifest")
	}
	if Version() != "shallow-builtins-v1-r6" || len(approved.Entries) != 40 {
		t.Fatal("registry version or entries changed")
	}
	ids := map[string]bool{}
	for _, entry := range approved.Entries {
		if ids[entry.ID] {
			t.Fatal("duplicate ID", entry.ID)
		}
		ids[entry.ID] = true
		got, ok := Lookup(entry.Language, entry.Match.Symbol, entry.Match.Signature, entry.Match.Provenance, entry.Match.Guards)
		if !ok || got.ID != entry.ID || got.HiddenCredit != "derive_from_flow_only" {
			t.Fatal("contract missing", entry.ID)
		}
		if _, ok := Lookup(entry.Language, entry.Match.Symbol, entry.Match.Signature, "user_defined", entry.Match.Guards); ok {
			t.Fatal("untrusted match", entry.ID)
		}
		if _, ok := Lookup(entry.Language, entry.Match.Symbol, entry.Match.Signature+"?", entry.Match.Provenance, entry.Match.Guards); ok {
			t.Fatal("wrong overload", entry.ID)
		}
		for i := range entry.Match.Guards {
			guards := append([]string(nil), entry.Match.Guards[:i]...)
			guards = append(guards, entry.Match.Guards[i+1:]...)
			if _, ok := Lookup(entry.Language, entry.Match.Symbol, entry.Match.Signature, entry.Match.Provenance, guards); ok {
				t.Fatal("missing guard accepted", entry.ID)
			}
		}
		if len(got.Match.Parameters) > 0 {
			got.Match.Parameters[0] = "changed"
		}
		again, _ := Lookup(entry.Language, entry.Match.Symbol, entry.Match.Signature, entry.Match.Provenance, entry.Match.Guards)
		if len(again.Match.Parameters) > 0 && again.Match.Parameters[0] == "changed" {
			t.Fatal("registry mutable")
		}
	}
}
