package analysiscache

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestArtifactStorageCodec(t *testing.T) {
	plain := bytes.Repeat([]byte("representative repetitive evidence payload\n"), 10000)
	stored, err := encodeArtifactStorage(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored)*100 > len(plain)*30 {
		t.Fatalf("insufficient compression: %d/%d", len(stored), len(plain))
	}
	again, _ := encodeArtifactStorage(plain)
	if !bytes.Equal(stored, again) {
		t.Fatal("nondeterministic gzip")
	}
	for _, data := range [][]byte{plain, stored} {
		got, err := decodeArtifactStorage(data)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("roundtrip: %v", err)
		}
	}
	if got, _ := encodeArtifactStorage([]byte("x")); string(got) != "x" {
		t.Fatal("grew tiny artifact")
	}
	t.Logf("repetitive evidence bytes: %d -> %d", len(plain), len(stored))
}

func TestArtifactStorageBound(t *testing.T) {
	var compressed bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	chunk := make([]byte, 1<<20)
	for i := 0; i < 128; i++ {
		if _, err := writer.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	writer.Write([]byte{0})
	writer.Close()
	if _, err := decodeArtifactStorage(compressed.Bytes()); err == nil {
		t.Fatal("accepted oversized stream")
	}
	oversized := make([]byte, maxDecodedArtifact+1)
	if got, err := encodeArtifactStorage(oversized); err != nil || len(got) != len(oversized) || &got[0] != &oversized[0] {
		t.Fatal("oversized legacy fallback failed")
	}
}

func TestCompactArtifactsCompatibility(t *testing.T) {
	store := newTestStore(t)
	key := keyFor([]byte("unit"))
	unit := sampleUnit(key)
	ref, err := store.PutUnit(key, unit)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.casPath("artifacts", ref.Digest)
	physical, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decodeArtifactStorage(physical)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.LoadUnit(ref); !ok {
		t.Fatal("legacy load failed")
	}
	// Noncanonical files and symlink targets are outside the artifact layout.
	bad := filepath.Join(store.root, "artifacts", "unrecognized")
	os.WriteFile(bad, []byte("untouched"), 0600)
	link := filepath.Join(store.root, "artifacts", "link")
	os.Symlink(path, link)
	temporary := filepath.Join(filepath.Dir(path), ".tmp-in-progress")
	if err := os.WriteFile(temporary, []byte("unfinished write"), 0o600); err != nil {
		t.Fatal(err)
	}
	stats, err := store.CompactArtifacts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Examined != 3 || stats.Compacted != 1 || stats.Skipped != 2 || stats.AfterBytes >= stats.BeforeBytes {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if _, got, ok := store.LoadUnitByKey(key); !ok || got != ref {
		t.Fatal("compaction broke pointer or artifact")
	}
	next, err := store.CompactArtifacts(context.Background())
	if err != nil || next.Compacted != 0 {
		t.Fatalf("non-idempotent: %+v %v", next, err)
	}
	if data, err := os.ReadFile(temporary); err != nil || string(data) != "unfinished write" {
		t.Fatalf("changed in-progress write: %q, %v", data, err)
	}
	if data, _ := os.ReadFile(bad); string(data) != "untouched" {
		t.Fatal("changed noncanonical file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.CompactArtifacts(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	// An ordinary write upgrades identical legacy bytes and keeps the ID.
	writeAtomic(path, plain)
	repeated, err := store.PutUnit(key, unit)
	if err != nil || repeated != ref {
		t.Fatalf("repeat: %v", err)
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(current, physical) {
		t.Fatal("legacy was not upgraded")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("artifact permissions")
	}
	writeAtomic(path, []byte("replacement"))
	if _, err := store.PutUnit(key, unit); err != nil {
		t.Fatalf("replacement write failed: %v", err)
	}
}

func TestCompactionPreservesGenerationReferences(t *testing.T) {
	store := newTestStore(t)
	key := keyFor([]byte("history-unit"))
	view := keyFor([]byte("history-view"))
	unit := sampleUnit(key)
	unitRef, err := store.PutUnit(key, unit)
	if err != nil {
		t.Fatal(err)
	}
	projection := ProjectionFromReport(view, unit.Report, FreshnessProvisional)
	projectionRef, err := store.PutProjection(view, projection)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := store.CommitGeneration(view, Generation{Units: map[Key]ArtifactRef{key: unitRef}, Projection: projectionRef}); err != nil {
			t.Fatal(err)
		}
	}
	for _, ref := range []ArtifactRef{unitRef, projectionRef} {
		path, _ := store.casPath("artifacts", ref.Digest)
		data, _ := os.ReadFile(path)
		plain, err := decodeArtifactStorage(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeAtomic(path, plain); err != nil {
			t.Fatal(err)
		}
	}
	generationRoot := filepath.Join(store.workspaceDir(view), "generations")
	before := map[string]string{}
	entries, _ := os.ReadDir(generationRoot)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(generationRoot, entry.Name()))
		before[entry.Name()] = string(data)
	}
	if stats, err := store.CompactArtifacts(context.Background()); err != nil || stats.Compacted != 2 {
		t.Fatalf("%+v %v", stats, err)
	}
	loaded, ok := store.LoadGeneration(view)
	if !ok || loaded.Number != 2 || loaded.Projection != projectionRef || loaded.Units[key] != unitRef {
		t.Fatal("generation changed")
	}
	if _, ok := store.LoadProjection(projectionRef); !ok {
		t.Fatal("projection no longer readable")
	}
	for name, want := range before {
		got, _ := os.ReadFile(filepath.Join(generationRoot, name))
		if string(got) != want {
			t.Fatal("historical manifest changed")
		}
	}
}

func TestCompactionWriteFailurePreservesArtifact(t *testing.T) {
	store := newTestStore(t)
	key := keyFor([]byte("readonly-unit"))
	unit := sampleUnit(key)
	ref, err := store.PutUnit(key, unit)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.casPath("artifacts", ref.Digest)
	data, _ := os.ReadFile(path)
	plain, _ := decodeArtifactStorage(data)
	if err := writeAtomic(path, plain); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0700)
	probe, probeErr := os.CreateTemp(directory, "permission-probe")
	if probeErr == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("runner can write to chmod 0500 directory")
	}
	stats, err := store.CompactArtifacts(context.Background())
	if err == nil {
		t.Fatal("expected write permission failure")
	}
	if stats.Errors != 1 {
		t.Fatalf("%+v", stats)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, plain) {
		t.Fatal("failed write changed artifact")
	}
	if _, ok := store.LoadUnit(ref); !ok {
		t.Fatal("failed write broke read")
	}
}

func TestConcurrentArtifactCompression(t *testing.T) {
	store := newTestStore(t)
	key := keyFor([]byte("concurrent-compression"))
	unit := sampleUnit(key)
	ref, err := store.PutUnit(key, unit)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := store.casPath("artifacts", ref.Digest)
	data, _ := os.ReadFile(path)
	plain, _ := decodeArtifactStorage(data)
	if err := writeAtomic(path, plain); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 10; j++ {
				if _, err := store.CompactArtifacts(context.Background()); err != nil {
					t.Error(err)
					return
				}
				got, err := store.PutUnit(key, unit)
				if err != nil || got != ref {
					t.Errorf("put: %v", err)
					return
				}
				if _, ok := store.LoadUnit(ref); !ok {
					t.Error("torn artifact")
					return
				}
			}
		}()
	}
	workers.Wait()
}

func TestCompactionSkipsOversizedPhysicalArtifact(t *testing.T) {
	store := newTestStore(t)
	digest := DigestBytes([]byte("oversized"))
	path, _ := store.casPath("artifacts", digest)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDecodedArtifact + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	stats, err := store.CompactArtifacts(context.Background())
	if err != nil || stats.Skipped != 1 || stats.Compacted != 0 || stats.BeforeBytes != maxDecodedArtifact+1 || stats.AfterBytes != stats.BeforeBytes {
		t.Fatalf("%+v %v", stats, err)
	}
}

func TestCanceledArtifactReplacementLeavesLegacyIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	plain := bytes.Repeat([]byte("evidence"), 100)
	compressed, err := encodeArtifactStorage(plain)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(path, plain); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, changed, err := writeArtifactResult(ctx, path, compressed)
	if changed || !errors.Is(err, context.Canceled) {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, plain) {
		t.Fatal("canceled write replaced legacy artifact")
	}
}
