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
)

func TestFixKeyWithoutServiceExplainsUnavailable(t *testing.T) {
	model := Model{selected: "a.go"}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	result := updated.(*Model)
	if command != nil || !strings.Contains(result.status, "Fix unavailable") || result.overlays.Len() != 0 {
		t.Fatalf("nil service behavior: status=%q overlays=%d command=%v", result.status, result.overlays.Len(), command)
	}
}

func TestFilesFooterAdvertisesFixAndAgentsAtUsableSizes(t *testing.T) {
	footer := ansi.Strip((Model{width: 80}).footer())
	if !strings.Contains(footer, "fix") || !strings.Contains(footer, "agents") {
		t.Fatalf("Files footer omitted the new workflow: %q", footer)
	}
}

func TestFixKeyOpensExistingReservationInsteadOfPreparingDuplicate(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 80, 24)
	model.agents.Jobs = []fix.JobPresentation{{
		ID: "existing", Phase: fix.PhaseFailed, Attention: fix.AttentionError,
		Targets: []fix.FilePresentation{{Path: "a.go"}},
	}}
	if command := model.openFixForSelected(); command != nil {
		t.Fatal("reserved target started a duplicate Prepare")
	}
	if model.mainView != MainViewAgents || model.agents.Selected.JobID != "existing" || model.hasOverlay(OverlayFixForm) {
		t.Fatalf("existing reservation was not opened: view=%d selected=%+v", model.mainView, model.agents.Selected)
	}
}

func TestFixPrepareUsesGenerationAndOwnsKeys(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 80, 24)
	command := model.openFixForSelected()
	if command == nil || model.overlays.Len() != 1 || !model.fixDialog.loading {
		t.Fatal("x did not open a loading Fix dialog")
	}
	oldGeneration := model.fixDialog.generation
	model.fixDialog.generation++
	model.handleFixPrepared(command().(fixPreparedMsg))
	if model.fixDialog.hasDraft {
		t.Fatal("late prepare result from stale generation was applied")
	}
	model.fixDialog.generation = oldGeneration
	model.handleFixPrepared(command().(fixPreparedMsg))
	if !model.fixDialog.hasDraft || model.fixDialog.loading {
		t.Fatal("current prepare result was not applied")
	}

	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if updated.(*Model).mainView != MainViewFiles {
		t.Fatal("Tab escaped the Fix dialog")
	}
}

func TestFixDialogResponsiveSurfacesAndContractSafeSubmit(t *testing.T) {
	for _, size := range []struct{ width, height int }{{36, 6}, {36, 8}, {60, 16}, {80, 24}, {120, 30}} {
		service := &fakeFixService{draft: readyFixDraft("a.go")}
		model := fixTestModel(service, size.width, size.height)
		command := model.openFixForSelected()
		model.handleFixPrepared(command().(fixPreparedMsg))
		assertScreenSize(t, model.View(), size.width, size.height)
		plain := ansi.Strip(model.View())
		if !strings.Contains(plain, "FIX FILE") || !strings.Contains(plain, "Target score") {
			t.Fatalf("%dx%d Fix surface omitted controls: %q", size.width, size.height, plain)
		}
	}

	service := &fakeFixService{draft: readyFixDraft("a.go"), submitID: "job-new"}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	model.fixDialog.cursor = fixFieldTargetScore
	model.adjustFixField(-1)
	model.fixDialog.focus["cog"] = true
	_, submit := model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if submit == nil {
		t.Fatalf("Run fix did not submit: %s", model.fixDialog.errorText)
	}
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !model.hasOverlay(OverlayFixForm) {
		t.Fatal("Esc hid a submission before its admission result was known")
	}
	message := submit().(fixSubmittedMsg)
	if message.err != nil || service.submitted.TargetScore != 90 || service.submitted.Baseline.Contract.Goal.MaximumScore != 90 {
		t.Fatalf("submitted score contract split: draft=%+v message=%+v", service.submitted, message)
	}
	if len(service.submitted.Baseline.Contract.Goal.Focus) != 1 || !strings.Contains(service.submitted.Instructions.Objective, "score of 90 or lower") ||
		!strings.Contains(service.submitted.Instructions.EffectiveBody(), "handle Git") {
		t.Fatalf("submitted prompt/goal not synchronized: goal=%+v instructions=%+v", service.submitted.Baseline.Contract.Goal, service.submitted.Instructions)
	}
	model.handleFixSubmitted(message)
	if model.mainView != MainViewAgents || model.agents.Selected.JobID != "job-new" || model.hasOverlay(OverlayFixForm) {
		t.Fatalf("successful admission did not transition to job: view=%d selected=%+v overlays=%d", model.mainView, model.agents.Selected, model.overlays.Len())
	}
}

func TestFixDialogUsesTheEssentialAgentNameOnly(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.Profile.Label = "Codex — managed sign-in (ChatGPT recommended)"
	draft.Probe.Authentication = agent.Authentication{Method: "api-key", Label: "Signed in with an API key"}
	model := Model{fixDialog: fixDialogState{hasDraft: true, draft: draft}}

	text := ansi.Strip(strings.Join(model.fixFieldRows(120), "\n"))
	if !strings.Contains(text, "Agent           Codex") || strings.Contains(text, "ChatGPT recommended") || strings.Contains(text, "Signed in") {
		t.Fatalf("Fix dialog did not reduce the agent profile to its essential name: %q", text)
	}
}

func TestTargetScoreAdjustmentsSerializeIntoGlobalPreferences(t *testing.T) {
	initial := settingsResolved()
	initial.Revision = 1
	initial.Fix.TargetScore = 100
	initial.Fix.Effort = "high"
	store := appconfig.NewMemory(initial)
	resolved, err := store.Resolve(t.Context(), fix.WorkspaceIdentity{}, appconfig.SessionOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	draft := readyFixDraft("a.go")
	draft.Preferences = resolved
	service := &fakeFixService{draft: draft}
	model := fixTestModel(service, 80, 24)
	model.configStore = store
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	model.fixDialog.cursor = fixFieldTargetScore
	_, first := model.adjustFixField(-1)
	if first == nil || !model.fixTargetSaving {
		t.Fatal("first target-score adjustment did not start a preference save")
	}
	_, queued := model.adjustFixField(-1)
	if queued != nil || model.fixTargetDesired != 80 {
		t.Fatalf("rapid adjustment was not queued behind the active save: desired=%v command=%v", model.fixTargetDesired, queued)
	}

	external := cloneConfigFix(resolved.Fix)
	external.Effort = "medium"
	if _, err := store.Save(t.Context(), fix.WorkspaceIdentity{}, appconfig.ScopeUser, appconfig.Patch{Fix: &external}, resolved.Revision); err != nil {
		t.Fatal(err)
	}
	next := model.handleFixTargetPreferenceSaved(first().(fixTargetPreferenceSavedMsg))
	if next == nil {
		t.Fatal("queued target score was not saved after the first write")
	}
	if trailing := model.handleFixTargetPreferenceSaved(next().(fixTargetPreferenceSavedMsg)); trailing != nil || model.fixTargetSaving {
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

func TestFixPrepareErrorStaysNonBlockingAndActionable(t *testing.T) {
	service := &fakeFixService{prepareErr: errors.New("Codex authentication is missing")}
	model := fixTestModel(service, 60, 16)
	command := model.openFixForSelected()
	model.handleFixPrepared(command().(fixPreparedMsg))
	if model.fixDialog.loading || !strings.Contains(model.fixDialog.errorText, "authentication") || !model.hasOverlay(OverlayFixForm) {
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
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))

	longBranch := strings.Repeat("engineering/platform/", 16) + "refactor-parser"
	model.fixDialog.branch.SetValue(longBranch)
	if got := model.fixDialog.branch.Value(); got != longBranch {
		t.Fatalf("branch name was clipped to %d of %d bytes", len(got), len(longBranch))
	}
	if !model.syncFixDraft() {
		t.Fatalf("long editor values were rejected: %s", model.fixDialog.errorText)
	}
	if model.fixDialog.draft.BranchName != longBranch {
		t.Fatalf("long branch did not survive draft synchronization: %d", len(model.fixDialog.draft.BranchName))
	}
}

func TestLiveJobsProjectWhileOverlayOpenAndCancelTargetsStableJob(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	jobs := []fix.JobPresentation{
		{ID: "one", Revision: 4, Phase: fix.PhaseRunning, AllowedActions: []fix.JobAction{fix.ActionCancel}},
		{ID: "two", Revision: 9, Phase: fix.PhaseRunning, AllowedActions: []fix.JobAction{fix.ActionCancel}},
	}
	model.handleFixJobs(fixJobsMsg{revision: 3, jobs: jobs})
	if len(model.agents.Jobs) != 2 || !model.hasOverlay(OverlayFixForm) {
		t.Fatal("job update did not project behind the open form")
	}
	model.overlays.Pop()
	model.mainView = MainViewAgents
	model.agents.Selected = AgentRowID{JobID: "two"}
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	result := updated.(*Model)
	if !result.hasOverlay(OverlayConfirmation) || result.cancelConfirmation.jobID != "two" || result.cancelConfirmation.revision != 9 {
		t.Fatalf("cancel confirmation captured %+v", result.cancelConfirmation)
	}
	updated, command := result.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	if command == nil {
		t.Fatal("cancel confirmation did not issue a command")
	}
	message := command().(fixCommandMsg)
	if service.executed.JobID != "two" || service.executed.ExpectedRevision != 9 || service.executed.Action != fix.ActionCancel {
		t.Fatalf("cancel command targeted %+v", service.executed)
	}
	result.handleFixCommand(message)
	if result.hasOverlay(OverlayConfirmation) || !strings.Contains(result.fixNotice, "two") {
		t.Fatalf("accepted cancel did not close safely: notice=%q", result.fixNotice)
	}
}

type fakeFixService struct {
	draft          fixapp.FixDraft
	prepareErr     error
	prepareRequest fixapp.PrepareRequest
	submitID       fix.JobID
	submitErr      error
	submitted      fixapp.FixDraft
	jobs           fixapp.JobListSnapshot
	executed       fix.JobCommand
	executeErr     error
	candidate      candidate.File
	diff           fixapp.DiffPage
	log            fixapp.LogPage
	shutdowns      int
	subscriptions  int
}

func (service *fakeFixService) Prepare(_ context.Context, request fixapp.PrepareRequest) (fixapp.FixDraft, error) {
	service.prepareRequest = request
	return service.draft, service.prepareErr
}

func (service *fakeFixService) Submit(_ context.Context, request fixapp.SubmitRequest) (fix.JobID, error) {
	service.submitted = request.Draft
	return service.submitID, service.submitErr
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

func (service *fakeFixService) Transcript(context.Context, fix.JobID, fixapp.LogCursor, int) (fixapp.LogPage, error) {
	return service.log, nil
}

func (service *fakeFixService) Shutdown(context.Context) error { service.shutdowns++; return nil }

func (service *fakeFixService) Reconfigure(context.Context, fixapp.RuntimeLimits) error { return nil }

func (service *fakeFixService) Execute(_ context.Context, command fix.JobCommand) (fixapp.CommandReceipt, error) {
	service.executed = command
	return fixapp.CommandReceipt{RequestID: command.RequestID, JobID: command.JobID, Accepted: service.executeErr == nil}, service.executeErr
}

type fakeFixSubscription struct{}

func (fakeFixSubscription) Wait(context.Context, fixapp.GlobalRevision) (fixapp.GlobalRevision, error) {
	return 0, errors.New("closed")
}
func (fakeFixSubscription) Close() error { return nil }

func fixTestModel(service FixService, width, height int) Model {
	file := testFile("a.go", 120)
	agentFindInput := textinput.New()
	agentFindInput.Prompt = "/ "
	return Model{
		width: width, height: height, mainView: MainViewFiles, selected: file.Path,
		document: report.Document{Files: []report.File{file}}, rows: map[string]rowState{file.Path: {}}, visible: defaultColumnVisibility(),
		options: Options{Workspace: "/repo"}, fixService: service,
		fixWorkspace:   fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"},
		agents:         AgentsState{Expanded: map[fix.JobID]bool{}},
		agentFindInput: agentFindInput,
	}
}

func readyFixDraft(path fix.RepoPath) fixapp.FixDraft {
	return fixapp.FixDraft{
		ID: "draft-1", Revision: 1,
		Workspace: fix.WorkspaceIdentity{Repository: "repo", RepositoryRoot: "/repo", AnalysisRoot: "/repo", BaseCommit: "abc"},
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
			Delegation: []agent.Option[agent.DelegationMode]{{ID: agent.DelegationSingle}},
			Isolation:  agent.RuntimeIsolation{Writes: agent.CandidateTreeAndGitMetadataProtected, SensitiveReadsDenied: true, TransportAuthIsolated: true, CrashContainment: true},
		}},
		Model: "gpt-5.6", Effort: "high", Delegation: agent.DelegationSingle,
		TargetScore: 100,
		ChangeScope: "targets-and-tests", DeliveryMode: "branch", BranchName: "slopwatch/fix/a",
		AllowedPaths: []fix.RepoPath{path},
		Instructions: agent.InstructionDocument{Version: "test", Envelope: "locked", Objective: "old", NextAttemptNotes: "baseline"},
		Preflight:    candidate.PreflightResult{Ready: true, Supported: true, AllowedPaths: []fix.RepoPath{path}},
	}
}

func TestFixMetricsAlwaysStartWithScore(t *testing.T) {
	metrics := availableFixMetrics(readyFixDraft("a.go"))
	if len(metrics) == 0 || metrics[0] != "score" {
		t.Fatalf("Fix metrics = %v", metrics)
	}
}
