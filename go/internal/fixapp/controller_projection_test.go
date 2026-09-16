package fixapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/jobstore"
)

func TestDiffInventoryProjectsAllRepositoryChangesAsSupporting(t *testing.T) {
	record := &jobRecord{input: FixInput{AllowedPaths: []fix.RepoPath{"target.go", "helper.go"}, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{Targets: []fix.TargetSnapshot{{Path: "target.go", Score: 100, Complete: true}}}}}}
	record.applyDiffInventory(candidate.DiffSnapshot{Fingerprint: "fingerprint", Scope: fix.ScopeViolated, Files: []candidate.DiffFile{
		{Path: "target.go", Status: "modified"}, {Path: "helper.go", Status: "added"}, {Path: "renamed.go", Previous: "outside.go", Status: "renamed"},
		{Path: "target/generated-sources/Generated.java", Status: "added"}, {Path: "target/classes/App.class", Status: "added"},
		{Path: "target/surefire-reports/AppTest.txt", Status: "added"}, {Path: "README.md", Status: "modified"},
	}})
	if len(record.presentation.Targets) != 3 || record.presentation.DiffFingerprint != "fingerprint" {
		t.Fatalf("projection=%+v", record.presentation)
	}
	byPath := map[fix.RepoPath]fix.FilePresentation{}
	for _, file := range record.presentation.Targets {
		byPath[file.Path] = file
	}
	if byPath["target.go"].Classification != "target" || byPath["helper.go"].Classification != "supporting" || byPath["renamed.go"].Classification != "supporting" || byPath["renamed.go"].ScopeViolation || byPath["renamed.go"].PreviousPath != "outside.go" {
		t.Fatalf("files=%+v", byPath)
	}
}

func TestAgentFileEventsDoNotAddArtifactsToTheSupportingFileList(t *testing.T) {
	job, attempt := fix.JobID("job-source-events"), fix.AttemptID("attempt-source-events")
	record := &jobRecord{attempt: attempt, input: FixInput{Preferences: appconfig.Resolved{Concurrency: appconfig.Concurrency{MaxActorsPerJob: 1}}},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseRunning}}
	manager := bareTestManager(Dependencies{Store: jobstore.NewMemory()}, Options{Clock: time.Now})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	manager.state = state
	for _, path := range []fix.RepoPath{"target/classes/App.class", "target/generated-sources/Generated.java", "target/surefire-reports/AppTest.txt", "helper.go"} {
		manager.handleEvent(agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventFileChanged, Path: path, Summary: "changed " + path.String()})
	}
	if len(record.presentation.Targets) != 1 || record.presentation.Targets[0].Path != "helper.go" {
		t.Fatalf("agent supporting files = %+v", record.presentation.Targets)
	}
}

func TestUnchangedDiffRefreshPreservesVerifiedBeforeAfterProjection(t *testing.T) {
	score := 42.0
	record := &jobRecord{input: FixInput{AllowedPaths: []fix.RepoPath{"target.go"}, Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{Targets: []fix.TargetSnapshot{{Path: "target.go", Score: 88, Complete: true}}}}},
		presentation: fix.JobPresentation{Targets: []fix.FilePresentation{{Path: "target.go", BaselineScore: 88, VerifiedScore: &score, VerifiedMetrics: []fix.MetricValue{{ID: "cog", Value: 4, Complete: true}}, Verification: "verified"}}}}
	record.applyDiffInventory(candidate.DiffSnapshot{Fingerprint: "same", Scope: fix.ScopeClean, Files: []candidate.DiffFile{{Path: "target.go", Status: "modified"}}})
	target := record.presentation.Targets[0]
	if target.VerifiedScore == nil || *target.VerifiedScore != 42 || len(target.VerifiedMetrics) != 1 || target.Verification != "verified" {
		t.Fatalf("verified projection lost: %+v", target)
	}
}

func TestRepositoryScopeDiffProjectionDoesNotInventViolations(t *testing.T) {
	record := &jobRecord{input: FixInput{ChangeScope: "repository"}, presentation: fix.JobPresentation{Targets: []fix.FilePresentation{{Path: "target.go", Classification: "target"}}}}
	record.applyDiffInventory(candidate.DiffSnapshot{Fingerprint: "repository", Scope: fix.ScopeClean, Files: []candidate.DiffFile{
		{Path: "target.go", Status: "modified"}, {Path: "new.go", Status: "added"}, {Path: "renamed.go", Previous: "old.go", Status: "renamed"},
	}})
	for _, file := range record.presentation.Targets {
		if file.ScopeViolation || file.Classification == "violation" {
			t.Fatalf("repository-scope file %+v was projected as a violation", file)
		}
	}
}

func TestAgentEventsProjectActorTreeActivityAndUsage(t *testing.T) {
	job, attempt := fix.JobID("job-events"), fix.AttemptID("attempt-events")
	record := &jobRecord{attempt: attempt, input: FixInput{Preferences: appconfig.Resolved{Concurrency: appconfig.Concurrency{MaxActorsPerJob: 2}}}, presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseRunning}, actors: map[string]bool{}}
	manager := bareTestManager(Dependencies{}, Options{Clock: time.Now})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
	manager.state = state
	manager.handleEvent(agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventActivity, ActorID: "primary", Summary: "editing", Usage: &agent.Usage{InputTokens: 10, OutputTokens: 2}})
	manager.handleEvent(agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventUsage, ActorID: "reviewer", ParentActorID: "primary", Summary: "reviewing", Usage: &agent.Usage{InputTokens: 20, CachedTokens: 5, OutputTokens: 4, Cumulative: true}})
	if len(record.presentation.Actors) != 2 || record.presentation.Actors[1].ParentID != "primary" || record.presentation.Usage.InputTokens != 20 || record.presentation.Usage.CachedTokens != 5 || len(record.logs) != 2 || record.logs[1].ActorID != "reviewer" {
		t.Fatalf("projection actors=%+v usage=%+v logs=%+v", record.presentation.Actors, record.presentation.Usage, record.logs)
	}
}

func TestStoredJobStateOmitsTranscriptText(t *testing.T) {
	job := fix.JobID("job-state")
	record := &jobRecord{
		presentation: fix.JobPresentation{ID: job},
		input: FixInput{
			Instructions: agent.InstructionDocument{Envelope: "full agent prompt that must remain outside job state"},
			Preferences:  appconfig.Resolved{Fix: appconfig.FixDefaults{PromptTemplate: "configured prompt that must remain outside job state"}},
		},
		commands: map[fix.CommandID]CommandReceipt{},
		logs:     []LogEntry{{Summary: "text that must remain outside job state"}},
	}
	encoded, err := json.Marshal(storedJobStateFor(record))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("must remain outside job state")) || bytes.Contains(encoded, []byte(`"transcript"`)) {
		t.Fatalf("stored job state contains transcript data: %s", encoded)
	}
}

func TestAgentActorProjectionUsesPinnedPerJobLimit(t *testing.T) {
	for _, test := range []struct {
		name, job string
		limit     int
		events    int
		want      int
	}{
		{name: "configured above former compiled ceiling", job: "job-many-actors", limit: 33, events: 33, want: 33},
		{name: "configured cap", job: "job-capped-actors", limit: 2, events: 3, want: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			job, attempt := fix.JobID(test.job), fix.AttemptID("attempt-actors")
			record := &jobRecord{attempt: attempt,
				input:        FixInput{Preferences: appconfig.Resolved{Concurrency: appconfig.Concurrency{MaxActorsPerJob: test.limit}}},
				presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseRunning}, actors: map[string]bool{}}
			manager := bareTestManager(Dependencies{}, Options{Clock: time.Now})
			state := &controllerState{jobs: map[fix.JobID]*jobRecord{job: record}, order: []fix.JobID{job}}
			manager.state = state
			for index := 0; index < test.events; index++ {
				manager.handleEvent(agent.Event{JobID: job, AttemptID: attempt, At: time.Now(), Kind: agent.EventActivity,
					ActorID: fmt.Sprintf("actor-%02d", index), Summary: "working"})
			}
			if len(record.actors) != test.want || record.presentation.ActorCount != test.want || len(record.presentation.Actors) != test.want {
				t.Fatalf("actor projection map=%d count=%d rows=%d, want %d pinned by input", len(record.actors), record.presentation.ActorCount, len(record.presentation.Actors), test.want)
			}
		})
	}
}
