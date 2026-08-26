package follow

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/validation"
)

func TestDegradedConfinementIsExplicitlyNonRunnableAndNotPresentedAsSettingsRepair(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.Probe.State = agent.ProbeDegraded
	draft.Probe.Diagnostic = "crash containment is not proven"
	draft.Probe.Capabilities.Isolation.CrashContainment = false
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	plain := ansi.Strip(model.View())
	for _, wanted := range []string{"Run disabled", "this build cannot run", "fixes safely", "Settings cannot enable it", "crash containment is not", "proven"} {
		if !strings.Contains(plain, wanted) {
			t.Fatalf("degraded runtime omitted %q: %q", wanted, plain)
		}
	}
	if model.fixDialogRunnable() || strings.Contains(model.fixDialogFooter(), "Enter run") {
		t.Fatal("degraded confinement was presented as runnable")
	}
	if _, ok := model.fixRemediationSettingsKind(); ok {
		t.Fatal("unsupported build confinement was falsely presented as settings-remediable")
	}
}

func TestUnauthenticatedRuntimeLinksDirectlyToAgentRepair(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.Probe.State = agent.ProbeUnauthenticated
	draft.Probe.Diagnostic = "Run codex login to authorize"
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	model.configStore = &settingsConfigStore{resolved: settingsResolved()}
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	if text := ansi.Strip(model.View()); !strings.Contains(text, "Settings › Agents") || !strings.Contains(text, "connection guidance") {
		t.Fatalf("authentication remediation was not actionable: %q", text)
	}
	_, command := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if command == nil || !model.configSettings.open || model.configSettings.kind != configAgents || !model.hasOverlay(OverlayFixForm) || !model.hasOverlay(OverlayConfigSettings) {
		t.Fatalf("agent remediation did not open directly: open=%t kind=%q overlays=%d", model.configSettings.open, model.configSettings.kind, model.overlays.Len())
	}
}

func TestFixRemediationSettingsRoundTripPreservesDraftAndRechecksReadiness(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.Probe.State = agent.ProbeUnauthenticated
	draft.Probe.Diagnostic = "Run codex login to authorize"
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	model.configStore = &settingsConfigStore{resolved: settingsResolved()}
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	model.fixDialog.draft.TargetScore = 70
	model.fixDialog.focus["cog"] = true
	model.fixDialog.branch.SetValue("slopwatch/fix/preserved")
	model.fixDialog.prompt.SetValue("preserved detached task")
	model.fixDialog.detached = true
	model.fixDialog.cursor = fixFieldEffort
	if !model.syncFixDraft() {
		t.Fatalf("could not establish edited draft: %s", model.fixDialog.errorText)
	}

	_, load := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if load == nil || model.overlays.Len() != 2 {
		t.Fatalf("remediation stack=%d command nil=%t", model.overlays.Len(), load == nil)
	}
	model.handleConfigResolved(load().(configResolvedMsg))

	service.draft = readyFixDraft("a.go")
	_, reprepare := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	if reprepare == nil || model.configSettings.open || !model.hasOverlay(OverlayFixForm) || model.hasOverlay(OverlayConfigSettings) {
		t.Fatalf("settings did not return to Fix and reprepare: open=%t overlays=%d command nil=%t", model.configSettings.open, model.overlays.Len(), reprepare == nil)
	}
	if !model.fixDialog.loading || model.fixDialog.cursor != fixFieldEffort || model.fixDialog.draft.TargetScore != 70 ||
		model.fixDialog.branch.Value() != "slopwatch/fix/preserved" || model.fixDialog.prompt.Value() != "preserved detached task" || !model.fixDialog.detached {
		t.Fatalf("Fix edits changed before reprepare: %+v", model.fixDialog)
	}

	model.handleFixPrepared(reprepare().(fixPreparedMsg))
	if model.fixDialog.loading || !model.fixDialogRunnable() || model.fixDialog.cursor != fixFieldEffort || model.fixDialog.draft.TargetScore != 70 ||
		model.fixDialog.draft.BranchName != "slopwatch/fix/preserved" || model.fixDialog.draft.Instructions.DetachedBody != "preserved detached task" {
		t.Fatalf("reprepared Fix lost edits or readiness: %+v", model.fixDialog)
	}
}

func TestUnavailableOrEmptyValidationPlanDisablesRunAndLinksToValidationSettings(t *testing.T) {
	for _, plans := range [][]validation.Plan{nil, {{ID: "ci"}}} {
		draft := readyFixDraft("a.go")
		draft.ValidationPlanID = "ci"
		draft.Preferences.Validation = plans
		service := &fakeFixService{draft: draft}
		model := fixTestModel(service, 80, 24)
		model.configStore = &settingsConfigStore{resolved: settingsResolved()}
		prepare := model.openFixForSelected()
		model.handleFixPrepared(prepare().(fixPreparedMsg))

		if model.fixDialogRunnable() || !strings.Contains(ansi.Strip(model.View()), "validation plan \"ci\" is unavailable") {
			t.Fatalf("unavailable validation was presented as runnable: %q", ansi.Strip(model.View()))
		}
		_, command := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		if command == nil || !model.configSettings.open || model.configSettings.kind != configValidation {
			t.Fatalf("validation remediation did not open directly: open=%t kind=%q", model.configSettings.open, model.configSettings.kind)
		}
	}
}

func TestValidationConfinementReadinessIsVisibleAndCanBeDisabledInForm(t *testing.T) {
	readiness := validation.Readiness{Required: true, Ready: false, Diagnostic: "validation commands are disabled until candidate-only confinement is proven"}
	draft := readyFixDraft("a.go")
	draft.ValidationPlanID = "ci"
	draft.Preferences.Validation = []validation.Plan{{ID: "ci", Checks: []validation.Check{{ID: "test"}}}}
	draft.ValidationReadiness = readiness
	draft.ValidationReadinessByPlan = map[string]validation.Readiness{"ci": readiness}
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	if model.fixDialogRunnable() {
		t.Fatal("unproven validation confinement was presented as runnable")
	}
	if summary := fixPreflightSummary(model.fixDialog.draft); !strings.Contains(summary, "validation plan \"ci\" is NOT RUNNABLE") || !strings.Contains(summary, readiness.Diagnostic) {
		t.Fatalf("validation readiness diagnostic=%q", summary)
	}
	if text := ansi.Strip(model.View()); !strings.Contains(text, "Validation     ci · NOT RUNNABLE") || !strings.Contains(text, "choose a ready") || !strings.Contains(text, "plan or none") {
		t.Fatalf("validation readiness was not visible in Fix: %q", text)
	}

	model.fixDialog.cursor = fixFieldValidation
	model.adjustFixField(1)
	if model.fixDialog.draft.ValidationPlanID != "" || !model.fixDialogRunnable() {
		t.Fatalf("choosing no optional validation did not update readiness: %+v", model.fixDialog.draft.ValidationReadiness)
	}
}

func TestSubmitFailureRequiresAuthoritativeRecheckBeforeRetry(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go"), submitErr: errors.New("agent profile became unauthenticated")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	model.fixDialog.cursor = fixFieldRun
	_, submit := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixSubmitted(submit().(fixSubmittedMsg))
	if !model.fixDialog.submitBlocked || model.fixDialogRunnable() || !strings.Contains(model.fixDialog.statusText, "recheck") {
		t.Fatalf("submit failure did not invalidate readiness: %+v", model.fixDialog)
	}
	if _, retry := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter}); retry != nil || !strings.Contains(model.fixDialog.errorText, "press r") {
		t.Fatal("stale submission readiness was retried without a recheck")
	}
	_, recheck := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if recheck == nil || !model.fixDialog.loading || model.fixDialog.submitBlocked {
		t.Fatal("r did not start a fresh authoritative readiness check")
	}
}

func TestSelectedValidationPlanWithNoChecksCannotBeSaved(t *testing.T) {
	resolved := settingsResolved()
	resolved.Validation = []validation.Plan{{ID: "empty"}}
	resolved.Fix.ValidationPlan = "empty"
	if err := validateConfigSettings(configValidation, resolved); err == nil || !strings.Contains(err.Error(), "no configured checks") {
		t.Fatalf("empty selected validation plan error=%v", err)
	}
}
