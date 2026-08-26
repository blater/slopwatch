package agent

import (
	"strings"
	"testing"
)

func TestEffectiveBodyAddsPreviousMeasurementsAfterTheMasterPrompt(t *testing.T) {
	document := InstructionDocument{Envelope: "WORKSPACE RULES", Objective: "Improve score", NextAttemptNotes: "score 72 exceeds target 50"}
	body := document.EffectiveBody()
	if !strings.HasPrefix(body, "WORKSPACE RULES\n\nImprove score") {
		t.Fatalf("prompt prefix changed: %q", body)
	}
	if !strings.Contains(body, "Measurements from the previous attempt:\nscore 72 exceeds target 50") {
		t.Fatalf("previous measurements absent: %q", body)
	}
}
