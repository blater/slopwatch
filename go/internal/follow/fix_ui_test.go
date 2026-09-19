package follow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/fixapp"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/style"
)

func TestFixKeyWithoutServiceExplainsUnavailable(t *testing.T) {
	model := Model{
		files: FilesState{
			Selected: "a.go",
		}}
	updated, command := handleKey(&model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	result := updated.(*Model)
	if command != nil || !strings.Contains(result.runtimeError, "Fix unavailable") || result.overlays.Len() != 1 {
		t.Fatalf("nil service behavior: status=%q overlays=%d command=%v", result.status, result.overlays.Len(), command)
	}
}

func TestFilesFooterAdvertisesFixAndAgentsAtUsableSizes(t *testing.T) {
	footer := ansi.Strip(footer(Model{width: 80}))
	if !strings.Contains(footer, "fix") || !strings.Contains(footer, "agents") {
		t.Fatalf("Files footer omitted the new workflow: %q", footer)
	}
}

func TestFixKeyOpensExistingReservationInsteadOfPreparingDuplicate(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 80, 24)
	model.agents.Jobs = []fix.JobPresentation{{
		ID: "existing", Phase: fix.PhaseFailed, Attention: fix.AttentionError,
		Targets: []fix.FilePresentation{{Path: "a.go"}},
	}}
	if command := model.openFixForSelected(); command != nil {
		t.Fatal("reserved target started a duplicate Prepare")
	}
	if model.mainView != MainViewAgents || model.agents.Selected.JobID != "existing" || overlayPresent(model.overlays, OverlayFixForm) {
		t.Fatalf("existing reservation was not opened: view=%d selected=%+v", model.mainView, model.agents.Selected)
	}
}

func TestFixPrepareUsesGenerationAndOwnsKeys(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 80, 24)
	command := model.openFixForSelected()
	if command == nil || model.overlays.Len() != 1 || !model.fixDialog.loading {
		t.Fatal("x did not open a loading Fix dialog")
	}
	oldGeneration := model.fixDialog.generation
	model.fixDialog.generation++
	model.handleFixLoaded(command().(fixLoadedMsg))
	if model.fixDialog.hasInput {
		t.Fatal("late prepare result from stale generation was applied")
	}
	model.fixDialog.generation = oldGeneration
	model.handleFixLoaded(command().(fixLoadedMsg))
	if !model.fixDialog.hasInput || model.fixDialog.loading {
		t.Fatal("current prepare result was not applied")
	}

	updated, _ := handleKey(&model, tea.KeyMsg{Type: tea.KeyTab})
	if updated.(*Model).mainView != MainViewFiles {
		t.Fatal("Tab escaped the Fix dialog")
	}
}

func TestFixDialogResponsiveSurfacesAndContractSafeRun(t *testing.T) {
	assertFixDialogSizes(t)
	assertFixRunContract(t)
}

func assertFixDialogSizes(t *testing.T) {
	t.Helper()
	for _, size := range []struct{ width, height int }{{36, 6}, {36, 8}, {60, 16}, {80, 24}, {120, 30}} {
		model := fixTestModel(&fakeFixService{input: readyFixInput("a.go")}, size.width, size.height)
		command := model.openFixForSelected()
		model.handleFixLoaded(command().(fixLoadedMsg))
		view := model.View()
		assertScreenSize(t, view, size.width, size.height)
		assertFixDialogText(t, ansi.Strip(view), size.width, size.height)
	}
}

func assertFixDialogText(t *testing.T, plain string, width, height int) {
	t.Helper()
	if !strings.Contains(plain, "FIX FILE") || !strings.Contains(plain, "Target score") || !strings.Contains(plain, "READY TO FIX") {
		t.Fatalf("%dx%d Fix surface omitted controls: %q", width, height, plain)
	}
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "╰") || !strings.Contains(plain, "r run") {
		t.Fatalf("%dx%d Fix surface stopped being a dialog: %q", width, height, plain)
	}
}

func assertFixRunContract(t *testing.T) {
	t.Helper()
	service := &fakeFixService{input: readyFixInput("a.go"), runID: "job-new"}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	model.fixDialog.cursor = fixFieldTargetScore
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyLeft})
	model.fixDialog.focus["cog"] = true
	_, run := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if run == nil {
		t.Fatalf("Run fix did not run: %s", model.fixDialog.errorText)
	}
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !overlayPresent(model.overlays, OverlayFixForm) {
		t.Fatal("Esc hid a start before its start result was known")
	}
	message := run().(fixStartedMsg)
	assertFixRunInput(t, service, message)
	model.handleFixStarted(message)
	if model.mainView != MainViewAgents || model.agents.Selected.JobID != "job-new" || overlayPresent(model.overlays, OverlayFixForm) {
		t.Fatalf("successful start did not transition to job: view=%d selected=%+v overlays=%d", model.mainView, model.agents.Selected, model.overlays.Len())
	}
}

func assertFixRunInput(t *testing.T, service *fakeFixService, message fixStartedMsg) {
	t.Helper()
	if message.err != nil || service.ranInput.TargetScore != 90 || service.ranInput.Baseline.Contract.Goal.MaximumScore != 90 {
		t.Fatalf("ranInput score contract split: input=%+v message=%+v", service.ranInput, message)
	}
	if len(service.ranInput.Baseline.Contract.Goal.Focus) != 1 || !strings.Contains(service.ranInput.Instructions.Objective, "score of 90 or lower") || !strings.Contains(service.ranInput.Instructions.EffectiveBody(), "handle Git") {
		t.Fatalf("ranInput prompt/goal not synchronized: goal=%+v instructions=%+v", service.ranInput.Baseline.Contract.Goal, service.ranInput.Instructions)
	}
}

func TestFixDialogUsesTheEssentialAgentNameOnly(t *testing.T) {
	input := readyFixInput("a.go")
	input.Profile.Label = "Codex — managed sign-in (ChatGPT recommended)"
	input.Probe.Authentication = agent.Authentication{Method: "api-key", Label: "Signed in with an API key"}
	model := Model{fixDialog: fixDialogState{hasInput: true, input: input}}

	text := ansi.Strip(strings.Join(model.fixDialog.fieldRows(model.profileCatalog, 120), "\n"))
	if !strings.Contains(text, "Agent           Codex") || strings.Contains(text, "ChatGPT recommended") || strings.Contains(text, "Signed in") {
		t.Fatalf("Fix dialog did not reduce the agent profile to its essential name: %q", text)
	}
}

func TestTargetScoreAdjustmentsSerializeIntoGlobalPreferences(t *testing.T) {
	model, store, resolved := targetScoreSaveModel(t)
	first := startTargetScoreSave(t, model)
	queueTargetScoreSave(t, model)
	assertTargetScoreConcurrentEdit(t, store, resolved)
	finishTargetScoreSaves(t, model, first, store)
}

func targetScoreSaveModel(t *testing.T) (*Model, *appconfig.Memory, appconfig.Resolved) {
	t.Helper()
	initial := settingsResolved()
	initial.Revision = 1
	initial.Fix.TargetScore = 100
	initial.Fix.Effort = "high"
	store := appconfig.NewMemory(initial)
	resolved, err := store.Resolve(t.Context(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	input := readyFixInput("a.go")
	input.Preferences = resolved
	service := &fakeFixService{input: input}
	model := fixTestModel(service, 80, 24)
	model.configStore = store
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	return &model, store, resolved
}

func startTargetScoreSave(t *testing.T, model *Model) tea.Cmd {
	t.Helper()
	model.fixDialog.cursor = fixFieldTargetScore
	_, first := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyLeft})
	if first == nil || !model.runtime.targetScorePreference.saving {
		t.Fatal("first target-score adjustment did not start a preference save")
	}
	return first
}

func queueTargetScoreSave(t *testing.T, model *Model) {
	t.Helper()
	_, queued := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyLeft})
	if queued != nil || model.runtime.targetScorePreference.desired != 80 {
		t.Fatalf("rapid adjustment was not queued behind the active save: desired=%v command=%v", model.runtime.targetScorePreference.desired, queued)
	}
}

func assertTargetScoreConcurrentEdit(t *testing.T, store *appconfig.Memory, resolved appconfig.Resolved) {
	t.Helper()
	external := cloneConfigFix(resolved.Fix)
	external.Effort = "medium"
	if _, err := store.Save(t.Context(), fix.WorkspaceIdentity{}, appconfig.ScopeUser, appconfig.Patch{Fix: &external}, resolved.Revision); err != nil {
		t.Fatal(err)
	}
}

func finishTargetScoreSaves(t *testing.T, model *Model, first tea.Cmd, store *appconfig.Memory) {
	t.Helper()
	next := model.handleFixTargetPreferenceSaved(first().(fixTargetPreferenceSavedMsg))
	if next == nil {
		t.Fatal("queued target score was not saved after the first write")
	}
	if trailing := model.handleFixTargetPreferenceSaved(next().(fixTargetPreferenceSavedMsg)); trailing != nil || model.runtime.targetScorePreference.saving {
		t.Fatal("serialized target-score saves did not settle")
	}
	saved, err := store.Resolve(t.Context(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Fix.TargetScore != 80 || saved.Fix.Effort != "medium" {
		t.Fatalf("global Fix preferences = %+v; target score or concurrent edit was lost", saved.Fix)
	}
}

func TestTargetScoreCanBeReplacedByTyping(t *testing.T) {
	model, store := targetScoreEditorModel(t)
	assertTargetScoreEditorPopup(t, model)
	assertTargetScoreValidation(t, model)
	assertTargetScoreTypedValue(t, model, store)
}

func targetScoreEditorModel(t *testing.T) (*Model, *appconfig.Memory) {
	t.Helper()
	initial := settingsResolved()
	initial.Revision = 1
	initial.Fix.TargetScore = 15
	store := appconfig.NewMemory(initial)
	resolved, err := store.Resolve(t.Context(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	input := readyFixInput("a.go")
	input.TargetScore = 15
	input.Baseline.Contract.Goal.MaximumScore = 15
	input.Preferences = resolved
	service := &fakeFixService{input: input}
	model := fixTestModel(service, 80, 24)
	model.configStore = store
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	model.fixDialog.cursor = fixFieldTargetScore
	_, blink := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if blink == nil {
		t.Fatal("Enter did not open the focused target-score popup")
	}
	return &model, store
}

func assertTargetScoreEditorPopup(t *testing.T, model *Model) {
	t.Helper()
	assertTargetScoreEditingState(t, model)
	assertTargetScoreCompactState(t, model)
	assertTargetScoreMinimumHeight(t, model)
	model.width, model.height = 80, 24
}

func assertTargetScoreEditingState(t *testing.T, model *Model) {
	t.Helper()
	if !model.fixDialog.scoreEditing || !model.fixDialog.score.Focused() || !overlayPresent(model.overlays, OverlayTargetScoreEditor) {
		t.Fatalf("Enter did not open the focused target-score popup: %+v", model.fixDialog)
	}
	if model.fixDialog.score.TextStyle.GetBackground() != style.SurfaceFieldActive {
		t.Fatal("target-score input did not use the global active-field background")
	}
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "TARGET SCORE") || !strings.Contains(view, "Score") || !strings.Contains(view, "Enter apply") {
		t.Fatalf("target-score popup did not make editing clear: %q", view)
	}
}

func assertTargetScoreCompactState(t *testing.T, model *Model) {
	t.Helper()
	model.width, model.height = 36, 8
	assertScreenSize(t, model.View(), 36, 8)
	compact := ansi.Strip(model.View())
	if !strings.Contains(compact, "TARGET SCORE") || !strings.Contains(compact, "Enter apply") {
		t.Fatalf("compact target-score popup lost its edit state: %q", compact)
	}
}

func assertTargetScoreMinimumHeight(t *testing.T, model *Model) {
	t.Helper()
	model.width, model.height = 36, 6
	compact := ansi.Strip(model.View())
	if !strings.Contains(compact, "TARGET SCORE") || !strings.Contains(compact, "Enter apply") || !strings.Contains(compact, "╭") || !strings.Contains(compact, "╰") {
		t.Fatalf("minimum-height target-score popup was clipped: %q", compact)
	}
	assertScreenSize(t, model.View(), 36, 6)
}

func assertTargetScoreValidation(t *testing.T, model *Model) {
	t.Helper()
	model.fixDialog.score.SetValue("invalid")
	handleKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !overlayPresent(model.overlays, OverlayTargetScoreEditor) || !strings.Contains(ansi.Strip(model.View()), "Enter a non-negative number") {
		t.Fatalf("invalid target score was not shown in the popup: %q", ansi.Strip(model.View()))
	}
	model.width, model.height = 36, 6
	errorView := ansi.Strip(model.View())
	if !strings.Contains(errorView, "Enter a non-negative number") || !strings.Contains(errorView, "Enter apply") || !strings.Contains(errorView, "╰") {
		t.Fatalf("minimum-height target-score error was clipped: %q", errorView)
	}
	model.width, model.height = 80, 24
}

func assertTargetScoreTypedValue(t *testing.T, model *Model, store *appconfig.Memory) {
	t.Helper()
	model.width, model.height = 80, 24
	model.fixDialog.score.SetValue("15")
	model.fixDialog.score.CursorEnd()
	handleKey(model, tea.KeyMsg{Type: tea.KeyBackspace})
	handleKey(model, tea.KeyMsg{Type: tea.KeyBackspace})
	handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10")})
	_, save := handleKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if save == nil || model.fixDialog.scoreEditing || model.fixDialog.input.TargetScore != 10 {
		t.Fatalf("typed target score was not applied: %+v", model.fixDialog)
	}
	model.handleFixTargetPreferenceSaved(save().(fixTargetPreferenceSavedMsg))
	saved, err := store.Resolve(t.Context(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Fix.TargetScore != 10 {
		t.Fatalf("typed target score was not saved globally: %v", saved.Fix.TargetScore)
	}
}

func TestFixChoiceFieldsUseSingleAndMultiSelectLists(t *testing.T) {
	model := choiceTestModel(t)
	assertMetricChoice(t, model)
	assertChoiceModelAndEffort(t, model)
	assertChoiceProfileScopePublish(t, model)
}

func choiceTestModel(t *testing.T) *Model {
	t.Helper()
	input := readyFixInput("a.go")
	input.Preferences.Profiles = []agent.Profile{
		{ID: "codex", Label: "Codex", Runtime: "codex-cli"},
		{ID: "openai", Label: "OpenAI API", Runtime: "openai-responses"},
	}
	input.Probe.Capabilities.Models = []agent.Option[agent.ModelID]{{ID: "gpt-5.6"}, {ID: "gpt-5.6-mini", Label: "GPT 5.6 Mini"}}
	input.Probe.Capabilities.Efforts = []agent.Option[agent.EffortID]{{ID: "high"}, {ID: "medium", Label: "Medium"}}
	model := fixTestModel(&fakeFixService{input: input}, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	return &model
}

func assertMetricChoice(t *testing.T, model *Model) {
	t.Helper()
	model.fixDialog.cursor = fixFieldFocus
	closedRows := model.fixDialog.fieldRows(model.profileCatalog, 76)
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	assertMetricDropdownOpened(t, model)
	assertMetricDropdownPreservesForm(t, model, closedRows)
	assertMetricDropdownFooter(t, model)
	assertMetricDropdownToggles(t, model)
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEsc})
}

func assertMetricDropdownOpened(t *testing.T, model *Model) {
	t.Helper()
	plain := ansi.Strip(model.View())
	if !model.fixDialog.choiceOpen || !strings.Contains(plain, "SCORE") || !strings.Contains(plain, "Cognitive complexity") {
		t.Fatalf("metric multi-select did not open: %q", plain)
	}
}

func assertMetricDropdownPreservesForm(t *testing.T, model *Model, closedRows []string) {
	t.Helper()
	openRows := model.fixDialog.fieldRows(model.profileCatalog, 76)
	if len(openRows) != len(closedRows) || strings.Join(openRows, "\n") != strings.Join(closedRows, "\n") {
		t.Fatalf("opening a combo changed the form rows: closed=%q open=%q", ansi.Strip(strings.Join(closedRows, "\n")), ansi.Strip(strings.Join(openRows, "\n")))
	}
	content := ansi.Strip(strings.Join(model.fixDialog.content(model.profileCatalog, 76, 14), "\n"))
	if !strings.Contains(content, "Metrics") || !strings.Contains(content, "Agent") || !strings.Contains(content, "Model") {
		t.Fatalf("dropdown covered the form instead of overlaying its field: %q", content)
	}
}

func assertMetricDropdownFooter(t *testing.T, model *Model) {
	t.Helper()
	if footer := model.fixDialog.footer(); !strings.Contains(footer, "Space toggle") || strings.Contains(footer, "Enter toggle") {
		t.Fatalf("metric footer=%q", footer)
	}
}

func assertMetricDropdownToggles(t *testing.T, model *Model) {
	t.Helper()
	model.width, model.height = 36, 8
	assertScreenSize(t, model.View(), 36, 8)
	model.width, model.height = 80, 24
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyDown})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.fixDialog.focus["cog"] || !model.fixDialog.choiceOpen {
		t.Fatal("Enter did not toggle the metric checkbox")
	}
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeySpace})
	if model.fixDialog.focus["cog"] || !model.fixDialog.choiceOpen {
		t.Fatal("Space did not toggle the metric checkbox")
	}
}

func assertChoiceModelAndEffort(t *testing.T, model *Model) {
	t.Helper()
	model.fixDialog.cursor = fixFieldModel
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRight})
	if model.fixDialog.input.Model != "gpt-5.6" {
		t.Fatal("Model still changed with left/right")
	}
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyDown})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if model.fixDialog.choiceOpen || model.fixDialog.input.Model != "gpt-5.6-mini" {
		t.Fatalf("model single-select did not apply: %+v", model.fixDialog)
	}

	model.fixDialog.cursor = fixFieldEffort
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyDown})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if model.fixDialog.choiceOpen || model.fixDialog.input.Effort != "medium" {
		t.Fatalf("effort single-select did not apply: %+v", model.fixDialog)
	}
}

func assertChoiceProfileScopePublish(t *testing.T, model *Model) {
	t.Helper()
	model.fixDialog.cursor = fixFieldProfile
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyDown})
	_, profileCommand := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if model.fixDialog.choiceOpen || !model.fixDialog.loading || profileCommand == nil {
		t.Fatalf("agent single-select did not start readiness preparation: %+v", model.fixDialog)
	}
	model.fixDialog.loading = false

	model.fixDialog.cursor = fixFieldScope
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyDown})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if model.fixDialog.input.ChangeScope != "repository" {
		t.Fatalf("May edit single-select=%q", model.fixDialog.input.ChangeScope)
	}

	model.fixDialog.cursor = fixFieldPublish
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyDown})
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	if model.fixDialog.input.DeliveryPlan.Publish != fix.PublishPullRequest {
		t.Fatalf("publish single-select=%q", model.fixDialog.input.DeliveryPlan.Publish)
	}
}

func TestFixDropdownUsesTheDialogInteriorInsteadOfOneSideOfTheField(t *testing.T) {
	input := readyFixInput("a.go")
	input.Probe.Capabilities.Models = []agent.Option[agent.ModelID]{
		{ID: "model-1", Label: "Model 1"}, {ID: "model-2", Label: "Model 2"},
		{ID: "model-3", Label: "Model 3"}, {ID: "model-4", Label: "Model 4"},
		{ID: "model-5", Label: "Model 5"}, {ID: "model-6", Label: "Model 6"},
		{ID: "model-7", Label: "Model 7"},
	}
	input.Model = "model-1"
	model := fixTestModel(&fakeFixService{input: input}, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	model.fixDialog.cursor = fixFieldModel
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})

	view := model.View()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "Model 1") || !strings.Contains(plain, "Model 7") {
		t.Fatalf("dropdown did not use the available dialog height: %q", plain)
	}
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "FIX FILE") {
		t.Fatalf("dropdown escaped the Fix dialog: %q", plain)
	}
	assertScreenSize(t, view, 80, 24)
}

func TestCompactFixDropdownUsesDialogWidthAndShowsMoreChoices(t *testing.T) {
	input := readyFixInput("a.go")
	model := fixTestModel(&fakeFixService{input: input}, 36, 6)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	model.fixDialog.cursor = fixFieldScope
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})

	view := model.View()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "Selected files") || !strings.Contains(plain, "relate") || !strings.Contains(plain, "↓") {
		t.Fatalf("compact dropdown remained narrow or hid additional choices: %q", plain)
	}
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "╰") {
		t.Fatalf("compact dropdown escaped an incomplete dialog: %q", plain)
	}
	assertScreenSize(t, view, 36, 6)
}

func TestOneRowDropdownShowsChoicesInBothDirections(t *testing.T) {
	menu := formChoiceMenu([]fixDialogChoice{{label: "First"}, {label: "Middle"}, {label: "Last"}}, 1, false, 30, 3, 1)
	plain := ansi.Strip(strings.Join(menu, "\n"))
	if !strings.Contains(plain, "↕") || !strings.Contains(plain, "Middle") {
		t.Fatalf("one-row dropdown did not show bidirectional continuation: %q", plain)
	}
}

func TestFixMetricChoicesOnlyIncludeMetricsMeasuredForEveryTarget(t *testing.T) {
	input := readyFixInput("a.go")
	input.Baseline.Contract.Targets = append(input.Baseline.Contract.Targets, fix.TargetSnapshot{
		Path: "b.go", Score: 90, Complete: true,
		Metrics: map[fix.MetricID]fix.MetricValue{"cog": {ID: "cog", Complete: true}},
	})
	input.Baseline.Contract.Targets[0].Metrics["npath"] = fix.MetricValue{ID: "npath", Complete: true}
	metrics := availableFixMetrics(input)
	if len(metrics) != 2 || metrics[0] != "score" || metrics[1] != "cog" {
		t.Fatalf("multi-file metric choices=%v, want only SCORE and common COG", metrics)
	}
}

func TestFixPrepareErrorStaysNonBlockingAndActionable(t *testing.T) {
	service := &fakeFixService{prepareErr: errors.New("Codex authentication is missing")}
	model := fixTestModel(service, 60, 16)
	command := model.openFixForSelected()
	model.handleFixLoaded(command().(fixLoadedMsg))
	if model.fixDialog.loading || !strings.Contains(model.fixDialog.errorText, "authentication") || !overlayPresent(model.overlays, OverlayFixForm) {
		t.Fatalf("prepare error state = %+v", model.fixDialog)
	}
	if !strings.Contains(ansi.Strip(model.View()), "authentication") {
		t.Fatal("prepare error was not visible in the dialog")
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if updated.(*Model).fixDialog.errorText == "" {
		t.Fatal("background resize erased the actionable error")
	}
}

func TestBranchEditorDoesNotSilentlyClipLongNames(t *testing.T) {
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))

	longBranch := strings.Repeat("engineering/platform/", 16) + "refactor-parser"
	model.fixDialog.branch.SetValue(longBranch)
	if got := model.fixDialog.branch.Value(); got != longBranch {
		t.Fatalf("branch name was clipped to %d of %d bytes", len(got), len(longBranch))
	}
	if !model.fixDialog.syncInput() {
		t.Fatalf("long editor values were rejected: %s", model.fixDialog.errorText)
	}
	if model.fixDialog.input.BranchName != longBranch {
		t.Fatalf("long branch did not survive input synchronization: %d", len(model.fixDialog.input.BranchName))
	}
}

func TestLiveJobsProjectWhileOverlayOpenAndCancelTargetsStableJob(t *testing.T) {
	model, service := liveJobsModel(t)
	assertLiveJobsProject(t, &model)
	assertLiveJobCancel(t, &model, service)
}

func liveJobsModel(t *testing.T) (Model, *fakeFixService) {
	t.Helper()
	service := &fakeFixService{input: readyFixInput("a.go")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixLoaded(prepare().(fixLoadedMsg))
	jobs := []fix.JobPresentation{
		{ID: "one", Phase: fix.PhaseRunning, AllowedActions: []fix.JobAction{fix.ActionCancel}},
		{ID: "two", Phase: fix.PhaseRunning, AllowedActions: []fix.JobAction{fix.ActionCancel}},
	}
	model.handleFixJobs(fixJobsMsg{jobs: jobs})
	return model, service
}

func assertLiveJobsProject(t *testing.T, model *Model) {
	t.Helper()
	if len(model.agents.Jobs) != 2 || !overlayPresent(model.overlays, OverlayFixForm) {
		t.Fatal("job update did not project behind the open form")
	}
}

func assertLiveJobCancel(t *testing.T, model *Model, service *fakeFixService) {
	t.Helper()
	model.overlays.Pop()
	model.mainView = MainViewAgents
	model.agents.Selected = AgentRowID{JobID: "two"}
	updated, _ := handleKey(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	result := updated.(*Model)
	if !overlayPresent(result.overlays, OverlayConfirmation) || result.runtime.jobActions.confirmation.jobID != "two" {
		t.Fatalf("cancel confirmation captured %+v", result.runtime.jobActions.confirmation)
	}
	updated, command := handleKey(result, tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	if command == nil {
		t.Fatal("cancel confirmation did not issue a command")
	}
	message := command().(fixCommandMsg)
	if service.executed.JobID != "two" || service.executed.Action != fix.ActionCancel {
		t.Fatalf("cancel command targeted %+v", service.executed)
	}
	result.handleFixCommand(message)
	if overlayPresent(result.overlays, OverlayConfirmation) || !strings.Contains(result.fixNotice, "two") {
		t.Fatalf("accepted cancel did not close safely: notice=%q", result.fixNotice)
	}
}

type fakeFixService struct {
	input          fixapp.FixInput
	prepareErr     error
	prepareRequest fixapp.LoadRequest
	runID          fix.JobID
	runErr         error
	ranInput       fixapp.FixInput
	jobs           fixapp.JobListSnapshot
	executed       fix.JobCommand
	executeErr     error
	candidate      candidate.File
	diff           fixapp.DiffPage
	log            fixapp.LogPage
	shutdowns      int
	subscriptions  int
}

func (service *fakeFixService) LoadFix(_ context.Context, request fixapp.LoadRequest) (fixapp.FixInput, error) {
	service.prepareRequest = request
	return service.input, service.prepareErr
}

func (service *fakeFixService) Run(_ context.Context, input fixapp.FixInput) (fix.JobID, error) {
	service.ranInput = input
	return service.runID, service.runErr
}

func (service *fakeFixService) Jobs(fixapp.JobFilter) fixapp.JobListSnapshot { return service.jobs }
func (service *fakeFixService) Job(id fix.JobID) (fix.JobPresentation, bool) {
	for _, job := range service.jobs.Jobs {
		if job.ID == id {
			return job, true
		}
	}
	return fix.JobPresentation{}, false
}
func (service *fakeFixService) Subscribe() fixapp.Subscription {
	service.subscriptions++
	return fakeFixSubscription{}
}

func (service *fakeFixService) CandidateFile(context.Context, fix.JobID, fix.RepoPath) (candidate.File, error) {
	return service.candidate, nil
}

func (service *fakeFixService) Diff(context.Context, fix.JobID, fixapp.DiffRequest) (fixapp.DiffPage, error) {
	return service.diff, nil
}

func (service *fakeFixService) Transcript(_ context.Context, _ fix.JobID, cursor fixapp.LogCursor, limit int) (fixapp.LogPage, error) {
	start := min(max(0, int(cursor)), len(service.log.Entries))
	if limit <= 0 {
		limit = len(service.log.Entries)
	}
	end := min(len(service.log.Entries), start+limit)
	return fixapp.LogPage{Entries: append([]fixapp.LogEntry(nil), service.log.Entries[start:end]...), Next: fixapp.LogCursor(end), Complete: end == len(service.log.Entries)}, nil
}

func (service *fakeFixService) Shutdown(context.Context) error { service.shutdowns++; return nil }

func (service *fakeFixService) Reconfigure(context.Context, fixapp.RuntimeLimits) error { return nil }

func (service *fakeFixService) Execute(_ context.Context, command fix.JobCommand) (fixapp.CommandReceipt, error) {
	service.executed = command
	return fixapp.CommandReceipt{RequestID: command.RequestID, JobID: command.JobID, Accepted: service.executeErr == nil}, service.executeErr
}

type fakeFixSubscription struct{}

func (fakeFixSubscription) Wait(context.Context) error { return errors.New("closed") }
func (fakeFixSubscription) Close() error               { return nil }

func fixTestModel(service FixService, width, height int) Model {
	file := testFile("a.go", 120)
	agentFindInput := textinput.New()
	agentFindInput.Prompt = "/ "
	return Model{
		files: FilesState{
			Selected: file.Path,
			Document: report.Document{Files: []report.File{file}},
			Rows:     map[string]rowState{file.Path: {}},
			Visible:  defaultColumnVisibility(),
		},
		width:        width,
		height:       height,
		mainView:     MainViewFiles,
		options:      Options{Workspace: "/repo"},
		fixService:   service,
		fixWorkspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"},
		agents:       AgentsState{Expanded: map[fix.JobID]bool{}, FindInput: agentFindInput},
	}
}

func readyFixInput(path fix.RepoPath) fixapp.FixInput {
	return fixapp.FixInput{
		Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", GitCommonDir: "/repo/.git", BaseCommit: "abc", CurrentBranch: "main"},
		Targets:   []fix.RepoPath{path},
		Baseline: fixanalysis.BaselineSnapshot{Contract: fix.ScoringContract{
			Goal: fix.ScoringGoal{MaximumScore: 100},
			Targets: []fix.TargetSnapshot{{Path: path, Score: 120, Complete: true, Metrics: map[fix.MetricID]fix.MetricValue{
				"cog": {ID: "cog", Label: "COG", Value: 12, Complete: true},
			}}},
		}},
		Preferences: appconfig.Resolved{
			Profiles: []agent.Profile{{ID: "codex", Label: "Codex"}},
			Fix:      appconfig.FixDefaults{},
		},
		Profile: agent.Profile{ID: "codex", Label: "Codex"},
		Probe: agent.ProbeResult{State: agent.ProbeReady, Capabilities: agent.Capabilities{
			Models: []agent.Option[agent.ModelID]{{ID: "gpt-5.6"}}, Efforts: []agent.Option[agent.EffortID]{{ID: "high"}},
			Isolation: agent.RuntimeIsolation{Writes: agent.CandidateTreeAndGitMetadataProtected, SensitiveReadsDenied: true, TransportAuthIsolated: true, CrashContainment: true},
		}},
		Model: "gpt-5.6", Effort: "high",
		TargetScore: 100,
		ChangeScope: "targets-and-tests", DeliveryPlan: fix.DeliveryPlan{Workspace: fix.WorkspaceWorktree, Git: fix.GitCommitNewBranch, Publish: fix.PublishPush}, BranchName: "slopwatch/fix/a",
		AllowedPaths: []fix.RepoPath{path},
		Instructions: agent.InstructionDocument{Version: "test", Envelope: "locked", Objective: "old", NextAttemptNotes: "baseline"},
		PlannedPaths: []fix.RepoPath{path},
	}
}

func TestFixMetricsAlwaysStartWithScore(t *testing.T) {
	metrics := availableFixMetrics(readyFixInput("a.go"))
	if len(metrics) == 0 || metrics[0] != "score" {
		t.Fatalf("Fix metrics = %v", metrics)
	}
}

func TestFixModelShowsProviderDefault(t *testing.T) {
	if got := agentOptionLabel([]agent.Option[agent.ModelID](nil), agent.ModelID("")); got != "Default" {
		t.Fatalf("empty provider-default model label = %q", got)
	}
}
