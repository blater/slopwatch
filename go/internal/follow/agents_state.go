package follow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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

type AgentsState struct {
	Jobs             []fix.JobPresentation
	Selected         AgentRowID
	Offset           int
	HorizontalOffset int
	SortKey          string
	SortReverse      bool
	FindQuery        string
	FindEditing      bool
	ShowAll          bool
	Expanded         map[fix.JobID]bool
	FindInput        textinput.Model
}

type agentLogicalRow struct {
	ID   AgentRowID
	Job  fix.JobPresentation
	File *fix.FilePresentation
}

type agentLayout struct {
	Tier ResponsiveTier
	Page int
}

func makeAgentLayout(width, height, page int) agentLayout {
	return agentLayout{Tier: responsiveTier(width, height), Page: max(0, page)}
}

func (state *AgentsState) setPresentations(jobs []fix.JobPresentation, layout agentLayout) {
	previousTopRelative := 0
	rows := state.rows()
	if index := agentRowIndex(rows, state.Selected); index >= 0 {
		previousTopRelative = agentRowSpans(rows, layout.Tier)[index].start - state.Offset
	}
	state.Jobs = cloneAgentPresentations(jobs)
	if state.Expanded == nil {
		state.Expanded = map[fix.JobID]bool{}
	}
	rows = state.rows()
	if index := agentRowIndex(rows, state.Selected); index >= 0 {
		state.Offset = agentRowSpans(rows, layout.Tier)[index].start - previousTopRelative
		state.ensureVisible(layout)
		return
	}
	state.reconcileSelection(layout)
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

func (state AgentsState) visibleJobs() []fix.JobPresentation {
	jobs := make([]fix.JobPresentation, 0, len(state.Jobs))
	query := strings.ToLower(strings.TrimSpace(state.FindQuery))
	for _, job := range state.Jobs {
		if job.Phase == fix.PhaseDiscarded {
			continue
		}
		if !state.ShowAll && (job.Phase == fix.PhaseCompleted || job.Phase == fix.PhaseCanceled) {
			continue
		}
		if query != "" && !agentJobMatches(job, query) {
			continue
		}
		jobs = append(jobs, job)
	}
	sort.SliceStable(jobs, func(left, right int) bool { return state.jobLess(jobs[left], jobs[right]) })
	return jobs
}

var agentSortKeys = []string{"attention", "state", "agent", "goal", "target", "time", "activity"}

func (state AgentsState) jobLess(left, right fix.JobPresentation) bool {
	key := state.SortKey
	if key == "" {
		key = "attention"
	}
	less, equal := agentSortComparison(key, left, right)
	if equal {
		less = left.ID < right.ID
	}
	if state.SortReverse && !equal {
		return !less
	}
	return less
}

func agentSortComparison(key string, left, right fix.JobPresentation) (bool, bool) {
	switch key {
	case "state":
		return compareAgentText(agentPhaseText(left), agentPhaseText(right))
	case "agent":
		return compareAgentText(left.ProfileLabel, right.ProfileLabel)
	case "goal":
		return compareAgentText(left.Goal, right.Goal)
	case "target":
		return firstAgentTarget(left) < firstAgentTarget(right), firstAgentTarget(left) == firstAgentTarget(right)
	case "time":
		return left.UpdatedAt.Before(right.UpdatedAt), left.UpdatedAt.Equal(right.UpdatedAt)
	case "activity":
		return compareAgentText(left.CurrentAction, right.CurrentAction)
	default:
		return agentJobPriority(left) < agentJobPriority(right), agentJobPriority(left) == agentJobPriority(right)
	}
}

func compareAgentText(left, right string) (bool, bool) {
	return strings.ToLower(left) < strings.ToLower(right), strings.EqualFold(left, right)
}

func firstAgentTarget(job fix.JobPresentation) string {
	if len(job.Targets) == 0 {
		return ""
	}
	return strings.ToLower(job.Targets[0].Path.String())
}

func (state *AgentsState) cycleSort(direction int, layout agentLayout) {
	current := 0
	for index, key := range agentSortKeys {
		if key == state.SortKey {
			current = index
		}
	}
	state.SortKey = agentSortKeys[fixCycleIndex(current, direction, len(agentSortKeys))]
	state.reconcileSelection(layout)
}

func (state *AgentsState) beginFind() {
	state.FindEditing = true
	state.FindInput.SetValue(state.FindQuery)
	state.FindInput.CursorEnd()
	state.FindInput.Focus()
}

func (state *AgentsState) handleFindKey(key tea.KeyMsg, layout agentLayout) tea.Cmd {
	switch key.String() {
	case "enter":
		state.FindQuery = strings.TrimSpace(state.FindInput.Value())
		state.FindEditing = false
		state.FindInput.Blur()
		state.reconcileSelection(layout)
	case "esc":
		state.FindEditing = false
		state.FindInput.Blur()
	case "ctrl+c":
		state.FindQuery = ""
		state.FindInput.SetValue("")
		state.FindEditing = false
		state.FindInput.Blur()
		state.reconcileSelection(layout)
	default:
		var command tea.Cmd
		state.FindInput, command = state.FindInput.Update(key)
		state.FindQuery = state.FindInput.Value()
		state.reconcileSelection(layout)
		return command
	}
	return nil
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

func (state AgentsState) rows() []agentLogicalRow {
	jobs := state.visibleJobs()
	query := strings.ToLower(strings.TrimSpace(state.FindQuery))
	rows := make([]agentLogicalRow, 0, len(jobs)*2)
	for _, job := range jobs {
		rows = append(rows, agentLogicalRow{ID: AgentRowID{JobID: job.ID}, Job: job})
		expanded := state.Expanded[job.ID]
		matchingFiles := query != "" && agentTargetMatches(job, query)
		if !expanded && !matchingFiles {
			continue
		}
		for index := range job.Targets {
			file := job.Targets[index]
			if query != "" && matchingFiles && !strings.Contains(strings.ToLower(file.Path.String()), query) {
				continue
			}
			rows = append(rows, agentLogicalRow{ID: AgentRowID{JobID: job.ID, Path: file.Path}, Job: job, File: &file})
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

func agentRowIndex(rows []agentLogicalRow, wanted AgentRowID) int {
	for index, row := range rows {
		if row.ID == wanted {
			return index
		}
	}
	return -1
}

func (state *AgentsState) reconcileSelection(layout agentLayout) {
	rows := state.rows()
	if len(rows) == 0 {
		state.Selected = AgentRowID{}
		state.Offset = 0
		return
	}
	if agentRowIndex(rows, state.Selected) < 0 {
		state.Selected = rows[0].ID
	}
	state.ensureVisible(layout)
}

func (state *AgentsState) moveSelection(delta int, layout agentLayout) {
	rows := state.rows()
	if len(rows) == 0 {
		return
	}
	index := agentRowIndex(rows, state.Selected)
	if index < 0 {
		index = 0
	} else {
		index = min(len(rows)-1, max(0, index+delta))
	}
	state.Selected = rows[index].ID
	state.ensureVisible(layout)
}

func (state *AgentsState) jumpSelection(last bool, layout agentLayout) {
	rows := state.rows()
	if len(rows) == 0 {
		return
	}
	index := 0
	if last {
		index = len(rows) - 1
	}
	state.Selected = rows[index].ID
	state.ensureVisible(layout)
}

func (state *AgentsState) pageSelection(direction int, layout agentLayout) {
	rows := state.rows()
	if len(rows) == 0 {
		return
	}
	spans := agentRowSpans(rows, layout.Tier)
	index := agentRowIndex(rows, state.Selected)
	if index < 0 {
		index = 0
	}
	targetLine := spans[index].start + direction*max(1, layout.Page-1)
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
	state.Selected = rows[target].ID
	state.ensureVisible(layout)
}

func (state *AgentsState) toggleSelectedJob(layout agentLayout) {
	if !state.Selected.IsJob() {
		return
	}
	if state.Expanded == nil {
		state.Expanded = map[fix.JobID]bool{}
	}
	state.Expanded[state.Selected.JobID] = !state.Expanded[state.Selected.JobID]
	state.ensureVisible(layout)
}

func (state *AgentsState) toggleFilter(layout agentLayout) {
	state.ShowAll = !state.ShowAll
	state.reconcileSelection(layout)
}

func (state *AgentsState) moveHorizontal(delta, maximum int) {
	state.HorizontalOffset = min(maximum, max(0, state.HorizontalOffset+delta))
}

func (state *AgentsState) clampHorizontal(maximum int) {
	state.HorizontalOffset = min(maximum, max(0, state.HorizontalOffset))
}

type agentRowSpan struct{ start, height int }

func agentRowSpans(rows []agentLogicalRow, tier ResponsiveTier) []agentRowSpan {
	spans := make([]agentRowSpan, len(rows))
	line := 0
	for index, row := range rows {
		height := agentRowHeight(row, tier)
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

func (state *AgentsState) ensureVisible(layout agentLayout) {
	rows := state.rows()
	if len(rows) == 0 {
		state.Offset = 0
		return
	}
	spans := agentRowSpans(rows, layout.Tier)
	index := agentRowIndex(rows, state.Selected)
	if index < 0 {
		index = 0
		state.Selected = rows[0].ID
	}
	page := layout.Page
	selected := spans[index]
	if selected.start < state.Offset {
		state.Offset = selected.start
	}
	if selected.start+selected.height > state.Offset+page {
		state.Offset = selected.start + selected.height - page
	}
	total := spans[len(spans)-1].start + spans[len(spans)-1].height
	state.Offset = min(max(0, total-page), max(0, state.Offset))
}
