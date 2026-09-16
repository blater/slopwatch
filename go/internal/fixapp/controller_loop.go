package fixapp

import (
	"errors"
	"time"

	"github.com/blater/slopwatch/internal/fix"
)

// run owns the controller goroutine. All job mutation is serialized here;
// workers only return immutable outcomes through results and events.
func (manager *controller) run() {
	state := manager.state
	manager.restore()
	close(manager.ready)
	sharedRefresh := time.NewTicker(time.Second)
	defer sharedRefresh.Stop()
	defer manager.closeController()

	for {
		manager.schedule()
		if state.canClose() {
			return
		}
		select {
		case request := <-manager.requests:
			manager.dispatchRequest(request)
		case update := <-manager.events:
			manager.dispatchUpdate(update)
		case result := <-manager.results:
			manager.handleResult(result)
		case <-sharedRefresh.C:
			manager.refreshSharedJobs()
		}
	}
}

func newControllerState() *controllerState {
	return &controllerState{jobs: map[fix.JobID]*jobRecord{}, reservations: map[string]fix.JobID{}}
}

func (state *controllerState) canClose() bool {
	return state.shuttingDown && state.agentsRunning == 0 && state.verifiersRunning == 0 && state.otherRunning == 0
}

func (manager *controller) closeController() {
	state := manager.state
	manager.closed.Store(true)
	closeErr := errors.Join(manager.persistence.store.Close(), manager.publication.candidates.Close())
	manager.notifyMu.Lock()
	close(manager.notify)
	manager.notify = make(chan struct{})
	manager.notifyMu.Unlock()
	close(manager.done)
	for _, waiter := range state.shutdownWaiters {
		waiter <- closeErr
	}
}

func (manager *controller) dispatchRequest(request any) {
	state := manager.state
	switch value := request.(type) {
	case runCall:
		manager.handleRun(value)
	case commandCall:
		manager.handleCommand(value)
	case candidateCall:
		manager.handleCandidateRequest(value)
	case jobsCall:
		value.response <- state.jobList(value.filter)
	case transcriptCall:
		value.response <- manager.transcriptPage(value.id, value.cursor, value.limit)
	case shutdownCall:
		manager.handleShutdown(value)
	case reconfigureCall:
		manager.handleReconfigure(value)
	}
}

func (manager *controller) handleCandidateRequest(call candidateCall) {
	state := manager.state
	record, found := state.jobs[call.id]
	if found && record.candidate != nil {
		call.response <- candidateResponse{identity: *record.candidate,
			previewBytes: record.input.Preferences.Concurrency.MaxCandidatePreviewBytes,
			previewLines: record.input.Preferences.Concurrency.MaxCandidatePreviewLines, ok: true}
		return
	}
	call.response <- candidateResponse{}
}

func (manager *controller) dispatchUpdate(update agentUpdate) {
	switch {
	case update.prompt != nil:
		manager.handlePrompt(update.job, update.attempt, *update.prompt)
	case update.barrier != nil:
		close(update.barrier)
	default:
		manager.handleEvent(update.event)
	}
}
