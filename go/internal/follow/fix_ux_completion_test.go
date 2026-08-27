package follow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/blater/slopmochi/internal/candidate"
	"github.com/blater/slopmochi/internal/fix"
	"github.com/blater/slopmochi/internal/fixapp"
)

func TestJobMonitorAndReadersLoadThroughService(t *testing.T) {
	job := fix.JobPresentation{ID: "job-1", Phase: fix.PhaseRunning, ProfileLabel: "Codex", ModelLabel: "gpt-5.6-sol", EffortLabel: "high", Goal: "SCORE <= 100", CurrentAction: "Running tests", AttemptOrdinal: 1, UpdatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Actors: []fix.ActorPresentation{{ID: "primary", CurrentAction: "Editing a.go"}, {ID: "reviewer", ParentID: "primary", CurrentAction: "Reviewing"}},
		Usage:  fix.UsagePresentation{InputTokens: 100, CachedTokens: 25, OutputTokens: 30, ReasoningTokens: 10}, UsageReported: true,
		Targets: []fix.FilePresentation{{Path: "a.go", BaselineScore: 120}}}
	service := &fakeFixService{
		jobs:      fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{job}},
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
	for _, want := range []string{"INSPECT", "RUNNING", "Running tests", "Focused file: a.go", "Agent: codex · gpt-5.6-sol · high", "ACTORS", "primary", "reviewer", "Tokens: input 100", "cached 25"} {
		if !strings.Contains(monitor, want) {
			t.Fatalf("monitor missing %q: %q", want, monitor)
		}
	}
	for _, unwanted := range []string{"ACTIVITY", "Read a.go", "Target status:", "Scope:"} {
		if strings.Contains(monitor, unwanted) {
			t.Fatalf("inspect contains unwanted %q: %q", unwanted, monitor)
		}
	}
	service.log.Entries = append(service.log.Entries, fixapp.LogEntry{At: time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC), Summary: "Edited a.go"})
	job.UpdatedAt = job.UpdatedAt.Add(time.Second)
	job.CurrentAction = "Editing a.go"
	service.jobs = fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{job}}
	command = model.handleFixJobs(fixJobsMsg{jobs: []fix.JobPresentation{job}})
	if command == nil {
		t.Fatal("open monitor did not schedule activity refresh on subscription advance")
	}
	model.handleJobMonitor(command().(jobMonitorMsg))
	if text := ansi.Strip(model.View()); !strings.Contains(text, "Editing a.go") || strings.Contains(text, "Edited a.go") {
		t.Fatalf("inspect did not refresh state cleanly: %q", text)
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
}

func TestJobLogOpensAtEndFollowsUpdatesAndScrollsBothWays(t *testing.T) {
	job := fix.JobPresentation{ID: "job-live", Phase: fix.PhaseRunning, UpdatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	entries := make([]fixapp.LogEntry, 30)
	for index := range entries {
		entries[index] = fixapp.LogEntry{
			At:      time.Date(2026, 1, 1, 12, 0, index, 0, time.UTC),
			Summary: fmt.Sprintf("entry-%02d %s", index, strings.Repeat("detail ", 20)),
		}
	}
	service := &fakeFixService{
		jobs: fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{job}},
		log:  fixapp.LogPage{Entries: entries, Complete: true},
	}
	model := fixTestModel(service, 60, 12)
	model.agents.Jobs = []fix.JobPresentation{job}
	model.handleJobReader(model.openJobLog(job.ID)().(jobReaderMsg))

	if !model.jobReader.follow || model.jobReader.offset != model.jobReaderMaxOffset() {
		t.Fatalf("log did not open following its end: %+v", model.jobReader)
	}
	bottom := model.jobReader.offset
	page := model.jobReaderPageSize()
	model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	if model.jobReader.offset != max(0, bottom-page) || model.jobReader.follow {
		t.Fatalf("Ctrl-B did not move one page back: %+v", model.jobReader)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlF})
	if model.jobReader.offset != bottom || !model.jobReader.follow {
		t.Fatalf("Ctrl-F did not move one page forward: %+v", model.jobReader)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if model.jobReader.offset != 0 || model.jobReader.follow {
		t.Fatalf("g did not jump to the top: %+v", model.jobReader)
	}
	model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if model.jobReader.offset != bottom || !model.jobReader.follow {
		t.Fatalf("G did not jump to the bottom and resume following: %+v", model.jobReader)
	}
	visible := ansi.Strip(model.View())
	if !strings.Contains(visible, "entry-29") || !strings.Contains(visible, "entry-22") {
		t.Fatalf("log did not show latest entry with previous entries above it: %q", visible)
	}

	model.handleJobReaderKey(tea.KeyMsg{Type: tea.KeyUp})
	pausedAt := model.jobReader.offset
	if model.jobReader.follow {
		t.Fatal("scrolling back did not pause live following")
	}
	model.handleJobReaderKey(tea.KeyMsg{Type: tea.KeyRight})
	if model.jobReader.horizontal == 0 {
		t.Fatal("right arrow did not scroll a long log line horizontally")
	}
	model.handleJobReaderKey(tea.KeyMsg{Type: tea.KeyLeft})
	if model.jobReader.horizontal != 0 {
		t.Fatalf("left arrow did not restore the horizontal position: %d", model.jobReader.horizontal)
	}

	service.log.Entries = append(service.log.Entries, fixapp.LogEntry{At: time.Now(), Summary: "new live entry"})
	job.UpdatedAt = job.UpdatedAt.Add(time.Second)
	refresh := model.handleFixJobs(fixJobsMsg{jobs: []fix.JobPresentation{job}})
	if refresh == nil {
		t.Fatal("job update did not schedule a live log refresh")
	}
	refreshMessage := refresh().(jobReaderMsg)
	if !refreshMessage.increment || len(refreshMessage.lines) != 1 {
		t.Fatalf("live refresh reloaded old transcript entries: %+v", refreshMessage)
	}
	model.handleJobReader(refreshMessage)
	if model.jobReader.offset != pausedAt || model.jobReader.follow {
		t.Fatalf("live refresh moved a paused reader: %+v", model.jobReader)
	}

	model.handleJobReaderKey(tea.KeyMsg{Type: tea.KeyEnd})
	if !model.jobReader.follow || model.jobReader.offset != model.jobReaderMaxOffset() {
		t.Fatalf("End did not resume following at the latest entry: %+v", model.jobReader)
	}
	if text := ansi.Strip(model.View()); !strings.Contains(text, "new live entry") {
		t.Fatalf("resumed log did not show the latest entry: %q", text)
	}
	model.handleJobReaderKey(tea.KeyMsg{Type: tea.KeyEsc})
	if model.jobReader.lines != nil {
		t.Fatal("closing the log retained its display copy")
	}
}

func TestJobLogDisplayDropsSubsecondTimestampNoise(t *testing.T) {
	for input, want := range map[string]string{
		"2026-08-27T01:41:32.92873+01:00  runtime_message": "2026-08-27T01:41:32+01:00  runtime_message",
		"Started: 2026-08-27T01:41:32.92873+01:00":         "Started: 2026-08-27T01:41:32+01:00",
		"Finished: 2026-08-27T01:41:32.92873+01:00":        "Finished: 2026-08-27T01:41:32+01:00",
		"Current measurements:":                            "Current measurements:",
	} {
		if got := jobLogDisplayLine(input); got != want {
			t.Errorf("jobLogDisplayLine(%q) = %q, want %q", input, got, want)
		}
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
	job := fix.JobPresentation{ID: "job-1", Phase: fix.PhaseRunning, CurrentAction: "starting", UpdatedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	service := &fakeFixService{
		jobs: fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{job}},
		log:  fixapp.LogPage{Entries: []fixapp.LogEntry{{Summary: "first"}}, Complete: true},
	}
	model := fixTestModel(service, 80, 24)
	model.mainView = MainViewAgents
	model.agents.Jobs = []fix.JobPresentation{job}
	initial := model.openJobMonitor(job.ID, "")

	job.UpdatedAt = job.UpdatedAt.Add(time.Second)
	job.CurrentAction = "second"
	service.jobs = fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{job}}
	if command := model.handleFixJobs(fixJobsMsg{jobs: []fix.JobPresentation{job}}); command != nil {
		t.Fatal("subscription burst started a second refresh while the initial monitor load was in flight")
	}
	job.UpdatedAt = job.UpdatedAt.Add(time.Second)
	job.CurrentAction = "third"
	service.jobs = fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{job}}
	if command := model.handleFixJobs(fixJobsMsg{jobs: []fix.JobPresentation{job}}); command != nil {
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
	if model.jobMonitor.job.CurrentAction != "third" || len(model.jobMonitor.activity) != 0 {
		t.Fatalf("coalesced inspect refresh did not load latest state without logs: %#v %#v", model.jobMonitor.job, model.jobMonitor.activity)
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

func TestFilesShowAggregateAndPerFileFixMarkerBeforeScoreChange(t *testing.T) {
	model := fixTestModel(&fakeFixService{}, 100, 12)
	model.options.TrendWindow = time.Minute
	model.agents.Jobs = []fix.JobPresentation{{ID: "job-1", Phase: fix.PhaseRunning, Targets: []fix.FilePresentation{{Path: "a.go"}}}}
	state := model.rows["a.go"]
	state.scoreChangedAt = time.Now()
	state.movementDelta = -1
	model.rows["a.go"] = state
	text := ansi.Strip(model.View())
	if !strings.Contains(text, "AGENTS 1") || !strings.Contains(text, "▶") || strings.Contains(text, "FIX JOB") {
		t.Fatalf("Files omitted fix observability: %q", text)
	}
	row := ansi.Strip(model.renderRow(model.document.Files[0], false))
	fixAt, changeAt, scoreAt := strings.Index(row, "▶"), strings.Index(row, "↓"), strings.Index(row, "120")
	if fixAt < 0 || changeAt != fixAt+len("▶") || scoreAt <= changeAt {
		t.Fatalf("fix/change markers are not immediately left of SCORE: %q", row)
	}
}

func TestCanceledJobUsesCanceledText(t *testing.T) {
	job := fix.JobPresentation{Phase: fix.PhaseFailed, Issue: &fix.JobIssue{Code: "canceled", Summary: "Job canceled"}}
	if got := agentPhaseText(job); got != "CANCELED" {
		t.Fatalf("phase text=%q", got)
	}
}

func TestFixSubscriptionErrorIsRecoverable(t *testing.T) {
	service := &fakeFixService{jobs: fixapp.JobListSnapshot{Jobs: []fix.JobPresentation{{ID: "job-9", Phase: fix.PhaseRunning}}}}
	model := fixTestModel(service, 80, 24)
	if command := model.handleFixJobs(fixJobsMsg{err: errors.New("wake failed")}); command == nil || !model.fixUpdatesStale {
		t.Fatal("subscription error did not enter recoverable stale state")
	}
	generation := model.fixRetryGeneration
	command := model.retryFixSubscription(fixRetrySubscriptionMsg{generation: generation})
	if command == nil || model.fixUpdatesStale || service.subscriptions != 1 {
		t.Fatalf("subscription recovery failed: stale=%t subscriptions=%d", model.fixUpdatesStale, service.subscriptions)
	}
}

func TestFixValidationExplainsMissingBranch(t *testing.T) {
	input := readyFixInput("a.go")
	input.DeliveryPlan = fix.DeliveryPlan{Workspace: fix.WorkspaceWorktree, Git: fix.GitCommitNewBranch, Publish: fix.PublishPullRequest}
	input.BranchName = ""
	if got := fixPreflightSummary(input); !strings.Contains(got, "branch name") {
		t.Fatalf("diagnostic=%q", got)
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
	for _, want := range []string{"Tab switches Files/Agents", "x opens Fix", "cancel-all", "C cancels", "v opens candidate source"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q", want)
		}
	}
}

func TestFeatureSettingsAutoSaveOnExitAndUseCompactFullScreen(t *testing.T) {
	store := &settingsConfigStore{resolved: settingsResolved()}
	model := settingsModel(configConcurrency, settingsResolved(), store)
	model.width, model.height = 40, 10
	model.settings = false
	model.configSettings.working.Concurrency.MaxAgents++
	model.configSettings.dirty = true
	save := model.closeConfigSettings()
	if save == nil || !model.configSettings.open || !model.configSettings.saving || model.hasOverlay(OverlaySettingsDirty) {
		t.Fatal("dirty settings did not begin an automatic save")
	}
	model.handleConfigSaved(save().(configSavedMsg))
	if model.configSettings.open || !model.settings || store.saveCalls != 1 {
		t.Fatal("automatic save did not return to Settings")
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
