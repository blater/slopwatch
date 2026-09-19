package follow

import (
	"context"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/fixapp"
	tea "github.com/charmbracelet/bubbletea"
)

const fixUpdatesUnavailablePrefix = "Fix updates unavailable:"

// fixSubscriptionState owns the lifecycle and recovery signals for the fix
// update stream. Presentation updates remain with the caller that owns the
// agent rows.
type fixSubscriptionState struct {
	subscription    fixapp.Subscription
	stale           bool
	retryGeneration uint64
}

type fixSubscriptionRecovery struct {
	jobs     []fix.JobPresentation
	notice   string
	command  tea.Cmd
	accepted bool
}

func newFixSubscriptionState(service FixService) fixSubscriptionState {
	state := fixSubscriptionState{}
	state.subscribe(service)
	return state
}

func (state *fixSubscriptionState) subscribe(service FixService) {
	if service != nil {
		state.subscription = service.Subscribe()
	}
}

func (state *fixSubscriptionState) close() {
	if state.subscription != nil {
		_ = state.subscription.Close()
		state.subscription = nil
	}
}

func (state fixSubscriptionState) wait(service FixService) tea.Cmd {
	if service == nil || state.subscription == nil {
		return nil
	}
	subscription := state.subscription
	return func() tea.Msg {
		if err := subscription.Wait(context.Background()); err != nil {
			return fixJobsMsg{err: err}
		}
		snapshot := service.Jobs(fixapp.JobFilter{IncludeFinished: true})
		return fixJobsMsg{jobs: snapshot.Jobs}
	}
}

func (state fixSubscriptionState) next(service FixService, monitorCommand, logCommand tea.Cmd) tea.Cmd {
	return tea.Batch(state.wait(service), monitorCommand, logCommand)
}

func (state *fixSubscriptionState) markUnavailable(err error) (string, tea.Cmd) {
	state.stale = true
	state.retryGeneration++
	generation := state.retryGeneration
	return fixUpdatesUnavailablePrefix + " " + err.Error(), tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return fixRetrySubscriptionMsg{generation: generation}
	})
}

func (state *fixSubscriptionState) clearError(currentNotice string) string {
	state.stale = false
	if strings.HasPrefix(currentNotice, fixUpdatesUnavailablePrefix) {
		return ""
	}
	return currentNotice
}

func (state *fixSubscriptionState) retry(service FixService, generation uint64) fixSubscriptionRecovery {
	if service == nil || generation != state.retryGeneration {
		return fixSubscriptionRecovery{}
	}
	state.close()
	state.subscribe(service)
	snapshot := service.Jobs(fixapp.JobFilter{IncludeFinished: true})
	state.stale = false
	return fixSubscriptionRecovery{
		jobs:     snapshot.Jobs,
		notice:   "Fix updates restored",
		command:  state.wait(service),
		accepted: true,
	}
}
