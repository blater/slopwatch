package fixapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/sourcepath"
)

type loggingOwner struct {
	indexPath string
	clock     func() time.Time
	mu        sync.Mutex
}

type jobIndexEntry struct {
	Job       fix.JobID         `json:"job"`
	LogFile   string            `json:"log_file"`
	StartedAt time.Time         `json:"started_at"`
	Targets   []fix.RepoPath    `json:"targets"`
	Profile   string            `json:"profile"`
	Runtime   agent.RuntimeKind `json:"runtime"`
	Model     agent.ModelID     `json:"model"`
	Effort    agent.EffortID    `json:"effort"`
	Score     float64           `json:"target_score"`
	Metrics   []fix.MetricGoal  `json:"metrics,omitempty"`
	Scope     string            `json:"may_edit"`
	Delivery  fix.DeliveryPlan  `json:"delivery"`
	Branch    string            `json:"branch,omitempty"`
	End       *jobIndexEnd      `json:"end,omitempty"`
}

type jobIndexEnd struct {
	At              time.Time             `json:"at"`
	Status          fix.Phase             `json:"status"`
	Result          string                `json:"result"`
	Error           string                `json:"error,omitempty"`
	FilesTouched    []jobIndexTouchedFile `json:"files_touched,omitempty"`
	AgentReferences []string              `json:"agent_references,omitempty"`
	Tokens          fix.UsagePresentation `json:"tokens,omitempty"`
	Git             fix.DeliveryState     `json:"git,omitempty"`
}

type jobIndexTouchedFile struct {
	Path   fix.RepoPath `json:"path"`
	Change string       `json:"change,omitempty"`
}

func (manager *loggingOwner) logJobStart(record *jobRecord) {
	if record == nil || manager.indexPath == "" {
		return
	}
	startedAt := record.presentation.CreatedAt
	if startedAt.IsZero() {
		startedAt = manager.clock()
	}
	manager.startJobTextLog(record, startedAt)
	manager.writeJobIndex(manager.indexEntry(record, startedAt))
}

func (manager *loggingOwner) logJobResult(record *jobRecord) {
	if record == nil || record.resultLogged || !isQuiescent(record.presentation.Phase) {
		return
	}
	record.resultLogged = true
	finishedAt := record.presentation.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = manager.clock()
	}
	entry := manager.indexEntry(record, record.presentation.CreatedAt)
	entry.End = &jobIndexEnd{At: finishedAt, Status: record.presentation.Phase, Result: record.presentation.CurrentAction,
		FilesTouched: touchedFiles(record.presentation.Targets), AgentReferences: append([]string(nil), record.agentReferences...),
		Tokens: record.presentation.Usage, Git: record.presentation.Delivery}
	if record.presentation.Issue != nil {
		entry.End.Error = nonempty(record.presentation.Issue.Detail, record.presentation.Issue.Summary)
	}
	manager.appendJobText(record.presentation.ID, formatJobEnd(entry.End))
	manager.writeJobIndex(entry)
}

func (manager *loggingOwner) indexEntry(record *jobRecord, startedAt time.Time) jobIndexEntry {
	if startedAt.IsZero() {
		startedAt = manager.clock()
	}
	return jobIndexEntry{Job: record.presentation.ID, LogFile: manager.jobTextLogPath(record.presentation.ID), StartedAt: startedAt,
		Targets: append([]fix.RepoPath(nil), record.input.Targets...), Profile: string(record.input.Profile.ID), Runtime: record.input.Profile.Runtime,
		Model: record.input.Model, Effort: record.input.Effort, Score: record.input.TargetScore, Metrics: append([]fix.MetricGoal(nil), record.input.Focus...),
		Scope: record.input.ChangeScope, Delivery: record.input.DeliveryPlan, Branch: record.input.BranchName}
}

func touchedFiles(files []fix.FilePresentation) []jobIndexTouchedFile {
	result := make([]jobIndexTouchedFile, 0)
	for _, file := range files {
		if file.Changed && sourcepath.IsSourceFile(file.Path.String()) {
			result = append(result, jobIndexTouchedFile{Path: file.Path, Change: file.ChangeStatus})
		}
	}
	return result
}

func (manager *loggingOwner) jobTextLogPath(job fix.JobID) string {
	if manager.indexPath == "" || job == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(manager.indexPath), "fix-jobs", string(job)+".log")
}

func (manager *loggingOwner) startJobTextLog(record *jobRecord, startedAt time.Time) {
	path := manager.jobTextLogPath(record.presentation.ID)
	if path == "" {
		return
	}
	var text strings.Builder
	fmt.Fprintf(&text, "SLOPWATCH FIX JOB\nJob: %s\nStarted: %s\nAgent: %s\nRuntime: %s\nModel: %s\nEffort: %s\nTarget score: %g\nMay edit: %s\nDelivery: workspace=%s git=%s publish=%s\n",
		record.presentation.ID, startedAt.Format(time.RFC3339Nano), record.input.Profile.ID, record.input.Profile.Runtime, record.input.Model,
		record.input.Effort, record.input.TargetScore, record.input.ChangeScope, record.input.DeliveryPlan.Workspace, record.input.DeliveryPlan.Git, record.input.DeliveryPlan.Publish)
	if record.input.BranchName != "" {
		fmt.Fprintf(&text, "Branch: %s\n", record.input.BranchName)
	}
	text.WriteString("Files:\n")
	for _, target := range record.input.Targets {
		fmt.Fprintf(&text, "  %s\n", target)
	}
	text.WriteByte('\n')
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = os.WriteFile(path, []byte(text.String()), 0o600)
}

func (manager *loggingOwner) logJobPrompt(job fix.JobID, attempt fix.AttemptID, prompt string) {
	manager.appendJobText(job, fmt.Sprintf("PROMPT %s\n%s\nEND PROMPT\n\n", attempt, cleanLogText(prompt)))
}

func (manager *loggingOwner) appendJobText(job fix.JobID, text string) {
	path := manager.jobTextLogPath(job)
	if path == "" || text == "" {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(text)
	_ = file.Close()
}

func formatJobEnd(end *jobIndexEnd) string {
	var text strings.Builder
	fmt.Fprintf(&text, "\nRESULT\nFinished: %s\nStatus: %s\nResult: %s\n", end.At.Format(time.RFC3339Nano), end.Status, cleanLogText(end.Result))
	if end.Error != "" {
		fmt.Fprintf(&text, "Error: %s\n", cleanLogText(end.Error))
	}
	if len(end.AgentReferences) > 0 {
		fmt.Fprintf(&text, "Agent references: %s\n", strings.Join(end.AgentReferences, ", "))
	}
	text.WriteString("Files touched:\n")
	for _, file := range end.FilesTouched {
		fmt.Fprintf(&text, "  %s %s\n", file.Change, file.Path)
	}
	return text.String()
}

func (manager *loggingOwner) writeJobIndex(entry jobIndexEntry) {
	path := manager.indexPath
	if path == "" {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	entries := []jobIndexEntry{}
	if contents, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
			var existing jobIndexEntry
			if json.Unmarshal([]byte(line), &existing) == nil && existing.Job != "" {
				entries = append(entries, existing)
			}
		}
	}
	replaced := false
	for index := range entries {
		if entries[index].Job != entry.Job {
			continue
		}
		if entry.End != nil {
			entries[index].End = entry.End
		} else {
			entries[index] = entry
		}
		replaced = true
		break
	}
	if !replaced {
		entries = append(entries, entry)
	}
	var output strings.Builder
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	for _, value := range entries {
		if encoder.Encode(value) != nil {
			return
		}
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	_ = os.WriteFile(path, []byte(output.String()), 0o600)
}
