package fixapp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

type saveCountingStore struct {
	delegate *jobstore.Memory
	mu       sync.Mutex
	saves    int
}

func (store *saveCountingStore) Save(ctx context.Context, record jobstore.Record) error {
	store.mu.Lock()
	store.saves++
	store.mu.Unlock()
	return store.delegate.Save(ctx, record)
}

func (store *saveCountingStore) Load(ctx context.Context) ([]jobstore.Record, error) {
	return store.delegate.Load(ctx)
}

func (store *saveCountingStore) Lock(job fix.JobID) (jobstore.Lock, error) {
	return store.delegate.Lock(job)
}

func (store *saveCountingStore) Close() error { return store.delegate.Close() }

func (store *saveCountingStore) saveCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.saves
}

func TestScheduleAgentSaveFailureRollsBackSlotAndDoesNotLaunchWorker(t *testing.T) {
	manager := bareTestManager(Dependencies{Store: &failSaveStore{}}, Options{Clock: time.Now, MaxAgents: 1})
	state := newControllerState()
	manager.state = state
	job := fix.JobID("save-failure-job")
	record := &jobRecord{input: FixInput{DeliveryPlan: testPushPlan}, presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseQueued}}
	state.jobs[job], state.order = record, []fix.JobID{job}

	manager.scheduleAgents()

	if state.agentsRunning != 0 || record.cancel != nil {
		t.Fatalf("failed launch left scheduler state running=%d cancel=%t", state.agentsRunning, record.cancel != nil)
	}
	if record.presentation.Phase != fix.PhaseFailed {
		t.Fatalf("failed launch phase=%s, want failed", record.presentation.Phase)
	}
}

func TestScheduleExistingCandidatePersistsLaunchOnce(t *testing.T) {
	store := &saveCountingStore{delegate: jobstore.NewMemory()}
	manager := bareTestManager(Dependencies{Store: store}, Options{Clock: time.Now, MaxAgents: 1})
	manager.events = nil
	manager.done = closedSignal()
	manager.workers.events = nil
	manager.workers.done = manager.done
	state := newControllerState()
	manager.state = state
	job := fix.JobID("existing-candidate-job")
	record := &jobRecord{input: FixInput{DeliveryPlan: testPushPlan},
		presentation: fix.JobPresentation{ID: job, Phase: fix.PhaseQueued}, candidate: &fix.CandidateIdentity{Job: job}}
	state.jobs[job], state.order = record, []fix.JobID{job}

	manager.scheduleAgents()

	if got := store.saveCount(); got != 1 {
		t.Fatalf("existing candidate launch saves=%d, want exactly one", got)
	}
	if state.agentsRunning != 1 || record.presentation.CurrentAction != "Starting agent" {
		t.Fatalf("launch state running=%d action=%q", state.agentsRunning, record.presentation.CurrentAction)
	}
}

func closedSignal() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}
