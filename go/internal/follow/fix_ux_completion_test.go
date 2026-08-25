package follow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopwatch/internal/candidate"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
)

func TestJobMonitorAndReadersLoadThroughService(t *testing.T) {
	job := fix.JobPresentation{ID: "job-1", Revision: 2, Phase: fix.PhaseRunning, Goal: "SCORE <= 100", CurrentAction: "Running tests", AttemptOrdinal: 1,
		Actors: []fix.ActorPresentation{{ID: "primary", CurrentAction: "Editing a.go"}, {ID: "reviewer", ParentID: "primary", CurrentAction: "Reviewing"}},
		Usage:  fix.UsagePresentation{InputTokens: 100, CachedTokens: 25, OutputTokens: 30, ReasoningTokens: 10}, UsageReported: true,
		Targets: []fix.FilePresentation{{Path: "a.go", BaselineScore: 120}}}
	service := &fakeFixService{
		jobs:      fixapp.JobListSnapshot{Revision: 2, Jobs: []fix.JobPresentation{job}},
		log:       fixapp.LogPage{Entries: []fixapp.LogEntry{{At: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), ActorID: "primary", Summary: "Read a.go"}}, Complete: true},
		diff:      fixapp.DiffPage{Files: []candidate.DiffFile{{Path: "a.go", Status: "modified", Additions: 3, Deletions: 2}}, Complete: true},
		candidate: candidate.File{Path: "a.go", Contents: []byte("package sample\n\x1b[31munsafe\x1b[0m\n")},
	}
	model := fixTestModel(service, 80, 24)
	model.mainView = MainViewAgents
	model.agents.Jobs = []fix.JobPresentation{job}
	model.agents.Selected = AgentRowID{JobID: job.ID}

	command := model.openJobMonitor(job.ID, "a.go")
	if command == nil || !model.hasOverlay(OverlayJobMonitor) {
		t.Fatal("monitor did not open asynchronously")
	}
	model.handleJobMonitor(command().(jobMonitorMsg))
	monitor := ansi.Strip(model.View())
	for _, want := range []string{"RUNNING", "Running tests", "Read a.go", "Focused file: a.go", "attempt 1", "ACTORS", "primary", "reviewer", "Usage: in 100", "cached 25"} {
		if !strings.Contains(monitor, want) {
			t.Fatalf("monitor missing %q: %q", want, monitor)
		}
	}
	service.log.Entries = append(service.log.Entries, fixapp.LogEntry{At: time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC), Summary: "Edited a.go"})
	job.Revision = 3
	job.CurrentAction = "Editing a.go"
	service.jobs = fixapp.JobListSnapshot{Revision: 3, Jobs: []fix.JobPresentation{job}}
	command = model.handleFixJobs(fixJobsMsg{revision: 3, jobs: []fix.JobPresentation{job}})
	if command == nil {
		t.Fatal("open monitor did not schedule activity refresh on subscription advance")
	}
	model.handleJobMonitor(command().(jobMonitorMsg))
	if text := ansi.Strip(model.View()); !strings.Contains(text, "Editing a.go") || !strings.Contains(text, "Edited a.go") {
		t.Fatalf("monitor activity stayed frozen after subscription advance: %q", text)
	}

	for _, test := range []struct {
		kind OverlayKind
		open func() tea.Cmd
		want string
	}{
		{OverlayJobLog, func() tea.Cmd { return model.openJobLog(job.ID) }, "Read a.go"},
		{OverlayJobDiff, func() tea.Cmd { return model.openJobDiff(job.ID, "a.go") }, "modified"},
		{OverlayCandidateSource, func() tea.Cmd { return model.openCandidateSource(job.ID, "a.go") }, "package sample"},
	} {
		command = test.open()
		if command == nil || !model.hasOverlay(test.kind) {
			t.Fatalf("reader %d did not open", test.kind)
		}
		model.handleJobReader(command().(jobReaderMsg))
		if rendered := model.View(); strings.Contains(rendered, "\x1b[31munsafe") || !strings.Contains(ansi.Strip(rendered), test.want) {
			t.Fatalf("reader %d unsafe or missing %q: %q", test.kind, test.want, ansi.Strip(rendered))
		}
		model.overlays.Pop()
	}
}

func TestJobReadersConsumeEveryServicePageAndExplainConfiguredTruncation(t *testing.T) {
	service := &pagedReaderFixService{
		fakeFixService: &fakeFixService{},
		logs: map[fixapp.LogCursor]fixapp.LogPage{
			0: {Entries: []fixapp.LogEntry{{Summary: "first activity"}}, Next: 1},
			1: {Entries: []fixapp.LogEntry{{Summary: "second activity"}}, Next: 2, Complete: true},
		},
		diffs: map[int]fixapp.DiffPage{
			0: {Files: []candidate.DiffFile{{Path: "first.go", Status: "modified"}}, NextOffset: 1},
			1: {Files: []candidate.DiffFile{{Path: "second.go", Status: "added"}}, Offset: 1, NextOffset: 2, Complete: true},
		},
	}
	model := fixTestModel(service, 80, 24)
	logMessage := model.openJobLog("job-pages")().(jobReaderMsg)
	logText := strings.Join(logMessage.lines, "\n")
	if logMessage.err != nil || logMessage.truncated || !strings.Contains(logText, "first activity") || !strings.Contains(logText, "second activity") {
		t.Fatalf("paged log=%+v", logMessage)
	}
	if got := service.logCursors; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("log cursors=%v", got)
	}
	model.overlays.Pop()
	diffMessage := model.openJobDiff("job-pages", "")().(jobReaderMsg)
	if diffMessage.err != nil || diffMessage.truncated || !strings.Contains(strings.Join(diffMessage.lines, "\n"), "first.go") || !strings.Contains(strings.Join(diffMessage.lines, "\n"), "second.go") {
		t.Fatalf("paged diff=%+v", diffMessage)
	}
	if got := service.diffOffsets; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("diff offsets=%v", got)
	}

	model.jobReader = jobReaderState{kind: OverlayCandidateSource, jobID: "job-pages", lines: []string{"partial"}, truncated: true}
	if text := ansi.Strip(strings.Join(model.jobReaderContent(80, 5), "\n")); !strings.Contains(text, "configured candidate byte/line limit") || strings.Contains(text, "retained transcript") {
		t.Fatalf("candidate truncation label=%q", text)
	}
	model.jobReader.kind = OverlayJobLog
	if text := ansi.Strip(strings.Join(model.jobReaderContent(80, 5), "\n")); !strings.Contains(text, "configured transcript retention limit") {
		t.Fatalf("log truncation label=%q", text)
	}
}

type pagedReaderFixService struct {
	*fakeFixService
	logs        map[fixapp.LogCursor]fixapp.LogPage
	diffs       map[int]fixapp.DiffPage
	logCursors  []fixapp.LogCursor
	diffOffsets []int
}

func (service *pagedReaderFixService) Transcript(_ context.Context, _ fix.JobID, cursor fixapp.LogCursor, _ int) (fixapp.LogPage, error) {
	service.logCursors = append(service.logCursors, cursor)
	return service.logs[cursor], nil
}

func (service *pagedReaderFixService) Diff(_ context.Context, _ fix.JobID, request fixapp.DiffRequest) (fixapp.DiffPage, error) {
	service.diffOffsets = append(service.diffOffsets, request.Offset)
	return service.diffs[request.Offset], nil
}

func TestJobMonitorCoalescesSubscriptionBurstsAndRejectsLateRefresh(t *testing.T) {
	job := fix.JobPresentation{ID: "job-1", Revision: 1, Phase: fix.PhaseRunning, CurrentAction: "starting"}
	service := &fakeFixService{
		jobs: fixapp.JobListSnapshot{Revision: 1, Jobs: []fix.JobPresentation{job}},
		log:  fixapp.LogPage{Entries: []fixapp.LogEntry{{Summary: "first"}}, Complete: true},
	}
	model := fixTestModel(service, 80, 24)
	model.mainView = MainViewAgents
	model.agents.Jobs = []fix.JobPresentation{job}
	initial := model.openJobMonitor(job.ID, "")

	job.Revision = 2
	job.CurrentAction = "second"
	service.jobs = fixapp.JobListSnapshot{Revision: 2, Jobs: []fix.JobPresentation{job}}
	if command := model.handleFixJobs(fixJobsMsg{revision: 2, jobs: []fix.JobPresentation{job}}); command != nil {
		t.Fatal("subscription burst started a second refresh while the initial monitor load was in flight")
	}
	job.Revision = 3
	job.CurrentAction = "third"
	service.jobs = fixapp.JobListSnapshot{Revision: 3, Jobs: []fix.JobPresentation{job}}
	if command := model.handleFixJobs(fixJobsMsg{revision: 3, jobs: []fix.JobPresentation{job}}); command != nil {
		t.Fatal("subscription burst was not coalesced")
	}

	service.log = fixapp.LogPage{Entries: []fixapp.LogEntry{{Summary: "latest"}}, Complete: true}
	queued := model.handleJobMonitor(initial().(jobMonitorMsg))
	if queued == nil || !model.jobMonitor.refreshing || model.jobMonitor.pending {
		t.Fatal("completion did not launch exactly one coalesced refresh")
	}
	latestGeneration := model.jobMonitor.generation
	model.handleJobMonitor(jobMonitorMsg{generation: latestGeneration - 1, job: fix.JobPresentation{ID: job.ID, CurrentAction: "late"}, found: true})
	if model.jobMonitor.job.CurrentAction == "late" {
		t.Fatal("late monitor response replaced the newer requested snapshot")
	}
	model.handleJobMonitor(queued().(jobMonitorMsg))
	if model.jobMonitor.job.CurrentAction != "third" || len(model.jobMonitor.activity) != 1 || model.jobMonitor.activity[0].Summary != "latest" {
		t.Fatalf("coalesced refresh did not load latest state: %#v %#v", model.jobMonitor.job, model.jobMonitor.activity)
	}
}

func TestQuitWithActiveJobsConfirmsAndJoinsService(t *testing.T) {
	service := &fakeFixService{}
	model := fixTestModel(service, 80, 24)
	model.agents.Jobs = []fix.JobPresentation{{ID: "job-running", Phase: fix.PhaseRunning}}
	updated, command := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	result := updated.(*Model)
	if command != nil || !result.hasOverlay(OverlayShutdown) || result.shutdown.active != 1 {
		t.Fatal("active-job quit bypassed confirmation")
	}
	updated, command = result.handleShutdownKey(tea.KeyMsg{Type: tea.KeyEnter})
	result = updated.(*Model)
	if command == nil || !result.shutdown.pending {
		t.Fatal("shutdown did not start a joined command")
	}
	message := command().(shutdownCompleteMsg)
	if service.shutdowns != 1 || message.err != nil {
		t.Fatalf("shutdown calls=%d err=%v", service.shutdowns, message.err)
	}
	if quit := result.handleShutdownComplete(message); quit == nil {
		t.Fatal("successful joined shutdown did not quit")
	}
}

func TestFilesShowAggregateAndPerFileFixCode(t *testing.T) {
	model := fixTestModel(&fakeFixService{}, 100, 12)
	model.agents.Jobs = []fix.JobPresentation{{ID: "job-1", Phase: fix.PhaseRunning, Targets: []fix.FilePresentation{{Path: "a.go"}}}}
	text := ansi.Strip(model.View())
	if !strings.Contains(text, "FIX 1 · 1 RUNNING") || !strings.Contains(text, "FIX") || !strings.Contains(text, "RUN") {
		t.Fatalf("Files omitted fix observability: %q", text)
	}
}

func TestAdvancedEditorProtectsDirtyTextAndConfirmsDetach(t *testing.T) {
	service := &fakeFixService{draft: readyFixDraft("a.go")}
	model := fixTestModel(service, 80, 24)
	prepare := model.openFixForSelected()
	model.handleFixPrepared(prepare().(fixPreparedMsg))
	model.fixDialog.cursor = fixFieldAdvanced
	model.handleFixFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	model.fixDialog.prompt.SetValue(model.fixDialog.promptOriginal + "\nchange this")
	model.handlePromptEditorKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !model.hasOverlay(OverlayPromptDirty) {
		t.Fatal("dirty advanced editor discarded without choice")
	}
	model.fixDialog.promptDirtyCursor = 0
	model.handlePromptDirtyKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.hasOverlay(OverlayPromptDetach) {
		t.Fatal("first advanced save did not confirm detachment")
	}
	model.handlePromptDetachKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.fixDialog.detached || model.hasOverlay(OverlayPromptEditor) {
		t.Fatal("confirmed detach was not applied and closed")
	}
}

func TestCanceledJobUsesCanceledText(t *testing.T) {
	job := fix.JobPresentation{Phase: fix.PhaseAwaitingAction, Issue: &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}}
	if got := agentPhaseText(job); got != "CANCELED" {
		t.Fatalf("phase text=%q", got)
	}
}

func TestFixSubscriptionErrorIsRecoverable(t *testing.T) {
	service := &fakeFixService{jobs: fixapp.JobListSnapshot{Revision: 9, Jobs: []fix.JobPresentation{{ID: "job-9", Phase: fix.PhaseRunning}}}}
	model := fixTestModel(service, 80, 24)
	if command := model.handleFixJobs(fixJobsMsg{err: errors.New("wake failed")}); command == nil || !model.fixUpdatesStale {
		t.Fatal("subscription error did not enter recoverable stale state")
	}
	generation := model.fixRetryGeneration
	command := model.retryFixSubscription(fixRetrySubscriptionMsg{generation: generation})
	if command == nil || model.fixUpdatesStale || model.fixRevision != 9 || service.subscriptions != 1 {
		t.Fatalf("subscription recovery failed: stale=%t revision=%d subscriptions=%d", model.fixUpdatesStale, model.fixRevision, service.subscriptions)
	}
}

func TestFixValidationExplainsMissingBranch(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.DeliveryMode = "pull-request"
	draft.BranchName = ""
	if got := fixPreflightSummary(draft); !strings.Contains(got, "branch name") {
		t.Fatalf("diagnostic=%q", got)
	}
}

func TestPullRequestFormRequiresReadyValidationPlan(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.DeliveryMode = "pull-request"
	draft.BranchName = "slopwatch/fix/a"
	draft.ValidationPlanID = ""
	draft.Preferences.Delivery.RequireValidation = true
	if fixDraftRunnable(draft) {
		t.Fatal("pull-request draft was runnable without a validation plan")
	}
	if got := fixPreflightSummary(draft); !strings.Contains(got, "configured to require validation") {
		t.Fatalf("diagnostic=%q", got)
	}
}

func TestPullRequestFormAllowsOptionalValidation(t *testing.T) {
	draft := readyFixDraft("a.go")
	draft.DeliveryMode = "pull-request"
	draft.BranchName = "slopwatch/fix/a"
	draft.ValidationPlanID = ""
	draft.Preferences.Delivery.RequireValidation = false
	if !fixDraftRunnable(draft) {
		t.Fatalf("pull-request draft was blocked despite optional validation: %s", fixPreflightSummary(draft))
	}
}

func TestAgentFindConsumesPrintableKeysAndSortCycles(t *testing.T) {
	model := fixTestModel(&fakeFixService{}, 80, 24)
	model.mainView = MainViewAgents
	model.agents.Jobs = []fix.JobPresentation{{ID: "one", ProfileLabel: "Codex", Phase: fix.PhaseRunning}, {ID: "two", ProfileLabel: "Claude", Phase: fix.PhaseQueued}}
	model.beginAgentFind()
	updated, _ := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	result := updated.(*Model)
	if !result.agents.FindEditing || result.agentFindInput.Value() != "q" {
		t.Fatal("printable q escaped Agents find")
	}
	result.handleAgentFindKey(tea.KeyMsg{Type: tea.KeyEsc})
	before := result.agents.SortKey
	result.cycleAgentSort(1)
	if result.agents.SortKey == before {
		t.Fatal("Agents sort did not cycle")
	}
}

func TestHelpDocumentsFixAndAgentsWorkflow(t *testing.T) {
	text := ""
	for _, entry := range mainScreenHelp {
		text += entry.label + " " + entry.description + "\n"
	}
	for _, want := range []string{"Tab switches Files/Agents", "x opens Fix", "cancel-all", "diamond", "Space opens its actions", "v opens candidate source"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q", want)
		}
	}
}

func TestFeatureSettingsProtectDirtyExitAndUseCompactFullScreen(t *testing.T) {
	model := settingsModel(configConcurrency, settingsResolved(), &settingsConfigStore{resolved: settingsResolved()})
	model.width, model.height = 40, 10
	model.settings = false
	model.configSettings.dirty = true
	model.closeConfigSettings()
	if !model.hasOverlay(OverlaySettingsDirty) || !model.configSettings.open {
		t.Fatal("dirty settings closed without confirmation")
	}
	model.configSettings.dirtyCursor = 1
	model.handleSettingsDirtyKey(tea.KeyMsg{Type: tea.KeyEnter})
	if model.configSettings.open || !model.settings {
		t.Fatal("discard did not return to Settings")
	}

	model = settingsModel(configConcurrency, settingsResolved(), &settingsConfigStore{})
	model.width, model.height = 40, 10
	assertScreenSize(t, model.configSettingsFullScreen(), 40, 10)
	if text := ansi.Strip(model.configSettingsFullScreen()); !strings.Contains(text, "CONCURRENCY & RETENTION") || !strings.Contains(text, "Esc") {
		t.Fatalf("compact settings omitted fixed chrome: %q", text)
	}
}

func TestMonitorReaderAndDirtyOverlaysRemainOperableAtResponsiveSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{36, 8}, {60, 16}, {80, 24}, {120, 30}} {
		for _, setup := range []func(*Model){
			func(model *Model) {
				model.jobMonitor = jobMonitorState{jobID: "job-1", job: fix.JobPresentation{ID: "job-1", Phase: fix.PhaseRunning}, activity: []fixapp.LogEntry{{Summary: "working"}}}
				model.overlays.Push(OverlayJobMonitor, OverlayCaller{MainView: MainViewAgents})
			},
			func(model *Model) {
				model.jobReader = jobReaderState{kind: OverlayCandidateSource, jobID: "job-1", path: "a.go", lines: []string{"package sample"}}
				model.overlays.Push(OverlayCandidateSource, OverlayCaller{MainView: MainViewAgents})
			},
			func(model *Model) {
				model.fixDialog.promptDirtyCursor = 0
				model.overlays.Push(OverlayPromptDirty, OverlayCaller{MainView: MainViewFiles})
			},
			func(model *Model) {
				model.shutdown = shutdownState{active: 2}
				model.overlays.Push(OverlayShutdown, OverlayCaller{MainView: MainViewAgents})
			},
		} {
			model := fixTestModel(&fakeFixService{}, size.width, size.height)
			setup(&model)
			view := model.View()
			assertScreenSize(t, view, size.width, size.height)
			if !strings.Contains(ansi.Strip(view), "Esc") {
				t.Fatalf("%dx%d overlay hid its recovery key: %q", size.width, size.height, ansi.Strip(view))
			}
		}
	}
}
