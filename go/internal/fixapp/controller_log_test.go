package fixapp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

func TestJobLogContainsStartAndResult(t *testing.T) {
	fixture := createJobLogFixture(t)
	contents := readJobLogFile(t, fixture.path)
	indexed := assertJobLogIndex(t, fixture.record, contents)
	logContents := readJobLogFile(t, indexed.LogFile)
	assertJobLogContents(t, fixture.record, logContents)
}

type jobLogFixture struct {
	path   string
	record *jobRecord
}

func createJobLogFixture(t *testing.T) jobLogFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fix-jobs.jsonl")
	manager := bareTestManager(Dependencies{}, Options{Clock: time.Now, JobIndexPath: path})
	startedAt := time.Now()
	record := &jobRecord{
		input: FixInput{Targets: []fix.RepoPath{"a.go"}, Profile: agent.Profile{ID: "codex", Runtime: "codex-cli"}, Model: "gpt", Effort: "high", TargetScore: 10,
			Instructions: agent.InstructionDocument{Envelope: "trusted instructions", Objective: "refactor every selected file"}},
		presentation: fix.JobPresentation{ID: "job-one", Phase: fix.PhaseQueued, CreatedAt: startedAt, Targets: []fix.FilePresentation{
			{Path: "a.go", Changed: true, ChangeStatus: "M"},
			{Path: "target/classes/A.class", Changed: true, ChangeStatus: "A", Classification: "supporting"},
		}},
		agentReferences: []string{"thread-one"},
	}
	manager.controller.logging.logJobStart(record)
	manager.controller.logging.logJobPrompt(record.presentation.ID, "attempt-one", record.input.Instructions.EffectiveBody())
	record.presentation.Phase = fix.PhaseCompleted
	record.presentation.CurrentAction = "Done"
	manager.controller.logging.logJobResult(record)
	return jobLogFixture{path: path, record: record}
}

func assertJobLogIndex(t *testing.T, record *jobRecord, contents []byte) jobIndexEntry {
	t.Helper()
	if strings.Count(string(contents), "\n") != 1 {
		t.Fatalf("job index should contain one record per job: %q", contents)
	}
	var indexed jobIndexEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(contents))), &indexed); err != nil {
		t.Fatal(err)
	}
	if indexed.End == nil || indexed.End.Status != fix.PhaseCompleted || indexed.LogFile == "" || len(indexed.End.FilesTouched) != 1 || len(indexed.End.AgentReferences) != 1 {
		t.Fatalf("job index = %+v", indexed)
	}
	if bytes.Contains(contents, []byte("trusted instructions")) {
		t.Fatalf("job index contains the prompt: %s", contents)
	}
	return indexed
}

func readJobLogFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func assertJobLogContents(t *testing.T, record *jobRecord, logContents []byte) {
	t.Helper()
	for _, wanted := range []string{"SLOPWATCH FIX JOB", "PROMPT attempt-one", record.input.Instructions.EffectiveBody(), "Status: completed", "thread-one", "M a.go"} {
		if !strings.Contains(string(logContents), wanted) {
			t.Fatalf("job text log omitted %q: %s", wanted, logContents)
		}
	}
	if strings.Contains(string(logContents), "target/classes/A.class") {
		t.Fatalf("job text log recorded a build artifact: %s", logContents)
	}
}

func TestRunningJobPersistsPromptActivityAndResultInItsTextLog(t *testing.T) {
	indexPath := filepath.Join(t.TempDir(), "fix-jobs.jsonl")
	manager, runtime := newTestManagerWithStore(t, 1, jobstore.NewMemory(), Options{JobIndexPath: indexPath})
	defer shutdownManager(t, manager)
	job := loadAndRun(t, manager, "one.go")
	waitStarted(t, runtime, job)
	runtime.complete(job)
	waitForPhase(t, manager, job, fix.PhaseCompleted)

	indexContents, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var indexed jobIndexEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(indexContents))), &indexed); err != nil {
		t.Fatal(err)
	}
	logContents, err := os.ReadFile(indexed.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"PROMPT ", "Required target checklist:", "Refactoring", "Final streamed activity", "RESULT", "Status: completed", "session-" + string(job)} {
		if !strings.Contains(string(logContents), wanted) {
			t.Fatalf("persisted job log omitted %q: %s", wanted, logContents)
		}
	}
	page, err := manager.Transcript(context.Background(), job, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	visible := ""
	for _, entry := range page.Entries {
		visible += entry.Text + "\n"
	}
	if !strings.Contains(visible, "Required target checklist:") || !strings.Contains(visible, "Final streamed activity") {
		t.Fatalf("UI transcript did not read the persisted job log: %q", visible)
	}
}
