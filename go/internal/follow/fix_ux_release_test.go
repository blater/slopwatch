package follow

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/validation"
)

func TestCompactJobInspectScrollsSummaryAndActorsWithoutLogs(t *testing.T) {
	job := fix.JobPresentation{
		ID: "job-compact", Phase: fix.PhaseRunning, Goal: "score <= 100", AttemptOrdinal: 1,
		Actors: []fix.ActorPresentation{{ID: "primary", CurrentAction: "editing"}, {ID: "reviewer", ParentID: "primary", CurrentAction: "reviewing"}},
	}
	model := Model{width: 36, height: 8, jobMonitor: jobMonitorState{job: job,
		activity: []fixapp.LogEntry{{At: time.Unix(1, 0), Summary: "must not render"}}, activityTruncated: true}}
	initial := ansi.Strip(strings.Join(model.jobMonitorContent(36, 6), "\n"))
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
		draft := readyFixDraft("a.go")
		draft.Probe.Capabilities.Network.TransportRequired = true
		service := &fakeFixService{draft: draft}
		model := fixTestModel(service, size.width, size.height)
		prepare := model.openFixForSelected()
		model.handleFixPrepared(prepare().(fixPreparedMsg))
		view := ansi.Strip(model.View())
		if !strings.Contains(view, "READY TO FIX") || strings.Contains(view, "Confinement:") || strings.Contains(view, "Network:") {
			t.Fatalf("%dx%d form did not use the concise ready status: %q", size.width, size.height, view)
		}
	}
}

func TestFixFormDoesNotExposeProviderBoundaryDetailsWhenReady(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.Probe.Capabilities.Isolation = agent.RuntimeIsolation{
		Writes: agent.CandidateTreeEnforced, ProviderManagedCancellation: true,
	}
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	view := ansi.Strip(model.View())
	for _, noise := range []string{"provider workspace sandbox", "per-job", "cancellation", "candidate/Git", "reads", "child processes"} {
		if strings.Contains(view, noise) {
			t.Fatalf("ready form exposed provider detail %q: %q", noise, view)
		}
	}
}

func TestFixFormDoesNotExposeAutomaticRetryCaps(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 44, 10)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	view := ansi.Strip(model.View())
	if strings.Contains(view, "Max attempts") || strings.Contains(view, "Attempt timeout") {
		t.Fatalf("fix form exposed removed automatic retry caps: %q", view)
	}
}

func TestDeliveryAndBranchEditsInvalidateReadinessUntilReprepare(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	model.fixDialog.cursor = fixFieldDelivery
	model.adjustFixField(1)
	if !model.fixDialog.deliveryStale || model.fixDialogRunnable() || !strings.Contains(ansi.Strip(model.View()), "press R") {
		t.Fatalf("delivery edit retained stale readiness: %+v", model.fixDialog)
	}
	_, reprepare := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if reprepare == nil {
		t.Fatal("delivery readiness could not be rechecked")
	}
	model.handleFixPrepared(reprepare().(fixPreparedMsg))
	if service.prepareRequest.Delivery == nil || service.prepareRequest.Delivery.Mode != fix.DeliveryModePullRequest || service.prepareRequest.Delivery.Branch != "slopwatch/fix/a" {
		t.Fatalf("recheck did not validate the edited delivery tuple: %+v", service.prepareRequest.Delivery)
	}
	if model.fixDialog.deliveryStale || !model.fixDialogRunnable() || model.fixDialog.draft.DeliveryMode != fix.DeliveryModePullRequest {
		t.Fatalf("reprepare did not restore edited delivery readiness: %+v", model.fixDialog)
	}

	model.fixDialog.cursor = fixFieldBranch
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.fixDialog.branch.SetValue("slopwatch/fix/edited")
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.fixDialog.deliveryStale || model.fixDialogRunnable() {
		t.Fatal("branch edit retained stale readiness")
	}
}

func TestPullRequestWithoutValidationAutofocusesDirectRepair(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.DeliveryMode = fix.DeliveryModePullRequest
	draft.Preferences.Delivery.RequireValidation = true
	draft.ValidationPlanID = ""
	draft.Preferences.Validation = []validation.Plan{{ID: "release", Checks: []validation.Check{{ID: "test"}}}}
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	if model.fixDialog.cursor != fixFieldValidation {
		t.Fatalf("cursor = %d, want validation field", model.fixDialog.cursor)
	}
	footer := ansi.Strip(model.fixDialogFooter())
	if !strings.Contains(footer, "←/→ choose validation plan") {
		t.Fatalf("blocked validation footer omitted direct repair controls: %q", footer)
	}
}

func TestMissingValidationPlansRemainClearlyUnavailable(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.DeliveryMode = fix.DeliveryModePullRequest
	draft.Preferences.Delivery.RequireValidation = true
	draft.ValidationPlanID = ""
	draft.Preferences.Validation = nil
	if summary := fixPreflightSummary(draft); !strings.Contains(summary, "no trusted plans") || !strings.Contains(summary, "preferences") {
		t.Fatalf("missing-plan remediation=%q", summary)
	}

	resolved := settingsResolved()
	resolved.Validation = nil
	model := settingsModel(configValidation, resolved, &settingsConfigStore{})
	view := ansi.Strip(model.configSettingsView())
	if !strings.Contains(view, "No validation plans configured") {
		t.Fatalf("empty validation settings dead-ended: %q", view)
	}
}
