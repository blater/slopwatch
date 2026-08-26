package fixapp

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

func TestFinishedJobIsPrunedToMakeRoomForNextJob(t *testing.T) {
	existing := fix.JobID("job-existing")
	store := jobstore.NewMemory()
	manager := &Manager{deps: Dependencies{Store: store}, options: Options{MaxRetainedJobs: 1, Clock: time.Now}, notify: make(chan struct{})}
	manager.current.Store(JobListSnapshot{})
	manager.logs.Store(map[fix.JobID]logSnapshot{})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{existing: {presentation: fix.JobPresentation{ID: existing, Phase: fix.PhaseCompleted}}}, order: []fix.JobID{existing}}
	if err := manager.pruneFinished(state, true); err != nil {
		t.Fatal(err)
	}
	records, err := store.Load(t.Context())
	if err != nil || len(records) != 0 || len(state.jobs) != 0 {
		t.Fatalf("finished job retained: records=%+v jobs=%+v err=%v", records, state.jobs, err)
	}
}

func TestDiscardedJobsAreRemovedByDurableCompactionAndReleaseCapacity(t *testing.T) {
	id := fix.JobID("job-discarded")
	store := jobstore.NewMemory()
	manager := &Manager{deps: Dependencies{Store: store}, options: Options{Clock: time.Now}}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{id: {presentation: fix.JobPresentation{ID: id, Revision: 2, Phase: fix.PhaseDiscarded}}}, order: []fix.JobID{id}}
	if err := manager.pruneFinished(state, true); err != nil {
		t.Fatal(err)
	}
	records, err := store.Load(t.Context())
	if err != nil || len(records) != 0 || len(state.jobs) != 0 {
		t.Fatalf("prune records=%+v jobs=%+v err=%v", records, state.jobs, err)
	}
}

func TestRecoveryAcceptsExistingJobsAboveReducedAdmissionLimit(t *testing.T) {
	var records []jobstore.Record
	for index, id := range []fix.JobID{"job-one", "job-two"} {
		envelope, _ := json.Marshal(journalEnvelope{Presentation: fix.JobPresentation{ID: id, Revision: 1, Phase: fix.PhaseCompleted}})
		records = append(records, jobstore.Record{Version: jobstore.RecordVersion, Sequence: uint64(index + 1), JobID: id, Revision: 1, Kind: "checkpoint", Data: envelope})
	}
	if err := validateInitialJournal(records); err != nil {
		t.Fatalf("valid retained jobs were rejected after lowering the admission-only limit: %v", err)
	}
}

func TestStartupPruningKeepsTheConfiguredHistoryLimit(t *testing.T) {
	for _, test := range []struct {
		name string
		jobs int
	}{
		{name: "exact limit", jobs: 2},
		{name: "above limit", jobs: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := jobstore.NewMemory()
			manager := &Manager{deps: Dependencies{Store: store}, options: Options{MaxRetainedJobs: 2, Clock: time.Now}}
			state := &controllerState{jobs: map[fix.JobID]*jobRecord{}}
			for index := 0; index < test.jobs; index++ {
				id := fix.JobID(fmt.Sprintf("job-%d", index))
				state.jobs[id] = &jobRecord{presentation: fix.JobPresentation{ID: id, Phase: fix.PhaseCompleted}}
				state.order = append(state.order, id)
			}
			if err := manager.pruneFinished(state, false); err != nil {
				t.Fatal(err)
			}
			if len(state.jobs) != 2 {
				t.Fatalf("startup retained %d jobs, want 2", len(state.jobs))
			}
		})
	}
}
