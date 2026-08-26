package fixapp

import (
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
)

func TestScoreAboveTargetAutomaticallyQueuesAnotherAgentAttempt(t *testing.T) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	path, _ := fix.ParseRepoPath("bad.go")
	record := &jobRecord{
		draft: FixDraft{DeliveryMode: fix.DeliveryModeBranch, TargetScore: 50, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{
			Targets: []fix.TargetSnapshot{{Path: path, Score: 88, Complete: true}},
		}}},
		presentation: fix.JobPresentation{ID: job, Revision: 1, Phase: fix.PhaseVerifying, AttemptOrdinal: 1, Targets: []fix.FilePresentation{{Path: path}}},
		attempt:      attempt,
		candidate:    &fix.CandidateIdentity{Job: job},
		commands:     map[fix.CommandID]CommandReceipt{},
	}
	manager := &Manager{deps: Dependencies{Store: jobstore.NewMemory()}, options: Options{Clock: time.Now}, notify: make(chan struct{})}
	manager.current.Store(JobListSnapshot{})
	manager.logs.Store(map[fix.JobID]logSnapshot{})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, verifiersRunning: 1}

	manager.handleResult(state, workerResult{kind: workerVerifier, job: job, attempt: attempt,
		diff: candidateDiffForNextAttempt(),
		verify: fixanalysis.VerificationResult{Files: []fixanalysis.FileResult{{Path: path, Score: 72, Complete: true}},
			Complete: true, TargetMet: false, FingerprintBefore: "same", FingerprintAfter: "same"},
	})

	if record.presentation.Phase != fix.PhaseQueued || record.presentation.AttemptOrdinal != 2 {
		t.Fatalf("next attempt was not queued automatically: %+v", record.presentation)
	}
	if record.presentation.TargetStatus != fix.ScorePending || !strings.Contains(record.nextAttemptNotes, "SCORE 72.0/50.0") {
		t.Fatalf("next-attempt notes = %q; status=%q", record.nextAttemptNotes, record.presentation.TargetStatus)
	}
}

func TestVerificationBuildsBoundedNextAttemptEvidence(t *testing.T) {
	path, _ := fix.ParseRepoPath("bad.go")
	record := &jobRecord{
		draft: FixDraft{TargetScore: 50, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{
			Targets: []fix.TargetSnapshot{{Path: path, Score: 88, Complete: true}},
		}}},
		presentation: fix.JobPresentation{AttemptOrdinal: 1, Targets: []fix.FilePresentation{{Path: path}}},
	}
	manager := &Manager{}
	manager.applyVerification(record, workerResult{
		diff:   candidateDiffForNextAttempt(),
		verify: fixanalysis.VerificationResult{Files: []fixanalysis.FileResult{{Path: path, Score: 72, Complete: true}}, Complete: true, TargetMet: false, Diagnostic: strings.Repeat("x", 600)},
	})
	if len(record.nextAttemptNotes) > 504 || !strings.Contains(record.nextAttemptNotes, "SCORE 72.0/50.0") {
		t.Fatalf("next-attempt notes are not concise: len=%d value=%q", len(record.nextAttemptNotes), record.nextAttemptNotes)
	}
}

func TestNextAttemptEvidenceIncludesFocusAndRegressionMeasurements(t *testing.T) {
	path, _ := fix.ParseRepoPath("bad.go")
	record := &jobRecord{
		draft: FixDraft{TargetScore: 50, Focus: []fix.MetricGoal{{Metric: "cog", Maximum: 10}}, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{
			Goal:    fix.ScoringGoal{AllowedRegression: map[fix.MetricID]float64{"cpl": 2}},
			Targets: []fix.TargetSnapshot{{Path: path, Metrics: map[fix.MetricID]fix.MetricValue{"cpl": {ID: "cpl", Label: "CPL", Value: 5, Complete: true}}}},
		}}},
		presentation: fix.JobPresentation{AttemptOrdinal: 2, Targets: []fix.FilePresentation{{Path: path}}},
	}
	manager := &Manager{}
	manager.applyVerification(record, workerResult{
		diff: candidateDiffForNextAttempt(),
		verify: fixanalysis.VerificationResult{Files: []fixanalysis.FileResult{{
			Path: path, Score: 40, Complete: true, TargetMet: false, Diagnostic: "COG exceeds focus target; CPL regressed",
			Metrics: map[fix.MetricID]fix.MetricValue{
				"cog": {ID: "cog", Label: "COG", Value: 12, Complete: true},
				"cpl": {ID: "cpl", Label: "CPL", Value: 9, Complete: true},
			},
		}}, Complete: true, TargetMet: false, FingerprintBefore: "same", FingerprintAfter: "same"},
	})
	for _, wanted := range []string{"SCORE 40.0/50.0", "COG 12.0/10.0", "CPL 9.0/7.0", "COG exceeds focus target", "CPL regressed"} {
		if !strings.Contains(record.nextAttemptNotes, wanted) {
			t.Fatalf("next-attempt notes omitted %q: %q", wanted, record.nextAttemptNotes)
		}
	}
}

func TestScopeViolationFailsInsteadOfRetrying(t *testing.T) {
	job, _ := fix.NewJobID()
	attempt, _ := fix.NewAttemptID()
	path, _ := fix.ParseRepoPath("bad.go")
	record := &jobRecord{
		draft:        FixDraft{TargetScore: 50},
		presentation: fix.JobPresentation{ID: job, Revision: 1, Phase: fix.PhaseVerifying, AttemptOrdinal: 1, Targets: []fix.FilePresentation{{Path: path}}},
		attempt:      attempt, candidate: &fix.CandidateIdentity{Job: job}, commands: map[fix.CommandID]CommandReceipt{},
	}
	manager := &Manager{deps: Dependencies{Store: jobstore.NewMemory()}, options: Options{Clock: time.Now}, notify: make(chan struct{})}
	manager.current.Store(JobListSnapshot{})
	manager.logs.Store(map[fix.JobID]logSnapshot{})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}, verifiersRunning: 1}
	manager.handleResult(state, workerResult{kind: workerVerifier, job: job, attempt: attempt,
		diff:   candidate.DiffSnapshot{Scope: fix.ScopeViolated, Fingerprint: "scope-diff"},
		verify: fixanalysis.VerificationResult{Files: []fixanalysis.FileResult{{Path: path, Score: 40, Complete: true, TargetMet: true}}, Complete: true, TargetMet: true, FingerprintBefore: "same", FingerprintAfter: "same"},
	})
	if record.presentation.Phase != fix.PhaseFailed || record.presentation.Issue == nil || record.presentation.Issue.Code != "scope_violated" {
		t.Fatalf("scope violation was not a terminal failure: %+v", record.presentation)
	}
}

func candidateDiffForNextAttempt() candidate.DiffSnapshot {
	return candidate.DiffSnapshot{Scope: fix.ScopeClean, Fingerprint: "next-diff"}
}
