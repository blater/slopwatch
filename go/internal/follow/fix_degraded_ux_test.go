package follow

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
)

func TestDegradedAgentIsWarningAndRuntimeAttemptRemainsAvailable(t *testing.T) {
	input := readyFixInput("a.go")
	input.Probe.State = agent.ProbeDegraded
	input.Probe.Diagnostic = "crash containment is not proven"
	input.Probe.Capabilities.Isolation.CrashContainment = false
	service := &fakeFixService{input: input}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))

	plain := ansi.Strip(model.View())
	for _, wanted := range []string{"READY WITH WARNING", "degraded", "runtime failure", "crash", "containment is not proven"} {
		if !strings.Contains(plain, wanted) {
			t.Fatalf("degraded runtime omitted %q: %q", wanted, plain)
		}
	}
	if !model.fixDialogRunnable() || !strings.Contains(model.fixDialogFooter(), "r run") {
		t.Fatal("degraded readiness warning prevented a runtime attempt")
	}
	if kind, ok := model.fixRemediationSettingsKind(); !ok || kind != configAgents {
		t.Fatalf("blocked agent did not link to agent settings: kind=%q ok=%t", kind, ok)
	}
}

func TestUnprobedAgentIsReadyWithoutAWarning(t *testing.T) {
	input := readyFixInput("a.go")
	input.Probe = agent.ProbeResult{}
	model := fixTestModel(&fakeFixService{input: input}, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	text := ansi.Strip(model.View())
	if !strings.Contains(text, "READY") || strings.Contains(text, "READY WITH WARNING") || strings.Contains(text, "not probed") {
		t.Fatalf("normal unprobed state was presented as a warning: %q", text)
	}
}

func TestUnauthenticatedRuntimeLinksDirectlyToAgentRepair(t *testing.T) {
	input := readyFixInput("a.go")
	input.Probe.State = agent.ProbeUnauthenticated
	input.Probe.Diagnostic = "Run codex login to authorize"
	service := &fakeFixService{input: input}
	model := fixTestModel(service, 80, 24)
	model.configStore = &settingsConfigStore{resolved: settingsResolved()}
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))

	if text := ansi.Strip(model.View()); !strings.Contains(text, "READY WITH WARNING") || !strings.Contains(text, "unauthenticated") || !strings.Contains(text, "runtime") {
		t.Fatalf("authentication remediation was not actionable: %q", text)
	}
	_, command := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if command == nil || !model.configSettings.open || model.configSettings.kind != configAgents || !model.hasOverlay(OverlayFixForm) || !model.hasOverlay(OverlayConfigSettings) {
		t.Fatalf("agent remediation did not open directly: open=%t kind=%q overlays=%d", model.configSettings.open, model.configSettings.kind, model.overlays.Len())
	}
}

func TestFixRemediationSettingsRoundTripPreservesDraftAndRechecksReadiness(t *testing.T) {
	input := readyFixInput("a.go")
	input.Probe.State = agent.ProbeUnauthenticated
	input.Probe.Diagnostic = "Run codex login to authorize"
	service := &fakeFixService{input: input}
	model := fixTestModel(service, 80, 24)
	model.configStore = &settingsConfigStore{resolved: settingsResolved()}
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))

	model.fixDialog.input.TargetScore = 70
	model.fixDialog.focus["cog"] = true
	model.fixDialog.branch.SetValue("slopwatch/fix/preserved")
	model.fixDialog.cursor = fixFieldEffort
	if !model.syncFixInput() {
		t.Fatalf("could not establish edited input: %s", model.fixDialog.errorText)
	}

	_, load := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if load == nil || model.overlays.Len() != 2 {
		t.Fatalf("remediation stack=%d command nil=%t", model.overlays.Len(), load == nil)
	}
	model.handleConfigResolved(load().(configResolvedMsg))

	service.input = readyFixInput("a.go")
	_, reprepare := model.handleConfigSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	if reprepare == nil || model.configSettings.open || !model.hasOverlay(OverlayFixForm) || model.hasOverlay(OverlayConfigSettings) {
		t.Fatalf("settings did not return to Fix and reprepare: open=%t overlays=%d command nil=%t", model.configSettings.open, model.overlays.Len(), reprepare == nil)
	}
	if !model.fixDialog.loading || model.fixDialog.cursor != fixFieldEffort || model.fixDialog.input.TargetScore != 70 ||
		model.fixDialog.branch.Value() != "slopwatch/fix/preserved" {
		t.Fatalf("Fix edits changed before reprepare: %+v", model.fixDialog)
	}

	model.handleFixLoaded(reprepare().(fixLoadedMsg))
	if model.fixDialog.loading || !model.fixDialogRunnable() || model.fixDialog.cursor != fixFieldEffort || model.fixDialog.input.TargetScore != 70 ||
		model.fixDialog.input.BranchName != "slopwatch/fix/preserved" {
		t.Fatalf("reprepared Fix lost edits or readiness: %+v", model.fixDialog)
	}
}

func TestRunFailureCanBeRetriedWithoutReadinessRecheck(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go"), runErr: errors.New("agent profile became unauthenticated")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	_, run := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model.handleFixStarted(run().(fixStartedMsg))
	if !model.fixDialogRunnable() || !strings.Contains(model.fixDialog.statusText, "retry") {
		t.Fatalf("run failure incorrectly invalidated readiness: %+v", model.fixDialog)
	}
	if _, retry := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}); retry == nil {
		t.Fatal("start could not be retried directly")
	}
}
