package follow

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	tea "github.com/charmbracelet/bubbletea"
)

func (model Model) openFixSurfaceUpdates() (time.Time, time.Time) {
	monitorUpdate, logUpdate := time.Time{}, time.Time{}
	if model.hasOverlay(OverlayJobMonitor) {
		if job, ok := model.agentJobByID(model.jobMonitor.jobID); ok {
			monitorUpdate = job.UpdatedAt
		}
	}
	if model.hasOverlay(OverlayJobLog) {
		if job, ok := model.agentJobByID(model.jobReader.jobID); ok {
			logUpdate = job.UpdatedAt
		}
	}
	return monitorUpdate, logUpdate
}

func (model *Model) refreshOpenFixSurfaces(previousMonitorUpdate, previousLogUpdate time.Time) (tea.Cmd, tea.Cmd) {
	monitorCommand, logCommand := tea.Cmd(nil), tea.Cmd(nil)
	if model.hasOverlay(OverlayJobMonitor) {
		if job, ok := model.agentJobByID(model.jobMonitor.jobID); ok {
			model.jobMonitor.job = job
			if !job.UpdatedAt.Equal(previousMonitorUpdate) {
				monitorCommand = model.beginJobMonitorRefresh()
			}
		}
	}
	if model.hasOverlay(OverlayJobLog) {
		if job, ok := model.agentJobByID(model.jobReader.jobID); ok && !job.UpdatedAt.Equal(previousLogUpdate) {
			logCommand = model.beginJobLogRefresh()
		}
	}
	return monitorCommand, logCommand
}

func (model Model) nextFixUpdate(monitorCommand, logCommand tea.Cmd) tea.Cmd {
	if model.fixService == nil || model.fixSubscription == nil {
		return tea.Batch(monitorCommand, logCommand)
	}
	return tea.Batch(waitFixJobsCommand(model.fixService, model.fixSubscription), monitorCommand, logCommand)
}

func (model *Model) retryFixSubscription(message fixRetrySubscriptionMsg) tea.Cmd {
	if message.generation != model.fixRetryGeneration || model.fixService == nil {
		return nil
	}
	if model.fixSubscription != nil {
		_ = model.fixSubscription.Close()
	}
	model.fixSubscription = model.fixService.Subscribe()
	snapshot := model.fixService.Jobs(fixapp.JobFilter{IncludeFinished: true})
	model.agents.setPresentations(snapshot.Jobs, makeAgentLayout(model.width, model.height, model.bodyHeight()))
	model.fixUpdatesStale = false
	model.fixNotice = "Fix updates restored"
	return waitFixJobsCommand(model.fixService, model.fixSubscription)
}

func (model *Model) openJobMonitor(jobID fix.JobID, focus fix.RepoPath) tea.Cmd {
	if jobID == "" || model.fixService == nil {
		model.fixNotice = "Select a fix job to inspect"
		return nil
	}
	model.fixGeneration++
	model.jobMonitor = jobMonitorState{generation: model.fixGeneration, jobID: jobID, focusPath: focus, loading: true, refreshing: true}
	if !model.hasOverlay(OverlayJobMonitor) {
		model.overlays.Push(OverlayJobMonitor, OverlayCaller{MainView: MainViewAgents, Selected: AgentRowID{JobID: jobID, Path: focus}.String()})
	}
	service, generation := model.fixService, model.fixGeneration
	return model.loadJobMonitorCommandWithService(service, jobID, generation)
}

func (model Model) loadJobMonitorCommand(jobID fix.JobID, generation uint64) tea.Cmd {
	return model.loadJobMonitorCommandWithService(model.fixService, jobID, generation)
}

// beginJobMonitorRefresh coalesces subscription bursts into at most one queued
// reload. Each actual reload receives a fresh generation so a late response can
// never replace a newer monitor snapshot.

func (model *Model) beginJobMonitorRefresh() tea.Cmd {
	if model.fixService == nil || model.jobMonitor.jobID == "" {
		return nil
	}
	if model.jobMonitor.refreshing {
		model.jobMonitor.pending = true
		return nil
	}
	model.fixGeneration++
	model.jobMonitor.generation = model.fixGeneration
	model.jobMonitor.refreshing = true
	return model.loadJobMonitorCommand(model.jobMonitor.jobID, model.jobMonitor.generation)
}

func (model Model) loadJobMonitorCommandWithService(service FixService, jobID fix.JobID, generation uint64) tea.Cmd {
	return func() tea.Msg {
		job, found := service.Job(jobID)
		if !found {
			return jobMonitorMsg{generation: generation, found: false}
		}
		return jobMonitorMsg{generation: generation, job: job, found: true}
	}
}

func (model *Model) handleJobMonitor(message jobMonitorMsg) tea.Cmd {
	if message.generation != model.jobMonitor.generation || !model.hasOverlay(OverlayJobMonitor) {
		return nil
	}
	if model.jobMonitor.apply(message, model.width, model.height, fullScreenSurface(model.width, model.height)) {
		return model.beginJobMonitorRefresh()
	}
	return nil
}

func (model *Model) handleJobMonitorKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	maximum := jobMonitorMaxOffset(model.jobMonitor, model.width, model.height, fullScreenSurface(model.width, model.height))
	result := model.jobMonitor.handleKey(key, model.height, maximum, model.agents.visibleJobs())
	switch result.action {
	case jobMonitorKeyClose:
		model.overlays.Pop()
	case jobMonitorKeyNext:
		return model, model.openJobMonitor(result.jobID, "")
	case jobMonitorKeyLog:
		return model, model.openJobLog(result.jobID)
	case jobMonitorKeyDiff:
		return model, model.openJobDiff(result.jobID, result.path)
	case jobMonitorKeyCancel:
		return model.activateJobAction(result.jobID, fix.ActionCancel)
	}
	return model, nil
}

func (model *Model) openJobLog(jobID fix.JobID) tea.Cmd {
	return model.openJobReader(OverlayJobLog, jobID, "")
}

func (model *Model) openJobDiff(jobID fix.JobID, path fix.RepoPath) tea.Cmd {
	return model.openJobReader(OverlayJobDiff, jobID, path)
}

func (model *Model) openCandidateSource(jobID fix.JobID, path fix.RepoPath) tea.Cmd {
	return model.openJobReader(OverlayCandidateSource, jobID, path)
}

func (model *Model) openJobReader(kind OverlayKind, jobID fix.JobID, path fix.RepoPath) tea.Cmd {
	if jobID == "" || model.fixService == nil {
		model.fixNotice = "Job details are unavailable"
		return nil
	}
	model.fixGeneration++
	model.jobReader = jobReaderState{
		generation: model.fixGeneration,
		kind:       kind,
		jobID:      jobID,
		path:       path,
		loading:    true,
		refreshing: true,
		follow:     kind == OverlayJobLog,
	}
	model.overlays.Push(kind, OverlayCaller{MainView: MainViewAgents, Overlay: OverlayJobMonitor, Selected: AgentRowID{JobID: jobID, Path: path}.String()})
	return model.loadJobReaderCommand(kind, jobID, path, model.fixGeneration, 0, false)
}

func (model *Model) loadJobReaderCommand(kind OverlayKind, jobID fix.JobID, path fix.RepoPath, generation uint64, cursor fixapp.LogCursor, increment bool) tea.Cmd {
	service := model.fixService
	return func() tea.Msg {
		result := loadJobReader(context.Background(), service, kind, jobID, path, cursor, increment)
		result.generation, result.kind, result.increment = generation, kind, increment
		return result
	}
}

func jobLogDisplayLine(line string) string {
	for _, prefix := range []string{"Started: ", "Finished: "} {
		if strings.HasPrefix(line, prefix) {
			return prefix + wholeSecondTimestamp(strings.TrimPrefix(line, prefix))
		}
	}
	if end := strings.IndexByte(line, ' '); end > 0 {
		return wholeSecondTimestamp(line[:end]) + line[end:]
	}
	return line
}

func wholeSecondTimestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.Format(time.RFC3339)
}

func (model *Model) beginJobLogRefresh() tea.Cmd {
	if model.jobReader.kind != OverlayJobLog || !model.hasOverlay(OverlayJobLog) || model.fixService == nil {
		return nil
	}
	if model.jobReader.refreshing {
		model.jobReader.pending = true
		return nil
	}
	model.fixGeneration++
	model.jobReader.generation = model.fixGeneration
	model.jobReader.refreshing = true
	return model.loadJobReaderCommand(OverlayJobLog, model.jobReader.jobID, "", model.fixGeneration, model.jobReader.logCursor, true)
}

func loadTranscript(ctx context.Context, service FixService, jobID fix.JobID, cursor fixapp.LogCursor) (fixapp.LogPage, error) {
	result := fixapp.LogPage{}
	for {
		page, err := service.Transcript(ctx, jobID, cursor, 500)
		if err != nil {
			return result, err
		}
		result.Entries = append(result.Entries, page.Entries...)
		result.Next = page.Next
		if page.Complete {
			result.Complete = true
			return result, nil
		}
		if page.Next <= cursor {
			return result, errors.New("activity pagination did not advance")
		}
		cursor = page.Next
	}
}

func (model *Model) handleJobReader(message jobReaderMsg) tea.Cmd {
	if message.generation != model.jobReader.generation || message.kind != model.jobReader.kind || !model.hasOverlay(message.kind) {
		return nil
	}
	if model.jobReader.apply(message, model.width, model.height, fullScreenSurface(model.width, model.height)) {
		return model.beginJobLogRefresh()
	}
	return nil
}

func (model *Model) handleJobReaderKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := model.jobReader.handleKey(key, model.width, model.height, fullScreenSurface(model.width, model.height))
	switch action {
	case jobReaderKeyClose:
		model.overlays.Pop()
	case jobReaderKeyRefresh:
		return model, model.beginJobLogRefresh()
	case jobReaderKeyReopen:
		model.overlays.Pop()
		return model, model.openJobReader(model.jobReader.kind, model.jobReader.jobID, model.jobReader.path)
	}
	return model, nil
}
