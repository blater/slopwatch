package jobstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestFileStoresOnePlainJSONDocumentPerJob(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record := Record{JobID: fix.JobID("job-one"), State: json.RawMessage(`{"presentation":{"id":"job-one","revision":7,"phase":"running"}}`)}
	if err := store.Save(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(directory, "job-one.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("job file is not plain JSON: %v", err)
	}
	presentation, ok := document["presentation"].(map[string]any)
	if !ok || presentation["id"] != "job-one" || presentation["revision"] != float64(7) {
		t.Fatalf("job document = %#v", document)
	}
	records, err := store.Load(context.Background())
	if err != nil || len(records) != 1 || records[0].JobID != record.JobID || !json.Valid(records[0].State) {
		t.Fatalf("Load() = %#v, %v", records, err)
	}
}

func TestFileOverwritesTheSingleDocumentForAJob(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for revision := uint64(1); revision <= 2; revision++ {
		state, _ := json.Marshal(map[string]uint64{"revision": revision})
		if err := store.Save(t.Context(), Record{JobID: "job", State: state}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := store.Load(t.Context())
	if err != nil || len(records) != 1 || !json.Valid(records[0].State) || !bytes.Contains(records[0].State, []byte(`"revision": 2`)) {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestFileIgnoresUnreadableJobDocuments(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "broken.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, err := store.Load(t.Context())
	if err != nil || len(records) != 0 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestFileLocksIndividualRunningJobs(t *testing.T) {
	directory := t.TempDir()
	first, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	firstLock, err := first.Lock("job-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Lock("job-one"); !errors.Is(err, ErrJobRunning) {
		t.Fatalf("second lock error = %v", err)
	}
	otherLock, err := second.Lock("job-two")
	if err != nil {
		t.Fatalf("different job was blocked: %v", err)
	}
	_ = otherLock.Close()
	_ = firstLock.Close()
	_ = first.Close()
	_ = second.Close()
}
