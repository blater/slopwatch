package follow

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopmochi/internal/agent"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/fixapp"
)

func TestCompactJobInspectScrollsSummaryAndActorsWithoutLogs(t *testing.T) {
	job := fix.JobPresentation{
		ID: "job-compact", Phase: fix.PhaseRunning, Goal: "score <= 100", AttemptOrdinal: 1,
		Actors: []fix.ActorPresentation{{ID: "primary", CurrentAction: "editing"}, {ID: "reviewer", ParentID: "primary", CurrentAction: "reviewing"}},
	}
	model := Model{width: 36, height: 8, jobMonitor: jobMonitorState{job: job,
		activity: []fixapp.LogEntry{{At: time.Unix(1, 0), Summary: "must not render"}}}}
	initial := ansi.Strip(strings.Join(model.jobMonitorContent(36, 8), "\n"))
	if !strings.Contains(initial, "Tokens: not reported") || strings.Contains(initial, "Tokens: input 0") {
		t.Fatalf("unreported usage was not truthful: %q", initial)
	}
	for range 20 {
		model.handleJobMonitorKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if model.jobMonitor.offset != model.jobMonitorMaxOffset() {
		t.Fatalf("monitor offset=%d max=%d", model.jobMonitor.offset, model.jobMonitorMaxOffset())
	}
	scrolled := ansi.Strip(strings.Join(model.jobMonitorContent(36, 6), "\n"))
	for _, want := range []string{"ACTORS", "reviewer"} {
		if !strings.Contains(scrolled, want) {
			t.Fatalf("compact inspect scroll omitted %q: %q", want, scrolled)
		}
	}
	if strings.Contains(scrolled, "ACTIVITY") || strings.Contains(scrolled, "must not render") || strings.Contains(scrolled, "Earlier activity") {
		t.Fatalf("compact inspect rendered log content: %q", scrolled)
	}

	model.jobMonitor.job.UsageReported = true
	model.jobMonitor.offset = 0
	if reported := ansi.Strip(strings.Join(model.jobMonitorContent(36, 6), "\n")); !strings.Contains(reported, "Tokens: input 0") {
		t.Fatalf("reported zero usage was hidden: %q", reported)
	}
}

func TestFixFormUsesConciseReadyStatusAtResponsiveSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{36, 8}, {60, 16}, {80, 24}} {
		input := readyFixInput("a.go")
		input.Probe.Capabilities.Network.TransportRequired = true
		service := &fakeFixService{input: input}
		model := fixTestModel(service, size.width, size.height)
		prepare := model.openFixForSelected()
		model.handleFixLoaded(prepare().(fixLoadedMsg))
		view := ansi.Strip(model.View())
		if !strings.Contains(view, "READY TO FIX") || strings.Contains(view, "Confinement:") || strings.Contains(view, "Network:") {
			t.Fatalf("%dx%d form did not use the concise ready status: %q", size.width, size.height, view)
		}
	}
}

func TestFixFormDoesNotExposeProviderBoundaryDetailsWhenReady(t *testing.T) {
	input := readyFixInput("a.go")
	input.Probe.Capabilities.Isolation = agent.RuntimeIsolation{
		Writes: agent.CandidateTreeEnforced, ProviderManagedCancellation: true,
	}
	service := &fakeFixService{input: input}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	view := ansi.Strip(model.View())
	for _, noise := range []string{"provider workspace sandbox", "per-job", "cancellation", "candidate/Git", "reads", "child processes"} {
		if strings.Contains(view, noise) {
			t.Fatalf("ready form exposed provider detail %q: %q", noise, view)
		}
	}
}

func TestFixFormDoesNotExposeAutomaticRetryCaps(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 44, 10)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	view := ansi.Strip(model.View())
	if strings.Contains(view, "Max attempts") || strings.Contains(view, "Attempt timeout") {
		t.Fatalf("fix form exposed removed automatic retry caps: %q", view)
	}
}

func TestDeliveryAndBranchEditsDoNotRequireSpeculativeRecheck(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))

	model.fixDialog.cursor = fixFieldPublish
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyDown})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.fixDialogRunnable() || strings.Contains(ansi.Strip(model.View()), "RECHECK REQUIRED") {
		t.Fatalf("delivery edit created a speculative readiness gate: %+v", model.fixDialog)
	}
	if model.fixDialog.input.DeliveryPlan.Publish != fix.PublishPullRequest {
		t.Fatalf("delivery edit was not retained: %+v", model.fixDialog)
	}

	model.fixDialog.cursor = fixFieldBranch
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.fixDialog.branch.SetValue("slopmochi/fix/edited")
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.fixDialogRunnable() {
		t.Fatal("branch edit created a speculative readiness gate")
	}
}
