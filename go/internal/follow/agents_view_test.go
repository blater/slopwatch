package follow

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/style"
)

func TestAgentRowsUseAttentionOrderAndActiveAllFilter(t *testing.T) {
	now := time.Now()
	jobs := []fix.JobPresentation{
		{ID: "completed", Phase: fix.PhaseCompleted, CreatedAt: now.Add(-7 * time.Minute)},
		{ID: "running", Phase: fix.PhaseRunning, CreatedAt: now.Add(-6 * time.Minute)},
		{ID: "failed-old", Phase: fix.PhaseFailed, CreatedAt: now.Add(-5 * time.Minute)},
		{ID: "blocked", Phase: fix.PhaseRunning, Attention: fix.AttentionBlocking, CreatedAt: now.Add(-4 * time.Minute)},
		{ID: "failed", Phase: fix.PhaseFailed, CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "verify", Phase: fix.PhaseVerifying, CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "completed-new", Phase: fix.PhaseCompleted, CreatedAt: now.Add(-time.Minute)},
		{ID: "discarded", Phase: fix.PhaseDiscarded},
	}
	model := Model{width: 80, height: 24, agents: AgentsState{Expanded: map[fix.JobID]bool{}}}
	model.setAgentPresentations(jobs)
	assertAgentJobOrder(t, model.visibleAgentJobs(), "blocked", "failed", "failed-old", "verify", "running")

	model.toggleAgentFilter()
	assertAgentJobOrder(t, model.visibleAgentJobs(), "blocked", "failed", "failed-old", "verify", "running", "completed", "completed-new")
}

func TestTerminalJobsRemainVisibleAndExplainFailure(t *testing.T) {
	failed := agentTestJob("failed", fix.PhaseFailed, "broken.go")
	failed.CurrentAction = "Failed"
	failed.Issue = &fix.JobIssue{Code: "provider", Summary: "Codex connection closed unexpectedly"}
	done := agentTestJob("done", fix.PhaseCompleted, "fixed.go")
	model := agentTestModel(100, 16, failed, done)
	model.agents.ShowAll = true

	view := ansi.Strip(model.View())
	if !strings.Contains(view, "Codex connection closed unexpectedly") || !strings.Contains(view, "DONE") {
		t.Fatalf("terminal jobs are not useful in history: %q", view)
	}
}

func TestLogsOpenFromExpandedFileOfFinishedJob(t *testing.T) {
	job := agentTestJob("done", fix.PhaseCompleted, "fixed.go")
	service := &fakeFixService{jobs: fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{job}}, log: fixapp.LogPage{
		Entries: []fixapp.LogEntry{{At: time.Now(), Kind: agent.EventActivity, Summary: "Committed the fix"}}, Complete: true,
	}}
	model := agentTestModel(80, 16, job)
	model.fixService = service
	model.agents.ShowAll = true
	model.agents.Expanded[job.ID] = true
	model.agents.Selected = AgentRowID{JobID: job.ID, Path: "fixed.go"}

	_, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if command == nil || !model.hasOverlay(OverlayJobLog) {
		t.Fatal("l did not open logs from an expanded file row")
	}
	model.handleJobReader(command().(jobReaderMsg))
	if view := ansi.Strip(model.View()); !strings.Contains(view, "Committed the fix") || !strings.Contains(view, "DONE") {
		t.Fatalf("finished job log omitted activity or result: %q", view)
	}
}

func TestAgentCountIncludesOnlyRunningActors(t *testing.T) {
	jobs := []fix.JobPresentation{
		{Phase: fix.PhasePreparing, ActorCount: 4},
		{Phase: fix.PhaseRunning, ActorCount: 3},
		{Phase: fix.PhaseRunning},
		{Phase: fix.PhaseVerifying, ActorCount: 2},
	}
	if got := fixAggregateText(jobs); got != "AGENTS 4" {
		t.Fatalf("aggregate = %q, want running actors only", got)
	}
}

func TestAgentTimeFreezesAtTerminalJobDuration(t *testing.T) {
	created := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	now := created.Add(20 * time.Minute)
	for _, test := range []struct {
		name string
		job  fix.JobPresentation
		want string
	}{
		{name: "running", job: fix.JobPresentation{Phase: fix.PhaseRunning, CreatedAt: created}, want: "20:00"},
		{name: "completed", job: fix.JobPresentation{Phase: fix.PhaseCompleted, CreatedAt: created, FinishedAt: created.Add(4*time.Minute + 12*time.Second)}, want: "4:12"},
		{name: "failed", job: fix.JobPresentation{Phase: fix.PhaseFailed, CreatedAt: created, UpdatedAt: created.Add(3*time.Minute + 5*time.Second)}, want: "3:05"},
		{name: "canceled", job: fix.JobPresentation{Phase: fix.PhaseCanceled, CreatedAt: created, FinishedAt: created.Add(2*time.Minute + 30*time.Second)}, want: "2:30"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := agentJobTime(test.job, now); got != test.want {
				t.Fatalf("TIME = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAgentSelectionExpansionAndVisualPositionSurviveUpdates(t *testing.T) {
	model := Model{width: 40, height: 8, agents: AgentsState{Expanded: map[fix.JobID]bool{"one": true}}}
	jobs := []fix.JobPresentation{
		agentTestJob("one", fix.PhaseRunning, "a.go", "b.go", "c.go"),
		agentTestJob("two", fix.PhaseQueued, "d.go"),
	}
	model.setAgentPresentations(jobs)
	model.agents.Selected = AgentRowID{JobID: "one", Path: "c.go"}
	model.ensureAgentVisible()
	rows := model.agentRows()
	index := model.agentRowIndex(rows, model.agents.Selected)
	before := model.agentRowSpans(rows)[index].start - model.agents.Offset

	updated := []fix.JobPresentation{
		agentTestJob("urgent", fix.PhaseFailed, "urgent.go"),
		jobs[0], jobs[1],
	}
	model.setAgentPresentations(updated)
	rows = model.agentRows()
	index = model.agentRowIndex(rows, model.agents.Selected)
	after := model.agentRowSpans(rows)[index].start - model.agents.Offset
	if model.agents.Selected != (AgentRowID{JobID: "one", Path: "c.go"}) || after != before {
		t.Fatalf("live update moved selection: selected=%+v relative=%d, want %d", model.agents.Selected, after, before)
	}

	model.toggleSelectedAgentJob()
	if !model.agents.Expanded["one"] {
		t.Fatal("Enter on a file row changed its parent expansion")
	}
	model.agents.Selected = AgentRowID{JobID: "one"}
	model.toggleSelectedAgentJob()
	if model.agents.Expanded["one"] {
		t.Fatal("Enter on a job row did not collapse it")
	}
}

func TestAgentsResponsiveRenderingAndExpandedMetrics(t *testing.T) {
	verified := 91.0
	job := agentTestJob("job-1", fix.PhaseRunning, "internal/service.go", "internal/service_test.go")
	job.ProfileLabel = "Codex"
	job.ModelLabel = "gpt-5.6"
	job.EffortLabel = "high"
	job.Goal = "SCORE ≤100 · COG,CPL"
	job.CurrentAction = "Refactoring SQL selection predicates"
	job.AttemptOrdinal = 2
	job.ActorCount = 3
	job.Targets[0].BaselineScore = 142
	job.Targets[0].VerifiedScore = &verified
	job.Targets[0].Verification = "final verified"
	job.Targets[0].Metrics = []fix.MetricValue{{ID: "cog", Label: "COG", Value: 11, Complete: true}}
	job.Targets[1].Classification = "supporting"
	job.Targets[1].Changed = true

	for _, size := range []struct{ width, height int }{{36, 8}, {60, 16}, {80, 24}, {96, 24}, {120, 30}} {
		model := agentTestModel(size.width, size.height, job)
		model.agents.Expanded[job.ID] = true
		model.agents.Selected = AgentRowID{JobID: job.ID, Path: job.Targets[0].Path}
		model.ensureAgentVisible()
		view := model.View()
		assertScreenSize(t, view, size.width, size.height)
		plain := ansi.Strip(view)
		for _, wanted := range []string{"RUNNING", "Codex", "internal/service.go"} {
			if !strings.Contains(plain, wanted) {
				t.Fatalf("%dx%d Agents view omitted %q: %q", size.width, size.height, wanted, plain)
			}
		}
		if strings.Contains(plain, "ATTEMPT") || strings.Contains(plain, "attempt 2") {
			t.Fatalf("%dx%d Agents view exposed retry attempt outside Activity: %q", size.width, size.height, plain)
		}
		if size.width == 36 {
			if !strings.Contains(plain, "SCORE 142→91✓") {
				t.Fatalf("compact metrics missing authoritative result: %q", plain)
			}
			assertCompactSelectedBlock(t, view, "internal/service.go", "SCORE 142→91✓", size.width)
		}
		if size.width == 120 && (!strings.Contains(plain, "gpt-5.6") || !strings.Contains(plain, "Refactoring SQL selection predicates")) {
			t.Fatalf("wide view omitted model/activity: %q", plain)
		}
	}
}

func TestAgentColumnsAlignAndActivityUsesResponsiveSpace(t *testing.T) {
	job := agentTestJob("job-1", fix.PhaseRunning, "service.go")
	job.ProfileLabel = "Codex"
	job.AttemptOrdinal = 2
	job.CurrentAction = "Refactoring SQL selection predicates"
	for _, width := range []int{60, 80, 96, 112, 120} {
		model := agentTestModel(width, 20, job)
		lines := strings.Split(ansi.Strip(model.View()), "\n")
		header, row := lines[1], lines[2]
		for _, title := range []string{"STATE", "AGENT", "ACTIVITY"} {
			position := strings.Index(header, title)
			if position < 0 || strings.TrimSpace(row[position:min(len(row), position+len(title))]) == "" {
				t.Fatalf("%d-column %s is not aligned with row content:\n%s\n%s", width, title, header, row)
			}
		}
		if strings.Contains(header, "ATTEMPT") || strings.Contains(row, "attempt 2") {
			t.Fatalf("%d-column Agents row exposed a dedicated attempt field:\n%s\n%s", width, header, row)
		}
		activityStart := strings.Index(header, "ACTIVITY")
		timeStart := strings.Index(header, "TIME")
		if timeStart < 0 || strings.TrimSpace(row[timeStart-2:]) == "" {
			t.Fatalf("%d-column TIME is not aligned with elapsed content:\n%s\n%s", width, header, row)
		}
		activity := strings.TrimSpace(row[activityStart:timeStart])
		if len(activity) <= 14 {
			t.Fatalf("%d-column activity remained unusably narrow: %q", width, activity)
		}
	}
}

func TestExpandedAgentFileUsesFilesMetricsAndRightAlignedFilename(t *testing.T) {
	job := agentTestJob("job-1", fix.PhaseRunning, "internal/service.go")
	job.Targets[0].BaselineScore = 142
	job.Targets[0].Metrics = []fix.MetricValue{
		{ID: "nesting", Value: 7, Complete: true},
		{ID: "npath", Value: 99, Complete: false},
		{ID: "coupling", Value: 8, Complete: true},
		{ID: "cog", Value: 11, Complete: true},
	}
	model := agentTestModel(120, 20, job)
	model.weightEnabled["coupling_between_objects"] = false
	model.agents.Expanded[job.ID] = true
	lines := strings.Split(ansi.Strip(model.View()), "\n")
	var fileRow string
	for _, line := range lines {
		if strings.Contains(line, "internal/service.go") {
			fileRow = strings.TrimRight(line, " ")
			break
		}
	}
	if fileRow == "" || !strings.HasSuffix(fileRow, "internal/service.go") {
		t.Fatalf("expanded filename is not on the right: %q", fileRow)
	}
	if !strings.Contains(fileRow, "SCORE 142→… · COG 11") {
		t.Fatalf("expanded row does not use Files metric naming/order: %q", fileRow)
	}
	for _, excluded := range []string{"NPATH", "NEST", "CPL"} {
		if strings.Contains(fileRow, excluded) {
			t.Fatalf("expanded row included unavailable or disabled %s: %q", excluded, fileRow)
		}
	}
}

func TestExpandedAgentFileHonorsFilesCompactMode(t *testing.T) {
	job := agentTestJob("job-1", fix.PhaseRunning, "service.go")
	job.Targets[0].BaselineScore = 142
	job.Targets[0].Metrics = []fix.MetricValue{{ID: "cog", Value: 11, Complete: true}}
	model := agentTestModel(80, 20, job)
	model.options.Compact = true
	model.agents.Expanded[job.ID] = true
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "SCORE 142→…") || !strings.Contains(view, "service.go") || strings.Contains(view, "COG 11") {
		t.Fatalf("expanded Agent row did not match compact Files metrics: %q", view)
	}
}

func TestJobInspectRendersOnlyEssentialAgentIdentity(t *testing.T) {
	model := Model{jobMonitor: jobMonitorState{job: fix.JobPresentation{
		ID: "job-1", Phase: fix.PhaseRunning, ProfileLabel: "Codex recommended account", ModelLabel: "gpt-5.6-sol", EffortLabel: "high", AttemptOrdinal: 2,
	}}}
	view := ansi.Strip(strings.Join(model.jobMonitorContent(72, 8), "\n"))
	if !strings.Contains(view, "Agent: codex · gpt-5.6-sol · high") || strings.Contains(view, "recommended") || strings.Contains(view, "attempt") {
		t.Fatalf("job inspect did not use the concise agent identity: %q", view)
	}
}

func TestAgentsEmptyStatesAndStickyParentBreadcrumb(t *testing.T) {
	model := Model{width: 60, height: 16, mainView: MainViewAgents, agents: AgentsState{Expanded: map[fix.JobID]bool{}}}
	if view := ansi.Strip(model.View()); !strings.Contains(view, "No fix jobs yet") {
		t.Fatalf("new-user empty state = %q", view)
	}
	model.setAgentPresentations([]fix.JobPresentation{{ID: "done", Phase: fix.PhaseCompleted}})
	if view := ansi.Strip(model.View()); !strings.Contains(view, "press a to show All") {
		t.Fatalf("active empty state = %q", view)
	}
	model.agents.FindQuery = "needle"
	if view := ansi.Strip(model.View()); !strings.Contains(view, `No fix jobs match "needle"`) {
		t.Fatalf("find empty state = %q", view)
	}

	job := agentTestJob("job-1", fix.PhaseRunning, "a.go", "b.go", "c.go", "d.go")
	job.ProfileLabel, job.Goal = "Codex", "SCORE ≤100"
	model = agentTestModel(36, 8, job)
	model.agents.Expanded[job.ID] = true
	model.agents.Selected = AgentRowID{JobID: job.ID, Path: "d.go"}
	model.ensureAgentVisible()
	header := strings.Split(ansi.Strip(model.View()), "\n")[1]
	if strings.Contains(header, "↑") || !strings.Contains(header, "RUNNING") {
		t.Fatalf("sticky parent breadcrumb = %q", header)
	}
}

func TestAgentsKeysNavigateLogicalRowsAndPreserveFileState(t *testing.T) {
	longPath := fix.RepoPath("zero/one/two/three/four/five/six/seven/eight/nine/a.go")
	job := agentTestJob("job-1", fix.PhaseRunning, longPath, "b.go")
	model := agentTestModel(36, 8, job)
	model.agents.Selected = AgentRowID{JobID: job.ID}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	result := updated.(*Model)
	if !result.agents.Expanded[job.ID] {
		t.Fatal("Enter did not expand selected job")
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	result = updated.(*Model)
	if result.agents.Selected != (AgentRowID{JobID: job.ID, Path: longPath}) {
		t.Fatalf("Down selected %+v", result.agents.Selected)
	}
	initialRow := agentFileRowContaining(result.View(), "a.go")
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	result = updated.(*Model)
	if result.agents.HorizontalOffset != pathScrollStep {
		t.Fatalf("Left horizontal offset = %d", result.agents.HorizontalOffset)
	}
	if shifted := agentFileRowContaining(result.View(), "T"); shifted == initialRow {
		t.Fatalf("Left did not reveal an earlier part of the right-anchored path: %q", shifted)
	}
	for result.agents.HorizontalOffset < result.maximumAgentHorizontalOffset() {
		updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
		result = updated.(*Model)
	}
	if row := agentFileRowContaining(result.View(), "zero/"); !strings.Contains(row, "zero/") {
		t.Fatalf("path could not reach its beginning: %q", row)
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	result = updated.(*Model)
	if result.agents.HorizontalOffset >= result.maximumAgentHorizontalOffset() {
		t.Fatal("Right did not return toward the filename end")
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	result = updated.(*Model)
	if !result.agents.ShowAll || result.agents.Selected.IsZero() {
		t.Fatalf("a did not toggle All while retaining selection: %+v", result.agents)
	}
}

func TestAgentPathScrollRangeUsesOnlyExactRenderedPathViewport(t *testing.T) {
	job := agentTestJob("job-1", fix.PhaseRunning, "short.go")
	job.Targets[0].Metrics = []fix.MetricValue{
		{ID: "cog", Value: 1, Complete: true}, {ID: "npath", Value: 2, Complete: true},
		{ID: "cyclo", Value: 3, Complete: true}, {ID: "deep", Value: 4, Complete: true},
		{ID: "god", Value: 5, Complete: true}, {ID: "coupling", Value: 6, Complete: true},
	}
	model := agentTestModel(36, 8, job)
	model.agents.Expanded[job.ID] = true
	if maximum := model.maximumAgentHorizontalOffset(); maximum != 0 {
		t.Fatalf("compact metric line created phantom path scroll range %d", maximum)
	}

	job.Targets[0].PreviousPath = "old/location/with/a/very/long/name.go"
	job.Targets[0].Path = "new/location/with/a/very/long/name.go"
	model = agentTestModel(60, 16, job)
	model.agents.Expanded[job.ID] = true
	want := max(0, lipgloss.Width(agentFileDisplayPath(job.Targets[0]))-model.agentFilePathViewport(job.Targets[0], ResponsiveMedium, 60))
	if got := model.maximumAgentHorizontalOffset(); got != want {
		t.Fatalf("renamed path scroll maximum = %d, want exact %d", got, want)
	}
}

func agentFileRowContaining(view, needle string) string {
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(line, needle) && strings.Contains(line, "    T") {
			return line
		}
	}
	return ""
}

func TestAgentProjectionStripsTerminalControlSequences(t *testing.T) {
	job := agentTestJob("job-1", fix.PhaseRunning, "safe.go")
	job.ProfileLabel = "Codex\x1b[2Jforged"
	job.Goal = "goal\nforged row"
	view := ansi.Strip(agentTestModel(80, 16, job).View())
	if strings.Contains(view, "\x1b") || !strings.Contains(view, "Codexforged") || !strings.Contains(view, "goal forged row") {
		t.Fatalf("provider text was not safely normalized: %q", view)
	}
}

func agentTestModel(width, height int, jobs ...fix.JobPresentation) Model {
	model := Model{
		width: width, height: height, mainView: MainViewAgents,
		agents:  AgentsState{Expanded: map[fix.JobID]bool{}},
		visible: defaultColumnVisibility(), weights: defaultWeights(), weightEnabled: defaultWeightEnabled(),
	}
	model.setAgentPresentations(jobs)
	return model
}

func agentTestJob(id fix.JobID, phase fix.Phase, paths ...fix.RepoPath) fix.JobPresentation {
	targets := make([]fix.FilePresentation, len(paths))
	for index, path := range paths {
		targets[index] = fix.FilePresentation{Path: path, Classification: "target"}
	}
	return fix.JobPresentation{ID: id, Phase: phase, Goal: "SCORE ≤100", Targets: targets, CreatedAt: time.Now().Add(-time.Minute)}
}

func assertAgentJobOrder(t *testing.T, jobs []fix.JobPresentation, wanted ...fix.JobID) {
	t.Helper()
	if len(jobs) != len(wanted) {
		t.Fatalf("job count = %d, want %d: %+v", len(jobs), len(wanted), jobs)
	}
	for index := range wanted {
		if jobs[index].ID != wanted[index] {
			t.Fatalf("job %d = %q, want %q", index, jobs[index].ID, wanted[index])
		}
	}
}

func assertCompactSelectedBlock(t *testing.T, view, firstNeedle, secondNeedle string, width int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	for index := range lines {
		if !strings.Contains(ansi.Strip(lines[index]), firstNeedle) || index+1 >= len(lines) {
			continue
		}
		firstPlain := ansi.Strip(lines[index])
		secondPlain := ansi.Strip(lines[index+1])
		if !strings.Contains(secondPlain, secondNeedle) {
			t.Fatalf("selected compact block second line = %q", secondPlain)
		}
		if lines[index] != agentScreenLine(strings.TrimRight(firstPlain, " "), true, width, style.TextPrimary) ||
			lines[index+1] != agentScreenLine(strings.TrimRight(secondPlain, " "), true, width, style.TextPrimary) {
			t.Fatal("selection background did not cover both lines of compact logical row")
		}
		return
	}
	t.Fatalf("selected compact row %q not found", firstNeedle)
}
