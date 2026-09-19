package goadapter

import (
	"testing"

	"slopslap.dev/structural/internal/facts"
)

func TestGoPassiveCarrierRequiresOnlyDataTransfer(t *testing.T) {
	for _, test := range []struct {
		name, body string
		passive    bool
	}{
		{"copy", "r.value = value; r.status = status", true},
		{"validation", "if value < 0 { panic(\"invalid\") }; r.value = value; r.status = status", false},
		{"transformation", "r.value = value * 2; r.status = status", false},
		{"transition", "r.value += value; r.status = status", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			score := depthCallScore(t, `package p
type Payload struct { value int; status int }
func (r *Payload) Value() int { return r.value }
func (r *Payload) Status() int { return r.status }
func (r *Payload) Store(value, status int) { `+test.body+` }
func (r *Payload) Reset() { r.value = 0; r.status = 0 }
`)
			proven := false
			for _, evidence := range score.Evidence {
				proven = proven || evidence.Kind == "passive-result-carrier-v1" && evidence.Status == "proven"
			}
			if proven != test.passive {
				t.Fatalf("role: %+v", score)
			}
			if test.passive && (score.State != facts.KnowledgeMeasured || score.Shallow == nil || *score.Shallow != 0 || score.Estimated) {
				t.Fatalf("carrier penalty: %+v", score)
			}
		})
	}
}
