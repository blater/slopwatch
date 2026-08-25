package agent

import (
	"strings"
	"testing"
)

func TestEffectiveBodyAppendsTrustedRetryEvidenceWithoutChangingEnvelope(t *testing.T) {
	for _, detached := range []string{"", "Keep my exact advanced edit"} {
		document := InstructionDocument{
			Envelope:      "LOCKED ENVELOPE",
			Objective:     "Improve score",
			Evidence:      "Baseline evidence",
			DetachedBody:  detached,
			RetryEvidence: "attempt 1/3; score 72 exceeds target 50",
		}
		body := document.EffectiveBody()
		if !strings.HasPrefix(body, "LOCKED ENVELOPE\n\nImprove score") {
			t.Fatalf("trusted envelope/objective prefix changed for detached=%q: %q", detached, body)
		}
		if !strings.Contains(body, "Trusted retry evidence from Slopwatch:\nattempt 1/3; score 72 exceeds target 50") {
			t.Fatalf("retry evidence absent for detached=%q: %q", detached, body)
		}
		if detached != "" && !strings.Contains(body, "Advanced instructions:\n"+detached) {
			t.Fatalf("advanced instructions changed: %q", body)
		}
	}
}
