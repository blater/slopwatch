package codexcli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
)

type appServerRun struct {
	request   agent.Request
	sink      agent.EventSink
	completed chan turnCompletion

	emitMu      sync.Mutex
	mu          sync.Mutex
	threadID    string
	turnID      string
	sequence    uint64
	events      int
	summary     string
	usage       agent.Usage
	emitErr     error
	terminal    bool
	finalAnswer bool
	actors      map[string]struct{}
	pendingTurn []rpcMessage
	turnReady   bool
}

func newAppServerRun(request agent.Request, sink agent.EventSink) *appServerRun {
	if sink == nil {
		sink = agent.EventSinkFunc(func(agent.Event) error { return nil })
	}
	return &appServerRun{request: request, sink: sink, completed: make(chan turnCompletion, 1), actors: make(map[string]struct{})}
}

func (run *appServerRun) setThread(value string) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.threadID != "" && run.threadID != value {
		run.setErrorLocked(errors.New("Codex App Server changed the owned thread id"))
		return
	}
	run.threadID = value
}

func (run *appServerRun) setTurn(value string) {
	run.mu.Lock()
	if run.turnID != "" && run.turnID != value {
		run.setErrorLocked(errors.New("Codex App Server changed the owned turn id"))
		run.mu.Unlock()
		return
	}
	run.turnID = value
	run.mu.Unlock()
	for {
		run.mu.Lock()
		pending := append([]rpcMessage(nil), run.pendingTurn...)
		run.pendingTurn = nil
		if len(pending) == 0 {
			run.turnReady = true
			run.mu.Unlock()
			return
		}
		run.mu.Unlock()
		for _, message := range pending {
			if message.Method == "turn/started" {
				run.handleBound(message)
			}
		}
		for _, message := range pending {
			if message.Method != "turn/started" {
				run.handleBound(message)
			}
		}
	}
}
func (run *appServerRun) emit(event agent.Event) {
	run.emitWithUpdate(event, nil)
}

func (run *appServerRun) emitWithUpdate(event agent.Event, update func()) {
	run.emitMu.Lock()
	defer run.emitMu.Unlock()
	run.mu.Lock()
	if run.emitErr != nil || run.terminal {
		run.mu.Unlock()
		return
	}
	if update != nil {
		update()
	}
	run.events++
	if run.request.Limits.MaxEvents > 0 && run.events > run.request.Limits.MaxEvents {
		run.setErrorLocked(fmt.Errorf("Codex emitted more than %d events", run.request.Limits.MaxEvents))
		run.mu.Unlock()
		return
	}
	if event.ActorID != "" {
		run.actors[event.ActorID] = struct{}{}
		if run.request.Limits.MaxActors > 0 && len(run.actors) > run.request.Limits.MaxActors {
			run.setErrorLocked(fmt.Errorf("Codex reported more than %d actors", run.request.Limits.MaxActors))
			run.mu.Unlock()
			return
		}
	}
	run.sequence++
	event.JobID, event.AttemptID, event.Sequence, event.At = run.request.JobID, run.request.AttemptID, run.sequence, time.Now()
	run.mu.Unlock()
	err := run.sink.Emit(event)
	if err != nil {
		run.mu.Lock()
		run.setErrorLocked(fmt.Errorf("emit Codex event: %w", err))
		run.mu.Unlock()
	}
}

func (run *appServerRun) stop() {
	// Serialize with the sink call so that returning from stop means no provider
	// event can still be entering the controller for this attempt.
	run.emitMu.Lock()
	run.mu.Lock()
	run.terminal = true
	run.mu.Unlock()
	run.emitMu.Unlock()
}

func (run *appServerRun) repoPath(value string) (fix.RepoPath, bool) {
	if filepath.IsAbs(value) {
		relative, err := filepath.Rel(run.request.Workspace.RepositoryRoot, value)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", false
		}
		value = relative
	}
	path, err := fix.ParseRepoPath(filepath.ToSlash(strings.TrimPrefix(value, "./")))
	return path, err == nil
}

func (run *appServerRun) setError(err error) {
	run.emitMu.Lock()
	defer run.emitMu.Unlock()
	run.mu.Lock()
	defer run.mu.Unlock()
	run.setErrorLocked(err)
}

func (run *appServerRun) setErrorLocked(err error) {
	if run.emitErr == nil && !run.terminal {
		run.emitErr = err
		run.terminal = true
		failed := turnCompletion{Status: "failed"}
		failed.Error.Message = err.Error()
		select {
		case run.completed <- failed:
		default:
		}
	}
}

func (run *appServerRun) err() error { run.mu.Lock(); defer run.mu.Unlock(); return run.emitErr }
func (run *appServerRun) hasFinalAnswer() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.finalAnswer
}
func (run *appServerRun) snapshot() (string, agent.Usage) {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.summary, run.usage
}
