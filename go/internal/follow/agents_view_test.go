package follow

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/style"
)

func TestAgentRowsUseAttentionOrderAndActiveAllFilter(t *testing.T) {
	now := time.Now()
	jobs := []fix.JobPresentation{
		{ID: "completed", Phase: fix.PhaseCompleted, CreatedAt: now.Add(-7 * time.Minute)},
		{ID: "running", Phase: fix.PhaseRunning, CreatedAt: now.Add(-6 * time.Minute)},
		{ID: "review", Phase: fix.PhaseAwaitingReview, CreatedAt: now.Add(-5 * time.Minute)},
		{ID: "blocked", Phase: fix.PhaseRunning, Attention: fix.AttentionBlocking, CreatedAt: now.Add(-4 * time.Minute)},
		{ID: "failed", Phase: fix.PhaseAwaitingAction, CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "verify", Phase: fix.PhaseVerifying, CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "archived", Phase: fix.PhaseArchived, CreatedAt: now.Add(-time.Minute)},
		{ID: "discarded", Phase: fix.PhaseDiscarded},
	}
	model := Model{width: 80, height: 24, agents: AgentsState{Expanded: map[fix.JobID]bool{}}}
	model.setAgentPresentations(jobs)
	assertAgentJobOrder(t, model.visibleAgentJobs(), "blocked", "failed", "review", "verify", "running")

	model.toggleAgentFilter()
	assertAgentJobOrder(t, model.visibleAgentJobs(), "blocked", "failed", "review", "verify", "running", "completed", "archived")
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
		agentTestJob("urgent", fix.PhaseAwaitingAction, "urgent.go"),
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
	job.CurrentAction = "Running tests"
	job.ActorCount = 3
	job.Targets[0].BaselineScore = 142
	job.Targets[0].VerifiedScore = &verified
	job.Targets[0].Verification = "final verified"
	job.Targets[0].Metrics = []fix.MetricValue{{ID: "cog", Label: "COG", Value: 11, Complete: true}}
	job.Targets[1].Classification = "supporting"
	job.Targets[1].Changed = true

	for _, size := range []struct{ width, height int }{{36, 8}, {60, 16}, {80, 24}, {120, 30}} {
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
		if size.width == 36 {
			if !strings.Contains(plain, "SCORE 142→91✓") {
				t.Fatalf("compact metrics missing authoritative result: %q", plain)
			}
			assertCompactSelectedBlock(t, view, "internal/service.go", "SCORE 142→91✓", size.width)
		}
		if size.width == 120 && (!strings.Contains(plain, "gpt-5.6") || !strings.Contains(plain, "Running tests")) {
			t.Fatalf("wide view omitted model/activity: %q", plain)
		}
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
	if !strings.Contains(header, "↑") || !strings.Contains(header, "RUNNING") {
		t.Fatalf("sticky parent breadcrumb = %q", header)
	}
}

func TestAgentsKeysNavigateLogicalRowsAndPreserveFileState(t *testing.T) {
	longPath := fix.RepoPath(strings.Repeat("long/", 10) + "a.go")
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
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	result = updated.(*Model)
	if result.agents.HorizontalOffset != pathScrollStep {
		t.Fatalf("Right horizontal offset = %d", result.agents.HorizontalOffset)
	}
	updated, _ = result.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	result = updated.(*Model)
	if !result.agents.ShowAll || result.agents.Selected.IsZero() {
		t.Fatalf("a did not toggle All while retaining selection: %+v", result.agents)
	}
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
	model := Model{width: width, height: height, mainView: MainViewAgents, agents: AgentsState{Expanded: map[fix.JobID]bool{}}}
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
