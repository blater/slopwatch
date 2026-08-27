package agent

import (
	"strings"
	"testing"
)

func TestEffectiveBodySubstitutesPreviousMeasurementsInsideTheMasterPrompt(t *testing.T) {
	document := InstructionDocument{Envelope: "WORKSPACE RULES", Objective: "Improve score\nPrevious: {previous_attempt}", NextAttemptNotes: "score 72 exceeds target 50"}
	body := document.EffectiveBody()
	if !strings.HasPrefix(body, "WORKSPACE RULES\n\nImprove score") {
		t.Fatalf("prompt prefix changed: %q", body)
	}
	if !strings.Contains(body, "Previous: score 72 exceeds target 50") {
		t.Fatalf("previous measurements absent: %q", body)
	}
}

func TestEffectivePromptOnlySubstitutesManifestData(t *testing.T) {
	task := RemediationTask{
		Instructions: InstructionDocument{Objective: "Read {target_manifest_count} targets from {target_manifest}."},
		Manifest:     &TargetManifest{Path: "/tmp/targets.txt", Count: 200},
	}
	if got, want := task.EffectivePrompt(), "Read 200 targets from /tmp/targets.txt."; got != want {
		t.Fatalf("EffectivePrompt() = %q, want %q", got, want)
	}
}

func TestEffectivePromptDoesNotAppendInstructionsMissingFromTheTemplate(t *testing.T) {
	task := RemediationTask{
		Instructions: InstructionDocument{Objective: "Use the configured instructions exactly."},
		Manifest:     &TargetManifest{Path: "/tmp/targets.txt", Count: 200},
	}
	if got, want := task.EffectivePrompt(), "Use the configured instructions exactly."; got != want {
		t.Fatalf("EffectivePrompt() = %q, want %q", got, want)
	}
}
