package fixapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/jobstore"
)

func TestRetainedJobLimitRejectsBeforeJournalOrReservationMutation(t *testing.T) {
	existing := fix.JobID("job-existing")
	store := jobstore.NewMemory()
	manager := &Manager{deps: Dependencies{Store: store}, options: Options{MaxRetainedJobs: 1, Clock: time.Now}, notify: make(chan struct{})}
	manager.current.Store(JobListSnapshot{})
	manager.logs.Store(map[fix.JobID]logSnapshot{})
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{existing: {presentation: fix.JobPresentation{ID: existing, Phase: fix.PhaseArchived}}}, order: []fix.JobID{existing}, reservations: map[string]fix.JobID{"kept": existing}}
	response := make(chan submitResponse, 1)
	manager.handleSubmit(state, submitCall{ctx: t.Context(), request: SubmitRequest{}, response: response})
	if result := <-response; result.err == nil || !strings.Contains(result.err.Error(), "retained-job limit") {
		t.Fatalf("submit error=%v", result.err)
	}
	if len(state.jobs) != 1 || state.reservations["kept"] != existing {
		t.Fatalf("state mutated: %+v", state)
	}
	records, err := store.Load(t.Context())
	if err != nil || len(records) != 0 {
		t.Fatalf("journal mutated: %v %+v", err, records)
	}
}

func TestDiscardedJobsAreRemovedByDurableCompactionAndReleaseCapacity(t *testing.T) {
	id := fix.JobID("job-discarded")
	store := jobstore.NewMemory()
	manager := &Manager{deps: Dependencies{Store: store}, options: Options{Clock: time.Now}}
	state := &controllerState{jobs: map[fix.JobID]*jobRecord{id: {presentation: fix.JobPresentation{ID: id, Revision: 2, Phase: fix.PhaseDiscarded}}}, order: []fix.JobID{id}}
	if err := manager.pruneDiscarded(state); err != nil {
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
		envelope, _ := json.Marshal(journalEnvelope{Presentation: fix.JobPresentation{ID: id, Revision: 1, Phase: fix.PhaseArchived}})
		records = append(records, jobstore.Record{Version: jobstore.RecordVersion, Sequence: uint64(index + 1), JobID: id, Revision: 1, Kind: "checkpoint", Data: envelope})
	}
	if err := validateInitialJournal(records); err != nil {
		t.Fatalf("valid retained jobs were rejected after lowering the admission-only limit: %v", err)
	}
}
