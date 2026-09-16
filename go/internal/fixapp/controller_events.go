package fixapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/sourcepath"
)

func (manager *controller) handlePrompt(job fix.JobID, attempt fix.AttemptID, prompt string) {
	state := manager.state
	record := state.jobs[job]
	if !activeAttempt(record, attempt) {
		return
	}
	manager.logging.logJobPrompt(job, attempt, prompt)
	record.logs = append(record.logs, LogEntry{At: manager.options.Clock(), Kind: "prompt", Summary: cleanLogText(prompt)})
	manager.changedRecord(record)
}

func (manager *controller) handleEvent(event agent.Event) {
	state := manager.state
	record := state.jobs[event.JobID]
	if !activeAttempt(record, event.AttemptID) {
		return
	}
	projectEvent(record, event)
	entry := eventLogEntry(event)
	record.logs = append(record.logs, entry)
	manager.logging.appendJobText(record.presentation.ID, formatJobActivity(entry))
	manager.changedRecord(record)
}

func activeAttempt(record *jobRecord, attempt fix.AttemptID) bool {
	return record != nil && record.attempt == attempt && (record.presentation.Phase == fix.PhasePreparing || record.presentation.Phase == fix.PhaseRunning)
}

func projectEvent(record *jobRecord, event agent.Event) {
	if event.Kind == agent.EventStarted || record.presentation.Phase == fix.PhasePreparing {
		record.presentation.Phase = fix.PhaseRunning
	}
	if event.Summary != "" {
		record.presentation.CurrentAction = sanitizeSummary(event.Summary)
	}
	if event.Kind == agent.EventWarning {
		record.presentation.WarningCount++
	}
	projectFileEvent(record, event)
	projectActorEvent(record, event)
	projectUsage(record, event)
}

func projectFileEvent(record *jobRecord, event agent.Event) {
	if event.Kind != agent.EventFileChanged || event.Path == "" || !sourcepath.IsSourceFile(event.Path.String()) {
		return
	}
	for index := range record.presentation.Targets {
		if record.presentation.Targets[index].Path == event.Path {
			record.presentation.Targets[index].Changed = true
			return
		}
	}
	record.presentation.Targets = append(record.presentation.Targets, fix.FilePresentation{Path: event.Path, Classification: "provisional", Changed: true})
}

func projectActorEvent(record *jobRecord, event agent.Event) {
	if event.ActorID == "" {
		return
	}
	actorLimit := record.input.Preferences.Concurrency.MaxActorsPerJob
	if record.actors == nil {
		record.actors = map[string]bool{}
	}
	if len(record.actors) < actorLimit {
		record.actors[event.ActorID] = true
	}
	record.presentation.ActorCount = len(record.actors)
	for index := range record.presentation.Actors {
		if record.presentation.Actors[index].ID != event.ActorID {
			continue
		}
		record.presentation.Actors[index].ParentID = sanitizeSummary(event.ParentActorID)
		if event.Summary != "" {
			record.presentation.Actors[index].CurrentAction = sanitizeSummary(event.Summary)
		}
		return
	}
	if len(record.presentation.Actors) < actorLimit {
		record.presentation.Actors = append(record.presentation.Actors, fix.ActorPresentation{ID: sanitizeSummary(event.ActorID), ParentID: sanitizeSummary(event.ParentActorID), CurrentAction: sanitizeSummary(event.Summary)})
	}
}

func projectUsage(record *jobRecord, event agent.Event) {
	if event.Usage == nil {
		return
	}
	record.presentation.UsageReported = true
	if event.Usage.Cumulative {
		record.presentation.Usage = fix.UsagePresentation{InputTokens: event.Usage.InputTokens, CachedTokens: event.Usage.CachedTokens, OutputTokens: event.Usage.OutputTokens, ReasoningTokens: event.Usage.ReasoningTokens}
		return
	}
	record.presentation.Usage.InputTokens += event.Usage.InputTokens
	record.presentation.Usage.CachedTokens += event.Usage.CachedTokens
	record.presentation.Usage.OutputTokens += event.Usage.OutputTokens
	record.presentation.Usage.ReasoningTokens += event.Usage.ReasoningTokens
}

func eventLogEntry(event agent.Event) LogEntry {
	entry := LogEntry{At: event.At, Kind: event.Kind, Summary: cleanLogText(event.Summary), ActorID: sanitizeSummary(event.ActorID), ParentActorID: sanitizeSummary(event.ParentActorID)}
	if event.Usage != nil {
		usage := *event.Usage
		entry.Usage = &usage
	}
	return entry
}

func cleanLogText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func formatJobActivity(entry LogEntry) string {
	actor := ""
	if entry.ActorID != "" {
		actor = " [" + entry.ActorID + "]"
	}
	text := strings.ReplaceAll(entry.Summary, "\n", "\n    ")
	return fmt.Sprintf("%s  %-16s%s %s\n", entry.At.Format(time.RFC3339Nano), entry.Kind, actor, text)
}
