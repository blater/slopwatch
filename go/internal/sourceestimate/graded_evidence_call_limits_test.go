package sourceestimate

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func referenceEvidenceLimits(evidence []evidenceItem, owners map[string]bool, protocol map[string]map[string]bool) (map[string]bool, bool, map[string]bool) {
	excluded, snapshots := map[string]bool{}, map[string]bool{}
	unknown := false
	for _, item := range evidence {
		if item.origin != nil && !owners[item.origin.owner] && len(protocol[itoa(item.origin.file)+"#"+item.origin.owner]) > 0 {
			for _, c := range referenceCallsIn(item.origin.body) {
				excluded["unresolved_call_range_0_2:"+c.signature] = true
			}
		} else if strings.HasPrefix(item.category, "unknown_") {
			unknown = true
		}
		if item.category == "storage_snapshot" && item.origin != nil {
			snapshots[item.origin.id] = snapshots[item.origin.id] || item.storageComputed
		}
	}
	return excluded, unknown, snapshots
}

func TestNeededCallLimitationsDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(431))
	alphabet := []string{"f", "g", "(", ")", "[", "]", "{", "}", ",", " ", "\t", "\u2003", "\xff", "\xe2", "\x80\x83", "/"}
	for trial := 0; trial < 600; trial++ {
		body := []token{{text: "f"}, {text: "("}}
		for i, size := 0, rng.Intn(80); i < size; i++ {
			body = append(body, token{text: alphabet[rng.Intn(len(alphabet))]})
		}
		body = append(body, token{text: ")"})
		op := &operation{id: "excluded", owner: "Other", file: 2, body: body}
		own := &operation{id: "owned", owner: "Root", body: body}
		evidence := []evidenceItem{{origin: op}, {origin: op, category: "unknown_call"}, {origin: op, category: "storage_snapshot", storageComputed: true}, {origin: own, category: "storage_snapshot"}, {category: "unknown_outcome"}}
		owners := map[string]bool{"Root": true}
		protocol := map[string]map[string]bool{"2#Other": {"protocol": true}}
		limitations := []string{"other", "unresolved_call_range_0_2:f/nonmatching", "unresolved_call_range_0_2:bad"}
		for _, c := range referenceCallsIn(body) {
			limitations = append(limitations, "unresolved_call_range_0_2:"+c.signature)
		}
		want, wu, ws := referenceEvidenceLimits(evidence, owners, protocol)
		got, gu, gs := gradedEvidenceLimits(evidence, owners, protocol, limitations)
		for _, reason := range limitations {
			if got[reason] != want[reason] {
				t.Fatalf("trial %d reason %q got %v want %v", trial, reason, got[reason], want[reason])
			}
		}
		if gu != wu || !reflect.DeepEqual(gs, ws) {
			t.Fatal("non-call evidence changed")
		}
		got, _, _ = gradedEvidenceLimits(evidence, owners, protocol, nil)
		if len(got) != 0 {
			t.Fatal("materialized unneeded reasons")
		}
	}
}

func BenchmarkNeededCallLimitations(b *testing.B) {
	for _, size := range []int{16, 64, 256, 1024} {
		body := make([]token, 0, 3*size)
		for i := 0; i < size; i++ {
			body = append(body, token{text: "f"}, token{text: "("})
		}
		for i := 0; i < size; i++ {
			body = append(body, token{text: ")"})
		}
		op := &operation{id: "origin", owner: "Other", body: body}
		evidence := make([]evidenceItem, 8)
		for i := range evidence {
			evidence[i] = evidenceItem{origin: op}
		}
		protocol := map[string]map[string]bool{"0#Other": {"p": true}}
		needed := []string{"unresolved_call_range_0_2:f/"}
		for _, mode := range []string{"needed", "reference"} {
			b.Run(fmt.Sprintf("%d/%s", size, mode), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if mode == "reference" {
						referenceEvidenceLimits(evidence, nil, protocol)
					} else {
						gradedEvidenceLimits(evidence, nil, protocol, needed)
					}
				}
			})
		}
	}
}
