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

func TestCompactJobMonitorScrollsSummaryActorsAndActivityAsOneSurface(t *testing.T) {
	job := fix.JobPresentation{
		ID: "job-compact", Phase: fix.PhaseRunning, Goal: "score <= 100", AttemptOrdinal: 1,
		Actors: []fix.ActorPresentation{{ID: "primary", CurrentAction: "editing"}, {ID: "reviewer", ParentID: "primary", CurrentAction: "reviewing"}},
	}
	model := Model{width: 36, height: 8, jobMonitor: jobMonitorState{
		job: job, activity: []fixapp.LogEntry{{At: time.Unix(1, 0), Summary: "started"}, {At: time.Unix(2, 0), Summary: "latest activity"}}, activityTruncated: true,
	}}
	initial := ansi.Strip(strings.Join(model.jobMonitorContent(36, 6), "\n"))
	if !strings.Contains(initial, "Usage: not reported") || strings.Contains(initial, "Usage: in 0") {
		t.Fatalf("unreported usage was not truthful: %q", initial)
	}
	for range 20 {
		model.handleJobMonitorKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if model.jobMonitor.offset != model.jobMonitorMaxOffset() {
		t.Fatalf("monitor offset=%d max=%d", model.jobMonitor.offset, model.jobMonitorMaxOffset())
	}
	scrolled := ansi.Strip(strings.Join(model.jobMonitorContent(36, 6), "\n"))
	for _, want := range []string{"reviewer", "ACTIVITY", "latest activity", "Earlier activity omitted"} {
		if !strings.Contains(scrolled, want) {
			t.Fatalf("compact unified scroll omitted %q: %q", want, scrolled)
		}
	}

	model.jobMonitor.job.UsageReported = true
	model.jobMonitor.offset = 0
	if reported := ansi.Strip(strings.Join(model.jobMonitorContent(36, 6), "\n")); !strings.Contains(reported, "Usage: in 0") {
		t.Fatalf("reported zero usage was hidden: %q", reported)
	}
}

func TestEffectivePromptPreviewIncludesEnvelopeAndDetachedPromptCanReset(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 60, 16)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	model.fixDialog.cursor = fixFieldAdvanced
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	preview := ansi.Strip(model.View())
	for _, want := range []string{"EFFECTIVE PROMPT PREVIEW", "locked", "old", "baseline"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("effective preview omitted %q: %q", want, preview)
		}
	}
	model.handlePromptEditorKey(tea.KeyMsg{Type: tea.KeyEsc})

	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.fixDialog.prompt.SetValue("exact detached instructions")
	model.handlePromptEditorKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	model.handlePromptDetachKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handlePromptEditorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if !model.fixDialog.detached || !model.fixDialog.promptResetPending {
		t.Fatal("first reset key did not require confirmation")
	}
	model.handlePromptEditorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if model.fixDialog.detached || model.fixDialog.promptResetPending || model.fixDialog.draft.Instructions.DetachedBody != "" {
		t.Fatalf("reset did not return to generated controls: %+v", model.fixDialog)
	}
	if got, want := model.fixDialog.prompt.Value(), generatedFixBody(model.fixDialog.draft); got != want {
		t.Fatalf("reset body=%q want generated=%q", got, want)
	}
}

func TestFixFormShowsResolvedConfinementAndNetworkAtResponsiveSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{36, 8}, {60, 16}, {80, 24}} {
		draft := readyFixDraft("a.go")
		draft.Probe.Capabilities.Network.TransportRequired = true
		service := &fakeFixService{draft: draft}
		model := fixTestModel(service, size.width, size.height)
		prepare := model.openFixForSelected()
		model.handleFixPrepared(prepare().(fixPreparedMsg))
		view := ansi.Strip(model.View())
		if !strings.Contains(view, "Confinement: enforced") || !strings.Contains(view, "Network:") {
			t.Fatalf("%dx%d form omitted resolved runtime summary: %q", size.width, size.height, view)
		}
	}
}

func TestFixFormDescribesProviderManagedBoundaryWithoutOverclaiming(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.Probe.Capabilities.Isolation = agent.RuntimeIsolation{
		Writes: agent.CandidateTreeEnforced, ProviderManagedCancellation: true,
	}
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "provider workspace sandbox") || !strings.Contains(view, "per-job") || !strings.Contains(view, "cancellation") {
		t.Fatalf("provider-managed boundary absent: %q", view)
	}
	for _, overclaim := range []string{"candidate/Git", "reads", "child processes"} {
		if strings.Contains(view, overclaim) {
			t.Fatalf("provider-managed boundary overclaimed %q: %q", overclaim, view)
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
	if !model.fixDialog.deliveryStale || model.fixDialogRunnable() || !strings.Contains(ansi.Strip(model.View()), "press r to recheck") {
		t.Fatalf("delivery edit retained stale readiness: %+v", model.fixDialog)
	}
	_, reprepare := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if reprepare == nil {
		t.Fatal("delivery readiness could not be rechecked")
	}
	model.handleFixPrepared(reprepare().(fixPreparedMsg))
	if service.prepareRequest.Delivery == nil || service.prepareRequest.Delivery.Mode != fix.DeliveryModeBranch || service.prepareRequest.Delivery.Branch != "slopwatch/fix/a" {
		t.Fatalf("recheck did not validate the edited delivery tuple: %+v", service.prepareRequest.Delivery)
	}
	if model.fixDialog.deliveryStale || !model.fixDialogRunnable() || model.fixDialog.draft.DeliveryMode != fix.DeliveryModeBranch {
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

func TestMissingValidationPlansExplainInstallationOwnedRemediation(t *testing.T) {
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
	if !strings.Contains(view, "No trusted validation plans") || !strings.Contains(view, "press r to") || !strings.Contains(view, "reload") {
		t.Fatalf("empty validation settings dead-ended: %q", view)
	}
}
