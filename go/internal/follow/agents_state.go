package follow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopmochi/internal/fix"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type AgentRowID struct {
	JobID fix.JobID
	Path  fix.RepoPath
}

func (id AgentRowID) IsZero() bool { return id.JobID == "" }
func (id AgentRowID) IsJob() bool  { return id.JobID != "" && id.Path == "" }

func (id AgentRowID) String() string {
	if id.Path == "" {
		return string(id.JobID)
	}
	return fmt.Sprintf("%s:%s", id.JobID, id.Path)
}

type agentLogicalRow struct {
	ID   AgentRowID
	Job  fix.JobPresentation
	File *fix.FilePresentation
}

func (model *Model) setAgentPresentations(jobs []fix.JobPresentation) {
	previousTopRelative := 0
	if rows := model.agentRows(); len(rows) > 0 {
		if index := model.agentRowIndex(rows, model.agents.Selected); index >= 0 {
			previousTopRelative = model.agentRowSpans(rows)[index].start - model.agents.Offset
		}
	}
	model.agents.Jobs = cloneAgentPresentations(jobs)
	if model.agents.Expanded == nil {
		model.agents.Expanded = map[fix.JobID]bool{}
	}
	rows := model.agentRows()
	if index := model.agentRowIndex(rows, model.agents.Selected); index >= 0 {
		model.agents.Offset = model.agentRowSpans(rows)[index].start - previousTopRelative
		model.ensureAgentVisible()
		return
	}
	model.reconcileAgentSelection()
}

func cloneAgentPresentations(jobs []fix.JobPresentation) []fix.JobPresentation {
	result := make([]fix.JobPresentation, len(jobs))
	for index, job := range jobs {
		result[index] = job
		result[index].Targets = append([]fix.FilePresentation(nil), job.Targets...)
		result[index].AllowedActions = append([]fix.JobAction(nil), job.AllowedActions...)
		if job.Issue != nil {
			issue := *job.Issue
			result[index].Issue = &issue
		}
	}
	return result
}

func (model Model) visibleAgentJobs() []fix.JobPresentation {
	jobs := make([]fix.JobPresentation, 0, len(model.agents.Jobs))
	query := strings.ToLower(strings.TrimSpace(model.agents.FindQuery))
	for _, job := range model.agents.Jobs {
		if job.Phase == fix.PhaseDiscarded {
			continue
		}
		if !model.agents.ShowAll && (job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseCanceled) {
			continue
		}
		if query != "" && !agentJobMatches(job, query) {
			continue
		}
		jobs = append(jobs, job)
	}
	sort.SliceStable(jobs, func(left, right int) bool { return model.agentJobLess(jobs[left], jobs[right]) })
	return jobs
}

var agentSortKeys = []string{"attention", "state", "agent", "goal", "target", "time", "activity"}

func (model Model) agentJobLess(left, right fix.JobPresentation) bool {
	key := model.agents.SortKey
	if key == "" {
		key = "attention"
	}
	less := false
	equal := false
	switch key {
	case "state":
		less, equal = agentPhaseText(left) < agentPhaseText(right), agentPhaseText(left) == agentPhaseText(right)
	case "agent":
		less, equal = strings.ToLower(left.ProfileLabel) < strings.ToLower(right.ProfileLabel), strings.EqualFold(left.ProfileLabel, right.ProfileLabel)
	case "goal":
		less, equal = strings.ToLower(left.Goal) < strings.ToLower(right.Goal), strings.EqualFold(left.Goal, right.Goal)
	case "target":
		leftTarget, rightTarget := firstAgentTarget(left), firstAgentTarget(right)
		less, equal = leftTarget < rightTarget, leftTarget == rightTarget
	case "time":
		less, equal = left.UpdatedAt.Before(right.UpdatedAt), left.UpdatedAt.Equal(right.UpdatedAt)
	case "activity":
		less, equal = strings.ToLower(left.CurrentAction) < strings.ToLower(right.CurrentAction), strings.EqualFold(left.CurrentAction, right.CurrentAction)
	default:
		leftPriority, rightPriority := agentJobPriority(left), agentJobPriority(right)
		less, equal = leftPriority < rightPriority, leftPriority == rightPriority
	}
	if equal {
		less = left.ID < right.ID
	}
	if model.agents.SortReverse && !equal {
		return !less
	}
	return less
}

func firstAgentTarget(job fix.JobPresentation) string {
	if len(job.Targets) == 0 {
		return ""
	}
	return strings.ToLower(job.Targets[0].Path.String())
}

func (model *Model) cycleAgentSort(direction int) {
	current := 0
	for index, key := range agentSortKeys {
		if key == model.agents.SortKey {
			current = index
		}
	}
	model.agents.SortKey = agentSortKeys[fixCycleIndex(current, direction, len(agentSortKeys))]
	model.reconcileAgentSelection()
}

func (model *Model) beginAgentFind() {
	model.agents.FindEditing = true
	model.agentFindInput.SetValue(model.agents.FindQuery)
	model.agentFindInput.CursorEnd()
	model.agentFindInput.Focus()
}

func (model *Model) handleAgentFindKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		model.agents.FindQuery = strings.TrimSpace(model.agentFindInput.Value())
		model.agents.FindEditing = false
		model.agentFindInput.Blur()
		model.reconcileAgentSelection()
		return model, nil
	case "esc":
		model.agents.FindEditing = false
		model.agentFindInput.Blur()
		return model, nil
	case "ctrl+c":
		model.agents.FindQuery = ""
		model.agentFindInput.SetValue("")
		model.agents.FindEditing = false
		model.agentFindInput.Blur()
		model.reconcileAgentSelection()
		return model, nil
	default:
		var command tea.Cmd
		model.agentFindInput, command = model.agentFindInput.Update(key)
		model.agents.FindQuery = model.agentFindInput.Value()
		model.reconcileAgentSelection()
		return model, command
	}
}

func agentJobMatches(job fix.JobPresentation, query string) bool {
	values := []string{string(job.ID), job.ProfileLabel, job.ModelLabel, job.EffortLabel, job.Goal, job.CurrentAction, job.BranchName, agentPhaseText(job)}
	if job.Issue != nil {
		values = append(values, job.Issue.Summary)
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	for _, file := range job.Targets {
		if strings.Contains(strings.ToLower(file.Path.String()), query) {
			return true
		}
	}
	return false
}

func agentJobPriority(job fix.JobPresentation) int {
	switch job.Attention {
	case fix.AttentionBlocking:
		return 0
	case fix.AttentionError:
		return 1
	}
	switch job.Phase {
	case fix.PhaseFailed:
		return 2
	case fix.PhaseVerifying, fix.PhaseWaitingVerifier:
		return 4
	case fix.PhaseRunning, fix.PhaseCanceling, fix.PhasePublishing, fix.PhaseReconciling:
		return 5
	case fix.PhaseQueued, fix.PhasePreflight, fix.PhasePreparing:
		return 6
	case fix.PhaseCompleted:
		return 7
	default:
		return 8
	}
}

func agentJobFinished(phase fix.Phase) bool {
	switch phase {
	case fix.PhaseFailed, fix.PhaseCompleted, fix.PhaseCanceled, fix.PhaseDiscarded:
		return true
	default:
		return false
	}
}

func (model Model) agentRows() []agentLogicalRow {
	jobs := model.visibleAgentJobs()
	query := strings.ToLower(strings.TrimSpace(model.agents.FindQuery))
	rows := make([]agentLogicalRow, 0, len(jobs)*2)
	for _, job := range jobs {
		rows = append(rows, agentLogicalRow{ID: AgentRowID{JobID: job.ID}, Job: job})
		expanded := model.agents.Expanded[job.ID]
		matchingFiles := query != "" && agentTargetMatches(job, query)
		if !expanded && !matchingFiles {
			continue
		}
		for index := range job.Targets {
			file := job.Targets[index]
			if query != "" && matchingFiles && !strings.Contains(strings.ToLower(file.Path.String()), query) {
				continue
			}
			rows = append(rows, agentLogicalRow{
				ID: AgentRowID{JobID: job.ID, Path: file.Path}, Job: job, File: &file,
			})
		}
	}
	return rows
}

func agentTargetMatches(job fix.JobPresentation, query string) bool {
	for _, file := range job.Targets {
		if strings.Contains(strings.ToLower(file.Path.String()), query) {
			return true
		}
	}
	return false
}

func (model *Model) reconcileAgentSelection() {
	rows := model.agentRows()
	if len(rows) == 0 {
		model.agents.Selected = AgentRowID{}
		model.agents.Offset = 0
		return
	}
	for _, row := range rows {
		if row.ID == model.agents.Selected {
			model.ensureAgentVisible()
			return
		}
	}
	model.agents.Selected = rows[0].ID
	model.ensureAgentVisible()
}

func (model *Model) moveAgentSelection(delta int) {
	rows := model.agentRows()
	if len(rows) == 0 {
		return
	}
	index := model.agentRowIndex(rows, model.agents.Selected)
	if index < 0 {
		index = 0
	} else {
		index = min(len(rows)-1, max(0, index+delta))
	}
	model.agents.Selected = rows[index].ID
	model.ensureAgentVisible()
}

func (model Model) agentRowIndex(rows []agentLogicalRow, wanted AgentRowID) int {
	for index, row := range rows {
		if row.ID == wanted {
			return index
		}
	}
	return -1
}

func (model *Model) jumpAgentSelection(last bool) {
	rows := model.agentRows()
	if len(rows) == 0 {
		return
	}
	index := 0
	if last {
		index = len(rows) - 1
	}
	model.agents.Selected = rows[index].ID
	model.ensureAgentVisible()
}

func (model *Model) pageAgentSelection(direction int) {
	rows := model.agentRows()
	if len(rows) == 0 {
		return
	}
	spans := model.agentRowSpans(rows)
	index := model.agentRowIndex(rows, model.agents.Selected)
	if index < 0 {
		index = 0
	}
	targetLine := spans[index].start + direction*max(1, model.bodyHeight()-1)
	target := index
	if direction > 0 {
		for target+1 < len(spans) && spans[target].start < targetLine {
			target++
		}
	} else {
		for target > 0 && spans[target].start > targetLine {
			target--
		}
	}
	model.agents.Selected = rows[target].ID
	model.ensureAgentVisible()
}

func (model *Model) toggleSelectedAgentJob() {
	if !model.agents.Selected.IsJob() {
		return
	}
	if model.agents.Expanded == nil {
		model.agents.Expanded = map[fix.JobID]bool{}
	}
	jobID := model.agents.Selected.JobID
	model.agents.Expanded[jobID] = !model.agents.Expanded[jobID]
	model.ensureAgentVisible()
}

func (model *Model) toggleAgentFilter() {
	model.agents.ShowAll = !model.agents.ShowAll
	model.reconcileAgentSelection()
}

func (model *Model) moveAgentHorizontal(delta int) {
	model.agents.HorizontalOffset = min(model.maximumAgentHorizontalOffset(), max(0, model.agents.HorizontalOffset+delta))
}

func (model *Model) clampAgentHorizontalOffset() {
	model.agents.HorizontalOffset = min(model.maximumAgentHorizontalOffset(), max(0, model.agents.HorizontalOffset))
}

func (model Model) maximumAgentHorizontalOffset() int {
	maximum := 0
	tier := responsiveTier(model.width, model.height)
	for _, row := range model.agentRows() {
		if row.File == nil {
			continue
		}
		path := agentFileDisplayPath(*row.File)
		viewport := model.agentFilePathViewport(*row.File, tier, model.width)
		maximum = max(maximum, lipgloss.Width(path)-viewport)
	}
	return maximum
}

type agentRowSpan struct{ start, height int }

func (model Model) agentRowSpans(rows []agentLogicalRow) []agentRowSpan {
	spans := make([]agentRowSpan, len(rows))
	line := 0
	for index, row := range rows {
		height := agentRowHeight(row, responsiveTier(model.width, model.height))
		spans[index] = agentRowSpan{start: line, height: height}
		line += height
	}
	return spans
}

func agentRowHeight(_ agentLogicalRow, tier ResponsiveTier) int {
	if tier == ResponsiveCompact {
		return 2
	}
	return 1
}

func (model *Model) ensureAgentVisible() {
	rows := model.agentRows()
	if len(rows) == 0 {
		model.agents.Offset = 0
		return
	}
	spans := model.agentRowSpans(rows)
	index := model.agentRowIndex(rows, model.agents.Selected)
	if index < 0 {
		index = 0
		model.agents.Selected = rows[0].ID
	}
	page := model.bodyHeight()
	selected := spans[index]
	if selected.start < model.agents.Offset {
		model.agents.Offset = selected.start
	}
	if selected.start+selected.height > model.agents.Offset+page {
		model.agents.Offset = selected.start + selected.height - page
	}
	total := spans[len(spans)-1].start + spans[len(spans)-1].height
	model.agents.Offset = min(max(0, total-page), max(0, model.agents.Offset))
}
