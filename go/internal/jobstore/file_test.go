package jobstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/fix"
)

func TestFileRoundTripAndIgnoresTornTail(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := OpenFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"state": "queued"})
	appended, err := store.Append(context.Background(), Record{JobID: fix.JobID("job-1"), Kind: "admitted", Data: data})
	if err != nil || appended.Sequence != 1 {
		t.Fatalf("Append() = %#v, %v", appended, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(directory, journalName)
	file, err := os.OpenFile(journal, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{0, 0, 0, 20, 1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	file.Close()
	reopened, err := OpenFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.Load(context.Background())
	if err != nil || len(records) != 1 || records[0].Kind != "admitted" {
		t.Fatalf("Load() = %#v, %v", records, err)
	}
}

func TestFileRejectsSymlinkJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, journalName)); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenFile(directory); err == nil {
		store.Close()
		t.Fatal("symlink journal was accepted")
	}
}

func TestFileHoldsExclusiveDirectoryLease(t *testing.T) {
	directory := t.TempDir()
	first, err := OpenFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := OpenFile(directory); err == nil {
		_ = second.Close()
		t.Fatal("second writer acquired an already-owned job store")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := OpenFile(directory)
	if err != nil {
		t.Fatalf("lease was not released: %v", err)
	}
	_ = third.Close()
}

func TestFileRejectsSymlinkLease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "lease-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, ".jobs.lock")); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenFile(directory); err == nil {
		_ = store.Close()
		t.Fatal("symlink lease was accepted")
	}
}

func TestFilePoisonsWriterAfterPartialDurabilityFailure(t *testing.T) {
	store, err := OpenFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.file.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"state": "queued"})
	if _, err := store.Append(context.Background(), Record{JobID: "job", Kind: "test", Data: data}); err == nil {
		t.Fatal("closed journal write unexpectedly succeeded")
	}
	if _, err := store.Append(context.Background(), Record{JobID: "job", Kind: "test", Data: data}); err == nil || !strings.Contains(err.Error(), "previous durable write failure") {
		t.Fatalf("poisoned store accepted another append: %v", err)
	}
	_ = store.Close()
}

func TestFileCompactAtomicallyReplacesWithCompleteCheckpoints(t *testing.T) {
	directory := t.TempDir()
	store, err := OpenFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if _, err := store.Append(context.Background(), Record{JobID: "job-old", Revision: uint64(index + 1), Kind: "event", Data: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	checkpoints := []Record{
		{JobID: "job-one", Revision: 7, Kind: "checkpoint", Data: json.RawMessage(`{"state":"one"}`)},
		{JobID: "job-two", Revision: 4, Kind: "checkpoint", Data: json.RawMessage(`{"state":"two"}`)},
	}
	if err := store.Compact(context.Background(), checkpoints); err != nil {
		t.Fatal(err)
	}
	appended, err := store.Append(context.Background(), Record{JobID: "job-one", Revision: 8, Kind: "after", Data: json.RawMessage(`{}`)})
	if err != nil || appended.Sequence != 3 {
		t.Fatalf("Append after compact = %+v, %v", appended, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.Load(context.Background())
	if err != nil || len(records) != 3 || records[0].JobID != "job-one" || records[1].JobID != "job-two" || records[2].Sequence != 3 {
		t.Fatalf("compacted records = %+v, %v", records, err)
	}
}

func TestFileIgnoresAbandonedTornCheckpointTemporary(t *testing.T) {
	directory := t.TempDir()
	store, err := OpenFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), Record{JobID: "job-safe", Revision: 1, Kind: "admitted", Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".jobs.checkpoint-crashed"), []byte{0, 0, 4, 0, 1}, 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFile(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err := reopened.Load(context.Background())
	if err != nil || len(records) != 1 || records[0].JobID != "job-safe" {
		t.Fatalf("live journal was affected by abandoned checkpoint: %+v, %v", records, err)
	}
}
